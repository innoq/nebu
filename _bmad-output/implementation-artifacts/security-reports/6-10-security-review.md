# Security Review — Story 6-10: Server Config API + Metrics API — 2026-05-01

**Agent:** Kassandra
**Diff base:** `git diff --staged`
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

The primary invariant — `oidc_client_secret` never appearing in any GET or PATCH response — holds: the
field is structurally absent from `ServerConfigData`, `adminConfigResponseBody`, and the OpenAPI
`AdminConfigResponse` schema, with a regression test that searches the raw response body. AES-256-GCM
encryption is implemented correctly (random nonce per call, no reuse). The HIGH finding concerns the
default oapi-codegen error path: when `UpsertServerConfigKey` fails, the wrapped error is rendered to
the client verbatim via `http.Error(w, err.Error(), 500)`. That error message contains the SQL key
name (including `"oidc_client_secret"`) and the unredacted underlying database error, which leaks
implementation detail to a parameter that the attacker controls. Two MEDIUM findings cover write-side
audit gaps that diverge from Nebu's "audit on intent, not just success" posture.

## Findings

### [HIGH] Verbose 5xx leaks SQL key name and DB driver error to admin response

- **CWE / OWASP:** CWE-209 / A09:2021 (Logging & Monitoring Failures), A04:2021 (Insecure Design)
- **Datei:** `gateway/internal/api/server.go:115-160` (each `return nil, err`); the rendering happens
  in `gateway/internal/api/api_gen.go:1841` (`http.Error(w, err.Error(), 500)`).
- **Beschreibung:** When `UpsertServerConfigKey` (or `GetServerConfig`, or `GetRoomDefaults`) fails,
  the handler returns `(nil, err)`. The strict-handler default `ResponseErrorHandlerFunc` writes
  `err.Error()` directly into the 500 body. The wrapped error reads
  `UpsertServerConfigKey("oidc_client_secret"): <pq error>` (see `server_config_repo.go:104`). An
  authenticated `instance_admin` who triggers a constraint violation, a connection failure, or a
  forced timeout therefore receives the database driver message — connection target, schema column,
  PostgreSQL error class — back in the response. The same shape applies to
  `encryptAES256GCMForAPI` errors which include the literal text
  `"oidc_client_secret encryption key not configured"`.
- **Impact:** Admin-only audience bounds the blast radius (HIGH, not CRITICAL per the
  Rufschädigungs-Test). Still: post-compromise of an admin token, the 500 body becomes a free
  reconnaissance channel — DB host, schema layout, and the precise key being mutated. It also makes
  the surface tooling-friendly for blind error-oracle attacks, since the message reliably differs
  per failing key.
- **Empfehlung:** Replace `return nil, err` in `PatchAdminConfig` (and `GetAdminConfig`,
  `GetAdminMetrics`) with explicit response objects that return a generic Matrix-envelope 500
  (`M_UNKNOWN`, message `"internal server error"`) and emit the detail to `slog.Error`. Either:
  (a) add a `patchAdminConfig500Resp` type and use it on each error branch, or (b) inject a custom
  `StrictHTTPServerOptions{ResponseErrorHandlerFunc: func(...) { slog.Error(...); ... write generic
  body ... }}` via `NewStrictHandlerWithOptions` so every handler benefits. Option (b) is the
  defense-in-depth choice — it closes the same leak across all current and future handlers.
- **Referenz:** OWASP ASVS V7.4.1 (no system errors in responses), NIST SI-11.

### [MEDIUM] Audit log not written on `PatchAdminConfig` failure paths

- **CWE / OWASP:** A09:2021 / NIST AU-2
- **Datei:** `gateway/internal/api/server.go:115-160`
- **Beschreibung:** The audit-log call lives at line 173 — past every early-return on validation
  error (line 105), upsert error (lines 117, 124, 132, 149, 158), and encryption error (line 146).
  An admin who triggers an encryption failure or any DB upsert error leaves no audit trail of the
  attempted change, even though the *intent* to mutate `oidc_client_secret` is itself security-
  relevant. The same is true for the partial-failure case: if `oidc_issuer` succeeds and
  `oidc_client_id` then fails, the audit row is never written and the partial state is invisible
  to the audit reader.
- **Impact:** Audit reconstructibility gap. Compliance posture (AU-2 "Audit Events" requires
  attempts on configuration changes, not only completions) is weakened. Not exploitable on its
  own — hence MEDIUM.
- **Empfehlung:** Switch to a defer-on-entry pattern: capture `intendedKeys` from the request body
  *before* any DB call, then `defer` an audit emission with `outcome="success"` or
  `outcome="failure"` plus `errorDetail`. Mirror what 5-2 / Story 6.5 already do for deactivate
  failures.

### [MEDIUM] Mass session invalidation has no rate limit, no per-window cap, and audit metadata loses the "why"

- **CWE / OWASP:** CWE-770 (Allocation of Resources Without Limits) / A04:2021
- **Datei:** `gateway/internal/api/server.go:163-170`,
  `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:721-738`
- **Beschreibung:** Any `instance_admin` PATCH that touches `oidc_issuer`, `oidc_client_id`, or
  `oidc_client_secret` triggers a sweep over the entire `:NebuSessions` ETS table and a
  `destroy_session/1` call per user (`Enum.each` is sequential, no batching, no upper bound).
  At MVP scale this is fine; at instance-scale (10k+ active sessions) a malicious or panicking
  admin can repeatedly PATCH the same `oidc_issuer` value and induce a self-DoS — every admin
  action logs the user out and forces them to re-auth, including the actor. The audit metadata
  records `changed_keys` but not whether the value actually changed, so a no-op PATCH (same
  value re-sent) still triggers the sweep and the audit row carries the same `success`. There
  is no idempotency guard, no debouncing, and no notification to other admins when their
  sessions get destroyed.
- **Impact:** Self-inflicted DoS by a confused admin or by a compromised admin token used to
  intentionally lock out concurrent admin holders. Bounded by the requirement of the
  `instance_admin` role, hence MEDIUM not HIGH.
- **Empfehlung:** Three orthogonal mitigations, pick at least one:
  (1) Compare new value against existing `server_config` row before upsert; only mark
      `oidcChanged=true` when the value actually changed.
  (2) Add a per-PATCH rate limit on `/admin/config` (e.g., 1 per 30s per actor) to bound the
      sweep frequency.
  (3) Skip the actor's own session in the sweep so an admin does not log themselves out
      mid-change — their session can be invalidated by a follow-up logout if needed.
  Lowest cost: (1). Highest value: (1) + (3).

### [MEDIUM] `GetAdminMetrics` and `GetAdminConfig` emit no audit event

- **CWE / OWASP:** NIST AU-2 / A09:2021
- **Datei:** `gateway/internal/api/server.go:61-83` (GetAdminConfig), `200-233` (GetAdminMetrics)
- **Beschreibung:** Every other admin GET handler in this file (`ListAdminUsers`, `GetAdminUser`,
  `ListAdminRooms`, `GetAdminRoom`) emits an `audit.LogEvent` on success. `GetAdminConfig` and
  `GetAdminMetrics` do not. The metrics endpoint exposes `active_sessions` and `msg_per_sec_1m`
  — useful reconnaissance signal for an attacker timing a coordinated action; the config endpoint
  reveals the live OIDC `issuer` and `client_id`. Inconsistency with peer handlers means an
  attacker who has obtained an admin token can probe these endpoints without leaving an audit
  trail, while their access to peer handlers would.
- **Impact:** Audit-reconstruction gap. No direct exploit. MEDIUM because the information is
  admin-bounded and not catastrophic on its own; the inconsistency itself is the risk.
- **Empfehlung:** Add `audit.LogEvent(ctx, s.CoreClient, actorID, "admin_config_viewed", "server",
  "config", nil, "success", "")` to `GetAdminConfig` and `audit.LogEvent(..., "admin_metrics_viewed",
  "server", "metrics", ...)` to `GetAdminMetrics`. Same never-raise pattern as peers.

### [LOW] Sequential session invalidation may partially fail silently

- **CWE / OWASP:** CWE-754 (Improper Check for Unusual Conditions)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:725-735`
- **Beschreibung:** `Enum.each` iterates user-by-user; a `{:error, reason}` from
  `destroy_session/1` is logged via `Logger.warning` and swallowed. The handler always returns
  `ok: true`, even when 9/10 sessions failed to destroy. The Go side cannot distinguish "all
  destroyed" from "no-op succeeded" from "9 failed silently".
- **Impact:** Stale sessions can persist past an `oidc_client_secret` rotation; the new secret
  invalidates *future* token-introspection but the cached access tokens in those sessions remain
  valid until expiry. The middleware's per-request OIDC validation should still catch them — so
  practical impact is small. Hence LOW.
- **Empfehlung:** Return a richer response (`{ok: true, destroyed: N, failed: M}`) and have the
  Go side log a warning when `failed > 0`. Optional follow-up — not blocking.

### [INFO] AES-256-GCM implementation is correct; oidc_client_secret never reaches the response

- **Datei:** `gateway/internal/api/server.go:253-272`
- **Beobachtung:** The encryption helper mirrors the proven `gateway/internal/admin/crypto.go`
  pattern: SHA-256 key derivation, `crypto/rand` per-encryption nonce, `gcm.Seal` with nonce
  prepended to ciphertext, hex output. Nonce reuse is impossible — every call generates a fresh
  12-byte random value via `io.ReadFull(rand.Reader, nonce)`. The secret never appears in
  `adminConfigResponseBody` (the struct has no field for it), the OpenAPI schema explicitly
  excludes it (`# NOTE: oidc_client_secret intentionally absent — write-only field`), and a
  raw-string regression test (`TestGetAdminConfig_OIDCClientSecretNeverInResponse`) catches any
  future regression. The empty-secret path correctly refuses to write rather than falling back
  to plaintext (`errEncryptionKeyMissing`, line 241), with a regression test
  (`TestPatchAdminConfig_OIDCClientSecret_NoEncryptionKey_Returns5xx`) that asserts the
  plaintext is never persisted on misconfiguration.
- **Note:** `internalSecret` is reused as the encryption key. This is consistent with the
  bootstrap encryption (`gateway/internal/admin/bootstrap.go:186`) and the Story 6.10 spec
  (AC#2: "same encryption used in bootstrap"). Sharing one secret across PSK + cookie signing +
  OIDC-secret encryption is a long-term key-separation gap, but it is pre-existing (not introduced
  by this story) and the spec explicitly mandates the reuse — flagged as INFO for visibility, not
  a finding against 6-10.

### [INFO] `InvalidateAllAdminSessions` gRPC has no caller authentication beyond PSK

- **Datei:** `proto/core.proto:99-103`,
  `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:721`
- **Beobachtung:** The new RPC inherits whatever authentication the existing gRPC channel
  enforces (PSK via `internalSecret`). No additional caller identity check is performed inside
  the handler. This matches the existing Nebu pattern (other admin RPCs follow the same
  trust-the-channel model — Gateway is the only gRPC client) and is consistent with ADR-008.
  Worth recording because the new RPC is a powerful primitive (mass-logout) and any future
  multi-client gRPC plane would require revisiting this.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no compliance-scoped tables touched) |
| `reason` field on compliance access         | ✅ (n/a — not a compliance handler) |
| Audit-log immutability                      | ✅ (no audit-table grants modified) |
| `instance_admin` notification (if in-scope) | ✅ (n/a) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (existing `jwtMW` chain wraps both new routes) |
| Matrix Power Level checks                   | ✅ (n/a — non-room handlers) |
| No hardcoded secrets                        | ✅ (`internalSecret` from file mount, no literals) |
| TLS 1.3 enforcement                         | ✅ (no TLS surface added) |
| AES-256-GCM correctness                     | ✅ (random nonce per encryption, no reuse path) |
| Ed25519 verify-before-accept                | ✅ (n/a) |
| No secrets in logs / error messages         | ⚠️ (see HIGH finding — DB errors leak via http.Error; encryption error message contains the literal `"oidc_client_secret"` key name) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 3 |
| LOW       | 1 |
| INFO      | 2 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with
warning. The HIGH (verbose 5xx → admin) should be scheduled as a follow-up story or fixed
inline before the next release; the cleanest fix is a custom `ResponseErrorHandlerFunc` wired
via `NewStrictHandlerWithOptions` in `main.go`, which closes the same leak across all admin
handlers in one change. The three MEDIUMs are best addressed together when the audit-on-intent
pattern is generalised.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
