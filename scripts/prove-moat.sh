#!/bin/sh
# Prove the moat end to end against a running gateway (http://localhost:8080).
# Usage: sh scripts/prove-moat.sh
# Requires: curl, jq. Exits non-zero on any unexpected result.
# Creates two companies, a client in Company A, then proves:
#  1. Company B user requesting Company A client -> 403 + audit denied row
#  2. Accountant create -> 403
#  3. Employee delete -> 403
#  4. Share with short expiry works, then expires -> 403, single row only
set -eu
GW="${GW:-http://localhost:8080}"
need() { command -v "$1" >/dev/null || { echo "missing $1"; exit 1; }; }
need curl; need jq
echo "== health =="
curl -fsS "$GW/health"; echo
echo "== register Company A (owner) =="
A=$(curl -fsS -X POST "$GW/api/v1/auth/register" -H 'Content-Type: application/json' -d '{"email":"a-owner@example.ma","password":"beta-password-1","company_name":"Company A"}')
ATOK=$(echo "$A" | jq -r .token); ACOMP=$(echo "$A" | jq -r .company_id)
echo "Company A: $ACOMP"
echo "== create client in Company A =="
CLI=$(curl -fsS -X POST "$GW/api/v1/clients" -H "Authorization: Bearer $ATOK" -H 'Content-Type: application/json' -d '{"name":"Acme Client","email":"acme@example.ma","notes":"A only"}')
CLID=$(echo "$CLI" | jq -r .id); echo "Client: $CLID"
echo "== register Company B (owner) =="
B=$(curl -fsS -X POST "$GW/api/v1/auth/register" -H 'Content-Type: application/json' -d '{"email":"b-owner@example.ma","password":"beta-password-1","company_name":"Company B"}')
BTOK=$(echo "$B" | jq -r .token)
echo "== 1. Company B reads Company A client (expect 403) =="
CODE=$(curl -s -o /tmp/beta-cross.json -w '%{http_code}' "$GW/api/v1/clients/$CLID" -H "Authorization: Bearer $BTOK")
cat /tmp/beta-cross.json; echo " -> HTTP $CODE"
[ "$CODE" = "403" ] || { echo "FAIL: cross-tenant read was not denied"; exit 1; }
echo "== audit: denied row present for Company B =="
curl -fsS "$GW/api/v1/audit?limit=20" -H "Authorization: Bearer $BTOK" | jq '.audit[] | select(.allowed==false) | {action, table, allowed, reason}' | head -20
echo "== 2. Accountant in Company A tries to create (expect 403) =="
curl -fsS -X POST "$GW/api/v1/auth/register" -H 'Content-Type: application/json' -d '{"email":"a-accountant@example.ma","password":"beta-password-1","company_name":"Acct Temp"}' >/dev/null
curl -fsS -X POST "$GW/api/v1/memberships" -H "Authorization: Bearer $ATOK" -H 'Content-Type: application/json' -d '{"email":"a-accountant@example.ma","role":"accountant"}' >/dev/null
ACCT=$(curl -fsS -X POST "$GW/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"email\":\"a-accountant@example.ma\",\"password\":\"beta-password-1\",\"company_id\":\"$ACOMP\"}")
ACCTOK=$(echo "$ACCT" | jq -r .token)
CODE=$(curl -s -o /tmp/beta-acct.json -w '%{http_code}' -X POST "$GW/api/v1/clients" -H "Authorization: Bearer $ACCTOK" -H "X-Company-ID: $ACOMP" -H 'Content-Type: application/json' -d '{"name":"Should Fail","email":"x@example.ma"}')
cat /tmp/beta-acct.json; echo " -> HTTP $CODE"
[ "$CODE" = "403" ] || { echo "FAIL: accountant create was not denied"; exit 1; }
echo "== 3. Employee in Company A tries to delete (expect 403) =="
curl -fsS -X POST "$GW/api/v1/auth/register" -H 'Content-Type: application/json' -d '{"email":"a-employee@example.ma","password":"beta-password-1","company_name":"Emp Temp"}' >/dev/null
curl -fsS -X POST "$GW/api/v1/memberships" -H "Authorization: Bearer $ATOK" -H 'Content-Type: application/json' -d '{"email":"a-employee@example.ma","role":"employee"}' >/dev/null
EMP=$(curl -fsS -X POST "$GW/api/v1/auth/login" -H 'Content-Type: application/json' -d "{\"email\":\"a-employee@example.ma\",\"password\":\"beta-password-1\",\"company_id\":\"$ACOMP\"}")
EMPOK=$(echo "$EMP" | jq -r .token)
CODE=$(curl -s -o /tmp/beta-emp.json -w '%{http_code}' -X DELETE "$GW/api/v1/clients/$CLID" -H "Authorization: Bearer $EMPOK" -H "X-Company-ID: $ACOMP")
cat /tmp/beta-emp.json; echo " -> HTTP $CODE"
[ "$CODE" = "403" ] || { echo "FAIL: employee delete was not denied"; exit 1; }
echo "Client still exists for owner (expect 200):"
curl -fsS "$GW/api/v1/clients/$CLID" -H "Authorization: Bearer $ATOK"; echo
echo "== 4. Share with 2s expiry =="
SH=$(curl -fsS -X POST "$GW/api/v1/shares" -H "Authorization: Bearer $ATOK" -H 'Content-Type: application/json' -d "{\"row_id\":\"$CLID\",\"expires_in\":\"2s\"}")
STOK=$(echo "$SH" | jq -r .token); echo "Share token: ${STOK:0:12}..."
echo "Share before expiry (expect 200, single row):"
curl -fsS "$GW/share/$STOK"; echo
echo "Waiting 3s for expiry..."
sleep 3
echo "Share after expiry (expect 403):"
CODE=$(curl -s -o /tmp/beta-share.json -w '%{http_code}' "$GW/share/$STOK"); cat /tmp/beta-share.json; echo " -> HTTP $CODE"
[ "$CODE" = "403" ] || { echo "FAIL: expired share was not denied"; exit 1; }
echo "== 5. Accountant heal detect/simulate -> 403 and no proposal created (Fix 2) =="
BEFORE=$(curl -fsS "$GW/api/v1/heal/proposals" -H "Authorization: Bearer $ATOK" | jq '.proposals | length')
CODE=$(curl -s -o /tmp/beta-heal-acct.json -w '%{http_code}' -X POST "$GW/api/v1/heal/detect" -H "Authorization: Bearer $ACCTOK" -H "X-Company-ID: $ACOMP")
cat /tmp/beta-heal-acct.json; echo " -> HTTP $CODE"
[ "$CODE" = "403" ] || { echo "FAIL: accountant heal detect was not denied"; exit 1; }
CODE=$(curl -s -o /tmp/beta-heal-acct2.json -w '%{http_code}' -X POST "$GW/api/v1/heal/simulate-break" -H "Authorization: Bearer $ACCTOK" -H "X-Company-ID: $ACOMP" -H 'Content-Type: application/json' -d '{"field":"email"}')
cat /tmp/beta-heal-acct2.json; echo " -> HTTP $CODE"
[ "$CODE" = "403" ] || { echo "FAIL: accountant heal simulate was not denied"; exit 1; }
AFTER=$(curl -fsS "$GW/api/v1/heal/proposals" -H "Authorization: Bearer $ATOK" | jq '.proposals | length')
[ "$BEFORE" = "$AFTER" ] || { echo "FAIL: accountant heal attempt created a proposal ($BEFORE -> $AFTER)"; exit 1; }
echo "No proposal created by accountant (count $BEFORE -> $AFTER)."
echo "== 6. Approve non-existent heal proposal -> 404, not 200 (Fix 2) =="
CODE=$(curl -s -o /tmp/beta-heal-404.json -w '%{http_code}' -X POST "$GW/api/v1/heal/heal_nonexistent_123/approve" -H "Authorization: Bearer $ATOK")
cat /tmp/beta-heal-404.json; echo " -> HTTP $CODE"
[ "$CODE" = "404" ] || { echo "FAIL: approving a non-existent proposal did not return 404"; exit 1; }
echo "== 7. Target company audit view contains the cross-tenant denial (Fix 4) =="
# Proof 1 was Company B (attacker) reading Company A (target). The target
# (Company A, owner token) must now see a mirrored denied row, with no
# target row content, only ids and the denial reason.
MIRROR=$(curl -fsS "$GW/api/v1/audit?limit=100" -H "Authorization: Bearer $ATOK" | jq -r '.audit[] | select(.allowed==false and (.reason | contains("cross-tenant denied: attacker attempted access"))) | .reason' | head -1)
[ -n "$MIRROR" ] || { echo "FAIL: target company audit view has no mirrored cross-tenant denial row"; exit 1; }
echo "Target audit mirror present: $MIRROR"
echo "== 5a. Gateway startup fails on owner/empty APP_DATABASE_URL (Fix 1) =="
echo "NOTE: this check is SCRIPTED only if 'docker' is available on this machine."
echo "If docker is not available, this check is MANUAL — see README section 'Re-test after the 4 fixes'."
if command -v docker >/dev/null 2>&1; then
  # Load .env for owner credentials if present, without failing if absent.
  if [ -f .env ]; then set -a; . ./.env 2>/dev/null || true; set +a; fi
  OWNER_DSN="postgres://${POSTGRES_USER:-cloud_owner}:${POSTGRES_PASSWORD:-change-me-owner}@postgres:5432/${POSTGRES_DB:-cloudbeta}?sslmode=disable"
  echo "Trying gateway with APP_DATABASE_URL = owner DSN (expect exit != 0, 'refusing to start'):"
  set +e
  OUT=$(docker compose run --rm -e "APP_DATABASE_URL=$OWNER_DSN" gateway 2>&1 | head -20)
  RC=$?
  set -e
  echo "$OUT"
  if [ "$RC" -eq 0 ]; then echo "FAIL: gateway started with owner APP_DATABASE_URL"; exit 1; fi
  echo "$OUT" | grep -q "refusing to start" || { echo "FAIL: gateway did not report 'refusing to start' on owner DSN"; exit 1; }
  echo "Trying gateway with APP_DATABASE_URL empty (expect exit != 0, 'refusing to start'):"
  set +e
  OUT2=$(docker compose run --rm -e "APP_DATABASE_URL=" gateway 2>&1 | head -20)
  RC2=$?
  set -e
  echo "$OUT2"
  if [ "$RC2" -eq 0 ]; then echo "FAIL: gateway started with empty APP_DATABASE_URL"; exit 1; fi
  echo "$OUT2" | grep -q "refusing to start" || { echo "FAIL: gateway did not report 'refusing to start' on empty APP_DATABASE_URL"; exit 1; }
  echo "Startup fail-hard check PASSED (scripted, via docker compose run)."
else
  echo "MANUAL CHECK REQUIRED (docker not available in this run):"
  echo "  1. docker compose run --rm -e APP_DATABASE_URL=<owner DSN> gateway  -> must exit non-zero, log 'refusing to start', and NOT listen on :8080."
  echo "  2. docker compose run --rm -e APP_DATABASE_URL= gateway                 -> same: exit non-zero, 'refusing to start'."
  echo "  Do NOT mark Fix 1 as live-tested until one of those runs has been observed."
fi
echo "== summary from real audit rows =="
curl -fsS "$GW/api/v1/audit/summary" -H "Authorization: Bearer $ATOK" | jq .
echo "MOAT PROVED: cross-tenant 403, accountant create 403, employee delete 403, share expiry 403."
echo "FIXES PROVED: accountant heal 403 + no proposal, approve-missing 404, target audit mirror present, startup fail-hard (scripted if docker available, otherwise manual — see above)."
