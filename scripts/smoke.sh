#!/usr/bin/env bash
# End-to-end smoke test: boots the server on a throwaway database and
# exercises auth, sync push/pull, conflict behavior, validation, read
# models, and estimates. Requires curl and python3.
set -euo pipefail

BASE="${FOLICULAR_BASE:-http://127.0.0.1:18080}"
ADDR="${FOLICULAR_ADDR:-127.0.0.1:18080}"
BIN="${BIN:-bin/folicular}"
DB="$(mktemp -u /tmp/folicular-smoke-XXXXXX.db)"
LOG="/tmp/folicular-smoke.log"

cleanup() {
  kill "${SRV_PID:-}" 2>/dev/null || true
  rm -f "$DB" "$DB-wal" "$DB-shm"
}
trap cleanup EXIT

FOLICULAR_ADDR="$ADDR" FOLICULAR_DB_PATH="$DB" "$BIN" >"$LOG" 2>&1 &
SRV_PID=$!

for _ in $(seq 1 50); do
  curl -sf "$BASE/healthz" >/dev/null 2>&1 && break
  sleep 0.1
done
curl -sf "$BASE/healthz" >/dev/null || { echo "FAIL: server did not start"; cat "$LOG"; exit 1; }
echo "ok: server up"

uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
# json_get '<python expr over d>' <<< "$JSON"
json_get() { python3 -c 'import json,sys; d=json.load(sys.stdin); print('"$1"')'; }
fail() { echo "FAIL: $1"; echo "--- last response:"; echo "${2:-}"; echo "--- server log tail:"; tail -5 "$LOG"; exit 1; }

# --- 1. Register -----------------------------------------------------------
REG="$(curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' -d '{"device_name":"smoke-1"}')" \
  || fail "register"
TOKEN="$(json_get 'd["device"]["token"]' <<<"$REG")"
CODE="$(json_get 'd["account"]["code"]' <<<"$REG")"
AUTH="Authorization: Bearer $TOKEN"
[ -n "$TOKEN" ] && [ -n "$CODE" ] || fail "register payload" "$REG"
echo "ok: register (account code shown once)"

# --- 2. /v1/me and settings -------------------------------------------------
ME="$(curl -sf "$BASE/v1/me" -H "$AUTH")" || fail "me"
[ "$(json_get 'd["settings"]["locale"]' <<<"$ME")" = "fr" ] || fail "default locale" "$ME"
ME="$(curl -sf -X PATCH "$BASE/v1/me" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"life_stage":"reproductive_peak","tracking_focus":["pms"]}')" || fail "patch me"
[ "$(json_get 'd["settings"]["life_stage"]' <<<"$ME")" = "reproductive_peak" ] || fail "patched life_stage" "$ME"
echo "ok: me + settings"

# --- 3. Sync push: one cycle + daily observations ---------------------------
T="$(now)"
C1_ID="$(uuid)"; C1_REV="$(uuid)"
D1_ID="$(uuid)"; D1_REV="$(uuid)"
B1_ID="$(uuid)"; B1_REV="$(uuid)"
PUSH1="$(curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"cycle\", \"data\": {
      \"id\": \"$C1_ID\", \"client_rev\": \"$C1_REV\", \"created_at\": \"$T\", \"updated_at\": \"$T\", \"deleted_at\": null,
      \"start_date\": \"2026-06-30\", \"end_date\": null, \"length_days\": null, \"bleeding_days\": 5,
      \"certainty\": \"recorded\", \"source\": \"manual\", \"notes\": \"\"}},
    {\"entity_type\": \"daily_entry\", \"data\": {
      \"id\": \"$D1_ID\", \"client_rev\": \"$D1_REV\", \"created_at\": \"$T\", \"updated_at\": \"$T\", \"deleted_at\": null,
      \"entry_date\": \"2026-07-01\", \"pain_level\": 2, \"mood_level\": 3, \"energy_level\": 4, \"notes\": \"\"}},
    {\"entity_type\": \"bleeding_observation\", \"data\": {
      \"id\": \"$B1_ID\", \"client_rev\": \"$B1_REV\", \"created_at\": \"$T\", \"updated_at\": \"$T\", \"deleted_at\": null,
      \"observed_date\": \"2026-06-30\", \"flow\": \"medium\", \"intermenstrual\": false, \"product_count\": 3, \"notes\": \"\"}}
  ]}")" || fail "push 1"
[ "$(json_get 'len(d["applied"])' <<<"$PUSH1")" = "3" ] || fail "push 1 applied" "$PUSH1"
CURSOR="$(json_get 'd["cursor"]' <<<"$PUSH1")"
echo "ok: push (3 applied, cursor=$CURSOR)"

# --- 4. Validation rejection -------------------------------------------------
BAD_ID="$(uuid)"; BAD_REV="$(uuid)"
PUSHBAD="$(curl -s -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"bleeding_observation\", \"data\": {
      \"id\": \"$BAD_ID\", \"client_rev\": \"$BAD_REV\", \"created_at\": \"$T\", \"updated_at\": \"$T\", \"deleted_at\": null,
      \"observed_date\": \"2026-07-02\", \"flow\": \"torrential\", \"intermenstrual\": false, \"product_count\": null, \"notes\": \"\"}},
    {\"entity_type\": \"daily_entry\", \"data\": {
      \"id\": \"$(uuid)\", \"client_rev\": \"$(uuid)\", \"created_at\": \"$T\", \"updated_at\": \"$T\", \"deleted_at\": null,
      \"entry_date\": \"2026-07-02\", \"pain_level\": 9, \"mood_level\": null, \"energy_level\": null, \"notes\": \"\"}}
  ]}")" || fail "push bad"
[ "$(json_get 'len(d["rejected"])' <<<"$PUSHBAD")" = "2" ] || fail "expected 2 rejections" "$PUSHBAD"
echo "ok: invalid records rejected with field detail"

# --- 5. Conflict: stale write loses, server state returned --------------------
T2="$(now)"
CONFLICT="$(curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [{\"entity_type\": \"cycle\", \"data\": {
    \"id\": \"$C1_ID\", \"client_rev\": \"$(uuid)\", \"created_at\": \"$T\", \"updated_at\": \"2020-01-01T00:00:00Z\", \"deleted_at\": null,
    \"start_date\": \"2026-06-29\", \"end_date\": null, \"length_days\": null, \"bleeding_days\": 5,
    \"certainty\": \"recorded\", \"source\": \"manual\", \"notes\": \"stale\"}}]}")" || fail "conflict push"
[ "$(json_get 'len(d["conflicts"])' <<<"$CONFLICT")" = "1" ] || fail "expected 1 conflict" "$CONFLICT"
[ "$(json_get 'd["conflicts"][0]["current"]["start_date"]' <<<"$CONFLICT")" = "2026-06-30" ] \
  || fail "conflict must carry server state" "$CONFLICT"
echo "ok: stale write rejected, server state returned (no silent loss)"

# --- 6. Pull: seeded symptoms + pushed records --------------------------------
PULL="$(curl -sf "$BASE/v1/sync/pull?since=0" -H "$AUTH")" || fail "pull"
[ "$(json_get 'len([c for c in d["changes"] if c["entity_type"]=="symptom_definition"])' <<<"$PULL")" -ge 10 ] \
  || fail "expected seeded symptom definitions in pull" "$PULL"
[ "$(json_get 'len([c for c in d["changes"] if c["entity_type"]=="cycle"])' <<<"$PULL")" = "1" ] \
  || fail "expected 1 cycle in pull" "$PULL"
echo "ok: pull returns seeded catalog + pushed records"

# --- 7. Estimates: insufficient, then real history -----------------------------
PRED="$(curl -sf "$BASE/v1/predictions/current" -H "$AUTH")" || fail "predictions"
[ "$(json_get 'd["next_menstruation"]' <<<"$PRED")" = "None" ] || fail "expected insufficient estimate" "$PRED"

STARTS=("2026-01-01" "2026-01-30" "2026-02-28" "2026-03-29" "2026-04-27" "2026-05-26" "2026-06-24")
CHANGES="["
FIRST=1
for S in "${STARTS[@]}"; do
  [ $FIRST -eq 0 ] && CHANGES+=","
  FIRST=0
  CHANGES+="{\"entity_type\": \"cycle\", \"data\": {
    \"id\": \"$(uuid)\", \"client_rev\": \"$(uuid)\", \"created_at\": \"$T2\", \"updated_at\": \"$T2\", \"deleted_at\": null,
    \"start_date\": \"$S\", \"end_date\": null, \"length_days\": null, \"bleeding_days\": null,
    \"certainty\": \"recorded\", \"source\": \"manual\", \"notes\": \"\"}}"
done
CHANGES+="]"
curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"changes\": $CHANGES}" >/dev/null || fail "push history"

PRED="$(curl -sf "$BASE/v1/predictions/current" -H "$AUTH")" || fail "predictions 2"
[ "$(json_get 'd["next_menstruation"]["central_date"]' <<<"$PRED")" = "2026-07-29" ] \
  || fail "expected central 2026-07-29 (last start 2026-06-30 + 29-day median)" "$PRED"
[ "$(json_get 'd["ovulation_estimate"]["confidence"]' <<<"$PRED")" = "low" ] \
  || fail "ovulation must stay low confidence" "$PRED"
[ "$(json_get 'd["disclaimer"]' <<<"$PRED")" != "" ] || fail "missing disclaimer" "$PRED"
echo "ok: estimates are ranges with uncertainty and disclaimer"

# --- 8. Days read model ---------------------------------------------------------
DAYS="$(curl -sf "$BASE/v1/days?from=2026-06-30&to=2026-07-01" -H "$AUTH")" || fail "days"
[ "$(json_get 'len(d["days"])' <<<"$DAYS")" = "2" ] || fail "expected 2 days" "$DAYS"
[ "$(json_get 'd["days"][0]["cycle_day"]' <<<"$DAYS")" = "1" ] || fail "expected cycle_day 1 on 2026-06-30" "$DAYS"
[ "$(json_get 'd["days"][0]["bleeding"]["flow"]' <<<"$DAYS")" = "medium" ] || fail "expected bleeding on day 1" "$DAYS"
echo "ok: days read model merges observations with cycle_day"

# --- 9. Tombstone deletion replicates ---------------------------------------------
T3="$(now)"
DEL="$(curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [{\"entity_type\": \"daily_entry\", \"data\": {
    \"id\": \"$D1_ID\", \"client_rev\": \"$(uuid)\", \"created_at\": \"$T\", \"updated_at\": \"$T3\", \"deleted_at\": \"$T3\",
    \"entry_date\": \"2026-07-01\", \"pain_level\": 2, \"mood_level\": 3, \"energy_level\": 4, \"notes\": \"\"}}]}")" \
  || fail "delete push"
[ "$(json_get 'len(d["applied"])' <<<"$DEL")" = "1" ] || fail "delete applied" "$DEL"
PULL2="$(curl -sf "$BASE/v1/sync/pull?since=$CURSOR" -H "$AUTH")" || fail "pull 2"
[ "$(json_get 'len([c for c in d["changes"] if c["deleted"]])' <<<"$PULL2")" -ge 1 ] \
  || fail "expected tombstone in pull" "$PULL2"
echo "ok: tombstone deletion replicates"

# --- 10. Multi-device + revocation -------------------------------------------------
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

# --- 11. Bad account code is rejected generically -----------------------------------
CODE3="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/v1/auth/devices" \
  -H 'Content-Type: application/json' -d '{"code": "LTL-00000-00000-00000-00000", "device_name": "x"}')"
[ "$CODE3" = "401" ] || fail "bad code must yield 401 (got $CODE3)"
echo "ok: invalid account code rejected"

# --- 12. Duo: link/QR pairing, grants, projection, support --------------------
REGP="$(curl -sf -X POST "$BASE/v1/auth/register" -H 'Content-Type: application/json' -d '{"device_name":"smoke-partner"}')" \
  || fail "register partner"
TOKENP="$(json_get 'd["device"]["token"]' <<<"$REGP")"
AUTHP="Authorization: Bearer $TOKENP"

START5="$(date -u -d '-5 days' +%F)"
TODAY="$(date -u +%F)"
T4="$(now)"
curl -sf -X POST "$BASE/v1/sync/push" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"changes\": [
    {\"entity_type\": \"cycle\", \"data\": {
      \"id\": \"$(uuid)\", \"client_rev\": \"$(uuid)\", \"created_at\": \"$T4\", \"updated_at\": \"$T4\", \"deleted_at\": null,
      \"start_date\": \"$START5\", \"end_date\": null, \"length_days\": null, \"bleeding_days\": null,
      \"certainty\": \"recorded\", \"source\": \"manual\", \"notes\": \"\"}},
    {\"entity_type\": \"daily_entry\", \"data\": {
      \"id\": \"$(uuid)\", \"client_rev\": \"$(uuid)\", \"created_at\": \"$T4\", \"updated_at\": \"$T4\", \"deleted_at\": null,
      \"entry_date\": \"$TODAY\", \"pain_level\": null, \"mood_level\": 4, \"energy_level\": 2, \"notes\": \"\"}}
  ]}" >/dev/null || fail "tracker pre-duo push"

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

for F in cycle_day mood support_requests; do
  curl -sf -X PATCH "$BASE/v1/duo/links/$LINKID/grants" -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"field\": \"$F\", \"granted\": true}" -o /dev/null || fail "grant $F"
done

VIEW="$(curl -sf "$BASE/v1/duo/view" -H "$AUTHP")" || fail "partner view"
[ "$(json_get 'd["cycle_day"]' <<<"$VIEW")" = "6" ] || fail "expected cycle_day 6" "$VIEW"
[ "$(json_get 'd["mood"]["level"]' <<<"$VIEW")" = "4" ] || fail "expected shared mood 4" "$VIEW"
[ "$(json_get 'd["energy"]' <<<"$VIEW")" = "None" ] || fail "ungranted energy must be null" "$VIEW"
[ "$(json_get 'd["period_estimate"]' <<<"$VIEW")" = "None" ] || fail "ungranted period_estimate must be null" "$VIEW"
echo "ok: partner projection respects grants exactly"

SR="$(curl -sf -X POST "$BASE/v1/duo/support-requests" -H "$AUTHP" -H 'Content-Type: application/json' \
  -d "{\"link_id\": \"$LINKID\", \"kind\": \"comfort\", \"message\": \"Un peu de douceur ce soir ?\"}")" \
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
