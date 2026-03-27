# Story 2.8: Ed25519 Keypair Generation + Unit Tests

Status: done

## Story

As a developer,
I want the Elixir `signature` app to generate Ed25519 keypairs using OTP native crypto,
so that message signing has a tested, dependency-free foundation.

## Acceptance Criteria

1. **Given** `Nebu.Signature.generate_signing_keypair/0` function in the `signature` app,
   **When** called,
   **Then** it returns `{public_key, private_key}` as binary tuples using `:crypto.generate_key(:eddsa, :ed25519)`

2. **Given** a generated Ed25519 keypair,
   **When** the key lengths are checked,
   **Then** public key is 32 bytes and private key is 64 bytes

3. **Given** a unit test `sign_and_verify`,
   **When** a message is signed with the private key via `:crypto.sign(:eddsa, :none, message, [private_key], [:ed25519])` and verified with the public key,
   **Then** the verification returns `true`

4. **Given** a unit test `tampered_message`,
   **When** a signed message is modified and re-verified,
   **Then** the verification returns `false`

5. **Given** `mix test --warnings-as-errors` in the `signature` app,
   **When** run,
   **Then** all unit tests pass with 0 failures

## Tasks / Subtasks

- [x] Implement `generate_signing_keypair/0` in `core/apps/signature/lib/nebu/signature.ex` (AC: #1, #2)
  - [x] Replace placeholder module with real implementation
  - [x] Add `@doc` and `@spec` for `generate_signing_keypair/0`
  - [x] Return `{public_key, private_key}` binaries via `:crypto.generate_key(:eddsa, :ed25519)`
  - [x] Update `@moduledoc` (remove "placeholder" language)

- [x] Replace placeholder test in `core/apps/signature/test/nebu_signature_test.exs` (AC: #3, #4, #5)
  - [x] Remove existing placeholder test ("signature app starts")
  - [x] Add `test "generate_signing_keypair/0 returns binary keypair of correct length"`
  - [x] Add `test "sign_and_verify: signature over message verifies correctly"`
  - [x] Add `test "tampered_message: modified message fails verification"`
  - [x] Verify `mix test --warnings-as-errors` passes with 0 failures

## Dev Notes

### What This Story Does

Replaces the placeholder `Nebu.Signature` module with the first real implementation: `generate_signing_keypair/0`. This function is the foundation for message signing (Story 2.13 orchestration, Epic 4 event signing).

**Scope:** Only `generate_signing_keypair/0` — Story 2.9 adds `generate_encryption_keypair/0` (X25519), Story 2.10 adds operational PII encryption, Story 2.11 adds sensitive PII encryption. Do NOT implement anything beyond this story's AC.

**No new dependencies:** `mix.exs` has `deps: []` — pure OTP `:crypto`. Do NOT add any external packages.

### Files to Modify

```
core/apps/signature/
  lib/
    nebu/
      signature.ex           ← MODIFY — replace placeholder with real implementation
  test/
    nebu_signature_test.exs  ← MODIFY — replace placeholder test with real tests
  mix.exs                    ← DO NOT TOUCH (no new deps needed)
  lib/nebu/signature/
    application.ex           ← DO NOT TOUCH
```

### Implementation: signature.ex

**Module:** `Nebu.Signature` (keep same module name — no rename needed)

**OTP API (native, no external library):**
```elixir
{public_key, private_key} = :crypto.generate_key(:eddsa, :ed25519)
```

**Key sizes (required by AC #2):**
- `public_key`: 32 bytes
- `private_key`: 64 bytes

**Architecture rule (CRITICAL):** Ed25519 is for SIGNING only — never for encryption. X25519 is the encryption keypair (Story 2.9). Do NOT confuse the two algorithms. [Source: architecture.md#V1]

**Architecture rule:** PII encryption uses X25519 (Encryption Key) — NEVER Ed25519 (Signing Key). [Source: architecture.md — Anti-pattern #10]

```elixir
defmodule Nebu.Signature do
  @moduledoc """
  Cryptographic key generation and signing operations for Nebu.

  Two separate keypairs per user (Architecture V1):
  - Signing: Ed25519 — message signing, non-repudiation
  - Encryption: X25519 — PII encryption via ECDH (Story 2.9)

  Both use OTP native :crypto — no external dependencies.
  """

  @doc """
  Generates an Ed25519 signing keypair.

  Returns `{public_key, private_key}` as binaries.
  - public_key: 32 bytes
  - private_key: 64 bytes

  Uses OTP native `:crypto.generate_key(:eddsa, :ed25519)` (available since OTP 24+).
  """
  @spec generate_signing_keypair() :: {binary(), binary()}
  def generate_signing_keypair do
    :crypto.generate_key(:eddsa, :ed25519)
  end
end
```

### Implementation: nebu_signature_test.exs

**Test module name:** `Nebu.SignatureTest` (keep existing name — matches file convention)

**Sign API:**
```elixir
signature = :crypto.sign(:eddsa, :none, message, [private_key], [:ed25519])
```

**Verify API:**
```elixir
:crypto.verify(:eddsa, :none, message, signature, [public_key], [:ed25519])
# returns true | false
```

```elixir
defmodule Nebu.SignatureTest do
  use ExUnit.Case, async: true

  alias Nebu.Signature

  describe "generate_signing_keypair/0" do
    test "returns binary keypair of correct length" do
      {pub, priv} = Signature.generate_signing_keypair()
      assert is_binary(pub)
      assert is_binary(priv)
      assert byte_size(pub) == 32
      assert byte_size(priv) == 64
    end

    test "sign_and_verify: signature over message verifies correctly" do
      {pub, priv} = Signature.generate_signing_keypair()
      message = "hello nebu"
      signature = :crypto.sign(:eddsa, :none, message, [priv], [:ed25519])
      assert :crypto.verify(:eddsa, :none, message, signature, [pub], [:ed25519])
    end

    test "tampered_message: modified message fails verification" do
      {pub, priv} = Signature.generate_signing_keypair()
      message = "hello nebu"
      signature = :crypto.sign(:eddsa, :none, message, [priv], [:ed25519])
      tampered = "hello nebu TAMPERED"
      refute :crypto.verify(:eddsa, :none, tampered, signature, [pub], [:ed25519])
    end
  end
end
```

**Note on test style:** Use `async: true` — all operations are pure crypto with no shared state. Use `describe` blocks (established pattern from event_dispatcher tests like `metadata_test.exs`).

### OTP Version Compatibility

OTP `:crypto` Ed25519 support:
- `:crypto.generate_key(:eddsa, :ed25519)` — available since OTP 24+
- `:crypto.sign(:eddsa, :none, ...)` — available since OTP 24+
- The project requires Elixir 1.19+ which targets OTP 26+ — no compatibility issues

**Verify the signature app's elixir version** in `mix.exs`: `elixir: "~> 1.19"` — OTP 24+ guaranteed.

### Previous Story Intelligence (2.7)

From Story 2.7 (Nebu.Grpc.Metadata):
- Test pattern: `async: true` on all pure unit tests
- Module convention: `alias ModuleName` at top of test file before tests
- Quality gate: `mix test --warnings-as-errors` must pass with 0 failures
- Scope discipline: Do NOT touch files not listed in scope (application.ex, mix.exs)
- No new dependencies — Story 2.8 follows same pattern

From Story 2.7 completion notes: "Created `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex` module with... all tests pass with `mix test --warnings-as-errors`"

### Cross-Story Dependencies

| Story | Relationship |
|-------|-------------|
| 2.9   | Adds `generate_encryption_keypair/0` (X25519) to same `Nebu.Signature` module |
| 2.10  | Adds operational PII encryption to `Nebu.Signature` |
| 2.11  | Adds sensitive PII encryption (X25519 ECDH) to `Nebu.Signature` |
| 2.13  | User provisioning calls `generate_signing_keypair/0` + `generate_encryption_keypair/0` |
| 4.x   | Event signing uses `Nebu.Signature` for message non-repudiation |

### Build & Test Commands

```bash
# Run signature app tests only (fast, targeted):
cd core && mix test apps/signature --warnings-as-errors

# Run all Elixir unit tests (full suite):
make test-unit-elixir
```

**Expected output:** `3 tests, 0 failures` (replaced 1 placeholder test with 3 real tests).

### Project Structure Notes

- Alignment: `Nebu.Signature` in `core/apps/signature/lib/nebu/signature.ex` — matches umbrella app naming (`Nebu.{App}.{Module}`)
- Test file: `core/apps/signature/test/nebu_signature_test.exs` — matches Elixir test discovery conventions (no registration needed)
- The `signature` app has no external deps — confirmed in `mix.exs` (`deps: []`); this remains unchanged

### References

- [Source: epics.md#Story-2.8] Authoritative acceptance criteria
- [Source: architecture.md#V1] Two-keypair architecture: Ed25519 (signing) + X25519 (encryption), OTP native
- [Source: architecture.md#G4] Unit tests mandatory for crypto operations
- [Source: architecture.md#Anti-pattern #10] PII encryption via X25519 only — NEVER Ed25519
- [Source: core/apps/signature/lib/nebu/signature.ex] Existing placeholder to replace
- [Source: core/apps/signature/test/nebu_signature_test.exs] Existing placeholder test to replace
- [Source: core/apps/signature/mix.exs] No external deps — pure OTP :crypto confirmed

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

- **OTP private key size discrepancy:** Story AC #2 specified private key = 64 bytes (libsodium convention). OTP's `:crypto.generate_key(:eddsa, :ed25519)` returns 32-byte private key seed. Test updated to assert 32 bytes — the actual OTP behavior. Sign/verify operations work correctly with the 32-byte key.
- **OTP sign/verify API correction:** Story Dev Notes showed 5-arg `:crypto.sign(:eddsa, :none, msg, [key], [:ed25519])` — this is RSA-only. For EdDSA, curve is included in the key list: `:crypto.sign(:eddsa, :none, msg, [key, :ed25519])`. Both sign and verify corrected to 4-arg form with curve in key list.

### Completion Notes List

- Implemented `Nebu.Signature.generate_signing_keypair/0` using OTP native `:crypto.generate_key(:eddsa, :ed25519)` — no external dependencies
- Updated `@moduledoc` to remove placeholder language; describes two-keypair architecture (Ed25519 signing + X25519 encryption)
- Added `@doc` and `@spec generate_signing_keypair() :: {binary(), binary()}`
- Replaced 1 placeholder test with 3 real unit tests using `async: true` and `describe` blocks
- All 3 tests pass with `mix test --warnings-as-errors`: keypair length test, sign-and-verify, tampered-message rejection
- Full Elixir suite: `make test-unit-elixir` passes with 0 failures (no regressions)
- Code review fix: Corrected `@doc` to state private_key is 32 bytes (OTP seed format), not 64 bytes (libsodium convention)

### File List

- `core/apps/signature/lib/nebu/signature.ex` — modified (placeholder → real implementation)
- `core/apps/signature/test/nebu_signature_test.exs` — modified (placeholder → 3 real tests)

## Change Log

- 2026-03-27: Implemented `generate_signing_keypair/0`, replaced placeholder tests with 3 unit tests covering keypair length, sign-and-verify, tampered-message rejection. All tests pass with `--warnings-as-errors`.
