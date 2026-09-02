package api_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
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
	srv *httptest.Server
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := server.NewRouter(log, dbgen.New(sqlDB), sqlDB, "test", "https://luteal.app/duo", nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &testServer{t: t, srv: srv}
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
	if len(respBytes) > 0 && resp.Header.Get("Content-Type") == "application/json; charset=utf-8" {
		_ = json.Unmarshal(respBytes, &parsed)
	}
	return resp, parsed
}

func TestZeroKnowledgeAuthLifecycle_Hex(t *testing.T) {
	ts := newTestServer(t)

	// Simulate client-side key derivation:
	// Master entropy never leaves device. Client derives K_auth and computes SHA-256(K_auth).
	kAuth := make([]byte, 32)
	if _, err := rand.Read(kAuth); err != nil {
		t.Fatalf("rand: %v", err)
	}
	kAuthHash := sha256.Sum256(kAuth)
	authHashHex := hex.EncodeToString(kAuthHash[:])

	// 1. Register with auth_hash in hex
	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Pixel 9 Pro",
		"auth_hash":   authHashHex,
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", regResp.StatusCode, regBody)
	}

	account, ok := regBody["account"].(map[string]any)
	if !ok || account["id"] == "" {
		t.Fatalf("expected account.id in response: %v", regBody)
	}
	accountID := account["id"].(string)

	// With auth_hash provided, server must NOT return or generate an account code
	if account["code"] != nil && account["code"] != "" {
		t.Fatalf("expected account.code to be omitted when auth_hash is used, got: %v", account["code"])
	}

	device, ok := regBody["device"].(map[string]any)
	if !ok || device["token"] == "" {
		t.Fatalf("expected device.token in response: %v", regBody)
	}
	firstToken := device["token"].(string)

	// 2. Add second device using the same hex auth_hash
	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Tablet",
		"auth_hash":   authHashHex,
	}, "")
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on add device, got %d: %v", addResp.StatusCode, addBody)
	}

	if addBody["account_id"] != accountID {
		t.Fatalf("expected account_id %s, got %v", accountID, addBody["account_id"])
	}
	secondDevice, ok := addBody["device"].(map[string]any)
	if !ok || secondDevice["token"] == "" {
		t.Fatalf("expected device.token in add device response: %v", addBody)
	}
	secondToken := secondDevice["token"].(string)

	if firstToken == secondToken {
		t.Fatal("second device token should be distinct from first")
	}

	// 3. Verify both tokens authenticate successfully on /v1/auth/devices
	for _, tok := range []string{firstToken, secondToken} {
		listResp, listBody := ts.request("GET", "/v1/auth/devices", nil, tok)
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("list devices with token failed: %d: %v", listResp.StatusCode, listBody)
		}
		devices, ok := listBody["devices"].([]any)
		if !ok || len(devices) != 2 {
			t.Fatalf("expected 2 devices in list, got: %v", listBody)
		}
	}
}

func TestZeroKnowledgeAuthLifecycle_Base64(t *testing.T) {
	ts := newTestServer(t)

	kAuth := make([]byte, 32)
	if _, err := rand.Read(kAuth); err != nil {
		t.Fatalf("rand: %v", err)
	}
	kAuthHash := sha256.Sum256(kAuth)
	authHashStdB64 := base64.StdEncoding.EncodeToString(kAuthHash[:])
	authHashURLB64 := base64.RawURLEncoding.EncodeToString(kAuthHash[:])

	// 1. Register with standard base64 auth_hash
	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Phone",
		"auth_hash":   authHashStdB64,
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", regResp.StatusCode, regBody)
	}

	account := regBody["account"].(map[string]any)
	accountID := account["id"].(string)
	if account["code"] != nil && account["code"] != "" {
		t.Fatalf("expected code to be omitted in ZK auth response: %v", account["code"])
	}

	// 2. Add device using URL-safe base64 auth_hash
	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Secondary Phone",
		"auth_hash":   authHashURLB64,
	}, "")
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on add device, got %d: %v", addResp.StatusCode, addBody)
	}
	if addBody["account_id"] != accountID {
		t.Fatalf("expected account_id %s, got %v", accountID, addBody["account_id"])
	}
}

func TestLegacyAuthLifecycle(t *testing.T) {
	ts := newTestServer(t)

	// 1. Register without auth_hash (server generates legacy account code)
	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Legacy Phone",
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", regResp.StatusCode, regBody)
	}

	account := regBody["account"].(map[string]any)
	accountID := account["id"].(string)
	displayCode, ok := account["code"].(string)
	if !ok || displayCode == "" {
		t.Fatalf("expected legacy code in response: %v", regBody)
	}
	if len(auth.NormalizeCode(displayCode)) != 20 {
		t.Fatalf("expected 20-symbol normalized code, got: %q", displayCode)
	}

	// 2. Add device using legacy code
	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Legacy Tablet",
		"code":        displayCode,
	}, "")
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on add device, got %d: %v", addResp.StatusCode, addBody)
	}
	if addBody["account_id"] != accountID {
		t.Fatalf("expected account_id %s, got %v", accountID, addBody["account_id"])
	}
}

func TestClientProvidedCodeLifecycle(t *testing.T) {
	ts := newTestServer(t)

	clientCode := "LTL-23456-789AB-CDEFG-HJKMN"

	// 1. Register with client-supplied code
	regResp, regBody := ts.request("POST", "/v1/auth/register", map[string]any{
		"device_name": "Client Code Phone",
		"code":        clientCode,
	}, "")
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", regResp.StatusCode, regBody)
	}
	account := regBody["account"].(map[string]any)
	if account["code"] != clientCode {
		t.Fatalf("expected account code %s, got %v", clientCode, account["code"])
	}

	// 2. Add device with the same client-supplied code
	addResp, addBody := ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "Secondary",
		"code":        clientCode,
	}, "")
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %v", addResp.StatusCode, addBody)
	}
}

func TestZeroKnowledgeAuth_ErrorCases(t *testing.T) {
	ts := newTestServer(t)

	// 1. Register with invalid auth_hash format -> 422
	resp, _ := ts.request("POST", "/v1/auth/register", map[string]any{
		"auth_hash": "not-valid-length-or-format",
	}, "")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d", resp.StatusCode)
	}

	// 2. Add device with non-existent auth_hash -> 401
	unknownHash := hex.EncodeToString(make([]byte, 32))
	resp, _ = ts.request("POST", "/v1/auth/devices", map[string]any{
		"auth_hash": unknownHash,
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}

	// 3. Add device with malformed auth_hash -> 401 (generic)
	resp, _ = ts.request("POST", "/v1/auth/devices", map[string]any{
		"auth_hash": "too-short",
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for malformed auth_hash, got %d", resp.StatusCode)
	}

	// 4. Add device with no code and no auth_hash -> 401
	resp, _ = ts.request("POST", "/v1/auth/devices", map[string]any{
		"device_name": "nameless",
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing credentials, got %d", resp.StatusCode)
	}
}
