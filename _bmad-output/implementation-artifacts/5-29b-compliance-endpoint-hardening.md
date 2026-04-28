---
security_review: required
---

# Story 5.29b: Compliance Endpoint Hardening — Rate Limits, Atomicity, Length Caps

Status: ready-for-dev

## Story

As an instance operator,
I want every compliance and admin endpoint introduced in Stories 5-3 through 5-8 to enforce sane rate limits, atomic state transitions, and bounded input sizes,
so that an authenticated officer or admin cannot accidentally or maliciously DOS the database, leave half-completed state, or store oversize payloads.

---

## Background / Motivation

Eight per-story SEC Gate 1 findings cluster into one theme: "tighten the compliance/admin API surface introduced in Epic 5". None of them is individually high-severity, but together they are the difference between a feature that ships and a feature that's defensible against operational misuse.

---

## Findings rolled into this story

### FB-53-01 — Rate-limit `/api/v1/compliance/*` (MEDIUM)
- Source: 5-3 Kassandra. None of the new compliance routes are wrapped in `strictRL` / `mediumRL`.
- Fix: wrap every `POST /api/v1/compliance/*` and the admin anonymize/key-delete routes in `strictRL` (10/min/IP). Tests: 11th request in window → 429.

### FB-53-03 — Restrict `time_range` (LOW → expand here to MEDIUM since 5-5/5-6 now use it)
- Source: 5-3 Kassandra. `time_range_start`/`end` accept any RFC3339 → 1000-year window blocks the export-events query.
- Fix: in 5-3 handler, reject `(end-start) > 365 days` → 400 `M_BAD_JSON`; reject `start < NOW() - 7 years`.

### FB-54-01 — `note` body field max-length (LOW)
- Source: 5-4 Kassandra (paired with FB-53-02).
- Fix: 4096-char cap in `postDecision`. Tests for approve+reject "note too long → 400".

### FB-56-01 — Compliance export: status re-check + LIMIT (MEDIUM)
- Source: 5-6 Kassandra + Code-Review.
- Fix: pre-flight SELECT also reads `status`; reject with 403 if no longer `'approved'`. Replace hardcoded `LIMIT 10000` with streaming response (`Transfer-Encoding: chunked`, one event per line) OR add `truncated: true` + `events_truncated_at: 10000` to export-doc + audit metadata when count would exceed the limit.

### FB-57-01 — Atomic guard for `users.deletion_status` (MEDIUM)
- Source: 5-7 Kassandra.
- Fix: replace SELECT+UPDATE with single conditional `UPDATE users SET deletion_status='deletion_in_progress' WHERE id=$1 AND deletion_status IN (NULL, 'active') RETURNING id`. 0 rows → `:conflict`. Test: `Task.async` with two concurrent calls — exactly one `:ok`, one `:conflict`, one audit entry.

### FB-58-01 — Anonymize multi-step DB ops non-atomic (MEDIUM)
- Source: 5-8 Code-Review.
- Fix: wrap steps 4–6 (UPDATE profiles → users → media_files) in `db.BeginTx`/`tx.Commit`. Pre-flight SELECT remains outside the TX.

### FB-58-02 — Avatar URL upstream validation lax (MEDIUM)
- Source: 5-8 Code-Review (root cause of the path-traversal MAJOR fixed in 5-8).
- Fix: extend `isSafePathSegment` (from `gateway/internal/compliance/user_anonymization.go`) into the matrix profile package. `PUT /profile/{userId}/avatar_url` validates `mxc://<safe-server>/<safe-mediaId>` format; reject with 400 `M_BAD_JSON: "invalid avatar URL"`. Migration to scrub already-stored unsafe URIs from `profiles.avatar_url` (set NULL where format invalid + audit `avatar_sanitized`).

### FB-58-03 — Self-anonymize permitted (LOW)
- Source: 5-8 Code-Review.
- Fix: `if userID == callerSub` → 403 `M_FORBIDDEN: "self-anonymize requires four-eyes approval"`. Optional: require a second-admin `confirm` query param signed analogously to 5-4 four-eyes.

---

## Acceptance Criteria

1. Every `/api/v1/compliance/*` and `/api/v1/admin/users/*/{keys,anonymize}` route is wrapped in `strictRL`.
2. 5-3 handler enforces `time_range_start ≤ time_range_end ≤ start + 365d` AND `start ≥ NOW() - 7y`.
3. 5-4 `postDecision` rejects `note > 4096 chars`.
4. 5-6 export handler re-reads `compliance_requests.status` in pre-flight; 403 if not `'approved'`. LIMIT replaced with streaming OR truncation flag.
5. 5-7 `Compliance.UserDeletion.delete_user_keys` uses single conditional UPDATE for the deletion_in_progress transition.
6. 5-8 `AnonymizationHandler` wraps profiles/users/media_files updates in a single TX.
7. Avatar URL upstream validation: PUT /profile/avatar_url enforces safe mxc format. Existing rows scrubbed via migration.
8. Self-anonymize blocked with 403.

---

## Acceptance Tests

Per finding:
- `TestComplianceRoutes_RateLimited_429`
- `TestPostAccessRequest_TimeRangeTooLarge_Returns400` + `_TimeRangeTooOld_Returns400`
- `TestApproveRequest_NoteTooLong_Returns400` / reject variant
- `TestGetExport_StatusFlippedAfterIssue_Returns403`
- `TestDeleteUserKeys_ConcurrentCalls_OneConflict` (Elixir, `Task.async`)
- `TestAnonymizeUser_TxRollbackOnUsersUpdateFailure` (no half-state)
- `TestPutAvatarURL_RejectsUnsafeMxc_Returns400` + migration test for the scrub
- `TestAnonymizeUser_SelfAnonymize_Returns403`

---

## Implementation Notes

- All eight items are independent and can be PR'd separately if helpful. Bundling here is a unit-of-thought, not a unit-of-commit.
- Ordering: rate-limit + length-caps first (smallest risk), then atomicity fixes (5-7, 5-8), then upstream avatar URL hardening (touches matrix package — needs care).

---

## Dependencies

- Independent of 5-29a (these fixes work today even with BYPASSRLS in place).

---

## Change Log

- 2026-04-23: Story split out from 5-29 master collector. Bundles FB-53-01/03, FB-54-01, FB-56-01, FB-57-01, FB-58-01/02/03.
