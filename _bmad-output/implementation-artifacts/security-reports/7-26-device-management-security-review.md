# Security Review — Story 7-26: Device Management API

**Reviewer:** Kassandra (SEC Gate 1)
**Date:** 2026-04-30
**Scope:** `git diff --staged` for story 7-26 (`_bmad-output/implementation-artifacts/7-26-device-management-get-put-delete-devices.md`)
**Classification:** **CLEAN** (no CRITICAL, no HIGH; 2 LOW, 2 INFO)

---

## Summary

Five Matrix device endpoints (`GET/GET-by-id/PUT/DELETE /devices`, `POST /delete_devices`) plus a reusable UIA module (`m.login.sso` only) and migration 000030 (nullable `device_display_name TEXT` on `sessions`).

The implementation gets the things that usually go wrong right:

- **IDOR closed at the SQL layer.** Every device query in `gateway/internal/db/device_store.go` (lines 33-39, 69-74, 96-105, 126-128, 156-159) carries `WHERE user_id = $1`. UPDATE/DELETE check `RowsAffected() == 0` to translate cross-user attempts into `ErrDeviceNotFound` instead of leaking existence — the test `TestGetDevice_IDORProtection_Returns404` (devices_test.go:347) and `TestPutDevice_IDORProtection_Returns404` (line 445) lock this in.
- **UIA session-to-user binding enforced.** `uiaSessionStore.check` (uia.go:112-132) rejects on missing/expired/wrong-user/incomplete and **consumes the session on success** (line 130: `delete(s.sessions, sessionID)`) — single-use, no replay. Cross-user reuse is covered by `TestDeleteDevice_UIASessionFromDifferentUser_Returns401` (line 667). This squarely addresses the focus area "can a UIA session be hijacked".
- **Migration 000030 is benign.** `ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device_display_name TEXT` (no DEFAULT, nullable, no GRANT change, no policy change). No RLS bypass — the `sessions` table never had RLS to begin with (verified: no `ROW LEVEL SECURITY` clause in any migration touching `sessions`). Down-migration is idempotent.
- **Parametrized queries throughout.** All five DB methods use `$1`/`$2`/`$3` placeholders — no string concatenation, no SQL injection vector even with attacker-controlled `deviceID` values.
- **Body limits in place.** `bodyLimit1MiB` wraps PUT/DELETE/POST on the device routes (main.go:500-505).
- **JWT middleware on all routes.** Confirmed at main.go:496-505.

The two known MVP risks are documented in the story's review notes (token not synchronously invalidated, in-memory UIA store on a single instance) — both reviewed below for completeness.

---

## Findings

### LOW-1 — `POST /delete_devices` performs N sequential `tx.ExecContext` round-trips inside a transaction

**File:** `gateway/internal/db/device_store.go:145-170`
**CWE:** CWE-400 (Resource Exhaustion)
**Frameworks:** OWASP API4:2023 (Unrestricted Resource Consumption)

The 1 MiB body cap allows an attacker to submit ~50,000 device IDs in one request. `DeleteDevices` opens a transaction, then loops one `DELETE` per ID — that's ~50k round-trips holding `RowExclusiveLock` on `sessions` for the duration. Authenticated, single-user — the impact is bounded (the user can only contend with their own subsequent operations on `sessions`), but a malicious client can keep a connection busy for tens of seconds and the `sessions` table is on the hot path of `/sync` recovery.

**Evidence path:** `for _, deviceID := range deviceIDs { tx.ExecContext(...) }` — no `len(deviceIDs)` upper bound, no batched `WHERE device_id = ANY($2)`.

**Recommendation (non-blocking):** Either (a) cap `len(req.Devices)` at e.g. 100 in `DeleteDevices` handler with `M_TOO_LARGE`, or (b) collapse the loop into one `DELETE FROM sessions WHERE user_id = $1 AND device_id = ANY($2::text[])`. Option (b) also makes the "atomic" claim in the doc actually meaningful at SQL level.

---

### LOW-2 — UIA `auth.type` field is read but never validated

**File:** `gateway/internal/matrix/uia.go:222-231`
**CWE:** CWE-20 (Improper Input Validation)
**Frameworks:** OWASP ASVS V11.1 (Business Logic)

`checkUIACompleted` extracts `authBody.Auth.Type` (uia.go:168) but only consults `authBody.Auth.Session`. A client can send `{"auth":{"type":"anything","session":"<valid-uuid>"}}` and the handler accepts it.

In the current implementation this is **defense-in-depth, not exploitable**: the only path that flips `completed=true` for any session is `approveUIASession`/`complete`, both of which today are reachable only from the SSO callback (when implemented) and from tests in the same package. So an attacker cannot craft a "fake type" that bypasses SSO. But the spec declares the only stage as `m.login.sso`, and silently accepting other types makes future stages (e.g., `m.login.password` if ever added) instantly reusable in unintended places.

**Recommendation (non-blocking):** Add `if authBody.Auth.Type != "m.login.sso" { writeUIAChallenge(...); return false, bodyBuf }` before the session check.

---

### INFO-1 — UIA store has no production wiring to the SSO callback

**File:** `gateway/internal/matrix/uia.go:134-149` (`approveUIASession`)
**Status:** Already noted in story header (Code Review finding: "did claim not currently issued by Dex — own-device protection partially untested in real flow"). Out-of-scope for security severity — this is a feature-completeness gap.

`approveUIASession` is package-private and called only from `devices_test.go`. `grep -rn` across `gateway/` shows zero callers in production code (no SSO callback wiring). Practical effect: in production, `DELETE /devices/{id}` and `POST /delete_devices` will return 401 forever because no path completes the session. **This is not a security vulnerability** (it is fail-closed: the more-privileged operation is never reachable without the auth step). Recording it here so the next story explicitly wires the OIDC callback to `complete()` rather than re-discovering the gap.

---

### INFO-2 — In-memory UIA store does not survive restart and is not cluster-shared

**File:** `gateway/internal/matrix/uia.go:47-49` (`var uiaStore = &uiaSessionStore{...}`)
**Status:** Accepted MVP risk per story implementation notes ("Session state is in-memory: acceptable for MVP. Phase 2: persistent store.") and CLAUDE.md context (single-instance MVP).

Documented for the audit trail. In a horizontally scaled deployment the UIA challenge issued by gateway A cannot be completed against gateway B — a usability bug, not a security one. There is no information leak: the session map is never serialized or transmitted.

---

## Out-of-Scope Risks Verified (No Finding)

- **SQL injection:** All five DB methods use parametrized queries. ✓
- **XSS:** No HTML output in this story (JSON-only API). ✓
- **CSRF on state-changing endpoints:** Bearer-token authenticated JSON API; CSRF does not apply (no cookie-based auth, no implicit credentials). ✓
- **Auth bypass:** All five routes wrapped in `jwtMiddleware` (main.go:496-505). ✓
- **Timing attacks on secret comparison:** UIA session lookup is a Go `map` access — constant-time relative to value but not relative to key length; session IDs are 16-byte hex from `crypto/rand` (uia.go:57-63), so an attacker has no way to learn anything from timing. ✓
- **Open redirects:** No redirects in this story. ✓
- **Missing body-size limits:** `bodyLimit1MiB` on PUT/DELETE/POST (main.go:500-505). GET endpoints don't read bodies. ✓
- **Missing rate limits:** No per-route limiter, but consistent with the rest of `/_matrix/client/v3/*` (the global pattern, comment at main.go:827). Not a regression introduced by this story.
- **Weak crypto:** `crypto/rand` for session IDs (uia.go:59), no MD5/SHA1/DES. ✓
- **Plaintext secrets in logs:** No `log.Print` of tokens, session IDs, or display names in the new code. The middleware error path at `auth.go:122` logs `err` from `idToken.Claims`, which never contains the token itself. ✓
- **Missing security headers:** Inherits from gateway's global headers; not modified by this story.
- **Path traversal:** `r.PathValue("deviceId")` is passed directly to parametrized SQL — no filesystem path operations involved.
- **JWT validation flaws:** The `did` claim is read but never trusted for authorization decisions (it is only used to identify the *caller's own* device in the M_FORBIDDEN check at devices.go:258). Forging the `did` claim would require forging the full JWT, which `JWTMiddleware` (auth.go:96-118) prevents via OIDC verifier with `SupportedSigningAlgs` whitelist + denylist check. ✓

---

## Severity Counts

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 2 |
| INFO | 2 |

## Verdict

**CLEAN — clear to merge.**

Neither LOW finding blocks the commit. LOW-1 (bulk-delete amplification) and LOW-2 (missing `auth.type` check) should be picked up as small follow-ups; both are defense-in-depth improvements to an implementation that already gets the harder parts right (IDOR, UIA single-use, session-to-user binding, parametrized SQL).
