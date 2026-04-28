---
security_review: not-needed
---

# Story 5.29: Security Follow-up Collector — INDEX (split into 5-29a..e)

Status: split (no implementation work — see child stories)

## Purpose

Master index for the 23 follow-up items deferred from Stories 5-1..5-8 + 5-27 (per-story SEC Gate 1) and the Epic-5 SEC Gate 2 epic-wide review (2026-04-23). Originally one collector story; split on 2026-04-23 because 23 items in one story would exceed any reasonable size envelope and bury HIGH severity issues alongside LOW items.

The five child stories are independent units of work but share the Epic-5 close-out context. Land them in the dependency order shown below.

---

## Child stories

| Story | Theme | Severity profile | Items |
|---|---|---|---|
| **[5-29a](5-29a-trust-model-tightening.md)** | Trust model: non-superuser DB role + gRPC transport auth | 2 HIGH | FB-51-01, FB-52-01 |
| **[5-29b](5-29b-compliance-endpoint-hardening.md)** | Rate limits, atomicity, length caps on Stories 5-3..5-8 endpoints | 8 MEDIUM/LOW | FB-53-01/03, FB-54-01, FB-56-01, FB-57-01, FB-58-01/02/03 |
| **[5-29c](5-29c-audit-crypto-lifecycle.md)** | JWT revocation, search filter, retention scheduler, key at-rest, action allowlist | 2 HIGH + 5 MEDIUM | FB-E5-04/05, FB-E5-06/07, FB-51-02, FB-52-02, FB-55-01 |
| **[5-29d](5-29d-test-infra-and-dev-hardening.md)** | Pre-existing test failures, Dex hardening, XSS verification | 1 MEDIUM + 2 LOW + 1 INFO | FB-E5-03, FB-E5-08, FB-53-02, FB-E5-09 |
| **[5-29e](5-29e-manual-testing-bugs.md)** | Production bugs from manual testing (room upgrade, DM, admin UI) | Functional, not security | Captured from `tmp/test-findings.md` |

---

## Recommended landing order

1. **5-29a** first — it changes the trust model and the migration grant matrix; many other tests can only be meaningful afterwards.
2. **5-29b** in parallel with 5-29a — endpoint hardening is independent of the trust model, fixes are small.
3. **5-29c** after 5-29a — FB-51-02 (event_time trigger) tests rely on the role split for correctness.
4. **5-29d** any time — independent.
5. **5-29e** any time — functional bugs, separate audience (end-users + admins, not security-team).

---

## Acceptance Criteria (this index story itself)

1. Each of 5-29a..e is created with `ready-for-dev` status.
2. Sprint-status.yaml lists all five with the correct status.
3. The `deferred-work.md` Story-5-1 entry now points at 5-29a as the canonical home for FB-51-01 (was the original anchor).
4. This story (5-29) is marked `done` in sprint-status.yaml because it has no further work — all content moved.

---

## Acceptance Tests

None — pure documentation reorganisation.

---

## Change Log

- 2026-04-23: Story created as collector for FB-51-01 (Story 5-1 follow-up). Reframed same day as security-followup-collector. Subsequently accumulated 23 items from per-story SEC Gate 1 + Epic-5 SEC Gate 2 + manual testing bugs.
- 2026-04-23: Split into 5-29a..e at user request (3 manual-testing bugs from `tmp/test-findings.md` would have made the single collector unworkable). This file now serves as the index.
