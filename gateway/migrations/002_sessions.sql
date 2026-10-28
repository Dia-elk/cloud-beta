-- Sessions: rotating refresh tokens and revoked access tokens.
-- Control-plane only (owner pool). cloud_app has no grant on either.

-- Only the SHA-256 of a refresh token is stored. Each refresh rotates
-- the token; family_id links a chain so reuse of a rotated token can
-- revoke the whole chain (stolen-token detection).
CREATE TABLE IF NOT EXISTS refresh_tokens (
  id TEXT PRIMARY KEY,
  family_id TEXT NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  company_id TEXT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  token_hash TEXT UNIQUE NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS refresh_tokens_family_idx ON refresh_tokens(family_id);

-- Access tokens are stateless JWTs; logout records the token's jti here
-- until it would have expired anyway.
CREATE TABLE IF NOT EXISTS revoked_access_tokens (
  jti TEXT PRIMARY KEY,
  expires_at TIMESTAMPTZ NOT NULL
);
