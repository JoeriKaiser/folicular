#!/usr/bin/env bash
# End-to-end smoke test: boots the server on a throwaway database and
# exercises auth, sync push/pull, conflict behavior, validation, and Duo flows.
# Requires curl and python3.
set -euo pipefail

BASE="${FOLICULAR_BASE:-http://127.0.0.1:18080}"
ADDR="${FOLICULAR_ADDR:-127.0.0.1:18080}"
BIN="${BIN:-bin/folicular}"
DB="$(mktemp -u /tmp/folicular-smoke-XXXXXX.db)"
LOG="/tmp/folicular-smoke.log"

cleanup() {
  if [ -n "${SRV_PID:-}" ] && kill -0 "$SRV_PID" 2>/dev/null; then
    kill "$SRV_PID" 2>/dev/null || true
    wait "$SRV_PID" 2>/dev/null || true
  fi
  rm -f "$DB" "$DB-wal" "$DB-shm" "$LOG"
}
trap cleanup EXIT

FOLICULAR_ADDR="$ADDR" FOLICULAR_DB_PATH="$DB" "$BIN" >"$LOG" 2>&1 &
SRV_PID=$!

for _ in $(seq 1 50); do
  if curl -sf "$BASE/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
curl -sf "$BASE/healthz" >/dev/null || { echo "FAIL: server did not start"; cat "$LOG"; exit 1; }
echo "ok: server up"

uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
b64blob() { python3 -c 'import base64, os; print(base64.b64encode(b"\x01" + os.urandom(44)).decode())'; }
json_get() { python3 -c 'import json,sys; d=json.load(sys.stdin); print('"$1"')'; }
fail() { echo "FAIL: $1"; echo "--- last response:"; echo "${2:-}"; echo "--- server log tail:"; tail -5 "$LOG"; exit 1; }

# --- 1. Operations endpoints --------------------------------------------------
curl -sf "$BASE/readyz" >/dev/null || fail "readyz"
VER="$(curl -sf "$BASE/version")" || fail "version"
[ "$(json_get 'd["version"]' <<<"$VER")" != "" ] || fail "version response" "$VER"
echo "ok: healthz, readyz, version"

# --- 2. Register --------------------------------------------------------------
REG="$(curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' -d '{"device_name":"smoke-1"}')" \
  || fail "register"
TOKEN="$(json_get 'd["device"]["token"]' <<<"$REG")"
CODE="$(json_get 'd["account"]["code"]' <<<"$REG")"
AUTH="Authorization: Bearer $TOKEN"
[ -n "$TOKEN" ] && [ -n "$CODE" ] || fail "register payload" "$REG"
echo "ok: register (account code shown once)"

# --- 3. /v1/me and sealed settings --------------------------------------------
ME="$(curl -sf "$BASE/v1/me" -H "$AUTH")" || fail "me"
[ "$(json_get 'd["account"]["status"]' <<<"$ME")" = "active" ] || fail "active account" "$ME"
SEALED_SETTINGS="$(b64blob)"
ME_PATCH="$(curl -sf -X PATCH "$BASE/v1/me" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"settings\":\"$SEALED_SETTINGS\"}")" || fail "patch me"
[ "$(json_get 'd["settings"]["settings"]' <<<"$ME_PATCH")" = "$SEALED_SETTINGS" ] || fail "patched settings blob" "$ME_PATCH"
echo "ok: me + settings patch"

# --- 4. Sync push: encrypted records ------------------------------------------
T="$(now)"
C1_ID="$(uuid)"; C1_REV="$(uuid)"; C1_BLOB="$(b64blob)"
D1_ID="$(uuid)"; D1_REV="$(uuid)"; D1_BLOB="$(b64blob)"
B1_ID="$(uuid)"; B1_REV="$(uuid)"; B1_BLOB="$(b64blob)"
PUSH1="$(curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"cycle\", \"entity_id\": \"$C1_ID\", \"client_rev\": \"$C1_REV\",
     \"updated_at\": \"$T\", \"deleted\": false, \"ciphertext\": \"$C1_BLOB\"},
    {\"entity_type\": \"daily_entry\", \"entity_id\": \"$D1_ID\", \"client_rev\": \"$D1_REV\",
     \"updated_at\": \"$T\", \"deleted\": false, \"ciphertext\": \"$D1_BLOB\"},
    {\"entity_type\": \"bleeding_observation\", \"entity_id\": \"$B1_ID\", \"client_rev\": \"$B1_REV\",
     \"updated_at\": \"$T\", \"deleted\": false, \"ciphertext\": \"$B1_BLOB\"}
  ]}")" || fail "push 1"
[ "$(json_get 'len(d["applied"])' <<<"$PUSH1")" = "3" ] || fail "push 1 applied" "$PUSH1"
CURSOR="$(json_get 'd["cursor"]' <<<"$PUSH1")"
echo "ok: push (3 applied, cursor=$CURSOR)"

# --- 5. Validation rejection --------------------------------------------------
BAD_ID="$(uuid)"; BAD_REV="$(uuid)"
PUSHBAD="$(curl -s -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"cycle\", \"entity_id\": \"$BAD_ID\", \"client_rev\": \"$BAD_REV\",
     \"updated_at\": \"$T\", \"deleted\": false, \"ciphertext\": \"\"}
  ]}")" || fail "push bad"
[ "$(json_get 'len(d["rejected"])' <<<"$PUSHBAD")" = "1" ] || fail "expected 1 rejection" "$PUSHBAD"
echo "ok: invalid record rejected"

# --- 6. Conflict: stale write rejected, current state returned ---------------
CONFLICT="$(curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"cycle\", \"entity_id\": \"$C1_ID\", \"client_rev\": \"$(uuid)\",
     \"updated_at\": \"2020-01-01T00:00:00Z\", \"deleted\": false, \"ciphertext\": \"$(b64blob)\"}
  ]}")" || fail "conflict push"
[ "$(json_get 'len(d["conflicts"])' <<<"$CONFLICT")" = "1" ] || fail "expected 1 conflict" "$CONFLICT"
[ "$(json_get 'd["conflicts"][0]["current_ciphertext"]' <<<"$CONFLICT")" = "$C1_BLOB" ] \
  || fail "conflict must carry server current ciphertext" "$CONFLICT"
echo "ok: stale write rejected, server state returned"

# --- 7. Pull: returns pushed encrypted records --------------------------------
PULL="$(curl -sf "$BASE/v1/sync/pull?since=0" -H "$AUTH")" || fail "pull"
[ "$(json_get 'len(d["changes"])' <<<"$PULL")" -ge 3 ] || fail "expected at least 3 records in pull" "$PULL"
echo "ok: pull returns synced records"

# --- 8. Tombstone deletion replicates -----------------------------------------
T2="$(date -u -d '+2 seconds' +%Y-%m-%dT%H:%M:%SZ)"
DEL="$(curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"daily_entry\", \"entity_id\": \"$D1_ID\", \"client_rev\": \"$(uuid)\",
     \"updated_at\": \"$T2\", \"deleted\": true, \"ciphertext\": null}
  ]}")" || fail "delete push"
[ "$(json_get 'len(d["applied"])' <<<"$DEL")" = "1" ] || fail "delete applied" "$DEL"
PULL2="$(curl -sf "$BASE/v1/sync/pull?since=$CURSOR" -H "$AUTH")" || fail "pull 2"
[ "$(json_get 'len([c for c in d["changes"] if c["deleted"]])' <<<"$PULL2")" -ge 1 ] \
  || fail "expected tombstone in pull" "$PULL2"
echo "ok: tombstone deletion replicates"

# --- 9. Multi-device + revocation ---------------------------------------------
DEV2="$(curl -sf -X POST "$BASE/v1/auth/devices" -H 'Content-Type: application/json' \
  -d "{\"code\": \"$CODE\", \"device_name\": \"smoke-2\"}")" || fail "add device"
TOKEN2="$(json_get 'd["device"]["token"]' <<<"$DEV2")"
DEVICES="$(curl -sf "$BASE/v1/auth/devices" -H "$AUTH")" || fail "list devices"
[ "$(json_get 'len(d["devices"])' <<<"$DEVICES")" = "2" ] || fail "expected 2 devices" "$DEVICES"
DEV2_ID="$(json_get 'd["device"]["id"]' <<<"$DEV2")"
curl -sf -X DELETE "$BASE/v1/auth/devices/$DEV2_ID" -H "$AUTH" -o /dev/null || fail "revoke device"
CODE2="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/me" -H "Authorization: Bearer $TOKEN2")"
[ "$CODE2" = "401" ] || fail "revoked token must be rejected (got $CODE2)"
echo "ok: multi-device registration and revocation"
# --- 10. Bad account code is rejected generically -----------------------------
CODE3="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/auth/devices" \
  -H 'Content-Type: application/json' -d '{"code": "LTL-00000-00000-00000-00000", "device_name": "x"}')"
[ "$CODE3" = "401" ] || fail "bad code must yield 401 (got $CODE3)"
echo "ok: invalid account code rejected"

# --- 11. Duo: link/QR pairing, grants, projection, support --------------------
REGP="$(curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' -d '{"device_name":"smoke-partner"}')" \
  || fail "register partner"
TOKENP="$(json_get 'd["device"]["token"]' <<<"$REGP")"
AUTHP="Authorization: Bearer $TOKENP"

INV="$(curl -sf -X POST "$BASE/v1/duo/invitations" -H "$AUTH")" || fail "create invitation"
PCODE="$(json_get 'd["pairing_code"]' <<<"$INV")"
PURL="$(json_get 'd["pairing_url"]' <<<"$INV")"
LINKID="$(json_get 'd["link_id"]' <<<"$INV")"
[[ "$PURL" == *"$PCODE"* ]] || fail "pairing_url must embed the code (QR/deep-link ready)" "$INV"
echo "ok: invitation with code + shareable URL (QR-ready)"

ACC="$(curl -sf -X POST "$BASE/v1/duo/links" -H "$AUTHP" -H 'Content-Type: application/json' \
  -d "{\"pairing_code\": \"$PCODE\"}")" || fail "accept link"
[ "$(json_get 'd["link"]["role"]' <<<"$ACC")" = "partner" ] || fail "accept role" "$ACC"
REUSE="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/duo/links" -H "$AUTHP" \
  -H 'Content-Type: application/json' -d "{\"pairing_code\": \"$PCODE\"}")"
[ "$REUSE" = "404" ] || fail "pairing code must be single-use (got $REUSE)"
echo "ok: link accepted; pairing code is single-use"

# Tracker pushes Duo shared payload
DUO_PAYLOAD="$(b64blob)"
curl -sf -X PUT "$BASE/v1/duo/payload" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"payload\": \"$DUO_PAYLOAD\"}" -o /dev/null || fail "put duo payload"

VIEW="$(curl -sf "$BASE/v1/duo/view" -H "$AUTHP")" || fail "partner view"
[ "$(json_get 'd["payload"]' <<<"$VIEW")" = "$DUO_PAYLOAD" ] || fail "partner payload" "$VIEW"
echo "ok: partner view receives shared duo payload"

SR_MSG="$(b64blob)"
SR="$(curl -sf -X POST "$BASE/v1/duo/support-requests" -H "$AUTHP" -H 'Content-Type: application/json' \
  -d "{\"link_id\": \"$LINKID\", \"kind\": \"comfort\", \"message\": \"$SR_MSG\"}")" \
  || fail "support request"
SRID="$(json_get 'd["id"]' <<<"$SR")"
TVIEW="$(curl -sf "$BASE/v1/duo/view" -H "$AUTH")" || fail "tracker view"
[ "$(json_get 'len(d["support_requests"])' <<<"$TVIEW")" = "1" ] || fail "tracker must see the thread" "$TVIEW"
curl -sf -X PATCH "$BASE/v1/duo/support-requests/$SRID/ack" -H "$AUTH" -o /dev/null || fail "ack"
TVIEW="$(curl -sf "$BASE/v1/duo/view" -H "$AUTH")" || fail "tracker view 2"
[ "$(json_get 'd["support_requests"][0]["acknowledged_at"]' <<<"$TVIEW")" != "None" ] \
  || fail "ack must be visible" "$TVIEW"
echo "ok: support request + acknowledgement round-trip"

curl -sf -X DELETE "$BASE/v1/duo/links/$LINKID" -H "$AUTH" -o /dev/null || fail "revoke link"
AFTER="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/duo/view" -H "$AUTHP")"
[ "$AFTER" = "404" ] || fail "revoked link must end the partner view (got $AFTER)"
echo "ok: revocation ends sharing immediately"

echo
echo "PASS: all smoke checks succeeded"
