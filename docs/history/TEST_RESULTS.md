# Cloud Beta — Machine Test Results

## 1. Machine test reported 2026-10-28 (Dia's machine, using AGENT_TEST_BRIEF.md)

All sections 7.1–7.7 PASSED. This record is the user's report, not a test
run on this workspace VM (Docker is not installed here — see section 4).

- **7.1 Register, create app, deploy:** passed — 201 / 201 / 200.
- **7.2 prove-moat.sh:** passed — all four proofs returned 403 and the
  script ended with `MOAT PROVED: …`.
- **7.3 RLS as cloud_app:** passed — a plain SELECT with no WHERE sees
  only the current company's rows, and no company set means 0 rows.
  A cross-company INSERT is rejected by RLS, and the gateway was
  confirmed to connect as cloud_app, not as a superuser.
- **7.4 Dashboard and sample app:** passed — analytics match the audit
  rows exactly for all three companies, and the sample app only talks
  to the gateway on :8080. A test client called "Atlas Test Client"
  was added through the sample app; a client called "hfhgfhg" was
  already there from an earlier session (not created by this test).
- **7.5 Self-healing:** passed — a proposal is created, the table is
  unchanged, and approval gives approved with applied=false.
- **7.6 MCP:** passed — it lists the 4 tools, returns real audit rows,
  refuses unknown tools like run_sql, and leaks no secrets.
- **7.7 Persistence:** passed — all data survived down/up.

## 2. Problems found in that test (must be fixed before a pitch demo)

1. **CRITICAL — Silent RLS fallback.** If the cloud_app connection
   fails, `db.go` quietly switched to the owner pool, which is a
   superuser, so RLS stopped applying with no error. prove-moat.sh
   would still print MOAT PROVED, because proofs 1–3 are enforced by
   the Go code, not by Postgres.
2. **Heal endpoints ignored policy.** An accountant could create heal
   proposals, and approving a proposal that does not exist still
   returned 200 "approved".
3. **RLS isolated companies, not roles.** "Accountant is read-only"
   and "employee can't delete" were checked only in Go, so the
   "even if the app code forgets" pitch held for cross-company access
   only.
4. **Targeted company could not see attacks.** A cross-company denial
   was logged only under the attacker's company, so the target company
   never saw it in its audit view.

Deploy (per-app containers), heal apply, billing, and SSO were still
stubbed at test time, and were NOT presented as working.

## 3. Fixes applied after that test (in this folder)

1. **Silent RLS fallback removed — fail hard at startup**
   (`gateway/internal/db/db.go`, Connect). No fallback to the owner
   pool for tenant data, ever. Startup now fails (gateway does not
   start) if APP_DATABASE_URL is empty, equals DATABASE_URL, or its
   Ping fails; if the app pool user is a superuser or is not
   cloud_app; or if RLS is not enabled on clients, audit_logs,
   shares, heal_proposals, and apps. Startup logs the app pool user
   (cloud_app, non-superuser) and that RLS is on.
2. **Heal endpoints enforce policy, approve is exact**
   (`gateway/cmd/gateway/main.go`). handleHealDetect and
   handleHealSimulate now require
   `policy.Allowed("heal_proposals", "write", role)` — owner-only per
   policy.json — otherwise 403 + denied audit row and nothing is
   created. handleHealApprove now updates only where
   `status='pending'` (scoped to the caller's company via the tenant
   transaction) and checks RowsAffected: 0 rows returns 404
   (proposal not found or already decided) with a denied audit row,
   never a fake 200 "approved". Applied stays false always in beta.
3. **RLS is now role-aware** (`gateway/internal/policy/policy.go`,
   RLS()). For each table in policy.json, one policy per operation
   now requires BOTH tenant match AND role in that operation's
   allowed list, using the already-set `app.current_role` session
   variable. Example: clients SELECT allows owner/employee/accountant
   of that company; INSERT allows owner/employee only; DELETE allows
   owner only. An empty allowed list denies that operation outright
   (false) — e.g. audit_logs INSERT/DELETE. Memberships and users
   special SELECT policies are unchanged, and companies keeps its
   `id` tenant-field handling. Policy tests now assert role
   conditions are present and that an accountant INSERT/DELETE
   policy expression does not include accountant.
4. **Cross-tenant denials are mirrored to the target company**
   (`gateway/internal/db/db.go`, Audit). When a denial has a
   targetCompany different from the actor's company, a second audit
   row is written with company_id = targetCompany, same actor/action/
   table/target id, allowed=false, reason prefixed
   "cross-tenant denied: attacker attempted access". The mirror row
   contains NO target row content, only ids and the denial reason.
   The attacker company still gets its own original row. By design,
   a cross-tenant denial therefore appears in both companies' audit
   views, and audit summary totals summed across companies
   double-count one attack; per-company totals remain correct.

Regression checks added to `scripts/prove-moat.sh` (existing proofs
unchanged): (a) startup fail-hard on owner/empty APP_DATABASE_URL —
scripted via `docker compose run` when docker is available, otherwise
printed as a MANUAL check that must be observed before Fix 1 is
called live-tested; (b) accountant heal detect/simulate -> 403 and
no proposal created; (c) approve non-existent heal proposal -> 404,
not 200; (d) target company audit view contains the mirrored
cross-tenant denial row after proof 1.

## 4. Validation on this workspace VM (after the fixes)

- `go vet ./...` clean.
- `go test -count=1 ./...` passing (gateway, auth, policy; mcp/db
  have no test files).
- `sh -n scripts/prove-moat.sh` syntax OK.
- Docker is NOT installed on this VM (`docker: command not found`),
  so live `docker compose` / prove-moat end-to-end re-testing was
  NOT possible here. The fixes are code-complete and unit-tested,
  but they are NOT live-tested until the re-test below runs.

## 5. Re-test in order, on a Docker machine

1. `cd cloud-beta && cp .env.example .env` (fill placeholders locally)
2. `docker compose config` — no error
3. `docker compose up --build -d`, then `curl http://localhost:8080/health`
4. `sh scripts/prove-moat.sh` — original 4 proofs PLUS sections
   5 (accountant heal 403, no proposal), 6 (approve-missing 404),
   7 (target audit mirror), and 5a (startup fail-hard, scripted if
   docker is available, otherwise do the two MANUAL runs printed by
   the script and record their output)
5. RLS psql proof as cloud_app: plain SELECT sees only the current
   company; also try, as cloud_app with role accountant set, an
   INSERT into clients — RLS must now deny it at the database layer,
   not just in Go
6. Dashboard :3000 and sample app :3001 with a real login; check
   analytics match audit rows (note: a cross-tenant denial now
   counts once in each of the two companies' views, by design)
7. `docker compose down` / `up -d` persistence check
   (do NOT run `down -v` unless data loss is intended)

## 6. Still stubbed at that point (superseded by MVP v1 below)

Per-app container deployment, heal apply (approval is preview only,
applied=false), billing, SSO/WorkOS, private data plane. Heal
proposals themselves are control-plane objects written via the owner
pool, so they are enforced in Go with the owner-only check above,
not by RLS — see README for the honest pitch claim.

## 7. MVP v1 — real per-app deploy + heal apply (2026-10-28)

Code-complete and unit-tested on this VM; NOT live-tested (Docker
not installed here, no container was started). The demo artifact
was not touched.

**Now REAL:**
- Per-app deploy: gateway `internal/deploy` (Docker API over
  `DOCKER_HOST`, default the Docker socket) + `POST
  /api/v1/apps/{id}/deploy` starts a separate container per app
  from the `cloudbeta-sample-app` image, host port 3100–3199, own
  URL, status deploying → running only after a live 2xx health
  check, failed on start/health failure (502) or no Docker runtime
  (503, never a fake deployed status). Stop, delete (owner only,
  container removed, row deleted via tenant tx), and health
  (healthy only from a live check) are real. `apps` gains
  `container_id` + `port` (migration + startup ALTER, so existing
  DBs get them too). Compose: gateway mounts `/var/run/docker.sock`
  with `DEPLOY_IMAGE=cloudbeta-sample-app`; sample-app is tagged
  `cloudbeta-sample-app` so the gateway can reference it.
- MCP `deploy_app` is real (same gateway endpoint); description
  updated, no other MCP change, no raw SQL, no secrets.
- Dashboard (glass, kept): create/deploy/open/stop/delete apps,
  permissions view from `/api/v1/policy`, shares create/list/revoke,
  heal Approve + Apply — all from real gateway responses.
- Heal Apply: `POST /api/v1/heal/{id}/apply`, owner only, approved
  only, whitelisted `add_missing_columns` on clients only
  (email/notes/name/company_id TEXT, created_at TIMESTAMPTZ;
  `id` never applyable). Verifies the column in
  `information_schema`, then status=applied, applied=true, audited
  with before/after. Pending → 409, simulated/unknown → 400 with
  nothing changed. Approval alone still never applies.
- Shares list: `GET /api/v1/shares`, own company only, never returns
  token hashes or raw tokens.
- New script `scripts/prove-deploy.sh`: two apps on different
  ports both 200, stop, delete, health, heal-apply preview-only
  refusal; prints SKIP + exit 0 (not a pass) if Docker is
  unavailable. `scripts/prove-moat.sh` was NOT edited.

**Validation on this VM (coordinator re-ran, not just the builders):**
- `go vet ./...` clean, `go test -count=1 ./...` passing
  (gateway incl. new deploy + heal-whitelist tests, auth, policy,
  deploy; mcp/db no test files), `gofmt -l` clean
- Dashboard `npm run build` passed, Next 15.5.9, `/` 5.8 kB /
  108 kB first load; sample-app build passed; `node_modules`/`.next`
  removed after both builds
- Compose YAML parses (postgres, gateway, dashboard, sample-app,
  mcp; gateway socket mount + sample-app image confirmed)
- `sh -n scripts/prove-moat.sh` and `sh -n scripts/prove-deploy.sh`
  both OK; only placeholder secrets in files (change-me / dev
  defaults, as before)

**Docker-socket caveat (local MVP only):** the gateway holding
`/var/run/docker.sock` could control the host Docker daemon if
compromised — acceptable on a throwaway demo machine, NOT
production multi-tenant isolation. Production needs a separate
orchestrator / per-tenant runtime, not the socket.

**Re-test in order, on a Docker machine (this replaces section 5
as the current checklist):**
1. `cd cloud-beta && cp .env.example .env` (fill placeholders)
2. `docker compose config` — no error
3. `docker compose up --build -d`, then
   `curl http://localhost:8080/health`
4. `sh scripts/prove-deploy.sh` — second app gets its own URL/port
   and works independently, stop/delete/health, heal-apply
   preview-only refusal (SKIP only if Docker is unavailable — that
   is not a pass)
5. `sh scripts/prove-moat.sh` — original 4 proofs + sections 5, 6,
   7, 5a (no regressions in fail-hard RLS, owner-only heal,
   role-aware RLS, mirrored denials)
6. MCP `deploy_app` against the live gateway — running app, own
   port/URL
7. Heal apply on a REAL missing column (throwaway DB only): drop a
   clients column, detect → approve → apply, verify column returns
   and proposal is applied=true
8. Dashboard :3000 with a real login — analytics match audit rows,
   app management, shares, heal Apply in the UI; open a deployed
   app URL in the browser
9. RLS psql proof as cloud_app (+ accountant INSERT denial at the
   DB layer) and `down` / `up -d` persistence (`down -v` wipes data)

**Still deferred in MVP v1 (do not present as working):**
Git deploy (non-template repo → 501, never faked as deployed),
billing, SSO/WorkOS, private data plane, Firecracker/Kubernetes/
Temporal, production multi-tenant isolation for the Docker socket.
Heal Apply covers one safe fix class only; arbitrary heal fixes
stay preview-only.
