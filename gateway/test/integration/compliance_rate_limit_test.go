//go:build integration

package integration_test

// compliance_rate_limit_test.go — Story 5.29b: AC1 (FB-53-01)
//
// ALL tests in this file are expected to FAIL until Story 5.29b is implemented.
// Failing reason: compliance and admin anonymize/key-delete routes are NOT currently
// wrapped in strictRL middleware. The 11th request in a window returns 200 instead of 429.
//
// Build tag: integration
// Run with:  go test -tags=integration ./gateway/test/integration/...
//
// Test strategy:
//   - Hits the live gateway directly via matrixURL (port 8008 — Matrix/compliance routes).
//   - Uses NEBU_RATE_LIMIT_DISABLED=false (production mode in integration stack).
//   - Each sub-test gets a unique X-Forwarded-For IP to ensure an independent bucket.
//   - strictRL config for compliance routes (per story): 10 req/min, burst 10.
//     → 11th consecutive request must receive 429 M_LIMIT_EXCEEDED.
//
// AC1 coverage:
//   - Parametrized table over ALL 8 compliance / admin routes.
//   - TestComplianceRoutes_RateLimited_429 — burst to 11, assert 429 on last.

import (
	"fmt"
	"net/http"
	"testing"
)

// complianceRouteCase describes one route to be rate-limit tested.
type complianceRouteCase struct {
	name   string
	method string
	path   string
}

// complianceRoutes lists every /api/v1/compliance/* and /api/v1/admin/users/*/…
// route that AC1 requires to be wrapped in strictRL (10 req/min, burst 10).
var complianceRoutes = []complianceRouteCase{
	{
		name:   "POST /access-requests",
		method: http.MethodPost,
		path:   "/api/v1/compliance/access-requests",
	},
	{
		name:   "POST /access-requests/{id}/approve",
		method: http.MethodPost,
		path:   "/api/v1/compliance/access-requests/test-id/approve",
	},
	{
		name:   "POST /access-requests/{id}/reject",
		method: http.MethodPost,
		path:   "/api/v1/compliance/access-requests/test-id/reject",
	},
	{
		name:   "GET /access-requests",
		method: http.MethodGet,
		path:   "/api/v1/compliance/access-requests",
	},
	{
		name:   "POST /access-requests/{id}/session",
		method: http.MethodPost,
		path:   "/api/v1/compliance/access-requests/test-id/session",
	},
	{
		name:   "GET /export",
		method: http.MethodGet,
		path:   "/api/v1/compliance/export",
	},
	{
		name:   "DELETE /admin/users/{id}/keys",
		method: http.MethodDelete,
		path:   "/api/v1/admin/users/test-user-id/keys",
	},
	{
		name:   "POST /admin/users/{id}/anonymize",
		method: http.MethodPost,
		path:   "/api/v1/admin/users/test-user-id/anonymize",
	},
	{
		// MINOR-1 (TEA): revoke-session is also in scope for AC1's strictRL wrap
		// (registered in main.go around line 848). Without this row, an accidental
		// regression that drops strictRL from the revoke route would not be caught
		// by this parametrized table.
		name:   "POST /admin/compliance/sessions/{sessionId}/revoke",
		method: http.MethodPost,
		path:   "/api/v1/admin/compliance/sessions/test-session-id/revoke",
	},
}

// TestComplianceRoutes_RateLimited_429 verifies that every compliance route is
// wrapped in strictRL (10 req/min, burst 10):
//   - Requests 1–10 may return any non-429 status (auth errors etc. are fine).
//   - Request 11 from the SAME IP must return 429 M_LIMIT_EXCEEDED.
//
// Failing reason: routes are currently registered without strictRL → 11th request
// does NOT return 429 (it returns 401/403/404/etc.) until 5.29b is implemented.
func TestComplianceRoutes_RateLimited_429(t *testing.T) {
	if matrixURL == "" {
		t.Skip("NEBU_TEST_MATRIX_URL not set — skipping integration rate-limit test")
	}

	client := &http.Client{
		// Do not follow redirects — we want the raw 429.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for i, tc := range complianceRoutes {
		tc := tc // capture
		// Give each route its own unique IP so buckets are independent.
		uniqueIP := fmt.Sprintf("10.29.%d.1", i+1)

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			url := matrixURL + tc.path

			// Send 10 requests — all should pass (any status except 429 is acceptable).
			for req := 1; req <= 10; req++ {
				httpReq, err := http.NewRequest(tc.method, url, nil)
				if err != nil {
					t.Fatalf("request %d: http.NewRequest: %v", req, err)
				}
				httpReq.Header.Set("X-Forwarded-For", uniqueIP+", 10.0.0.1")

				resp, err := client.Do(httpReq)
				if err != nil {
					t.Fatalf("request %d: Do: %v", req, err)
				}
				resp.Body.Close()

				if resp.StatusCode == http.StatusTooManyRequests {
					t.Fatalf("request %d: got premature 429 before burst exhausted (burst should be 10)", req)
				}
			}

			// 11th request — MUST be rate-limited.
			httpReq, err := http.NewRequest(tc.method, url, nil)
			if err != nil {
				t.Fatalf("11th request: http.NewRequest: %v", err)
			}
			httpReq.Header.Set("X-Forwarded-For", uniqueIP+", 10.0.0.1")

			resp, err := client.Do(httpReq)
			if err != nil {
				t.Fatalf("11th request: Do: %v", err)
			}
			resp.Body.Close()

			// RED-PHASE ASSERTION: this will FAIL until strictRL is applied to the route.
			if resp.StatusCode != http.StatusTooManyRequests {
				t.Errorf("11th request to %s %s: expected 429 Too Many Requests (strictRL), got %d — "+
					"route is not wrapped in strictRL middleware (Story 5.29b AC1 not implemented)",
					tc.method, tc.path, resp.StatusCode)
			}
		})
	}
}
