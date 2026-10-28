package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type auditRow struct {
	Action    string    `json:"action"`
	Table     string    `json:"table"`
	TargetID  string    `json:"target_id"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason"`
	LatencyMS int64     `json:"latency_ms"`
	CreatedAt time.Time `json:"created_at"`
	ActorRole string    `json:"actor_role"`
}

type auditDay struct {
	Date    string `json:"date"`
	Total   int64  `json:"total"`
	Allowed int64  `json:"allowed"`
	Denied  int64  `json:"denied"`
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, time.Now(), "audit_logs", "read", "read", "") {
		return
	}
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	var out []auditRow
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`SELECT action,table_name,target_id,allowed,reason,latency_ms,created_at,actor_role FROM audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (auditRow, error) {
			var a auditRow
			err := row.Scan(&a.Action, &a.Table, &a.TargetID, &a.Allowed, &a.Reason, &a.LatencyMS, &a.CreatedAt, &a.ActorRole)
			return a, err
		})
		return err
	})
	if err != nil {
		dbError(w, "list audit", err)
		return
	}
	if out == nil {
		out = []auditRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}

// handleAuditSummary aggregates real audit rows only; nothing is
// invented when the table is empty.
func (s *Server) handleAuditSummary(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, time.Now(), "audit_logs", "read", "read", "") {
		return
	}
	var total, allowed, denied int64
	var avg float64
	days := []auditDay{}
	err := s.tenant(r.Context(), session(r), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(),
			`SELECT count(*), count(*) FILTER (WHERE allowed), count(*) FILTER (WHERE NOT allowed), coalesce(avg(latency_ms),0) FROM audit_logs`).
			Scan(&total, &allowed, &denied, &avg); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(),
			`SELECT created_at::date, count(*), count(*) FILTER (WHERE allowed), count(*) FILTER (WHERE NOT allowed)
			 FROM audit_logs WHERE created_at >= now() - interval '7 days' GROUP BY 1 ORDER BY 1`)
		if err != nil {
			return err
		}
		days, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (auditDay, error) {
			var d auditDay
			var date time.Time
			err := row.Scan(&date, &d.Total, &d.Allowed, &d.Denied)
			d.Date = date.Format("2006-01-02")
			return d, err
		})
		return err
	})
	if err != nil {
		dbError(w, "audit summary", err)
		return
	}
	if days == nil {
		days = []auditDay{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "allowed": allowed, "denied": denied, "avg_latency_ms": avg, "per_day": days})
}
