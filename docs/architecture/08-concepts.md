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

**Failure audit trail pattern (Story 9-27):** Multi-step operations (e.g. room upgrade) that can
fail partway through wrap their entire body in `try/rescue`. The `rescue` clause writes a "failure"
audit entry (including an `"error"` metadata field with the exception message) before reraising the
original exception. A nested `try/rescue` around the audit write itself ensures an outage in the
audit writer never masks the original error. This guarantees that any partially-applied operation
leaves a forensic record even when the gRPC call ultimately returns an error to the client.

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

**gRPC error surface rule (Story 9-27):** Elixir gRPC handlers must use `raise GRPC.RPCError,
status: GRPC.Status.<code>(), message: "..."` to propagate errors to the Go gateway. Bare `:ok =`
pattern matches on `Room.Server` calls produce `MatchError` at runtime, which gRPC-elixir maps to
`codes.Unknown` → HTTP 500 with no structured error message. The correct form is a `case` expression
that raises `GRPC.RPCError` with `GRPC.Status.internal()` on unexpected `{:error, reason}` tuples.
This distinction is critical: `codes.Unknown` is ambiguous whereas `codes.Internal` correctly
signals a server-side failure to the Go gateway's error mapper.

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

## Testing Architecture (Story 9-26)

Nebu uses a two-layer E2E test strategy, splitting tests by the level of observable behavior:

| Layer | Runner | Scope | Location |
|---|---|---|---|
| HTTP / Matrix API | Godog + `net/http` | REST endpoints, Matrix CS protocol, gRPC | `gateway/features/` |
| Browser / UI | Playwright + `playwright-bdd` | Element Web flows, Admin UI, real OIDC redirects | `e2e/` |

**No plain `.spec.ts` files without a `.feature` counterpart are accepted for new stories.**
Gherkin `.feature` files are the single source of truth for all E2E scenarios.

### Browser-First E2E Layer

All browser-level tests use `playwright-bdd` as the execution engine. Feature files are defined
first (failing), step definitions implement them in TypeScript. The `playwright.config.ts` registers
three test projects:

- `chromium` — legacy API-contract tests (no BDD change)
- `admin-ui` — Admin UI BDD tests via `playwright-bdd`
- `element-web` — Element Web browser-first E2E via `playwright-bdd`

```
e2e/
  features/
    element/            ← login, room/{create,join,leave}, messages/{send,receive}
    admin/              ← bootstrap, dashboard, auth-guard, users, rooms, audit-log
  step-definitions/
    common/             ← auth, navigation, stack-health, room-setup, assertions (shared)
    element/            ← login, room, messages steps
    admin/              ← bootstrap, dashboard, users, rooms steps
  fixtures/
    users.ts            ← NEBU_USERS const (4 pre-configured Dex test users)
    dex-auth.ts         ← loginViaOidcBrowser(), ensureStorageState(), getApiSession()
    element-app.ts      ← ElementAppPage (Playwright page object for Element Web)
    nebu-fixtures.ts    ← createBdd(test) — exports { Given, When, Then }
  global-setup.ts       ← warms token sidecars + bootstraps admin before tests
```

### Token Sidecar Pattern for IndexedDB Sessions (Story 9-26b)

Element Web 1.11+ stores `mx_access_token` in **IndexedDB**, not localStorage. Playwright's
`storageState()` captures only localStorage and cookies — not IndexedDB. The token sidecar
pattern solves this:

1. `loginViaOidcBrowser()` intercepts the `POST /_matrix/client/v3/login` response via
   Playwright route interception, captures the `access_token` + `user_id` from the JSON body.
2. The token is written to `e2e/auth-state/{user}.token.json` (the "sidecar" file).
3. `getApiSession()` reads the sidecar to obtain a valid token for Matrix API setup calls
   (`createRoom`, `inviteUser`) — without touching localStorage or IndexedDB.
4. `global-setup.ts` warms sidecars for `alex`, `marie`, and `kai` before any test runs.
5. Each test context performs a **fresh OIDC browser login** (no storageState restore) because
   IndexedDB sessions cannot be injected via `browser.newContext({ storageState })`.

The `auth-state/` directory is gitignored. Sidecars expire after 12 hours (staleness check in
`ensureStorageState()`).

### API Seeding vs. UI Assertion Boundary

Matrix API calls via `page.request` are permitted in **Given/When (setup)** steps only:

| Step type | API calls allowed? | Rationale |
|---|---|---|
| `Given` — test pre-condition | Yes | Set up rooms, send invites, seed data |
| `When` — user action | No (UI only) | The feature under test |
| `Then` — assertion | No (UI only) | Assertions must target the visible UI |

Example for `room/join.feature`: kai creates the room and sends the invite via Matrix API
(`Given`), the assertion is that alex sees the invite banner in Element Web and clicks "Accept"
(`When`/`Then`).

### OIDC Authorization Code + PKCE Requirement

All Gherkin E2E tests use Authorization Code + PKCE. `grant_type=password` (ROPC) is not
supported by Dex v2.41+ and is forbidden in all tests. The consent screen is handled inside
`loginViaOidcBrowser()` (first login only) via a role-based button locator
(`/grant access|allow|approve|confirm/i`).

### Makefile Targets

```bash
make test-e2e-element   # element-web project (bddgen + playwright)
make test-e2e-admin     # admin-ui project (bddgen + playwright)
make test-e2e           # all projects (legacy + BDD)
```

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Cross-Cutting Concerns, §Auth-Token-Flow, §Enforcement; `_bmad-output/planning-artifacts/prd.md`, §Cryptographic Identity Architecture; Story 9-22 (per-device sync token isolation, logout cleanup); Story 9-26 (Browser-First E2E layer, playwright-bdd, token sidecar pattern); Story 9-27 (gRPC error surface rule — GRPC.RPCError vs MatchError; failure audit trail pattern for multi-step operations)_
