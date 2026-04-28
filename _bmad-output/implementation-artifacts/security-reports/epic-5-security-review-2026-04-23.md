# Epic-5 Security Review (SEC Gate 2)

**Date:** 2026-04-23
**Reviewer:** Kassandra
**Diff-Range:** `267a4bf..HEAD` (Epic-5 base → tip)
**Diff-Size:** 192 files, ~30k insertions / ~1.5k deletions
**Stories covered:** 5-1..5-9 (compliance) + 5-10..5-27 (security hardening) + 5-28 (this gate's trigger meta-story)
**Per-story Gate 1 reviews:** all green or with deferred FBs (see 5-29)
**Carry-over FBs already in 5-29 (NOT re-reported here):** FB-51-01, FB-51-02, FB-52-01, FB-52-02, FB-53-01, FB-53-02, FB-53-03, FB-54-01, FB-55-01, FB-56-01, FB-57-01, FB-58-01, FB-58-02, FB-58-03, FB-E5-03

---

## Executive Summary

Epic 5 lands a substantial compliance and security-hardening surface (audit log, four-eyes approval, 24h compliance JWT, signed export, GDPR key deletion, PII anonymization) plus 18 hardening stories. Per-story SEC Gate 1 reviews caught most issues and deferred 14 items into 5-29 / 6-x.

This SEC Gate 2 pass focused on **cross-story systemic concerns** that per-story reviews structurally cannot see. Two new findings emerge: a HIGH (compliance session JWT is not revocable in practice — `revoked_at` column exists but `ValidateComplianceToken` never reads it) and a HIGH (post-anonymization user IDs continue to surface in user_directory search, leaking PII that the anonymization story did not consider). Two MEDIUM findings concern compliance-JWT claim hygiene (no `iss`/`aud`) and the absence of any scheduler that actually calls `audit_log_purge` despite shipping the SECURITY DEFINER function.

No CRITICAL findings. The audit-immutability invariant, Ed25519 signature path, and four-eyes self-approval guard hold up under the cross-cutting review.

**Severity counts (NEW only, excluding 14 carry-overs):** CRITICAL 0 / HIGH 2 / MEDIUM 2 / LOW 1 / INFO 1.

---

## NEW Findings (over Per-Story Reviews)

### HIGH

#### FB-E5-04 — Compliance session JWT is not revocable; `revoked_at` column is decorative
**File:** `gateway/internal/compliance/jwt.go:65-106` (ValidateComplianceToken)
**Cross-story:** 5-5 (issuance) + 5-6 (validation) + 5-7 (post-deletion impact)
**CWE:** CWE-613 (Insufficient Session Expiration), OWASP A07:2021
**Observation:**
- `compliance_sessions.revoked_at` is set by `Compliance.SessionExpiryWorker` once the natural 24 h TTL passes, and the SessionHandler reads it for the duplicate-active check on POST /session.
- `ValidateComplianceToken` (used by GetExport) verifies only EdDSA signature + `exp` + `iat-skew` + `sub`. It does **not** look up `compliance_sessions` by `token_hash` and **never** checks `revoked_at`.
- Result: even if an operator manually executes `UPDATE compliance_sessions SET revoked_at = NOW() WHERE token_hash = ...` after a leak / officer suspension / legal hold, the leaked token continues to authorise GET /api/v1/compliance/export until the natural exp.
- There is also no admin-facing endpoint to set `revoked_at`. The only mutator is the expiry worker.

**Attack path:** Officer leaks JWT (chat history, dev tools, screen share). Operator detects within minutes. Today: nothing they can do for up to 24 h. With FB-55-01 (key plaintext at rest) compromise, the impact widens to forging arbitrary new tokens, but even without key compromise the in-flight token is unstoppable.

**Recommendation:** Add a DB lookup in `ValidateComplianceToken`: SHA-256 the incoming token, `SELECT revoked_at FROM compliance_sessions WHERE token_hash = $1`, reject if row missing or `revoked_at IS NOT NULL`. Add a POST /session/{id}/revoke endpoint (compliance_officer + 4-eyes approver-equivalent gate). Belt-and-suspenders: emit `compliance_session_revoked` audit.

**Severity rationale:** HIGH — practical, single-officer-action exploit; affects sensitive compliance data; press-headline-worthy if a leaked token exfiltrates room contents.

---

#### FB-E5-05 — User-directory search exposes anonymized / key-deleted users by `user_id` (PII leak after GDPR action)
**File:** `gateway/internal/db/user_directory_store.go:25-52` (SearchUsers)
**Cross-story:** 5-7 (key deletion) + 5-8 (anonymization) + 5-26 (user_directory)
**CWE:** CWE-359 (Exposure of Private Personal Information), GDPR Art. 17 (right to erasure)
**Observation:**
- After Story 5-8 anonymizes a user, `profiles.displayname` becomes `"Deleted User"` and `profiles.avatar_url = NULL`. Good.
- After Story 5-7 marks `users.deletion_status = 'keys_deleted'`. Good.
- `users.user_id` is **never updated** by either story. For real OIDC deployments, `user_id` derives from the OIDC `name` claim via `FormatUserIDFromClaims` — i.e. the user's full name, sanitised: `@alice.bauer:nebu.local` (see `gateway/internal/grpc/metadata.go:48`).
- `SearchUsers` does `SELECT u.user_id, ... FROM users u LEFT JOIN profiles p` with **no filter on `deletion_status` or `anonymized_at`**. An anonymized user `alice.bauer` continues to appear in `/_matrix/client/v3/user_directory/search?term=alice` indefinitely.
- This contradicts the spirit of "anonymization" — DSGVO Art. 17 right-to-erasure expects the user to no longer be discoverable by their personal identifier.

**Attack path:** Any authenticated user searches for the deleted person's name and confirms they once existed on the platform — a privacy disclosure even though the displayname is masked.

**Recommendation:** Add `AND u.anonymized_at IS NULL AND (u.deletion_status IS NULL OR u.deletion_status != 'keys_deleted')` to the `SearchUsers` WHERE clause. Also consider hashing/rotating `user_id` localpart on anonymize (more invasive — track as a separate story).

**Severity rationale:** HIGH — real PII leak surfaced by a documented compliance workflow; the gap is invisible to the per-story review of either 5-7 or 5-8 because each story tested only its own table.

---

### MEDIUM

#### FB-E5-06 — Compliance JWT lacks `iss` and `aud` claims; future key reuse risks token confusion
**File:** `gateway/internal/compliance/jwt.go:30-58` (ComplianceClaims, IssueComplianceToken, ValidateComplianceToken)
**Cross-story:** 5-5 + 5-6
**CWE:** CWE-345 (Insufficient Verification of Data Authenticity), RFC 7519 §4.1.1 / §4.1.3
**Observation:**
- `ComplianceClaims` carries `sub`, `compliance_request_id`, `room_id`, time bounds, `iat`, `exp`. No `iss`, no `aud`, no `jti`.
- Today the EdDSA `compliance_signing_key` signs only compliance session JWTs, so token-confusion is mitigated by-construction. The instant the same key (or any future one with the same algorithm) is reused for a second purpose (e.g. an admin API token, a webhook signature), there is no claim-level guard against accepting one as the other.
- Nebu invariant checklist requires `iss` / `aud` / `exp` validation on tokens. Two of three pass; the third is structurally absent.

**Recommendation:** Add `iss = "nebu-compliance"` (or the configured server name) and `aud = "compliance-export"`. Validate both on the verify path. Also adopt `jti` (random UUID) and store it in `compliance_sessions` to support per-token revocation (combines with FB-E5-04).

**Severity rationale:** MEDIUM — defence-in-depth gap, no exploit today, becomes HIGH the moment the signing key is repurposed. Cheap to fix.

---

#### FB-E5-07 — `audit_log_purge` ships and is callable, but no scheduler invokes `audit.RunCleanup`
**File:** `gateway/internal/audit/audit.go:27-39` (RunCleanup defined) + `gateway/cmd/gateway/main.go` (no caller)
**Cross-story:** 5-1 (retention design)
**CWE:** CWE-1059 (Insufficient Technical Documentation) — operational, not security per se
**Observation:**
- Migration 000018 declares `audit_log_purge(retention_days INT)` SECURITY DEFINER and grants EXECUTE to `nebu`.
- `gateway/internal/audit/audit.go` exports `RunCleanup` that wraps it.
- `gateway/cmd/gateway/main.go` never calls `RunCleanup`. There is no goroutine ticker, no Cron job, no Elixir GenServer that fires it. `audit_log_retention_days = 2555` is seeded but never consulted.
- Net effect: audit_log grows unbounded. Over the 7-year retention window of a busy instance this is a multi-GB table that operators must purge by hand. It is also a latent footgun: any operator who notices the missing scheduler and adds one without first reading the SECURITY DEFINER notes could grant the wrong role EXECUTE.
- Adjacent risk: any code path with `nebu` DB role can call `SELECT audit_log_purge(1)` directly and wipe almost the entire audit history. Combined with FB-51-01 (`nebu` is BYPASSRLS+rolsuper), the attack surface for "audit rewrite via SQL" is broader than the migration's docstring suggests. FB-51-01 already covers the role-split.

**Recommendation:** Either (a) wire a daily ticker in `main.go` that calls `audit.RunCleanup(ctx, complianceDB, retentionDays)` reading `audit_log_retention_days` from `server_config`, or (b) explicitly defer to Epic 6 and remove the dead code with a TODO in the migration. Document either way.

**Severity rationale:** MEDIUM — operational reliability + latent foot-gun, not a confidentiality/integrity/availability breach today.

---

### LOW

#### FB-E5-08 — Dex dev-config still enables `password` grant_type despite project ban on ROPC
**File:** `dev/dex/config.yaml:14-16`
**Cross-story:** ATDD test convention
**Observation:**
- `grantTypes: [authorization_code, password]` permits Resource Owner Password Credentials.
- `CLAUDE.md` and Epic-2 retro both forbid ROPC; per-story tests use Authorization Code + PKCE.
- Risk is dev-only (Dex is not exposed in prod), but a future test that drifts to ROPC will succeed silently and bypass the policy. Tightening the dev config closes that drift vector.

**Recommendation:** Drop `password` from `grantTypes`. If any test still relies on it, switch to PKCE; if any external dev script needs it, add it via override file rather than the checked-in config.

---

### INFO

#### FB-E5-09 — gRPC `DeleteUserKeys` accepts `admin_user_id` from the request body
**File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:606-631` (delete_user_keys handler) + `gateway/internal/compliance/user_key_deletion.go:99-103` (gateway calls with `callerSub`)
**Status:** SUBSUMED by FB-52-01 (no transport auth on Core gRPC). Listed here as a reminder that fixing FB-52-01 must include attaching the trusted-identity metadata to `DeleteUserKeys` (and to `WriteAuditLog`) so a hardened Core can reject calls whose `admin_user_id`/`actor_user_id` do not match the authenticated client identity. The carry-over already mentions WriteAuditLog; DeleteUserKeys deserves an explicit line in the FB-52-01 fix scope.

---

## Verified Clean (cross-cutting checks that passed)

- **Audit-log immutability** — RLS `FORCE`, `audit_log_no_update USING (false)`, `audit_log_no_delete USING (false)`, `SECURITY DEFINER` purge with `SET search_path = pg_catalog, public`, EXECUTE granted only to `nebu`. Belt-and-suspenders pattern is correctly applied. Confirmed in migration 000018.
- **Four-eyes self-approval guard** — `handler.go:postDecision` fetches `requester_user_id` and rejects when `== callerSub` before mutating status. Two distinct OIDC subs are required (Vier-Augen via two OIDC identities is a known consideration but is consistent with the Nebu trust model: an operator who provisions two OIDC accounts to a single human has already broken the four-eyes contract socially; technical mitigation belongs in OIDC IdP policy, not gateway).
- **Migration ordering** — 000018 → 000023 are linearly applied. Each is column-add or table-create only; partial-failure leaves a consistent state because `golang-migrate` runs each `.up.sql` in its own transaction. No cross-migration FK that could orphan rows.
- **RLS coverage** — `audit_log`, `compliance_requests`, `compliance_sessions` all have `FORCE ROW LEVEL SECURITY` with explicit `no_delete` policy. Consistent across the three new tables.
- **Ed25519 verify-before-accept** — `ValidateComplianceToken` parses with `[]jose.SignatureAlgorithm{jose.EdDSA}`, then verifies, then unmarshals — order is correct (algorithm pinning at parse, signature before payload trust).
- **Path traversal in anonymize** — `parseMxcURI` + `isSafePathSegment` reject `.`, `..`, `/`, `\`, `\x00`. Defence-in-depth against attacker-controlled `avatar_url`.
- **JWT denylist after verify** — `JWTMiddleware` denylist check sits behind cryptographic verification (Story 5-23 fix verified at code level).
- **OIDC algorithm pinning** — `ParseSupportedAlgs()` runs once at middleware construction; `idToken.Claims` after `Verify` only.
- **Audit emission never-raises** — both Go (`audit.LogEvent`) and Elixir (`Compliance.AuditWriter.log` returns `{:error, ...}` instead of raising) hold the contract.
- **GDPR deletion atomicity** — `Compliance.UserDeletion.delete_user_keys` wraps Steps 2–5 in `Repo.transaction/1`; failure-invariant `user_keys_deletion_attempted` audit is emitted in a SEPARATE transaction so a rollback of the deletion TX cannot suppress the audit row.

---

## Carry-over reminder (already in 5-29; do not double-track)

| ID | Sev | Story | Theme |
|---|---|---|---|
| FB-51-01 | HIGH | 5-1 | `nebu` DB role has BYPASSRLS+rolsuper — RLS nominal in dev |
| FB-51-02 | MED/LOW | 5-1 | audit_log event_time trigger + retention upper-bound |
| FB-52-01 | HIGH | 5-2 | Core gRPC :9000 has no transport auth; WriteAuditLog forgeable |
| FB-52-02 | MED | 5-2 | No `action` allowlist on WriteAuditLog |
| FB-53-01 | MED | 5-3 | No rate-limit on `/api/v1/compliance/*` |
| FB-53-02 | LOW | 5-4 | XSS-escape verification for justification in admin UI |
| FB-53-03 | LOW | 5-3 | `time_range` max 365 days |
| FB-54-01 | LOW | 5-4 | `note` max-length cap |
| FB-55-01 | MED | 5-5 | `compliance_signing_key` plaintext at rest |
| FB-56-01 | MED | 5-6 | Export status TOCTOU + streaming |
| FB-57-01 | MED | 5-7 | TOCTOU on `users.deletion_status` |
| FB-58-01 | MED | 5-8 | Anonymize multi-step non-atomic |
| FB-58-02 | MED | 5-8 | Avatar URL upstream validation |
| FB-58-03 | LOW | 5-8 | Self-anonymize |
| FB-E5-03 | MED | epic | 23 pre-existing event_dispatcher test failures |

---

## Pipeline Decision

**Classification:** HIGH

**Rationale:** Two new HIGH findings (FB-E5-04 token revocation, FB-E5-05 user_directory PII leak after anonymization). Both are cross-cutting, neither is a regression in shipped code paths, both have concrete attack paths.

**Recommended action (per `blocking_severity: CRITICAL` config):** PROCEED with retro **after** FB-E5-04 and FB-E5-05 are appended to Story 5-29 as new finding blocks. They join the 14 existing carry-overs and will be addressed during the 5-29 dev pass (or split into 5-30 / 5-31 if scope grows). FB-E5-06 / FB-E5-07 / FB-E5-08 / FB-E5-09 also append to 5-29 at MEDIUM/LOW/INFO. No commit blocked at the epic boundary.

**Audit-trail status:** This report is the SEC Gate 2 artifact for Epic 5. It is immutable from this commit forward.
