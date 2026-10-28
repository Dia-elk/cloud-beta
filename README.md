# Cloud Beta — real, tryable scaffold

**Not a demo artifact. Not production. Nothing bought or published.**

This is a working beta of the Cloud for Small Software pitch:
**“From shadow app to governed app in one click.”**
An AI-built app runs through a permission gateway. The app code is
untrusted. Postgres Row Level Security, generated from a policy file,
enforces who sees which rows — even if the app code forgets the filter.

## Stack (beta, chosen deliberately)

```
User / AI agent (Cursor, Claude, …)
        │  JWT + MCP (no SQL, no secrets)
        ▼
┌──────────────────────────────┐
│  Gateway — Go 1.23, stdlib   │  control plane + permission gateway
│  policy.json → RLS SQL      │  JWT + bcrypt auth, audit log
└──────────────┬───────────────┘
               │  tenant session: app.current_company_id
               │  app.current_user_id, app.current_role
               ▼
┌──────────────────────────────┐
│  PostgreSQL 16                │  RLS ON for tenant tables
│  role cloud_app (non-owner)  │  owner pool only for migrations,
└──────────────┬───────────────┘  auth, audit writes, share lookup
               │
        ┌──────┴──────┐
        ▼             ▼
  Dashboard        Sample app
  Next.js 15       Next.js 15
  TS + Tailwind    client tracker
  dark Laravel /   NO database password,
  Nightwatch       NO direct DB access
  small analytics  talks to gateway only
  from real audit
  rows only
```

**Explicitly OUT for beta (later, not now):** Firecracker microVMs,
Kubernetes, Temporal. The app runtime in beta is Docker containers in
`docker-compose.yml`. **Later:** WorkOS/SSO (own JWT+bcrypt first).

## Security fixes — 2026-10-28, after the machine test (see TEST_RESULTS.md)

The machine test passed sections 7.1–7.7, but found 4 problems that
had to be fixed before a pitch demo. All 4 are fixed in code here:

1. **Silent RLS fallback removed (CRITICAL).** The gateway used to
   quietly fall back to the owner/superuser pool if the cloud_app
   connection failed, so RLS stopped applying with no error — while
   Go-layer proofs still passed. Now startup FAILS and the gateway
   does not start if APP_DATABASE_URL is empty, equals DATABASE_URL,
   or its Ping fails; if the app pool user is a superuser or is not
   cloud_app; or if RLS is not enabled on clients, audit_logs,
   shares, heal_proposals, and apps. Startup logs the cloud_app
   non-superuser user and that RLS is on. Tenant data is never read
   via the owner pool.
2. **Heal endpoints enforce policy.** Detect and simulate now require
   owner-only write on heal_proposals (403 + denied audit row for
   anyone else, nothing created). Approve now updates only pending
   proposals in the caller's company and returns 404 if none match,
   never a fake 200 "approved". Applied stays false in beta.
3. **RLS is now role-aware.** Each policy now requires tenant match
   AND role in that operation's allowed list (from policy.json, via
   `app.current_role`). An empty list denies outright.
4. **Cross-tenant denials are mirrored to the target.** A denial with
   a different target company now writes a second audit row under
   the target company (ids + denial reason only, no row content), so
   the target sees the attack. The attacker still gets its own row,
   so one attack appears in both views by design — audit totals
   summed across companies double-count it; per-company totals are
   correct.

**Honest pitch claim, after these fixes:** "even if the app code
forgets" now holds for cross-company AND role access, via RLS — a
caller talking to Postgres directly as cloud_app with role
accountant still cannot INSERT/DELETE clients, because RLS denies
it. EXCEPTION, stated plainly: heal proposals are control-plane
objects written via the owner pool, so they are NOT protected by
RLS; they are enforced in Go with the owner-only check above.
That is the one place where the claim rests on gateway code, not
on Postgres.

## MVP v1 — what is real now (2026-10-28)

This is no longer the stubbed beta. MVP v1 is the version Dia asked
for: well designed (glass dashboard, kept) and well developed (real
deploy, real heal apply for safe fixes). The demo artifact is set
aside; this folder is the product.

**Real in MVP v1 (new in this pass):**
- **Real per-app deploy:** `POST /api/v1/apps/{id}/deploy` starts a
  separate Docker container per app from the `cloudbeta-sample-app`
  image (template = Client Tracker, cloned per app), with its own
  host port in 3100–3199 and its own URL
  (`http://localhost:<port>`). Status goes
  registered → deploying → running (only after a live HTTP health
  check returns 2xx) or failed (never a fake URL). Two apps get two
  different ports and work independently.
- **App lifecycle:** stop (`POST /api/v1/apps/{id}/stop`, status
  stopped), delete (`DELETE /api/v1/apps/{id}`, owner only, container
  stopped + removed, row deleted via the tenant transaction), and
  health (`GET /api/v1/apps/{id}/health`, healthy only from a live
  HTTP check, never claimed without one). List apps returns url,
  status, port, and container_id.
- **MCP deploy is real:** `deploy_app` calls the same gateway deploy
  endpoint, so it now starts a real per-app container. `create_app`,
  `check_permissions`, `view_audit_log` unchanged, no raw SQL, no
  secrets.
- **Dashboard app management (glass, kept):** create app, deploy,
  open app, stop, delete, status/health, permissions view from
  `/api/v1/policy`, shares with expiry + revoke, live audit from
  real rows, heal Approve + Apply — all from real gateway responses.
- **Heal Apply, safe fixes only:** `POST /api/v1/heal/{id}/apply`
  applies an APPROVED proposal only, owner only, and only for the
  whitelisted fix `add_missing_columns` on the clients table
  (email / notes / name / company_id as TEXT, created_at as
  TIMESTAMPTZ; `id` is never applyable). It runs
  `ALTER TABLE clients ADD COLUMN IF NOT EXISTS <whitelisted column>
  <whitelisted definition>`, verifies the column exists in
  `information_schema`, then sets status=applied, applied=true, and
  audits before/after. Pending → 409 "approve first"; simulated or
  unknown fixes → 400 "preview only, not safely applyable" and
  NOTHING changes. Approval alone still never applies anything.
- **Shares list:** `GET /api/v1/shares` (own company only) returns
  id, table, row, expiry, revoked — never token hashes or raw tokens.

**Still real from the beta (unchanged, no regressions):**
- Register/login, workspaces (companies), memberships with roles
  owner / employee / accountant
- Policy file (`gateway/policy.json`): tenant field `company_id`,
  source `jwt.company_id`, default **deny**, per-table read/write/delete
- Gateway role checks **plus** Postgres RLS generated from that file,
  applied on every tenant transaction via `SET LOCAL` session variables
- Audit log in Postgres for every allowed/denied data action, with
  latency; dashboard analytics read those real rows only
- Shares: token hash stored (never the raw token), single-row read,
  expiry, revoke
- Self-healing **preview** for unsafe/simulated breaks (see Apply
  limits above)
- MCP server (Go, JSON-RPC over stdio): `create_app`, `deploy_app`,
  `check_permissions`, `view_audit_log` — all via gateway HTTP, no SQL,
  no database password

**Docker-socket caveat — say this plainly, it is a local MVP
tradeoff, not production isolation:** the gateway container mounts
`/var/run/docker.sock` so it can start per-app containers. A
compromised gateway could therefore control the host Docker daemon.
This is acceptable for a local MVP / pitch demo on a throwaway
machine, and NOT acceptable as production multi-tenant isolation.
Production needs a separate orchestrator / per-tenant runtime, not
the gateway holding the Docker socket.

**Stubbed / deferred in MVP v1 (say this plainly in a pitch):**
- **Git deploy: NOT in MVP v1.** Any non-template repo on deploy
  returns 501 and the app is never marked deployed. This is the
  explicit next step: clone a repo with a Dockerfile, build it, run
  it as a per-app container — built safely, not faked. Template +
  MCP deploy are fully real in the meantime.
- Sample app is one template, not an arbitrary repo build.
- Heal Apply covers one safe fix class only (add a missing clients
  column). Arbitrary heal fixes stay preview-only, by design.
- No billing, no SSO/WorkOS, no private data plane, no
  Firecracker/Kubernetes/Temporal yet.

## Try it locally (needs Docker — see validation note below)

```sh
cd cloud-beta
cp .env.example .env
# Fill in the passwords/JWT secret in .env (placeholders only in .env.example)
docker compose up --build -d
curl http://localhost:8080/health
# Dashboard:  http://localhost:3000
# Sample app: http://localhost:3001
```

### 1. Register and deploy the sample app
In the dashboard, register (e.g. `owner@example.ma` /
`beta-password-1` / `Beta Company`), or with curl:

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

### 2. Prove the moat (automated)

```sh
sh scripts/prove-moat.sh
```

It proves, against a running gateway:
1. Company B reading a Company A client → **403 + denied audit row**
2. Accountant creating a client → **403**
3. Employee deleting a client → **403** (client still exists after)
4. A share with a 2-second expiry works once, then → **403**
   (single row only, never a list)
5. Accountant heal detect/simulate → **403**, no proposal created
6. Approving a non-existent heal proposal → **404**, not 200
7. Target company audit view contains the mirrored cross-tenant
   denial row from proof 1
5a. Gateway startup fails on owner/empty APP_DATABASE_URL —
   scripted via `docker compose run` when docker is available,
   otherwise printed as a MANUAL check (do not call Fix 1
   live-tested until that run is observed)

### 3. RLS proof, directly in Postgres (optional, strong demo)

```sh
docker compose exec postgres psql -U cloud_app -d cloudbeta \
  -c "SELECT set_config('app.current_company_id','<COMPANY_A>',true); SELECT id,name FROM clients;"
# As cloud_app, with Company A set, you only see Company A rows —
# even with a plain SELECT *, no WHERE clause. That is the moat.
```

### 4. Self-healing preview

```sh
# Simulated break (does not change any table):
curl -X POST http://localhost:8080/api/v1/heal/simulate-break \
  -H "Authorization: Bearer <TOKEN>" -H 'Content-Type: application/json' \
  -d '{"field":"email"}'
# See it in the dashboard, then approve it:
curl -X POST http://localhost:8080/api/v1/heal/<PROPOSAL_ID>/approve \
  -H "Authorization: Bearer <TOKEN>"
# Result: status=approved, applied=false. Apply the diff manually.
# Real detection: POST /api/v1/heal/detect checks information_schema
# for missing client columns and proposes a restore diff if any are gone.
```

### 5. MCP (stdio, needs a real JWT)

```sh
# Get a token via login first, then:
GATEWAY_URL=http://localhost:8080 MCP_TOKEN=<TOKEN> MCP_COMPANY_ID=<COMPANY_ID> \
  docker compose run --rm -i mcp
# Then send JSON-RPC lines, e.g.:
# {"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
# {"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
# {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"view_audit_log","arguments":{}}}
```

## Files

- `gateway/` — Go gateway + MCP, `policy.json`, `migrations/001_init.sql`
- `dashboard/` — Next.js 15 console, Tailwind, dark Laravel/Nightwatch style
- `sample-app/` — Next.js 15 client tracker, gateway-only, no DB password
- `docker-compose.yml` — postgres + gateway + dashboard + sample-app + mcp
- `scripts/prove-moat.sh` — end-to-end moat proof
- `postgres/init-app-role.sh` — creates the non-owner `cloud_app` role

## What was validated here, and what was NOT

**Validated on this workspace VM:**
- `go vet ./...` clean, `go test -count=1 ./...` passing (policy default-deny,
  RLS SQL generation, auth/JWT, share hashing, heal column detection,
  plus new role-aware RLS tests: accountant INSERT/DELETE policy
  expressions do not include accountant, empty-list operations deny)
- Dashboard `npm run build` passed (Next 15.5.9, 4 pages, ~104 kB first load)
- Sample app `npm run build` passed (Next 15.5.9, 4 pages)
- MCP binary: `initialize` + `tools/list` return the 4 tools over stdio
- `docker-compose.yml` parses as YAML (services: postgres, gateway,
  dashboard, sample-app, mcp); no secrets in any file (placeholders only)

**NOT validated here — Docker is not installed on this VM
(`docker: command not found`), so this could not be run:**
- `docker compose up --build -d` — no images built, no containers started
- The live gateway against Postgres, `scripts/prove-moat.sh` end to end,
  the RLS psql proof, dashboard/sample-app against a live gateway,
  and MCP against a live gateway

**Must be tested, in order, on a machine with Docker (MVP v1
re-test — full list in TEST_RESULTS.md section 7):**
1. `docker compose config` with a real `.env`
2. `docker compose up --build -d`, then `curl http://localhost:8080/health`
3. `sh scripts/prove-deploy.sh` — create + deploy a second app and
   confirm it gets its own URL/port and works independently, stop,
   delete, health, and heal-apply preview-only refusal (script
   prints SKIP, exit 0, if the Docker runtime is unavailable —
   that is not a pass)
4. `sh scripts/prove-moat.sh` — original four proofs PLUS sections
   5 (accountant heal 403, no proposal), 6 (approve-missing 404),
   7 (target audit mirror), and 5a (startup fail-hard, scripted if
   docker is available, otherwise the two MANUAL runs it prints)
5. MCP deploy: `deploy_app` via the MCP server against the live
   gateway must return a running app with its own port/URL
6. Heal apply on a real missing column (throwaway DB only): drop a
   clients column, detect → approve → apply, verify the column
   returns and the proposal is applied=true
7. Dashboard at http://localhost:3000 with a real login, then check
   dashboard analytics match the audit rows (note: a cross-tenant
   denial now counts once in each of the two companies' views, by
   design), app management, shares, and heal Apply in the UI
8. The RLS psql proof above — plus, as cloud_app with role accountant
   set, an INSERT into clients, which RLS must now deny at the
   database layer, not just in Go — and `docker compose down` / `up -d`
   to confirm Postgres data persists (note: `down -v` wipes it)

**Local-only warnings:** Next.js locally warns about a stray
`/home/hatch/package-lock.json` outside the project as workspace root;
in Docker the build context is just the app folder, so this does not
affect the Docker build. Dashboard/sample-app tokens are stored in
`localStorage` for beta convenience only — move to httpOnly cookies
before any shared use.
