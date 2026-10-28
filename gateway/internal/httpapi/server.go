// Package httpapi is the gateway's HTTP layer: routing, middleware and
// handlers. Every tenant request is checked twice: (1) policy.Allowed
// here, (2) Postgres RLS via session variables set on every tenant
// transaction. Default is deny.
package httpapi

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/db"
	"cloudbeta/gateway/internal/deploy"
	"cloudbeta/gateway/internal/policy"
)

type Config struct {
	JWTSecret string
	// CookieSecure marks session cookies Secure. Keep true anywhere
	// served over HTTPS; local http://localhost needs false in Safari.
	CookieSecure bool
	// AllowedOrigins are the browser origins that may call the API with
	// credentials (the dashboard, the sample app). Deployed apps on
	// DeployPublicHost within the deploy port range are allowed too.
	AllowedOrigins   []string
	DeployPublicHost string
	DeployProbeHost  string
	DeployImage      string
}

// ConfigFromEnv reads gateway configuration with local-dev defaults.
func ConfigFromEnv() Config {
	c := Config{
		JWTSecret:        os.Getenv("JWT_SECRET"),
		CookieSecure:     os.Getenv("COOKIE_SECURE") != "false",
		DeployPublicHost: envOr("DEPLOY_PUBLIC_HOST", "http://localhost"),
		DeployProbeHost:  os.Getenv("DEPLOY_PROBE_HOST"),
		DeployImage:      envOr("DEPLOY_IMAGE", "cloudbeta-sample-app"),
	}
	for _, o := range strings.Split(envOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, strings.TrimSuffix(o, "/"))
		}
	}
	if !strings.HasPrefix(c.DeployPublicHost, "http://") && !strings.HasPrefix(c.DeployPublicHost, "https://") {
		c.DeployPublicHost = "http://" + c.DeployPublicHost
	}
	c.DeployPublicHost = strings.TrimSuffix(c.DeployPublicHost, "/")
	return c
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type Server struct {
	DB  *db.DB
	Pol *policy.Policy
	Cfg Config
	// Deployer, when set, is the Docker deployer. When nil, handlers
	// build one from DOCKER_HOST on demand; a missing/unreachable
	// daemon then yields GATEWAY_005, never a fake deploy. The gateway
	// itself must still start without Docker.
	Deployer deploy.Deployer
}

// Handler returns the gateway's full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Public.
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/policy", s.handlePolicy)
	mux.HandleFunc("GET /share/{token}", s.handleSharePublic)

	// Authenticated.
	a := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.authenticate(h)) }
	a("GET /api/v1/me", s.handleMe)
	a("POST /api/v1/auth/switch-company", s.handleSwitchCompany)
	a("GET /api/v1/companies", s.handleListCompanies)
	a("POST /api/v1/companies", s.handleCreateCompany)
	a("GET /api/v1/memberships", s.handleListMembers)
	a("POST /api/v1/memberships", s.handleAddMember)
	a("GET /api/v1/apps", s.handleListApps)
	a("POST /api/v1/apps", s.handleCreateApp)
	a("DELETE /api/v1/apps/{id}", s.handleAppDelete)
	a("POST /api/v1/apps/{id}/deploy", s.handleAppDeploy)
	a("POST /api/v1/apps/{id}/stop", s.handleAppStop)
	a("GET /api/v1/apps/{id}/health", s.handleAppHealth)
	a("GET /api/v1/clients", s.handleListClients)
	a("POST /api/v1/clients", s.handleCreateClient)
	a("GET /api/v1/clients/{id}", s.handleGetClient)
	a("PUT /api/v1/clients/{id}", s.handleUpdateClient)
	a("DELETE /api/v1/clients/{id}", s.handleDeleteClient)
	a("GET /api/v1/audit", s.handleAudit)
	a("GET /api/v1/audit/summary", s.handleAuditSummary)
	a("GET /api/v1/shares", s.handleListShares)
	a("POST /api/v1/shares", s.handleCreateShare)
	a("POST /api/v1/shares/{id}/revoke", s.handleShareRevoke)
	a("POST /api/v1/heal/detect", s.handleHealDetect)
	a("POST /api/v1/heal/simulate-break", s.handleHealSimulate)
	a("GET /api/v1/heal/proposals", s.handleHealProposals)
	a("POST /api/v1/heal/{id}/approve", s.handleHealApprove)
	a("POST /api/v1/heal/{id}/apply", s.handleHealApply)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, apperr.GatewayNotFound) })
	return s.recoverer(s.cors(logRequests(mux)))
}

// cors allows credentialed requests only from known origins. A wildcard
// is never sent, since session cookies ride on these requests.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !s.originAllowed(origin) {
				fail(w, apperr.GatewayOrigin)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Company-ID")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Max-Age", "600")
			h.Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	origin = strings.TrimSuffix(origin, "/")
	for _, o := range s.Cfg.AllowedOrigins {
		if o == origin {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil || u.Port() == "" {
		return false
	}
	if u.Scheme+"://"+u.Hostname() != s.Cfg.DeployPublicHost {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	min, max := deploy.PortRange()
	return err == nil && port >= min && port <= max
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, v)
				fail(w, apperr.GatewayInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logRequests writes one line per API request. Share tokens are never
// logged: the /share/ path is reduced to its prefix.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		path := r.URL.Path
		if strings.HasPrefix(path, "/share/") {
			path = "/share/<redacted>"
		}
		if path != "/health" {
			log.Printf("%s %s %d %s", r.Method, path, rec.status, time.Since(start).Round(time.Millisecond))
		}
	})
}
