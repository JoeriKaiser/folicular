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
		"/v1/cycles", "/v1/days", "/v1/predictions/current",
		"/v1/duo/invitations", "/v1/duo/links", "/v1/duo/links/{linkID}",
		"/v1/duo/links/{linkID}/grants", "/v1/duo/view",
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

	// Estimates must expose uncertainty and the disclaimer.
	pred := doc.Components.Schemas["Prediction"]
	if pred == nil {
		t.Fatal("spec is missing schema Prediction")
	}
	for _, field := range []string{"method", "basis", "disclaimer"} {
		if _, ok := pred.Value.Properties[field]; !ok {
			t.Errorf("Prediction is missing field %q", field)
		}
	}
	if conf := doc.Components.Schemas["Confidence"]; conf == nil {
		t.Error("spec is missing the Confidence enum")
	} else {
		for _, banned := range []string{"high", "certain", "sure"} {
			for _, v := range conf.Value.Enum {
				if v == banned {
					t.Errorf("Confidence must never include %q", banned)
				}
			}
		}
	}
}
