#!/bin/sh
# Prove real per-app deploy end to end against a running gateway (http://localhost:8080).
# Usage: sh scripts/prove-deploy.sh
# Requires: curl, jq. Exits non-zero on any unexpected result.
# Steps: register -> create app -> deploy (expect running + http://localhost:31xx
# in 3100-3199 + app URL returns 200) -> second app gets a DIFFERENT port and
# also returns 200 -> stop first (stopped) -> delete second (gone from list)
# -> heal simulate -> approve -> apply must return 400 preview-only, nothing changed.
# If deploy returns 503 docker-unavailable, this prints SKIP and exits 0:
# the docker runtime is not available on this machine, so live deploy proof
# must happen on a Docker machine. That is NOT a success claim and NOT a
# moat failure (prove-moat.sh is unaffected).
# A detect-style apply (real missing column) is documented as MANUAL only:
# this script never drops a real column to manufacture one.
set -eu
GW="${GW:-http://localhost:8080}"
need() { command -v "$1" >/dev/null || { echo "missing $1"; exit 1; }; }
need curl; need jq
TS=$(date +%s)
echo "== health =="
curl -fsS "$GW/health"; echo
echo "== register deploy owner =="
REG=$(curl -fsS -X POST "$GW/api/v1/auth/register" -H 'Content-Type: application/json' -d "{\"email\":\"deploy-owner-$TS@example.ma\",\"password\":\"beta-password-1\",\"company_name\":\"Deploy Co $TS\"}")
TOK=$(echo "$REG" | jq -r .token)
echo "== create app 1 (template) =="
APP1=$(curl -fsS -X POST "$GW/api/v1/apps" -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"name":"tracker-one","repo":"sample-app"}')
APP1ID=$(echo "$APP1" | jq -r .id); echo "App 1: $APP1ID"
echo "== deploy app 1 =="
CODE=$(curl -s -o /tmp/beta-deploy1.json -w '%{http_code}' -X POST "$GW/api/v1/apps/$APP1ID/deploy" -H "Authorization: Bearer $TOK")
cat /tmp/beta-deploy1.json; echo " -> HTTP $CODE"
if [ "$CODE" = "503" ]; then
  echo "SKIP: docker runtime not available (deploy returned 503 docker-unavailable). Live deploy proof must run on a Docker machine."
  exit 0
fi
[ "$CODE" = "200" ] || { echo "FAIL: deploy app 1 did not return 200"; exit 1; }
STATUS=$(jq -r .status /tmp/beta-deploy1.json)
URL1=$(jq -r .url /tmp/beta-deploy1.json)
PORT1=$(jq -r .port /tmp/beta-deploy1.json)
[ "$STATUS" = "running" ] || { echo "FAIL: app 1 status is $STATUS, not running"; exit 1; }
echo "$URL1" | grep -Eq '^http://localhost:31[0-9][0-9]$' || { echo "FAIL: app 1 url $URL1 is not http://localhost:31xx"; exit 1; }
[ "$PORT1" -ge 3100 ] && [ "$PORT1" -le 3199 ] || { echo "FAIL: app 1 port $PORT1 outside 3100-3199"; exit 1; }
echo "App 1 running at $URL1 (port $PORT1)"
echo "== app 1 URL returns 200 =="
curl -fsS "$URL1" >/dev/null; echo "OK: $URL1 -> 200"
echo "== create + deploy app 2 (must get a DIFFERENT port) =="
APP2=$(curl -fsS -X POST "$GW/api/v1/apps" -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"name":"tracker-two","repo":"sample-app"}')
APP2ID=$(echo "$APP2" | jq -r .id); echo "App 2: $APP2ID"
CODE=$(curl -s -o /tmp/beta-deploy2.json -w '%{http_code}' -X POST "$GW/api/v1/apps/$APP2ID/deploy" -H "Authorization: Bearer $TOK")
cat /tmp/beta-deploy2.json; echo " -> HTTP $CODE"
[ "$CODE" = "200" ] || { echo "FAIL: deploy app 2 did not return 200"; exit 1; }
URL2=$(jq -r .url /tmp/beta-deploy2.json)
PORT2=$(jq -r .port /tmp/beta-deploy2.json)
[ "$PORT2" != "$PORT1" ] || { echo "FAIL: app 2 got the same port as app 1 ($PORT1)"; exit 1; }
[ "$URL2" != "$URL1" ] || { echo "FAIL: app 2 got the same URL as app 1"; exit 1; }
echo "App 2 running at $URL2 (port $PORT2)"
curl -fsS "$URL2" >/dev/null; echo "OK: $URL2 -> 200"
echo "== health endpoint for app 1 =="
curl -fsS "$GW/api/v1/apps/$APP1ID/health" -H "Authorization: Bearer $TOK" | jq -e '.healthy == true' >/dev/null; echo "OK: app 1 healthy=true"
echo "== stop app 1 (expect stopped) =="
STOP=$(curl -fsS -X POST "$GW/api/v1/apps/$APP1ID/stop" -H "Authorization: Bearer $TOK")
echo "$STOP"
echo "$STOP" | jq -e '.status == "stopped"' >/dev/null || { echo "FAIL: stop did not return stopped"; exit 1; }
echo "== delete app 2 (expect 200, then gone from list) =="
curl -fsS -X DELETE "$GW/api/v1/apps/$APP2ID" -H "Authorization: Bearer $TOK"; echo
LIST=$(curl -fsS "$GW/api/v1/apps" -H "Authorization: Bearer $TOK")
echo "$LIST" | jq -e --arg id "$APP2ID" '.apps | map(.id) | index($id) == null' >/dev/null || { echo "FAIL: deleted app 2 still lists"; exit 1; }
echo "OK: app 2 no longer lists"
echo "== heal: simulate -> approve -> apply must be 400 preview-only, nothing changed =="
SIM=$(curl -fsS -X POST "$GW/api/v1/heal/simulate-break" -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"field":"email"}')
HEALID=$(echo "$SIM" | jq -r .id); echo "Simulated proposal: $HEALID"
curl -fsS -X POST "$GW/api/v1/heal/$HEALID/approve" -H "Authorization: Bearer $TOK"; echo
CODE=$(curl -s -o /tmp/beta-heal-apply.json -w '%{http_code}' -X POST "$GW/api/v1/heal/$HEALID/apply" -H "Authorization: Bearer $TOK")
cat /tmp/beta-heal-apply.json; echo " -> HTTP $CODE"
[ "$CODE" = "400" ] || { echo "FAIL: applying a simulated proposal did not return 400"; exit 1; }
curl -fsS "$GW/api/v1/heal/proposals" -H "Authorization: Bearer $TOK" | jq -e --arg id "$HEALID" '.proposals[] | select(.id == $id) | .status == "approved" and .applied == false' >/dev/null || { echo "FAIL: simulated proposal changed after refused apply"; exit 1; }
echo "OK: simulated proposal still approved, applied=false"
echo "== cleanup: delete app 1 (best effort) =="
curl -fsS -X DELETE "$GW/api/v1/apps/$APP1ID" -H "Authorization: Bearer $TOK" || true; echo
echo "DEPLOY PROVED: two apps on different ports both returned 200, stop/delete work, simulated heal apply refused with nothing changed."
echo "MANUAL ONLY (not scripted, never drop a real column here): a detect-style apply — remove a column from clients in a throwaway DB, run heal/detect, approve, apply, and verify the column returns and the proposal is applied=true."
