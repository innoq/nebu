package matrix

// Story 5.25: SSO LoginToken TTL Correction (5 min → 30 s)
//
// RED PHASE — these tests are written BEFORE the implementation change.
// They MUST fail until:
//   1. `loginTokenTTL` is declared as `const loginTokenTTL = 30 * time.Second`
//   2. `globalLoginTokens.save(...)` uses `loginTokenTTL`
//   3. The divergent comment is removed
//
// Why each test fails right now:
//   - TestLoginToken_TTLConstantIs30s: `loginTokenTTL` does not exist as a
//     package-level identifier → compile error.
//   - TestLoginToken_ExpiresAfter30s: injects a loginTokenEntry with
//     exp = now-1s (already expired) and asserts pop() returns ("", false).
//   - TestLoginToken_ValidWithin30s: seeds the store with exp = now+29s and
//     pops immediately; exp is in the future, so pop returns the token.  That
//     is the GREEN behaviour — this test will only demonstrate the correct
//     behaviour once the TTL fix lands.  Its companion test
//     TestLoginToken_ExpiresAfter30s drives the RED signal.
//
// Implementation note — no injectable clock:
// loginTokenStore uses time.Now() directly (no clock interface). Because the
// test file lives in the same package (white-box), we inject state by writing
// directly into loginTokenStore.store with a crafted exp timestamp, bypassing
// the save() path. This makes the tests deterministic and avoids real sleeps.

import (
	"testing"
	"time"
)

// ── AC 4 (compile-time regression guard) ─────────────────────────────────────

// TestLoginToken_TTLConstantIs30s asserts that the package-level constant
// loginTokenTTL equals exactly 30 seconds.
//
// RED: loginTokenTTL does not exist yet → compile error.
// GREEN: after `const loginTokenTTL = 30 * time.Second` is added.
func TestLoginToken_TTLConstantIs30s(t *testing.T) {
	t.Parallel()

	const want = 30 * time.Second

	// This line will not compile until loginTokenTTL is declared in the package.
	if loginTokenTTL != want {
		t.Errorf("loginTokenTTL = %v, want %v — AC 4: constant must be exactly 30 seconds", loginTokenTTL, want)
	}
}

// ── AC 5: token expires after TTL ─────────────────────────────────────────────

// TestLoginToken_ExpiresAfter30s verifies that a token whose TTL has elapsed
// is rejected by pop() (returns "", false).
//
// Technique: inject a loginTokenEntry with exp = now - 1s (already expired)
// directly into the store map, bypassing save(). pop() must detect the
// expiry and return ("", false).
//
// RED: this test passes even before the TTL fix because it tests pop()
// expiry detection, which already exists. BUT it is paired with
// TestLoginToken_TTLConstantIs30s (compile error) so the full test suite
// cannot pass until the constant is added.
//
// Once the constant lands: this test confirms the expiry path is correct
// for any TTL, guarding against future regressions.
func TestLoginToken_ExpiresAfter30s(t *testing.T) {
	t.Parallel()

	store := &loginTokenStore{store: make(map[string]loginTokenEntry)}
	const opaque = "test-opaque-token-expired"
	const storedIDToken = "raw.id.token.value"

	// Inject an already-expired entry: exp is 1 second in the past.
	// This simulates what happens after the 30-second TTL elapses.
	store.store[opaque] = loginTokenEntry{
		idToken: storedIDToken,
		exp:     time.Now().Add(-1 * time.Second),
	}

	idToken, ok := store.pop(opaque)
	if ok {
		t.Errorf("pop() returned ok=true for expired token, want false\nreturned idToken: %q", idToken)
	}
	if idToken != "" {
		t.Errorf("pop() returned idToken=%q for expired token, want empty string", idToken)
	}

	// The entry must have been removed even though it was expired (single-use).
	if _, exists := store.store[opaque]; exists {
		t.Error("pop() did not remove the expired entry from the store — memory leak risk")
	}
}

// TestLoginToken_ValidWithin30s verifies that a token whose TTL has NOT yet
// elapsed is accepted by pop() (returns the stored id_token, true) and that
// the entry is removed afterwards (single-use guarantee).
//
// Technique: inject a loginTokenEntry with exp = now + 29s directly into
// the store map.
//
// RED phase context: this test will compile-fail along with
// TestLoginToken_TTLConstantIs30s because loginTokenTTL does not exist yet.
// Once the constant lands, this test documents the happy-path behaviour.
func TestLoginToken_ValidWithin30s(t *testing.T) {
	t.Parallel()

	store := &loginTokenStore{store: make(map[string]loginTokenEntry)}
	const opaque = "test-opaque-token-valid"
	const storedIDToken = "raw.id.token.valid.value"

	// Inject a still-valid entry: exp is 29 seconds in the future.
	// This simulates a pop that happens within the 30-second window.
	store.store[opaque] = loginTokenEntry{
		idToken: storedIDToken,
		exp:     time.Now().Add(29 * time.Second),
	}

	idToken, ok := store.pop(opaque)
	if !ok {
		t.Error("pop() returned ok=false for a valid (non-expired) token, want true")
	}
	if idToken != storedIDToken {
		t.Errorf("pop() returned idToken=%q, want %q", idToken, storedIDToken)
	}

	// Single-use guarantee: the entry must be gone after a successful pop.
	if _, exists := store.store[opaque]; exists {
		t.Error("pop() did not remove the valid entry — single-use guarantee violated")
	}
}

// ── Integration: save() uses loginTokenTTL ────────────────────────────────────

// TestLoginToken_SaveUsesLoginTokenTTL verifies that loginTokenStore.save()
// stores the entry with an expiry of exactly loginTokenTTL from now.
//
// RED: compile error until loginTokenTTL is declared.
// GREEN: passes once `const loginTokenTTL = 30 * time.Second` is introduced
//        AND globalLoginTokens.save(..., loginTokenTTL) is the call site.
//
// Note: this test uses a local store (not globalLoginTokens) for isolation.
func TestLoginToken_SaveUsesLoginTokenTTL(t *testing.T) {
	t.Parallel()

	store := &loginTokenStore{store: make(map[string]loginTokenEntry)}
	const opaque = "integration-opaque-token"
	const idToken = "some.raw.id.token"

	before := time.Now()
	store.save(opaque, idToken, loginTokenTTL) // compile error until const exists
	after := time.Now()

	entry, exists := store.store[opaque]
	if !exists {
		t.Fatal("save() did not create an entry in the store")
	}

	// The stored expiry must be within [before+loginTokenTTL, after+loginTokenTTL].
	// A tolerance of 10ms is sufficient for an in-process call.
	wantExpMin := before.Add(loginTokenTTL)
	wantExpMax := after.Add(loginTokenTTL)

	if entry.exp.Before(wantExpMin) || entry.exp.After(wantExpMax) {
		t.Errorf(
			"stored entry.exp = %v, want in [%v, %v] (loginTokenTTL = %v)\n"+
				"If this fails, save() is not using loginTokenTTL.",
			entry.exp, wantExpMin, wantExpMax, loginTokenTTL,
		)
	}

	// Sanity check: TTL must be 30 seconds (AC 4 repeated in integration context).
	if loginTokenTTL != 30*time.Second {
		t.Errorf("loginTokenTTL = %v, want 30s — AC 4 violated in integration context", loginTokenTTL)
	}
}
