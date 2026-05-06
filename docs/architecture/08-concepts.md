# 8 Cross-Cutting Concepts

## Authentication and Authorization

**OIDC-only (no local accounts):** Every user authenticates via an OIDC-conformant provider
(Keycloak, Azure AD, Dex, Google). No local passwords, no shadow directory.

**Token flow:**
```
Matrix Client → Go Gateway:
  Authorization: Bearer <oidc_token>
  → Go validates token via OIDC provider (JWK verification)
  → extracts: user_id (@sub:server.name), system_role

Go → Elixir (gRPC metadata):
  "x-user-id": "@user:server.name"
  "x-system-role": "user" | "instance_admin" | "compliance_officer"

Elixir: trusts Go fully — no own token validation
Auth token never forwarded to Elixir — only user_id + system_role
```

**Bootstrap mode:** First OIDC login automatically receives `instance_admin`. Bootstrap mode is
permanently disabled after the first admin setup. No default password, no insecure fallback.

**Rate limiting (per IP):**

| Tier | Rate | Endpoints |
|---|---|---|
| strict | 30/min, burst 10 | `POST /login` (brute-force risk) |
| compliance | 10/min, burst 10 | All compliance/* and admin key/anonymize |
| admin | 60/min, burst 20 | Login UI, bootstrap wizard |
| medium | 30/min, burst 10 | SSO redirect/callback, public profile |
| loose | 300/min, burst 100 | Discovery, capabilities, unauthenticated stubs |

## Cryptography

**Two key pairs per user (ADR-007):**

| Key Pair | Algorithm | Purpose |
|---|---|---|
| Signing Key | Ed25519 | Message signatures, non-repudiation |
| Encryption Key | X25519 (ECDH) | PII encryption, GDPR deletion |

Generated at user registration via Erlang/OTP 27 `:crypto` — no external Hex packages.

**Event signing:** All Matrix events are signed with the sender's Ed25519 private key before storage.
`Nebu.EventId.generate/1` computes `$<base64url(SHA-256(canonical_json(event)))>`.

**GDPR Right-to-be-Forgotten:** Delete both private keys → sensitive PII (email, IdP subject) is
permanently irrecoverable. Audit log integrity is preserved — message signatures remain verifiable
via the permanent public key.

**Compliance key:** A separate Ed25519 keypair in `server_config` (AES-256-GCM encrypted) signs
compliance export tokens. Survives Elixir restarts (unlike the ephemeral `:nebu_signing_key`).

## Audit Logging

All administrative actions and compliance access events are written to an append-only `audit_logs`
table. An `BEFORE INSERT` trigger prevents UPDATE and DELETE (enforced at the DB level via RLS).
Entries are Ed25519-signed for tamper evidence. Retention is configurable (default: 2555 days / 7 years).

## Three PII Tiers

| Tier | Data | At Rest | GDPR Deletion |
|---|---|---|---|
| Operational PII | Display name, avatar | Encrypted at rest | Overwrite with "Deleted User [id]" |
| Sensitive PII | Email, IdP subject | Encrypted with user's X25519 key | Delete private key → irrecoverable |
| Message Content | Chat messages | Plain in DB (audit requirement) | Not deleted; sender anonymized |

## Per-Device Sync Token Isolation (Story 9-22)

Matrix clients identify themselves with a `device_id` extracted from the `"did"` claim in the
JWT access token. The Elixir Core maintains independent sync checkpoints per `(user_id, device_id)`
in the `sync_tokens` table (composite PK, migration 000041).

**Design invariants:**

- A single user with N active devices has N independent rows in `sync_tokens`.
- Each device's `since` token is advanced only by that device's sync responses.
- On logout, only the `(user_id, device_id)` row is removed; other devices are unaffected.
- Legacy clients (no `device_id` in JWT) fall back to the `device_id = ''` row; a token mismatch
  triggers a full initial sync (safe degradation).
- `persist_since_token/4` and `get_since_token/2` are the device-aware arities in
  `Nebu.Session.PgStore.Postgres`; `/3` and `/1` remain for backward compatibility.

**Token cleanup on logout:**
`POST /logout` triggers `gRPC InvalidateUserSessions(user_id, device_id)` → Elixir
`SessionSupervisor.destroy_session/2` → DB transaction deletes `sync_tokens` + `sessions` rows
for that device. ETS is NOT evicted (other devices may still be active).

## Error Handling

**Go:** Return-based, no panic in library code. gRPC status codes map to HTTP status codes at the
gateway boundary. Matrix endpoints return `{"errcode": "M_...", "error": "..."}` format.

**Elixir:** Tagged tuples `{:ok, result}` / `{:error, reason}`. No raise/throw for business logic.
Let-it-crash + OTP Supervisor Trees for unexpected failures.

## Logging

**Go:** `log/slog` (stdlib), structured key-value pairs.
**Elixir:** `Logger` with keyword metadata.

PII (email, display name) must never appear in log output. Debug level never enabled in production.

## API Response Formats

**Strict separation:** Matrix endpoints return Matrix format; Admin API returns wrapper format.
No mixing.

```json
// Matrix API success
{"event_id": "$abc123", "room_id": "!xyz:server.name"}

// Admin API success
{"data": {...}, "meta": {"cursor": "v1_abc", "limit": 50}}

// Admin API error
{"error": {"code": "USER_NOT_FOUND", "message": "..."}}
```

## Admin UI Security

CSRF double-submit cookie on all state-changing Admin UI POST endpoints. Security headers
middleware on all `/admin/*` routes (CSP, HSTS, X-Frame-Options). Session cookies are
`HttpOnly`, `SameSite=Lax`. Admin UI templates served via `go:embed` — no filesystem access
at runtime.

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Cross-Cutting Concerns, §Auth-Token-Flow, §Enforcement; `_bmad-output/planning-artifacts/prd.md`, §Cryptographic Identity Architecture; Story 9-22 (per-device sync token isolation, logout cleanup)_
