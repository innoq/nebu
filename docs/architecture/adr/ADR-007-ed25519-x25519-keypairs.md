# ADR-007: Ed25519 (Signing) + X25519 (Encryption) — Two Key Pairs per User

## Status

Accepted — 2026-03-18

## Context

Nebu requires two cryptographic capabilities per user:
1. **Message signing** (non-repudiation): Prove that a message was sent by a specific user and
   was not tampered with. Used for audit export and compliance.
2. **PII encryption** (GDPR deletion): Encrypt sensitive PII (email, IdP subject) so that
   deleting the private key makes it permanently irrecoverable.

A common misconception is that Ed25519 can be used for both signing and encryption. This is wrong:
Ed25519 is a signing algorithm (Edwards-curve Digital Signature Algorithm). It cannot perform
Diffie-Hellman key agreement, which is required for asymmetric encryption.

For encryption, the correct algorithm is **X25519 (Curve25519 Diffie-Hellman)**: two parties
exchange public keys, compute a shared secret, and use that secret to derive an AES-256-GCM key.

Both Ed25519 and X25519 are natively supported in Erlang/OTP 27 via `:crypto` — no external
Hex packages are required.

Reference models: Signal Protocol, Age Encryption, WireGuard — all use separate Ed25519/X25519 pairs.

## Decision

Every Nebu user receives **two separate key pairs** at registration:

| Key Pair | Algorithm | Purpose | OTP Module |
|---|---|---|---|
| Signing Key | Ed25519 | Message signing, non-repudiation | `:crypto.sign/4` with `eddsa` |
| Encryption Key | X25519 (ECDH) | PII encryption, GDPR deletion | `:crypto.generate_key(:ecdh, :x25519)` |

```elixir
# Key generation at user registration
{signing_pub, signing_priv}    = :crypto.generate_key(:eddsa, :ed25519)
{encrypt_pub, encrypt_priv}    = :crypto.generate_key(:ecdh, :x25519)
```

GDPR Right-to-be-Forgotten: delete both private keys → sensitive PII irrecoverable;
audit log integrity preserved (event signatures remain verifiable via the permanent public key).

## Consequences

**Positive:**
- Cryptographically correct: right algorithm for each purpose
- GDPR deletion is irreversible and verifiable (audit trail entry `deletion_succeeded`)
- Both algorithms natively in OTP 27 — no supply chain risk from external crypto packages
- Message non-repudiation survives GDPR deletion (public signing key is permanent)

**Negative:**
- Two key pairs per user doubles the key management surface
- Developers must not confuse signing key with encryption key (enforced by code review rule #10)
- X25519 ECDH requires understanding of key agreement protocols

**Enforcement rule:** "PII encryption exclusively via X25519 (Encryption Key) — never via Ed25519
(Signing Key)." This is explicitly listed as an anti-pattern in `_bmad-output/planning-artifacts/architecture.md`.

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Core Architectural Decisions (V1); `_bmad-output/planning-artifacts/prd.md`, §Cryptographic Identity Architecture; `CLAUDE.md`, §ADR Table_
