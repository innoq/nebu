---
security_review: required
---

# Story 5.29: Matrix Validator & JSON-Hardening Roll-out to Remaining Handlers

Status: ready-for-dev

## Story

As a security-conscious operator,
I want the validators (`ValidateMatrixRoomID`, `ValidateMatrixUserID`, `ValidateMatrixEventID`),
the `requireJSON` helper, and `DisallowUnknownFields` consistently applied across **all**
Matrix handlers that accept path params or JSON bodies,
so that the boundary hardening introduced in 5-27 actually covers the whole Matrix API surface — not just the three handlers touched during that bundle.

---

## Background / Motivation

Story 5-27 ("Matrix Path Parameter Validation + Minor-Finding Bundle") introduced three Matrix-ID validators, a `requireJSON` helper and `DisallowUnknownFields` — but only wired them into the three handlers that received new code in that story (`GetRoomMessages`, `PostCreateRoom`, `PutPresenceStatus`). The code review (2026-04-23) flagged this as MAJOR-B: AC2/AC3/AC4 of 5-27 were written as *"all handlers"* but the implementation stopped at the three new ones. Scope of 5-27 was reduced to match the actual diff; this follow-up story closes the gap.

Reason for the split: extending the validators to remaining handlers is non-trivial — it can break existing Matrix clients (FluffyChat, Element) if the regex is stricter than what they send. Each handler needs its own failing ATDD test (happy + malformed path), and the interaction with existing integration tests must be checked. That warrants a dedicated pipeline pass.

---

## Acceptance Criteria

1. **Roll-out of `ValidateMatrixRoomID`** to every handler that accepts a `roomId` / `roomIdOrAlias` path param before gRPC/DB:
   - `PostJoinRoom` (`POST /join/{roomIdOrAlias}` — alias vs. roomId branching kept)
   - `PostJoinRoomById` (`POST /rooms/{roomId}/join`)
   - `PostInviteUser` (`POST /rooms/{roomId}/invite`)
   - `PutSetRoomState` (`PUT /rooms/{roomId}/state/{eventType}` + variant with stateKey)
   - `PutSendEvent` (`PUT /rooms/{roomId}/send/{eventType}/{txnId}`)
   - `PostReadMarkers` (`POST /rooms/{roomId}/read_markers`)
   - `PutTyping` (`PUT /rooms/{roomId}/typing/{userId}`)
   - Invalid → 400 `M_INVALID_PARAM` with current body shape.

2. **Roll-out of `ValidateMatrixUserID`** to every handler with a `userId` path param:
   - `GetProfile`, `PutDisplayname`, `PutAvatarURL` (`profile.go`)
   - `GetPresenceStatus` (`presence.go`) — unauthenticated endpoint, MUST still validate format.
   - `PutTyping` (`userId` component — should equal authenticated user; AC8).

3. **Roll-out of `ValidateMatrixEventID` / `eventType`** where the Matrix spec defines a reverse-DNS event-type format (`m.room.*`, `m.presence`, `com.example.*`):
   - `PutSendEvent` and `PutSetRoomState` accept `eventType`. Reject empty and reject values > 255 bytes (Matrix spec limit). No strict reverse-DNS regex (breaks Element custom types) — just length + non-empty + no control chars.
   - Add `ValidateMatrixEventType(s) error` in `validate.go` (new validator).

4. **`requireJSON` roll-out** to every JSON-body handler: `PostLogin`, `PostInviteUser`, `PutSetRoomState`, `PutSendEvent`, `PutDisplayname`, `PutAvatarURL`, `PostReadMarkers`, `PutTyping`, `user_directory.Search`.

5. **`DisallowUnknownFields`** roll-out to every typed-struct decoder:
   - `login.go:PostLogin` (`LoginRequest`)
   - `profile.go:PutDisplayname` (displayname body)
   - `profile.go:PutAvatarURL` (avatar_url body)
   - `presence.go:PutPresenceStatus` (already done in 5-27 — verify)
   - `user_directory.go:Search` (search_term/limit)
   - `typing.go:PutTyping` (typingRequestBody)
   - Unknown field → 400 `M_BAD_JSON`.

6. `PutTyping` enforces `path userId == authenticated userID` analogous to 5-27 AC5 for presence. Mismatch → 403 `M_FORBIDDEN`.

7. **Backward compatibility regression check:** FluffyChat smoke test (manual or Playwright scripted) still logs in, joins a room, sends a message, sets presence, sets typing, changes displayname, changes avatar — all green against the hardened gateway.

8. **No new gRPC surface is introduced.** All roll-out is gateway-side only.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestAllRoomHandlers_RejectInvalidRoomID` — parameterized table across the 7 handlers from AC1; each returns 400 `M_INVALID_PARAM` for a malformed roomId (e.g. `"not-a-room"`).

2. `TestAllUserHandlers_RejectInvalidUserID` — parameterized table across the handlers from AC2; each returns 400 `M_INVALID_PARAM` for a malformed userId.

3. `TestEventType_RejectsOverlong` — `PUT /rooms/{roomId}/send/{eventType}/{txnId}` with 256-byte eventType → 400; with empty eventType → 400.

4. `TestAllJsonHandlers_RejectFormEncoded` — parameterized table: every handler in AC4 rejects `application/x-www-form-urlencoded` with 415.

5. `TestAllTypedStructHandlers_RejectUnknownField` — parameterized table: every handler in AC5 rejects `{"sneaky": "..."}` in its typed body with 400 `M_BAD_JSON`.

6. `TestTyping_RejectsUserMismatch` — authenticated `@alice:test`, `PUT /rooms/!r:test/typing/@bob:test` → 403 (AC6).

7. `TestTyping_HappyPath` — self-typing returns 200 `{}` (regression guard for AC6).

8. **Playwright smoke test** `e2e/features/fluffychat-compat-after-5-29.spec.ts` — covers AC7: login → join → send → presence → typing → displayname → avatar against real gateway.

---

## Implementation Notes

- Validators and helpers are already in `gateway/internal/matrix/validate.go` from 5-27 — only call-sites are added.
- Use table-driven tests where natural to avoid copy-pasted boilerplate (TEA 2nd Nebu retrospective feedback).
- `eventType`-validator is new — keep permissive per Matrix spec (no reverse-DNS enforcement).
- Watch out for `PostJoinRoom`: path param is `roomIdOrAlias`. Alias format is `#localpart:domain` — needs a separate check or a combined `ValidateMatrixRoomIdentifier` that accepts both.
- FluffyChat regression test: document the exact FluffyChat version used in the test run (see Epic 4 retro — FluffyChat gap).
- Size: **"M"** — 10 handlers, ~120–150 LOC implementation + ~200 LOC tests.

---

## Dependencies

- **Blocks:** Story 5-28 (Epic-5 security gate) should run *after* this rollout to get a clean pass.
- **Depends on:** Story 5-27 (which introduced the validators + helpers).

---

## Change Log

- 2026-04-23: Story created as follow-up to Story 5-27 code-review MAJOR-B (scope reduction).
