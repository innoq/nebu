# Story 4.27: POST /rooms/{roomId}/read_markers — Retry-Loop Fix

Status: done

## Story

As a developer,
I want `POST /_matrix/client/v3/rooms/{roomId}/read_markers` to return 200 `{}`,
so that Element Web stops retrying the request and logging "Error sending fully_read" spam.

---

## Background / Motivation

When entering a room, Element Web posts `{"m.fully_read": "$eventId", "m.read": "$eventId"}` to advance the fully-read marker.
Without this endpoint, every request returned 404 → Element retried repeatedly (6+ times visible in console).
The console log showed hundreds of "Error sending fully_read" messages per session.

MVP scope: accept and acknowledge without persisting (same as POST /receipt which is already persisted via Core).

---

## Acceptance Criteria

1. `POST /_matrix/client/v3/rooms/{roomId}/read_markers` is registered in `main.go` with JWT middleware.

2. Valid JSON body → 200 `{}`.

3. Empty body `{}` → 200 `{}` (clients sometimes omit optional fields).

4. Malformed JSON body → 400 `M_BAD_JSON`.

5. Unauthenticated → 401 `M_MISSING_TOKEN`.

6. No "Error sending fully_read" errors appear in Element Web console after entering a room.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestPostReadMarkers_HappyPath` — Go httptest
   - Given: authenticated, POST `{"m.fully_read":"$event","m.read":"$event"}`
   - Then: 200 OK, response is valid JSON `{}`

2. `TestPostReadMarkers_EmptyBody` — POST `{}` → 200 OK

3. `TestPostReadMarkers_BadJSON` — POST malformed → 400 M_BAD_JSON

4. `TestPostReadMarkers_Unauthenticated` — no Bearer → 401

5. Browser E2E: "No read_markers retry loop when entering room"
   - Given: SSO login, room with message, navigate to room
   - When: wait 5 seconds (retry storm window)
   - Then: zero console errors containing "fully_read" or "Error"

---

## Implementation Notes

- Handler: `gateway/internal/matrix/read_markers.go` — `ReadMarkersHandler`, no gRPC
- MVP: accept body, return `{}\n` — no persistence
- Registered in `main.go` after receipt handler
