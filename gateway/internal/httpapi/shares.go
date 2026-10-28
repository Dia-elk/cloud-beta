package httpapi

import (
	"errors"
	"net/http"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/auth"
	"cloudbeta/gateway/internal/db"

	"github.com/jackc/pgx/v5"
)

const maxShareTTL = 30 * 24 * time.Hour

type share struct {
	ID        string    `json:"id"`
	Table     string    `json:"table_name"`
	RowID     string    `json:"row_id"`
	RowName   string    `json:"row_name"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

// handleListShares lists the caller's company shares via RLS. Token
// hashes and raw tokens are never returned.
func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !s.allow(w, r, start, "shares", "read", "read", "") {
		return
	}
	var out []share
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`SELECT s.id, s.table_name, s.row_id, coalesce(c.name,''), s.expires_at, s.revoked, s.created_at
			 FROM shares s LEFT JOIN clients c ON c.id = s.row_id ORDER BY s.created_at DESC`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (share, error) {
			var sh share
			err := row.Scan(&sh.ID, &sh.Table, &sh.RowID, &sh.RowName, &sh.ExpiresAt, &sh.Revoked, &sh.CreatedAt)
			return sh, err
		})
		return err
	})
	if err != nil {
		dbError(w, "list shares", err)
		return
	}
	if out == nil {
		out = []share{}
	}
	s.audit(r, start, "read", "shares", "", session(r).CompanyID, true, "")
	writeJSON(w, http.StatusOK, map[string]any{"shares": out})
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	start, sess := time.Now(), session(r)
	if !s.allow(w, r, start, "shares", "write", "create", "") {
		return
	}
	var in struct {
		RowID     string `json:"row_id"`
		ExpiresIn string `json:"expires_in"` // Go duration, e.g. 1h, 24h
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.RowID == "" {
		fail(w, apperr.ShareRowRequired)
		return
	}
	company, ok := s.clientCompany(w, r, in.RowID)
	if !ok || !s.ownedBy(w, r, start, company, "create", "shares", in.RowID) {
		return
	}
	ttl := 24 * time.Hour
	if d, err := time.ParseDuration(in.ExpiresIn); err == nil && d > 0 {
		ttl = min(d, maxShareTTL)
	}
	token, id, expires := auth.RandomToken(), db.NewID(), time.Now().Add(ttl)
	if _, err := s.DB.Owner.Exec(r.Context(),
		`INSERT INTO shares(id,company_id,table_name,row_id,token_hash,expires_at,created_by) VALUES ($1,$2,'clients',$3,$4,$5,$6)`,
		id, sess.CompanyID, in.RowID, auth.HashToken(token), expires, sess.UserID); err != nil {
		dbError(w, "create share", err)
		return
	}
	s.audit(r, start, "create", "shares", id, sess.CompanyID, true, "")
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "token": token, "url": "/share/" + token, "expires_at": expires,
		"note": "the token is shown once; only its hash is stored",
	})
}

// handleSharePublic serves one row for a valid token: no listing,
// expiry and revocation checked. It is the only owner-pool tenant read,
// scoped to a single token hash.
func (s *Server) handleSharePublic(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var companyID, rowID string
	var expires time.Time
	var revoked bool
	err := s.DB.Owner.QueryRow(r.Context(),
		`SELECT company_id,row_id,expires_at,revoked FROM shares WHERE token_hash=$1`, auth.HashToken(token)).
		Scan(&companyID, &rowID, &expires, &revoked)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		dbError(w, "share lookup", err)
		return
	}
	if err != nil || revoked || time.Now().After(expires) {
		fail(w, apperr.ShareInvalid)
		return
	}
	var name, email, notes string
	err = s.DB.Owner.QueryRow(r.Context(),
		`SELECT name,email,notes FROM clients WHERE id=$1 AND company_id=$2`, rowID, companyID).Scan(&name, &email, &notes)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.ShareRowNotFound)
		return
	}
	if err != nil {
		dbError(w, "share row", err)
		return
	}
	s.DB.Audit(r.Context(), companyID, "share", "share", "", "read", "clients", rowID, companyID, true, "share-token read, single row only", 0)
	writeJSON(w, http.StatusOK, map[string]any{"id": rowID, "name": name, "email": email, "notes": notes, "via": "share_token", "expires_at": expires})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	if !s.allow(w, r, start, "shares", "delete", "revoke", id) {
		return
	}
	var affected int64
	if err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		cmd, err := tx.Exec(r.Context(), `UPDATE shares SET revoked=true WHERE id=$1`, id)
		affected = cmd.RowsAffected()
		return err
	}); err != nil {
		dbError(w, "revoke share", err)
		return
	}
	if affected == 0 {
		fail(w, apperr.ShareNotFound)
		return
	}
	s.audit(r, start, "revoke", "shares", id, session(r).CompanyID, true, "")
	writeJSON(w, http.StatusOK, map[string]string{"revoked": id})
}
