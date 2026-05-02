// Package api_test contains ATDD acceptance tests for Story 6.1:
// OpenAPI Spec-First Setup (codegen Pipeline + StrictServerInterface + Live-Endpoint).
//
// GREEN PHASE — all tests active after implementation is complete.
//
// Covered Acceptance Criteria:
//   - AC#1  openapi.yaml declares OpenAPI 3.1.0 with all Admin API placeholder paths + BearerAuth
//   - AC#4  GET /api/v1/openapi.yaml — live unauthenticated endpoint, Content-Type: application/yaml
//   - AC#7  Response body contains "Nebu Admin API", status 200
package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/api"
)

// TestOpenAPIYAMLHandler_ServeSpec covers AC#4 and AC#7:
//   - GET /api/v1/openapi.yaml with no Authorization header must return 200
//   - Content-Type must be application/yaml
//   - Body must contain the title "Nebu Admin API"
//
// [P0] — core contract of the spec-first pipeline; failing this blocks all Epic 6 work.
func TestOpenAPIYAMLHandler_ServeSpec(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	// No Authorization header — endpoint must be unauthenticated per FR51.
	w := httptest.NewRecorder()

	api.OpenAPIYAMLHandler(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("[AC#7] expected HTTP 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/yaml") {
		t.Errorf("[AC#4] expected Content-Type to contain 'application/yaml', got %q", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Nebu Admin API") {
		t.Errorf("[AC#7] expected response body to contain 'Nebu Admin API'; body excerpt: %.200s", body)
	}
}

// TestOpenAPIYAMLHandler_NoAuthRequired covers AC#4 (FR51 unauthenticated) at the
// HANDLER level only: it asserts that the OpenAPIYAMLHandler function itself does
// not perform any auth check (no 401/403 even without an Authorization header).
//
// SCOPE LIMITATION: This test does NOT verify the full middleware wiring in
// gateway/cmd/gateway/main.go — i.e. that the route is registered outside the
// jwtMiddleware/sessionGuard chain. That contract is covered by inspection of
// main.go (mux.HandleFunc registration without wrapping middleware) and by the
// integration tests in gateway/test/integration/. If a future refactor wraps the
// handler inside an auth middleware at the mux level, this unit test will not
// catch it.
//
// [P0] — a secured spec endpoint would break all unauthenticated API tooling.
func TestOpenAPIYAMLHandler_NoAuthRequired(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	// Intentionally omit Authorization header — handler itself must not return 401/403.
	w := httptest.NewRecorder()

	api.OpenAPIYAMLHandler(w, req)

	resp := w.Result()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.Fatalf("[AC#4/FR51] handler must not perform auth check; got %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("[AC#4/FR51] expected 200 for unauthenticated request, got %d", resp.StatusCode)
	}
}

// TestOpenAPIYAMLHandler_SpecIsOpenAPI31 covers AC#1:
// The served YAML must declare OpenAPI version 3.1 (not 3.0.x).
//
// [P1] — oapi-codegen strict-server requires 3.1; a 3.0.x spec would break gen-api.
func TestOpenAPIYAMLHandler_SpecIsOpenAPI31(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	w := httptest.NewRecorder()

	api.OpenAPIYAMLHandler(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `openapi: "3.1.0"`) && !strings.Contains(body, "openapi: '3.1.0'") && !strings.Contains(body, "openapi: 3.1.0") {
		t.Errorf("[AC#1] expected spec to declare OpenAPI 3.1.0; body excerpt: %.300s", body)
	}
}

// TestOpenAPIYAMLHandler_SpecContainsAdminPaths covers AC#1 (placeholder paths):
// The served spec must expose all required Admin API route groups as paths.
//
// [P1] — missing paths mean the StrictServerInterface is incomplete and compile will fail.
func TestOpenAPIYAMLHandler_SpecContainsAdminPaths(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	w := httptest.NewRecorder()

	api.OpenAPIYAMLHandler(w, req)

	body := w.Body.String()

	requiredPaths := []string{
		"/admin/users",
		"/admin/rooms",
		"/admin/config",
		"/admin/metrics",
		"/compliance/access-requests",
	}

	for _, path := range requiredPaths {
		if !strings.Contains(body, path) {
			t.Errorf("[AC#1] expected spec to contain path %q", path)
		}
	}
}

// TestOpenAPIYAMLHandler_SpecHasInfoVersionAndServers covers AC#1 explicitly for the
// info.version and servers.url fields, which were previously only checked indirectly
// via "Nebu Admin API" in TestOpenAPIYAMLHandler_ServeSpec.
//
// [P1] — version drift or a missing servers entry would silently break client
// codegen tooling (Stoplight, Swagger UI, generated SDKs).
func TestOpenAPIYAMLHandler_SpecHasInfoVersionAndServers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	w := httptest.NewRecorder()

	api.OpenAPIYAMLHandler(w, req)

	body := w.Body.String()

	// AC#1 — info.version must be "1.0.0".
	if !strings.Contains(body, `version: "1.0.0"`) && !strings.Contains(body, "version: '1.0.0'") && !strings.Contains(body, "version: 1.0.0") {
		t.Errorf("[AC#1] expected spec to declare info.version 1.0.0; body excerpt: %.300s", body)
	}

	// AC#1 — servers entry must declare base URL "/api/v1".
	if !strings.Contains(body, `url: "/api/v1"`) && !strings.Contains(body, "url: '/api/v1'") && !strings.Contains(body, "url: /api/v1") {
		t.Errorf("[AC#1] expected spec to declare servers.url /api/v1; body excerpt: %.300s", body)
	}
}

// TestOpenAPIYAMLHandler_SpecContainsBearerAuth covers AC#1 (BearerAuth security scheme):
// The served spec must declare a BearerAuth JWT security scheme.
//
// [P1] — missing security scheme means generated code lacks auth annotations.
func TestOpenAPIYAMLHandler_SpecContainsBearerAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	w := httptest.NewRecorder()

	api.OpenAPIYAMLHandler(w, req)

	body := w.Body.String()

	if !strings.Contains(body, "BearerAuth") {
		t.Errorf("[AC#1] expected spec to contain 'BearerAuth' security scheme; body excerpt: %.300s", body)
	}
	if !strings.Contains(body, "bearer") {
		t.Errorf("[AC#1] expected spec to contain 'bearer' scheme type; body excerpt: %.300s", body)
	}
}
