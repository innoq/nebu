---
name: nebu-invariants
description: Nebu-specific security invariants — compliance, audit, OIDC, Matrix power levels, crypto, secrets.
---

# Nebu Invariants

These are cross-cutting — always check them, even when the diff does not obviously touch these areas. A missing invariant almost always produces a CRITICAL or HIGH finding because it affects the trust model of the whole system.

## Compliance & Audit

- **Compliance RSP coverage.** Every SQL query against a compliance-scoped table must be executed under an application role that is subject to the Row Security Policy. Direct superuser / migration-role access bypasses RSP. Evidence: the Go handler or Elixir module uses the application DB role, not a privileged role. A new compliance handler without this is a CRITICAL violation.

- **`reason` field is mandatory.** Any compliance data access must attach a non-empty `reason`. If a new code path inserts or reads compliance data without supplying `reason`, it is a violation. HIGH if the missing path is narrow; CRITICAL if it is a primary handler.

- **Audit-log immutability.** Audit tables must have no UPDATE or DELETE grants, and no migration may add them. A migration that relaxes audit-table permissions is a CRITICAL violation — audit immutability is Nebu's rule-of-law.

- **`instance_admin` notification.** Compliance accesses that escalate scope (cross-tenant, bulk export, raw-dump) must trigger a notification to the `instance_admin` role. Check for the notification hook on any new such path.

## Authentication & Authorization

- **OIDC token validation.** Every new handler or middleware that reads an OIDC token must verify `iss`, `aud`, `exp`, signature — and `nbf` if present. Relying on upstream middleware is acceptable **only if the middleware is visibly applied to the route**. A route added without verifiable middleware wiring is a CRITICAL violation.

- **Matrix Power Levels.** Room-scoped state-changing operations (send-state-event, kick, ban, invite, modify power levels) must check the actor's power level before mutation. A handler that skips this is a CRITICAL violation. Read-only operations are not subject to this check.

- **No hardcoded secrets.** API keys, tokens, passwords, session cookies, signing keys must not appear as string literals in source. `.env.example` placeholders are fine. Real production values are CRITICAL.

- **Test integrity.** E2E tests must obtain sessions via the real OIDC flow — cookie forging or DB session seeding is a test-integrity violation. Called out in the report at MEDIUM (not a runtime CRITICAL, but important for Nebu's test-credibility posture, per CLAUDE.md / Epic 3 retro).

## Cryptography

- **TLS 1.3 enforced.** `tls.Config{MinVersion: tls.VersionTLS13}`. `VersionTLS12` is acceptable only with an explicit, documented, in-code reason. Absent / default is a MEDIUM (Go default is currently 1.2).

- **AES-256-GCM correctness.** CBC mode with manual HMAC is a red flag (CWE-327). ECB mode is CRITICAL. Nonce must be random per encryption and must not be reused. `aes.NewCipher(key)` followed by `cipher.NewCBC(...)` for encryption → HIGH / CRITICAL depending on surface.

- **Ed25519 verify-before-accept.** Every incoming signed message must be verified **before** being accepted into application state (Room GenServer, DB, ETS). A code path that parses or deserializes first and verifies afterward is a violation — the parser is attack surface.

- **Key material handling.** Private keys must never be serialized to logs, HTTP responses, or stored unencrypted in ETS. Public keys may be handled freely. X25519 and Ed25519 key pairs must be stored separately — shared key-pair across roles is a violation.

## Secrets & Configuration

- **No secrets in logs.** Check the diff for `log.`, `logger.`, `:logger`, `slog`, `Logger.info/debug/warn` calls that could include a token, password, API key, bearer header, or cookie. Structured loggers with field-level redaction are fine.

- **No secrets in error messages returned to clients.** A handler returning `fmt.Sprintf("db error: %v", err)` where `err` may contain a connection string, credential, or internal path is a violation.

- **Docker / env-var hygiene.** Secrets should be mounted as files (preferred) or injected from `compose secrets`. Inline `environment: - DB_PASSWORD=plaintext` in Compose (outside a `.env` reference) is a violation.

- **Secrets directory.** `.secrets/` directory contents must never appear in the diff. Check the staged-files list — if anything under `.secrets/` is staged, flag as CRITICAL and recommend `git restore --staged`.

## Rendering in the report

Render the invariants as a table with three-state status:

- ✅ — passed (verified or not applicable because the diff does not affect this invariant)
- ⚠️ — partial, or not verifiable from the diff alone
- ❌ — violated (must have a matching finding in the Findings section)

Every ❌ must have a corresponding HIGH or CRITICAL finding. If an invariant shows ⚠️ and the diff could plausibly affect it, state what would be needed to verify.