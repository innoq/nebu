# Security Review — Story 5-3 (Compliance Access Request API)

- **Reviewer:** Kassandra (BMAD SEC Gate 1)
- **Date:** 2026-04-26
- **Scope:** `git diff --staged` — 9 files
  - `gateway/internal/compliance/handler.go` (new, 194 LoC)
  - `gateway/internal/compliance/handler_test.go` (new)
  - `gateway/migrations/000019_compliance_requests.{up,down}.sql` (new)
  - `gateway/migrations/migrations_test.go` (modified)
  - `gateway/test/integration/compliance_requests_migration_test.go` (new)
  - `gateway/cmd/gateway/main.go` (route wiring)
  - `_bmad-output/implementation-artifacts/5-3-compliance-access-request-api.md`
  - `_bmad-output/implementation-artifacts/sprint-status.yaml`
- **Story status:** `security_review: required`
- **Classification:** **CLEAN** (1 MEDIUM, 2 LOW, 2 INFO — all non-blocking; no CRITICAL/HIGH)

---

## Summary

The handler is small, defensive, and correct on the principal axes:

- Role gate uses the JWT-derived `system_role` from middleware; `compliance_officer` is the only accepted value, so `user` and `instance_admin` callers are both rejected (separation of duties).
- `requester_user_id` is sourced from the verified JWT `sub` (`ContextKeySub`), never from the body — non-spoofable.
- All SQL is parameterised; `room_id` is regex-validated (max 512 bytes) before reaching the DB.
- JSON decoding uses `DisallowUnknownFields`; body is bounded by `bodyLimit64KiB`.
- Audit emission is never-raise (500ms timeout, error swallowed) and PII-hygienic (`justification_length` only — raw text never leaves the gateway).
- Migration 000019 enables FORCE RLS, defines explicit policies for I/S/U, and denies DELETE — structurally correct even though `nebu` superuser bypasses RLS in dev (FB-51-01, tracked in 5-29).

The findings below are quality-of-defence improvements, not exploitable today.

## Severity Counter

| CRITICAL | HIGH | MEDIUM | LOW | INFO |
|---|---|---|---|---|
| 0 | 0 | 1 | 2 | 2 |

---

## Findings

### MEDIUM-1 — Missing rate-limit on `POST /api/v1/compliance/access-requests`

- **File:** `gateway/cmd/gateway/main.go:706`
- **CWE:** CWE-770 (Allocation of Resources Without Limits or Throttling), CWE-400 (Uncontrolled Resource Consumption)
- **Description:** Route is wired as `bodyLimit64KiB(jwtMiddleware(...))` only. Other authenticated mutation endpoints in main.go follow the `strictRL`/`looseRL` pattern; this route has neither.
- **Path:** A token-holding `compliance_officer` (or any token-holder probing the endpoint and getting 403) can hit it without per-IP throttling. Each request executes a `SELECT 1 FROM rooms` and (on success) an INSERT + gRPC audit emission. While the role gate prevents non-officers from reaching the DB-INSERT branch, every authenticated request reaches the validation + DB-existence-check branch.
- **Impact:** DB query flood from a compromised compliance-officer account; combined with FB-52-01 (gRPC port unauthenticated) the audit-log channel can be saturated indirectly. Not catastrophic — compliance officer is a small population with strong identity — but no defence-in-depth layer exists.
- **Recommendation:** Wrap with `strictRL` (consistent with admin POST endpoints). Trivial change. Defer to follow-up if scope-locked, but record.

### LOW-1 — `justification` has minimum but no maximum length

- **File:** `gateway/internal/compliance/handler.go:104` (only `len(req.Justification) < 20`)
- **CWE:** CWE-20 (Improper Input Validation), CWE-79 downstream (Cross-Site Scripting in admin UI render)
- **Description:** Justification is `TEXT NOT NULL` with no upper bound enforced. The `bodyLimit64KiB` middleware caps total body, so realistic max is ~63 KiB of justification — still well above any legitimate use.
- **Impact:** No direct impact on this endpoint. **Downstream risk for Story 5-4:** if the admin UI renders justification text with `{{ . }}` instead of `{{ . | html }}` (Go template auto-escape is on by default — but explicit verification needed), 63 KiB of attacker text becomes XSS surface in the admin officer's browser session.
- **Recommendation:** Add an explicit max (e.g. 4 KiB) here, OR document a hard requirement that Story 5-4 templates auto-escape and that the admin UI imposes a render-time truncation.

### LOW-2 — `time_range_start`/`time_range_end` accept arbitrary years

- **File:** `gateway/internal/compliance/handler.go:80–96`
- **CWE:** CWE-20 (Improper Input Validation)
- **Description:** `time.Parse(time.RFC3339, ...)` accepts year 0001 through 9999. A request with `time_range_start = 0001-01-01` and `time_range_end = 9999-12-31` is stored verbatim.
- **Impact:** No direct impact here (DB stores TIMESTAMPTZ; index unaffected). **Downstream risk for Story 5-5** (compliance session handler): a session built from such a request will query `events WHERE timestamp BETWEEN $start AND $end` over the full retention window — potential expensive scan / data-exfil through over-broad query.
- **Recommendation:** Sanity-bound the timestamps (e.g. start ≥ 2020-01-01, end ≤ now() + 1 year, total span ≤ 365 days) — either here or as a CHECK constraint in 5-4 when status transitions to `approved`.

### INFO-1 — Compliance handler opens its own DB pool

- **File:** `gateway/cmd/gateway/main.go:692–697`
- **Description:** A second `sql.Open(...)` for the same `cfg.DBURL`, distinct from the gateway's existing pool. Adds a second connection-limit budget without sharing pgxpool tuning.
- **Impact:** None today; could mask connection-leak diagnostics later.
- **Recommendation:** Reuse the existing DB handle if one is already plumbed.

### INFO-2 — Audit-log emission re-uses the unauthenticated gRPC channel (FB-52-01)

- **Description:** `WriteAuditLog` calls cross the same Core port (9000) that has no authentication. This is documented as **FB-52-01** under Story 5-29 and applies identically here. Not re-counted as a new finding.

---

## Nebu Invariants

| Invariant | Status | Note |
|---|---|---|
| Compliance RSP coverage | ✅ | Justification min 20 chars, captured before audit |
| `reason` field on compliance access | ✅ | `justification` field (mapped to `reason` in audit semantics) |
| Audit-log immutability | ✅ | Never-raise, append-only via gRPC; no UPDATE/DELETE path here |
| `instance_admin` notification | n/a | Out of scope for 5-3 (Story 5-4 territory) |
| OIDC token validation (`iss`, `aud`, `exp`) | ✅ | Inherited from `jwtMiddleware` |
| Power Level checks before room mutation | n/a | No room mutation in 5-3 |
| No hardcoded secrets | ✅ | Verified |
| No secrets in logs / error messages | ✅ | `slog.Error` carries `room_id` (not PII) and `err`; no token / JWT logging |
| Separation of duties (officer ≠ admin) | ✅ | `instance_admin` would be 403 |

---

## Decision

- **No CRITICAL/HIGH findings — Gate 4 PASS.**
- MEDIUM-1 and LOW-1/LOW-2 should be tracked as follow-ups (suggested location: Story 5-29 alongside FB-51-01/FB-52-01). Recommend creating **FB-53-01** (rate-limit on `/api/v1/compliance/access-requests`), **FB-53-02** (justification max length), **FB-53-03** (timestamp sanity bounds).
- Re-running this review is not required after the follow-ups are filed.
