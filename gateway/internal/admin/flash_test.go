package admin

// Story 7-18: Flash-Message Allowlist auf Admin GET-Handlern
//
// RED PHASE: sanitizeFlash does not exist yet — this file fails to compile
// until flash.go is created. After implementation, all tests must pass.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for sanitizeFlash (AC1–AC4)
// ---------------------------------------------------------------------------

// TestSanitizeFlash_AllowlistValuesPassThrough verifies AC1:
// Each of the 11 known-safe flash values is returned unchanged.
func TestSanitizeFlash_AllowlistValuesPassThrough(t *testing.T) {
	t.Parallel()

	for msg := range allowedFlashMessages {
		msg := msg
		t.Run(msg, func(t *testing.T) {
			t.Parallel()
			got := sanitizeFlash(msg)
			if got != msg {
				t.Errorf("sanitizeFlash(%q) = %q, want %q", msg, got, msg)
			}
		})
	}
}

// TestSanitizeFlash_UnknownValueRejected verifies AC2:
// A value not in the allowlist is silently dropped (returns "").
func TestSanitizeFlash_UnknownValueRejected(t *testing.T) {
	t.Parallel()

	got := sanitizeFlash("Please re-enter your credentials")
	if got != "" {
		t.Errorf("sanitizeFlash(unknown) = %q, want %q", got, "")
	}
}

// TestSanitizeFlash_OversizedValueRejected verifies AC3:
// A value longer than 80 characters is rejected, even if the prefix matches
// an allowlist entry.
func TestSanitizeFlash_OversizedValueRejected(t *testing.T) {
	t.Parallel()

	// Plain oversized string — rejected by length check.
	got := sanitizeFlash(strings.Repeat("x", 81))
	if got != "" {
		t.Errorf("sanitizeFlash(81-char string) = %q, want empty string", got)
	}

	// Prefix IS an allowlist value, but the total length exceeds 80 — still rejected.
	prefixMatch := "Config updated" + strings.Repeat("x", 80-len("Config updated")+1)
	if len(prefixMatch) <= 80 {
		t.Fatalf("test setup error: prefixMatch length %d must be > 80", len(prefixMatch))
	}
	got2 := sanitizeFlash(prefixMatch)
	if got2 != "" {
		t.Errorf("sanitizeFlash(oversized with allowlist prefix) = %q, want empty string", got2)
	}
}

// TestSanitizeFlash_ExactlyEightyCharsIsRejected verifies the > 80 boundary:
// A value of exactly 81 characters is rejected; 80 characters would be accepted
// only if also in the allowlist.
func TestSanitizeFlash_ExactlyEightyCharsIsRejected(t *testing.T) {
	t.Parallel()

	// 80 'x' chars — not in allowlist → still rejected
	msg := strings.Repeat("x", 80)
	got := sanitizeFlash(msg)
	if got != "" {
		t.Errorf("sanitizeFlash(80-char non-allowlist string) = %q, want empty string", got)
	}
}

// TestSanitizeFlash_EmptyStringIsNoOp verifies AC4:
// An empty string is returned as "" (no panic, no banner).
func TestSanitizeFlash_EmptyStringIsNoOp(t *testing.T) {
	t.Parallel()

	got := sanitizeFlash("")
	if got != "" {
		t.Errorf("sanitizeFlash(\"\") = %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// Integration tests: GET handlers read sanitized flash (AC3–AC7)
// ---------------------------------------------------------------------------

// TestFlash_ValidFlashRenderedInBanner verifies AC4:
// A known-safe flash value is rendered in the alert banner.
func TestFlash_ValidFlashRenderedInBanner(t *testing.T) {
	t.Parallel()

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/config?flash=Config+updated", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Config updated") {
		t.Error("expected 'Config updated' in response body (valid flash must render)")
	}
}

// TestFlash_UnknownFlashShowsNoBanner verifies AC5:
// A value not in the allowlist is dropped — no banner is shown.
func TestFlash_UnknownFlashShowsNoBanner(t *testing.T) {
	t.Parallel()

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/config?flash=Hacked", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "Hacked") {
		t.Error("injected flash value 'Hacked' must NOT appear in response body")
	}
}

// TestFlash_OversizedFlashShowsNoBanner verifies AC6:
// A flash value longer than 80 characters is dropped.
func TestFlash_OversizedFlashShowsNoBanner(t *testing.T) {
	t.Parallel()

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/users/{userId}", h.DetailHandler)

	longFlash := strings.Repeat("a", 81)
	req := httptest.NewRequest(http.MethodGet, "/admin/users/usr-001?flash="+longFlash, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), longFlash) {
		t.Error("oversized flash value must NOT appear in response body")
	}
}

// TestFlash_AllFiveHandlersRejectUnknownFlash verifies AC5 across all five handlers:
// GET /admin/users/{userId}, GET /admin/rooms/{roomId}, GET /admin/config,
// GET /admin/config/role-mapping, and GET /admin/compliance all drop unknown flash values.
func TestFlash_AllFiveHandlersRejectUnknownFlash(t *testing.T) {
	t.Parallel()

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	type handlerCase struct {
		name    string
		pattern string
		url     string
		handler http.HandlerFunc
	}

	usersH := NewUsersHandler(tmpl)
	roomsH := NewRoomsHandler(tmpl)
	configH := NewConfigHandler(tmpl)
	roleMappingH := NewRoleMappingHandler(tmpl)
	complianceH := NewComplianceHandler(tmpl)

	cases := []handlerCase{
		{
			name:    "users detail",
			pattern: "GET /admin/users/{userId}",
			url:     "/admin/users/usr-001?flash=BAD",
			handler: usersH.DetailHandler,
		},
		{
			name:    "rooms detail",
			pattern: "GET /admin/rooms/{roomId}",
			url:     "/admin/rooms/room-001?flash=BAD",
			handler: roomsH.DetailHandler,
		},
		{
			name:    "config",
			pattern: "GET /admin/config",
			url:     "/admin/config?flash=BAD",
			handler: configH.Handler,
		},
		{
			name:    "role-mapping",
			pattern: "GET /admin/config/role-mapping",
			url:     "/admin/config/role-mapping?flash=BAD",
			handler: roleMappingH.Handler,
		},
		{
			name:    "compliance",
			pattern: "GET /admin/compliance",
			url:     "/admin/compliance?flash=BAD",
			handler: complianceH.ListHandler,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc(tc.pattern, tc.handler)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want 200 got %d", w.Code)
			}
			if strings.Contains(w.Body.String(), "BAD") {
				t.Errorf("%s: unknown flash value 'BAD' must NOT appear in response body", tc.name)
			}
		})
	}
}

// TestFlash_AllowlistValueForEachHandler verifies AC4 across all five GET handlers:
// Each handler renders its canonical flash value correctly.
func TestFlash_AllowlistValueForEachHandler(t *testing.T) {
	t.Parallel()

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	usersH := NewUsersHandler(tmpl)
	roomsH := NewRoomsHandler(tmpl)
	configH := NewConfigHandler(tmpl)
	roleMappingH := NewRoleMappingHandler(tmpl)
	complianceH := NewComplianceHandler(tmpl)

	type handlerCase struct {
		name     string
		pattern  string
		url      string
		handler  http.HandlerFunc
		flashMsg string
	}

	cases := []handlerCase{
		{
			name:     "users detail — Display name updated",
			pattern:  "GET /admin/users/{userId}",
			url:      "/admin/users/usr-001?flash=Display+name+updated",
			handler:  usersH.DetailHandler,
			flashMsg: "Display name updated",
		},
		{
			name:     "rooms detail — Room name updated",
			pattern:  "GET /admin/rooms/{roomId}",
			url:      "/admin/rooms/room-001?flash=Room+name+updated",
			handler:  roomsH.DetailHandler,
			flashMsg: "Room name updated",
		},
		{
			name:     "config — Config updated",
			pattern:  "GET /admin/config",
			url:      "/admin/config?flash=Config+updated",
			handler:  configH.Handler,
			flashMsg: "Config updated",
		},
		{
			name:     "role-mapping — Role mapping updated",
			pattern:  "GET /admin/config/role-mapping",
			url:      "/admin/config/role-mapping?flash=Role+mapping+updated",
			handler:  roleMappingH.Handler,
			flashMsg: "Role mapping updated",
		},
		{
			name:     "compliance — Approved",
			pattern:  "GET /admin/compliance",
			url:      "/admin/compliance?flash=Approved",
			handler:  complianceH.ListHandler,
			flashMsg: "Approved",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc(tc.pattern, tc.handler)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want 200 got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.flashMsg) {
				t.Errorf("%s: expected %q in response body (valid allowlist value must render)", tc.name, tc.flashMsg)
			}
		})
	}
}
