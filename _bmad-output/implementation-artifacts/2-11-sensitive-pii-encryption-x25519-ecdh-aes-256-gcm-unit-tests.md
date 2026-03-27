# Story 2.11: Sensitive PII Encryption (X25519 ECDH + AES-256-GCM) + Unit Tests

Status: done

## Story

As a developer,
I want email and IdP subject encrypted with the user's X25519 public key via ECDH key exchange,
so that Tier 2 PII (Sensitive PII) becomes cryptographically irrecoverable when the user's private key is deleted (DSGVO Right to be Forgotten).

## Acceptance Criteria

1. **Given** `Nebu.Signature.encrypt_sensitive_pii/2` taking `plaintext` and `recipient_public_key`,
   **When** called,
   **Then** it generates an ephemeral X25519 keypair, performs ECDH to derive a shared secret, derives an AES-256 key via `derive_aes_key/1`, encrypts with AES-256-GCM, and returns `{ciphertext, ephemeral_public_key, nonce}`

2. **Given** `Nebu.Signature.decrypt_sensitive_pii/4` taking `ciphertext`, `ephemeral_public_key`, `nonce`, and `recipient_private_key`,
   **When** called with values from a prior encryption and the matching private key,
   **Then** it returns `{:ok, original_plaintext}`

3. **Given** a unit test `encrypt_decrypt_roundtrip`,
   **When** sensitive PII is encrypted with a recipient's public key and decrypted with the matching private key,
   **Then** the original plaintext is recovered

4. **Given** a unit test `deletion_makes_irrecoverable`,
   **When** the private key is `nil` and decryption is attempted,
   **Then** it returns `{:error, :no_private_key}` — the data is effectively deleted (NFR-S5, NFR-C1)

5. **Given** `mix test --warnings-as-errors` in the `signature` app,
   **When** run,
   **Then** all 11 unit tests pass with 0 failures

## Tasks / Subtasks

- [x] Add `encrypt_sensitive_pii/2` to `core/apps/signature/lib/nebu/signature.ex` (AC: #1)
  - [x] Add `@spec encrypt_sensitive_pii(binary(), binary()) :: {binary(), binary(), binary()}`
  - [x] Generate ephemeral X25519 keypair: `{ephemeral_pub, ephemeral_priv} = :crypto.generate_key(:ecdh, :x25519)`
  - [x] ECDH: `shared = :crypto.compute_key(:ecdh, recipient_public_key, ephemeral_priv, :x25519)`
  - [x] Derive AES key: `aes_key = derive_aes_key(shared)` — reuse existing function, do NOT reimplement
  - [x] Generate 12-byte nonce: `nonce = :crypto.strong_rand_bytes(12)`
  - [x] Encrypt: `{ciphertext, tag} = :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, plaintext, <<>>, 16, true)`
  - [x] Return `{ciphertext <> tag, ephemeral_pub, nonce}`

- [x] Add `decrypt_sensitive_pii/4` to `core/apps/signature/lib/nebu/signature.ex` (AC: #2, #4)
  - [x] Add `@spec decrypt_sensitive_pii(binary(), binary(), binary(), binary() | nil) :: {:ok, binary()} | {:error, :no_private_key} | {:error, :decryption_failed}`
  - [x] Add nil-guard clause first: `def decrypt_sensitive_pii(_, _, _, nil), do: {:error, :no_private_key}`
  - [x] ECDH: `shared = :crypto.compute_key(:ecdh, ephemeral_public_key, recipient_private_key, :x25519)`
  - [x] Derive AES key: `aes_key = derive_aes_key(shared)`
  - [x] Split tag: `ct_size = byte_size(ciphertext_with_tag) - 16; <<ct::binary-size(ct_size), tag::binary-size(16)>> = ciphertext_with_tag`
  - [x] Decrypt with OTP 28 compat pattern (case + rescue — same as `decrypt_operational_pii/3`)
  - [x] Return `{:ok, plaintext}` on success, `{:error, :decryption_failed}` on failure

- [x] Add new `describe` block to `core/apps/signature/test/nebu_signature_test.exs` (AC: #3, #4, #5)
  - [x] Add `describe "encrypt_sensitive_pii/2 and decrypt_sensitive_pii/4"` block after the operational PII block
  - [x] Add `test "encrypt_decrypt_roundtrip: recovers plaintext with matching private key"`
  - [x] Add `test "deletion_makes_irrecoverable: nil private key returns {:error, :no_private_key}"`
  - [x] Verify `mix test --warnings-as-errors` passes — prefix all unused vars with `_`
  - [x] Confirm **11 tests, 0 failures** (9 existing + 2 new)

## Dev Notes

### Scope

Adds asymmetric (ECDH-based) AES-256-GCM encryption for **Tier 2 (Sensitive) PII** — email and IdP subject — protected by the **user's X25519 public key**.

**This story only:** `encrypt_sensitive_pii/2` and `decrypt_sensitive_pii/4` + unit tests. No DB writes, no user provisioning (Story 2.13).

**NOT in this story:**
- DB write-on-login (Story 2.12)
- User provisioning orchestration (Story 2.13)
- No new env vars (no server key needed — keys are per-user X25519 keypairs)
- No changes to `runtime.exs` or `test.exs` — do NOT touch config files

### PII Tier Reference (from Story 2.10 Dev Notes)

| Tier | Data | Encryption Scheme | Story |
|------|------|--------------------|-------|
| Tier 1 (Operational) | display_name, avatar_url | AES-256-GCM with server key | 2.10 (done) |
| **Tier 2 (Sensitive)** | **email, IdP subject** | **X25519 ECDH → AES-256-GCM** | **2.11 (this story)** |

[Source: architecture.md#V1, architecture.md#NFR-S2]

### Encryption Protocol: Ephemeral ECDH

Story 2-11 uses **ephemeral sender keys** (not the system sender's permanent key). This gives forward-secrecy properties:
- Every encryption call generates a fresh ephemeral X25519 keypair
- ECDH: `shared_secret = ECDH(ephemeral_priv, recipient_pub)` — same as `ECDH(recipient_priv, ephemeral_pub)`
- `ephemeral_pub` must be stored alongside the ciphertext so the recipient can recompute `shared_secret`
- Deleting `recipient_priv` makes the shared secret permanently unrecoverable → DSGVO deletion

### Reuse `derive_aes_key/1` — Do Not Reinvent

`derive_aes_key/1` already exists in `Nebu.Signature` (added in Story 2.9):
```elixir
def derive_aes_key(shared_secret) do
  :crypto.hash(:sha256, shared_secret)
end
```
**ALWAYS call `derive_aes_key(shared_secret)` — do NOT inline the SHA-256 hash.**

### Exact OTP API Calls

```elixir
# Step 1: Generate ephemeral keypair
{ephemeral_pub, ephemeral_priv} = :crypto.generate_key(:ecdh, :x25519)

# Step 2: ECDH shared secret (ephemeral private + recipient public)
shared = :crypto.compute_key(:ecdh, recipient_public_key, ephemeral_priv, :x25519)
# NOTE: arg order is (public_key, private_key, curve) — NOT (priv, pub)
# Returns 32-byte binary

# Step 3: Derive AES-256 key
aes_key = derive_aes_key(shared)  # SHA-256 of shared_secret → 32 bytes

# Step 4: Encrypt (identical to operational PII AES call)
nonce = :crypto.strong_rand_bytes(12)
{ciphertext, tag} = :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, plaintext, <<>>, 16, true)
# Returns {ciphertext_binary, 16-byte_auth_tag}

# Step 5: Return tuple (3-element)
{ciphertext <> tag, ephemeral_pub, nonce}
```

```elixir
# Decryption Step 1: guard clause for DSGVO deletion case
def decrypt_sensitive_pii(_, _, _, nil), do: {:error, :no_private_key}

# Decryption Step 2: ECDH reconstruct shared secret
# (symmetric: recipient_priv + ephemeral_pub == ephemeral_priv + recipient_pub)
shared = :crypto.compute_key(:ecdh, ephemeral_public_key, recipient_private_key, :x25519)

# Decryption Step 3: Derive same AES key
aes_key = derive_aes_key(shared)

# Decryption Step 4: Split tag (16 bytes from end)
ct_size = byte_size(ciphertext_with_tag) - 16
<<ct::binary-size(ct_size), tag::binary-size(16)>> = ciphertext_with_tag

# Decryption Step 5: OTP 28 compat decrypt (SAME pattern as decrypt_operational_pii/3)
try do
  case :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, ct, <<>>, tag, false) do
    :error -> {:error, :decryption_failed}
    plaintext -> {:ok, plaintext}
  end
rescue
  _ -> {:error, :decryption_failed}
end
```

### Complete Implementation: signature.ex additions

Add after `decrypt_operational_pii/3`, before the closing `end` of the module:

```elixir
@doc """
Encrypts Sensitive PII (Tier 2: email, IdP subject) with the recipient's X25519 public key.

Uses ephemeral ECDH: a fresh X25519 keypair is generated per call, shared secret derived,
then AES-256-GCM encryption applied. The ephemeral public key must be stored alongside
the ciphertext so decryption can reconstruct the shared secret.

Returns `{ciphertext, ephemeral_public_key, nonce}` where:
- `ciphertext` = encrypted data with appended 16-byte GCM auth tag
- `ephemeral_public_key` = 32-byte public key of the ephemeral sender (store in DB)
- `nonce` = 12-byte random nonce (store in DB)

DSGVO: deleting `recipient_private_key` renders this data permanently irrecoverable.
"""
@spec encrypt_sensitive_pii(binary(), binary()) :: {binary(), binary(), binary()}
def encrypt_sensitive_pii(plaintext, recipient_public_key) do
  {ephemeral_pub, ephemeral_priv} = :crypto.generate_key(:ecdh, :x25519)
  shared = :crypto.compute_key(:ecdh, recipient_public_key, ephemeral_priv, :x25519)
  aes_key = derive_aes_key(shared)
  nonce = :crypto.strong_rand_bytes(12)
  {ciphertext, tag} = :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, plaintext, <<>>, 16, true)
  {ciphertext <> tag, ephemeral_pub, nonce}
end

@doc """
Decrypts Sensitive PII (Tier 2) encrypted by `encrypt_sensitive_pii/2`.

Returns `{:ok, plaintext}` on success.
Returns `{:error, :no_private_key}` when `recipient_private_key` is `nil` — DSGVO deletion case.
Returns `{:error, :decryption_failed}` if AES-GCM authentication fails.

`recipient_private_key` must match the public key used during encryption.
`ephemeral_public_key` must be the 32-byte value returned by `encrypt_sensitive_pii/2`.
`nonce` must be the 12-byte value returned by `encrypt_sensitive_pii/2`.
"""
@spec decrypt_sensitive_pii(binary(), binary(), binary(), binary() | nil) ::
        {:ok, binary()} | {:error, :no_private_key} | {:error, :decryption_failed}
def decrypt_sensitive_pii(_, _, _, nil), do: {:error, :no_private_key}

def decrypt_sensitive_pii(ciphertext_with_tag, ephemeral_public_key, nonce, recipient_private_key) do
  shared = :crypto.compute_key(:ecdh, ephemeral_public_key, recipient_private_key, :x25519)
  aes_key = derive_aes_key(shared)
  ct_size = byte_size(ciphertext_with_tag) - 16
  <<ciphertext::binary-size(ct_size), tag::binary-size(16)>> = ciphertext_with_tag

  try do
    case :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, ciphertext, <<>>, tag, false) do
      :error -> {:error, :decryption_failed}
      plaintext -> {:ok, plaintext}
    end
  rescue
    _ -> {:error, :decryption_failed}
  end
end
```

### Complete Implementation: nebu_signature_test.exs additions

**ADD one new describe block at the END of the test file, after the `encrypt_operational_pii` describe block. DO NOT modify existing 9 tests.**

```elixir
describe "encrypt_sensitive_pii/2 and decrypt_sensitive_pii/4" do
  test "encrypt_decrypt_roundtrip: recovers plaintext with matching private key" do
    {recipient_pub, recipient_priv} = Signature.generate_encryption_keypair()
    plaintext = "kai.mueller@example.com"

    {ciphertext, ephemeral_pub, nonce} = Signature.encrypt_sensitive_pii(plaintext, recipient_pub)

    assert {:ok, ^plaintext} = Signature.decrypt_sensitive_pii(ciphertext, ephemeral_pub, nonce, recipient_priv)
  end

  test "deletion_makes_irrecoverable: nil private key returns {:error, :no_private_key}" do
    {recipient_pub, _recipient_priv} = Signature.generate_encryption_keypair()
    plaintext = "idp-subject-uuid-1234"

    {ciphertext, ephemeral_pub, nonce} = Signature.encrypt_sensitive_pii(plaintext, recipient_pub)

    assert {:error, :no_private_key} = Signature.decrypt_sensitive_pii(ciphertext, ephemeral_pub, nonce, nil)
  end
end
```

**Total expected test count after this story:** `11 tests, 0 failures`
(3 Ed25519 + 2 X25519 keypair + 2 derive_aes_key + 2 AES-GCM operational + 2 ECDH sensitive)

### Critical Anti-Patterns to Avoid

**NEVER use `encrypt_operational_pii/2` for Sensitive PII** — Tier 2 uses ECDH, not a server key:
```elixir
# ❌ WRONG — Sensitive PII does NOT use a server key
encrypt_operational_pii(email, server_key)

# ✅ CORRECT — Sensitive PII uses recipient's X25519 public key
encrypt_sensitive_pii(email, recipient_public_key)
```

**NEVER skip the ephemeral keypair** — the ephemeral private key is discarded after ECDH; only the ephemeral public key is stored:
```elixir
# ❌ WRONG — using recipient public key for ECDH with a static sender key
{fixed_pub, fixed_priv} = Application.get_env(...)  # no such thing here

# ✅ CORRECT — fresh ephemeral keypair every call
{ephemeral_pub, ephemeral_priv} = :crypto.generate_key(:ecdh, :x25519)
```

**NEVER inline SHA-256 — always call `derive_aes_key/1`:**
```elixir
# ❌ WRONG
aes_key = :crypto.hash(:sha256, shared)

# ✅ CORRECT — reuse existing function
aes_key = derive_aes_key(shared)
```

**NEVER use the wrong `:crypto.compute_key/4` arg order:**
```elixir
# ❌ WRONG — reversed args
:crypto.compute_key(:ecdh, private_key, public_key, :x25519)

# ✅ CORRECT — (public_key, private_key, curve)
:crypto.compute_key(:ecdh, public_key, private_key, :x25519)
```

**Return tuple is 3-element for encryption (not 2-element like operational PII):**
```elixir
# ❌ WRONG — missing ephemeral_pub
{ciphertext <> tag, nonce}

# ✅ CORRECT
{ciphertext <> tag, ephemeral_pub, nonce}
```

### OTP 28 Compatibility Note (from Story 2-10 Debug Log)

`crypto_one_time_aead/7` in OTP 28 returns `:error` atom on authentication failure **instead of raising an exception**. The `decrypt_sensitive_pii/4` implementation above uses `case` inside `try/rescue` — identical to the battle-tested `decrypt_operational_pii/3` pattern. **Do NOT simplify to `rescue`-only.**

### Previous Story Intelligence (2.10)

- **Test pattern:** `async: true` on `use ExUnit.Case` — already present at file top, do NOT add again
- **Module alias:** `alias Nebu.Signature` — already present, do NOT add again
- **Unused variable discipline:** Prefix all unused vars with `_` or `mix test --warnings-as-errors` WILL fail
- **Test order:** Describe block order = Ed25519 → X25519 keypair → derive_aes_key → operational PII → **sensitive PII (new)**
- **No external deps:** `mix.exs` stays with `deps: []` — everything in OTP `:crypto`
- **Do NOT touch `runtime.exs` or `test.exs`** — no new env vars, no config changes needed for this story

### Build & Test Commands

```bash
# Run signature app tests only (targeted):
cd core && mix test apps/signature --warnings-as-errors

# Run all Elixir unit tests (no regressions):
make test-unit-elixir
```

**Expected output:** `11 tests, 0 failures`

### Files to Modify

```
core/apps/signature/
  lib/nebu/signature.ex                        ← MODIFY — add 2 new functions
  test/nebu_signature_test.exs                 ← MODIFY — add 1 new describe block (2 tests)
```

**Do NOT touch:**
- `core/apps/signature/mix.exs` — no new deps
- `core/apps/signature/lib/nebu/signature/application.ex` — no changes
- `core/config/runtime.exs` — no new env vars
- `core/config/test.exs` — no new test config
- Any other file in the umbrella

### Project Structure Notes

- All additions go into existing `Nebu.Signature` module — no new files, no new modules
- `decrypt_sensitive_pii/4` has two clauses: nil-guard first, then the main implementation
- Function ordering in module: `encrypt_sensitive_pii/2` then `decrypt_sensitive_pii/4` (keep encrypt/decrypt pairs together)

### References

- [Source: epics.md#Story-2.11] Authoritative user story, acceptance criteria, technical requirements
- [Source: architecture.md#V1] Two-keypair architecture; Sensitive PII = X25519 ECDH → AES-256-GCM
- [Source: architecture.md#NFR-S2] Sensitive PII encryption requirement
- [Source: architecture.md#AI-Constraints rule 6] `{:ok, result}` / `{:error, reason}` mandate for fallible ops
- [Source: architecture.md#Anti-pattern #10] PII encryption exclusively via X25519 Encryption Key
- [Source: implementation-artifacts/2-10-operational-pii-encryption-at-rest-unit-tests.md] OTP 28 compat pattern (`case` + `rescue`), test patterns, `--warnings-as-errors` discipline
- [Source: core/apps/signature/lib/nebu/signature.ex] Current module — 5 functions; add 2 more here
- [Source: core/apps/signature/test/nebu_signature_test.exs] Current test file — 9 tests in 4 describe blocks; add 1 more block

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

None — implementation matched story spec exactly on first attempt.

### Completion Notes List

- Implemented `encrypt_sensitive_pii/2`: ephemeral X25519 keypair generation → ECDH shared secret → `derive_aes_key/1` → AES-256-GCM → returns 3-tuple `{ciphertext <> tag, ephemeral_pub, nonce}`
- Implemented `decrypt_sensitive_pii/4`: nil-guard clause first (DSGVO deletion case → `{:error, :no_private_key}`), then ECDH reconstruct + AES-256-GCM decrypt using OTP 28 compat pattern (case + rescue)
- Added 2 unit tests in new `describe` block: `encrypt_decrypt_roundtrip` and `deletion_makes_irrecoverable`
- All 11 tests pass with `--warnings-as-errors` (3 Ed25519 + 2 X25519 keypair + 2 derive_aes_key + 2 operational PII + 2 sensitive PII)
- No regressions — full umbrella test suite green

### File List

- `core/apps/signature/lib/nebu/signature.ex` — MODIFIED: added `encrypt_sensitive_pii/2` and `decrypt_sensitive_pii/4`
- `core/apps/signature/test/nebu_signature_test.exs` — MODIFIED: added `describe "encrypt_sensitive_pii/2 and decrypt_sensitive_pii/4"` block with 2 tests

### Change Log

- 2026-03-27: Added `encrypt_sensitive_pii/2` (ephemeral ECDH + AES-256-GCM) and `decrypt_sensitive_pii/4` (with DSGVO nil-guard) to `Nebu.Signature`; added 2 unit tests; 11 tests total, 0 failures
