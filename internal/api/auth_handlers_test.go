package api_test

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"folicular/internal/auth"
	"folicular/internal/db"
	"folicular/internal/db/dbgen"
	"folicular/internal/server"
)

type testServer struct {
	t   *testing.T
	db  *sql.DB
	srv *httptest.Server
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := server.NewRouter(log, dbgen.New(sqlDB), sqlDB, "test", "https://luteal.app/duo", nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &testServer{t: t, db: sqlDB, srv: srv}
}

func (ts *testServer) request(method, path string, body any, token string) (*http.Response, map[string]any) {
	ts.t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			ts.t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, ts.srv.URL+path, reqBody)
	if err != nil {
		ts.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatalf("execute request: %v", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		ts.t.Fatalf("read response body: %v", err)
	}

	var parsed map[string]any
	if len(respBytes) > 0 {
		_ = json.Unmarshal(respBytes, &parsed)
	}
	return resp, parsed
}

func fakeEnvelope(t *testing.T, version byte) string {
	t.Helper()
	buf := make([]byte, 1+12+24+16)
	buf[0] = version
	if _, err := rand.Read(buf[1:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func TestV2AuthLifecycle_HexAndPassTheHashRejected(t *testing.T) {
	ts := newTestServer(t)

	kAuth := randomKey(t)
	authKeyHex := hex.EncodeToString(kAuth)

	// 1. Register with auth_key (hex)
	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Pixel 9 Pro",
		"auth_key":    authKeyHex,
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", regResp.StatusCode, regBody)
	}

	account := regBody["account"].(map[string]any)
	accountID := account["id"].(string)
	if account["auth_version"] != float64(2) {
		t.Fatalf("expected auth_version=2, got %v", account["auth_version"])
	}
	if account["code"] != nil && account["code"] != "" {
		t.Fatalf("expected account.code omitted for v2, got %v", account["code"])
	}
	firstToken := regBody["device"].(map[string]any)["token"].(string)

	// 2. Verify pass-the-hash is impossible: reading accounts.code_hash directly
	// from SQLite and submitting it as auth_key must fail with 401.
	var storedHash []byte
	if err := ts.db.QueryRow("SELECT code_hash FROM accounts WHERE id = ?", accountID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored code_hash: %v", err)
	}
	if bytes.Equal(storedHash, kAuth) {
		t.Fatal("stored code_hash must NOT equal raw auth_key")
	}
	pthResp, _ := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Attacker",
		"auth_key":    hex.EncodeToString(storedHash),
	}, "")
	if pthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pass-the-hash replay MUST fail with 401, got %d", pthResp.StatusCode)
	}

	// 3. Add second device with genuine auth_key
	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Tablet",
		"auth_key":    authKeyHex,
	}, "")
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on add device, got %d: %v", addResp.StatusCode, addBody)
	}
	if addBody["account_id"] != accountID || addBody["auth_version"] != float64(2) {
		t.Fatalf("unexpected addDevice response: %v", addBody)
	}
	secondToken := addBody["device"].(map[string]any)["token"].(string)
	if firstToken == secondToken {
		t.Fatal("second device token must be distinct")
	}

	// 4. Verify v2 account rejects 0x01 ciphertext and accepts 0x02 ciphertext
	pushBadResp, pushBadBody := ts.request("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{{
			"entity_type": "cycle",
			"entity_id":   "019832e0-6c14-7000-8000-000000000010",
			"client_rev":  "019832e0-6c14-7000-8000-000000000011",
			"updated_at":  "2026-07-01T08:00:00Z",
			"deleted":     false,
			"ciphertext":  fakeEnvelope(t, 0x01),
		}},
	}, firstToken)
	if pushBadResp.StatusCode != http.StatusOK || len(pushBadBody["rejected"].([]any)) != 1 {
		t.Fatalf("expected 0x01 record rejected on v2 account, got %d: %v", pushBadResp.StatusCode, pushBadBody)
	}

	pushGoodResp, pushGoodBody := ts.request("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{{
			"entity_type": "cycle",
			"entity_id":   "019832e0-6c14-7000-8000-000000000010",
			"client_rev":  "019832e0-6c14-7000-8000-000000000011",
			"updated_at":  "2026-07-01T08:00:00Z",
			"deleted":     false,
			"ciphertext":  fakeEnvelope(t, 0x02),
		}},
	}, firstToken)
	if pushGoodResp.StatusCode != http.StatusOK || len(pushGoodBody["applied"].([]any)) != 1 {
		t.Fatalf("expected 0x02 record applied on v2 account, got %d: %v", pushGoodResp.StatusCode, pushGoodBody)
	}
}

func TestV2AuthLifecycle_Base64(t *testing.T) {
	ts := newTestServer(t)

	kAuth := randomKey(t)
	authKeyStdB64 := base64.StdEncoding.EncodeToString(kAuth)
	authKeyURLB64 := base64.RawURLEncoding.EncodeToString(kAuth)

	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Phone",
		"auth_key":    authKeyStdB64,
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", regResp.StatusCode, regBody)
	}
	accountID := regBody["account"].(map[string]any)["id"].(string)

	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Secondary",
		"auth_key":    authKeyURLB64,
	}, "")
	if addResp.StatusCode != http.StatusCreated || addBody["account_id"] != accountID {
		t.Fatalf("expected 201 matching account_id, got %d: %v", addResp.StatusCode, addBody)
	}
}

func TestLegacyAuthLifecycle(t *testing.T) {
	ts := newTestServer(t)

	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Legacy Phone",
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", regResp.StatusCode, regBody)
	}

	account := regBody["account"].(map[string]any)
	accountID := account["id"].(string)
	if account["auth_version"] != float64(1) {
		t.Fatalf("expected auth_version=1, got %v", account["auth_version"])
	}
	displayCode := account["code"].(string)
	if len(auth.NormalizeCode(displayCode)) != 20 {
		t.Fatalf("expected 20-symbol normalized code, got %q", displayCode)
	}

	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Legacy Tablet",
		"code":        displayCode,
	}, "")
	if addResp.StatusCode != http.StatusCreated || addBody["account_id"] != accountID || addBody["auth_version"] != float64(1) {
		t.Fatalf("unexpected legacy addDevice: %d %v", addResp.StatusCode, addBody)
	}
}

func TestV2Auth_ErrorCases(t *testing.T) {
	ts := newTestServer(t)

	// 1. Deprecated auth_hash -> 422 on register, 401 on addDevice
	resp, _ := ts.request("POST", "/v1/auth/register", map[string]any{
		"auth_hash": hex.EncodeToString(randomKey(t)),
	}, "")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for deprecated auth_hash on register, got %d", resp.StatusCode)
	}
	resp, _ = ts.request("POST", "/v1/auth/devices", map[string]any{
		"auth_hash": hex.EncodeToString(randomKey(t)),
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for deprecated auth_hash on addDevice, got %d", resp.StatusCode)
	}

	// 2. Invalid auth_key format -> 422 on register
	resp, _ = ts.request("POST", "/v1/auth/register", map[string]any{
		"auth_key": "not-valid",
	}, "")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}

	// 3. Both auth_key and code -> 422 on register, 401 on addDevice
	resp, _ = ts.request("POST", "/v1/auth/register", map[string]any{
		"auth_key": hex.EncodeToString(randomKey(t)),
		"code":     "LTL-23456-789AB-CDEFG-HJKMN",
	}, "")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for mutually exclusive fields, got %d", resp.StatusCode)
	}

	// 4. Unknown auth_key -> 401 on addDevice
	resp, _ = ts.request("POST", "/v1/auth/devices", map[string]any{
		"auth_key": hex.EncodeToString(randomKey(t)),
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestV1ToV2AtomicUpgradeLifecycle(t *testing.T) {
	ts := newTestServer(t)

	// 1. Create a v1 account with two devices
	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Primary v1",
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("register v1 failed: %d", regResp.StatusCode)
	}
	accountID := regBody["account"].(map[string]any)["id"].(string)
	v1Code := regBody["account"].(map[string]any)["code"].(string)
	primaryToken := regBody["device"].(map[string]any)["token"].(string)

	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Secondary v1",
		"code":        v1Code,
	}, "")
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("add secondary v1 failed: %d", addResp.StatusCode)
	}
	secondaryToken := addBody["device"].(map[string]any)["token"].(string)

	// 2. Push two v1 records (one live, one tombstone) and v1 sealed settings
	liveID := "019832e0-6c14-7000-8000-000000000101"
	liveRev := "019832e0-6c14-7000-8000-000000000102"
	deadID := "019832e0-6c14-7000-8000-000000000103"
	deadRev := "019832e0-6c14-7000-8000-000000000104"

	pushResp, pushBody := ts.request("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{
			{
				"entity_type": "cycle",
				"entity_id":   liveID,
				"client_rev":  liveRev,
				"updated_at":  "2026-07-01T08:00:00Z",
				"deleted":     false,
				"ciphertext":  fakeEnvelope(t, 0x01),
			},
			{
				"entity_type": "daily_entry",
				"entity_id":   deadID,
				"client_rev":  deadRev,
				"updated_at":  "2026-07-01T09:00:00Z",
				"deleted":     true,
				"ciphertext":  nil,
			},
		},
	}, primaryToken)
	if pushResp.StatusCode != http.StatusOK {
		t.Fatalf("push v1 failed: %d %v", pushResp.StatusCode, pushBody)
	}
	v1Cursor := int64(pushBody["cursor"].(float64))

	patchResp, _ := ts.request("PATCH", "/v1/me", map[string]any{
		"settings": fakeEnvelope(t, 0x01),
	}, primaryToken)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch settings v1 failed: %d", patchResp.StatusCode)
	}

	// 3. Establish an active Duo link + grant + sealed Duo payload to verify continuity
	invResp, invBody := ts.request("POST", "/v1/duo/invitations", nil, primaryToken)
	if invResp.StatusCode != http.StatusCreated {
		t.Fatalf("duo invite failed: %d", invResp.StatusCode)
	}
	pairingCode := invBody["pairing_code"].(string)
	linkID := invBody["link_id"].(string)

	partnerRegResp, partnerRegBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Partner",
		"auth_key":    hex.EncodeToString(randomKey(t)),
	}, "")
	if partnerRegResp.StatusCode != http.StatusCreated {
		t.Fatalf("partner register failed: %d", partnerRegResp.StatusCode)
	}
	partnerToken := partnerRegBody["device"].(map[string]any)["token"].(string)

	accResp, _ := ts.request("POST", "/v1/duo/links", map[string]any{"pairing_code": pairingCode}, partnerToken)
	if accResp.StatusCode != http.StatusCreated {
		t.Fatalf("duo accept failed: %d", accResp.StatusCode)
	}
	grantResp, _ := ts.request("PATCH", "/v1/duo/links/"+linkID+"/grants", map[string]any{
		"field": "mood", "granted": true,
	}, primaryToken)
	if grantResp.StatusCode != http.StatusNoContent {
		t.Fatalf("duo grant failed: %d", grantResp.StatusCode)
	}
	duoPayload := fakeEnvelope(t, 0x01)
	putDuoResp, _ := ts.request("PUT", "/v1/duo/payload", map[string]any{"payload": duoPayload}, primaryToken)
	if putDuoResp.StatusCode != http.StatusNoContent {
		t.Fatalf("duo payload put failed: %d", putDuoResp.StatusCode)
	}

	// 4. Prepare v2 upgrade payload
	newAuthKey := randomKey(t)
	newAuthKeyHex := hex.EncodeToString(newAuthKey)
	v2RecordBlob := fakeEnvelope(t, 0x02)
	v2SettingsBlob := fakeEnvelope(t, 0x02)

	// 4a. Stale expected_cursor -> 409 Conflict
	staleResp, _ := ts.request("POST", "/v1/auth/upgrade", map[string]any{
		"auth_key":        newAuthKeyHex,
		"expected_cursor": v1Cursor - 1,
		"settings":        v2SettingsBlob,
		"records": []map[string]any{{
			"entity_type": "cycle",
			"entity_id":   liveID,
			"client_rev":  liveRev,
			"updated_at":  "2026-07-01T08:00:00Z",
			"deleted":     false,
			"ciphertext":  v2RecordBlob,
		}},
	}, primaryToken)
	if staleResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for stale expected_cursor, got %d", staleResp.StatusCode)
	}

	// 4b. Missing live record -> 422 completeness guard
	incompleteResp, _ := ts.request("POST", "/v1/auth/upgrade", map[string]any{
		"auth_key":        newAuthKeyHex,
		"expected_cursor": v1Cursor,
		"settings":        v2SettingsBlob,
		"records":         []map[string]any{},
	}, primaryToken)
	if incompleteResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing live record, got %d", incompleteResp.StatusCode)
	}

	// 4c. Missing re-sealed settings when account has settings -> 422
	noSettingsResp, _ := ts.request("POST", "/v1/auth/upgrade", map[string]any{
		"auth_key":        newAuthKeyHex,
		"expected_cursor": v1Cursor,
		"records": []map[string]any{{
			"entity_type": "cycle",
			"entity_id":   liveID,
			"client_rev":  liveRev,
			"updated_at":  "2026-07-01T08:00:00Z",
			"deleted":     false,
			"ciphertext":  v2RecordBlob,
		}},
	}, primaryToken)
	if noSettingsResp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing re-sealed settings, got %d", noSettingsResp.StatusCode)
	}

	// 5. Execute valid atomic upgrade
	upgradeBodyReq := map[string]any{
		"auth_key":        newAuthKeyHex,
		"expected_cursor": v1Cursor,
		"settings":        v2SettingsBlob,
		"records": []map[string]any{{
			"entity_type": "cycle",
			"entity_id":   liveID,
			"client_rev":  liveRev,
			"updated_at":  "2026-07-01T08:00:00Z",
			"deleted":     false,
			"ciphertext":  v2RecordBlob,
		}},
	}
	upResp, upBody := ts.request("POST", "/v1/auth/upgrade", upgradeBodyReq, primaryToken)
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("upgrade failed: %d %v", upResp.StatusCode, upBody)
	}
	if upBody["auth_version"] != float64(2) || upBody["revoked_devices"] != float64(1) || upBody["records_migrated"] != float64(1) {
		t.Fatalf("unexpected upgrade response: %v", upBody)
	}
	newCursor := int64(upBody["cursor"].(float64))

	// 6. Verify idempotent retry with identical auth_key succeeds with 200
	retryResp, retryBody := ts.request("POST", "/v1/auth/upgrade", upgradeBodyReq, primaryToken)
	if retryResp.StatusCode != http.StatusOK || int64(retryBody["cursor"].(float64)) != newCursor {
		t.Fatalf("expected idempotent 200 on retry, got %d %v", retryResp.StatusCode, retryBody)
	}

	// 7. Verify retry with DIFFERENT auth_key fails with 409
	diffResp, _ := ts.request("POST", "/v1/auth/upgrade", map[string]any{
		"auth_key":        hex.EncodeToString(randomKey(t)),
		"expected_cursor": newCursor,
		"settings":        v2SettingsBlob,
		"records":         upgradeBodyReq["records"],
	}, primaryToken)
	if diffResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 when upgrading already-v2 account with different key, got %d", diffResp.StatusCode)
	}

	// 8. Verify zero 0x01 rows remain anywhere in SQLite for this account
	var v1RecordCount, v1ChangeCount int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM records WHERE account_id = ? AND ciphertext IS NOT NULL AND substr(ciphertext, 1, 1) != X'02'", accountID).Scan(&v1RecordCount); err != nil {
		t.Fatalf("query records: %v", err)
	}
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM sync_changes WHERE account_id = ? AND ciphertext IS NOT NULL AND substr(ciphertext, 1, 1) != X'02'", accountID).Scan(&v1ChangeCount); err != nil {
		t.Fatalf("query sync_changes: %v", err)
	}
	if v1RecordCount != 0 || v1ChangeCount != 0 {
		t.Fatalf("legacy ciphertext leaked after upgrade: records=%d sync_changes=%d", v1RecordCount, v1ChangeCount)
	}

	// 9. Verify calling device stays valid, secondary device is revoked, old v1 code fails
	meResp, meBody := ts.request("GET", "/v1/me", nil, primaryToken)
	if meResp.StatusCode != http.StatusOK || meBody["account"].(map[string]any)["auth_version"] != float64(2) {
		t.Fatalf("expected GET /v1/me auth_version=2, got %d %v", meResp.StatusCode, meBody)
	}
	secMeResp, _ := ts.request("GET", "/v1/me", nil, secondaryToken)
	if secMeResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected secondary v1 device revoked (401), got %d", secMeResp.StatusCode)
	}
	oldAddResp, _ := ts.request("POST", "/v1/auth/devices", map[string]any{
		"code": v1Code, "device_name": "Old Attempt",
	}, "")
	if oldAddResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected old v1 code rejected (401), got %d", oldAddResp.StatusCode)
	}

	// 10. Reconnect secondary device with new v2 auth_key and pull clean 0x02 history
	newAddResp, newAddBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"auth_key": newAuthKeyHex, "device_name": "Reconnected Secondary",
	}, "")
	if newAddResp.StatusCode != http.StatusCreated || newAddBody["account_id"] != accountID {
		t.Fatalf("reconnect v2 failed: %d %v", newAddResp.StatusCode, newAddBody)
	}
	newSecToken := newAddBody["device"].(map[string]any)["token"].(string)
	pullResp, pullBody := ts.request("GET", "/v1/sync/pull?since=0", nil, newSecToken)
	if pullResp.StatusCode != http.StatusOK || len(pullBody["changes"].([]any)) != 1 {
		t.Fatalf("pull after upgrade failed: %d %v", pullResp.StatusCode, pullBody)
	}

	// 11. Verify Duo link, grant, and payload survived intact
	duoViewResp, duoViewBody := ts.request("GET", "/v1/duo/view", nil, partnerToken)
	if duoViewResp.StatusCode != http.StatusOK || duoViewBody["payload"] != duoPayload {
		t.Fatalf("Duo link disrupted by upgrade: %d %v", duoViewResp.StatusCode, duoViewBody)
	}
}
