# Cloud

**From shadow app to governed app.** Apps your team builds, including AI-built ones, run behind one permission gateway. The app code is untrusted: Postgres row-level security, generated from a single policy file, decides who sees which rows, even when the app forgets the filter. Every decision lands in an audit log.

> Status: working MVP for local use and demos. Not production: see [Limits](#limits).

## What's in the box

| Part | What it does |
|---|---|
| **Gateway** (`gateway/`, Go 1.23) | Auth (cookie sessions with rotating refresh tokens), policy checks, RLS session setup, audit log, share links, app deploys, self-healing |
| **Console** (`dashboard/`, Next.js 15) | Overview, apps, clients, share links, access and roles, healing, audit log. Light and dark mode |
| **Client Tracker** (`sample-app/`, Next.js 15) | The template app. Holds no database password; signs in through the same session as the console |
| **MCP server** (`gateway/cmd/mcp`) | `create_app`, `deploy_app`, `check_permissions`, `view_audit_log` for AI agents, through the gateway only |
| **PostgreSQL 16** | One owner role for the control plane, one non-owner `cloud_app` role for tenant data, so RLS always applies |

```
Browser / AI agent ──► Gateway ──► Postgres (RLS on every tenant table)
  console :3000          :8080       owner pool: migrations, auth, audit
  apps :3001, :3100+                 cloud_app pool: all tenant reads/writes
```

Each tenant request is checked twice: once by the policy in the gateway, then again by Postgres RLS using `app.current_company_id` and `app.current_role`, set per transaction. The gateway **refuses to start** if the tenant pool is missing, is the owner, is a superuser, or if RLS is off on any tenant table.

## Quick start

Needs Docker with Compose v2. Ports 3000, 3001, 8080 and 3100–3199 must be free.

```bash
cp .env.example .env
```

Replace every `REPLACE` value in `.env` with `openssl rand -hex 32` output (`JWT_SECRET` needs 32+ characters), then:

```bash
docker compose up -d --build --wait
```

Open **http://localhost:3000**, create a workspace, then create and deploy an app from **Apps**. Deployed apps open already signed in.

## Roles

Defined in [`gateway/policy.json`](gateway/policy.json) and compiled into RLS on startup. Anything not listed is denied.

| Role | Can |
|---|---|
| Owner | Everything: read, write, delete, manage members, approve and apply fixes |
| Employee | Read and write records, deploy apps. No deletes |
| Accountant | Read only |

## Sessions

- `POST /api/v1/auth/login` (or `register`) sets two **httpOnly, SameSite=Strict** cookies: a 1-hour access token and a 7-day refresh token scoped to `/api/v1/auth`.
- `POST /api/v1/auth/refresh` **rotates** the refresh token. Reusing an old one revokes the whole session family (stolen-token detection).
- `POST /api/v1/auth/logout` revokes the refresh family and the access token, even before it expires.
- API clients (MCP, scripts) may send `Authorization: Bearer <token>` instead; the token is also in the login response body.
- CORS allows credentials only from `ALLOWED_ORIGINS` and deployed apps on `DEPLOY_PUBLIC_HOST:3100–3199`. Never `*`.
- Set `COOKIE_SECURE=true` whenever the stack is served over HTTPS.

## Errors

Every error is `{"code": "SERVICE_NNN", "message": {"en", "fr", "ar"}}`. No raw database or Docker error ever reaches a client. Full list: [docs/ERROR_CODES.md](docs/ERROR_CODES.md).

## API

| Method & path | Who |
|---|---|
| `POST /api/v1/auth/register` · `login` · `refresh` · `logout` | public |
| `GET /api/v1/me` · `POST /api/v1/auth/switch-company` | signed in |
| `GET/POST /api/v1/companies` · `GET/POST /api/v1/memberships` | signed in · owner to add members |
| `GET/POST /api/v1/apps` · `DELETE /api/v1/apps/{id}` | by policy |
| `POST /api/v1/apps/{id}/deploy` · `/stop` · `GET /api/v1/apps/{id}/health` | by policy |
| `GET/POST /api/v1/clients` · `GET/PUT/DELETE /api/v1/clients/{id}` | by policy |
| `GET/POST /api/v1/shares` · `POST /api/v1/shares/{id}/revoke` · `GET /share/{token}` | by policy · public token |
| `POST /api/v1/heal/detect` · `simulate-break` · `GET /api/v1/heal/proposals` · `POST /api/v1/heal/{id}/approve` · `/apply` | owner writes |
| `GET /api/v1/audit` · `GET /api/v1/audit/summary` · `GET /api/v1/policy` | by policy · public |

## Testing

```bash
docker run --rm -v "$PWD":/repo -w /repo/gateway golang:1.23-alpine go test ./...
```

```bash
sh scripts/prove-moat.sh
```

```bash
sh scripts/prove-deploy.sh
```

- **Unit tests** cover policy and RLS generation, JWT, UUIDs, error codes (including that every code is documented), CORS, auth failure codes, cookies, heal whitelists and the Docker client.
- **`prove-moat.sh`** runs against the live stack: cross-tenant 403 with an audit row mirrored to the target company, accountant create 403, employee delete 403, share expiry, heal owner-only, and the gateway refusing to start with an owner or empty tenant DSN.
- **`prove-deploy.sh`** deploys two apps on separate ports, checks health, stop and delete, and that a preview-only fix cannot be applied.
- **CI** (`.github/workflows/ci.yml`) runs all of it on every pull request, including a full Docker end-to-end job.

## MCP

```bash
MCP_TOKEN=<token from login> MCP_COMPANY_ID=<workspace id> docker compose run --rm -i mcp
```

Send JSON-RPC lines on stdin (`initialize`, `tools/list`, `tools/call`). No tool can run SQL or see a secret.

## Limits

Honest list, for demos and planning:

- **Local runtime.** The gateway mounts the host Docker socket to start app containers. A compromised gateway could control the host's Docker. Production needs a separate orchestrator or per-tenant runtime.
- **One template.** Apps deploy from the Client Tracker template. Git repository deploys return `APP_003`.
- **Healing applies one fix class**: restoring a missing `clients` column. Everything else is preview-only by design.
- **Not built yet:** billing, SSO, private data plane, rate limiting, email verification and password reset.
- Heal proposals are control-plane rows written by the owner pool, so they are protected by gateway checks rather than RLS.

## Layout

```
gateway/
  cmd/gateway        entry point, graceful shutdown, healthcheck
  cmd/mcp            MCP server (stdio)
  internal/apperr    error codes + FR/AR/EN messages
  internal/auth      passwords, JWT, tokens, UUIDs
  internal/db        pools, migrations, RLS fail-hard checks, audit writer
  internal/deploy    Docker API client for app containers
  internal/httpapi   routes, middleware, handlers
  internal/policy    policy.json loader + RLS generator
  migrations/        idempotent SQL, applied in order on startup
dashboard/           console (Next.js)
sample-app/          Client Tracker template (Next.js)
scripts/             live proof scripts
docs/                error codes, test history
```
