# Security Review — Story 5.1 Audit Log Schema + RLS + Retention Config — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (14 files, ~1150 insertions)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4.7-1m`

## Executive Summary

The audit-log foundation is well-constructed. Append-only immutability is expressed at three layers (explicit `USING (false)` deny-policies on UPDATE/DELETE, `FORCE ROW LEVEL SECURITY`, and a narrowly-scoped `SECURITY DEFINER` purge function). CVE-2018-1058-class hardening (`SET search_path = pg_catalog, public` plus `REVOKE ... FROM PUBLIC` + targeted `GRANT EXECUTE TO nebu`) is applied per the pre-review fix. Input validation on `retention_days` is enforced both in Go (`ErrInvalidRetentionDays`) and in SQL (`RAISE EXCEPTION` with `ERRCODE = 'invalid_parameter_value'`) — proper defense-in-depth. Two observations deserve the author's attention: `event_time` is not trigger-enforced to `NOW()`, so an INSERT-capable caller can choose any timestamp (MEDIUM, defense-in-depth); and `RunCleanup` has no production caller yet — scheduling + authorization wiring is deferred to a later story, which this review cannot assess. No CRITICAL or HIGH findings. FB-51-01 (nebu role has `BYPASSRLS=t` in Dev) is accepted as documented follow-up in Story 5-29 and is not re-raised here.

## Findings

### [MEDIUM] `event_time` is caller-supplied, not trigger-enforced

- **CWE / OWASP:** CWE-1287 (Improper Validation of Specified Type of Input) / A04:2021
- **Datei:** `gateway/migrations/000018_audit_log.up.sql:8`
- **Beschreibung:** `event_time TIMESTAMPTZ NOT NULL DEFAULT NOW()` sets a default but does not prevent the INSERT caller from providing an explicit value. The RLS policies block UPDATE and DELETE but do not constrain the values an INSERT may write. The integration test `TestAuditLogRetentionCleanup_DeletesOldRows` itself demonstrates the capability at `gateway/internal/audit/retention_test.go:57-65` (`INSERT INTO audit_log (event_time, ...) VALUES ($1, ...)` with a handpicked `oldTime`). An attacker who has already compromised the app process can therefore forward-date (hide an entry beyond the visible window), back-date (make an action look older than it is), or plant entries with `event_time < NOW() - retention_days` that are swept by the next purge cycle — effectively deleting evidence of their own action through the sanctioned purge function.
- **Impact:** Weakens non-repudiation of audit timestamps. Requires app-role compromise to exploit (the role that writes audit rows is the same role that an attacker-controlled handler runs under), so impact is bounded; not a primary breach vector, but erodes the "append-only, immutable" invariant from the story premise. A compliance auditor would flag a log whose timestamps are not system-enforced.
- **Empfehlung:** Either (a) make the column non-writable from INSERT via a `BEFORE INSERT` trigger that overwrites `NEW.event_time := NOW()`, or (b) revoke column-level INSERT on `event_time` from the app role (`REVOKE INSERT (event_time) ON audit_log FROM nebu`) and grant it only to a migration/system role. Option (a) is more robust because column-level REVOKE under FORCE RLS interacts in subtle ways; the trigger is unambiguous. Update the retention integration test to seed `event_time` via `UPDATE` through the owner connection (or via `pg_catalog.pg_class`-scoped seed helper) rather than caller-supplied INSERT.
- **Referenz:** OWASP ASVS V7.3.1 (log data integrity); NIST SP 800-92 §4.2 (log source integrity).

### [LOW] `make_interval(days => retention_days)` has no upper bound

- **CWE / OWASP:** CWE-190 (Integer Overflow or Wraparound)
- **Datei:** `gateway/migrations/000018_audit_log.up.sql:69`
- **Beschreibung:** The Go guard refuses `retentionDays < 1` but not a very large value. `make_interval(days => 2147483647)` (INT_MAX) raises `ERROR: interval out of range`. A corrupted `server_config` value or a caller that passes INT_MAX crashes the purge goroutine rather than purging. Not a security exploit — it is a DoS against the cleanup path, which is not externally reachable in the current diff (no caller).
- **Impact:** Runtime error if the retention value is ever pathological. No data exposure, no privilege escalation.
- **Empfehlung:** Add an upper bound to `ErrInvalidRetentionDays` (e.g. `retentionDays > 36500` — 100 years — rejected) or cap the SQL side (`IF retention_days > 36500 THEN RAISE EXCEPTION ...`). Matches the existing defense-in-depth pattern.
- **Referenz:** CWE-190.

### [INFO] `RunCleanup` is library-only — no production caller wired

- **Datei:** `gateway/internal/audit/audit.go:27`
- **Beschreibung:** The staged diff adds `RunCleanup` but grep finds no caller outside test files. AC4 of the story defers the scheduling strategy ("goroutine im Gateway (Ticker, täglich) oder pg_cron") without committing to one. Once wired, the call site MUST ensure:
  1. It runs under an identity the audit-log trail can attribute (ideally the system/scheduler user, not an interactive admin).
  2. If exposed via an HTTP handler, the endpoint is admin-only (CWE-862) AND the purge itself is audited (meta-logging) — otherwise the act of purging has no record.
  3. The `retentionDays` value is read from `server_config` via an admin-only write path; confirm `server_config` UPDATE is not reachable by regular users (appears to be gated through admin handlers per `auth.go`).
- **Impact:** Not an issue in this story; flagged so the wiring story (likely 5-2 or a successor) inherits the checklist.
- **Empfehlung:** In the wiring story, add a Playwright/Godog acceptance test that proves unauthenticated callers cannot trigger a purge, plus an explicit audit-log entry `action='audit_log_purged', outcome='success', metadata={deleted_count: N, retention_days: R}`.

### [INFO] Down-migration drops the audit history

- **Datei:** `gateway/migrations/000018_audit_log.down.sql:5`
- **Beschreibung:** `DROP TABLE IF EXISTS audit_log` on rollback destroys every compliance record. Standard for migration reversibility; operators running a rollback in production need to know this means deliberate destruction of regulated data. Noted for the operations runbook rather than a code fix.
- **Empfehlung:** Operations runbook entry: "Rolling back migration 000018 is an irreversible destruction of audit history. Prefer a forward-fix migration; escalate to the compliance officer before any rollback in production."

### [INFO] Positive observations

- `ENABLE ROW LEVEL SECURITY` + `FORCE ROW LEVEL SECURITY` both set — aligned with the Nebu DB invariant.
- `SET search_path = pg_catalog, public` present on the SECURITY DEFINER function — CVE-2018-1058-class defense in place.
- `REVOKE ALL ON FUNCTION audit_log_purge(INT) FROM PUBLIC; GRANT EXECUTE ... TO nebu` — principle of least privilege applied to the DEFINER function.
- Double-guard on `retention_days`: Go refuses `< 1`, SQL also refuses `IS NULL OR < 1` with typed `ERRCODE = 'invalid_parameter_value'`. Defense-in-depth, deliberate, commented.
- Parameterized SQL throughout — no string concatenation, no `format()` with `EXECUTE`.
- Bootstrap seed value `"2555"` is a hard-coded literal, not attacker-controllable.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ (not in scope — writer is Story 5-2) |
| Audit-log immutability                      | ⚠️ (UPDATE/DELETE denied; `event_time` is caller-supplied — see MEDIUM) |
| `instance_admin` notification (if in-scope) | ✅ (not in scope) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (not in scope) |
| Matrix Power Level checks                   | ✅ (not in scope) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (not in scope) |
| AES-256-GCM correctness                     | ✅ (not in scope) |
| Ed25519 verify-before-accept                | ✅ (not in scope) |
| No secrets in logs / error messages         | ✅ |

The ⚠️ on audit-log immutability maps to the MEDIUM finding above. Verifying full immutability would require either a trigger-enforced `event_time` or column-level REVOKE of INSERT on `event_time` from the app role; neither is present in the staged diff.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 1 |
| INFO      | 3 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed. The MEDIUM finding (`event_time` not trigger-enforced) should be triaged: either addressed in-story or tracked as a follow-up alongside the FB-51-01 BYPASSRLS architectural fix in Story 5-29, since both concern the same "what can the app role actually do under the RLS envelope" question. The LOW upper-bound on `retention_days` is a hardening improvement for the caller-wiring story.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
