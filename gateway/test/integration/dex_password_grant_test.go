//go:build integration

package integration_test

// dex_password_grant_test.go — Story 5.29d AC2 (FB-E5-08)
//
// RED-PHASE: These tests FAIL until:
//   1. `dev/dex/config.yaml` has `password` removed from oauth2.grantTypes AND
//      `passwordConnector: local` removed from oauth2 section.
//   2. The config-parse test passes once config.yaml is updated.
//
// Tests:
//   - TestDexPasswordGrant_ConfigHasNoPasswordGrant: reads dev/dex/config.yaml
//     as raw text and asserts `- password` and `passwordConnector:` are absent.
//   - TestDexPasswordGrant_Rejected: HTTP smoke (integration-only, requires
//     running Dex at DEX_URL env or http://localhost:5556) — POST to /dex/token
//     with grant_type=password → must NOT return 200 with access_token.
//
// AC coverage: AC2 (FB-E5-08) — Dex dev config enforces Authorization Code only.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// dexConfigPath is the path to the Dex development config relative to project root.
// Integration tests run from the gateway/ directory, so we traverse up.
const dexConfigPath = "../dev/dex/config.yaml"

// ─── AC2 Test 1: config.yaml has no password grant ────────────────────────────
//
// Given: dev/dex/config.yaml as committed
// When:  the raw file content is inspected line by line
// Then:  the file does NOT contain "- password" as a grantType entry
//        AND does NOT contain a "passwordConnector:" line
//
// Strategy: raw string matching avoids introducing a YAML library dependency.
// The Dex config format is stable enough that these patterns uniquely identify
// the offending configuration lines.
//
// RED-PHASE: FAILS until config.yaml is updated to remove the password grant.

func TestDexPasswordGrant_ConfigHasNoPasswordGrant(t *testing.T) {
	data, err := os.ReadFile(dexConfigPath)
	if err != nil {
		// Try alternate path (when running from project root).
		data, err = os.ReadFile("dev/dex/config.yaml")
		if err != nil {
			t.Fatalf(
				"cannot read Dex config (tried %s and dev/dex/config.yaml): %v — "+
					"run this test from the gateway/ or project root directory",
				dexConfigPath, err,
			)
		}
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Assert: "- password" grant type line must NOT appear in the grantTypes list.
	// Matches YAML list items like "    - password" (with any leading whitespace).
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "- password" {
			t.Errorf(
				"AC2 FAIL: dev/dex/config.yaml line %d contains '- password' in oauth2.grantTypes — "+
					"remove this line to enforce Authorization Code + PKCE only (CLAUDE.md policy). "+
					"Line: %q",
				i+1, line,
			)
		}
	}

	// Assert: passwordConnector must be absent.
	if strings.Contains(content, "passwordConnector:") {
		t.Errorf(
			"AC2 FAIL: dev/dex/config.yaml contains 'passwordConnector:' — " +
				"remove the passwordConnector entry from the oauth2 section.",
		)
	}
}

// ─── AC2 Test 2: live Dex rejects password grant ──────────────────────────────
//
// Given: Dex is running (DEX_URL env or http://localhost:5556)
// When:  POST /dex/token with grant_type=password, username, password, client_id
// Then:  response is NOT 200 with an access_token
//        (expected: 400 Bad Request with error=unsupported_grant_type or similar)
//
// RED-PHASE: FAILS if Dex still has the password grant enabled.
// SKIP: if Dex is not reachable (container not running).

func TestDexPasswordGrant_Rejected(t *testing.T) {
	dexURL := os.Getenv("DEX_URL")
	if dexURL == "" {
		dexURL = "http://localhost:5556"
	}

	tokenURL := fmt.Sprintf("%s/dex/token", dexURL)

	// Attempt a Resource Owner Password Credentials (ROPC) request.
	// This is the grant type that must be disabled in dev Dex.
	form := url.Values{
		"grant_type": {"password"},
		"username":   {"kai@example.com"},
		"password":   {"password"},
		"client_id":  {"nebu-gateway"},
		"scope":      {"openid email profile"},
	}

	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		t.Skipf(
			"Dex not reachable at %s (%v) — skipping live smoke test (start with: make dev)",
			tokenURL, err,
		)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// A 200 response with an access_token means the password grant is accepted — FAIL.
	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		if decErr := json.Unmarshal(body, &result); decErr == nil {
			if _, hasToken := result["access_token"]; hasToken {
				truncated := string(body)
				if len(truncated) > 200 {
					truncated = truncated[:200] + "..."
				}
				t.Errorf(
					"AC2 FAIL: Dex returned 200 with access_token for grant_type=password — "+
						"the password grant must be disabled in dev/dex/config.yaml. "+
						"Response body: %s",
					truncated,
				)
				return
			}
		}
		t.Logf("Warning: Dex returned 200 without access_token for password grant. Body: %s", string(body))
		return
	}

	// Any non-200 is acceptable — Dex has rejected the grant.
	// Expected: 400 (unsupported_grant_type), 401, or 501.
	t.Logf("Dex returned status %d for password grant (expected non-200) — OK", resp.StatusCode)

	// Log the rejection reason for observability.
	var errResponse map[string]string
	if json.Unmarshal(body, &errResponse) == nil {
		if errType, ok := errResponse["error"]; ok {
			t.Logf("Dex rejection reason: error=%q — OK", errType)
		}
	}
}
