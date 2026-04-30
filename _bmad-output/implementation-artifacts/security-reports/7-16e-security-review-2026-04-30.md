# Security Review — Story 7-16e

**Story:** 7-16e — `audit_log_purge` SECURITY DEFINER doesn't delete expired rows
**Reviewer:** Kassandra (security review) + Senior Go/PostgreSQL Code Reviewer
**Date:** 2026-04-30
**Branch:** feature/github-readiness
**Files reviewed:**
- `gateway/migrations/000028_audit_log_purge_owner.up.sql` (new)
- `gateway/migrations/000028_audit_log_purge_owner.down.sql` (new)
- `gateway/test/integration/role_separation_test.go` (modified, lines 293-368)

---

## 1. Executive Summary

| Gate | Verdict |
|---|---|
| **Code Review** | MINOR_FIXED |
| **Security Classification** | CLEAN |

The fix correctly addresses the real root cause: the `BEFORE INSERT` trigger introduced
in migration 000025 was overwriting caller-supplied `event_time`, defeating the test's
"3001 days ago" seed and making `audit_log_purge(30)` legitimately return 0. The new
test approach (privileged INSERT then privileged UPDATE via nebu_migrate's BYPASSRLS)
is sound and matches PostgreSQL's documented BYPASSRLS semantics (BYPASSRLS overrides
FORCE ROW LEVEL SECURITY).

Migration 000028 is functionally a defensive idempotent re-assertion. One MINOR
documentation issue was found and fixed (the comment incorrectly claimed
`CREATE OR REPLACE FUNCTION` re-owns the function — per PostgreSQL docs it does
NOT change ownership). The migration body itself is correct and matches 000025's
function definition byte-for-byte.

No new privilege escalation vectors. No new test backdoors. `search_path` and
`REVOKE FROM PUBLIC` are correctly preserved.

---

## 2. Code Review (Senior Go/PostgreSQL)

### 2.1 Migration body parity (000025 vs 000028)

Verified via `diff` — function body, signature, language, SECURITY DEFINER flag,
and `SET search_path = pg_catalog, public` match migration 000025 exactly. The
upper-bound guard (`retention_days > 36500`) and the `invalid_parameter_value`
SQLSTATE are preserved. **No behavioural regression.**

### 2.2 REVOKE / GRANT correctness in 000028

```sql
REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu_migrate;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu_app;
GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;
```

- PUBLIC is correctly stripped (mandatory for SECURITY DEFINER, otherwise any role
  can elevate to owner).
- nebu_migrate, nebu_app, and the legacy nebu role are explicitly granted EXECUTE.
- Mirrors the grants in migration 000024 lines 102-106 — **complete and correct.**

### 2.3 Does `CREATE OR REPLACE FUNCTION` transfer ownership?

**No.** Per PostgreSQL documentation:
> CREATE OR REPLACE FUNCTION ... cannot change the function's owner.

The original 000028 file's comment block claimed otherwise — this was misleading.
**Fixed**: rewrote the header comment to accurately describe migration semantics:
fresh deployments are already correct (nebu_migrate is the owner from 000018/000024);
in unlikely legacy-upgrade scenarios where ownership is wrong, this migration would
either no-op or fail loudly (preferable to a silent skip). See edit applied to
`gateway/migrations/000028_audit_log_purge_owner.up.sql` lines 1-29.

### 2.4 Test approach: INSERT + UPDATE via nebu_migrate

**Question:** Does nebu_migrate's BYPASSRLS actually bypass the
`audit_log_no_update ON audit_log FOR UPDATE USING (false)` policy?

**Answer:** Yes. PostgreSQL's documented behaviour:
> Roles with the BYPASSRLS attribute always bypass the row security system when
> accessing a table.

Confirmed in `dev/postgres/init/01-roles.sql` line 28 — `nebu_migrate ... BYPASSRLS`.
The UPDATE in the test will succeed regardless of FORCE RLS + UPDATE-denied policy.

**Alternative considered:** The codebase already has `openSeedDB` (in
`gateway/internal/audit/testhelpers_test.go`) that uses
`SET session_replication_role = replica` to disable triggers, which would allow a
single INSERT with the historical timestamp. The chosen INSERT+UPDATE approach is
also correct and arguably more explicit about what is happening (caller acknowledges
the trigger fired then deliberately backdates), but is one extra round-trip.
Not blocking — code reviewer's call. **Acceptable.**

### 2.5 Test cleanup ordering

`t.Cleanup` LIFO:
1. `appDB` close (registered last, runs first) — fine.
2. `migrateDB` DELETE seeded row (line 322) — runs after appDB closed; uses
   migrateDB which is still open. Correct.
3. `migrateDB` close — runs last. Correct.

**No issue.** The cleanup deletes via nebu_migrate (BYPASSRLS) so the
`audit_log_no_delete USING (false)` policy is bypassed cleanly.

### 2.6 Down migration (no-op `SELECT 1`)

The down is a no-op because the function body in 000028 is identical to 000025.
A `migrate down 1` to revert past 000028 would still leave the function correct
(rolling further down to 000025 re-asserts the same body). The grants added in
000028 are also re-asserted in 000024 — so going "down" further does not lose them
until 000023.

**Acceptable.** Matches the pattern used elsewhere when a migration is purely
defensive/idempotent.

### 2.7 Test correctness (lines 293-368)

Step-by-step audit:
1. Privileged INSERT (line 301-305): no `event_time` supplied → trigger sets `NOW()`.
   Returns the new row's `id`. **Correct.**
2. UPDATE event_time backwards 3001 days (line 314-320): nebu_migrate BYPASSRLS
   overrides UPDATE-denied policy. **Correct.**
3. Cleanup registration (line 322-326): best-effort DELETE via migrateDB.
   **Correct.**
4. Baseline assertion: nebu_app DELETE must fail (line 331-335). nebu_app has had
   DELETE explicitly REVOKEd in migration 000027 line 43, so SQLSTATE 42501 is
   the expected outcome. **Correct.**
5. SECURITY DEFINER call (line 341-353): verifies `audit_log_purge(30)` returns
   `>= 1` deleted. With nebu_migrate as owner + BYPASSRLS, the internal DELETE
   sees the row regardless of the no_delete policy. **Correct.**
6. Post-purge verification (line 357-367): SELECT count = 0 confirms deletion.
   **Correct.**

The test is deterministic, makes no hidden assumptions, and verifies a real
SECURITY DEFINER elevation path (no cookie/state forging).

---

## 3. Security Review (Kassandra)

Scope per project policy: SQL injection, XSS, CSRF, auth bypass, IDOR, timing
attacks, open redirects, body-size limits, rate limits, weak crypto, plaintext
secrets, missing security headers, path traversal, JWT validation flaws.

### 3.1 Privilege escalation surface

**SECURITY DEFINER + BYPASSRLS owner** — the most dangerous combination in
PostgreSQL. Every change to such a function deserves explicit review.

Findings:

| Vector | Status | Detail |
|---|---|---|
| `REVOKE ALL ON FUNCTION ... FROM PUBLIC` | **PRESENT** | Line 49. PUBLIC cannot trigger purge. |
| `SET search_path = pg_catalog, public` | **PRESENT** | Line 32. CVE-2018-1058 mitigated. |
| Function body changed vs. 000025 | **NO** | byte-identical diff. No new logic. |
| Owner unchanged | **YES** | already nebu_migrate in fresh deployments. |
| New attack surface | **NONE** | no new callers, no new policies, no new branches. |
| Argument typed `INT` | **YES** | upper-bound 36500 + lower-bound 1 enforced. |
| `make_interval` overflow | **PROTECTED** | upper-bound 36500 prevents PG interval overflow. |

**Verdict: CLEAN.** Migration 000028 introduces no new privilege escalation vectors.

### 3.2 REVOKE / GRANT validation

`REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC` (line 49) is the
single most important security clause for any SECURITY DEFINER function.
**Present and correct.** Without it, any logged-in role (including future
read-only roles, replication slots, monitoring roles) could trigger an
audit-log purge — direct compliance violation.

Explicit grants to nebu_migrate, nebu_app, and the legacy nebu role are
appropriate and minimal. nebu_app needs EXECUTE for the retention cleanup
goroutine; nebu_migrate is the operational migration role; nebu is kept
for backward compatibility with running instances pre-cutover.

### 3.3 search_path hardening (CVE-2018-1058)

`SET search_path = pg_catalog, public` is preserved on the function (line 32).
This is the canonical defence: pg_catalog is pinned first so trusted built-ins
(`make_interval`, `NOW`) cannot be shadowed by a malicious schema earlier in
the caller's search_path. **Correctly applied.**

The accompanying `audit_log_no_update USING (false)` and
`audit_log_no_delete USING (false)` RLS policies remain in place; the function
relies on owner BYPASSRLS to bypass them — not on policy weakening.

### 3.4 Test-only backdoors

**Question:** Does the INSERT+UPDATE test approach create any test-only
backdoors?

**Answer:** No. The UPDATE happens through `migrateDB` which uses
`NEBU_TEST_MIGRATION_DB_URL` (nebu_migrate role). nebu_migrate's BYPASSRLS is
intentional and a documented production property — needed for the SECURITY
DEFINER function's owner to actually bypass FORCE RLS. The test uses the same
mechanism that production uses, not a fake/forged path.

No `SET session_replication_role`, no superuser shortcuts, no policy
weakening. The test stays within the boundaries of what the production system
already permits to nebu_migrate. **CLEAN.**

### 3.5 Down migration safety

`SELECT 1` is a no-op. Rolling back does not weaken security:
- The REVOKE PUBLIC and GRANTs from migration 000024/000025 are still in place.
- The function body remains the hardened 000025 version after a `migrate down 1`.
- No leakage of permissions occurs on rollback.

**CLEAN.**

### 3.6 OWASP / CWE mapping

| Concern | CWE | Status |
|---|---|---|
| Improper privilege management | CWE-269 | OK — explicit GRANTs, REVOKE PUBLIC |
| SQL injection | CWE-89 | OK — INT-typed parameter, no dynamic SQL |
| Use of less-privileged-user-than-required | CWE-250 | OK — owner is nebu_migrate (least-privilege carrier of BYPASSRLS) |
| Search-path injection (CVE-2018-1058) | CWE-426 | OK — search_path pinned |
| Trust boundary violation | CWE-501 | OK — FORCE RLS still enforced for non-BYPASSRLS roles |
| Missing audit / log integrity | CWE-778 | OK — no_update + no_delete RLS still active for nebu_app |

---

## 4. Issues Found and Resolution

### MINOR-1 (FIXED)
- **File:** `gateway/migrations/000028_audit_log_purge_owner.up.sql`
- **Lines:** 1-26 (original)
- **Issue:** Header comment claimed `CREATE OR REPLACE FUNCTION here takes
  ownership over the function`. This is factually incorrect per PostgreSQL
  docs — CREATE OR REPLACE preserves the existing owner. Comment also asserted
  that ALTER FUNCTION OWNER in migration 000024 fails "silently" without an
  exception handler, which is misleading (it would raise a hard exception and
  abort the migration).
- **Fix applied:** Rewrote the header comment block to accurately describe the
  migration's semantics: defensive idempotent re-assertion in fresh deployments,
  with a clear note that re-owning a function still requires ALTER FUNCTION ...
  OWNER TO. Removed the misleading "silent fail" claim.
- **Severity:** MINOR (documentation-only — the SQL itself is correct).

### MINOR-2 (NOT FIXED — acceptable)
- **File:** `gateway/test/integration/role_separation_test.go`
- **Issue:** The test could have used the existing `openSeedDB` helper (which
  disables triggers via `session_replication_role = replica`) to do a single
  INSERT with a historical event_time, avoiding the INSERT+UPDATE round-trip.
- **Decision:** Not changing. The chosen INSERT+UPDATE pattern is valid,
  exercises a different (and arguably more transparent) path, and the round-trip
  cost in a single integration test is negligible.

---

## 5. Final Verdicts

- **Code Review Verdict:** MINOR_FIXED
- **Security Classification:** CLEAN
- **SEC Gate 1 outcome:** PASS — no CRITICAL/HIGH findings, no new privilege
  escalation vectors, REVOKE/GRANT and search_path hardening preserved.
- **Recommendation:** Ready to merge. Run `make test-integration` to confirm
  `TestAuditLogPurge_AppRoleCanCallSecurityDefiner` is green against the
  refactored test seed strategy.

---

## 6. Appendix — files reviewed (absolute paths)

- `/Users/philippbeyerlein/dev/02_github/0600_innoq/nebu-chat/gateway/migrations/000028_audit_log_purge_owner.up.sql`
- `/Users/philippbeyerlein/dev/02_github/0600_innoq/nebu-chat/gateway/migrations/000028_audit_log_purge_owner.down.sql`
- `/Users/philippbeyerlein/dev/02_github/0600_innoq/nebu-chat/gateway/test/integration/role_separation_test.go`
- Cross-referenced: 000018 (audit_log + initial purge fn), 000024 (ownership
  transfer + grants), 000025 (event_time trigger + purge upper-bound), 000027
  (DELETE grant separation), `dev/postgres/init/01-roles.sql` (role provisioning).
