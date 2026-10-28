# Agent Test Brief — Cloud Beta (real beta, not a demo)

> NOTE (2026-10-28, after the first machine test): read TEST_RESULTS.md
> and the README "Security fixes" section first. prove-moat.sh now
> also checks sections 5 (accountant heal 403, no proposal), 6
> (approve-missing 404), 7 (target audit mirror), and 5a (startup
> fail-hard, scripted if docker is available, otherwise manual).
> The 4 original proofs are unchanged.

## For the agent on Dia's machine: read this whole brief first, then test in order. Do not skip steps.

**Product:** Cloud for Small Software — pitch: "From shadow app to governed app in one click."
**Idea in one sentence:** An AI-built app runs through a permission gateway. The app code is untrusted. PostgreSQL Row Level Security, generated from a policy file, enforces who sees which rows — even if the app code forgets the filter.

**What you are testing:** The real beta scaffold in `cloud-beta/` (Go gateway + PostgreSQL 16 + Docker + Next.js dashboard + sample app + MCP). This is NOT the hosted demo artifact with sample data. This is NOT production.

**What you must NOT do:**
- Do NOT buy anything, deploy to production, publish a public link, or claim production readiness.
- Do NOT use real personal data, real company data, real passwords, or real API keys. Use only the test accounts in this brief.
- Do NOT commit a real `.env` file. `.env.example` has placeholders only — copy it and fill it locally.
- Do NOT auto-apply a self-healing diff. Healing approval in beta sets `approved` with `applied=false` only. An operator applies the diff manually.
- Do NOT change the policy, the roles, or the moat rules to make a test pass. If a test fails, report it as a failure with logs.
- Do NOT start a new container per app or build arbitrary repos in this beta — that is explicitly out of scope (see "Stubbed" below).

## 1. What you need on the machine

- Docker + Docker Compose (v2, `docker compose` command)
- curl, jq
- Ports free: 3000 (dashboard), 3001 (sample app), 8080 (gateway), Postgres internal only (not exposed on host)
- No Go / Node install required if you only run Docker, but `npm install` is needed if you rebuild dashboard/sample-app outside Docker

## 2. Code delivery

You should receive the full `cloud-beta/` folder (see file tree in section 4), ideally as a zip. Unzip it, then:

```sh
cd cloud-beta
ls -la
```

If any file in section 4 is missing, stop and report exactly which file is missing — do not invent it.

## 3. docker-compose.yml — use this file exactly as shipped

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-cloud_owner}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-change-me-owner}
      POSTGRES_DB: ${POSTGRES_DB:-cloudbeta}
      APP_DB_PASSWORD: ${APP_DB_PASSWORD:-change-me-app}
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./postgres/init-app-role.sh:/docker-entrypoint-initdb.d/01-app-role.sh:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-cloud_owner}"]
      interval: 5s
      timeout: 5s
      retries: 20
  gateway:
    build: ./gateway
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      PORT: "8080"
      DATABASE_URL: postgres://${POSTGRES_USER:-cloud_owner}:${POSTGRES_PASSWORD:-change-me-owner}@postgres:5432/${POSTGRES_DB:-cloudbeta}?sslmode=disable
      APP_DATABASE_URL: postgres://cloud_app:${APP_DB_PASSWORD:-change-me-app}@postgres:5432/${POSTGRES_DB:-cloudbeta}?sslmode=disable
      JWT_SECRET: ${JWT_SECRET:-beta-dev-secret-change-me}
    ports:
      - "8080:8080"
    volumes:
      - ./gateway/policy.json:/gateway/policy.json:ro
      - ./gateway/migrations:/gateway/migrations:ro
  dashboard:
    build: ./dashboard
    depends_on:
      - gateway
    environment:
      NEXT_PUBLIC_GATEWAY_URL: ${NEXT_PUBLIC_GATEWAY_URL:-http://localhost:8080}
    ports:
      - "3000:3000"
  sample-app:
    build: ./sample-app
    depends_on:
      - gateway
    environment:
      NEXT_PUBLIC_GATEWAY_URL: ${NEXT_PUBLIC_GATEWAY_URL:-http://localhost:8080}
    ports:
      - "3001:3000"
  mcp:
    build:
      context: ./gateway
      dockerfile: Dockerfile
    # MCP runs the /mcp binary, not the gateway. Override entrypoint.
    entrypoint: ["/mcp"]
    depends_on:
      - gateway
    environment:
      GATEWAY_URL: http://gateway:8080
      MCP_TOKEN: ${MCP_TOKEN:-}
      MCP_COMPANY_ID: ${MCP_COMPANY_ID:-}
    stdin_open: true
volumes:
  pgdata:
```

## 4. File tree (main files — the zip also contains dashboard/components/, gateway/internal/handlers/, gateway/testdata/, all code, no node_modules / .next — reinstall before running outside Docker)

```
cloud-beta/
  .env.example
  README.md
  AGENT_TEST_BRIEF.md  (this file)
  docker-compose.yml
  dashboard/           Next.js 15, TypeScript, Tailwind, dark Laravel/Nightwatch style
    Dockerfile, package.json, app/, lib/api.ts, next.config.ts, tailwind.config.js
  gateway/             Go 1.23, stdlib HTTP, pgx
    Dockerfile, go.mod, go.sum, policy.json, migrations/001_init.sql
    cmd/gateway/main.go, cmd/gateway/heal_test.go
    cmd/mcp/main.go
    internal/auth/auth.go, internal/auth/auth_test.go
    internal/db/db.go
    internal/policy/policy.go, internal/policy/policy_test.go
  postgres/
    init-app-role.sh   creates non-owner cloud_app role
  sample-app/          Next.js 15 client tracker, gateway-only, NO database password
    Dockerfile, package.json, app/, lib/api.ts
  scripts/
    prove-moat.sh      automated moat proof (see section 7)
```

## 5. Stack — big description

```
User / AI agent (Cursor, Claude, ...)
        |  JWT + MCP (no SQL, no secrets)
        v
Gateway — Go 1.23, stdlib HTTP, pgx
  policy.json -> RLS SQL
  JWT + bcrypt auth, audit log
        |  tenant session: app.current_company_id
        |  app.current_user_id, app.current_role
        v
PostgreSQL 16
  RLS ON for tenant tables
  role cloud_app (non-owner)
  owner pool only for migrations, auth, audit writes, share lookup
        |
   Dashboard (Next.js 15)     Sample app (Next.js 15)
   dark Laravel/Nightwatch    client tracker, NO DB password,
   small analytics from       talks to gateway only
   real audit rows only
```

- **Control plane + gateway:** Go. It authenticates, checks `policy.Allowed()` in code, and sets `app.current_company_id` / `app.current_user_id` / `app.current_role` via `SET LOCAL` on every tenant transaction.
- **Policy file:** `gateway/policy.json` — tenant field `company_id`, source `jwt.company_id`, default `deny`, roles: owner = read/write/delete/manage_members/manage_company, employee = read/write, accountant = read. Per-table read/write/delete lists for apps, audit_logs, clients, companies, heal_proposals, shares.
- **Tenant reads:** through non-owner `cloud_app` role, so RLS actually applies. Owner pool is NOT used for tenant data.
- **Audit:** every allowed/denied data action logged to Postgres with latency. Dashboard analytics read `/api/v1/audit/summary` — real rows only, no invented sample data at runtime.
- **Shares:** token hash stored only, raw token shown once, single-row read, expiry, revoke, no listing endpoint.
- **Self-healing preview:** `POST /api/v1/heal/detect` checks `information_schema` for missing client columns and proposes a restore diff; `POST /api/v1/heal/simulate-break` creates a labelled simulated proposal without changing any table. Owner approval -> `approved`, `applied=false`. Nothing auto-applied.
- **MCP:** Go JSON-RPC over stdio: `create_app`, `deploy_app`, `check_permissions`, `view_audit_log` — all via gateway HTTP, no SQL, no database password.
- **Explicitly OUT for beta:** Firecracker microVMs, Kubernetes, Temporal, billing, SSO/WorkOS, private data plane, auto-apply healing, per-app container orchestration.

## 6. Start it — in this exact order

```sh
cd cloud-beta
cp .env.example .env
# Edit .env: replace ALL change-me placeholders with strong local-only values.
# Do NOT use real secrets. Example .env.example content (placeholders only):
# POSTGRES_USER=cloud_owner
# POSTGRES_PASSWORD=change-me-owner
# POSTGRES_DB=cloudbeta
# APP_DB_PASSWORD=change-me-app
# JWT_SECRET=beta-dev-secret-change-me
# NEXT_PUBLIC_GATEWAY_URL=http://localhost:8080
# MCP_TOKEN= (leave empty until you have a real JWT from login)
# MCP_COMPANY_ID= (leave empty until you have a real company id)

docker compose config
# EXPECT: prints resolved compose, no error, services: postgres, gateway, dashboard, sample-app, mcp

docker compose up --build -d
docker compose ps
# EXPECT: all services up/healthy (postgres healthy first, then gateway)

curl http://localhost:8080/health
# EXPECT: a healthy response from the gateway

# Dashboard: http://localhost:3000
# Sample app: http://localhost:3001
```

If `docker compose up` fails, capture the full log (`docker compose logs --tail=200 <service>`) and report it — do not edit code to hide the failure.

## 7. Test protocol — all steps must pass, in order

### 7.1 Register and deploy (dashboard or curl)

```sh
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.ma","password":"beta-password-1","company_name":"Beta Company"}'
# Save the token, then:
curl -X POST http://localhost:8080/api/v1/apps \
  -H "Authorization: Bearer <TOKEN>" -H 'Content-Type: application/json' \
  -d '{"name":"Client Tracker","repo":"sample-app"}'
curl -X POST http://localhost:8080/api/v1/apps/<APP_ID>/deploy \
  -H "Authorization: Bearer <TOKEN>"
```

EXPECT: register returns a token + company_id, app creation returns an app id, deploy marks it deployed.

### 7.2 Prove the moat (automated — this is the core test)

```sh
sh scripts/prove-moat.sh
```

This script creates Company A + a client, Company B, an accountant and an employee in Company A, and a 2-second share. It MUST end with:
`MOAT PROVED: cross-tenant 403, accountant create 403, employee delete 403, share expiry 403.`

The four proofs, individually:
1. Company B reading a Company A client -> **403 + denied audit row** (not a silent 404)
2. Accountant in Company A creating a client -> **403**
3. Employee in Company A deleting a client -> **403**, and the client still exists for the owner afterwards (200)
4. Share with 2s expiry -> works once, single row only, then after 3s -> **403**

If ANY proof returns a different code, the moat has FAILED. Report the exact HTTP code, the response body, and the audit rows. Do not change policy.json to make it pass.

### 7.3 RLS proof, directly in Postgres (strong demo)

```sh
docker compose exec postgres psql -U cloud_app -d cloudbeta \
  -c "SELECT set_config('app.current_company_id','<COMPANY_A>',true); SELECT id,name FROM clients;"
```

EXPECT: as `cloud_app`, with Company A set, you only see Company A rows — even with a plain `SELECT *`, no WHERE clause. That is the moat, at the database layer, not just in app code. Repeat with Company B id and confirm you see only Company B rows.

### 7.4 Dashboard + sample app with a real login

- Open http://localhost:3000, register/login with a test account, confirm the dashboard loads.
- Open http://localhost:3001, use the sample client tracker through the gateway (no direct DB access from the sample app).
- Check dashboard analytics (requests, allowed, denied, avg latency, governed apps, clients, per-day counts) match the audit rows created by sections 7.2–7.3. If analytics show numbers with no audit rows behind them, report that as a failure.

### 7.5 Self-healing preview

```sh
curl -X POST http://localhost:8080/api/v1/heal/simulate-break \
  -H "Authorization: Bearer <TOKEN>" -H 'Content-Type: application/json' \
  -d '{"field":"email"}'
# EXPECT: a proposal is created, NO table is changed.
curl -X POST http://localhost:8080/api/v1/heal/<PROPOSAL_ID>/approve \
  -H "Authorization: Bearer <TOKEN>"
# EXPECT: status=approved, applied=false. Apply the diff manually only if asked.
# Real detection: POST /api/v1/heal/detect checks information_schema for missing client columns.
```

### 7.6 MCP (stdio, needs a real JWT)

```sh
GATEWAY_URL=http://localhost:8080 MCP_TOKEN=<TOKEN> MCP_COMPANY_ID=<COMPANY_ID> \
  docker compose run --rm -i mcp
# Then send JSON-RPC lines:
# {"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
# {"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
# {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"view_audit_log","arguments":{}}}
```

EXPECT: `tools/list` returns all 4 tools: create_app, deploy_app, check_permissions, view_audit_log. No tool may run raw SQL or return a secret.

### 7.7 Persistence

```sh
docker compose down
docker compose up -d
# EXPECT: data from sections 7.1–7.2 is still there.
# WARNING: docker compose down -v WIPES the pgdata volume. Do not run it unless Dia explicitly asks.
```

## 8. What was already validated (before this machine test)

- `go vet ./...` clean, `go test ./...` passing (policy default-deny, RLS SQL generation, auth/JWT, share hashing, heal column detection)
- Dashboard `npm run build` passed (Next 15.5.9, 4 pages, ~104 kB first load)
- Sample app `npm run build` passed (Next 15.5.9, 4 pages)
- MCP binary: `initialize` + `tools/list` return the 4 tools over stdio
- `docker-compose.yml` parses as YAML; no real secrets in files (placeholders only)
- `node_modules/` and `.next/` were removed after building; reinstall with `npm install` before running outside Docker

## 9. What was NOT validated yet — this brief exists to close these gaps

Docker was NOT installed on the original workspace VM (`docker: command not found`), so none of the following had been run there: `docker compose config`, `docker compose up --build -d`, any image build, any container start, live gateway against Postgres, `prove-moat.sh` end to end, RLS psql proof, dashboard/sample-app against a live gateway, or MCP against a live gateway. Those are exactly what this test must cover.

## 10. Report the agent must return

Return, in this format, with no invented results:
1. Machine + Docker version (`docker --version`, `docker compose version`)
2. Each step 6, 7.1–7.7: PASS / FAIL, with the exact command, HTTP code, and key output
3. Full `prove-moat.sh` output, verbatim
4. Any failures, with full logs (`docker compose logs --tail=200 <service>`) and the exact file/line you think caused it
5. Dashboard analytics vs audit rows: do they match? Yes/No, with numbers
6. What is still stubbed (deploy_app, heal apply, billing, SSO, private data plane) — confirm you did NOT present any of these as working
7. Next fix, if any: the single most important bug or gap to fix before a pitch demo, in one sentence

## 11. Troubleshooting notes

- Ports 3000/3001/8080 already in use -> stop the other service, or report the conflict; do not silently remap ports without noting it in the report.
- Postgres not healthy -> check `docker compose logs postgres`, check `.env` passwords match between services.
- Gateway 500 on first start -> check `docker compose logs gateway`, check DATABASE_URL / APP_DATABASE_URL, check migrations applied.
- Next.js locally warns about a stray `/home/hatch/package-lock.json` outside the project as workspace root — in Docker the build context is just the app folder, so this does not affect the Docker build.
- Dashboard/sample-app tokens are stored in `localStorage` for beta convenience only — move to httpOnly cookies before any shared use. Report this as a known beta limitation, not as a production design.
