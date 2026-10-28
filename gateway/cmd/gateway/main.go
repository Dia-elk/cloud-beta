// Gateway: control plane + permission gateway for the Cloud beta.
// Every tenant request is checked twice: (1) policy.Allowed in this
// process, (2) Postgres RLS in the database, using session variables
// set on every tenant transaction. Default is deny.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cloudbeta/gateway/internal/auth"
	"cloudbeta/gateway/internal/db"
	"cloudbeta/gateway/internal/deploy"
	"cloudbeta/gateway/internal/policy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey string

const (
	keyUser    ctxKey = "user"
	keyCompany ctxKey = "company"
	keyRole    ctxKey = "role"
	keyEmail   ctxKey = "email"
)

type Server struct {
	DB     *db.DB
	Pol    *policy.Policy
	Secret string
	// Deployer, when set, is the Docker deployer. When nil, handlers
	// build one from DOCKER_HOST on demand; a missing/unreachable
	// daemon then yields 503 docker-unavailable, never a fake deploy.
	// The gateway itself must still start without Docker.
	Deployer deploy.Deployer
}

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "beta-dev-secret-change-me"
		log.Println("WARNING: JWT_SECRET unset, using dev default (beta only)")
	}
	pol, err := policy.Load(os.Getenv("POLICY_PATH"))
	if err != nil {
		// Try default locations.
		if pol, err = policy.Load("policy.json"); err != nil {
			if pol, err = policy.Load("/gateway/policy.json"); err != nil {
				log.Fatalf("load policy: %v", err)
			}
		}
	}
	ctx := context.Background()
	d, err := db.Connect(ctx, os.Getenv("DATABASE_URL"), os.Getenv("APP_DATABASE_URL"), pol)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	s := &Server{DB: d, Pol: pol, Secret: secret}
	if dep, err := deploy.NewFromEnv(); err != nil {
		// Not fatal: the gateway serves everything except deploy
		// without Docker; deploy requests then return 503.
		log.Printf("docker deployer unavailable at startup (deploy will return 503): %v", err)
	} else {
		s.Deployer = dep
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("/api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/policy", s.handlePolicy)
	mux.HandleFunc("/share/", s.handleSharePublic) // public, token only, single row
	// Authenticated API.
	authMux := http.NewServeMux()
	authMux.HandleFunc("/api/v1/me", s.handleMe)
	authMux.HandleFunc("/api/v1/companies", s.handleCompanies)
	authMux.HandleFunc("/api/v1/memberships", s.handleMemberships)
	authMux.HandleFunc("/api/v1/apps", s.handleApps)
	authMux.HandleFunc("/api/v1/apps/", s.handleAppOne)
	authMux.HandleFunc("/api/v1/clients", s.handleClients)
	authMux.HandleFunc("/api/v1/clients/", s.handleClientOne)
	authMux.HandleFunc("/api/v1/audit", s.handleAudit)
	authMux.HandleFunc("/api/v1/audit/summary", s.handleAuditSummary)
	authMux.HandleFunc("/api/v1/shares", s.handleShares)
	authMux.HandleFunc("/api/v1/shares/", s.handleShareRevoke)
	authMux.HandleFunc("/api/v1/heal/detect", s.handleHealDetect)
	authMux.HandleFunc("/api/v1/heal/simulate-break", s.handleHealSimulate)
	authMux.HandleFunc("/api/v1/heal/proposals", s.handleHealProposals)
	authMux.HandleFunc("/api/v1/heal/", s.handleHealOne)
	mux.Handle("/", s.authMiddleware(authMux))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("gateway listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, cors(mux)))
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Company-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// dbError reports a failed tenant query or write as 500 instead of a
// silent 200 with empty data. The real error is logged, never sent to
// the client.
func dbError(w http.ResponseWriter, where string, err error) {
	log.Printf("db error in %s: %v", where, err)
	writeJSON(w, 500, map[string]string{"error": "database error", "code": "DB_ERROR"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only guard /api/v1/*; /health, /share and /api/v1/policy are public above.
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims, err := auth.VerifyAccess(s.Secret, tok)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		companyID := claims.CompanyID
		if h := r.Header.Get("X-Company-ID"); h != "" {
			companyID = h
		}
		var role string
		err = s.DB.Owner.QueryRow(r.Context(),
			`SELECT role FROM memberships WHERE user_id=$1 AND company_id=$2`, claims.UserID, companyID).Scan(&role)
		if err != nil {
			writeJSON(w, 403, map[string]string{"error": "no membership in this company"})
			return
		}
		ctx := context.WithValue(r.Context(), keyUser, claims.UserID)
		ctx = context.WithValue(ctx, keyCompany, companyID)
		ctx = context.WithValue(ctx, keyRole, role)
		ctx = context.WithValue(ctx, keyEmail, claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userCtx(r *http.Request) (userID, companyID, role, email string) {
	userID, _ = r.Context().Value(keyUser).(string)
	companyID, _ = r.Context().Value(keyCompany).(string)
	role, _ = r.Context().Value(keyRole).(string)
	email, _ = r.Context().Value(keyEmail).(string)
	return
}

// tenant runs fn in a transaction with RLS session variables set.
func (s *Server) tenant(ctx context.Context, companyID, userID, role string, fn func(pgx.Tx) error) error {
	tx, err := s.DB.App.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_company_id', $1, true)`, companyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_user_id', $1, true)`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_role', $1, true)`, role); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) audit(r *http.Request, start time.Time, action, table, targetID, targetCompany string, allowed bool, reason string) {
	u, c, role, _ := userCtx(r)
	s.DB.Audit(r.Context(), c, u, role, "", action, table, targetID, targetCompany, allowed, reason, time.Since(start).Milliseconds())
}

func (s *Server) deny(w http.ResponseWriter, r *http.Request, start time.Time, action, table, targetID, targetCompany, reason string) {
	s.audit(r, start, action, table, targetID, targetCompany, false, reason)
	writeJSON(w, 403, map[string]string{"error": reason})
}

// --- Auth ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		CompanyName string `json:"company_name"`
	}
	if err := readJSON(r, &in); err != nil || in.Email == "" || in.CompanyName == "" {
		writeJSON(w, 400, map[string]string{"error": "email, password and company_name required"})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	userID, companyID := db.ID("usr"), db.ID("cmp")
	_, err = s.DB.Owner.Exec(r.Context(),
		`INSERT INTO users(id,email,password_hash) VALUES ($1,$2,$3)`, userID, in.Email, hash)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "email already registered"})
		return
	}
	_, _ = s.DB.Owner.Exec(r.Context(), `INSERT INTO companies(id,name) VALUES ($1,$2)`, companyID, in.CompanyName)
	_, _ = s.DB.Owner.Exec(r.Context(), `INSERT INTO memberships(user_id,company_id,role) VALUES ($1,$2,'owner')`, userID, companyID)
	tok, _ := auth.SignAccess(s.Secret, userID, in.Email, companyID)
	writeJSON(w, 201, map[string]any{"token": tok, "user_id": userID, "company_id": companyID, "role": "owner"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	var in struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		CompanyID string `json:"company_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	var userID, hash string
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT id,password_hash FROM users WHERE email=$1`, in.Email).Scan(&userID, &hash)
	if err != nil || !auth.CheckPassword(hash, in.Password) {
		writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	companyID := in.CompanyID
	var role string
	if companyID == "" {
		err = s.DB.Owner.QueryRow(r.Context(), `SELECT company_id, role FROM memberships WHERE user_id=$1 ORDER BY company_id LIMIT 1`, userID).Scan(&companyID, &role)
	} else {
		err = s.DB.Owner.QueryRow(r.Context(), `SELECT role FROM memberships WHERE user_id=$1 AND company_id=$2`, userID, companyID).Scan(&role)
	}
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "no membership"})
		return
	}
	tok, _ := auth.SignAccess(s.Secret, userID, in.Email, companyID)
	writeJSON(w, 200, map[string]any{"token": tok, "user_id": userID, "company_id": companyID, "role": role})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, c, role, email := userCtx(r)
	writeJSON(w, 200, map[string]string{"user_id": u, "company_id": c, "role": role, "email": email})
}

func (s *Server) handleCompanies(w http.ResponseWriter, r *http.Request) {
	u, _, _, _ := userCtx(r)
	if r.Method == "POST" {
		var in struct {
			Name string `json:"name"`
		}
		_ = readJSON(r, &in)
		if in.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		id := db.ID("cmp")
		_, _ = s.DB.Owner.Exec(r.Context(), `INSERT INTO companies(id,name) VALUES ($1,$2)`, id, in.Name)
		_, _ = s.DB.Owner.Exec(r.Context(), `INSERT INTO memberships(user_id,company_id,role) VALUES ($1,$2,'owner')`, u, id)
		writeJSON(w, 201, map[string]string{"id": id, "name": in.Name})
		return
	}
	rows, _ := s.DB.Owner.Query(r.Context(), `SELECT c.id,c.name,m.role FROM companies c JOIN memberships m ON m.company_id=c.id WHERE m.user_id=$1 ORDER BY c.created_at`, u)
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, name, role string
		_ = rows.Scan(&id, &name, &role)
		out = append(out, map[string]string{"id": id, "name": name, "role": role})
	}
	writeJSON(w, 200, map[string]any{"companies": out})
}

func (s *Server) handleMemberships(w http.ResponseWriter, r *http.Request) {
	// Owner adds an existing user to their company with a role.
	// This is how the beta creates accountant/employee test users.
	_, c, role, _ := userCtx(r)
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if role != "owner" {
		writeJSON(w, 403, map[string]string{"error": "only owner may manage members"})
		return
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	_ = readJSON(r, &in)
	if in.Role != "owner" && in.Role != "employee" && in.Role != "accountant" {
		writeJSON(w, 400, map[string]string{"error": "role must be owner, employee or accountant"})
		return
	}
	var userID string
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT id FROM users WHERE email=$1`, in.Email).Scan(&userID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "user not found; register them first"})
		return
	}
	_, err = s.DB.Owner.Exec(r.Context(), `INSERT INTO memberships(user_id,company_id,role) VALUES ($1,$2,$3) ON CONFLICT (user_id,company_id) DO UPDATE SET role=$3`, userID, c, in.Role)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]string{"user_id": userID, "company_id": c, "role": in.Role})
}

func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	b, _ := os.ReadFile("policy.json")
	if b == nil {
		b, _ = os.ReadFile("/gateway/policy.json")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// --- Apps ---

type App struct {
	ID, CompanyID, Name, Repo, URL, Status, ContainerID string
	Port                                                int
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	_ = u
	start := time.Now()
	if r.Method == "POST" {
		if !s.Pol.Allowed("apps", "write", role) {
			s.deny(w, r, start, "create", "apps", "", "", "role not allowed to create apps")
			return
		}
		var in struct {
			Name string `json:"name"`
			Repo string `json:"repo"`
		}
		_ = readJSON(r, &in)
		if in.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		id := db.ID("app")
		err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `INSERT INTO apps(id,company_id,name,repo,status) VALUES ($1,$2,$3,$4,'registered')`, id, c, in.Name, in.Repo)
			return err
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r, start, "create", "apps", id, c, true, "")
		writeJSON(w, 201, map[string]string{"id": id, "name": in.Name, "status": "registered"})
		return
	}
	if !s.Pol.Allowed("apps", "read", role) {
		s.deny(w, r, start, "read", "apps", "", "", "role not allowed to read apps")
		return
	}
	var out []map[string]any
	err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id,name,repo,url,status,container_id,port FROM apps ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []map[string]any{}
		for rows.Next() {
			var a App
			if err := rows.Scan(&a.ID, &a.Name, &a.Repo, &a.URL, &a.Status, &a.ContainerID, &a.Port); err != nil {
				return err
			}
			out = append(out, map[string]any{"id": a.ID, "name": a.Name, "repo": a.Repo, "url": a.URL, "status": a.Status, "container_id": a.ContainerID, "port": a.Port})
		}
		return rows.Err()
	})
	if err != nil {
		dbError(w, "list apps", err)
		return
	}
	s.audit(r, start, "read", "apps", "", c, true, "")
	writeJSON(w, 200, map[string]any{"apps": out})
}

// --- Apps: deploy / stop / delete / health (real containers, MVP v1) ---

// appRow is the owner-pool lookup for one app row (company + deploy state).
func (s *Server) appRow(r *http.Request, id string) (App, bool) {
	var a App
	a.ID = id
	err := s.DB.Owner.QueryRow(r.Context(),
		`SELECT company_id,repo,status,container_id,port FROM apps WHERE id=$1`, id).
		Scan(&a.CompanyID, &a.Repo, &a.Status, &a.ContainerID, &a.Port)
	return a, err == nil
}

func (s *Server) deployer() (deploy.Deployer, error) {
	if s.Deployer != nil {
		return s.Deployer, nil
	}
	return deploy.NewFromEnv()
}

// publicAppURL builds the URL reported for a deployed app from
// DEPLOY_PUBLIC_HOST (compose sets http://localhost); the host part
// is deployment configuration, never taken from the request.
func publicAppURL(port int) string {
	host := os.Getenv("DEPLOY_PUBLIC_HOST")
	if host == "" {
		host = "http://localhost"
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return strings.TrimSuffix(host, "/") + ":" + strconv.Itoa(port)
}

// appHealthy checks the public URL first, then the same port on
// localhost, then on DEPLOY_PROBE_HOST. The probe host exists because a
// gateway running inside a container cannot reach a host-published
// port via its own localhost (compose sets http://host.docker.internal);
// any 2xx counts, and no check means not healthy. The reported URL is
// always the public one.
func appHealthy(url string, port int) bool {
	if url != "" && deploy.CheckURL(url) {
		return true
	}
	if port <= 0 {
		return false
	}
	local := "http://localhost:" + strconv.Itoa(port)
	if local != url && deploy.CheckURL(local) {
		return true
	}
	if probe := os.Getenv("DEPLOY_PROBE_HOST"); probe != "" {
		if !strings.HasPrefix(probe, "http://") && !strings.HasPrefix(probe, "https://") {
			probe = "http://" + probe
		}
		probeURL := strings.TrimSuffix(probe, "/") + ":" + strconv.Itoa(port)
		if probeURL != url && probeURL != local && deploy.CheckURL(probeURL) {
			return true
		}
	}
	return false
}

func (s *Server) handleAppOne(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	switch {
	case r.Method == "POST" && strings.HasSuffix(rest, "/deploy"):
		s.handleAppDeploy(w, r, strings.TrimSuffix(rest, "/deploy"))
	case r.Method == "POST" && strings.HasSuffix(rest, "/stop"):
		s.handleAppStop(w, r, strings.TrimSuffix(rest, "/stop"))
	case r.Method == "GET" && strings.HasSuffix(rest, "/health"):
		s.handleAppHealth(w, r, strings.TrimSuffix(rest, "/health"))
	case r.Method == "DELETE" && rest != "" && !strings.Contains(rest, "/"):
		s.handleAppDelete(w, r, rest)
	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

// isTemplateRepo reports whether a repo may be deployed in MVP v1.
// Only the template is real: "" (no repo given), "sample-app", and
// "template:client-tracker" all deploy the sample-app image. Anything
// else — git:, http(s):, a path with "/", or any other name — is a
// 501: git deploy is NOT in MVP v1, documented as a next step, and is
// never faked by marking the app deployed.
func isTemplateRepo(repo string) bool {
	return repo == "" || repo == "sample-app" || repo == "template:client-tracker"
}

func (s *Server) handleAppDeploy(w http.ResponseWriter, r *http.Request, id string) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	if !s.Pol.Allowed("apps", "write", role) {
		s.deny(w, r, start, "deploy", "apps", id, "", "role not allowed to deploy apps")
		return
	}
	a, ok := s.appRow(r, id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app not found"})
		return
	}
	if a.CompanyID != c {
		s.deny(w, r, start, "deploy", "apps", id, a.CompanyID, "cross-tenant denied")
		return
	}
	if !isTemplateRepo(a.Repo) {
		// Status is deliberately left untouched: never deployed.
		writeJSON(w, 501, map[string]string{"error": "git deploy is NOT in MVP v1 (documented next step); only template deploy is available"})
		return
	}
	dep, err := s.deployer()
	if err != nil {
		_ = s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `UPDATE apps SET status='failed' WHERE id=$1`, id)
			return err
		})
		s.audit(r, start, "deploy", "apps", id, c, false, "docker-unavailable")
		writeJSON(w, 503, map[string]string{"error": "docker-unavailable", "detail": "docker runtime not available"})
		return
	}
	_ = s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE apps SET status='deploying' WHERE id=$1`, id)
		return err
	})
	// Redeploy: clear any previous container first (best effort).
	if a.ContainerID != "" {
		_ = dep.Stop(a.ContainerID)
		_ = dep.Remove(a.ContainerID)
	}
	image := os.Getenv("DEPLOY_IMAGE")
	if image == "" {
		image = "cloudbeta-sample-app"
	}
	containerID, port, _, err := dep.Start(id, c, image)
	if err != nil {
		_ = s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `UPDATE apps SET status='failed' WHERE id=$1`, id)
			return err
		})
		s.audit(r, start, "deploy", "apps", id, c, false, "deploy failed")
		if err != nil && isDockerUnavailable(err) {
			writeJSON(w, 503, map[string]string{"error": "docker-unavailable", "detail": "docker runtime not available"})
			return
		}
		writeJSON(w, 502, map[string]string{"error": "deploy failed: " + err.Error()})
		return
	}
	url := publicAppURL(port)
	healthy := false
	for i := 0; i < 15; i++ {
		if appHealthy(url, port) {
			healthy = true
			break
		}
		time.Sleep(time.Second)
	}
	if !healthy {
		_ = dep.Stop(containerID)
		_ = dep.Remove(containerID)
		_ = s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `UPDATE apps SET status='failed', container_id='', port=0 WHERE id=$1`, id)
			return err
		})
		s.audit(r, start, "deploy", "apps", id, c, false, "deployed container never became healthy")
		writeJSON(w, 502, map[string]string{"error": "deploy failed: container did not become healthy"})
		return
	}
	if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE apps SET status='running', url=$2, container_id=$3, port=$4 WHERE id=$1`, id, url, containerID, port)
		return err
	}); err != nil {
		// The container runs but the app row does not record it; do not
		// report a running app the control plane cannot track.
		_ = dep.Stop(containerID)
		_ = dep.Remove(containerID)
		dbError(w, "deploy record running", err)
		return
	}
	s.audit(r, start, "deploy", "apps", id, c, true, "real container deployed")
	writeJSON(w, 200, map[string]any{"id": id, "status": "running", "url": url, "port": port, "container_id": containerID})
}

func isDockerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	// The deploy client wraps deploy.ErrDockerUnavailable, whose
	// message is "docker-unavailable".
	return strings.Contains(err.Error(), "docker-unavailable")
}

func (s *Server) handleAppStop(w http.ResponseWriter, r *http.Request, id string) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	if !s.Pol.Allowed("apps", "write", role) {
		s.deny(w, r, start, "stop", "apps", id, "", "role not allowed to stop apps")
		return
	}
	a, ok := s.appRow(r, id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app not found"})
		return
	}
	if a.CompanyID != c {
		s.deny(w, r, start, "stop", "apps", id, a.CompanyID, "cross-tenant denied")
		return
	}
	if a.ContainerID != "" {
		if dep, err := s.deployer(); err == nil {
			_ = dep.Stop(a.ContainerID)
		}
	}
	if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE apps SET status='stopped' WHERE id=$1`, id)
		return err
	}); err != nil {
		dbError(w, "stop app", err)
		return
	}
	s.audit(r, start, "stop", "apps", id, c, true, "")
	writeJSON(w, 200, map[string]string{"id": id, "status": "stopped"})
}

func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request, id string) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	if !s.Pol.Allowed("apps", "delete", role) {
		s.deny(w, r, start, "delete", "apps", id, "", "only owner may delete apps")
		return
	}
	a, ok := s.appRow(r, id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app not found"})
		return
	}
	if a.CompanyID != c {
		s.deny(w, r, start, "delete", "apps", id, a.CompanyID, "cross-tenant denied")
		return
	}
	if a.ContainerID != "" {
		if dep, err := s.deployer(); err == nil {
			_ = dep.Stop(a.ContainerID)
			_ = dep.Remove(a.ContainerID)
		}
	}
	// Delete via the tenant transaction, so the RLS delete-owner-only
	// policy still applies (owner role + own company).
	var affected int64
	err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		cmd, err := tx.Exec(r.Context(), `DELETE FROM apps WHERE id=$1`, id)
		if err != nil {
			return err
		}
		affected = cmd.RowsAffected()
		return nil
	})
	if err != nil || affected == 0 {
		writeJSON(w, 404, map[string]string{"error": "app not found"})
		return
	}
	s.audit(r, start, "delete", "apps", id, c, true, "")
	writeJSON(w, 200, map[string]string{"deleted": id})
}

func (s *Server) handleAppHealth(w http.ResponseWriter, r *http.Request, id string) {
	_, c, role, _ := userCtx(r)
	start := time.Now()
	if !s.Pol.Allowed("apps", "read", role) {
		s.deny(w, r, start, "read", "apps", id, "", "role not allowed to read apps")
		return
	}
	a, ok := s.appRow(r, id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "app not found"})
		return
	}
	if a.CompanyID != c {
		s.deny(w, r, start, "read", "apps", id, a.CompanyID, "cross-tenant denied")
		return
	}
	containerStatus := "not-found"
	if a.ContainerID != "" {
		containerStatus = "unknown"
		if dep, err := s.deployer(); err == nil {
			if st, err := dep.InspectStatus(a.ContainerID); err == nil {
				containerStatus = st
			}
		}
	}
	healthy := false
	if a.Status == "running" {
		// Re-derive the URL from the stored port so a stale stored URL
		// cannot claim health; healthy only from a live HTTP check.
		healthy = appHealthy(publicAppURL(a.Port), a.Port)
	}
	// Report the stored URL when present, else the derived one.
	url := a.URL
	if url == "" && a.Port > 0 {
		url = publicAppURL(a.Port)
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": a.Status, "container_status": containerStatus, "url": url, "healthy": healthy})
}

// --- Clients (sample app tenant data) ---

func (s *Server) clientCompany(r *http.Request, id string) (string, bool) {
	var c string
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT company_id FROM clients WHERE id=$1`, id).Scan(&c)
	return c, err == nil
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	if r.Method == "POST" {
		if !s.Pol.Allowed("clients", "write", role) {
			s.deny(w, r, start, "create", "clients", "", "", "accountant is read-only")
			return
		}
		var in struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Notes string `json:"notes"`
		}
		_ = readJSON(r, &in)
		if in.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name required"})
			return
		}
		id := db.ID("cli")
		err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `INSERT INTO clients(id,company_id,name,email,notes) VALUES ($1,$2,$3,$4,$5)`, id, c, in.Name, in.Email, in.Notes)
			return err
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r, start, "create", "clients", id, c, true, "")
		writeJSON(w, 201, map[string]string{"id": id, "name": in.Name})
		return
	}
	if !s.Pol.Allowed("clients", "read", role) {
		s.deny(w, r, start, "read", "clients", "", "", "role not allowed to read clients")
		return
	}
	var out []map[string]string
	err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id,name,email,notes FROM clients ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []map[string]string{}
		for rows.Next() {
			var id, name, email, notes string
			if err := rows.Scan(&id, &name, &email, &notes); err != nil {
				return err
			}
			out = append(out, map[string]string{"id": id, "name": name, "email": email, "notes": notes})
		}
		return rows.Err()
	})
	if err != nil {
		dbError(w, "list clients", err)
		return
	}
	s.audit(r, start, "read", "clients", "", c, true, "")
	writeJSON(w, 200, map[string]any{"clients": out})
}

func (s *Server) handleClientOne(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/clients/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	targetCompany, exists := s.clientCompany(r, id)
	if !exists {
		writeJSON(w, 404, map[string]string{"error": "client not found"})
		return
	}
	if targetCompany != c {
		// Proof of the moat: cross-tenant access is denied AND logged,
		// not silently hidden as a 404.
		s.deny(w, r, start, strings.ToLower(r.Method), "clients", id, targetCompany, "cross-tenant denied")
		return
	}
	if r.Method == "DELETE" {
		if !s.Pol.Allowed("clients", "delete", role) {
			s.deny(w, r, start, "delete", "clients", id, c, "only owner may delete clients")
			return
		}
		if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `DELETE FROM clients WHERE id=$1`, id)
			return err
		}); err != nil {
			dbError(w, "delete client", err)
			return
		}
		s.audit(r, start, "delete", "clients", id, c, true, "")
		writeJSON(w, 200, map[string]string{"deleted": id})
		return
	}
	if r.Method == "PUT" {
		if !s.Pol.Allowed("clients", "write", role) {
			s.deny(w, r, start, "update", "clients", id, c, "role not allowed to update clients")
			return
		}
		var in struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Notes string `json:"notes"`
		}
		_ = readJSON(r, &in)
		if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `UPDATE clients SET name=$2,email=$3,notes=$4 WHERE id=$1`, id, in.Name, in.Email, in.Notes)
			return err
		}); err != nil {
			dbError(w, "update client", err)
			return
		}
		s.audit(r, start, "update", "clients", id, c, true, "")
		writeJSON(w, 200, map[string]string{"id": id})
		return
	}
	if !s.Pol.Allowed("clients", "read", role) {
		s.deny(w, r, start, "read", "clients", id, c, "role not allowed to read clients")
		return
	}
	var name, email, notes string
	err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT name,email,notes FROM clients WHERE id=$1`, id).Scan(&name, &email, &notes)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "client not found"})
		return
	}
	if err != nil {
		dbError(w, "read client", err)
		return
	}
	s.audit(r, start, "read", "clients", id, c, true, "")
	writeJSON(w, 200, map[string]string{"id": id, "name": name, "email": email, "notes": notes})
}

// --- Audit ---

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	if !s.Pol.Allowed("audit_logs", "read", role) {
		writeJSON(w, 403, map[string]string{"error": "not allowed"})
		return
	}
	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var out []map[string]any
	err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT action,table_name,target_id,allowed,reason,latency_ms,created_at,actor_role FROM audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []map[string]any{}
		for rows.Next() {
			var action, table, target, reason, actorRole string
			var allowed bool
			var latency int64
			var ts time.Time
			if err := rows.Scan(&action, &table, &target, &allowed, &reason, &latency, &ts, &actorRole); err != nil {
				return err
			}
			out = append(out, map[string]any{"action": action, "table": table, "target_id": target, "allowed": allowed, "reason": reason, "latency_ms": latency, "created_at": ts, "actor_role": actorRole})
		}
		return rows.Err()
	})
	if err != nil {
		dbError(w, "list audit", err)
		return
	}
	writeJSON(w, 200, map[string]any{"audit": out})
}

func (s *Server) handleAuditSummary(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	if !s.Pol.Allowed("audit_logs", "read", role) {
		writeJSON(w, 403, map[string]string{"error": "not allowed"})
		return
	}
	var total, allowed, denied int64
	var avg float64
	if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT count(*), coalesce(sum(CASE WHEN allowed THEN 1 ELSE 0 END),0), coalesce(sum(CASE WHEN NOT allowed THEN 1 ELSE 0 END),0), coalesce(avg(latency_ms),0) FROM audit_logs`).Scan(&total, &allowed, &denied, &avg)
	}); err != nil {
		dbError(w, "audit summary totals", err)
		return
	}
	// Per-day counts, last 7 days, from real audit rows only.
	days := []map[string]any{}
	if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT created_at::date AS d, count(*) AS total, coalesce(sum(CASE WHEN allowed THEN 1 ELSE 0 END),0) AS a, coalesce(sum(CASE WHEN NOT allowed THEN 1 ELSE 0 END),0) AS den FROM audit_logs WHERE created_at >= now() - interval '7 days' GROUP BY d ORDER BY d`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d time.Time
			var t, a, den int64
			if err := rows.Scan(&d, &t, &a, &den); err != nil {
				return err
			}
			days = append(days, map[string]any{"date": d.Format("2006-01-02"), "total": t, "allowed": a, "denied": den})
		}
		return rows.Err()
	}); err != nil {
		dbError(w, "audit summary per day", err)
		return
	}
	writeJSON(w, 200, map[string]any{"total": total, "allowed": allowed, "denied": denied, "avg_latency_ms": avg, "per_day": days})
}

// --- Shares ---

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	if r.Method == "GET" {
		// List shares for the caller's company only, via the tenant
		// transaction. Token hashes and raw tokens are NEVER returned.
		if !s.Pol.Allowed("shares", "read", role) {
			s.deny(w, r, start, "read", "shares", "", "", "role not allowed to read shares")
			return
		}
		var out []map[string]any
		if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
			rows, err := tx.Query(r.Context(), `SELECT id,table_name,row_id,expires_at,revoked,created_at FROM shares ORDER BY created_at DESC`)
			if err != nil {
				return err
			}
			defer rows.Close()
			out = []map[string]any{}
			for rows.Next() {
				var id, table, rowID string
				var expires time.Time
				var revoked bool
				var created time.Time
				if err := rows.Scan(&id, &table, &rowID, &expires, &revoked, &created); err != nil {
					return err
				}
				out = append(out, map[string]any{"id": id, "table_name": table, "row_id": rowID, "expires_at": expires, "revoked": revoked, "created_at": created})
			}
			return rows.Err()
		}); err != nil {
			dbError(w, "list shares", err)
			return
		}
		s.audit(r, start, "read", "shares", "", c, true, "")
		writeJSON(w, 200, map[string]any{"shares": out})
		return
	}
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.Pol.Allowed("shares", "write", role) {
		s.deny(w, r, start, "create", "shares", "", "", "role not allowed to create shares")
		return
	}
	var in struct {
		RowID     string `json:"row_id"`
		ExpiresIn string `json:"expires_in"` // e.g. 1h, 24h
	}
	_ = readJSON(r, &in)
	if in.RowID == "" {
		writeJSON(w, 400, map[string]string{"error": "row_id required"})
		return
	}
	targetCompany, exists := s.clientCompany(r, in.RowID)
	if !exists || targetCompany != c {
		s.deny(w, r, start, "create", "shares", in.RowID, targetCompany, "cannot share a client from another company")
		return
	}
	dur := 24 * time.Hour
	if in.ExpiresIn != "" {
		if d, err := time.ParseDuration(in.ExpiresIn); err == nil {
			dur = d
		}
	}
	token := auth.RandomToken()
	id := db.ID("shr")
	_, err := s.DB.Owner.Exec(r.Context(), `INSERT INTO shares(id,company_id,table_name,row_id,token_hash,expires_at,created_by) VALUES ($1,$2,'clients',$3,$4,$5,$6)`, id, c, in.RowID, auth.HashToken(token), time.Now().Add(dur), u)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r, start, "create", "shares", id, c, true, "")
	writeJSON(w, 201, map[string]string{"id": id, "token": token, "url": "/share/" + token, "expires_at": time.Now().Add(dur).Format(time.RFC3339), "note": "token shown once; only the hash is stored"})
}

func (s *Server) handleSharePublic(w http.ResponseWriter, r *http.Request) {
	// GET /share/{token}: single row only, no listing, expiry + revoke checked.
	// This is the ONLY owner-pool tenant read, scoped to one token hash.
	token := strings.TrimPrefix(r.URL.Path, "/share/")
	if token == "" || strings.Contains(token, "/") {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	var companyID, rowID string
	var expires time.Time
	var revoked bool
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT company_id,row_id,expires_at,revoked FROM shares WHERE token_hash=$1`, auth.HashToken(token)).Scan(&companyID, &rowID, &expires, &revoked)
	if err != nil || revoked || time.Now().After(expires) {
		writeJSON(w, 403, map[string]string{"error": "share expired, revoked, or invalid"})
		return
	}
	var name, email, notes string
	err = s.DB.Owner.QueryRow(r.Context(), `SELECT name,email,notes FROM clients WHERE id=$1 AND company_id=$2`, rowID, companyID).Scan(&name, &email, &notes)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "shared row not found"})
		return
	}
	s.DB.Audit(r.Context(), companyID, "share", "share", "", "read", "clients", rowID, companyID, true, "share-token read, single row only", 0)
	writeJSON(w, 200, map[string]string{"id": rowID, "name": name, "email": email, "notes": notes, "via": "share_token"})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	start := time.Now()
	if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/revoke") {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/shares/"), "/revoke")
	if !s.Pol.Allowed("shares", "delete", role) {
		s.deny(w, r, start, "revoke", "shares", id, c, "only owner may revoke shares")
		return
	}
	if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `UPDATE shares SET revoked=true WHERE id=$1`, id)
		return err
	}); err != nil {
		dbError(w, "revoke share", err)
		return
	}
	s.audit(r, start, "revoke", "shares", id, c, true, "")
	writeJSON(w, 200, map[string]string{"revoked": id})
}

// --- Self-healing preview (beta: propose, never auto-apply) ---

var errHealNotFound = fmt.Errorf("heal proposal not found or already decided")

var expectedClientColumns = []string{"id", "company_id", "name", "email", "notes", "created_at"}

func missingColumns(actual []string) []string {
	have := map[string]bool{}
	for _, c := range actual {
		have[c] = true
	}
	var missing []string
	for _, e := range expectedClientColumns {
		if !have[e] {
			missing = append(missing, e)
		}
	}
	return missing
}

func (s *Server) handleHealDetect(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	// Fix 2: heal proposals are control-plane objects written via the
	// owner pool, so RLS does not protect them — enforce policy in Go.
	// heal_proposals write is owner-only in policy.json.
	if !s.Pol.Allowed("heal_proposals", "write", role) {
		s.deny(w, r, time.Now(), "create", "heal_proposals", "", c, "only owner may create heal proposals")
		return
	}
	rows, _ := s.DB.Owner.Query(r.Context(), `SELECT column_name FROM information_schema.columns WHERE table_name='clients'`)
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var col string
		_ = rows.Scan(&col)
		actual = append(actual, col)
	}
	missing := missingColumns(actual)
	if len(missing) == 0 {
		writeJSON(w, 200, map[string]any{"status": "healthy", "missing": []string{}})
		return
	}
	id := db.ID("heal")
	diff := "Proposed fix (NOT applied): restore missing column(s) " + strings.Join(missing, ", ") + " in clients, then re-run the sample app query selecting name/email/notes. Approval is required before any change."
	_, _ = s.DB.Owner.Exec(r.Context(), `INSERT INTO heal_proposals(id,company_id,title,diff,status,applied,fix_kind,fix_column) VALUES ($1,$2,$3,$4,'pending',false,'add_missing_columns',$5)`, id, c, "Missing client column(s): "+strings.Join(missing, ", "), diff, strings.Join(missing, ","))
	_ = u
	_ = role
	writeJSON(w, 200, map[string]any{"status": "proposal_created", "id": id, "missing": missing, "diff": diff})
}

func (s *Server) handleHealSimulate(w http.ResponseWriter, r *http.Request) {
	// Beta demo of the preview flow: creates a proposal for a field the
	// operator names, WITHOUT changing any table. Labelled simulated.
	_, c, role, _ := userCtx(r)
	if r.Method != "POST" {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	// Fix 2: same owner-only policy check as handleHealDetect.
	if !s.Pol.Allowed("heal_proposals", "write", role) {
		s.deny(w, r, time.Now(), "create", "heal_proposals", "", c, "only owner may create heal proposals")
		return
	}
	var in struct {
		Field string `json:"field"`
	}
	_ = readJSON(r, &in)
	if in.Field == "" {
		in.Field = "email"
	}
	id := db.ID("heal")
	diff := "Proposed fix (SIMULATED break, NOT applied): sample app renamed field '" + in.Field + "'; update the app query to use the new field name, or restore '" + in.Field + "'. Approval is required before any change."
	_, _ = s.DB.Owner.Exec(r.Context(), `INSERT INTO heal_proposals(id,company_id,title,diff,status,applied,fix_kind,fix_column) VALUES ($1,$2,$3,$4,'pending',false,'simulated',$5)`, id, c, "Simulated broken field: "+in.Field, diff, in.Field)
	writeJSON(w, 201, map[string]any{"id": id, "diff": diff, "status": "pending", "applied": false})
}

func (s *Server) handleHealProposals(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	if !s.Pol.Allowed("heal_proposals", "read", role) {
		writeJSON(w, 403, map[string]string{"error": "not allowed"})
		return
	}
	var out []map[string]any
	if err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id,title,diff,status,applied,fix_kind,fix_column FROM heal_proposals ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []map[string]any{}
		for rows.Next() {
			var id, title, diff, status, fixKind, fixColumn string
			var applied bool
			if err := rows.Scan(&id, &title, &diff, &status, &applied, &fixKind, &fixColumn); err != nil {
				return err
			}
			out = append(out, map[string]any{"id": id, "title": title, "diff": diff, "status": status, "applied": applied, "fix_kind": fixKind, "fix_column": fixColumn})
		}
		return rows.Err()
	}); err != nil {
		dbError(w, "list heal proposals", err)
		return
	}
	writeJSON(w, 200, map[string]any{"proposals": out})
}

func (s *Server) handleHealApprove(w http.ResponseWriter, r *http.Request) {
	u, c, role, _ := userCtx(r)
	if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/approve") {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/heal/"), "/approve")
	if !s.Pol.Allowed("heal_proposals", "write", role) {
		writeJSON(w, 403, map[string]string{"error": "only owner may approve heal proposals"})
		return
	}
	// Approval ONLY: applied stays false in beta. The operator applies
	// the diff manually after approval. Nothing is auto-applied here.
	// Fix 2: only approve a pending proposal in the caller's company.
	// RowsAffected == 0 means not found, already decided, or another
	// company — return 404, never a fake 200 "approved".
	var affected int64
	start := time.Now()
	err := s.tenant(r.Context(), c, u, role, func(tx pgx.Tx) error {
		cmd, err := tx.Exec(r.Context(), `UPDATE heal_proposals SET status='approved' WHERE id=$1 AND status='pending'`, id)
		if err != nil {
			return err
		}
		affected = cmd.RowsAffected()
		if affected == 0 {
			return errHealNotFound
		}
		return nil
	})
	if errors.Is(err, errHealNotFound) {
		// Audit the failed approval attempt, then 404 (not 200 approved).
		s.audit(r, start, "approve", "heal_proposals", id, c, false, "proposal not found or already decided")
		writeJSON(w, 404, map[string]string{"error": "proposal not found or already decided"})
		return
	}
	if err != nil {
		dbError(w, "approve heal proposal", err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": "approved", "applied": false, "note": "approved, NOT applied in beta — apply the diff manually"})
}

func (s *Server) handleHealOne(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/heal/")
	switch {
	case r.Method == "POST" && strings.HasSuffix(rest, "/approve"):
		s.handleHealApprove(w, r)
	case r.Method == "POST" && strings.HasSuffix(rest, "/apply"):
		s.handleHealApply(w, r, strings.TrimSuffix(rest, "/apply"))
	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

// healColumnDefinitions is the ONLY source of column names and types
// the apply path may use. It mirrors the clients schema exactly.
// "id" is deliberately absent: it is a primary key and is never
// applyable, so a proposal naming it can never pass this whitelist.
var healColumnDefinitions = map[string]string{
	"email":      "TEXT NOT NULL DEFAULT ''",
	"notes":      "TEXT NOT NULL DEFAULT ''",
	"name":       "TEXT NOT NULL DEFAULT ''",
	"company_id": "TEXT NOT NULL DEFAULT ''",
	"created_at": "TIMESTAMPTZ NOT NULL DEFAULT now()",
}

// applyableColumns validates a proposal for apply. Only
// fix_kind='add_missing_columns' with every column in the whitelist
// is applyable; simulated proposals, unknown kinds and unknown
// columns (including "id") are preview-only. The returned list is
// deduplicated, in proposal order.
func applyableColumns(fixKind, fixColumn string) ([]string, bool) {
	if fixKind != "add_missing_columns" {
		return nil, false
	}
	parts := strings.Split(fixColumn, ",")
	var out []string
	seen := map[string]bool{}
	for _, p := range parts {
		col := strings.TrimSpace(p)
		if col == "" {
			return nil, false
		}
		if _, ok := healColumnDefinitions[col]; !ok {
			return nil, false
		}
		if !seen[col] {
			seen[col] = true
			out = append(out, col)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func clientColumns(s *Server, ctx context.Context) []string {
	rows, _ := s.DB.Owner.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='clients'`)
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var col string
		_ = rows.Scan(&col)
		cols = append(cols, col)
	}
	return cols
}

// handleHealApply applies an APPROVED, whitelisted missing-column fix
// for real, via the owner pool. Anything else changes nothing:
// pending -> 409 "approve first", already applied/decided -> 409,
// simulated/unknown kind/unknown column -> 400 "preview only, not
// safely applyable" with the proposal left approved and unapplied.
func (s *Server) handleHealApply(w http.ResponseWriter, r *http.Request, id string) {
	_, c, role, _ := userCtx(r)
	start := time.Now()
	if !s.Pol.Allowed("heal_proposals", "write", role) {
		s.deny(w, r, start, "apply", "heal_proposals", id, c, "only owner may apply heal proposals")
		return
	}
	var proposalCompany, status, fixKind, fixColumn string
	var applied bool
	err := s.DB.Owner.QueryRow(r.Context(),
		`SELECT company_id,status,fix_kind,fix_column,applied FROM heal_proposals WHERE id=$1`, id).
		Scan(&proposalCompany, &status, &fixKind, &fixColumn, &applied)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "proposal not found"})
		return
	}
	if proposalCompany != c {
		s.deny(w, r, start, "apply", "heal_proposals", id, proposalCompany, "cross-tenant denied")
		return
	}
	if status == "pending" {
		writeJSON(w, 409, map[string]string{"error": "approve first"})
		return
	}
	if applied || status == "applied" {
		writeJSON(w, 409, map[string]string{"error": "proposal already applied"})
		return
	}
	if status != "approved" {
		writeJSON(w, 409, map[string]string{"error": "proposal already decided"})
		return
	}
	cols, ok := applyableColumns(fixKind, fixColumn)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "preview only, not safely applyable"})
		return
	}
	before := clientColumns(s, r.Context())
	have := map[string]bool{}
	for _, col := range before {
		have[col] = true
	}
	var missingBefore []string
	for _, col := range cols {
		if !have[col] {
			missingBefore = append(missingBefore, col)
		}
	}
	// DDL: column name and definition come ONLY from the whitelist
	// map validated above, never from raw user input.
	for _, col := range cols {
		def := healColumnDefinitions[col]
		if _, err := s.DB.Owner.Exec(r.Context(),
			`ALTER TABLE clients ADD COLUMN IF NOT EXISTS `+col+` `+def); err != nil {
			writeJSON(w, 500, map[string]string{"error": "apply failed: " + err.Error()})
			return
		}
	}
	after := clientColumns(s, r.Context())
	afterSet := map[string]bool{}
	for _, col := range after {
		afterSet[col] = true
	}
	for _, col := range cols {
		if !afterSet[col] {
			writeJSON(w, 500, map[string]string{"error": "apply failed: column " + col + " not present after apply"})
			return
		}
	}
	_, _ = s.DB.Owner.Exec(r.Context(),
		`UPDATE heal_proposals SET status='applied', applied=true WHERE id=$1`, id)
	s.audit(r, start, "apply", "heal_proposals", id, c, true,
		"applied columns "+strings.Join(cols, ",")+
			"; missing before: "+strings.Join(missingBefore, ",")+
			"; columns after: "+strings.Join(after, ","))
	writeJSON(w, 200, map[string]any{"id": id, "status": "applied", "applied": true, "applied_columns": cols, "columns_after": after})
}

var _ = pgxpool.Pool{}
