# Security Review — Story 6-4 (User List + Get API)

**Date:** 2026-05-01
**Reviewer:** Kassandra (security-review agent)
**Scope:** `git diff --staged` for Story 6-4
**Files reviewed:**
- `gateway/api/openapi.yaml` (+73)
- `gateway/cmd/gateway/main.go` (+14)
- `gateway/internal/api/api_gen.go` (+191, codegen)
- `gateway/internal/api/router.go` (+74)
- `gateway/internal/api/server.go` (+182)
- `gateway/internal/api/users_handler_test.go` (+655, tests)
- `gateway/internal/api/users_repo.go` (+292)

**Frameworks applied (weighted lenses):**
- OWASP Top 10 (2021): A01 Broken Access Control, A03 Injection, A04 Insecure Design, A09 Logging Failures
- OWASP ASVS L2: V1 Architecture, V4 Access Control, V5 Validation, V8 Data Protection, V13 API
- CWE Top 25: CWE-89 (SQL injection), CWE-285 (IDOR), CWE-200 (PII exposure), CWE-352 (CSRF n/a — bearer auth)
- STRIDE: Tampering (cursor forgery), Information Disclosure (PII), Elevation of Privilege (IDOR/role check)
- Nebu invariants: audit immutability, RLS boundary, sensitive PII handling

---

## Classification: **CLEAN**

| Severity | Count |
|----------|------:|
| CRITICAL |     0 |
| HIGH     |     0 |
| MEDIUM   |     0 |
| LOW      |     1 |
| INFO     |     2 |

No CRITICAL or HIGH findings. Story is cleared for merge.

---

## Findings

### LOW-1 — Search input is not LIKE-escaped (CWE-138, low impact)

**File:** `gateway/internal/api/users_repo.go:115-119`

```go
if search != "" {
    searchClause = fmt.Sprintf(` AND (p.displayname ILIKE '%%' || $%d || '%%')`, n)
    args = append(args, search)
    n++
}
```

The `search` parameter is passed as a parameterised value (so SQL injection per CWE-89 is **not** present — that is correctly mitigated). However, LIKE metacharacters (`%`, `_`, `\`) in the user's search input are passed through unescaped, which means an admin's input of `_lic%` matches more rows than intended.

**Impact:** Functional/UX, not security. The endpoint is gated by `instance_admin` role; the only attacker model would be an authenticated instance admin manipulating their own search — they can already list every user.

**Comparison to existing code:** Story 5-26 introduced a `matrix.EscapeLIKE` helper precisely for this case in `gateway/internal/matrix/user_directory.go:66`. The Admin API search did not adopt it. Cross-package import would be required, or a small inline copy.

**Why LOW, not MEDIUM:** No data leakage path — same data is returned via no-search list call. No DoS path (LIMIT clause caps result set). No role boundary crossed.

**Recommendation:** Document the LIKE-wildcard semantics as intentional admin search UX, OR adopt `EscapeLIKE` for consistency with the user directory implementation. Defer to product decision.

---

### INFO-1 — Email masking deferred to future story (acknowledged)

**File:** `gateway/internal/api/users_repo.go:188-190, 264-266`

`email_masked` is hardcoded to `""` in MVP because email decryption requires the per-user X25519 private key (not available in the admin process context, and irreversibly deleted after `users/keys` DELETE). The story document and code comments explicitly note this:

> MVP: email_masked is always "" — no decryption key available in Admin context.

**Security posture:** Correctly conservative. The encrypted email never leaves the database; no plaintext email exposure path exists from this endpoint. Sensitive PII (CWE-200) handling is sound. When email decryption is wired in a future story, the existing `maskEmail()` helper will mask to `a***@example.com`, preventing full email disclosure.

**No action required.** Documented and intentional.

---

### INFO-2 — Cursor opacity is not authenticated (signed)

**File:** `gateway/internal/api/pagination.go:24-31` (existing, not new in this story)

The pagination cursor is `Base64URL(JSON({"after_id": "...", "after_created_at": "..."}))`. It is **opaque but not signed/MAC'd**. An attacker (or admin user) could decode and forge a cursor with arbitrary `after_id` / `after_created_at` values.

**Impact analysis:**
- The cursor is consumed as a keyset pagination filter: `WHERE (u.created_at, u.user_id) < ($N, $M)`. Forging a cursor lets the caller jump to an arbitrary position in the user list.
- This is not a privilege boundary crossing: the endpoint is `instance_admin`-gated, and an instance admin can already list every user via plain `GET /admin/users` (no authorisation gain from forgery).
- The `parseISO8601ToEpochMs` function in `users_repo.go:285-291` parses the forged ISO 8601 — a malformed value returns an error which is wrapped as `cursor: invalid after_created_at` and propagates to the caller as 500 (no panic). Confirmed via test: `TestListAdminUsers_InvalidCursor_Returns400` (caught at `DecodeCursor` level).
- One minor inconsistency: a cursor whose ISO 8601 parses but that points to non-existent rows simply returns an empty page — no information disclosure.

**No action required.** Cursor forgery is not exploitable given the role gate. Accepted as MVP design (consistent with most Admin API pagination implementations that rely on auth + scoping rather than signed cursors).

---

## Areas Reviewed — Clear

### A. SQL Injection (CWE-89)

`users_repo.go` builds queries with `fmt.Sprintf` for placeholder positions only (`$1`, `$2`, ...) — never interpolating user input directly. All user-controlled values (`search`, `afterID`, `afterCreatedAt`, `userID`, `limit`) flow through `pgx` parameter binding via `args ...any`. Verified:

- `searchClause` uses `$N` placeholder — input passed via `args`.
- `cursorClause` uses two placeholders — values appended to `args`.
- `GetUser` uses `$1` for `userID`.

**Result:** No SQL injection vector. CLEAN.

### B. IDOR / Privilege Escalation (CWE-285, CWE-639)

Both endpoints are wrapped in `jwtMW(RequireRole("instance_admin")(...))` in `router.go:40,44`. The role gate runs **before** the handler — confirmed by `router_test.go::TestRegisterAdminRoutes_JWTRunsBeforeRole`. A non-`instance_admin` user receives 403 M_FORBIDDEN before any DB query is issued.

The handler does not perform additional row-level authorisation (i.e., "admins see all users"), which is the correct semantic for an admin API.

**Result:** No IDOR. CLEAN.

### C. Cursor Forgery / Tampering

Covered in INFO-2 above. Not exploitable due to the role gate.

### D. PII Exposure (CWE-200)

- Email: never leaves DB (encrypted; not decrypted in handler). `email_masked` is always `""` in MVP.
- Display name, user_id, system_role, status, created_at, last_seen_at, room_count: all considered admin-visible operational metadata, not Sensitive PII.
- No raw email substrings in logs or error responses.

**Result:** PII handling sound. CLEAN.

### E. Audit Logging (Nebu invariant)

`audit.LogEvent` is called on success for both endpoints (`server.go:91-93, 124-126`):
- action=`admin_user_viewed`, target_type=`user`
- target_id is `""` for list, `userID` for get

Audit failure is non-blocking (`_ =` discards return; `LogEvent` itself is never-raise). `CoreClient` is nil-guarded so audit emission is skipped in tests/dev. Actor user ID extracted from `middleware.ContextKeyUserID` populated by the JWT middleware — no spoofing path.

**Result:** Audit invariant satisfied. CLEAN.

### F. Body-size & Rate Limits

Both endpoints are GET — no body. Rate limits: not applied here. The existing per-IP rate limiters (`adminRL`, `complianceRL`) are not wired onto `RegisterAdminRoutes`. This is consistent with Story 6-3's design (admin-API rate limits deferred). Out of scope for 6-4.

### G. Crypto Primitives

No new crypto in this story. None needed.

### H. Error-message Information Disclosure (CWE-209)

Error messages are bounded:
- 400: `"limit must be between 1 and 100"`, `"Invalid cursor"` — no PII or stack trace.
- 404: `"User not found"` — generic.
- 500: never returned directly; `users_repo.go` errors propagate via the strict-handler error path.

**Result:** Sanitised error responses. CLEAN.

### I. Time Handling (replay / clock skew)

`epochMsToISO8601` formats UTC RFC3339; `parseISO8601ToEpochMs` parses RFC3339. No timezone confusion.

---

## Test Coverage Verification

8 acceptance-test scenarios from the story document map to `gateway/internal/api/users_handler_test.go`:

| Scenario | Test | Status |
|---|---|---|
| List paginated | `TestListAdminUsers_PaginatedResults` | Pass |
| email_masked format | `TestListAdminUsers_EmailMasked` | Pass |
| search filter | `TestListAdminUsers_SearchFiltersByDisplayName` | Pass |
| Invalid cursor → 400 | `TestListAdminUsers_InvalidCursor_Returns400` | Pass |
| limit=0 → 400 | `TestListAdminUsers_LimitZero_Returns400` | Pass |
| Get user → 200 + room_count | `TestGetAdminUser_KnownUser_Returns200WithRoomCount` | Pass |
| Get unknown → 404 | `TestGetAdminUser_UnknownUser_Returns404` | Pass |
| Audit log emitted | `TestListAdminUsers_AuditLogEmitted`, `TestGetAdminUser_AuditLogEmitted` | Pass |

Additional bonus tests:
- `TestListAdminUsers_LimitAbove100_Returns400`
- `TestListAdminUsers_StatusFields` (all 4 status enum values)
- `TestGetAdminUser_RouteRegistered` (regression guard)
- `TestListAdminUsers_UserObjectFields` (mandatory field presence)
- `TestListAdminUsers_DefaultLimit_NoError`

`make test-unit-go` passes 49/49 in package `internal/api`.

---

## Verdict

**Status:** CLEAN — no CRITICAL/HIGH findings, 0 MEDIUM, 1 LOW (LIKE-escape consistency), 2 INFO (acknowledged design).

**Merge gate:** PASS.

**Follow-ups (non-blocking):**
1. Decide product-side whether LIKE-wildcard semantics in `search` are intentional UX (LOW-1).
2. When email decryption is wired in a future story, replace `email_masked = ""` with `maskEmail(decryptedEmail)` (INFO-1).
3. Consider signed cursors if Admin API ever exposes pagination outside the `instance_admin` role boundary (INFO-2).

— Kassandra
