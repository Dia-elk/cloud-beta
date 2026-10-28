package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cloudbeta/gateway/internal/apperr"
	"cloudbeta/gateway/internal/db"

	"github.com/jackc/pgx/v5"
)

// Self-healing: detect a schema break, propose a fix, require owner
// approval, and apply only whitelisted fixes. Heal proposals are
// control-plane objects written via the owner pool, so RLS does not
// protect them; policy is enforced here, owner-only for every write.

var errHealNotFound = errors.New("heal proposal not found or already decided")

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

// healColumnDefinitions is the ONLY source of column names and types
// the apply path may use. "id" is deliberately absent: it is a primary
// key and is never applyable.
var healColumnDefinitions = map[string]string{
	"email":      "TEXT NOT NULL DEFAULT ''",
	"notes":      "TEXT NOT NULL DEFAULT ''",
	"name":       "TEXT NOT NULL DEFAULT ''",
	"company_id": "TEXT NOT NULL DEFAULT ''",
	"created_at": "TIMESTAMPTZ NOT NULL DEFAULT now()",
}

// applyableColumns validates a proposal for apply. Only
// fix_kind='add_missing_columns' with every column in the whitelist is
// applyable; simulated proposals, unknown kinds and unknown columns
// (including "id") are preview-only. The result is deduplicated, in
// proposal order.
func applyableColumns(fixKind, fixColumn string) ([]string, bool) {
	if fixKind != "add_missing_columns" {
		return nil, false
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(fixColumn, ",") {
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

func (s *Server) clientColumns(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Owner.Query(ctx,
		`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='clients' ORDER BY ordinal_position`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Server) handleHealDetect(w http.ResponseWriter, r *http.Request) {
	start, sess := time.Now(), session(r)
	if !s.allow(w, r, start, "heal_proposals", "write", "create", "") {
		return
	}
	cols, err := s.clientColumns(r.Context())
	if err != nil {
		dbError(w, "heal detect", err)
		return
	}
	missing := missingColumns(cols)
	if len(missing) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "healthy", "missing": []string{}})
		return
	}
	id := db.NewID()
	list := strings.Join(missing, ", ")
	title := "Missing client column(s): " + list
	diff := "Restore the missing column(s) " + list + " on the clients table, then re-run the app's client query. Nothing changes until an owner approves and applies this fix."
	if _, err := s.DB.Owner.Exec(r.Context(),
		`INSERT INTO heal_proposals(id,company_id,title,diff,status,applied,fix_kind,fix_column) VALUES ($1,$2,$3,$4,'pending',false,'add_missing_columns',$5)`,
		id, sess.CompanyID, title, diff, strings.Join(missing, ",")); err != nil {
		dbError(w, "heal detect insert", err)
		return
	}
	s.audit(r, start, "detect", "heal_proposals", id, sess.CompanyID, true, title)
	writeJSON(w, http.StatusOK, map[string]any{"status": "proposal_created", "id": id, "missing": missing, "diff": diff})
}

// handleHealSimulate creates a labelled preview proposal for a field the
// operator names, without changing any table. It can never be applied.
func (s *Server) handleHealSimulate(w http.ResponseWriter, r *http.Request) {
	start, sess := time.Now(), session(r)
	if !s.allow(w, r, start, "heal_proposals", "write", "create", "") {
		return
	}
	var in struct {
		Field string `json:"field"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Field = strings.TrimSpace(in.Field); in.Field == "" {
		in.Field = "email"
	}
	id := db.NewID()
	title := "Simulated break: field '" + in.Field + "'"
	diff := "Simulation only: the app stopped finding the field '" + in.Field + "'. Update the app's query to the new field name, or restore '" + in.Field + "'. Preview only: this proposal can be approved but never applied automatically."
	if _, err := s.DB.Owner.Exec(r.Context(),
		`INSERT INTO heal_proposals(id,company_id,title,diff,status,applied,fix_kind,fix_column) VALUES ($1,$2,$3,$4,'pending',false,'simulated',$5)`,
		id, sess.CompanyID, title, diff, in.Field); err != nil {
		dbError(w, "heal simulate", err)
		return
	}
	s.audit(r, start, "simulate", "heal_proposals", id, sess.CompanyID, true, title)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "title": title, "diff": diff, "status": "pending", "applied": false})
}

type proposal struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Diff      string    `json:"diff"`
	Status    string    `json:"status"`
	Applied   bool      `json:"applied"`
	FixKind   string    `json:"fix_kind"`
	FixColumn string    `json:"fix_column"`
	Applyable bool      `json:"applyable"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) handleHealProposals(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, time.Now(), "heal_proposals", "read", "read", "") {
		return
	}
	var out []proposal
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`SELECT id,title,diff,status,applied,fix_kind,fix_column,created_at FROM heal_proposals ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (proposal, error) {
			var p proposal
			err := row.Scan(&p.ID, &p.Title, &p.Diff, &p.Status, &p.Applied, &p.FixKind, &p.FixColumn, &p.CreatedAt)
			_, p.Applyable = applyableColumns(p.FixKind, p.FixColumn)
			return p, err
		})
		return err
	})
	if err != nil {
		dbError(w, "list heal proposals", err)
		return
	}
	if out == nil {
		out = []proposal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": out})
}

// handleHealApprove approves a pending proposal in the caller's company.
// Approval never applies anything.
func (s *Server) handleHealApprove(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	if !s.allow(w, r, start, "heal_proposals", "write", "approve", id) {
		return
	}
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		cmd, err := tx.Exec(r.Context(), `UPDATE heal_proposals SET status='approved' WHERE id=$1 AND status='pending'`, id)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return errHealNotFound
		}
		return nil
	})
	if errors.Is(err, errHealNotFound) {
		s.audit(r, start, "approve", "heal_proposals", id, session(r).CompanyID, false, "proposal not found or already decided")
		fail(w, apperr.HealNotFound)
		return
	}
	if err != nil {
		dbError(w, "approve heal proposal", err)
		return
	}
	s.audit(r, start, "approve", "heal_proposals", id, session(r).CompanyID, true, "")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "approved", "applied": false})
}

// handleHealApply applies an APPROVED, whitelisted missing-column fix.
// Anything else changes nothing: pending -> HEAL_002, already applied ->
// HEAL_003, simulated/unknown -> HEAL_004 with the proposal untouched.
func (s *Server) handleHealApply(w http.ResponseWriter, r *http.Request) {
	start, id := time.Now(), r.PathValue("id")
	if !s.allow(w, r, start, "heal_proposals", "write", "apply", id) {
		return
	}
	var company, status, fixKind, fixColumn string
	var applied bool
	err := s.DB.Owner.QueryRow(r.Context(),
		`SELECT company_id,status,fix_kind,fix_column,applied FROM heal_proposals WHERE id=$1`, id).
		Scan(&company, &status, &fixKind, &fixColumn, &applied)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, apperr.HealNotFound)
		return
	}
	if err != nil {
		dbError(w, "heal apply lookup", err)
		return
	}
	if !s.ownedBy(w, r, start, company, "apply", "heal_proposals", id) {
		return
	}
	switch {
	case status == "pending":
		fail(w, apperr.HealApproveFirst)
		return
	case applied || status == "applied":
		fail(w, apperr.HealAlreadyApplied)
		return
	case status != "approved":
		fail(w, apperr.HealNotFound)
		return
	}
	cols, ok := applyableColumns(fixKind, fixColumn)
	if !ok {
		fail(w, apperr.HealNotApplyable)
		return
	}
	// DDL: column names and definitions come ONLY from the whitelist
	// validated above, never from raw input.
	err = pgx.BeginFunc(r.Context(), s.DB.Owner, func(tx pgx.Tx) error {
		for _, col := range cols {
			if _, err := tx.Exec(r.Context(), `ALTER TABLE clients ADD COLUMN IF NOT EXISTS `+col+` `+healColumnDefinitions[col]); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `UPDATE heal_proposals SET status='applied', applied=true WHERE id=$1`, id)
		return err
	})
	if err != nil {
		s.audit(r, start, "apply", "heal_proposals", id, company, false, "apply failed: "+err.Error())
		fail(w, apperr.HealApplyFailed)
		return
	}
	after, err := s.clientColumns(r.Context())
	if err != nil {
		dbError(w, "heal apply verify", err)
		return
	}
	if m := missingColumns(after); len(m) > 0 {
		for _, col := range cols {
			for _, still := range m {
				if col == still {
					fail(w, apperr.HealApplyFailed)
					return
				}
			}
		}
	}
	s.audit(r, start, "apply", "heal_proposals", id, company, true, "applied columns "+strings.Join(cols, ",")+"; columns after: "+strings.Join(after, ","))
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "applied", "applied": true, "applied_columns": cols, "columns_after": after})
}
