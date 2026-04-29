# Epic-5 SEC Gate 2 — CLOSURE Review

**Date:** 2026-04-29
**Reviewer:** Kassandra
**Diff-Range:** `git diff 5094556..HEAD` (post-5-28 → all of 5-29a..e split)
**Diff-Size:** ~80 files, ~9.7 k insertions / ~0.6 k deletions
**Stories covered:** 5-29a (trust-model), 5-29b (compliance hardening), 5-29c (audit/crypto lifecycle), 5-29d (test infra & dev hardening), 5-29e (manual-testing bug fixes)
**Predecessor report:** `epic-5-security-review-2026-04-23.md`

---

## Executive Summary

Epic-5 SEC Gate 2 (2026-04-23) opened with 2 HIGH + 2 MEDIUM + 1 LOW + 1 INFO new findings plus 14 carry-overs deferred from per-story Gate 1 reviews and 1 partial (FB-E5-03). All four 2026-04-23 NEW Epic-5 findings are now structurally closed in the 5-29a..e split. All 14 carry-overs are closed or explicitly deferred to a tracked downstream story (FB-53-02 → 7-11 by user decision; FB-E5-03 partial → 5-29d.1, see below). Two latent issues surfaced during this closure pass — both already documented in the per-story 5-29d review (MEDIUM-1: enc:v1 marker not AAD-bound; LOW-1: NEBU_ENV value-space) and rolled forward as Epic-6 backlog candidates rather than re-opened here.

No CRITICAL findings. No NEW HIGH findings beyond what 5-29a..d already self-reviewed. The audit-immutability, gRPC PSK, role-split, and KEK-validation invariants hold up under cross-cutting examination.

**Severity counts:** CRITICAL 0 / HIGH 0 / MEDIUM 1 (carry-forward) / LOW 1 (carry-forward) / INFO 2.
**Closed in 5-29a..e:** HIGH 2 (FB-E5-04, FB-E5-05) / MEDIUM 2 (FB-E5-06, FB-E5-07) / LOW 1 (FB-E5-08) / INFO 1 (FB-E5-09) / + 14 carry-overs.

---

## Closure Verification — Original Epic-5 Findings (2026-04-23)

### HIGH-1 — FB-E5-04: Compliance JWT not revocable — **CLOSED**

- `gateway/internal/compliance/jwt.go:46-49` introduces `SessionLookupDB` interface.
- `ValidateComplianceToken` (jwt.go:142-151) computes `sha256.Sum256([]byte(tokenStr))`, calls `db.IsTokenActive(ctx, tokenHash[:])`, rejects on `!active` with `"token revoked"`.
- `gateway/internal/compliance/session_revoke.go:37-50` implements `SQLSessionLookupDB.IsTokenActive` querying `SELECT 1 FROM compliance_sessions WHERE token_hash=$1 AND revoked_at IS NULL`.
- New admin endpoint `POST /api/v1/admin/compliance/sessions/{sessionId}/revoke` is wired in `main.go:855-856` behind `strictRL(bodyLimit64KiB(csrf(sessionGuard(...))))` — CSRF wrap was added per the 5-29c-internal HIGH (review at `security-reports/5-29c-security-review.md:14-22`).
- `instance_admin` role gate enforced inside the handler (`session_revoke.go:76-80`).
- `compliance_session_revoked` audit action added to allowlist (`core/apps/compliance/lib/compliance/audit_writer.ex:38`).
- **Verdict:** Closed; rationale and attack path from the original finding fully addressed.

### HIGH-2 — FB-E5-05: User-directory leaks anonymized users — **CLOSED**

- `gateway/internal/db/user_directory_store.go:36-42` now reads:
  ```sql
  WHERE u.user_id ILIKE $1 ESCAPE '\'
    AND u.deletion_status IS DISTINCT FROM 'keys_deleted'
    AND u.anonymized_at IS NULL
  ```
- Combined with Story 5-26 `EscapeLIKE` and Story 5-8 anonymization, anonymized + key-deleted users no longer surface in `/_matrix/client/v3/user_directory/search`.
- **Verdict:** Closed.

### MEDIUM-1 — FB-E5-06: Compliance JWT lacks `iss`/`aud` — **CLOSED**

- `gateway/internal/compliance/jwt.go:37-40` defines `JWTIssuer = "nebu-gateway"` and `JWTAudience = "compliance-export"`.
- `ComplianceClaims.Iss` / `Aud` added (jwt.go:62-63).
- Validation enforced at jwt.go:135-140 with explicit mismatch errors.
- `jti` was not adopted; revocation is keyed on `token_hash` instead (functionally equivalent for the original concern). Acceptable.
- **Verdict:** Closed.

### MEDIUM-2 — FB-E5-07: No scheduler invokes `audit_log_purge` — **CLOSED**

- `gateway/internal/audit/scheduler.go` introduces `PurgeScheduler` with two constructors (`NewPurgeScheduler` external-ticker, `NewPurgeSchedulerWithJitter` for multi-instance spread — Story 5-29d AC7).
- Wired in `gateway/cmd/gateway/main.go:253-271` — daily ticker, retention read from `server_config.audit_log_retention_days`.
- Per-tick observability (`slog.Info "audit: purge tick completed" deleted_rows=...`) and never-raise behaviour on errors.
- **Note:** `main.go` still uses the **external-ticker** constructor, not the jitter variant. This was flagged in the 5-29c security review (MEDIUM-3, multi-instance lock contention) and addressed via the jitter constructor in 5-29d, but the production wiring path was not switched. **Carry-forward as INFO-1 below.**

### LOW — FB-E5-08: Dex `password` grant — **CLOSED**

- `dev/dex/config.yaml:14` no longer lists `password` in `grantTypes`.
- Negative integration test `gateway/test/integration/dex_password_grant_test.go` asserts ROPC requests fail.

### INFO — FB-E5-09: gRPC `DeleteUserKeys` accepts `admin_user_id` from body — **CLOSED (subsumed)**

- Trust shifted to authenticated PSK channel via Story 5-29a interceptor (Elixir `Nebu.Grpc.AuthInterceptor`, attached at `event_dispatcher/endpoint.ex:7`). Caller identity is now authenticated; `admin_user_id` in body is contextual metadata, not a bypass surface.

---

## Closure Verification — 14 Carry-over FBs

| ID | Sev | Story | Closure path | Verdict |
|---|---|---|---|---|
| FB-51-01 | HIGH | 5-1 | 5-29a Block A: `nebu_migrate` + `nebu_app` roles in `dev/postgres/init/01-roles.sql`; migration 000024 transfers ownership. `nebu_app` is `NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`. | Closed |
| FB-51-02 | MED/LOW | 5-1 | 5-29c migration 000025: `BEFORE INSERT` trigger forces `event_time := NOW()`; `audit_log_purge` rejects `retention_days > 36500`. | Closed |
| FB-52-01 | HIGH | 5-2 | 5-29a Block B: Elixir `Nebu.Grpc.AuthInterceptor` registered at `endpoint.ex`; Go `internal/grpc/client.go` injects `x-nebu-node-token` PSK on every Unary/Stream call. | Closed |
| FB-52-02 | MED | 5-2 | 5-29c: `Compliance.AuditWriter` `@known_actions` allowlist (`audit_writer.ex:25-43`); unknown actions return `{:error, :audit_unknown_action}`. | Closed |
| FB-53-01 | MED | 5-3 | 5-29b: all 9 compliance/admin routes wrapped in `strictRL` (5/min/IP). Verified `main.go:751-877`. | Closed |
| FB-53-02 | LOW | 5-3 | Deferred to **Story 7-11** (Compliance Admin UI surface not yet implemented; user decision documented in `5-29d-test-infra-and-dev-hardening.md:190-192`). | Deferred (tracked) |
| FB-53-03 | LOW | 5-3 | 5-29b: `time_range` capped at 365 days. | Closed |
| FB-54-01 | LOW | 5-4 | 5-29b: `note` capped at 4096 chars. | Closed |
| FB-55-01 | MED | 5-5 | 5-29c: AES-256-GCM encrypt-at-rest via `EnsureComplianceSigningKey`/`LoadComplianceSigningKey`; `enc:v1:` envelope; `MigrateLegacyPlaintextKey` upgrades pre-5.29c rows. | Closed |
| FB-56-01 | MED | 5-6 | 5-29b: export status TOCTOU re-check in pre-flight (`compliance/handler.go:725-742`). | Closed |
| FB-57-01 | MED | 5-7 | 5-29b: deletion-status race-free via `UPDATE … RETURNING` (`compliance/user_deletion.ex:138-156`). | Closed |
| FB-58-01 | MED | 5-8 | 5-29b: `AnonymizeUser` profiles/users/media steps in single `BeginTx`. | Closed |
| FB-58-02 | MED | 5-8 | 5-29b: `isSafeMxcSegment` literal-byte check + migration 000026 scrubs unsafe stored values. | Closed |
| FB-58-03 | LOW | 5-8 | 5-29b: self-anonymize blocked in handler. | Closed |
| FB-E5-03 | MED | epic | 5-29d: 15 of 23 event_dispatcher test failures fixed (98 tests, 8 failures remain). 8 split out to **Story 5-29d.1** per 5-29d.md:184. | Partially closed (tracked) |

---

## Cross-Story Systemic Concerns Discovered (5-29a..e)

### INFO-1 — Production `main.go` uses fixed-interval scheduler, not jitter variant

- **File:** `gateway/cmd/gateway/main.go:267-269`
- **Context:** 5-29c review flagged multi-instance contention as MEDIUM and 5-29d shipped `NewPurgeSchedulerWithJitter` to address it. The production wiring still calls `NewPurgeScheduler` (external ticker, no jitter).
- **Impact:** None today (single-instance dev). Becomes a multi-instance lock-contention concern only on horizontal scale. Tracked already as Epic-6 backlog (NFR/scaling).
- **Recommendation:** When horizontal scaling lands, switch the wiring to `audit.NewPurgeSchedulerWithJitter(retentionDays, cleanupFn, 24*time.Hour, 0.10)`. Both constructors exist; this is a one-line change.

### INFO-2 — Elixir `secure_compare` uses HMAC-SHA256 of random key, not constant-time comparator

- **File:** `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex:106-114`
- **Observation:** `:crypto.mac(:hmac, :sha256, key, a) == :crypto.mac(:hmac, :sha256, key, b)` — the unknown random key randomises the MAC output, but the final `==` on the resulting 32-byte binaries is BEAM term-level equality, which is **not** constant-time in the strict sense. The standard Plug/Phoenix idiom for this is `Plug.Crypto.secure_compare/2` (which copies the bytes and OR-accumulates differences in a constant-time loop).
- **Why it is INFO, not HIGH:** the input `a` is the attacker-controlled token; both `a` and `b` are first run through HMAC-SHA256 with a 32-byte cryptographically random key generated per call (`:crypto.strong_rand_bytes(32)`), which destroys any byte-position correlation an attacker could exploit via timing. The attacker observes only timing of the final `==` on a 32-byte HMAC output that varies unpredictably per call — there is no signal to amplify.
- **Recommendation:** Switch to `Plug.Crypto.secure_compare/2` (already a transitive dep of grpc-elixir) for clarity and to align with Erlang/Elixir idiom. Optional; no exploit path.

### Carried forward from 5-29d review (no re-finding here)

- **MEDIUM (5-29d MEDIUM-1):** `enc:v1:` envelope is a marker, not AAD-bound. Becomes HIGH the moment a `v2` ships. Recommendation: pass version + KEK-id as AAD when adding `v2`. **Track as precondition of next KEK-rotation story.**
- **LOW (5-29d LOW-1):** `NEBU_ENV` accepts only the literal `"production"`; typos (`prod`, `Production`) silently disable the KEK hard-fail. Recommendation: case-insensitive synonym set or unknown-env rejection. **Track as Epic-6 ops-hardening.**

---

## Verified Clean — Cross-Cutting Checks

- **DB role split (5-29a):** Verified — `nebu_app` `NOSUPERUSER NOBYPASSRLS`, owner ALTER for tables/sequences/functions covers `audit_log_purge`. Audit-purge regression caught in 5-29a self-review and fixed via `BYPASSRLS` on `nebu_migrate` (necessary for `DELETE` under `FORCE RLS USING (false)`).
- **gRPC PSK (5-29a):** `intercept(Nebu.Grpc.AuthInterceptor)` is at the `Endpoint` level (`event_dispatcher/endpoint.ex:7`), so **all** RPCs registered under `Nebu.EventDispatcher.Server` (SendEvent, CreateRoom, JoinRoom, LeaveRoom, InviteUser, DeleteUserKeys, WriteAuditLog, EventBus, etc.) are gated. Go client interceptors cover both Unary and Stream paths. Fail-secure default when secret file unreadable.
- **Compliance rate limits (5-29b):** All 9 routes (`POST /access-requests`, `GET /access-requests`, `POST .../approve`, `POST .../reject`, `POST .../session`, `GET /export`, `POST /admin/.../revoke`, `DELETE /admin/users/{id}/keys`, `POST /admin/users/{id}/anonymize`) wrapped in `strictRL`.
- **Audit retention scheduler (5-29c):** Wired in `main.go`. Tick loop has `select { ctx.Done() | ticker.C }` so context cancellation cleanly exits.
- **enc:v1 envelope (5-29d):** `MigrateLegacyPlaintextKey` is idempotent, refuses non-canonical lengths (no false rewrites), uses `nebu_migrate` BYPASSRLS to UPDATE `server_config`. `LoadComplianceSigningKey` rejects plaintext (`ErrPlaintextKey`) and unknown versions (`ErrUnknownKeyVersion`) — fail-closed at boot.
- **KEK validation (5-29d):** Production hard-fail unless `NEBU_ALLOW_INSECURE_KEK=true` opt-in. Validation runs before AES-GCM construction; misconfigured prod boots fail at line 782 (`os.Exit(1)`).
- **Bug 4 sync device fields (5-29e):** Pure response-shape fix — three new JSON fields with empty defaults (`map[string]int{}`, `[]string{}`, `syncDeviceLists{[],[] }`). No new auth surface, no input parsed from these fields.
- **Migration ordering:** 000024 → 000025 → 000026 linearly applied. 000024 transfers ownership BEFORE 000025 alters `audit_log_purge` (which is now owned by `nebu_migrate`). 000026 is a one-shot data scrub.

---

## Severity Counter — NEW vs CLOSED

| Bucket | CRITICAL | HIGH | MEDIUM | LOW | INFO |
|---|---:|---:|---:|---:|---:|
| **NEW (this closure pass)** | 0 | 0 | 0 | 0 | 2 |
| **CLOSED in 5-29a..e** | 0 | 4 | 7 | 4 | 1 |
| **DEFERRED to follow-up story** | 0 | 0 | 1 (FB-E5-03 → 5-29d.1) | 1 (FB-53-02 → 7-11) | 0 |
| **CARRY-FORWARD to Epic-6 backlog** | 0 | 0 | 1 (5-29d MEDIUM-1 enc-v1 AAD) | 1 (5-29d LOW-1 NEBU_ENV) | 0 |

NEW INFO-1 (jitter wiring) and INFO-2 (Plug.Crypto.secure_compare) are documentation-quality nits. Neither blocks the epic.

---

## Pipeline Decision

**Classification:** CLEAN

**Rationale:** All 4 NEW Epic-5 findings from 2026-04-23 are structurally closed. All 14 carry-over FBs are closed or tracked downstream. No new HIGH or CRITICAL emerges from cross-story analysis. Two future-state issues (enc:v1 AAD, NEBU_ENV synonyms) are already documented in the per-story 5-29d review and parked as Epic-6 backlog. One partial (FB-E5-03) has a recorded follow-up (5-29d.1) with a residual count of 8 test failures.

**Epic-5 disposition:** Ready for retrospective (`/bmad-retrospective`).

**Recommended Epic-6 backlog (do not block Epic-5):**

1. Switch production audit-purge wiring to `NewPurgeSchedulerWithJitter` once horizontal scaling is on the roadmap.
2. Add AAD-bound version envelope before introducing `enc:v2:` (precondition of next KEK-rotation work).
3. Replace `NEBU_ENV` exact-match with a synonym set or unknown-value rejection (ops-hardening).
4. Replace Elixir `secure_compare` with `Plug.Crypto.secure_compare/2`.
5. Land Story 5-29d.1 (8 remaining event_dispatcher test failures).
6. Land Story 7-11 (Compliance Admin UI) — XSS Playwright test from FB-53-02 ships there.

**Audit-trail status:** This report is the SEC Gate 2 closure artifact for Epic 5. Together with `epic-5-security-review-2026-04-23.md` and the five per-story 5-29x reports it forms the complete Epic-5 security audit record. Immutable from this commit forward.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
