---
security_review: required
---

# Story 5.27: Matrix Path Parameter Validation + Minor-Finding Bundle

Status: ready-for-dev

## Story

As a security-conscious operator,
I want every Matrix path parameter (`roomId`, `userId`, `eventId`, `eventType`) validated against its Matrix-ID format before being passed to gRPC or the DB,
and the remaining minor findings from the security audit bundled into one coherent change,
so that malformed path values are rejected at the gateway boundary and log-injection / over-length footguns are closed.

---

## Background / Motivation

Security audit (2026-04-20) MINOR findings (Matrix m1–m6) — all individually low-impact, combine well into one hardening story:

- m1: No `Content-Type: application/json` check on JSON handlers
- m2: Path params (`roomId`, `userId`, `eventId`) passed to Core without format validation
- m3 (info only): IDOR protection delegated entirely to Core — add one-line regression test
- m4: `GET /profile/{userId}` unauthenticated, no rate limit (partially covered by 5-21); add response flattening for user-enumeration oracle
- m5: `PUT /presence/{userId}/status` silently ignores path param `userId` instead of 403 on mismatch
- m6: `json.Decoder.DisallowUnknownFields()` nowhere set

Gateway `keys/changes` unauthenticated at `main.go:524` is a spec violation (included in this bundle).

---

## Acceptance Criteria

1. `gateway/internal/matrix/validate.go` adds three validators:
   - `ValidateMatrixRoomID(s) error` — `^![A-Za-z0-9._=-]{1,63}:[A-Za-z0-9.-]{1,255}$`
   - `ValidateMatrixUserID(s) error` — `^@[A-Za-z0-9._=/-]{1,63}:[A-Za-z0-9.-]{1,255}$`
   - `ValidateMatrixEventID(s) error` — `^\$[A-Za-z0-9+/=_-]{1,64}(?::[A-Za-z0-9.-]{1,255})?$` (rooms v3+ hash form or legacy)
   - All three cap the full string at 512 bytes before regex match.

2. All Matrix handlers that use path params call the appropriate validator before any gRPC or DB call. Invalid → 400 `M_INVALID_PARAM`.

3. `Content-Type: application/json` check helper `requireJSON(w, r) bool`; applied to every JSON handler. Rejects with 415 `M_UNSUPPORTED_MEDIA_TYPE` if wrong type.

4. `json.NewDecoder(r.Body).DisallowUnknownFields()` on every handler that uses a typed struct (not `map[string]any`).

5. `PUT /presence/{userId}/status` reads `r.PathValue("userId")`, compares to authenticated `userID`; mismatch → 403 `M_FORBIDDEN`.

6. `GET /profile/{userId}`: return identical `404 M_NOT_FOUND` for "user exists but no profile data" and "user does not exist" (no oracle); cache for 60s.

7. `keys/changes` endpoint wrapped with `JWTMiddleware` (spec-compliance fix).

8. One new integration test asserts gRPC `PermissionDenied` → 403 propagation (regression against m3).

9. Unit tests per validator (happy path + 3 malformed variants each).

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestValidateRoomID_Table` — 10+ malformed inputs rejected; 5 valid inputs accepted

2. `TestPresence_PUT_RejectsUserMismatch` — authenticated as `@alice:test`, PUT to `/presence/@bob:test/status` → 403

3. `TestProfile_Flattened404` — two requests, one for a user with no profile, one for a non-existent user, both return identical status + body

4. `TestContentType_RejectsFormEncoded` — POST `/createRoom` with `application/x-www-form-urlencoded` → 415

5. `TestDisallowUnknownFields_Rejects` — POST with extra field `"sneaky":"..."` in a typed-struct handler → 400

6. `TestKeysChanges_RequiresAuth` — no Bearer → 401

---

## Implementation Notes

- Validators in one shared file; keep regexes strict but not paranoid (Matrix spec is the source of truth, not a stricter local interpretation)
- `requireJSON` is a helper, not a middleware, to keep per-handler control
- IDOR regression test goes in `gateway/test/integration/idor_test.go`
- Size: "S" — bundle but logically coherent (all Matrix-handler boundary hardening)
