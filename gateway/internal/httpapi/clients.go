package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/db"

	"github.com/jackc/pgx/v5"
)

// Client is the sample app's tenant data, the rows the moat protects.
type Client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

type clientInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Notes string `json:"notes"`
}

func (in *clientInput) normalise() bool {
	in.Name, in.Email, in.Notes = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), strings.TrimSpace(in.Notes)
	return in.Name != ""
}

// clientCompany looks a client up across tenants (owner pool) so a
// cross-tenant attempt is denied and audited, not hidden as a 404.
func (s *Server) clientCompany(w http.ResponseWriter, r *http.Request, id string) (string, bool) {
	var c string
	err := s.DB.Owner.QueryRow(r.Context(), `SELECT company_id FROM clients WHERE id=$1`, id).Scan(&c)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.ClientNotFound)
		return "", false
	}
	if err != nil {
		dbError(w, "client lookup", err)
		return "", false
	}
	return c, true
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !s.allow(w, r, start, "clients", "read", "read", "") {
		return
	}
	var out []Client
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id,name,email,notes,created_at FROM clients ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Client, error) {
			var c Client
			err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Notes, &c.CreatedAt)
			return c, err
		})
		return err
	})
	if err != nil {
		dbError(w, "list clients", err)
		return
	}
	if out == nil {
		out = []Client{}
	}
	s.audit(r, start, "read", "clients", "", session(r).CompanyID, true, "")
	writeJSON(w, http.StatusOK, map[string]any{"clients": out})
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	start, sess := time.Now(), session(r)
	if !s.allow(w, r, start, "clients", "write", "create", "") {
		return
	}
	var in clientInput
	if !readJSON(w, r, &in) {
		return
	}
	if !in.normalise() {
		fail(w, apperr.ClientNameRequired)
		return
	}
	c := Client{ID: db.NewID(), Name: in.Name, Email: in.Email, Notes: in.Notes}
	if err := s.tenant(r.Context(), sess, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`INSERT INTO clients(id,company_id,name,email,notes) VALUES ($1,$2,$3,$4,$5) RETURNING created_at`,
			c.ID, sess.CompanyID, c.Name, c.Email, c.Notes).Scan(&c.CreatedAt)
	}); err != nil {
		dbError(w, "create client", err)
		return
	}
	s.audit(r, start, "create", "clients", c.ID, sess.CompanyID, true, "")
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	company, ok := s.clientCompany(w, r, id)
	if !ok || !s.ownedBy(w, r, start, company, "get", "clients", id) {
		return
	}
	if !s.allow(w, r, start, "clients", "read", "read", id) {
		return
	}
	c := Client{ID: id}
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT name,email,notes,created_at FROM clients WHERE id=$1`, id).
			Scan(&c.Name, &c.Email, &c.Notes, &c.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.ClientNotFound)
		return
	}
	if err != nil {
		dbError(w, "read client", err)
		return
	}
	s.audit(r, start, "read", "clients", id, company, true, "")
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	company, ok := s.clientCompany(w, r, id)
	if !ok || !s.ownedBy(w, r, start, company, "put", "clients", id) {
		return
	}
	if !s.allow(w, r, start, "clients", "write", "update", id) {
		return
	}
	var in clientInput
	if !readJSON(w, r, &in) {
		return
	}
	if !in.normalise() {
		fail(w, apperr.ClientNameRequired)
		return
	}
	c := Client{ID: id, Name: in.Name, Email: in.Email, Notes: in.Notes}
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`UPDATE clients SET name=$2,email=$3,notes=$4 WHERE id=$1 RETURNING created_at`,
			id, c.Name, c.Email, c.Notes).Scan(&c.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.ClientNotFound)
		return
	}
	if err != nil {
		dbError(w, "update client", err)
		return
	}
	s.audit(r, start, "update", "clients", id, company, true, "")
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	company, ok := s.clientCompany(w, r, id)
	if !ok || !s.ownedBy(w, r, start, company, "delete", "clients", id) {
		return
	}
	if !s.allow(w, r, start, "clients", "delete", "delete", id) {
		return
	}
	if err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM clients WHERE id=$1`, id)
		return err
	}); err != nil {
		dbError(w, "delete client", err)
		return
	}
	s.audit(r, start, "delete", "clients", id, company, true, "")
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}
