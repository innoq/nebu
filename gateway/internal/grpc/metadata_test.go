package grpc

// RED PHASE additions — Story 11-10: OIDC Claim Mapping Configuration
//
// TestFormatUserIDFromClaims_Configured and TestFormatUserIDFromClaims_FallbackToSub
// are written BEFORE the refactored FormatUserIDFromClaims signature exists.
//
// Current signature (to be replaced):
//   FormatUserIDFromClaims(sub, name, serverName string) string
//
// New signature (AC6):
//   FormatUserIDFromClaims(claimName string, claims map[string]interface{}, serverName string) string
//
// These tests MUST fail (compile error or wrong behaviour) until metadata.go is updated.

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestWithUserMetadata_SetsOutgoingMetadata(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		systemRole string
	}{
		{"regular user", "@alice:example.com", "user"},
		{"instance admin", "@kai:example.com", "instance_admin"},
		{"empty values", "", "user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithUserMetadata(context.Background(), tt.userID, tt.systemRole)
			md, ok := metadata.FromOutgoingContext(ctx)
			if !ok {
				t.Fatal("no outgoing metadata found in context")
			}
			if got := md.Get(MetadataKeyUserID); len(got) == 0 || got[0] != tt.userID {
				t.Errorf("x-user-id = %v, want %q", got, tt.userID)
			}
			if got := md.Get(MetadataKeySystemRole); len(got) == 0 || got[0] != tt.systemRole {
				t.Errorf("x-system-role = %v, want %q", got, tt.systemRole)
			}
		})
	}
}

func TestFormatUserID(t *testing.T) {
	tests := []struct {
		sub        string
		serverName string
		want       string
	}{
		{"abc-uuid-123", "example.com", "@_zbjphk2ji31:example.com"},
		{"", "example.com", ""},
		{"user1", "nebu.internal", "@cgqblglkpkmb:nebu.internal"},
		// Dex local-connector sub: SHA-256 → base64url[:12] → lowercase (fallback when name is empty)
		{"CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDMSBWxvY2Fs", "localhost", "@xph3zgstbkdb:localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.sub+"/"+tt.serverName, func(t *testing.T) {
			got := FormatUserID(tt.sub, tt.serverName)
			if got != tt.want {
				t.Errorf("FormatUserID(%q, %q) = %q, want %q", tt.sub, tt.serverName, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RED PHASE — Story 11-10: FormatUserIDFromClaims new signature (AC5, AC6, AC7)
// These tests compile against the NEW signature:
//   FormatUserIDFromClaims(claimName string, claims map[string]interface{}, serverName string) string
//
// Until metadata.go is refactored, these tests will not compile (AC6 is unimplemented).
// ---------------------------------------------------------------------------

// TestFormatUserIDFromClaims_Configured verifies that when claims contain the configured
// claim name and its value sanitises to a non-empty localpart, the Matrix user ID uses
// that localpart.
// AC5 — "the Matrix user ID is derived from preferred_username claim via sanitiseLocalpart"
// AC6 — new FormatUserIDFromClaims(claimName, claims, serverName) signature
func TestFormatUserIDFromClaims_Configured(t *testing.T) {
	claims := map[string]interface{}{
		"sub":                "u123",
		"preferred_username": "alice",
		"name":               "Alice Smith",
	}
	got := FormatUserIDFromClaims("preferred_username", claims, "server.example")
	want := "@alice:server.example"
	if got != want {
		t.Errorf("FormatUserIDFromClaims(%q, claims, %q) = %q, want %q",
			"preferred_username", "server.example", got, want)
	}
}

// TestFormatUserIDFromClaims_FallbackToSub verifies that when the configured claim is
// absent from the claims map, the function falls back to FormatUserID(sub, serverName)
// (SHA-256 opaque localpart).
// AC6 — "falls back to FormatUserID(sub, serverName) if the result is empty"
// AC7 — backward compat: missing/empty configured claim never panics, always falls back
func TestFormatUserIDFromClaims_FallbackToSub(t *testing.T) {
	claims := map[string]interface{}{
		"sub": "u123", // preferred_username absent
	}
	got := FormatUserIDFromClaims("preferred_username", claims, "server.example")
	// Must equal FormatUserID("u123", "server.example") — SHA-256 fallback
	want := FormatUserID("u123", "server.example")
	if got != want {
		t.Errorf("FormatUserIDFromClaims fallback = %q, want SHA-256 fallback %q", got, want)
	}
}

// TestFormatUserIDFromClaims_FallbackWhenClaimEmptyString verifies that when the configured
// claim exists but its value is the empty string, the function falls back to FormatUserID.
// AC6 — sanitiseLocalpart("") returns "" → fall through to SHA-256 path
func TestFormatUserIDFromClaims_FallbackWhenClaimEmptyString(t *testing.T) {
	claims := map[string]interface{}{
		"sub":   "u456",
		"email": "", // present but empty
	}
	got := FormatUserIDFromClaims("email", claims, "nebu.internal")
	want := FormatUserID("u456", "nebu.internal")
	if got != want {
		t.Errorf("FormatUserIDFromClaims (empty claim value) = %q, want %q", got, want)
	}
}

// TestFormatUserIDFromClaims_EmptySubFallback verifies that when sub is also absent or empty,
// the function returns "" (same as FormatUserID("", serverName)).
// AC6 — sub="" edge case must not panic
func TestFormatUserIDFromClaims_EmptySubFallback(t *testing.T) {
	claims := map[string]interface{}{} // neither sub nor configured claim present
	got := FormatUserIDFromClaims("preferred_username", claims, "nebu.internal")
	if got != "" {
		t.Errorf("FormatUserIDFromClaims (no sub, no claim) = %q, want empty string", got)
	}
}

// TestFormatUserIDFromClaims_SubClaim verifies that claimName="sub" uses the sub value
// directly (sanitised), which is the recommended default for new installs.
// AC5 — "oidc_user_id_claim = sub → FormatUserID(sub, serverName) is used as the fallback
// (SHA-256 based opaque localpart)" — but first tries sanitiseLocalpart(sub).
func TestFormatUserIDFromClaims_SubClaim(t *testing.T) {
	claims := map[string]interface{}{
		"sub":  "alice",
		"name": "Alice Smith",
	}
	got := FormatUserIDFromClaims("sub", claims, "nebu.internal")
	// "alice" is Matrix-safe → @alice:nebu.internal
	want := "@alice:nebu.internal"
	if got != want {
		t.Errorf("FormatUserIDFromClaims(%q, claims, %q) = %q, want %q",
			"sub", "nebu.internal", got, want)
	}
}

// TestFormatUserIDFromClaims_NonStringClaimValueFallsBack verifies that when the configured
// claim value is a non-string type (e.g., int), the function falls back gracefully to FormatUserID.
// AC6 — "Extract claims[claimName] as string (ok to ignore non-string types)"
func TestFormatUserIDFromClaims_NonStringClaimValueFallsBack(t *testing.T) {
	claims := map[string]interface{}{
		"sub":   "u789",
		"level": 42, // integer — must not panic
	}
	got := FormatUserIDFromClaims("level", claims, "nebu.internal")
	want := FormatUserID("u789", "nebu.internal")
	if got != want {
		t.Errorf("FormatUserIDFromClaims (non-string claim) = %q, want %q", got, want)
	}
}
