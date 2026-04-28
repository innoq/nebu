# Security Review — Story 5.7 (Atomic GDPR Key Deletion + Failure-Invariant Audit) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `aa1cc89~1..HEAD` (commits `aa1cc89` + `7180486`)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=opus-4-7[1m]`

## Executive Summary

Story 5.7 implements `DELETE /api/v1/admin/users/{userId}/keys` with an atomic 5-step Postgres transaction in Elixir Core and a never-raise "attempted" audit emitted in a separate transaction on transactional failure. The implementation is sound and the post-code-review fixes (1000-char `reason` cap, no-shadowing of `reason` in the DB-error guard branch) close the local issues correctly. One MEDIUM finding remains: the `check_user → mark_in_progress` sequence is non-atomic, so two parallel admin calls can both pass the guard and trigger duplicate `keys_deleted` audit entries; functional impact is limited but the audit-trail noise warrants a fix. Cryptographic destruction is best-effort against the live row only — WAL/backup retention is documented as an operations boundary, not a code defect.

## Findings

### [MEDIUM] TOCTOU race between `check_user` guard and `mark_in_progress`

- **CWE / OWASP:** CWE-367 (Time-of-check Time-of-use Race) / A04:2021
- **Datei:** `core/apps/compliance/lib/compliance/user_deletion.ex:49-72` (`check_user/1` outside TX, `mark_in_progress/1` inside TX with non-conditional UPDATE on user_id)
- **Beschreibung:** `check_user/1` reads `deletion_status` outside the transaction. Two concurrent admin requests (admin A and admin B) can both observe `deletion_status = NULL`, both pass the `:conflict` guard, both enter `Repo.transaction/1`, and both run the full 5-step pipeline. Postgres row-locking serialises the UPDATE on the same row, so both transactions complete successfully; the second is a no-op on `private_key` (already NULL) but writes a fresh `keys_deleted_at`, and crucially each path emits a separate `user_keys_deleted` success audit entry with a different `admin_user_id`. The audit trail therefore shows two completed deletions for one logical event.
- **Impact:** Audit-trail noise / ambiguity — a single GDPR deletion appears as multiple completed events with different actors. Forensically misleading; not exploitable for data confidentiality (private keys are NULL after the first commit). No bypass of the deletion itself.
- **Empfehlung:** Fold the conflict check into a single conditional UPDATE inside the transaction:
  ```elixir
  UPDATE users
     SET deletion_status = 'deletion_in_progress'
   WHERE user_id = $1
     AND (deletion_status IS NULL OR deletion_status = 'active')
  RETURNING user_id
  ```
  If `num_rows = 0`, return `{:error, :conflict}` (or `:user_not_found` after a separate existence probe). This makes the guard atomic against concurrent callers without changing the public contract.
- **Referenz:** OWASP ASVS V11.1.4 (atomic state transitions); CWE-367.

### [INFO] Soft-delete leaves cleartext private keys in WAL and backups

- **CWE / OWASP:** CWE-212 (Improper Removal of Sensitive Information Before Storage or Transfer)
- **Datei:** `core/apps/compliance/lib/compliance/user_deletion.ex:139-167`; migration `gateway/migrations/000004_users.up.sql` (BYTEA column).
- **Beschreibung:** `UPDATE user_keys SET private_key = NULL` removes the cleartext from the live row only. Pre-update tuples remain in heap pages until VACUUM, and the original `private_key BYTEA` payload persists in WAL records, archived WAL, base backups, and PITR streams for the configured retention window (industry default 7–30 days). AC5 ("permanently unreadable") is satisfied for the application surface, but operationally the secret is recoverable by anyone with backup/WAL access during retention.
- **Impact:** Without backup/WAL hygiene, a DSGVO erasure request is incomplete in the strict cryptographic-shredding sense. The application behaves correctly; the gap is operational.
- **Empfehlung:** Document the WAL/backup retention as a known boundary in the operations runbook for GDPR compliance. Optional hardening (Phase 2): wrap `private_key` at rest with a per-user data-encryption key (DEK) stored in a separate KMS-backed table; "deletion" then becomes destruction of the DEK, which propagates correctly through WAL/backups within minutes (crypto-shredding pattern).
- **Referenz:** NIST SP 800-88 Rev. 1 §2.5 (Cryptographic Erase); GDPR Art. 17 §1 ("right to erasure").

### [INFO] gRPC `INTERNAL` error message echoes raw `inspect(reason)` to caller

- **CWE / OWASP:** CWE-209 (Information Exposure Through Error Message)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:625-633`
- **Beschreibung:** On a transactional failure the gRPC handler raises `GRPC.RPCError` with `message: "deletion failed: #{inspect(reason)}"`. The Go gateway (`user_key_deletion.go:111-113`) maps any non-`NotFound`/`AlreadyExists` code to a generic 500 `M_UNKNOWN`, so the `inspect(reason)` payload never reaches the HTTP client — but it is still serialised onto the gRPC status `message` field, which is logged or forwarded by intermediaries and may surface DB error tuples (constraint names, column names). Informational only because it is contained inside the trust boundary.
- **Impact:** Internal observability leaks raw `Postgrex` error tuples into logs that are also written through the audit pipeline (`error: inspect(error_reason)` is already in the `attempted`-audit metadata, which is the correct destination). The duplication on the gRPC `message` field is redundant.
- **Empfehlung:** Replace `message: "deletion failed: #{inspect(reason)}"` with a fixed string `message: "deletion failed"`; the detailed reason is already persisted in the `user_keys_deletion_attempted` audit metadata.
- **Referenz:** OWASP ASVS V8.3.4 (sensitive data not in logs at higher layers than necessary).

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ (required, 10–1000 chars, persisted in success + attempted audits) |
| Audit-log immutability                      | ✅ (RLS on `audit_log` from Story 5-1; append-only) |
| `instance_admin` notification (if in-scope) | n/a (this IS the admin-action; no notification target) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (route guarded by `jwtMiddleware`; unchanged) |
| Matrix Power Level checks                   | n/a (admin API, not a Matrix room mutation) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (gateway-level; unchanged) |
| AES-256-GCM correctness                     | n/a (no new crypto; relies on existing `Nebu.Signature`) |
| Ed25519 verify-before-accept                | n/a |
| No secrets in logs / error messages         | ⚠️ (see INFO finding on `inspect(reason)` in gRPC `message`; private-key bytes are never logged) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 0 |
| INFO      | 2 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The MEDIUM TOCTOU race should be tracked as a follow-up story (audit-trail integrity for concurrent admin actions). The two INFO items are documentation/operations boundaries — no code change required for this story.

Carry-over findings (FB-51-01 BYPASSRLS, FB-52-01 gRPC-Auth, FB-52-02 audit-action allowlist, FB-53-01 rate-limit, FB-55-01 plain-key-storage, FB-56-01 export-streaming) are not re-reported here per scope instruction.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
