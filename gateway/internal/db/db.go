// Package db: owner pool (migrations, auth, audit writes, share-token lookup)
// and app pool (tenant data, subject to RLS). The app pool connects as
// cloud_app, a non-owner role, so Postgres RLS actually applies to it.
// The owner pool must never be given to a sample app.
package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloudbeta/gateway/internal/policy"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Owner *pgxpool.Pool
	App   *pgxpool.Pool
	Pol   *policy.Policy
}

func Connect(ctx context.Context, ownerDSN, appDSN string, pol *policy.Policy) (*DB, error) {
	owner, err := pgxpool.New(ctx, ownerDSN)
	if err != nil {
		return nil, err
	}
	if err := owner.Ping(ctx); err != nil {
		return nil, fmt.Errorf("owner db ping: %w", err)
	}
	mig, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		mig, err = os.ReadFile("/gateway/migrations/001_init.sql")
		if err != nil {
			return nil, err
		}
	}
	if _, err := owner.Exec(ctx, string(mig)); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// MVP v1 columns for existing databases (fresh schemas get them
	// from 001_init.sql itself). ADD COLUMN IF NOT EXISTS keeps this
	// safe to run on every startup.
	for _, ddl := range []string{
		`ALTER TABLE apps ADD COLUMN IF NOT EXISTS container_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE apps ADD COLUMN IF NOT EXISTS port INT NOT NULL DEFAULT 0`,
		`ALTER TABLE heal_proposals ADD COLUMN IF NOT EXISTS fix_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE heal_proposals ADD COLUMN IF NOT EXISTS fix_column TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := owner.Exec(ctx, ddl); err != nil {
			return nil, fmt.Errorf("migrate columns: %w", err)
		}
	}
	if _, err := owner.Exec(ctx, pol.RLS()); err != nil {
		return nil, fmt.Errorf("apply RLS: %w", err)
	}
	if _, err := owner.Exec(ctx, `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO cloud_app`); err == nil {
		_, _ = owner.Exec(ctx, `GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO cloud_app`)
	}
	// Fix 1 (CRITICAL): fail hard, never fall back to the owner pool for
	// tenant data. A silent fallback would make RLS stop applying while
	// the Go-layer proofs still pass, hiding a total moat failure.
	if strings.TrimSpace(appDSN) == "" {
		owner.Close()
		return nil, fmt.Errorf("APP_DATABASE_URL is empty: tenant data requires a separate non-owner pool, refusing to start")
	}
	if appDSN == ownerDSN {
		owner.Close()
		return nil, fmt.Errorf("APP_DATABASE_URL equals DATABASE_URL: tenant pool must not be the owner/superuser pool, refusing to start")
	}
	app, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		owner.Close()
		return nil, fmt.Errorf("app db pool: %w", err)
	}
	if err := app.Ping(ctx); err != nil {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("app db ping: %w", err)
	}
	// Verify the app pool really is a non-superuser (expected: cloud_app).
	var appUser string
	var isSuper bool
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&appUser); err != nil {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("app db current_user: %w", err)
	}
	if err := app.QueryRow(ctx, `SELECT usesuper FROM pg_catalog.pg_user WHERE usename = current_user`).Scan(&isSuper); err != nil {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("app db usesuper check: %w", err)
	}
	if isSuper {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("app db user %q is a superuser: tenant pool must be a non-superuser (cloud_app), refusing to start", appUser)
	}
	if appUser != "cloud_app" {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("app db user %q is not cloud_app: tenant pool must connect as cloud_app, refusing to start", appUser)
	}
	// Verify RLS is enabled on every tenant table the policy covers.
	tenantTables := []string{"clients", "audit_logs", "shares", "heal_proposals", "apps"}
	var rlsOn int
	if err := app.QueryRow(ctx,
		`SELECT count(*) FROM pg_class WHERE relname = ANY($1) AND relrowsecurity = true`,
		tenantTables).Scan(&rlsOn); err != nil {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("RLS check: %w", err)
	}
	if rlsOn != len(tenantTables) {
		app.Close()
		owner.Close()
		return nil, fmt.Errorf("RLS not enabled on all tenant tables: %d of %d enabled (%v), refusing to start", rlsOn, len(tenantTables), tenantTables)
	}
	log.Printf("app pool user=%s superuser=false, RLS enabled on %v (tenant pool is cloud_app, non-superuser)", appUser, tenantTables)
	return &DB{Owner: owner, App: app, Pol: pol}, nil
}

// Audit writes via the owner pool, so a denied attempt is logged even
// though the tenant session would not be able to see the other tenant.
//
// Fix 4: a cross-tenant denial is mirrored to the targeted company.
// Without this, the target never sees the attack, because audit reads
// are filtered by company_id. The mirror row contains NO target row
// content — only actor/action/table/target ids and the denial reason.
// The attacker company still gets its own original row, so a single
// cross-tenant denial appears in both companies' audit views, by design.
// That means audit summary totals across companies double-count one
// attack; per-company totals remain correct.
func (d *DB) Audit(ctx context.Context, companyID, actorID, actorRole, appID, action, table, targetID, targetCompany string, allowed bool, reason string, latencyMs int64) {
	_, _ = d.Owner.Exec(ctx,
		`INSERT INTO audit_logs(id, company_id, actor_id, actor_role, app_id, action, table_name, target_id, target_company_id, allowed, reason, latency_ms)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		ID("aud"), companyID, actorID, actorRole, appID, action, table, targetID, targetCompany, allowed, reason, latencyMs)
	if !allowed && targetCompany != "" && targetCompany != companyID {
		mirrorReason := "cross-tenant denied: attacker attempted access"
		if reason != "" && reason != "cross-tenant denied" {
			mirrorReason = mirrorReason + " (" + reason + ")"
		}
		_, _ = d.Owner.Exec(ctx,
			`INSERT INTO audit_logs(id, company_id, actor_id, actor_role, app_id, action, table_name, target_id, target_company_id, allowed, reason, latency_ms)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			ID("aud"), targetCompany, actorID, actorRole, appID, action, table, targetID, targetCompany, false, mirrorReason, latencyMs)
	}
}

func ID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}
