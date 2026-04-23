# Security Review — Story 5.27 (Matrix Path Param Validation + Minor Bundle) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (12 files, ~1014 insertions / 27 deletions)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=default`

## Executive Summary

Story 5.27 is a hardening change, and it hardens cleanly. The three Matrix-ID validators are ReDoS-free (linear-time with a 512-byte pre-cap), the `keys/changes` auth gap is closed without collateral, and the new `PutPresenceStatus` check and flattened `GetProfile` 404 are structurally correct. No CRITICAL or HIGH findings. Three MEDIUM notes on defense-in-depth and one LOW on log hygiene; all non-blocking.

## Findings

### [MEDIUM] `GetProfile` sets `Cache-Control: public, max-age=60` without a `Vary` header

- **CWE / OWASP:** CWE-524 / A04:2021 (Insecure Design — caching)
- **Datei:** `gateway/internal/matrix/profile.go:78, 86`
- **Beschreibung:** Both the 200-OK and the flattened 404 responses set `Cache-Control: public, max-age=60`. No `Vary` header is set. If a downstream shared cache (CDN, corporate proxy) ever keys requests by only path+host, a request carrying an `Authorization` header could poison the cache entry read by subsequent unauthenticated clients — or, less dramatically, the absence of `Vary: Accept-Encoding` can produce incorrect compression behaviour.
- **Impact:** Today no compliant Matrix client sends `Authorization` on the public `GET /profile/{userId}`, and the handler ignores it anyway — so the real-world exposure is low. The flattening of 404s (AC6) correctly neutralises the enumeration oracle for 60 s. A regression that starts varying the body by auth header — or a future CDN in front of the gateway — would reopen an information-leak surface.
- **Empfehlung:** Add `w.Header().Set("Vary", "Accept-Encoding")` (or at minimum document the invariant in a code comment) alongside the `Cache-Control` write, on both the 200 and the 404 paths. If any caller is ever expected to vary the response by `Authorization`, add that to `Vary` as well.
- **Referenz:** OWASP ASVS V8.3.4, RFC 9111 §4.1

### [MEDIUM] `PutPresenceStatus` uses non-constant-time string comparison for authorisation

- **CWE / OWASP:** CWE-208 / A01:2021 (Broken Access Control — timing)
- **Datei:** `gateway/internal/matrix/presence.go:58`
- **Beschreibung:** `pathUserID != authedUserID` is a standard byte comparison. The two values being compared are a path segment (attacker-controlled) and the authenticated user ID (from JWT claims). In principle a timing oracle could leak prefix information about the authenticated user ID.
- **Impact:** Low. The authenticated user ID is not a secret in this system — it is the `sub` claim returned in the access token, which the attacker already possesses as the token bearer. A timing attack against oneself has no privilege delta. Flagged only because constant-time comparison is the Nebu pattern elsewhere (see Story 5.22, PSK compare).
- **Empfehlung:** No change required for this story. If symmetry with the rest of the code base is desired, `subtle.ConstantTimeCompare([]byte(pathUserID), []byte(authedUserID)) == 1` is the drop-in. Not a blocker.
- **Referenz:** OWASP ASVS V2.1.14

### [MEDIUM] `DisallowUnknownFields` does not guard against duplicate JSON keys

- **CWE / OWASP:** CWE-20 / A03:2021 (Injection — parser discrepancy)
- **Datei:** `gateway/internal/matrix/rooms.go:74-76`
- **Beschreibung:** `json.Decoder.DisallowUnknownFields()` rejects unknown fields but silently accepts duplicate known fields — Go's `encoding/json` takes the **last** occurrence. If a different service in the stack (e.g. a logger, a WAF) parses the same body and takes the **first** occurrence, the two components may see different values (HTTP Parameter Pollution class).
- **Impact:** No concrete attack path exists in this diff — the gateway is the only consumer of the body, and `CreateRoomRequest` fields are not security-sensitive (room name/topic/etc.). Flagged because the same handler pattern will be rolled out to more sensitive handlers in Story 5-29 (`login.go`, `profile.go` displayname). If those handlers pass the decoded struct to anything that re-parses the raw bytes (audit log, compliance export), the duplicate-key discrepancy becomes real.
- **Empfehlung:** Document the "last-key-wins" behaviour in a comment alongside `DisallowUnknownFields()` so the 5-29 rollout does not assume stricter semantics. If future handlers require first-key-wins or strict-uniqueness, use a tokenising decoder (`json.Decoder.Token()`) or a schema validator.
- **Referenz:** OWASP ASVS V13.1.1, CWE-235

### [LOW] `createRoom` invite-fail log line includes invitee user ID in clear

- **CWE / OWASP:** CWE-532 / A09:2021 (Logging — sensitive data)
- **Datei:** `gateway/internal/matrix/rooms.go:120`
- **Beschreibung:** `slog.Warn("createRoom: invite failed", "room_id", resp.RoomId, "invitee", invitee, "err", invErr)` — not a secret, but the Matrix user ID is PII under the compliance invariant (see Epic-5 Story 5.8, operational PII anonymisation). Epic-5 explicitly redacts user IDs from operational logs.
- **Impact:** No token leakage. The invitee ID is already present in the inbound request body; logging it here is redundant information, not new exposure. Flagged only for alignment with Story 5.8 once that lands.
- **Empfehlung:** After Story 5.8 ships its log-redaction helper, apply it here. No change required today.
- **Referenz:** Nebu invariant — operational PII anonymisation (Epic 5)

### [INFO] Matrix-ID validators are ReDoS-free and length-capped before regex match

- **Datei:** `gateway/internal/matrix/validate.go:12-55`
- **Beschreibung:** All three regexes (`reRoomID`, `reUserID`, `reEventID`) are linear-time. They contain no nested quantifiers, no overlapping alternation, and no back-references. Go's `regexp` package uses RE2 semantics (no catastrophic backtracking by design), but even under a PCRE engine these patterns would be safe. The 512-byte cap before `MatchString` provides a second layer — worst-case input is bounded both in length and in state transitions.
- **Recommendation:** None. Positive finding, recorded for epic-end SEC Gate 2 trace.

### [INFO] `keys/changes` middleware chain correctly layered: `looseRL → jwtMiddleware → handler`

- **Datei:** `gateway/cmd/gateway/main.go:563`
- **Beschreibung:** Order is correct — rate limit first (cheap rejection of unauth flood), then JWT (expensive verification), then handler stub. Matches the pattern used on the sibling `keys/query` and `keys/claim` endpoints (lines 556, 568). `keys/claim` already had JWT; no regression introduced by the change to `keys/changes`. The spec-compliance gap noted in the story background is closed.
- **Recommendation:** None. Positive finding.

### [INFO] IDOR integration test (`idor_test.go`) uses real OIDC flow

- **Datei:** `gateway/test/integration/idor_test.go:45-90`
- **Beschreibung:** The test obtains sessions via the live Dex + Matrix `/login` flow (`kaiIsAuthenticated()`, `alexIsAuthenticated()`), not by forging bearer tokens or seeding DB sessions. Aligned with CLAUDE.md / Epic 3 retro — no test-integrity violation. The `integration` build tag correctly excludes it from unit runs.
- **Recommendation:** None. Positive finding.

### [INFO] `ValidateMatrixEventID` permissively accepts `+/=` (base64 standard padding)

- **Datei:** `gateway/internal/matrix/validate.go:15`
- **Beschreibung:** The regex allows `[A-Za-z0-9+/=_-]` for the hash-form localpart. Strict Matrix v3+ spec uses base64-url-unpadded (no `+/=`). The validator is therefore permissive — it accepts both base64-standard and base64-url. This is deliberate per the story's implementation note ("keep regexes strict but not paranoid") and does not introduce an injection vector: `+`, `/`, `=` are not SQL metacharacters, shell metacharacters, or path-traversal sequences, and they pass through gRPC metadata as inert strings.
- **Recommendation:** None. If future strict-spec compliance is required, tighten in a later story.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (not in scope) |
| `reason` field on compliance access         | ✅ (not in scope) |
| Audit-log immutability                      | ✅ (not in scope) |
| `instance_admin` notification (if in-scope) | ✅ (not in scope) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (unchanged; `keys/changes` now behind `jwtMiddleware`) |
| Matrix Power Level checks                   | ✅ (not in scope) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (not in scope) |
| AES-256-GCM correctness                     | ✅ (not in scope) |
| Ed25519 verify-before-accept                | ✅ (not in scope) |
| No secrets in logs / error messages         | ⚠️ — LOW finding on `createRoom` invite log (PII, not a secret) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 3 |
| LOW       | 1 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The three MEDIUM findings are defense-in-depth improvements with no concrete exploit path today. The LOW finding is a log-hygiene item that aligns naturally with Story 5.8 (operational PII anonymisation) and requires no action in this bundle. The author correctly deferred the broad validator rollout to Story 5-29, which is the right call — the current bundle's three reached handlers (`GetRoomMessages`, `PostCreateRoom`, `PutPresenceStatus`) are fully hardened.

Specifically verified against the caller's focus list:

1. **ReDoS.** RE2 semantics + 512-byte pre-cap → linear time. ✅
2. **Validator bypass via Unicode / URL-decoding stages.** Byte-level regex on already-URL-decoded path segments; character classes reject all non-ASCII. ✅
3. **404 oracle.** Identical status, body, and cache header on both cache-miss paths — verified by `TestProfile_Flattened404` asserting body equality. ✅
4. **`PutPresenceStatus` timing.** String comparison on non-secret sub — MEDIUM note above, not blocking. ⚠️
5. **`keys/changes` chain.** `looseRL → jwtMiddleware → handler` — correct order. ✅  No regression on `keys/claim` / `keys/query` (diff is additive). ✅
6. **IDOR mapping.** `PermissionDenied → 403`, `NotFound → 404`, `Unauthenticated` handled upstream by `jwtMiddleware → 401` — no code path conflates the three. ✅
7. **`DisallowUnknownFields`.** Duplicate-keys note → MEDIUM, not blocking. ⚠️
8. **Byte-cap vs UTF-8.** Character class is ASCII-only; `len` byte-count is correct and there is no path for overlong UTF-8 to pass the regex. ✅
9. **Profile cache & Vary.** Missing `Vary` → MEDIUM. ⚠️
10. **Secrets hygiene in tests.** IDOR test uses real OIDC flow, no forging. ✅
11. **`reEventID` permissiveness.** Documented in INFO finding. No security impact. ✅

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
