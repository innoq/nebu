package matrix

// Story 5.24: SSO Redirect Scheme Allowlist
//
// These tests verify the allowlist-based isRedirectURLAllowed behaviour
// introduced in Story 5.24 (replacing the previous "any non-http scheme" logic).
//
// AC 1: isRedirectURLAllowed rejects any URL whose scheme is not in:
//         - https (always allowed)
//         - http only if host is localhost or 127.0.0.1
//         - NEBU_SSO_REDIRECT_SCHEMES configured deep-link schemes
// AC 2: Default allowlist includes: element, io.element.fluffychat, fluffychat
// AC 3: Explicit deny list: javascript, data, file, vbscript, blob (blocklist wins)
// AC 4: Rejected redirect → 400, M_INVALID_PARAM, scheme NOT echoed in response body
// AC 5: Named unit tests cover every category

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nebu/nebu/internal/auth"
)

// ─── AC 1 + AC 2 + AC 3: isRedirectURLAllowed ─────────────────────────────────

// TestSSORedirect_AllowsHTTPS verifies that https:// URLs are always allowed
// regardless of the configured allowlist (AC 1).
func TestSSORedirect_AllowsHTTPS(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want bool
	}{
		{"https://app.example.com/login", true},
		{"https://matrix.example.org/", true},
		{"https://localhost/callback", true},
		{"https://127.0.0.1:8080/cb", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			got := isRedirectURLAllowed(tc.url)
			if got != tc.want {
				t.Errorf("isRedirectURLAllowed(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestSSORedirect_AllowsHTTPLocalhost verifies http:// is allowed only for
// loopback hosts (AC 1).
func TestSSORedirect_AllowsHTTPLocalhost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want bool
	}{
		{"http://localhost:7070/", true},
		{"http://127.0.0.1:3000/callback", true},
		{"http://localhost/", true},
		// Non-loopback http MUST be rejected.
		{"http://evil.example.com/steal", false},
		{"http://example.com/", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			got := isRedirectURLAllowed(tc.url)
			if got != tc.want {
				t.Errorf("isRedirectURLAllowed(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestSSORedirect_AllowsDefaultDeepLinkSchemes verifies that the default
// allowlist contains element, io.element.fluffychat, and fluffychat (AC 2).
// Regression guard: these schemes must remain allowed after the allowlist change.
func TestSSORedirect_AllowsDefaultDeepLinkSchemes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want bool
	}{
		{"element://login?loginToken=abc", true},
		{"io.element.fluffychat://login?loginToken=abc", true},
		{"fluffychat://login?loginToken=abc", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			got := isRedirectURLAllowed(tc.url)
			if got != tc.want {
				t.Errorf("isRedirectURLAllowed(%q) = %v, want %v (default allowlist must include this scheme)", tc.url, got, tc.want)
			}
		})
	}
}

// TestSSORedirect_AllowsConfiguredCustomScheme verifies that operator-configured
// schemes from NEBU_SSO_REDIRECT_SCHEMES are accepted (AC 1).
func TestSSORedirect_AllowsConfiguredCustomScheme(t *testing.T) {
	t.Parallel()

	extraSchemes := []string{"myapp", "com.example.client"}

	cases := []struct {
		url  string
		want bool
	}{
		{"myapp://callback?loginToken=xyz", true},
		{"com.example.client://auth/callback", true},
		// Schemes NOT in the list must still be rejected.
		{"otherscheme://open", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			got := isRedirectURLAllowedWithSchemes(tc.url, extraSchemes)
			if got != tc.want {
				t.Errorf("isRedirectURLAllowedWithSchemes(%q, %v) = %v, want %v", tc.url, extraSchemes, got, tc.want)
			}
		})
	}
}

// TestSSORedirect_RejectsUnconfiguredCustomScheme verifies that arbitrary
// custom schemes are NOT allowed when they are not in the allowlist (AC 1).
func TestSSORedirect_RejectsUnconfiguredCustomScheme(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want bool
	}{
		// These must be rejected because they are not in the default allowlist
		// and no extra schemes are configured.
		{"intent://open#Intent;scheme=malicious;end", false},
		{"myunknownapp://callback", false},
		{"customscheme://some/path", false},
		{"ftp://files.example.com/", false},
		{"ssh://host/path", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			// Using default allowlist (no extra schemes).
			got := isRedirectURLAllowed(tc.url)
			if got != tc.want {
				t.Errorf("isRedirectURLAllowed(%q) = %v, want %v — scheme must be in the allowlist", tc.url, got, tc.want)
			}
		})
	}
}

// TestSSORedirect_RejectsJavaScript verifies that the javascript: scheme is
// always denied, even if it somehow appeared in the operator allowlist (AC 3).
func TestSSORedirect_RejectsJavaScript(t *testing.T) {
	t.Parallel()

	cases := []string{
		"javascript:alert(1)",
		"javascript://comment%0aalert(1)",
		"JAVASCRIPT:alert(1)",
		"Javascript:void(0)",
	}

	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if isRedirectURLAllowed(raw) {
				t.Errorf("isRedirectURLAllowed(%q) = true, want false — javascript: is in the hard-deny blocklist", raw)
			}
		})
	}
}

// TestSSORedirect_RejectsDataURL verifies that the data: scheme is always
// denied (AC 3). A data: URL containing a loginToken query parameter would
// allow exfiltration via base64-encoded HTML.
func TestSSORedirect_RejectsDataURL(t *testing.T) {
	t.Parallel()

	cases := []string{
		"data:text/html,<script>alert(1)</script>",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"DATA:text/plain,hello",
	}

	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if isRedirectURLAllowed(raw) {
				t.Errorf("isRedirectURLAllowed(%q) = true, want false — data: is in the hard-deny blocklist", raw)
			}
		})
	}
}

// TestSSORedirect_RejectsBlocklistedSchemes is a table-driven test that
// covers every explicitly denied scheme from AC 3 and additional edge cases.
// Meets the story requirement of ≥10 scheme/host combinations in one table.
func TestSSORedirect_RejectsBlocklistedSchemes(t *testing.T) {
	t.Parallel()

	type row struct {
		name string
		url  string
		want bool
	}

	rows := []row{
		// ── Hard-deny blocklist (AC 3) ──────────────────────────────────────
		{"javascript plain", "javascript:alert(document.cookie)", false},
		{"javascript case-insensitive", "JAVASCRIPT:alert(1)", false},
		{"data html", "data:text/html,<h1>evil</h1>", false},
		{"data base64", "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==", false},
		{"file local", "file:///etc/passwd", false},
		{"file windows", "file:///C:/Windows/System32/drivers/etc/hosts", false},
		{"vbscript", "vbscript:msgbox(1)", false},
		{"blob", "blob:https://example.com/uuid", false},
		// ── Allowed: https (AC 1) ────────────────────────────────────────────
		{"https public", "https://chat.example.com/login", true},
		{"https localhost", "https://localhost/cb", true},
		// ── Allowed: http loopback only (AC 1) ──────────────────────────────
		{"http localhost", "http://localhost:7070/", true},
		{"http 127.0.0.1", "http://127.0.0.1:3000/", true},
		// ── Denied: http non-loopback (AC 1) ────────────────────────────────
		{"http non-loopback", "http://attacker.example.com/harvest", false},
		// ── Allowed: default deep-link schemes (AC 2) ────────────────────────
		{"element scheme", "element://login?loginToken=tok", true},
		{"fluffychat scheme", "fluffychat://login?loginToken=tok", true},
		{"io.element.fluffychat scheme", "io.element.fluffychat://login?loginToken=tok", true},
		// ── Denied: unknown custom schemes not in allowlist (AC 1) ──────────
		{"intent scheme android", "intent://open#Intent;end", false},
		{"ftp scheme", "ftp://files.example.com/", false},
		{"custom unknown", "myunknownapp://callback", false},
		// ── Edge cases ───────────────────────────────────────────────────────
		{"empty string", "", false},
		{"no scheme bare string", "just-a-string", false},
		{"relative url", "/relative/path", false},
	}

	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got := isRedirectURLAllowed(r.url)
			if got != r.want {
				t.Errorf("isRedirectURLAllowed(%q) = %v, want %v", r.url, got, r.want)
			}
		})
	}
}

// ─── AC 4: HTTP response — scheme not echoed, correct error code ──────────────

// TestSSORedirect_ErrorDoesNotLeakScheme verifies that a rejected redirect
// returns HTTP 400, errcode M_INVALID_PARAM, and that the response body does
// NOT echo the hostile scheme or its payload back (AC 4 — no XSS vector).
//
// Two-layer approach:
//  1. isRedirectURLAllowed must return false for all hostile schemes (AC 3).
//  2. GetSSORedirect must write a 400/M_INVALID_PARAM response and NOT
//     echo the scheme in the body.
func TestSSORedirect_ErrorDoesNotLeakScheme(t *testing.T) {
	t.Parallel()

	oidcSrv, _ := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	hostileURLs := []string{
		"javascript:alert(document.cookie)",
		"javascript://comment%0aalert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"vbscript:msgbox(1)",
	}

	// ── Layer 1: function-level assertions (fast) ────────────────────────────
	for _, hostile := range hostileURLs {
		hostile := hostile
		t.Run("allowlist_rejects/"+hostile, func(t *testing.T) {
			t.Parallel()
			if isRedirectURLAllowed(hostile) {
				t.Errorf("isRedirectURLAllowed(%q) = true, want false — hostile scheme must be rejected", hostile)
			}
		})
	}

	// ── Layer 2: handler-level assertions — 400 + no scheme in body ──────────
	// The OIDC provider is provided so the test does not panic even if the
	// allowlist check is not yet fixed (provider.Inner() is reachable from the
	// real OIDC mock server, so no nil-pointer). Once the allowlist is correct,
	// GetSSORedirect returns before ever touching the provider.
	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	for _, hostile := range hostileURLs {
		hostile := hostile
		t.Run("handler_400/"+hostile, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(
				http.MethodGet,
				"/_matrix/client/v3/login/sso/redirect?redirectUrl="+hostile,
				nil,
			)
			req.Host = "localhost:8008"
			w := httptest.NewRecorder()

			h := NewLoginHandler(LoginConfig{
				DisplayName:  "Test SSO",
				Provider:     provider,
				ClientID:     "nebu-gateway",
				ClientSecret: "secret",
			})
			h.GetSSORedirect(w, req)

			// AC 4a: must return 400 — not 302.
			if w.Code != http.StatusBadRequest {
				t.Errorf("hostile URL %q: expected HTTP 400, got %d (body: %s)",
					hostile, w.Code, w.Body.String())
			}

			// AC 4b: errcode must be M_INVALID_PARAM.
			var errResp matrixError
			body := w.Body.String()
			if err := json.NewDecoder(strings.NewReader(body)).Decode(&errResp); err != nil {
				t.Fatalf("hostile URL %q: cannot decode error body: %v\nbody: %s", hostile, err, body)
			}
			if errResp.ErrCode != "M_INVALID_PARAM" {
				t.Errorf("hostile URL %q: expected errcode M_INVALID_PARAM, got %q",
					hostile, errResp.ErrCode)
			}

			// AC 4c: the scheme name and payload must NOT appear in the response body.
			// This prevents using the error response as an XSS reflection vector.
			forbiddenTerms := []string{
				"javascript",
				"alert",
				"data:",
				"file://",
				"vbscript",
				"<script",
			}
			for _, term := range forbiddenTerms {
				if strings.Contains(strings.ToLower(body), strings.ToLower(term)) {
					t.Errorf("hostile URL %q: response body must not contain %q — XSS reflection risk\nBody: %s",
						hostile, term, body)
				}
			}
		})
	}
}

// TestSSORedirect_ErrorCodeIsInvalidParam verifies the exact errcode value for
// any rejected redirect URL (AC 4 — regression guard for errcode spelling).
//
// Uses intent:// — a scheme that is not in the hard-deny blocklist but also
// not in the default allowlist (unlike element/fluffychat).
func TestSSORedirect_ErrorCodeIsInvalidParam(t *testing.T) {
	t.Parallel()

	oidcSrv, _ := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/login/sso/redirect?redirectUrl=intent://open",
		nil,
	)
	req.Host = "localhost:8008"
	w := httptest.NewRecorder()

	h := NewLoginHandler(LoginConfig{
		DisplayName:  "Test SSO",
		Provider:     auth.NewProvider(context.Background(), oidcSrv.URL),
		ClientID:     "nebu-gateway",
		ClientSecret: "secret",
	})
	h.GetSSORedirect(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for intent:// scheme, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("cannot decode error body: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp.ErrCode)
	}
}

// ─── Blocklist wins over allowlist (defense in depth) ─────────────────────────

// TestSSORedirect_BlocklistWinsOverAllowlist verifies that a blocklisted scheme
// is rejected even if an operator accidentally added it to the custom allowlist
// via NEBU_SSO_REDIRECT_SCHEMES (AC 3 — blocklist takes precedence).
func TestSSORedirect_BlocklistWinsOverAllowlist(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url          string
		extraSchemes []string
		want         bool
	}{
		{
			url:          "javascript:alert(1)",
			extraSchemes: []string{"javascript"}, // oops — operator misconfiguration
			want:         false,                   // blocklist must win
		},
		{
			url:          "data:text/html,evil",
			extraSchemes: []string{"data"},
			want:         false,
		},
		{
			url:          "file:///etc/passwd",
			extraSchemes: []string{"file"},
			want:         false,
		},
		{
			url:          "vbscript:msgbox(1)",
			extraSchemes: []string{"vbscript"},
			want:         false,
		},
		{
			url:          "blob:https://example.com/uuid",
			extraSchemes: []string{"blob"},
			want:         false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			got := isRedirectURLAllowedWithSchemes(tc.url, tc.extraSchemes)
			if got != tc.want {
				t.Errorf("isRedirectURLAllowedWithSchemes(%q, %v) = %v, want %v — blocklist must win over allowlist",
					tc.url, tc.extraSchemes, got, tc.want)
			}
		})
	}
}
