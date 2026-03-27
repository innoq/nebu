# Story 2.10: Operational PII Encryption at Rest + Unit Tests

Status: done

## Story

As a developer,
I want display names and avatar URLs encrypted with a server-side key,
so that Tier 1 PII (Operational PII) is protected at rest and can be anonymised on account deletion.

## Acceptance Criteria

1. **Given** `Nebu.Signature.encrypt_operational_pii/2` taking `plaintext` and `server_key`,
   **When** called,
   **Then** it returns `{ciphertext, nonce}` using AES-256-GCM with a freshly generated 12-byte random nonce

2. **Given** `Nebu.Signature.decrypt_operational_pii/3` taking `ciphertext`, `nonce`, and `server_key`,
   **When** called with values from a prior encryption,
   **Then** it returns the original plaintext (as `{:ok, plaintext}` — per architecture rule #6)

3. **Given** `NEBU_PII_ENCRYPTION_KEY` env var (32-byte hex string = 64 hex chars),
   **When** the `signature` app starts in prod/dev,
   **Then** it reads and validates the key length — exits with a clear error if missing or wrong length

4. **Given** a unit test `encrypt_decrypt_roundtrip`,
   **When** the same plaintext is encrypted twice with the same key,
   **Then** the two ciphertexts differ (random nonce per encryption) but both decrypt to the original plaintext

5. **Given** a unit test `wrong_key_fails`,
   **When** decryption is attempted with a different server key,
   **Then** AES-GCM authentication fails and returns `{:error, :decryption_failed}`

6. **Given** `mix test --warnings-as-errors` in the `signature` app,
   **When** run,
   **Then** all unit tests pass with 0 failures

## Tasks / Subtasks

- [x] Add `encrypt_operational_pii/2` to `core/apps/signature/lib/nebu/signature.ex` (AC: #1)
  - [x] Add `@spec encrypt_operational_pii(binary(), binary()) :: {binary(), binary()}`
  - [x] Generate 12-byte nonce via `:crypto.strong_rand_bytes(12)`
  - [x] Encrypt with `:crypto.crypto_one_time_aead(:aes_256_gcm, key, nonce, plaintext, <<>>, 16, true)` — returns `{ciphertext, tag}`
  - [x] Return `{ciphertext <> tag, nonce}` (tag appended to ciphertext, nonce returned separately)

- [x] Add `decrypt_operational_pii/3` to `core/apps/signature/lib/nebu/signature.ex` (AC: #2, #5)
  - [x] Add `@spec decrypt_operational_pii(binary(), binary(), binary()) :: {:ok, binary()} | {:error, :decryption_failed}`
  - [x] Split last 16 bytes of `ciphertext` as auth tag: `<<ct::binary-size(n), tag::binary-size(16)>> = ciphertext`
  - [x] Decrypt with `:crypto.crypto_one_time_aead(:aes_256_gcm, key, nonce, ct, <<>>, tag, false)` — raises on auth failure
  - [x] Wrap in try/rescue: return `{:ok, plaintext}` on success, `{:error, :decryption_failed}` on any exception

- [x] Add `NEBU_PII_ENCRYPTION_KEY` validation to `core/config/runtime.exs` (AC: #3)
  - [x] Gate with `if config_env() in [:prod, :dev] do`
  - [x] Read hex string, decode via `Base.decode16!`, validate `byte_size == 32`
  - [x] Store decoded key as `config :signature, pii_encryption_key: decoded_key`
  - [x] Add test key to `core/config/test.exs`: `config :signature, pii_encryption_key: :crypto.strong_rand_bytes(32)`

- [x] Add new `describe` block to `core/apps/signature/test/nebu_signature_test.exs` (AC: #4, #5, #6)
  - [x] Add `describe "encrypt_operational_pii/2 and decrypt_operational_pii/3"` block
  - [x] Add `test "encrypt_decrypt_roundtrip: random nonces, both decrypt correctly"`
  - [x] Add `test "wrong_key_fails: different server key returns {:error, :decryption_failed}"`
  - [x] Verify `mix test --warnings-as-errors` passes — no unused variables (prefix with `_`)
  - [x] Confirm all 9 tests pass (7 existing + 2 new)

## Dev Notes

### Scope

Adds symmetric AES-256-GCM encryption for **Tier 1 (Operational) PII** — display names and avatar URLs — protected by a **server-side key** (`NEBU_PII_ENCRYPTION_KEY`).

**This story only:** `encrypt_operational_pii/2` and `decrypt_operational_pii/3` + env var validation.

**NOT in this story:**
- X25519 ECDH encryption (Story 2.11) — Sensitive PII uses a different scheme entirely
- DB writes or user provisioning (Story 2.13)
- Do NOT use `derive_aes_key/1` (X25519 ECDH helper) — this story uses direct server key, no ECDH

### Architecture Context

**NFR-S2:** Sensitive PII is encrypted via X25519; Operational PII is encrypted via server key (both at-rest encrypted).

**PII Tiers:**
| Tier | Data | Encryption Scheme | Story |
|------|------|--------------------|-------|
| Tier 1 (Operational) | display_name, avatar_url | AES-256-GCM with server key | **2.10** |
| Tier 2 (Sensitive) | email, IdP subject | X25519 ECDH → AES-256-GCM with user public key | 2.11 |

[Source: architecture.md#NFR-S2, architecture.md#FR27]

### Files to Modify

```
core/apps/signature/
  lib/nebu/signature.ex                        ← MODIFY — add 2 new functions
  test/nebu_signature_test.exs                 ← MODIFY — add 1 new describe block
  mix.exs                                      ← DO NOT TOUCH (no new deps)
  lib/nebu/signature/application.ex            ← DO NOT TOUCH (no changes needed)
core/config/
  runtime.exs                                  ← MODIFY — add NEBU_PII_ENCRYPTION_KEY validation
  test.exs                                     ← MODIFY — add test PII key config
```

### OTP AES-256-GCM API (exact)

```elixir
# ENCRYPT — 7-arg form, EncFlag = true
# Returns {ciphertext, auth_tag}
{ciphertext, tag} = :crypto.crypto_one_time_aead(
  :aes_256_gcm,    # cipher
  server_key,      # 32-byte binary key
  nonce,           # 12-byte binary nonce
  plaintext,       # binary plaintext
  <<>>,            # AAD (Additional Authenticated Data) — empty binary
  16,              # tag length in bytes (16 = 128-bit tag)
  true             # EncFlag: true = encrypt
)

# DECRYPT — 7-arg form, EncFlag = false
# Returns plaintext on success, RAISES on authentication failure (OTP 24+)
plaintext = :crypto.crypto_one_time_aead(
  :aes_256_gcm,
  server_key,
  nonce,
  ciphertext,      # just the ciphertext (no tag)
  <<>>,            # same AAD as during encryption
  tag,             # 16-byte auth tag binary
  false            # EncFlag: false = decrypt
)
```

**Key API facts:**
- `nonce`: 12 bytes (96-bit) is the GCM standard; use `:crypto.strong_rand_bytes(12)`
- `server_key`: must be exactly 32 bytes for AES-256; validated via `NEBU_PII_ENCRYPTION_KEY`
- `AAD` (`<<>>`): empty here; no additional data to authenticate (no context in this story)
- Auth tag: 16 bytes (128-bit) — maximum GCM tag size, strongest security
- Decryption raises `ErlangError` / `:error` class exception on authentication failure — catch with `rescue`

### Implementation: signature.ex additions

```elixir
@doc """
Encrypts Operational PII (Tier 1: display_name, avatar_url) with the server-side AES-256 key.

Returns `{ciphertext, nonce}` where:
- `ciphertext` is the encrypted data with appended 16-byte GCM auth tag (`encrypted_bytes <> tag`)
- `nonce` is a freshly generated 12-byte random nonce (must be stored alongside ciphertext)

`server_key` must be a 32-byte binary (decoded from `NEBU_PII_ENCRYPTION_KEY`).
"""
@spec encrypt_operational_pii(binary(), binary()) :: {binary(), binary()}
def encrypt_operational_pii(plaintext, server_key) do
  nonce = :crypto.strong_rand_bytes(12)
  {ciphertext, tag} = :crypto.crypto_one_time_aead(:aes_256_gcm, server_key, nonce, plaintext, <<>>, 16, true)
  {ciphertext <> tag, nonce}
end

@doc """
Decrypts Operational PII (Tier 1) encrypted by `encrypt_operational_pii/2`.

Returns `{:ok, plaintext}` on success.
Returns `{:error, :decryption_failed}` if authentication fails (wrong key, tampered data).

`ciphertext` must include the 16-byte GCM auth tag appended by `encrypt_operational_pii/2`.
`nonce` must be the 12-byte nonce returned during encryption.
`server_key` must match the key used during encryption.
"""
@spec decrypt_operational_pii(binary(), binary(), binary()) :: {:ok, binary()} | {:error, :decryption_failed}
def decrypt_operational_pii(ciphertext_with_tag, nonce, server_key) do
  ct_size = byte_size(ciphertext_with_tag) - 16
  <<ciphertext::binary-size(ct_size), tag::binary-size(16)>> = ciphertext_with_tag

  try do
    plaintext = :crypto.crypto_one_time_aead(:aes_256_gcm, server_key, nonce, ciphertext, <<>>, tag, false)
    {:ok, plaintext}
  rescue
    _ -> {:error, :decryption_failed}
  end
end
```

### Implementation: nebu_signature_test.exs additions

**ADD one new describe block after the existing `derive_aes_key/1` block. DO NOT modify existing 7 tests:**

```elixir
describe "encrypt_operational_pii/2 and decrypt_operational_pii/3" do
  test "encrypt_decrypt_roundtrip: random nonces, both decrypt correctly" do
    server_key = :crypto.strong_rand_bytes(32)
    plaintext = "Kai Müller"

    {ciphertext1, nonce1} = Signature.encrypt_operational_pii(plaintext, server_key)
    {ciphertext2, nonce2} = Signature.encrypt_operational_pii(plaintext, server_key)

    # Nonces must differ (random per call)
    assert nonce1 != nonce2
    # Ciphertexts must differ (different nonces → different ciphertexts)
    assert ciphertext1 != ciphertext2

    # Both decrypt to original plaintext
    assert {:ok, ^plaintext} = Signature.decrypt_operational_pii(ciphertext1, nonce1, server_key)
    assert {:ok, ^plaintext} = Signature.decrypt_operational_pii(ciphertext2, nonce2, server_key)
  end

  test "wrong_key_fails: different server key returns {:error, :decryption_failed}" do
    server_key = :crypto.strong_rand_bytes(32)
    wrong_key = :crypto.strong_rand_bytes(32)
    plaintext = "avatar.example.com/kai.jpg"

    {ciphertext, nonce} = Signature.encrypt_operational_pii(plaintext, server_key)

    assert {:error, :decryption_failed} = Signature.decrypt_operational_pii(ciphertext, nonce, wrong_key)
  end
end
```

**Total expected test count after this story:** `9 tests, 0 failures` (3 Ed25519 + 4 X25519 + 2 AES-GCM)

### Implementation: runtime.exs addition

```elixir
# Add to core/config/runtime.exs after the existing placeholder comment:
if config_env() in [:prod, :dev] do
  pii_key_hex =
    System.get_env("NEBU_PII_ENCRYPTION_KEY") ||
      raise "NEBU_PII_ENCRYPTION_KEY is not set. Must be a 64-char hex string (32 bytes)."

  pii_key =
    case Base.decode16(pii_key_hex, case: :mixed) do
      {:ok, decoded} -> decoded
      :error -> raise "NEBU_PII_ENCRYPTION_KEY is not valid hex. Must be a 64-char hex string."
    end

  unless byte_size(pii_key) == 32 do
    raise "NEBU_PII_ENCRYPTION_KEY must decode to exactly 32 bytes, got #{byte_size(pii_key)}"
  end

  config :signature, pii_encryption_key: pii_key
end
```

**test.exs addition:**
```elixir
# Add to core/config/test.exs (alongside existing logger config):
config :signature, pii_encryption_key: :crypto.strong_rand_bytes(32)
```

**Rationale:** `runtime.exs` is evaluated at app startup (including `mix test`), so gating with `config_env() in [:prod, :dev]` prevents startup failure in the test environment. The `test.exs` sets a random key so `Application.get_env(:signature, :pii_encryption_key)` is available if needed by later stories. [Source: core/config/runtime.exs#existing pattern]

### Critical Anti-Patterns to Avoid

**NEVER use X25519/ECDH for Operational PII** — this is a direct server key encryption:
```elixir
# ❌ WRONG — Operational PII does NOT use ECDH
shared = :crypto.compute_key(:ecdh, pub, priv, :x25519)
key = derive_aes_key(shared)

# ✅ CORRECT — Operational PII uses server_key directly
:crypto.crypto_one_time_aead(:aes_256_gcm, server_key, nonce, plaintext, <<>>, 16, true)
```

**NEVER reuse nonces** — always generate fresh via `:crypto.strong_rand_bytes(12)`.

**NEVER store nonce inside ciphertext** — the story returns `{ciphertext_with_tag, nonce}` as a tuple; both must be persisted separately in the DB (Stories 2.12/2.13 will handle the DB columns `display_name_encrypted` and `display_name_nonce`).

**Return `{:ok, plaintext}` not bare `plaintext`** — Architecture rule #6 mandates `{:ok, result}` / `{:error, reason}` for fallible operations. The AC says "returns the original plaintext" as description language; use the tuple form. [Source: architecture.md#AI-Constraints rule 6]

### Previous Story Intelligence (2.8, 2.9)

- **Test pattern:** `async: true` on ExUnit.Case — all pure unit tests; follow same for new describe block
- **Module alias:** `alias Nebu.Signature` at top of test file — already present, don't add again
- **Unused variable warning prevention:** Prefix with `_` any variable not used (e.g., `_nonce2` if only checking inequality). `--warnings-as-errors` will fail the test run otherwise
- **OTP :crypto API shapes vary by algorithm:** Ed25519 sign/verify uses `[key, :ed25519]` list form; X25519 ECDH uses 4th arg; AES-256-GCM uses `:crypto.crypto_one_time_aead/7` — do not mix these up
- **No external deps:** `mix.exs` stays with `deps: []` — everything in OTP `:crypto`
- **Quality gate:** `mix test --warnings-as-errors` must pass with ALL existing tests still green

From Story 2.9 debug log:
> "Warning fix: unused variables prefixed with `_` to satisfy `--warnings-as-errors`"

In the roundtrip test, if `nonce2` and `ciphertext2` are used (for the second decrypt assert), no underscore needed. Review all variables after implementation.

### Build & Test Commands

```bash
# Run signature app tests only (targeted):
cd core && mix test apps/signature --warnings-as-errors

# Run all Elixir unit tests (no regressions):
make test-unit-elixir
```

**Expected output:** `9 tests, 0 failures`

### Project Structure Notes

- All additions go into existing `Nebu.Signature` module — no new files, no new modules, no new Elixir apps
- Test file adds one new `describe` block after `derive_aes_key/1` describe block — consistent ordering (Ed25519 → X25519 keypair → X25519 ECDH → AES-GCM)
- Config changes are minimal: `runtime.exs` guard + `test.exs` key stub
- No changes to umbrella `mix.exs` or any app's `mix.exs`

### References

- [Source: epics.md#Story-2.10] Authoritative acceptance criteria
- [Source: architecture.md#NFR-S2] Operational PII at-rest encryption requirement
- [Source: architecture.md#V1] Two-keypair architecture; Operational PII = server key AES-256-GCM
- [Source: architecture.md#AI-Constraints rule 6] `{:ok, result}` / `{:error, reason}` mandate
- [Source: architecture.md#Anti-pattern #10] PII encryption via X25519 encryption key only — story 2.10 is the exception: Operational PII uses server key, not X25519
- [Source: implementation-artifacts/2-9-x25519-keypair-generation-unit-tests.md] Patterns for tests, `async: true`, `--warnings-as-errors` discipline
- [Source: core/apps/signature/lib/nebu/signature.ex] Current module (3 functions: generate_signing_keypair, generate_encryption_keypair, derive_aes_key)
- [Source: core/apps/signature/test/nebu_signature_test.exs] Current test file (7 tests in 3 describe blocks)
- [Source: core/config/runtime.exs] Pattern for env var validation with `|| raise`

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

- **OTP 28 decrypt return value:** `crypto_one_time_aead/7` in OTP 28 returns `:error` atom on authentication failure instead of raising an exception. The story's Dev Notes described `rescue` as the mechanism, but in OTP 28 the function returns `:error` silently. Fixed by wrapping in `case` inside `try/rescue` to handle both return-value failures and exception-based failures.

### Completion Notes List

- Implemented `encrypt_operational_pii/2`: AES-256-GCM with 12-byte random nonce, returns `{ciphertext <> tag, nonce}` tuple. No ECDH — direct server key as specified.
- Implemented `decrypt_operational_pii/3`: splits 16-byte auth tag from end of ciphertext, uses `case` to handle OTP 28's `:error` return value on auth failure, also catches exceptions via `rescue`.
- Added `NEBU_PII_ENCRYPTION_KEY` env var validation to `runtime.exs` (gated to `:prod, :dev` only); `Base.decode16/2` with `:mixed` case for flexible hex input; length assertion for exactly 32 bytes.
- Added `config :signature, pii_encryption_key: :crypto.strong_rand_bytes(32)` to `test.exs` for test environment.
- All 9 tests pass (`mix test --warnings-as-errors`): 3 Ed25519 + 4 X25519/ECDH + 2 AES-GCM. Zero regressions.

### File List

- core/apps/signature/lib/nebu/signature.ex
- core/apps/signature/test/nebu_signature_test.exs
- core/config/runtime.exs
- core/config/test.exs

## Senior Developer Review (AI)

**Reviewer:** Phil (adversarial code review via claude-opus-4-6)
**Date:** 2026-03-27
**Outcome:** Approved — clean review, no issues found.

**Checklist:**
- [x] Story file loaded
- [x] Story Status verified as reviewable (review)
- [x] Epic and Story IDs resolved (2.10)
- [x] Architecture/standards docs loaded
- [x] Tech stack detected (Elixir/OTP, :crypto AES-256-GCM)
- [x] Acceptance Criteria cross-checked against implementation (6/6 IMPLEMENTED)
- [x] File List reviewed and validated for completeness (4/4 match git)
- [x] Tests identified and mapped to ACs; no gaps
- [x] Code quality review performed on changed files
- [x] Security review performed — correct crypto usage confirmed
- [x] Outcome: Approved
- [x] Review notes appended
- [x] Change Log updated
- [x] Status updated to done
- [x] Sprint status synced

**Notes:**
- OTP 28 compatibility: `decrypt_operational_pii/3` uses `case` + `rescue` dual-approach to handle both `:error` return and exception-based failures. Well-documented in Dev Agent Record.
- All 9 tests pass with `--warnings-as-errors`. No regressions across all 6 umbrella apps (34 total tests).
- Git vs Story File List: perfect match (0 discrepancies).

## Change Log

- 2026-03-27: Code review passed — approved with 0 issues. Story status → done.
- 2026-03-27: Implemented `encrypt_operational_pii/2` and `decrypt_operational_pii/3` in `Nebu.Signature` — AES-256-GCM Tier-1-PII-Verschlüsselung mit server-seitigem Schlüssel. `NEBU_PII_ENCRYPTION_KEY` Env-Var-Validierung in `runtime.exs` (prod/dev) und Test-Key-Stub in `test.exs`. 9 tests, 0 failures.
