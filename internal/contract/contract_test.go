// Package contract guards the OpenAPI spec: it must parse, validate, and
// cover the routes the server actually exposes. The spec is the single
// source of truth for the Android client's generated DTOs, so a rotting
// spec is a client-breaking bug.
package contract

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../openapi/openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("openapi.yaml does not validate: %v", err)
	}
	return doc
}

func TestSpecIsValid(t *testing.T) {
	loadSpec(t)
}

func TestSpecCoversCoreRoutes(t *testing.T) {
	doc := loadSpec(t)
	paths := []string{
		"/healthz", "/readyz", "/version",
		"/v1/auth/register", "/v1/auth/devices", "/v1/auth/devices/{deviceID}",
		"/v1/me",
		"/v1/sync/push", "/v1/sync/pull",
		"/v1/duo/invitations", "/v1/duo/links", "/v1/duo/links/{linkID}",
		"/v1/duo/links/{linkID}/grants", "/v1/duo/view", "/v1/duo/payload",
		"/v1/duo/support-requests", "/v1/duo/support-requests/{requestID}/ack",
	}
	for _, p := range paths {
		if doc.Paths.Find(p) == nil {
			t.Errorf("spec is missing path %q", p)
		}
	}
}

func TestSpecSecurityAndSchemas(t *testing.T) {
	doc := loadSpec(t)

	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Fatal("spec must define the bearerAuth security scheme")
	}

	// The synchronized entity schemas are the client's DTO surface; they
	// must exist and carry the sync envelope.
	entities := []string{
		"CycleData", "BleedingObservationData", "DailyEntryData",
		"SymptomDefinitionData", "SymptomLogData",
		"BiomarkerObservationData", "MedicationLogData",
	}
	for _, name := range entities {
		s := doc.Components.Schemas[name]
		if s == nil {
			t.Errorf("spec is missing schema %q", name)
			continue
		}
		for _, field := range []string{"id", "client_rev", "created_at", "updated_at", "deleted_at"} {
			if _, ok := s.Value.Properties[field]; !ok {
				t.Errorf("schema %q is missing envelope field %q", name, field)
			}
		}
	}

	// Record content must be sealed: nothing on the wire may carry plaintext
	// observations. Server-side estimates were removed with the E2EE
	// migration, so there is no Prediction schema to police any more.
	for _, name := range []string{"SyncChangeInput", "SyncPullChange"} {
		sch := doc.Components.Schemas[name]
		if sch == nil {
			t.Fatalf("spec is missing schema %q", name)
		}
		if _, ok := sch.Value.Properties["ciphertext"]; !ok {
			t.Errorf("%s must carry a ciphertext field", name)
		}
		if _, ok := sch.Value.Properties["data"]; ok {
			t.Errorf("%s must not carry a plaintext data field", name)
		}
	}

	// The routing metadata the server still needs must stay present, or delta
	// pull and the last-write-wins guard cannot work on opaque payloads.
	in := doc.Components.Schemas["SyncChangeInput"]
	for _, field := range []string{"entity_type", "entity_id", "client_rev", "updated_at", "deleted"} {
		if _, ok := in.Value.Properties[field]; !ok {
			t.Errorf("SyncChangeInput is missing routing field %q", field)
		}
	}

	// The Duo projection must be relayed, not composed server-side.
	dv := doc.Components.Schemas["DuoView"]
	if dv == nil {
		t.Fatal("spec is missing schema DuoView")
	}
	if _, ok := dv.Value.Properties["payload"]; !ok {
		t.Error("DuoView must carry a sealed payload")
	}
	for _, banned := range []string{"cycle_day", "period_estimate", "mood", "energy"} {
		if _, ok := dv.Value.Properties[banned]; ok {
			t.Errorf("DuoView must not expose plaintext %q; it belongs in the sealed payload", banned)
		}
	}

	// Settings carry Art. 9 health data and must be sealed too.
	set := doc.Components.Schemas["Settings"]
	if set == nil {
		t.Fatal("spec is missing schema Settings")
	}
	for _, banned := range []string{"life_stage", "tracking_focus"} {
		if _, ok := set.Value.Properties[banned]; ok {
			t.Errorf("Settings must not expose plaintext %q", banned)
		}
	}

}
