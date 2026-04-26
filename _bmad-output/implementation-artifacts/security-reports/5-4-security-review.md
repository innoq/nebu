# Security Review — Story 5-4 (Four-Eyes Approval API + Admin Dashboard Pending Badge)

- **Reviewer:** Kassandra (BMAD SEC Gate 1)
- **Date:** 2026-04-23
- **Scope:** `git diff --staged` — 9 files, +2320 / −20 LoC
  - `gateway/internal/compliance/handler.go` (+247) — `GetAccessRequests`, `PostApprove`, `PostReject`, `PendingCountHandler`, `postDecision` helper, `formatTimeField`
  - `gateway/internal/compliance/approval_test.go` (+1183, new)
  - `gateway/internal/admin/dashboard.go` (+49 / −15) — `CompliancePendingCounter` interface + Postgres impl
  - `gateway/internal/admin/dashboard_pending_badge_test.go` (+213, new)
  - `gateway/internal/admin/page_data.go` (+10 / −2) — `CompliancePendingCount` field on `PageData`
  - `gateway/internal/admin/templates/layouts/base.html` (+13) — Compliance nav entry + DaisyUI badge
  - `gateway/cmd/gateway/main.go` (+16) — 4 new routes
  - `_bmad-output/implementation-artifacts/5-4-...md`, `sprint-status.yaml`
- **Story status:** `security_review: required`
- **Classification:** **CLEAN** (1 LOW, 4 INFO — all non-blocking; no CRITICAL/HIGH/MEDIUM)

---

## Summary

The four-eyes approval implementation is small, defensive, and correct on every load-bearing axis:

- **Self-approval guard is unbypassable in the API:** pre-flight `SELECT requester_user_id` followed by `requesterUserID == callerSub` comparison runs before the UPDATE. `callerSub` derives from the verified JWT `sub` (non-spoofable). The same guard applies to reject (AC3 parity).
- **Atomic status transition:** `UPDATE ... WHERE id=$1 AND status='pending' RETURNING id` is race-free between two concurrent officers; one wins, the other gets 0 rows → 409. Direct DB writes that pre-flip `status` cannot be exploited via a second approve (UPDATE returns 0 rows).
- **GET list excludes self at DB level** (`requester_user_id != $1`), not in app-layer post-filter.
- **All SQL is parameterised** (`pgx $1..$3`); `requestId` path-param has a 256-byte cap before reaching the DB.
- **Body validation:** `requireJSON` → 415 on wrong Content-Type, `DisallowUnknownFields` rejects extra keys, `bodyLimit64KiB` bounds the body, empty body (`{}` / EOF) is treated as `note=""` per AC.
- **Audit emission** uses the same `auditpkg.LogEvent` path as Story 5-2/5-3 (never-raise, 500ms timeout, target_id=requestId, actor=callerSub). Differentiates approve vs reject via the `action` field (`compliance_access_approved` / `_rejected`).
- **Pending-count endpoint** correctly uses `sessionGuard` (admin session), separate auth schiene from the JWT-protected officer API. No cross-auth bypass.
- **Sidebar badge** is server-side rendered via Go `html/template` auto-escape; `CompliancePendingCount` is `int` so XSS via the count value is structurally impossible. `BootstrapMode` correctly hides it.

The findings below are quality-of-defence improvements, not exploitable today.

## Severity Counter

| CRITICAL | HIGH | MEDIUM | LOW | INFO |
|---|---|---|---|---|
| 0 | 0 | 0 | 1 | 4 |

---

## Findings

### LOW-1 — `note` field has no maximum length (handler-level)

- **File:** `gateway/internal/compliance/handler.go:328` (`var body approveRejectBody`)
- **CWE:** CWE-20 (Improper Input Validation), CWE-79 downstream (Stored XSS in officer/admin UI)
- **Description:** `bodyLimit64KiB` caps the entire request body, so a single `note` can hold up to ~63 KiB. The handler stores the value verbatim in audit metadata (`map[string]any{"note": note}`).
- **Path:** A compromised compliance officer enters 60 KiB of HTML/JS into the note. Story 7-11 (compliance UI) is the consumer; if any future audit-log viewer renders metadata via `{{ . }}` without explicit escape verification, the second officer or an admin viewing the audit trail receives the payload. Go `html/template` default is auto-escape, but a future JSON-encoded fetch + `innerHTML` render in a custom renderer would bypass that.
- **Impact:** Defence-in-depth gap, not exploitable today (no UI consumes the field yet). Mirrors LOW-1 from Story 5-3 review (`justification` — same class).
- **Recommendation:** Cap `note` at e.g. 4 KiB at decode time. Trivial: `if len(body.Note) > 4096 { 400 }`. Defer as follow-up alongside FB-53-02 (`justification` max-length) — same fix in same file.

### INFO-1 — Reject and approve share `approved_at` timestamp column (audit-trail granularity)

- **File:** `gateway/internal/compliance/handler.go:362` (`UPDATE ... SET status = $3, approver_user_id = $2, approved_at = NOW()`)
- **Description:** Both transitions write the decision time into `approved_at`. The schema (migration 000019) intentionally omits `rejected_at` per ADec-5.4-1. Approve vs reject is distinguishable only via `status` field.
- **Impact:** Pragmatically OK — audit trail has the action via `compliance_access_approved` / `compliance_access_rejected` events, plus `status` on the row. A regulator could query "show me all decisions on request X" and reconstruct the timeline. No security defect.
- **Recommendation:** Document in epic-end retro. If a future audit story needs separate timestamps, add `decision_at` migration; do not retro-rename `approved_at`.

### INFO-2 — Pending-count endpoint visible to all admins (not only compliance officers)

- **File:** `gateway/cmd/gateway/main.go` (`GET /admin/api/compliance/pending-count`)
- **Description:** `sessionGuard` only enforces a valid admin session — any logged-in admin (not just `compliance_officer` JWT) sees the count. The dashboard sidebar badge therefore renders the pending count to all admin-portal users.
- **Impact:** Information-disclosure bound is "N pending compliance requests exist" — a single integer, no PII. Per compliance-spec intent ("Admin sees the indicator, not the contents"), this is correct. Listed for audit completeness.
- **Recommendation:** None. Confirms intended design.

### INFO-3 — Sidebar Compliance link points to non-existent route until Story 7-11

- **File:** `gateway/internal/admin/templates/layouts/base.html` (`<a href="/admin/compliance">`)
- **Description:** The nav entry is wired before the compliance UI exists; clicking it currently 404s. Already noted as INFO-5 in code review.
- **Impact:** UX issue, not a security issue.
- **Recommendation:** None for Gate 4. Tracked under Story 7-11.

### INFO-4 — Carry-over from prior reviews (not re-counted)

- **FB-51-01** — `nebu` DB role is `BYPASSRLS+rolsuper` in dev; the RLS policies in 000019 are nominal. Architectural fix tracked in 5-29.
- **FB-52-01** — Core gRPC port 9000 has no authentication; `WriteAuditLog` is forgeable on the loopback wire. The audit emissions in 5-4 (`compliance_access_approved` / `_rejected`) flow over the same channel — same risk class, already documented.
- **FB-53-01** — No rate-limit on `/api/v1/compliance/*`. Applies automatically to the three new endpoints in 5-4 (GET list, POST approve, POST reject). Already tracked.

Per review brief, these are **not** raised as new 5-4 findings.

---

## Nebu Invariants

| Invariant | Status | Note |
|---|---|---|
| Four-eyes (separation of duties) | ✅ | Pre-flight `requester_user_id != callerSub` + atomic `WHERE status='pending'` |
| Compliance RSP coverage | ✅ | `note` captured in audit metadata; reason already stored in 5-3 row |
| Audit-log immutability | ✅ | Append-only via never-raise gRPC; no UPDATE/DELETE on audit_log |
| Audit-emission for both approve and reject | ✅ | Distinct `action` strings; `actor_user_id = approver`, `target_id = request_id` |
| OIDC token validation (`iss`, `aud`, `exp`, `nonce`) | ✅ | Inherited from `jwtMiddleware` |
| Self-approval blocking | ✅ | API-level; DB-level enforcement deferred to 5-29 (BYPASSRLS) |
| Atomic state transition (no TOCTOU) | ✅ | Single `UPDATE ... WHERE status='pending' RETURNING` |
| Body-size limits | ✅ | `bodyLimit64KiB` on POST routes |
| Content-Type enforcement | ✅ | `requireJSON` (415 on wrong CT) |
| Strict JSON shape | ✅ | `DisallowUnknownFields` |
| Path-param sanity bounds | ✅ | 256-byte cap on `requestId` |
| SQL injection | ✅ | All queries parameterised |
| XSS in admin badge | ✅ | `int` field through `html/template` auto-escape |
| No secrets in logs | ✅ | `slog.Error` carries `requestId`, `new_status`, `err` — no token / sub-claim payload |
| Cross-auth bypass (session ↔ JWT) | ✅ | Distinct middleware schiene; no shared cookie/header |
| Role gate (compliance_officer only on JWT API) | ✅ | Inline check via `ContextKeySystemRole` |
| Session gate (admin only on /admin/api) | ✅ | `sessionGuard` |

---

## Decision

- **No CRITICAL/HIGH/MEDIUM findings — Gate 4 PASS.**
- LOW-1 (`note` max length) recommended as follow-up alongside FB-53-02 (same fix class). Track in Story 5-29 or epic-end retro.
- INFO-1..INFO-4 are non-blocking; no re-review required.
- The four-eyes invariant is correctly enforced at the API layer; the residual DB-bypass risk via BYPASSRLS is already in flight under FB-51-01.
