# Security Review — Story 5.29b (Compliance Endpoint Hardening) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` — 17 files, ~1950 insertions / 43 deletions
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

Story 5.29b closes eight per-story SEC Gate 1 findings (FB-53-01/03, FB-54-01, FB-56-01, FB-57-01, FB-58-01/02/03) without introducing new security regressions. Rate-limit coverage is now uniform across the nine compliance/admin routes, the `time_range` cap and `note` length cap are enforced before DB write, the export TOCTOU window on `compliance_requests.status` is closed with a re-read in pre-flight, the deletion-status transition is now race-free via `UPDATE … RETURNING`, and the `AnonymizeUser` profiles/users/media_files steps run inside one transaction with rollback on every error path. Avatar URL validation rejects unsafe `mxc://` segments at write time, and migration 000026 scrubs already-stored unsafe rows. Self-anonymize is blocked. Carry-over deferrals (FB-29c-1..4) are explicitly out of scope per the brief.

The pre-review concern — that the `mxc://` validator might miss URL-encoded traversal (`%2e%2e`) — was investigated. `gateway/internal/compliance/user_anonymization.go:194` builds the disk path with `filepath.Join(StoragePath, mxcServerName, mxcMediaID)` and calls `os.Remove` without any URL-decoding step in between. `filepath.Join` treats `%2e%2e` as a literal seven-character segment, not as `..`. No other consumer of `profiles.avatar_url` performs path-decoding before filesystem access. The vector is not exploitable in this codebase. No MAJOR finding.

## Findings

### [INFO] Avatar URL: URL-encoded traversal vector (`%2e%2e`) is not exploitable

- **Datei:** `gateway/internal/matrix/profile.go:228-236`, `gateway/internal/compliance/user_anonymization.go:194-199`
- **Beschreibung:** `isSafeMxcSegment` rejects literal `..`, `.`, `/`, `\`, NUL but does not URL-decode `s` first. A stored value of `mxc://server.ex/%2e%2e` would pass validation. However, the only filesystem consumer (`AnonymizeUser` step 6b) joins the segment with `filepath.Join` and passes the result to `os.Remove` without any prior `url.PathUnescape`. The seven-byte literal `%2e%2e` does not collapse to `..` in `filepath.Clean`, so no traversal occurs. If a future consumer adds URL-decoding before path use, this assumption breaks — record this invariant in `parseMxcURI`'s contract and consider an explicit reject of `%` in the segment as defence-in-depth.
- **Empfehlung:** Optional follow-up. Either add `strings.ContainsRune(s, '%')` to `isSafeMxcSegment` (rejecting any percent-encoded segments, since canonical `mxc://` mediaIDs are `[A-Za-z0-9_-]+`), or document explicitly in `parseMxcURI` that callers must not URL-decode before path use.

### [INFO] Rate-limit `strictRL` is per-IP only

- **Datei:** `gateway/cmd/gateway/main.go:751-870`
- **Beschreibung:** All nine compliance/admin routes are now wrapped in `strictRL` (5 req/min/IP, burst 3). An attacker rotating source IPs (botnet, residential proxies) can bypass the budget. Mitigation by per-`sub` rate-limit is a known follow-up; not blocking for the eight findings this story closes. Documented for the threat model.
- **Empfehlung:** Track as follow-up. Adding a per-JWT-`sub` token bucket to `strictRL` for authenticated routes is straightforward and would cap a single compromised officer account regardless of IP rotation.

### [INFO] Avatar scrub migration may set legitimate rows to NULL

- **Datei:** `gateway/migrations/000026_avatar_url_scrub.up.sql:14-27`
- **Beschreibung:** The `LIKE '%..%'` predicate is greedy — any mediaID that happens to contain two consecutive dots in succession is scrubbed to NULL even when traversal is impossible (e.g. a future random-mediaID generator that allows `.` is in the alphabet). False-positive risk only; NULL is the safe default and the user can re-set their avatar.

### [INFO] `BeginTx` has no `defer tx.Rollback()` safety net

- **Datei:** `gateway/internal/compliance/user_anonymization.go:128-185`
- **Beschreibung:** Every error path inside the TX explicitly calls `_ = tx.Rollback()` before returning. Idiomatic Go often adds `defer tx.Rollback()` immediately after `BeginTx` as a belt-and-suspenders safety net (rollback after commit is a no-op). The current code is correct against panics in callees because `database/sql.Tx` aborts on any unhandled error, and the surrounding handler does not panic — so this is hygiene, not a finding.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no new compliance tables) |
| `reason` field on compliance access         | ✅ (no new compliance-data read paths) |
| Audit-log immutability                      | ✅ (no audit-table changes) |
| `instance_admin` notification               | n/a |
| OIDC token validation (iss/aud/exp)         | ✅ (no JWT changes) |
| Matrix Power Level checks                   | n/a (no room mutations) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | n/a |
| AES-256-GCM correctness                     | n/a |
| Ed25519 verify-before-accept                | n/a |
| No secrets in logs / error messages         | ✅ (slog.Warn lines log media_id and disk path — no PII, no secrets) |
| Self-anonymize blocked (FB-58-03)           | ✅ (`gateway/internal/compliance/user_anonymization.go:101-105`) |
| Atomic deletion-status transition (FB-57-01)| ✅ (`core/apps/compliance/lib/compliance/user_deletion.ex:138-156`) |
| Anonymize multi-step TX (FB-58-01)          | ✅ (`gateway/internal/compliance/user_anonymization.go:128-185`) |
| Export status TOCTOU re-check (FB-56-01)    | ✅ (`gateway/internal/compliance/handler.go:725-742`) |
| Path-traversal guard on avatar URL (FB-58-02)| ✅ (literal-byte check; URL-encoding not exploitable in current consumers) |
| Rate-limit on compliance/admin routes       | ✅ (9/9 wrapped in `strictRL`) |
| Time-range cap (FB-53-03)                   | ✅ (365 d window, 7 y horizon) |
| `note` length cap (FB-54-01)                | ✅ (4096 chars) |

## Carry-overs (out of scope, not re-flagged)

- FB-51-01/02 — closed by 5.29a.
- FB-52-01 — closed by 5.29a.
- FB-E5-04/05/06/07, FB-55-01 — closed by 5.29c.
- FB-29c-1..4 — explicitly deferred to 5.29d.

## Severity Counters

- CRITICAL: 0
- HIGH: 0
- MEDIUM: 0
- LOW: 0
- INFO: 4

## Decision

CLEAN. No CRITICAL/HIGH/MEDIUM findings. Pipeline proceeds. Two optional follow-ups noted (per-`sub` rate-limit, explicit `%`-byte rejection in `mxc://` segments) — neither is blocking and both can be tracked in the next 5-29 sub-story or epic-end review if `bmad-pipeline` chooses.
