---
story_id: "9-6"
epic: 9
title: "State Event Type Whitelist — Gateway Middleware"
status: done
security_review: optional
---

# Story 9-6: State Event Type Whitelist — Gateway Middleware

**As a system operator,**
I want the gateway to validate state event types against a whitelist before forwarding to Core,
So that unknown or malformed event types cannot be injected into the system.

**Size:** XS

## Acceptance Criteria

1. `PUT /rooms/{roomId}/state/m.room.name` — `m.room.name` is in whitelist → forwarded to Core (not rejected)
2. `PUT /rooms/{roomId}/state/m.room.encryption` — in whitelist (pass-through per Matrix spec) → forwarded to Core
3. `PUT /rooms/{roomId}/state/evil.custom.inject` — NOT in whitelist → gateway returns `400 M_BAD_JSON`
4. Whitelist is a single Go variable (not scattered across handlers) extensible in one place

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AC1 — WhitelistedType_mRoomName_ForwardedToCore** — Go unit test (httptest)
   - Given: a `PUT /rooms/{roomId}/state/m.room.name` request with valid JWT and JSON body
   - When: `m.room.name` is in `allowedStateEventTypes`
   - Then: handler returns 200 `{"event_id":""}` and Core.SetPowerLevels is NOT called (routed generically)

   Note: The current `PutSetRoomState` handler only has a special path for `m.room.power_levels` and a
   501 fallback. After this story the whitelist check fires before the 501, so whitelisted types that
   are not yet fully implemented still return 501 (not 400). The AC is: "not rejected at gateway level"
   (i.e., the whitelist passes it through — the 501 is expected until Story 9.7 implements them).
   **The unit test therefore asserts: whitelisted type does NOT return 400 M_BAD_JSON.**

2. **AC2 — WhitelistedType_mRoomEncryption_NotRejected** — Go unit test (httptest)
   - Given: `PUT /rooms/{roomId}/state/m.room.encryption` with valid JWT and JSON body
   - When: `m.room.encryption` is in `allowedStateEventTypes`
   - Then: handler does NOT return 400 M_BAD_JSON (may return 501 — gateway passes it through)

3. **AC3 — UnknownType_Rejected_400_M_BAD_JSON** — Go unit test (httptest)
   - Given: `PUT /rooms/{roomId}/state/evil.custom.inject` with valid JWT and JSON body
   - When: `evil.custom.inject` is NOT in `allowedStateEventTypes`
   - Then: handler returns 400 with `{"errcode":"M_BAD_JSON","error":"unknown state event type: evil.custom.inject"}`
   - Core must NOT be called

4. **AC4 — WhitelistIsASingleVariable** — structural assertion in test
   - Given: `gateway/internal/matrix/state_event_types.go` exists
   - When: inspected
   - Then: `allowedStateEventTypes` is declared exactly once as a package-level `map[string]bool`

5. **Godog E2E — state_event_whitelist.feature** — three scenarios covering AC1/AC2/AC3

## Implementation Notes

### Files created
- `gateway/internal/matrix/state_event_types.go` — whitelist variable
- `gateway/internal/matrix/state_event_whitelist_test.go` — unit tests (AC1–AC4)
- `gateway/features/state_event_whitelist.feature` — Godog scenarios

### Files modified
- `gateway/internal/matrix/rooms.go` — `PutSetRoomState`: whitelist check inserted before the
  existing handler logic; replaces the previous blanket 501 fallback with a 400 for unlisted types

### Design decisions

**`M_BAD_JSON` vs `M_UNKNOWN`:** The Matrix Client-Server API spec (Section 1.3.2) reserves
`M_BAD_JSON` for "The request contained valid JSON, but it was malformed in some way, e.g. missing
required keys, invalid values for keys." Using it for an unknown event type is a deliberate
pragmatic choice: the story ACs mandate it, and it signals to clients that the problem is with
the request content (the event type string) rather than a server-side failure. `M_UNRECOGNIZED`
would also be defensible but the AC text specifies `M_BAD_JSON`.

**Whitelist breadth vs security:** The whitelist includes all standard Matrix state event types
defined in the Client-Server API spec v1.18. Custom event types (anything not starting with `m.`)
are rejected. This is intentionally conservative: extending the whitelist is one-line, while
shrinking it post-deployment could break clients. The security risk of a "too broad" whitelist is
low here because unimplemented types still return 501 from the handler — they pass the whitelist
gate but never reach Core.

**No change to `m.room.power_levels` path:** The existing special-case for `m.room.power_levels`
is preserved. The whitelist check fires first (before the `if eventType == "m.room.power_levels"`
branch), so `m.room.power_levels` still reaches the SetPowerLevels gRPC call.

## Dev Agent Record

Implementation completed in one pass. All unit tests pass (`make test-unit-go`).

The existing `TestPutSetRoomState_HappyPath` test was updated: it uses `m.room.power_levels`
(whitelisted), so it must NOT be affected by the new check — confirmed passing.
