package contract

// Conformance tests: boot the REAL server, make REAL HTTP calls, and validate
// every response against openapi.yaml using kin-openapi's openapi3filter.
// This is what makes a hand-written spec safe - if a handler ever returns
// something the spec does not describe, these tests fail. This is the
// server-side enforcement mechanism (chosen over oapi-codegen strict mode,
// which only checks Go signatures, not the JSON actually sent).

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"folicular/internal/auth"
	"folicular/internal/db"
	"folicular/internal/db/dbgen"
	"folicular/internal/server"
)

type harness struct {
	t          *testing.T
	base       string
	specServer string
	router     routers.Router
	token      string
}

func newHarness(t *testing.T) *harness {
	return newHarnessWith(t, nil)
}

func newHarnessWith(t *testing.T, inviteCodes map[string]struct{}) *harness {
	t.Helper()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "conformance.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := server.NewRouter(log, dbgen.New(sqlDB), sqlDB, "test", "https://luteal.app/duo", nil, inviteCodes)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	doc := loadSpec(t)
	specRouter, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build spec router: %v", err)
	}
	specServer := ""
	if len(doc.Servers) > 0 {
		specServer = doc.Servers[0].URL
	}

	return &harness{t: t, base: srv.URL, specServer: specServer, router: specRouter}
}

// do performs a request and validates the RESPONSE against the spec. It
// returns the decoded body for chaining. This is the core guarantee: the
// server can never send a response the contract does not describe.
func (h *harness) do(method, path string, body any, wantStatus int) map[string]any {
	h.t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal request: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.base+path, reqBody)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != wantStatus {
		h.t.Fatalf("%s %s: status = %d, want %d; body=%s", method, path, resp.StatusCode, wantStatus, respBytes)
	}

	h.validateResponse(method, path, resp.Header, resp.StatusCode, respBytes)

	if len(respBytes) == 0 || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(respBytes, &out); err != nil {
		h.t.Fatalf("%s %s: response is not a JSON object: %v", method, path, err)
	}
	return out
}

// validateResponse checks a real response against the spec. The matching
// request is rebuilt against the spec's declared server URL because the
// gorillamux router keys routes on that host, while the real call went to the
// httptest server's random port.
func (h *harness) validateResponse(method, path string, header http.Header, status int, body []byte) {
	h.t.Helper()

	specReq, err := http.NewRequest(method, h.specServer+path, nil)
	if err != nil {
		h.t.Fatalf("build spec request: %v", err)
	}
	specReq.Header = header

	route, _, err := h.router.FindRoute(specReq)
	if err != nil {
		h.t.Fatalf("spec has no route for %s %s: %v", method, path, err)
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: specReq,
			Route:   route,
			Options: &openapi3filter.Options{
				AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			},
		},
		Status: status,
		Header: header,
		Body:   io.NopCloser(bytes.NewReader(body)),
	}
	if err := openapi3filter.ValidateResponse(specReq.Context(), input); err != nil {
		h.t.Fatalf("response for %s %s violates the spec: %v\nbody=%s", method, path, err, body)
	}
}

func TestConformance_CoreFlow(t *testing.T) {
	h := newHarness(t)

	// Register: 201, and the response must match RegisterResponse schema.
	reg := h.do("POST", "/v1/auth/register", map[string]any{"device_name": "conf"}, 201)
	h.token = reg["device"].(map[string]any)["token"].(string)

	// /v1/me: 200 Me.
	h.do("GET", "/v1/me", nil, 200)

	// Device list: 200 (validates the Device schema incl. nullable last_seen).
	h.do("GET", "/v1/auth/devices", nil, 200)

	// Settings patch: 200 Me. Settings are sealed client-side, so the server
	// receives an opaque blob and performs no merge.
	h.do("PATCH", "/v1/me", map[string]any{"settings": sealedBlob()}, 200)

	// Sync push: 200 SyncPushResult. Content is sealed; the server validates
	// only the routing envelope.
	h.do("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{
			{
				"entity_type": "cycle",
				"entity_id":   "019832e0-6c14-7000-8000-000000000001",
				"client_rev":  "019832e0-6c14-7000-8000-000000000002",
				"updated_at":  "2026-07-01T08:00:00Z",
				"deleted":     false,
				"ciphertext":  sealedBlob(),
			},
		},
	}, 200)

	// Sync pull: 200 SyncPullResult.
	h.do("GET", "/v1/sync/pull?since=0", nil, 200)

	// /v1/cycles, /v1/days and /v1/predictions/current were removed with the
	// E2EE migration: all three required reading record content. Reads and
	// estimates are served from the client's local store.

	// Duo: invitation 201, then view 404 for a tracker with no active link is
	// not the case here (tracker has a pending link) - view returns 404 because
	// the link is pending, not active. Validate the 404 problem shape.
	inv := h.do("POST", "/v1/duo/invitations", nil, 201)
	if inv["pairing_url"] == nil || inv["pairing_code"] == nil {
		t.Fatalf("invitation missing pairing_url/pairing_code: %v", inv)
	}
	h.do("GET", "/v1/duo/view", nil, 404)

	// Ops.
	h.do("GET", "/healthz", nil, 200)
	h.do("GET", "/version", nil, 200)
}

func TestConformance_ErrorShapes(t *testing.T) {
	h := newHarness(t)

	// Unauthenticated /v1/me -> 401 problem+json.
	h.do("GET", "/v1/me", nil, 401)

	// Register then push an invalid record -> 200 with a rejected entry (the
	// batch endpoint reports per-change validation in-band).
	reg := h.do("POST", "/v1/auth/register", map[string]any{"device_name": "conf"}, 201)
	h.token = reg["device"].(map[string]any)["token"].(string)
	// The server can no longer reject on content (it cannot read it), so the
	// in-band rejection path is exercised with an invalid routing envelope:
	// a non-deleted change carrying no ciphertext.
	res := h.do("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{
			{
				"entity_type": "daily_entry",
				"entity_id":   "019832e0-6c14-7000-8000-000000000003",
				"client_rev":  "019832e0-6c14-7000-8000-000000000004",
				"updated_at":  "2026-07-01T08:00:00Z",
				"deleted":     false,
				"ciphertext":  nil,
			},
		},
	}, 200)
	if len(res["rejected"].([]any)) != 1 {
		t.Fatalf("expected 1 rejected change, got %v", res["rejected"])
	}

	// Malformed body -> 400 problem+json.
	h.do("POST", "/v1/auth/register", map[string]any{"device_name": 123}, 400)
}

func TestConformance_InviteGate(t *testing.T) {
	codes := map[string]struct{}{auth.HashInviteCode("BETA-1234"): {}}
	h := newHarnessWith(t, codes)

	// Missing code -> 401 problem+json.
	h.do("POST", "/v1/auth/register", map[string]any{"device_name": "x"}, 401)
	// Wrong code -> 401 problem+json (generic, no enumeration).
	h.do("POST", "/v1/auth/register", map[string]any{"device_name": "x", "invite_code": "NOPE"}, 401)
	// Correct code -> 201.
	reg := h.do("POST", "/v1/auth/register", map[string]any{"device_name": "x", "invite_code": "BETA-1234"}, 201)
	if reg["device"] == nil {
		t.Fatalf("expected device in register response: %v", reg)
	}
}

// sealedBlob returns a base64 payload shaped like a real sealed record:
// 0x01 version || 12-byte nonce || ciphertext || 16-byte tag. The server never
// decrypts it, so the contents are irrelevant - only the shape matters.
func sealedBlob() string {
	buf := make([]byte, 1+12+32+16)
	buf[0] = 0x01
	if _, err := rand.Read(buf[1:]); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
