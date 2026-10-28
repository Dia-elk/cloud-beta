-- Cloud beta schema. Applied by the gateway on startup (owner connection).
-- RLS policies are generated from policy.json, NOT hand-written here,
-- so the policy file is the single source of truth.
CREATE TABLE IF NOT EXISTS companies (
  id TEXT PRIMARY KEY, name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS memberships (
  user_id TEXT NOT NULL REFERENCES users(id),
  company_id TEXT NOT NULL REFERENCES companies(id),
  role TEXT NOT NULL DEFAULT 'employee',
  PRIMARY KEY (user_id, company_id)
);
CREATE TABLE IF NOT EXISTS apps (
  id TEXT PRIMARY KEY, company_id TEXT NOT NULL REFERENCES companies(id),
  name TEXT NOT NULL, repo TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'registered',
  container_id TEXT NOT NULL DEFAULT '', port INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Sample app: a simple client tracker. This is the tenant data the moat protects.
CREATE TABLE IF NOT EXISTS clients (
  id TEXT PRIMARY KEY, company_id TEXT NOT NULL REFERENCES companies(id),
  name TEXT NOT NULL, email TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY, company_id TEXT NOT NULL, actor_id TEXT NOT NULL DEFAULT '',
  actor_role TEXT NOT NULL DEFAULT '', app_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL, table_name TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL DEFAULT '',
  target_company_id TEXT NOT NULL DEFAULT '', allowed BOOLEAN NOT NULL DEFAULT true,
  reason TEXT NOT NULL DEFAULT '', latency_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS shares (
  id TEXT PRIMARY KEY, company_id TEXT NOT NULL REFERENCES companies(id),
  table_name TEXT NOT NULL, row_id TEXT NOT NULL, token_hash TEXT UNIQUE NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL, revoked BOOLEAN NOT NULL DEFAULT false,
  created_by TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS heal_proposals (
  id TEXT PRIMARY KEY, company_id TEXT NOT NULL REFERENCES companies(id),
  app_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL, diff TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending', applied BOOLEAN NOT NULL DEFAULT false,
  fix_kind TEXT NOT NULL DEFAULT '', fix_column TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
