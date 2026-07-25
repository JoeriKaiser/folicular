package contract

// Conformance tests: boot the REAL server, make REAL HTTP calls, and validate
// every response against openapi.yaml using kin-openapi's openapi3filter.
// This is what makes a hand-written spec safe - if a handler ever returns
// something the spec does not describe, these tests fail. This is the
// server-side enforcement mechanism (chosen over oapi-codegen strict mode,
// which only checks Go signatures, not the JSON actually sent).

import (
	"bytes"
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

	// Settings patch: 200 Me.
	h.do("PATCH", "/v1/me", map[string]any{"life_stage": "reproductive_peak"}, 200)

	// Sync push: 200 SyncPushResult.
	h.do("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{
			{
				"entity_type": "cycle",
				"data": map[string]any{
					"id": "019832e0-6c14-7000-8000-000000000001", "client_rev": "019832e0-6c14-7000-8000-000000000002",
					"created_at": "2026-07-01T08:00:00Z", "updated_at": "2026-07-01T08:00:00Z", "deleted_at": nil,
					"start_date": "2026-06-30", "end_date": nil, "length_days": nil, "bleeding_days": 5,
					"certainty": "recorded", "source": "manual", "notes": "",
				},
			},
		},
	}, 200)

	// Sync pull: 200 SyncPullResult.
	h.do("GET", "/v1/sync/pull?since=0", nil, 200)

	// Read models.
	h.do("GET", "/v1/cycles?from=2026-01-01&to=2026-12-31", nil, 200)
	h.do("GET", "/v1/days?from=2026-06-30&to=2026-07-02", nil, 200)

	// Estimates: 200 Prediction (insufficient history -> null windows, still valid).
	h.do("GET", "/v1/predictions/current", nil, 200)

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
	res := h.do("POST", "/v1/sync/push", map[string]any{
		"changes": []map[string]any{
			{
				"entity_type": "daily_entry",
				"data": map[string]any{
					"id": "019832e0-6c14-7000-8000-000000000003", "client_rev": "019832e0-6c14-7000-8000-000000000004",
					"created_at": "2026-07-01T08:00:00Z", "updated_at": "2026-07-01T08:00:00Z", "deleted_at": nil,
					"entry_date": "2026-07-01", "pain_level": 9, "mood_level": nil, "energy_level": nil, "notes": "",
				},
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
