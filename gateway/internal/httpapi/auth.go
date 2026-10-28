package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/auth"
	"cloudbeta/gateway/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	accessCookie  = "access_token"
	refreshCookie = "refresh_token"
	refreshPath   = "/api/v1/auth"
)

// dummyHash keeps login timing the same whether or not the email exists.
var dummyHash, _ = auth.HashPassword("timing-equaliser-not-a-password")

// --- Cookies ---

func (s *Server) setCookie(w http.ResponseWriter, name, value, path string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: path,
		MaxAge: int(ttl.Seconds()), Expires: time.Now().Add(ttl),
		HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearCookies(w http.ResponseWriter) {
	for _, c := range []struct{ name, path string }{{accessCookie, "/"}, {refreshCookie, refreshPath}} {
		http.SetCookie(w, &http.Cookie{
			Name: c.name, Value: "", Path: c.path, MaxAge: -1, Expires: time.Unix(0, 0),
			HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteStrictMode,
		})
	}
}

// accessToken reads the session cookie first (browsers), then a Bearer
// header (MCP, scripts, API clients).
func accessToken(r *http.Request) string {
	if c, err := r.Cookie(accessCookie); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

// issueSession signs an access token, stores a new refresh token in the
// given family (a fresh family when empty), and sets both cookies. The
// access token is also returned for non-browser API clients.
func (s *Server) issueSession(ctx context.Context, w http.ResponseWriter, userID, email, companyID, familyID string) (string, error) {
	access, err := auth.SignAccess(s.Cfg.JWTSecret, userID, email, companyID)
	if err != nil {
		return "", err
	}
	if familyID == "" {
		familyID = db.NewID()
	}
	refresh := auth.RandomToken()
	if _, err := s.DB.Owner.Exec(ctx,
		`INSERT INTO refresh_tokens(id,family_id,user_id,company_id,token_hash,expires_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		db.NewID(), familyID, userID, companyID, auth.HashToken(refresh), time.Now().Add(auth.RefreshTTL)); err != nil {
		return "", err
	}
	s.setCookie(w, accessCookie, access, "/", auth.AccessTTL)
	s.setCookie(w, refreshCookie, refresh, refreshPath, auth.RefreshTTL)
	return access, nil
}

// --- Middleware ---

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := accessToken(r)
		if tok == "" {
			fail(w, apperr.AuthMissingToken)
			return
		}
		claims, err := auth.VerifyAccess(s.Cfg.JWTSecret, tok)
		if errors.Is(err, auth.ErrTokenExpired) {
			fail(w, apperr.AuthTokenExpired)
			return
		}
		if err != nil {
			fail(w, apperr.AuthInvalidToken)
			return
		}
		var revoked bool
		if err := s.DB.Owner.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM revoked_access_tokens WHERE jti=$1)`, claims.ID).Scan(&revoked); err != nil {
			dbError(w, "check revoked token", err)
			return
		}
		if revoked {
			fail(w, apperr.AuthInvalidToken)
			return
		}
		companyID := claims.CompanyID
		if h := r.Header.Get("X-Company-ID"); h != "" {
			companyID = h
		}
		var role string
		err = s.DB.Owner.QueryRow(r.Context(),
			`SELECT role FROM memberships WHERE user_id=$1 AND company_id=$2`, claims.UserID, companyID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, apperr.AuthNoMembership)
			return
		}
		if err != nil {
			dbError(w, "resolve membership", err)
			return
		}
		sess := Session{UserID: claims.UserID, CompanyID: companyID, Role: role, Email: claims.Email, TokenID: claims.ID}
		if claims.ExpiresAt != nil {
			sess.TokenExp = claims.ExpiresAt.Time
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

// --- Register / login / refresh / logout ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		CompanyName string `json:"company_name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Email, in.CompanyName = strings.TrimSpace(in.Email), strings.TrimSpace(in.CompanyName)
	if _, err := mail.ParseAddress(in.Email); err != nil || in.CompanyName == "" || in.Password == "" {
		fail(w, apperr.AuthFieldsRequired)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if errors.Is(err, auth.ErrPasswordShort) {
		fail(w, apperr.AuthPasswordShort)
		return
	}
	if err != nil {
		fail(w, apperr.GatewayInternal)
		return
	}
	userID, companyID := db.NewID(), db.NewID()
	err = pgx.BeginFunc(r.Context(), s.DB.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `INSERT INTO users(id,email,password_hash) VALUES ($1,$2,$3)`, userID, in.Email, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO companies(id,name) VALUES ($1,$2)`, companyID, in.CompanyName); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO memberships(user_id,company_id,role) VALUES ($1,$2,'owner')`, userID, companyID)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail(w, apperr.AuthEmailExists)
		return
	}
	if err != nil {
		dbError(w, "register", err)
		return
	}
	tok, err := s.issueSession(r.Context(), w, userID, in.Email, companyID, "")
	if err != nil {
		dbError(w, "issue session", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": tok, "user_id": userID, "email": in.Email,
		"company_id": companyID, "company_name": in.CompanyName, "role": "owner",
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		CompanyID string `json:"company_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Email = strings.TrimSpace(in.Email)
	var userID, hash string
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT id,password_hash FROM users WHERE email=$1`, in.Email).Scan(&userID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, "login lookup", err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		auth.CheckPassword(dummyHash, in.Password)
		fail(w, apperr.AuthInvalidCreds)
		return
	}
	if !auth.CheckPassword(hash, in.Password) {
		fail(w, apperr.AuthInvalidCreds)
		return
	}
	companyID, companyName, role := in.CompanyID, "", ""
	q := `SELECT c.id, c.name, m.role FROM memberships m JOIN companies c ON c.id=m.company_id
	      WHERE m.user_id=$1 AND ($2='' OR c.id=$2) ORDER BY c.created_at LIMIT 1`
	err = s.DB.Owner.QueryRow(r.Context(), q, userID, in.CompanyID).Scan(&companyID, &companyName, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.AuthNoMembership)
		return
	}
	if err != nil {
		dbError(w, "login membership", err)
		return
	}
	tok, err := s.issueSession(r.Context(), w, userID, in.Email, companyID, "")
	if err != nil {
		dbError(w, "issue session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok, "user_id": userID, "email": in.Email,
		"company_id": companyID, "company_name": companyName, "role": role,
	})
}

var errRefreshInvalid = errors.New("refresh invalid")

// handleRefresh rotates the refresh token: the presented token is
// revoked and a new one issued in the same family. Presenting an
// already-rotated token revokes the whole family, since it means the
// token was copied.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	presented := ""
	if c, err := r.Cookie(refreshCookie); err == nil {
		presented = c.Value
	}
	if presented == "" {
		var in struct {
			RefreshToken string `json:"refresh_token"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		presented = in.RefreshToken
	}
	if presented == "" {
		s.clearCookies(w)
		fail(w, apperr.AuthRefreshInvalid)
		return
	}
	var userID, email, companyID, familyID string
	err := pgx.BeginFunc(r.Context(), s.DB.Owner, func(tx pgx.Tx) error {
		var id string
		var revoked bool
		var expires time.Time
		err := tx.QueryRow(r.Context(),
			`SELECT rt.id, rt.family_id, rt.user_id, rt.company_id, rt.revoked, rt.expires_at, u.email
			 FROM refresh_tokens rt JOIN users u ON u.id=rt.user_id WHERE rt.token_hash=$1 FOR UPDATE OF rt`,
			auth.HashToken(presented)).Scan(&id, &familyID, &userID, &companyID, &revoked, &expires, &email)
		if errors.Is(err, pgx.ErrNoRows) {
			return errRefreshInvalid
		}
		if err != nil {
			return err
		}
		if revoked {
			// Reuse of a rotated token: end every session in this family.
			if _, err := tx.Exec(r.Context(), `UPDATE refresh_tokens SET revoked=true WHERE family_id=$1`, familyID); err != nil {
				return err
			}
			familyID = ""
			return nil
		}
		if time.Now().After(expires) {
			return errRefreshInvalid
		}
		var member bool
		if err := tx.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND company_id=$2)`, userID, companyID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return errRefreshInvalid
		}
		_, err = tx.Exec(r.Context(), `UPDATE refresh_tokens SET revoked=true WHERE id=$1`, id)
		return err
	})
	if errors.Is(err, errRefreshInvalid) || (err == nil && familyID == "") {
		s.clearCookies(w)
		fail(w, apperr.AuthRefreshInvalid)
		return
	}
	if err != nil {
		dbError(w, "refresh", err)
		return
	}
	tok, err := s.issueSession(r.Context(), w, userID, email, companyID, familyID)
	if err != nil {
		dbError(w, "issue session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "company_id": companyID})
}

// handleLogout revokes the refresh family and the current access token,
// then clears cookies. It succeeds even for an already-expired session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(refreshCookie); err == nil && c.Value != "" {
		if _, err := s.DB.Owner.Exec(r.Context(),
			`UPDATE refresh_tokens SET revoked=true WHERE family_id=(SELECT family_id FROM refresh_tokens WHERE token_hash=$1)`,
			auth.HashToken(c.Value)); err != nil {
			dbError(w, "logout refresh", err)
			return
		}
	}
	if claims, err := auth.VerifyAccess(s.Cfg.JWTSecret, accessToken(r)); err == nil && claims.ExpiresAt != nil {
		if _, err := s.DB.Owner.Exec(r.Context(),
			`INSERT INTO revoked_access_tokens(jti,expires_at) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			claims.ID, claims.ExpiresAt.Time); err != nil {
			dbError(w, "logout access", err)
			return
		}
		_, _ = s.DB.Owner.Exec(r.Context(), `DELETE FROM revoked_access_tokens WHERE expires_at < now()`)
	}
	s.clearCookies(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed_out"})
}

// --- Me / workspaces / members / policy ---

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := session(r)
	var name string
	if err := s.DB.Owner.QueryRow(r.Context(), `SELECT name FROM companies WHERE id=$1`, sess.CompanyID).Scan(&name); err != nil {
		dbError(w, "me", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": sess.UserID, "email": sess.Email, "company_id": sess.CompanyID,
		"company_name": name, "role": sess.Role,
	})
}

func (s *Server) handleSwitchCompany(w http.ResponseWriter, r *http.Request) {
	sess := session(r)
	var in struct {
		CompanyID string `json:"company_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	var role string
	err := s.DB.Owner.QueryRow(r.Context(),
		`SELECT role FROM memberships WHERE user_id=$1 AND company_id=$2`, sess.UserID, in.CompanyID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.AuthNoMembership)
		return
	}
	if err != nil {
		dbError(w, "switch company", err)
		return
	}
	if c, err := r.Cookie(refreshCookie); err == nil && c.Value != "" {
		_, _ = s.DB.Owner.Exec(r.Context(), `UPDATE refresh_tokens SET revoked=true WHERE token_hash=$1`, auth.HashToken(c.Value))
	}
	tok, err := s.issueSession(r.Context(), w, sess.UserID, sess.Email, in.CompanyID, "")
	if err != nil {
		dbError(w, "issue session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "company_id": in.CompanyID, "role": role})
}

func (s *Server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Owner.Query(r.Context(),
		`SELECT c.id, c.name, m.role FROM companies c JOIN memberships m ON m.company_id=c.id WHERE m.user_id=$1 ORDER BY c.created_at`,
		session(r).UserID)
	if err != nil {
		dbError(w, "list companies", err)
		return
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (map[string]string, error) {
		var id, name, role string
		err := row.Scan(&id, &name, &role)
		return map[string]string{"id": id, "name": name, "role": role}, err
	})
	if err != nil {
		dbError(w, "list companies", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"companies": out})
}

func (s *Server) handleCreateCompany(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Name = strings.TrimSpace(in.Name); in.Name == "" {
		fail(w, apperr.CompanyNameRequired)
		return
	}
	id := db.NewID()
	err := pgx.BeginFunc(r.Context(), s.DB.Owner, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `INSERT INTO companies(id,name) VALUES ($1,$2)`, id, in.Name); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO memberships(user_id,company_id,role) VALUES ($1,$2,'owner')`, session(r).UserID, id)
		return err
	})
	if err != nil {
		dbError(w, "create company", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "name": in.Name, "role": "owner"})
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Owner.Query(r.Context(),
		`SELECT u.id, u.email, m.role FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.company_id=$1 ORDER BY u.email`,
		session(r).CompanyID)
	if err != nil {
		dbError(w, "list members", err)
		return
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (map[string]string, error) {
		var id, email, role string
		err := row.Scan(&id, &email, &role)
		return map[string]string{"user_id": id, "email": email, "role": role}, err
	})
	if err != nil {
		dbError(w, "list members", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

// handleAddMember lets an owner add an existing user to the workspace
// with a role, or change an existing member's role.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sess := session(r)
	if sess.Role != "owner" {
		s.deny(w, r, start, apperr.AuthForbidden, "add_member", "memberships", "", "", "only owner may manage members")
		return
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, ok := s.Pol.Roles[in.Role]; !ok {
		fail(w, apperr.AuthInvalidRole)
		return
	}
	var userID string
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT id FROM users WHERE email=$1`, strings.TrimSpace(in.Email)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.AuthUserNotFound)
		return
	}
	if err != nil {
		dbError(w, "add member lookup", err)
		return
	}
	if _, err := s.DB.Owner.Exec(r.Context(),
		`INSERT INTO memberships(user_id,company_id,role) VALUES ($1,$2,$3) ON CONFLICT (user_id,company_id) DO UPDATE SET role=$3`,
		userID, sess.CompanyID, in.Role); err != nil {
		dbError(w, "add member", err)
		return
	}
	s.audit(r, start, "add_member", "memberships", userID, sess.CompanyID, true, "role "+in.Role)
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": userID, "company_id": sess.CompanyID, "role": in.Role})
}

func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Pol)
}
