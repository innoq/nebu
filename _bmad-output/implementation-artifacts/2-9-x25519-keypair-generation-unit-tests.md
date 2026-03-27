# Story 2.9: X25519 Keypair Generation + Unit Tests

Status: done

## Story

As a developer,
I want the Elixir `signature` app to generate X25519 keypairs using OTP native crypto,
so that PII encryption has a tested, dependency-free foundation.

## Acceptance Criteria

1. **Given** `Nebu.Signature.generate_encryption_keypair/0` function,
   **When** called,
   **Then** it returns `{public_key, private_key}` as binary tuples using `:crypto.generate_key(:ecdh, :x25519)`

2. **Given** a generated X25519 keypair,
   **When** key lengths are checked,
   **Then** both public key and private key are 32 bytes

3. **Given** a unit test `ecdh_shared_secret`,
   **When** two X25519 keypairs perform ECDH exchange (Alice's private × Bob's public, Bob's private × Alice's public),
   **Then** both computations produce the identical shared secret

4. **Given** `Nebu.Signature.derive_aes_key/1` function that takes a shared secret,
   **When** called with a 32-byte shared secret,
   **Then** it returns a 32-byte AES-256 key (via HKDF-SHA256 or SHA256 derivation)

5. **Given** `mix test --warnings-as-errors` in the `signature` app,
   **When** run,
   **Then** all unit tests pass with 0 failures

## Tasks / Subtasks

- [x] Add `generate_encryption_keypair/0` to `core/apps/signature/lib/nebu/signature.ex` (AC: #1, #2)
  - [x] Add `@doc` and `@spec generate_encryption_keypair() :: {binary(), binary()}`
  - [x] Implement using `:crypto.generate_key(:ecdh, :x25519)` — returns `{pub, priv}` both 32 bytes
  - [x] Update `@moduledoc` to mention X25519 is now implemented (remove "(Story 2.9)" forward reference)

- [x] Add `derive_aes_key/1` to `core/apps/signature/lib/nebu/signature.ex` (AC: #4)
  - [x] Add `@doc` and `@spec derive_aes_key(binary()) :: binary()`
  - [x] Implement using `:crypto.hash(:sha256, shared_secret)` — returns 32-byte AES-256 key
  - [x] Document the output is deterministic and suitable for AES-256-GCM

- [x] Add new `describe` blocks to `core/apps/signature/test/nebu_signature_test.exs` (AC: #3, #4, #5)
  - [x] Add `describe "generate_encryption_keypair/0"` with test for 32-byte key lengths
  - [x] Add `test "ecdh_shared_secret: ECDH exchange produces identical shared secrets on both sides"`
  - [x] Add `describe "derive_aes_key/1"` with test verifying 32-byte output
  - [x] Verify `mix test --warnings-as-errors` passes with 0 failures (all existing + new tests)

## Dev Notes

### Scope

Adds two functions to the existing `Nebu.Signature` module:
- `generate_encryption_keypair/0` — X25519 keypair for ECDH
- `derive_aes_key/1` — AES-256 key derivation from ECDH shared secret

**Do NOT implement encryption/decryption in this story** — that is Stories 2.10 (operational PII, server key) and 2.11 (sensitive PII, user X25519 key). This story only builds the keypair + derivation foundation.

### Files to Modify

```
core/apps/signature/
  lib/
    nebu/
      signature.ex           ← MODIFY — add generate_encryption_keypair/0 and derive_aes_key/1
  test/
    nebu_signature_test.exs  ← MODIFY — add new describe blocks; keep existing Ed25519 tests untouched
  mix.exs                    ← DO NOT TOUCH (no new deps needed)
  lib/nebu/signature/
    application.ex           ← DO NOT TOUCH
```

### Implementation: signature.ex additions

**OTP API — X25519 key generation (no external library):**
```elixir
{public_key, private_key} = :crypto.generate_key(:ecdh, :x25519)
# Both are 32-byte binaries — this is the standard X25519/Curve25519 key size
```

**OTP API — ECDH shared secret (for use in tests only in this story):**
```elixir
shared_secret = :crypto.compute_key(:ecdh, other_pub, my_priv, :x25519)
# Returns 32-byte binary — identical when computed from both sides
```

**AES key derivation:**
```elixir
aes_key = :crypto.hash(:sha256, shared_secret)
# SHA-256 of 32-byte input → 32-byte output; deterministic, no external deps
# Suitable for AES-256-GCM (Stories 2.10 and 2.11 will use this)
```

**Key sizes (AC #2) — X25519 is fundamentally 32 bytes for BOTH keys:**
- `public_key`: 32 bytes
- `private_key`: 32 bytes
- ⚠️ Unlike Ed25519 (Story 2.8 lesson), X25519 has symmetric key sizes — no libsodium convention discrepancy

**Complete additions to `signature.ex`:**
```elixir
@doc """
Generates an X25519 encryption keypair.

Returns `{public_key, private_key}` as binaries.
- public_key: 32 bytes
- private_key: 32 bytes

Used for PII encryption via ECDH key agreement (Architecture V1).
Uses OTP native `:crypto.generate_key(:ecdh, :x25519)` (available since OTP 24+).
"""
@spec generate_encryption_keypair() :: {binary(), binary()}
def generate_encryption_keypair do
  :crypto.generate_key(:ecdh, :x25519)
end

@doc """
Derives a 32-byte AES-256 key from an ECDH shared secret.

Uses SHA-256 (`:crypto.hash(:sha256, shared_secret)`) — deterministic, no external deps.
Input must be a 32-byte binary (X25519 ECDH shared secret).

Used as the symmetric key for AES-256-GCM encryption in Stories 2.10 and 2.11.
"""
@spec derive_aes_key(binary()) :: binary()
def derive_aes_key(shared_secret) do
  :crypto.hash(:sha256, shared_secret)
end
```

### Implementation: nebu_signature_test.exs additions

**ADD two new describe blocks — DO NOT modify the existing `describe "generate_signing_keypair/0"` block:**

```elixir
describe "generate_encryption_keypair/0" do
  test "returns binary keypair of correct length" do
    {pub, priv} = Signature.generate_encryption_keypair()
    assert is_binary(pub)
    assert is_binary(priv)
    assert byte_size(pub) == 32
    assert byte_size(priv) == 32
  end

  test "ecdh_shared_secret: ECDH exchange produces identical shared secrets on both sides" do
    {alice_pub, alice_priv} = Signature.generate_encryption_keypair()
    {bob_pub, bob_priv} = Signature.generate_encryption_keypair()

    alice_shared = :crypto.compute_key(:ecdh, bob_pub, alice_priv, :x25519)
    bob_shared = :crypto.compute_key(:ecdh, alice_pub, bob_priv, :x25519)

    assert alice_shared == bob_shared
    assert byte_size(alice_shared) == 32
  end
end

describe "derive_aes_key/1" do
  test "returns a 32-byte AES-256 key from shared secret" do
    {alice_pub, alice_priv} = Signature.generate_encryption_keypair()
    {bob_pub, bob_priv} = Signature.generate_encryption_keypair()

    shared = :crypto.compute_key(:ecdh, bob_pub, alice_priv, :x25519)
    aes_key = Signature.derive_aes_key(shared)

    assert is_binary(aes_key)
    assert byte_size(aes_key) == 32
  end

  test "derive_aes_key is deterministic: same input yields same key" do
    shared_secret = :crypto.strong_rand_bytes(32)
    assert Signature.derive_aes_key(shared_secret) == Signature.derive_aes_key(shared_secret)
  end
end
```

**Keep all three existing `generate_signing_keypair/0` tests unchanged.**

**Total expected test count after this story:** `7 tests, 0 failures` (3 existing Ed25519 + 4 new X25519)

### OTP Version Compatibility

OTP `:crypto` X25519 support:
- `:crypto.generate_key(:ecdh, :x25519)` — available since OTP 24+
- `:crypto.compute_key(:ecdh, ...)` — available since OTP 24+
- `:crypto.hash(:sha256, ...)` — available since OTP 1+
- Project requires Elixir `~> 1.19` (targets OTP 26+) — no compatibility issues

### Critical API Note: ECDH vs EDDSA key format difference

From Story 2.8 (Ed25519) learning:
- Ed25519 sign/verify had a subtle API shape: `:crypto.sign(:eddsa, :none, msg, [key, :ed25519])` — curve goes IN the key list
- X25519 ECDH is different: `:crypto.compute_key(:ecdh, other_pub, my_priv, :x25519)` — curve is the 4th argument, NOT in the key list
- These two APIs have different shapes — do NOT copy-paste the Ed25519 sign pattern for ECDH

### Architecture Rules (mandatory)

- **V1 (CRITICAL):** X25519 is for ECDH key agreement (encryption) ONLY — never for signing
- **Anti-pattern #10:** Ed25519 keys MUST NOT be used for encryption — that's `generate_signing_keypair/0`, not `generate_encryption_keypair/0`
- Two-keypair architecture: `generate_signing_keypair/0` (Ed25519) + `generate_encryption_keypair/0` (X25519) are separate functions for separate purposes
- Both use OTP native `:crypto` — no external hex packages
- [Source: architecture.md#V1]

### Cross-Story Dependencies

| Story | Relationship |
|-------|-------------|
| 2.8   | Predecessor: established `generate_signing_keypair/0` (Ed25519), `Nebu.Signature` module, test patterns |
| 2.10  | Consumer: uses `generate_encryption_keypair/0` + `derive_aes_key/1` for operational PII (server key, AES-256-GCM) |
| 2.11  | Consumer: uses `generate_encryption_keypair/0` + `derive_aes_key/1` for sensitive PII (user X25519 public key, ECDH) |
| 2.13  | Orchestrator: calls both `generate_signing_keypair/0` AND `generate_encryption_keypair/0` during user provisioning |
| 4.x   | Event signing uses Ed25519 (not X25519) |

### Previous Story Intelligence (2.8)

From Story 2.8 implementation:
- **Test pattern:** `async: true` on all pure unit tests — follow same for X25519 tests
- **Module structure:** `alias Nebu.Signature` at top of test; `describe` blocks; no test setup/teardown needed
- **Quality gate:** `mix test --warnings-as-errors` must pass with 0 failures — includes all existing Ed25519 tests
- **Scope discipline:** Do NOT touch `application.ex` or `mix.exs`
- **Key size lesson:** OTP key sizes may differ from libsodium/docs — verify actual returned sizes. For X25519, both keys ARE 32 bytes (no discrepancy expected, but confirm)
- **API shape lesson:** OTP's `:crypto` API shapes vary by algorithm family — for X25519 ECDH, the curve is the 4th arg to `compute_key/4`, not embedded in a list

From Story 2.8 debug log:
> "OTP sign/verify API correction: For EdDSA, curve is included in the key list. Both sign and verify corrected to 4-arg form with curve in key list."

The X25519 `compute_key` is 4-arg with curve as 4th standalone argument — this is DIFFERENT from the Ed25519 sign/verify pattern.

### Build & Test Commands

```bash
# Run signature app tests only (fast, targeted):
cd core && mix test apps/signature --warnings-as-errors

# Run all Elixir unit tests (full suite, no regressions):
make test-unit-elixir
```

**Expected output:** `7 tests, 0 failures` (3 Ed25519 existing + 4 X25519 new)

### Project Structure Notes

- All additions go into existing `Nebu.Signature` module — no new files, no new modules
- Test file adds two new `describe` blocks after the existing one — no reorganization needed
- Module path: `core/apps/signature/lib/nebu/signature.ex` — consistent with umbrella naming `Nebu.{App}.{Module}`
- No `deps` changes — `mix.exs` stays with `deps: []`

### References

- [Source: epics.md#Story-2.9] Authoritative acceptance criteria
- [Source: architecture.md#V1] Two-keypair architecture: Ed25519 (signing) + X25519 (encryption), OTP native
- [Source: architecture.md#Anti-pattern #10] PII encryption via X25519 only — NEVER Ed25519
- [Source: implementation-artifacts/2-8-ed25519-keypair-generation-unit-tests.md] Previous story patterns and API lessons
- [Source: core/apps/signature/lib/nebu/signature.ex] Current module state (has `generate_signing_keypair/0`, no X25519 yet)
- [Source: core/apps/signature/test/nebu_signature_test.exs] Existing test file (3 Ed25519 tests to preserve)
- [Source: core/apps/signature/mix.exs] No external deps — pure OTP :crypto confirmed (`deps: []`)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

- Warning fix: unused variables `alice_pub` and `bob_priv` in `derive_aes_key/1` test prefixed with `_` to satisfy `--warnings-as-errors`

### Completion Notes List

- Implemented `generate_encryption_keypair/0` using `:crypto.generate_key(:ecdh, :x25519)` — returns `{pub, priv}` both 32 bytes
- Implemented `derive_aes_key/1` using `:crypto.hash(:sha256, shared_secret)` — deterministic 32-byte AES-256 key
- Updated `@moduledoc` to remove "(Story 2.9)" forward reference
- Added 4 new unit tests in 2 new `describe` blocks; all 7 tests (3 existing Ed25519 + 4 new X25519) pass
- `mix test --warnings-as-errors` passes with 0 failures, 0 warnings
- No new dependencies — pure OTP `:crypto` as required

### File List

- `core/apps/signature/lib/nebu/signature.ex` (modified)
- `core/apps/signature/test/nebu_signature_test.exs` (modified)
