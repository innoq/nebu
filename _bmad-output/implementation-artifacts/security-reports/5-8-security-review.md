# Security Review — Story 5.8 (Operational PII Anonymization) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (13 files, ~1845 insertions)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4.7-1m`

## Executive Summary

The MAJOR path-traversal flaw caught at Code-Review Gate 3 — `mxc://` parsing
feeding `filepath.Join` directly into `os.Remove`, an arbitrary-file-delete
primitive triggered by a stored attacker-controlled `avatar_url` — has been
closed at the right layer (`isSafePathSegment` rejects `.`, `..`, `/`, `\`,
`\x00`) and is exercised by six dedicated traversal sub-tests. The remaining
weaknesses (multi-step non-atomic update, self-anonymize permitted, lax avatar
PUT format check) are documented in carry-overs FB-58-01 / FB-58-02 / FB-58-03
and acceptable at MVP. No new CRITICAL or HIGH issues; the diff is admin-gated,
parameterised, audit-emitting, and respects audit immutability.

## Findings

### [INFO] Defense-in-depth path-segment validator added

- **CWE / OWASP:** CWE-22 (Path Traversal) — defensive control
- **Datei:** `gateway/internal/compliance/user_anonymization.go:205-231`
- **Beschreibung:** `parseMxcURI` rejects malformed and traversing URIs;
  `isSafePathSegment` blocks `.`, `..`, embedded separators (`/`, `\`) and NUL
  bytes. Six sub-tests in `TestAnonymizeUser_PathTraversalMxc_Skipped`
  (`mxc://../media/file`, `mxc://server/../etc/passwd`,
  `mxc://./media/file`, slash-in-mediaID, backslash, NUL byte) verify both the
  parser refusal and the absence of any `FileRemover.Remove` invocation.
- **Impact:** Closes the Gate-3 MAJOR finding. Production attack path requires
  upstream `PUT /profile/{userId}/avatar_url` to also accept malformed URIs
  (FB-58-02); even then, the gateway will not delete arbitrary files.
- **Empfehlung:** Track FB-58-02 as the proper root-cause fix —
  `PutAvatarURL` should run the same `parseMxcURI` validation before
  persisting, not just check the `mxc://` prefix.
- **Referenz:** OWASP ASVS V12.3 (File Resource Validation), CWE-22.

### [INFO] StoragePath unset → disk removal explicitly skipped

- **CWE / OWASP:** CWE-23 (Relative Path Traversal) — defensive control
- **Datei:** `gateway/internal/compliance/user_anonymization.go:159-161`
- **Beschreibung:** When `NEBU_MEDIA_STORAGE_PATH` is empty, the handler logs a
  warning and skips `os.Remove` entirely instead of resolving against the
  process cwd. This prevents accidental deletion under unexpected working
  directories in dev/test deployments.
- **Impact:** No exploit path; hardens defense.
- **Empfehlung:** None. Production deployments must set the env var (verify
  via Compose / Helm chart).

### [INFO] Idempotent re-anonymize converges, double audit accepted

- **Datei:** `gateway/internal/compliance/user_anonymization.go:115-179`
- **Beschreibung:** Repeated calls update already-anonymised values to the same
  values and emit a second `user_anonymized` audit. Acceptable for 5-8 — the
  audit row records each admin action even when state is unchanged. Distinct
  from 5-7 where double-audit was problematic because the second call signalled
  no-op.
- **Empfehlung:** None.

### [INFO] Multi-step non-atomic anonymise (FB-58-01, deferred)

- **CWE / OWASP:** CWE-662 (Improper Synchronization)
- **Datei:** `gateway/internal/compliance/user_anonymization.go:115-170`
- **Beschreibung:** `UPDATE profiles` then `UPDATE users` then `UPDATE
  media_files` in three separate statements without a transaction. Failure
  between steps leaves a half-anonymised state (e.g. profile cleared, but
  `users.anonymized_at` not yet set).
- **Impact:** No data leak; idempotent retry repairs state. FB-58-01 already
  tracks this for hardening.
- **Empfehlung:** Wrap steps 4-6a in `BEGIN/COMMIT` in a follow-up.

### [INFO] Self-anonymize permitted (FB-58-03, deferred)

- **CWE / OWASP:** CWE-840 (Business Logic Errors)
- **Beschreibung:** An instance admin can anonymise their own user-id, locking
  themselves out of further admin actions. Audit row lands but lists the
  caller as the actor on a now-anonymised account.
- **Impact:** Pragmatic for an admin exercising GDPR self-deletion. Not a
  privilege escalation. FB-58-03 deferred.
- **Empfehlung:** Optional self-anonymize guard or two-admin requirement.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ |
| Audit-log immutability                      | ✅ |
| `instance_admin` notification (if in-scope) | ✅ |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ |
| Matrix Power Level checks                   | ✅ |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ |
| AES-256-GCM correctness                     | ✅ |
| Ed25519 verify-before-accept                | ✅ |
| No secrets in logs / error messages         | ✅ |

Notes:
- `reason` field: Story 5.8 spec explicitly omits a `reason` body — this is an
  admin DSGVO action with the audit row capturing actor + target. Consistent
  with 5.7 pattern.
- Audit-log immutability: handler only emits via `auditpkg.LogEvent`; no
  migration touches audit grants.
- OIDC: route is wired through `jwtMiddleware` in `main.go:775` — verified.
- `media_files.deleted` filter: only one read path (`pgMediaStore.GetMediaFile`
  in `media/cmd/media/main.go:42`) consumes this column — query updated to
  `AND NOT deleted`. No other code reads `media_files` rows.
- AC4 events-untouched: `TestEventsUnchanged_AfterAnonymize` captures all
  exec'd queries and asserts no `UPDATE`/`DELETE events` is issued.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 0 |
| INFO      | 5 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The Gate-3 MAJOR (path traversal via `mxc://` URI feeding `os.Remove`) is
properly closed by `isSafePathSegment` plus six dedicated traversal sub-tests.
Carry-overs FB-58-01 (atomicity), FB-58-02 (avatar-URL upstream validation),
FB-58-03 (self-anonymize) remain as documented follow-ups; none are exploitable
through the new admin-only route as it stands.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
