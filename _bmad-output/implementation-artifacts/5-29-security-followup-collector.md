---
security_review: required
---

# Story 5.29: Security Follow-up Collector — Deferred Findings from Epic 5

Status: ready-for-dev (living document — new blocks appended during Epic 5 pipeline runs)

## Story

As a security-conscious operator,
I want a single, tracked story that collects every security finding from Epic 5 that was
intentionally deferred from its source story (because it was out of scope, too complex to fix
in-diff, or cross-cutting across multiple handlers),
so that nothing falls through the cracks and each deferred finding has a clear place to be
fixed — either here, or by being split into its own story when the scope grows too large.

---

## Background / Motivation

This story is the **collector/umbrella** for deferred security work during Epic 5. It was
originally created (2026-04-23) from Story 5-27's code-review MAJOR-B (scope reduction:
the Matrix validator roll-out to remaining handlers was too broad to safely land in 5-27).

**Collector pattern:**
Each of the remaining Epic 5 stories (5-1 … 5-9, plus the epic-wide gate 5-28) may surface
MAJOR-severity findings in the TEA / Code-Review / Kassandra gates that are either
(a) too complex to fix in-diff without breaking the source story's coherence, or
(b) cross-cutting concerns that belong together rather than scattered across commits.

When the pipeline for story **5-X** encounters such a finding:
1. The code-reviewer or Kassandra proposes the finding as a **new block** in this document
   (append to "Finding Blocks" below, with a unique ID like `FB-5X-01`).
2. The source story keeps its commit moving with a clear deferral note.
3. The pipeline picks up 5-29 **after 5-28 completes** (the epic-wide gate may add further
   blocks).
4. If a single block becomes too large (> Size M or > 200 LOC impact), it gets **split out
   into its own follow-up story** (5-30, 5-31, …) during the dev pass.

---

## Finding Blocks

### FB-527-01 — Matrix Validator & JSON-Hardening Roll-out to Remaining Handlers

**Source:** Story 5-27 code-review MAJOR-B (2026-04-23).
**Severity:** MAJOR (scope gap against AC2/AC3/AC4 as originally worded).
**Size estimate:** M (≈120–150 LOC impl + ≈200 LOC tests, ~10 handlers).

**What to do:**

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

3. **Roll-out of `ValidateMatrixEventID` / `eventType`**:
   - `PutSendEvent` and `PutSetRoomState` accept `eventType`. Reject empty and reject values > 255 bytes (Matrix spec limit). No strict reverse-DNS regex (breaks Element custom types) — just length + non-empty + no control chars.
   - Add `ValidateMatrixEventType(s) error` in `validate.go` (new validator).

4. **`requireJSON` roll-out** to every JSON-body handler: `PostLogin`, `PostInviteUser`, `PutSetRoomState`, `PutSendEvent`, `PutDisplayname`, `PutAvatarURL`, `PostReadMarkers`, `PutTyping`, `user_directory.Search`.

5. **`DisallowUnknownFields`** roll-out to every typed-struct decoder:
   - `login.go:PostLogin` (`LoginRequest`)
   - `profile.go:PutDisplayname` (displayname body)
   - `profile.go:PutAvatarURL` (avatar_url body)
   - `user_directory.go:Search` (search_term/limit)
   - `typing.go:PutTyping` (typingRequestBody)
   - Unknown field → 400 `M_BAD_JSON`.

6. `PutTyping` enforces `path userId == authenticated userID` analogous to 5-27 AC5 for presence. Mismatch → 403 `M_FORBIDDEN`.

7. **Backward compatibility regression check:** FluffyChat smoke test (Playwright scripted) still logs in, joins a room, sends a message, sets presence, sets typing, changes displayname, changes avatar.

**Tests (ATDD first):**
- `TestAllRoomHandlers_RejectInvalidRoomID` (parameterized table over 7 handlers).
- `TestAllUserHandlers_RejectInvalidUserID` (parameterized table over 5 handlers).
- `TestEventType_RejectsOverlong` + `TestEventType_RejectsEmpty`.
- `TestAllJsonHandlers_RejectFormEncoded` (parameterized).
- `TestAllTypedStructHandlers_RejectUnknownField` (parameterized).
- `TestTyping_RejectsUserMismatch` + `TestTyping_HappyPath`.
- Playwright `e2e/features/fluffychat-compat-after-5-29.spec.ts`.

---

<!--
  Appending pattern (for pipeline runs of 5-1 through 5-9 and 5-28):

  ### FB-{storyId}-{NN} — {short title}

  **Source:** Story 5-X {gate: code-review | kassandra | TEA} (date).
  **Severity:** {MAJOR|HIGH|CRITICAL}.
  **Size estimate:** {S|M|L}.

  **What to do:** …
  **Tests (ATDD first):** …
  **Why deferred (instead of fixed in source story):** …
-->

---

## Acceptance Criteria (for when 5-29 itself enters the pipeline)

1. Every `FB-*` block in this document is either:
   (a) fully addressed (code + tests + green pipeline gates), or
   (b) split into its own story (5-30, 5-31, …) with this document updated to reference the split story.

2. No `FB-*` block may be silently dropped — dropping requires an explicit `**Accepted as risk:** …` note with justification, signed by the instance admin.

3. Each `FB-*` block with size L or complexity exceeding M (per Nebu T-shirt sizing) MUST be split out rather than landed in 5-29's commit.

4. After landing, `make test-unit-go` and `make test-integration` both exit 0. Playwright smoke (FB-527-01 only) exits 0.

5. Kassandra re-runs on the 5-29 diff (SEC Gate 1) and reports CLEAN or MEDIUM-only.

---

## Acceptance Tests

(Tests per `FB-*` block — see each block's "Tests (ATDD first)" section.)

---

## Implementation Notes

- This story is a **living document**. The dev pass reads all blocks at the time of pickup
  (not at story creation). The pipeline may append blocks between story creation and dev.
- When splitting a block, update the block to read: `**Split into Story 5-XX** — see {link}`.
- Use table-driven tests where natural to avoid copy-pasted boilerplate.
- Validators and helpers already exist in `gateway/internal/matrix/validate.go` from 5-27 —
  only call-sites are added.
- Size estimate for the whole collector: **L** if all blocks land here; **M** if FB-527-01
  is the only block; larger blocks should split out.

---

## Dependencies

- **Blocked by:** Stories 5-1 through 5-9 must complete their pipeline first (so all their
  deferred findings are captured as blocks).
- **Blocked by:** Story 5-28 (Epic-5 security gate) must complete first (so any cross-cutting
  findings from the epic-wide scan are captured).
- **Blocks:** None — 5-29 is the last substantive story in Epic 5 before retrospective.

---

## Change Log

- 2026-04-23: Story created as follow-up of 5-27 code-review MAJOR-B (initial scope: Matrix validator roll-out, now block `FB-527-01`).
- 2026-04-23: Reframed as **Security Follow-up Collector** — living document for deferred findings across Epic 5 stories 5-1 through 5-9 and the 5-28 epic gate. Pattern documented (append `FB-{storyId}-{NN}` blocks; split into new story if > Size M).
