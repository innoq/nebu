//go:build go1.22

// Package api_test contains ATDD acceptance tests for Story 6.2:
// Admin API Response Format + Cursor-Pagination.
//
// RED PHASE — all tests fail until implementation is complete.
// Remove the t.Skip(...) call in each test to activate it during implementation.
//
// Covered Acceptance Criteria:
//   - AC#2  EncodeCursor/DecodeCursor round-trip (encode then decode returns original values)
//   - AC#2  ErrInvalidCursor sentinel is a package-level var
//   - AC#3  Malformed cursor (truncated, invalid base64, invalid JSON, empty) → ErrInvalidCursor
//   - AC#5  Unit tests cover round-trip and malformed-cursor cases
package api_test

import (
	"errors"
	"testing"

	"github.com/nebu/nebu/internal/api"
)

// TestEncodeCursor_DecodeCursor_RoundTrip covers AC#2 + AC#5:
// EncodeCursor followed by DecodeCursor must return the original values without error.
//
// [P0] — foundational contract for cursor-based pagination; all list endpoints depend on this.
func TestEncodeCursor_DecodeCursor_RoundTrip(t *testing.T) {

	afterID := "550e8400-e29b-41d4-a716-446655440000"
	afterCreatedAt := "2026-01-15T10:30:00Z"

	cursor := api.EncodeCursor(afterID, afterCreatedAt)

	if cursor == "" {
		t.Fatal("[AC#2] EncodeCursor returned empty string")
	}

	gotID, gotCreatedAt, err := api.DecodeCursor(cursor)
	if err != nil {
		t.Fatalf("[AC#2] unexpected error from DecodeCursor: %v", err)
	}
	if gotID != afterID {
		t.Errorf("[AC#2] afterID: got %q, want %q", gotID, afterID)
	}
	if gotCreatedAt != afterCreatedAt {
		t.Errorf("[AC#2] afterCreatedAt: got %q, want %q", gotCreatedAt, afterCreatedAt)
	}
}

// TestDecodeCursor_MalformedBase64_ReturnsErrInvalidCursor covers AC#3 + AC#5:
// Input that is not valid base64url must return ErrInvalidCursor.
//
// [P0] — invalid cursors on list endpoints must yield 400 M_BAD_JSON; this is the guard.
func TestDecodeCursor_MalformedBase64_ReturnsErrInvalidCursor(t *testing.T) {

	gotID, gotCreatedAt, err := api.DecodeCursor("not-valid-base64!!")

	if !errors.Is(err, api.ErrInvalidCursor) {
		t.Errorf("[AC#3] expected errors.Is(err, api.ErrInvalidCursor) to be true; got err=%v", err)
	}
	if gotID != "" {
		t.Errorf("[AC#3] expected empty afterID on error; got %q", gotID)
	}
	if gotCreatedAt != "" {
		t.Errorf("[AC#3] expected empty afterCreatedAt on error; got %q", gotCreatedAt)
	}
}

// TestDecodeCursor_ValidBase64ButInvalidJSON_ReturnsErrInvalidCursor covers AC#3 + AC#5:
// A valid base64url string whose decoded content is not JSON must return ErrInvalidCursor.
//
// [P0] — prevents silent data corruption from corrupt or tampered cursor tokens.
func TestDecodeCursor_ValidBase64ButInvalidJSON_ReturnsErrInvalidCursor(t *testing.T) {

	// base64url(no-padding) of "this is not json"
	// echo -n 'this is not json' | base64 | tr '+/' '-_' | tr -d '='
	// => dGhpcyBpcyBub3QganNvbg
	cursor := "dGhpcyBpcyBub3QganNvbg"

	_, _, err := api.DecodeCursor(cursor)

	if !errors.Is(err, api.ErrInvalidCursor) {
		t.Errorf("[AC#3] expected ErrInvalidCursor for valid-base64 / invalid-JSON input; got err=%v", err)
	}
}

// TestDecodeCursor_ValidJSONButMissingFields_ReturnsErrInvalidCursor covers AC#3 + AC#5:
// A cursor whose JSON payload exists but lacks required fields must return ErrInvalidCursor.
//
// [P1] — ensures partial / structurally-altered cursors are rejected rather than silently accepted.
func TestDecodeCursor_ValidJSONButMissingFields_ReturnsErrInvalidCursor(t *testing.T) {

	// base64url(no-padding) of `{"after_id":"","after_created_at":""}` — empty fields
	// echo -n '{"after_id":"","after_created_at":""}' | base64 | tr '+/' '-_' | tr -d '='
	// => eyJhZnRlcl9pZCI6IiIsImFmdGVyX2NyZWF0ZWRfYXQiOiIifQ
	cursorEmptyFields := "eyJhZnRlcl9pZCI6IiIsImFmdGVyX2NyZWF0ZWRfYXQiOiIifQ"

	_, _, err := api.DecodeCursor(cursorEmptyFields)

	if !errors.Is(err, api.ErrInvalidCursor) {
		t.Errorf("[AC#3] expected ErrInvalidCursor for cursor with empty required fields; got err=%v", err)
	}
}

// TestDecodeCursor_EmptyString_ReturnsErrInvalidCursor covers AC#3 + AC#5:
// An empty string input must return ErrInvalidCursor, not a panic or nil error.
//
// [P1] — defensive: callers may pass empty cursor from missing query param; must be handled.
func TestDecodeCursor_EmptyString_ReturnsErrInvalidCursor(t *testing.T) {

	_, _, err := api.DecodeCursor("")

	if !errors.Is(err, api.ErrInvalidCursor) {
		t.Errorf("[AC#3] expected ErrInvalidCursor for empty cursor; got err=%v", err)
	}
}

// TestEncodeCursor_IsBase64URLNoPad covers AC#2 (encoding spec):
// The cursor string must consist solely of URL-safe base64 characters without padding ('=').
//
// [P1] — using standard base64 ('+', '/', '=') would break URL query param safety.
func TestEncodeCursor_IsBase64URLNoPad(t *testing.T) {

	cursor := api.EncodeCursor("550e8400-e29b-41d4-a716-446655440000", "2026-01-15T10:30:00Z")

	for i, ch := range cursor {
		// Valid base64url-no-pad alphabet: A-Z a-z 0-9 - _
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			t.Errorf("[AC#2] cursor contains non-base64url character %q at position %d", ch, i)
		}
	}

	if len(cursor) > 0 && cursor[len(cursor)-1] == '=' {
		t.Errorf("[AC#2] cursor must use no-padding encoding (RawURLEncoding); trailing '=' found: %q", cursor)
	}
}

// TestErrInvalidCursor_IsPackageLevelVar covers AC#2 sentinel requirement:
// api.ErrInvalidCursor must be a package-level var so callers can use errors.Is().
//
// [P0] — without the sentinel, callers cannot distinguish invalid-cursor from other errors.
func TestErrInvalidCursor_IsPackageLevelVar(t *testing.T) {

	// Verify the sentinel is non-nil (package-level var must be initialized).
	if api.ErrInvalidCursor == nil {
		t.Fatal("[AC#2] api.ErrInvalidCursor must be a non-nil package-level error sentinel")
	}

	// Verify errors.Is wrapping works: wrapping ErrInvalidCursor must still match.
	wrapped := errors.Join(api.ErrInvalidCursor, errors.New("additional context"))
	if !errors.Is(wrapped, api.ErrInvalidCursor) {
		t.Error("[AC#2] errors.Is(wrapped, api.ErrInvalidCursor) returned false; sentinel must support Is() unwrapping")
	}
}
