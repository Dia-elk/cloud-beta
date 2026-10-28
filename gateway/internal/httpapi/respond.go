package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"cloudbeta/gateway/internal/apperr"

	"github.com/jackc/pgx/v5"
)

const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// fail writes a registered error code; text comes only from apperr.
func fail(w http.ResponseWriter, code apperr.Code) {
	status, body := apperr.Lookup(code)
	writeJSON(w, status, body)
}

// dbError reports a failed tenant query or write as GATEWAY_001 instead
// of a silent 200 with empty data. The real error is logged, never sent.
func dbError(w http.ResponseWriter, where string, err error) {
	log.Printf("db error in %s: %v", where, err)
	fail(w, apperr.GatewayDatabase)
}

// readJSON decodes a size-limited JSON body. An empty body decodes to
// the zero value, so optional-body endpoints still work.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		fail(w, apperr.GatewayBadBody)
		return false
	}
	return true
}

type ctxKey int

const sessionKey ctxKey = iota

// Session is the authenticated caller, resolved on every request.
type Session struct {
	UserID    string
	CompanyID string
	Role      string
	Email     string
	TokenID   string
	TokenExp  time.Time
}

func session(r *http.Request) Session {
	s, _ := r.Context().Value(sessionKey).(Session)
	return s
}

// tenant runs fn in a transaction on the non-owner pool with RLS
// session variables set, so Postgres enforces company and role.
func (s *Server) tenant(ctx context.Context, sess Session, fn func(pgx.Tx) error) error {
	tx, err := s.DB.App.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.current_company_id', $1, true), set_config('app.current_user_id', $2, true), set_config('app.current_role', $3, true)`,
		sess.CompanyID, sess.UserID, sess.Role); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) audit(r *http.Request, start time.Time, action, table, targetID, targetCompany string, allowed bool, reason string) {
	sess := session(r)
	s.DB.Audit(r.Context(), sess.CompanyID, sess.UserID, sess.Role, "", action, table, targetID, targetCompany, allowed, reason, time.Since(start).Milliseconds())
}

// deny audits a refused action with its internal reason, then answers
// with the public error code only.
func (s *Server) deny(w http.ResponseWriter, r *http.Request, start time.Time, code apperr.Code, action, table, targetID, targetCompany, reason string) {
	s.audit(r, start, action, table, targetID, targetCompany, false, reason)
	fail(w, code)
}

// allow checks the gateway-layer policy and denies with AUTH_007.
func (s *Server) allow(w http.ResponseWriter, r *http.Request, start time.Time, table, op, action, targetID string) bool {
	if s.Pol.Allowed(table, op, session(r).Role) {
		return true
	}
	s.deny(w, r, start, apperr.AuthForbidden, action, table, targetID, "", "role "+session(r).Role+" may not "+op+" "+table)
	return false
}

// ownedBy denies with TENANT_001 (audited and mirrored to the target
// company) when a row belongs to another company.
func (s *Server) ownedBy(w http.ResponseWriter, r *http.Request, start time.Time, rowCompany, action, table, targetID string) bool {
	if rowCompany == session(r).CompanyID {
		return true
	}
	s.deny(w, r, start, apperr.TenantCrossDenied, action, table, targetID, rowCompany, "cross-tenant denied")
	return false
}
