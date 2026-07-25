package contract

// Shared conformance fixtures: golden response bodies in ../../conformance/,
// each tagged with the route + status it represents. This test validates every
// fixture against the OpenAPI spec, so the artifacts the Android client's
// ConformanceFixturesTest decodes are guaranteed spec-conformant. Both repos
// reference the same fixtures - the server proves it can produce this wire
// format, the client proves it can parse it.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

type fixture struct {
	Name   string          `json:"name"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func TestConformanceFixtures_MatchSpec(t *testing.T) {
	doc := loadSpec(t)
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build spec router: %v", err)
	}
	specServer := ""
	if len(doc.Servers) > 0 {
		specServer = doc.Servers[0].URL
	}

	files, err := filepath.Glob("../../conformance/*.json")
	if err != nil {
		t.Fatalf("glob conformance fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no conformance fixtures found in ../../conformance/")
	}
	sort.Strings(files)

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		var fx fixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: invalid fixture envelope: %v", file, err)
		}
		if fx.Method == "" || fx.Path == "" || fx.Status == 0 {
			t.Fatalf("%s: fixture must declare method, path, and status", file)
		}

		t.Run(fx.Name, func(t *testing.T) {
			req, err := http.NewRequest(fx.Method, specServer+fx.Path, nil)
			if err != nil {
				t.Fatalf("build spec request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			route, _, err := router.FindRoute(req)
			if err != nil {
				t.Fatalf("spec has no route for %s %s: %v", fx.Method, fx.Path, err)
			}
			input := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{
					Request: req,
					Route:   route,
					Options: &openapi3filter.Options{
						AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
					},
				},
				Status: fx.Status,
				Header: http.Header{"Content-Type": []string{"application/json"}},
				Body:   io.NopCloser(bytes.NewReader(fx.Body)),
			}
			if err := openapi3filter.ValidateResponse(req.Context(), input); err != nil {
				t.Fatalf("fixture %s violates the spec for %s %s (%d): %v\nbody=%s",
					fx.Name, fx.Method, fx.Path, fx.Status, err, fx.Body)
			}
		})
	}
}
