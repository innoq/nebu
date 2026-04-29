---
security_review: required
---

# Story 5.29c: Audit & Crypto Lifecycle — JWT Revocation, Search Filter, Retention Scheduler, Key At-Rest

Status: review

## Story

As a compliance-conscious operator,
I want compliance JWTs to be revocable at validation time, anonymized users to disappear from search, the audit retention purge to actually run on a schedule, and the compliance signing key to be encrypted at rest,
so that the lifecycle promises Epic 5 makes (revocation, retention, key custody, anonymization) are kept by code, not just by docs.

---

## Background / Motivation

Two HIGH findings from Epic-5 SEC Gate 2 (cross-story) plus five MEDIUM/LOW findings from per-story reviews cluster into "complete the lifecycle gaps left open by Stories 5-1, 5-5, 5-6, 5-7, 5-8". Every item is a contract a user, regulator, or operator would reasonably expect to hold but currently does not.

---

## Findings rolled into this story

### FB-E5-04 — Compliance JWT not revocable (HIGH, cross-story 5-5/5-6)
- Source: Epic-5 SEC Gate 2.
- Symptom: `ValidateComplianceToken` (jwt.go:65-106) checks signature/exp/sub but never the DB. A leaked token works for 24h even after `revoked_at` is set. The `token_hash` SHA-256 column added in 5-5 specifically for this lookup is dead weight.
- Fix: extend `ValidateComplianceToken` to compute `sha256(token)` and `SELECT 1 FROM compliance_sessions WHERE token_hash=$1 AND revoked_at IS NULL`. 0 rows → reject as `compliance/jwt: token revoked`. Add admin endpoint `POST /api/v1/admin/compliance/sessions/{id}/revoke`.

### FB-E5-05 — Anonymized users still in user_directory search (HIGH, cross-story 5-7/5-8/5-26)
- Source: Epic-5 SEC Gate 2.
- Symptom: After 5-7 (key delete) + 5-8 (anonymize), `users.user_id` is preserved (Matrix-spec correct) but `SearchUsers` does not filter on `deletion_status` / `anonymized_at`. Real-name PII (e.g. `@alice.bauer:nebu.local`) leaks indefinitely.
- Fix: `SearchUsers` query: `... AND deletion_status IS DISTINCT FROM 'keys_deleted' AND anonymized_at IS NULL`. Verify `GetProfile` behaviour for anonymized users (clarify 404 vs `displayname="Deleted User"` contract).

### FB-E5-06 — Compliance JWT missing `iss` and `aud` claims (MEDIUM)
- Source: Epic-5 SEC Gate 2.
- Fix: add `iss="nebu-gateway"` and `aud="compliance-export"` at issue time, validate on consume.

### FB-E5-07 — `audit_log_purge` has no scheduler (MEDIUM)
- Source: Epic-5 SEC Gate 2.
- Symptom: 5-1 introduced the SECURITY DEFINER purge function and `audit_log_retention_days=2555` config. `audit.RunCleanup` wraps it. **No production code ever invokes `RunCleanup`.** Audit log grows unbounded; retention policy is dead.
- Fix: gateway-side goroutine timer (every 24h) calling `RunCleanup(ctx, db, retentionDays)`. Or Elixir worker analogous to `Compliance.SessionExpiryWorker`.

### FB-51-02 — `audit_log.event_time` trigger-enforced + retention upper-bound (MEDIUM/LOW)
- Source: 5-1 Kassandra.
- Symptom: `event_time` has `DEFAULT NOW()` but caller can override with arbitrary timestamp → backdate / placement into the purge window. `RunCleanup` accepts arbitrary `retention_days` up to INT_MAX → `make_interval` overflow crashes the purge.
- Fix: `BEFORE INSERT` trigger setting `NEW.event_time := NOW()`. Refactor existing test seed paths (use migration role for historic seeds, or SECURITY DEFINER seed function). Cap `retention_days` at 36500 (~100y) in Go and SQL.

### FB-52-02 — `action` field allowlist (MEDIUM)
- Source: 5-2 Kassandra.
- Fix: `@known_actions` allowlist in `Compliance.AuditWriter` covering all Epic-5 vocabulary (`admin_login`, `admin_login_failed`, `admin_logout`, `bootstrap_completed`, `bootstrap_failed`, `room_created`, `room_joined`, `compliance_access_requested`, `compliance_access_approved`, `compliance_access_rejected`, `compliance_session_issued`, `compliance_session_expired`, `compliance_export_downloaded`, `user_keys_deleted`, `user_keys_deletion_attempted`, `user_anonymized`). Unknown action → `{:error, :audit_unknown_action}`.

### FB-55-01 — Compliance signing key plaintext at rest (MEDIUM)
- Source: 5-5 Kassandra.
- Symptom: Ed25519 private key stored as `hex.EncodeToString(priv)` in `server_config.value`. Anyone with DB read access can mint compliance JWTs.
- Fix: wrap `LoadComplianceSigningKey` / `ensureComplianceSigningKey` in `Nebu.Crypto.PII` encryption (X25519+AES-256-GCM from Story 4.7), or equivalent Go-side helper. Migration `000024_encrypt_compliance_signing_key` re-encrypts the existing plaintext value.

---

## Acceptance Criteria

1. `ValidateComplianceToken` rejects tokens whose `token_hash` is absent from `compliance_sessions` or whose row has `revoked_at IS NOT NULL`.
2. New admin endpoint `POST /api/v1/admin/compliance/sessions/{id}/revoke` (instance_admin role) sets `revoked_at = NOW()`.
3. `SearchUsers` excludes anonymized / key-deleted users.
4. Compliance JWTs carry `iss="nebu-gateway"` and `aud="compliance-export"`; validation rejects mismatches.
5. Audit retention purge runs at least every 24h (gateway timer or Elixir worker, single-instance leader-election OK to skip until FB-51-01).
6. `audit_log.event_time` is set by a `BEFORE INSERT` trigger; existing test seed paths refactored.
7. `RunCleanup` rejects `retention_days > 36500` (Go side); `audit_log_purge` raises on the same condition.
8. `Compliance.AuditWriter.log/6+7` rejects unknown action strings.
9. `compliance_signing_key_priv` is stored encrypted at rest. Migration re-encrypts the existing row.

---

## Acceptance Tests

Per finding (RED-phase first):
- `TestValidateComplianceToken_Revoked_Rejected`
- `TestComplianceSession_Revoke_AdminEndpoint`
- `TestSearchUsers_AnonymizedUserNotInResults`
- `TestComplianceJWT_MissingIss_Rejected` + `TestComplianceJWT_MismatchedAud_Rejected`
- `TestAuditPurgeWorker_RunsOnSchedule` (+ smoke: 1h tick triggers `RunCleanup`)
- `TestAuditLog_EventTimeTrigger_ForcesNow`
- `TestRunCleanup_RejectsExtremeRetentionDays`
- `TestAuditWriter_UnknownAction_IsRejected`
- `TestComplianceSigningKey_EncryptedAtRest` (DB row is ciphertext, decrypted form matches in-memory keypair)

---

## Implementation Notes

- **Ordering:** FB-E5-04 + FB-E5-05 first (the two HIGHs). Then FB-E5-07 + FB-51-02 (audit retention path). Then FB-E5-06 + FB-52-02 (validation/allowlist hardening). Then FB-55-01 (key encryption — depends on confirmation that 4.7's PII helper is reusable).
- **Test infrastructure note:** the trigger fix (FB-51-02) interacts with how `audit_room_ops_test.exs` and other Elixir tests seed historical rows. Plan the seed-path refactor before landing the trigger.
- **Dependency on 5-29a:** the trigger test for FB-51-02 only meaningfully verifies enforcement once `nebu_app` no longer bypasses RLS. Land 5-29a first.

---

## Dependencies

- **Depends on:** Story 5-29a (non-superuser app role) for FB-51-02 test correctness.
- **Independent of:** Story 5-29b (different code paths).

---

## Dev Agent Record

**Agent:** Amelia (Dev)
**Completed:** 2026-04-28
**make test-unit-go:** exit 0 (all packages pass)
**make test-unit-elixir:** exit 0 (all tests pass)

### File List

**New files:**
- `gateway/internal/compliance/signing_key.go` — AC9: EnsureComplianceSigningKey / LoadComplianceSigningKey with enc: prefix scheme
- `gateway/internal/compliance/session_revoke.go` — AC1+AC2: SQLSessionLookupDB + RevokeSessionHandler
- `gateway/internal/audit/scheduler.go` — AC5: PurgeScheduler goroutine (injectable tick channel)
- `gateway/migrations/000025_audit_log_event_time_trigger.up.sql` — AC6+AC7: BEFORE INSERT trigger + purge upper-bound
- `gateway/migrations/000025_audit_log_event_time_trigger.down.sql` — rollback

**Modified files:**
- `gateway/internal/compliance/jwt.go` — AC1+AC4: SessionLookupDB interface, ValidateComplianceToken 4-arg signature, iss/aud claims check, revocation DB check
- `gateway/internal/compliance/handler.go` — AC4: set Iss/Aud explicitly in claims (IssueComplianceToken no longer auto-fills)
- `gateway/internal/audit/audit.go` — AC7: RunCleanup rejects retentionDays > 36500
- `gateway/internal/db/user_directory_store.go` — AC3: SearchUsers excludes anonymized/key-deleted users
- `gateway/cmd/gateway/main.go` — AC5+AC9: PurgeScheduler goroutine, AES-256-GCM encrypt/decrypt helpers, EnsureComplianceSigningKey call, RevokeSession route
- `core/apps/compliance/lib/compliance/audit_writer.ex` — AC8: @known_actions allowlist
- `gateway/internal/compliance/jwt_test.go` — updated legacy tests to set Iss/Aud explicitly
- `gateway/internal/compliance/export_test.go` — updated token creation helpers + fake DB for compliance_sessions
- `gateway/internal/compliance/signing_key_test.go` — fixed fake driver Exec to store args[0] for priv key

## Change Log

- 2026-04-23: Story split out from 5-29 master collector. Bundles FB-E5-04, FB-E5-05, FB-E5-06, FB-E5-07, FB-51-02, FB-52-02, FB-55-01.
- 2026-04-28: Implementation complete. All 9 ACs green. Status → review.
