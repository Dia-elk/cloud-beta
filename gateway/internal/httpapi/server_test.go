package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-at-least-thirty-two-characters"

func testServer() *Server {
	return &Server{Cfg: Config{
		JWTSecret:        testSecret,
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:3001"},
		DeployPublicHost: "http://localhost",
	}}
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) apperr.Body {
	t.Helper()
	var b apperr.Body
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, w.Body.String())
	}
	if b.Message["en"] == "" || b.Message["fr"] == "" || b.Message["ar"] == "" {
		t.Fatalf("error body must carry en/fr/ar messages: %s", w.Body.String())
	}
	return b
}

func TestUnknownRouteIsJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	testServer().Handler().ServeHTTP(w, httptest.NewRequest("GET", "/nope", nil))
	if w.Code != 404 || decodeErr(t, w).Code != apperr.GatewayNotFound {
		t.Fatalf("want 404 GATEWAY_002, got %d %s", w.Code, w.Body.String())
	}
}

func TestCORSAllowlist(t *testing.T) {
	h := testServer().Handler()
	cases := []struct {
		origin string
		ok     bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost:3001", true},
		{"http://localhost:3150", true},  // deployed app in range
		{"http://localhost:3300", false}, // outside deploy range
		{"http://evil.example", false},
		{"https://localhost:3100", false}, // scheme must match public host
	}
	for _, c := range cases {
		req := httptest.NewRequest("OPTIONS", "/api/v1/apps", nil)
		req.Header.Set("Origin", c.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		got := w.Header().Get("Access-Control-Allow-Origin")
		if c.ok && (w.Code != 204 || got != c.origin || w.Header().Get("Access-Control-Allow-Credentials") != "true") {
			t.Errorf("%s: want allowed with credentials, got %d %q", c.origin, w.Code, got)
		}
		if !c.ok && (w.Code != 403 || got != "") {
			t.Errorf("%s: want 403 and no allow-origin, got %d %q", c.origin, w.Code, got)
		}
	}
}

func TestAuthFailureCodes(t *testing.T) {
	h := testServer().Handler()
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: "u",
		RegisteredClaims: jwt.RegisteredClaims{
			ID: "j", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}).SignedString([]byte(testSecret))
	cases := []struct {
		name   string
		set    func(*http.Request)
		status int
		code   apperr.Code
	}{
		{"missing", func(*http.Request) {}, 401, apperr.AuthMissingToken},
		{"garbage bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, 401, apperr.AuthInvalidToken},
		{"garbage cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: accessCookie, Value: "nope"}) }, 401, apperr.AuthInvalidToken},
		{"expired", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+expired) }, 401, apperr.AuthTokenExpired},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/api/v1/apps", nil)
		c.set(req)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != c.status || decodeErr(t, w).Code != c.code {
			t.Errorf("%s: want %d %s, got %d %s", c.name, c.status, c.code, w.Code, w.Body.String())
		}
	}
}

func TestDBErrorIs500AndHidesCause(t *testing.T) {
	w := httptest.NewRecorder()
	dbError(w, "list clients", errors.New(`ERROR: column "notes" does not exist (SQLSTATE 42703)`))
	if w.Code != 500 {
		t.Fatalf("db failure must be 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "notes") || strings.Contains(w.Body.String(), "SQLSTATE") {
		t.Fatalf("raw db error leaked to client: %s", w.Body.String())
	}
	if decodeErr(t, w).Code != apperr.GatewayDatabase {
		t.Fatalf("want GATEWAY_001, got %s", w.Body.String())
	}
}

func TestReadJSONRejectsMalformedBody(t *testing.T) {
	w := httptest.NewRecorder()
	var v struct{ Name string }
	if readJSON(w, httptest.NewRequest("POST", "/", strings.NewReader("{nope")), &v) {
		t.Fatal("malformed JSON must be rejected")
	}
	if w.Code != 400 || decodeErr(t, w).Code != apperr.GatewayBadBody {
		t.Fatalf("want 400 GATEWAY_004, got %d %s", w.Code, w.Body.String())
	}
	if !readJSON(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader("")), &v) {
		t.Fatal("an empty body must be accepted as the zero value")
	}
}

func TestSessionCookiesAreHardened(t *testing.T) {
	s := testServer()
	s.Cfg.CookieSecure = true
	w := httptest.NewRecorder()
	s.setCookie(w, refreshCookie, "v", refreshPath, time.Hour)
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != refreshPath {
		t.Fatalf("refresh cookie must be HttpOnly, Secure, SameSite=Strict and path-scoped: %+v", c)
	}
}
