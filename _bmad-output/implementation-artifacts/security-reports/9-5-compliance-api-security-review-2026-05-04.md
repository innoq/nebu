# Story 9.5 Security Review — Admin UI Compliance API Integration

**Reviewer:** Kassandra (BMAD security agent, executed via /bmad-code-review SEC Gate 1)
**Date:** 2026-05-04
**Scope:** Staged diff for Story 9.5
- `gateway/internal/admin/compliance_handler.go`
- `gateway/cmd/gateway/main.go`
- `gateway/internal/admin/compliance_todo_test.go`
- `e2e/tests/features/admin/compliance-api-integration.spec.ts`
- `_bmad-output/implementation-artifacts/9-5-admin-ui-compliance-api-integration.md`

**Frameworks consulted:** OWASP Top 10 (2021), ASVS L2, CWE Top 25, STRIDE, NIST SP 800-53.
**Nebu invariants checked:** four-eyes compliance principle (Story 5.4 / FB-54-01), audit immutability, OIDC session attribution, CSRF on state-changing admin POST routes, body-size limits, rate limits, SQL injection, secrets handling.

---

## Classification: HIGH (resolved inline) → CLEAN (post-fix)

| Severity | Found | Fixed inline | Open |
|----------|-------|--------------|------|
| CRITICAL | 0     | 0            | 0    |
| HIGH     | 2     | 2            | 0    |
| MEDIUM   | 1     | 0            | 1 (accepted, see below) |
| LOW      | 1     | 0            | 1 (accepted) |
| INFO     | 2     | —            | —    |

All HIGH findings were fixed inline in this review pass; `make test-unit-go` runs green.

---

## HIGH-1 — Four-eyes principle bypassed (self-approval)  [FIXED INLINE]

**File:** `gateway/internal/admin/compliance_handler.go` — `DBComplianceApprovalClient.decide`
**CWE:** CWE-863 (Incorrect Authorization), CWE-840 (Business Logic Errors)
**STRIDE:** Elevation of Privilege
**ASVS:** V4.1.5 (Application enforces business-logic separation of duties)
**Nebu invariant:** Four-eyes principle, originally enforced by `gateway/internal/compliance/handler.go:378` and explicitly tested by Story 5.4.

### What I found
The new `DBComplianceApprovalClient.decide()` only verified that the row exists and is `pending`. It did NOT verify that the approver is distinct from the requester. The JWT-gated path (`compliance.AccessRequestHandler.postDecision`) enforces:

```go
if requesterUserID == callerSub {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Self-approval is not permitted")
    return
}
```

The new admin-UI path bypassed the JWT role gate by design (the story Dev Notes explicitly call this out), but in doing so it also bypassed the self-approval guard — a separate, equally important security control. Any compliance officer who happens to have an `instance_admin` session, or any instance_admin who has previously filed a compliance request as a regular user, could approve their own request via `/admin/compliance/{id}/approve`.

This is a regression of an existing security control: the four-eyes property promised by the JWT API was silently dropped on the parallel admin path. This is exactly the failure mode the SEC Gate 2 audit (2026-04-20) called out as recurring across epics — security invariants enforced on one path but not on a sibling path added later.

### Attack path
1. Compliance officer Alice files a compliance access request via `POST /api/v1/compliance/access-requests`.
2. Alice (or anyone sharing her sub) is also an `instance_admin`.
3. Alice navigates to `/admin/compliance` → sees her own pending request → clicks Approve.
4. **Pre-fix:** approval succeeds, audit row written with Alice as both requester and approver.
5. Alice immediately mints a compliance JWT and exports messages from any room she nominated.

### Fix applied (inline this review)
- `decide()` pre-flight `SELECT` now reads `status, requester_user_id` (single row).
- New sentinel `errComplianceSelfDecision`; both `ApproveHandler` and `RejectHandler` map it to `flash=Self-approval+is+not+permitted` (302) with a `slog.Warn` audit-log breadcrumb.
- Empty `approverSub` is rejected upfront so a session-guard misconfiguration cannot pass an empty string and trivially satisfy the inequality.
- Comment block in `decide()` documents the invariant and points to the canonical guard in `gateway/internal/compliance/handler.go`.

### Verification
- `make test-unit-go` green (1.0 s — `cmd/gateway`, 7.0 s — `internal/admin`).
- Manual spot-check: pre-flight `SELECT` returns both columns, `requesterUserID == approverSub` short-circuits before UPDATE.

---

## HIGH-2 — Rejection-reason length unbounded → audit-log inflation  [FIXED INLINE]

**File:** `gateway/internal/admin/compliance_handler.go` — `RejectHandler` + `decide`
**CWE:** CWE-770 (Allocation of Resources Without Limits or Throttling), CWE-779 (Logging of Excessive Data)
**STRIDE:** Denial of Service (against the audit log + Postgres JSONB), Repudiation (logs become hard to read).
**ASVS:** V8.1.4 (cap log payload size).
**Nebu invariant:** FB-54-01 — JWT compliance API caps `note` at 4096 chars; admin UI must not provide a wider channel.

### What I found
`RejectHandler` reads `r.FormValue("rejection_reason")` and passes it to `decide()` as `note`, which is later marshalled into the audit `metadata` JSON. The intermediate hop has the 16 KiB metadata cap (`audit.MaxMetadataJSONBytes`) which silently *replaces* the payload with `"{}"` if exceeded — meaning a long rejection reason simply vanishes from the audit log without any error surfaced to the operator. The matching JWT path enforces 4096 chars and returns `400 M_BAD_JSON` (FB-54-01); the admin-UI path silently swallows the input.

A malicious or misclicking admin pasting a 100 KB log dump as the rejection reason would (a) silently lose their reasoning from the audit row, and (b) waste a write cycle. Combined with `bodyLimit64KiB` on the route, the upper bound is 64 KiB per request, but the audit-log behaviour is still wrong.

### Fix applied (inline this review)
- `errComplianceNoteTooLong` sentinel + `maxComplianceNoteLen = 4096` const, both mirroring the JWT path.
- `decide()` returns `errComplianceNoteTooLong` when `len(note) > 4096`.
- `RejectHandler` redirects with `flash=Rejection+reason+is+too+long` so the operator gets immediate feedback.
- `r.ParseForm()` error is no longer swallowed — handler now redirects with the generic error flash on parse failure.

### Verification
- `make test-unit-go` green; existing `compliance_test.go` stub-fallback paths unaffected (svc==nil branch unchanged).

---

## MEDIUM-1 — Duplicate `*sql.DB` pool against the same DSN  [ACCEPTED]

**File:** `gateway/cmd/gateway/main.go` lines 344 and 954.
**CWE:** CWE-405 (Asymmetric Resource Consumption — Amplification)

`adminComplianceDB` (line 344) and `complianceDB` (line 954) both `sql.Open("pgx", cfg.DBURL)`. Two connection pools to the same DB are wasteful (doubled idle connections, doubled DSN parse, doubled `defer Close`).

**Risk:** Low — `sql.DB` defaults are conservative; Postgres connection limit (`max_connections=100` default) is not at risk.
**Decision:** Not a security issue, just a hygiene one. Leaving the structural refactor for a Phase-2 cleanup story so this review does not re-shape ordering of `main.go` (which would balloon the diff and risk regressions in unrelated startup flow).

---

## LOW-1 — Playwright test does not actually verify DB persistence  [ACCEPTED]

**File:** `e2e/tests/features/admin/compliance-api-integration.spec.ts`
**Test review concern, not a vulnerability.**

The "approved request appears under ?status=approved (DB-backed, not in-memory)" test cannot distinguish the in-memory stub branch from the real DB branch. The test comment acknowledges this: *"With stubs only: the stub mutation is in-memory but visible in the same process. With real DB: the request must be persisted and appear in the real query."* — both paths satisfy the assertion in the same process.

**Decision:** Acceptable. True persistence verification would require either an admin API for compliance state (out of scope for Story 9.5) or `docker exec ... psql` access from Playwright (banned by CLAUDE.md "no DB-seeding shortcuts"). The AC3 `TestNoTODOEpic6InComplianceHandlerGo` test guarantees the stub-mutation branch was deleted, which is the strongest static guarantee available without infra changes.

---

## Items checked, no findings

### CSRF on state-changing endpoints — CLEAN
Both POST routes are wrapped by `csrf(sessionGuard(...))` (main.go lines 356–357). Pattern matches the rest of the admin write surface.

### SQL injection in compliance queries — CLEAN
All queries in `DBComplianceApprovalClient` use `$N` placeholders and `QueryRowContext` / `QueryContext`. No string concatenation, no `fmt.Sprintf` into SQL, no dynamic table/column names. Parameter list is fully bound.

### Audit log attribution — CLEAN (after HIGH-1 fix)
`auditpkg.LogEvent` is called with `approverSub` (admin's session sub) as `actor_user_id`, `compliance_request` as `target_type`, and the requestID as `target_id`. The `note` is passed as JSON metadata, now length-capped (HIGH-2 fix). The 500 ms timeout + never-raise semantics match the existing compliance API path.

### XSS in flash / reject-reason display — CLEAN
The rejection reason is stored in audit metadata only — never rendered back into the admin UI in this story. Flash messages are routed through `sanitizeFlash()` (existing). Audit log UI (Story 7.12) is read-only and applies its own escaping.

### Auth: instance_admin session vs compliance_officer JWT — CLEAN (with documented rationale)
The story's Dev Notes correctly identify that the admin UI path uses session-based `instance_admin` auth rather than JWT `compliance_officer`. The session guard runs on every admin POST. The session sub is propagated into gRPC metadata via `contextWithAdminIdentity` so the Core audit row is attributed correctly. The HIGH-1 fix restores the four-eyes invariant that this auth-model split inadvertently dropped.

### Body-size limit — CLEAN
`bodyLimit64KiB(csrf(sessionGuard(...)))` wraps both POST routes. Inner `r.FormValue` cannot exceed 64 KiB — within the reduced 4096-char `note` cap (HIGH-2).

### Rate limiting — INFO
The admin compliance routes are not wrapped in `complianceRL` (10/min/IP) the way `/api/v1/compliance/*` is. They sit behind the session guard and the global admin rate limit (if any). Approve/Reject is not anonymous-reachable, so an exterior brute-force is gated on a valid admin session. Acceptable for MVP; revisit if four-eyes scale to many concurrent reviewers.

### Secrets in logs — CLEAN
`slog.Warn` calls log `id`, `err`, `approver_sub` (the session subject claim — the same value already in audit). No JWTs, no DB DSN, no `note` content. Reject-reason content is never logged.

### Path traversal / IDOR — CLEAN
`requestID` is `r.PathValue("id")`, used only as a `$1` parameter against the UUID PK. Pre-flight `SELECT` ensures the row exists before any write. RLS on `compliance_requests` is open-USING for the application role (migration 000019), so RLS does not provide additional defence; the application-layer guards (status='pending', requester != approver, request exists) are the load-bearing controls.

### JWT validation flaws — N/A (no JWTs in this path)
The admin-UI path explicitly does not consume a compliance JWT.

---

## Final classification: CLEAN (post-fix)

All HIGH issues were fixed inline this review and verified with `make test-unit-go`. The remaining MEDIUM and LOW are accepted with rationale above; neither blocks the story.

**Test-Review (TEA) summary:**
- AC1, AC2, AC3 each have at least one test (Playwright × 4, Go × 1).
- AC3 Go test (`TestNoTODOEpic6InComplianceHandlerGo`) is the strongest gate — it deletes the stub-mutation branch as a code-level invariant.
- No hard waits in Playwright tests (all `expect(...).toBe...({timeout})`); `loginAsAdmin` is imported from the shared helper as required by the helper-extraction work in Story 9.2.
- No GenServer state in this story → no crash/restart test required (CLAUDE.md exception clause).
- LOW-1 above is the one test-quality concern; acceptable for the reasons stated.
