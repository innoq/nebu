---
status: review
epic: 12
story: 6
security_review: required
matrix: true
ui: false
---

# Story 12.6: Blurhash Pass-Through + Animated Thumbnail Correctness

Status: review

## Story

As a Matrix client user,
I want blurhash data to be correctly stored and returned with media metadata,
So that clients can show loading placeholders before the full image loads.

**Size:** S

---

## Acceptance Criteria

**AC1 — Blurhash field persisted as part of content.info (client-provided, not server-computed):**

Given a client uploads an image with `content.info.blurhash` in the `m.room.message` event,
When the event is stored,
Then the `blurhash` field is persisted as part of `content.info` in the events table — the server does NOT compute blurhash (client-provided value only).

**AC2 — Blurhash returned in sync unmodified:**

Given `/sync` returns a `m.room.message` event with an image,
When the event `content.info` is inspected,
Then `blurhash` is present if the client provided it during send — not stripped or modified.

**AC3 — animated=false on animated GIF source returns static image:**

Given `GET /thumbnail?animated=false` is called on an animated GIF source,
When the thumbnail is generated,
Then the response is a static single-frame image (not animated) — correct `Content-Type: image/jpeg` or `image/png`.

**AC4 — Missing width or height returns 400 M_BAD_JSON:**

Given `GET /thumbnail` is called without `width` or `height` params,
When the request is processed,
Then the server returns `400 M_BAD_JSON` — both params are required per Matrix spec Section 13.8.2.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1** — `TestBlurhashPersisted_ViaRoundTrip` in `gateway/internal/matrix/rooms_test.go` [Go unit test]
- Given: A `m.room.message` event with `content = {"msgtype": "m.image", "body": "photo.jpg", "info": {"blurhash": "LEHV6nWB2yk8pyo0adR*.7kCMdnj"}}`
- When: `PutSendEvent` handler processes the request (via httptest with fake gRPC)
- Then: The gRPC `SendEventRequest.Content` bytes contain `"blurhash"` key inside `"info"` — not stripped

**AT-2** — `TestBlurhashInSyncResponse` in `gateway/internal/matrix/sync_test.go` [Go unit test]
- Given: An event in the DB with `content = {"msgtype": "m.image", "body": "photo.jpg", "info": {"blurhash": "LEHV6nWB2yk8pyo0adR*.7kCMdnj", "mimetype": "image/jpeg"}}`
- When: `/sync` response is built (via buildTimeline or similar)
- Then: The returned event `content.info.blurhash` equals the stored value — not nil, not stripped

**AT-3** — `TestThumbnailHandler_AnimatedFalse_GIFSource_ReturnsStaticJPEG` in `media/internal/thumbnail/handler_test.go` [Go unit test]
- **NOTE:** This test already exists as part of Story 12.5 ATDD (AT-10 in handler_test.go). Verify it passes as a regression guard.
- Given: An encrypted animated GIF stored via fakeThumbStorer
- When: `GET /thumbnail?width=100&height=100&animated=false` is served
- Then: Response is HTTP 200 with `Content-Type: image/jpeg` (not `image/gif`)

**AT-4** — `TestThumbnailHandler_MissingWidth_Returns400` / `TestThumbnailHandler_MissingHeight_Returns400` in `media/internal/thumbnail/handler_test.go` [Go unit test]
- **NOTE:** These tests already exist as part of Story 12.5 ATDD (AT-4 in handler_test.go). Verify they pass as regression guards.
- Given: GET request without `width` or `height`
- Then: `400 M_BAD_JSON`

**AT-5** — NEW: `TestSendEvent_BlurhashInContentInfo_PassedToGRPC` in `gateway/internal/matrix/rooms_test.go` [Go unit test — write first, failing]
- Given: An HTTP PUT send-event body containing `{"msgtype":"m.image","body":"cat.jpg","info":{"w":800,"h":600,"mimetype":"image/jpeg","size":12345,"blurhash":"LEHV6nWB2yk8pyo0adR*.7kCMdnj"}}`
- When: `PutSendEvent` handler executes
- Then: The `SendEvent` gRPC call receives `Content` bytes that JSON-unmarshal to a map where `content["info"].(map)["blurhash"]` equals `"LEHV6nWB2yk8pyo0adR*.7kCMdnj"`

**AT-6** — NEW: `TestSendEvent_BlurhashAbsent_ContentInfoPreserved` in `gateway/internal/matrix/rooms_test.go` [Go unit test — write first, failing]
- Given: A `m.image` event without `blurhash` in `content.info`
- When: `PutSendEvent` processes it
- Then: gRPC receives the content with `info` intact (no fields added or removed)

---

## Architecture Context

### What this story covers

Story 12.6 has two distinct sub-concerns:

1. **Blurhash pass-through** (AC1 + AC2): Tests that verify the existing pass-through behavior works correctly. The system already passes `content` as raw JSON bytes from gateway → gRPC → Core → DB. No new code is needed for this — the tests verify the existing architecture is correct.

2. **Animated thumbnail correctness** (AC3 + AC4): These are regression tests for behavior already implemented in Story 12.5. The ATDD tests from Story 12.5 (`AT-10` for animated=false and `AT-4` for missing params) already cover these ACs. This story adds explicit named tests for traceability.

### Key architecture facts

**Gateway → Core content flow (no transformation):**
- `PutSendEvent` in `gateway/internal/matrix/rooms.go` decodes the JSON body into `map[string]any` then re-encodes to bytes and passes to `pb.SendEventRequest.Content`
- The gateway does NOT inspect or modify `content.info` fields — blurhash is passed through unchanged
- Core receives `content` as bytes, decodes to Elixir map, stores as JSONB in PostgreSQL `events.content`

**Sync content flow:**
- `gateway/internal/matrix/sync.go` reads `content` from PostgreSQL as `content_json` string
- Returns it as `json.RawMessage(contentJSON)` — no parsing, no field filtering
- `content.info.blurhash` is already present in the raw JSON and returned verbatim

**Thumbnail handler (Story 12.5):**
- `media/internal/thumbnail/handler.go` — `animated` param defaults to `false`; when `false` + GIF source, `GenerateThumbnail` takes the static JPEG path
- `w/h` validation: if `width == ""` → 400 M_BAD_JSON immediately (checked before anything else)
- Both behaviors are already implemented and tested by 12.5 ATDD tests

### Files to verify / add tests to

| File | Action |
|------|--------|
| `gateway/internal/matrix/rooms_test.go` | ADD AT-5 + AT-6 (new failing tests) |
| `media/internal/thumbnail/handler_test.go` | VERIFY AT-3 + AT-4 pass (already exist from 12.5) |
| `gateway/internal/matrix/sync_test.go` | ADD AT-2 (verify blurhash in sync response) |

### No implementation changes expected

The pass-through behavior is already correct. This story's value is:
1. **Explicit test coverage** for blurhash (previously untested, documented behavior)
2. **Regression guards** for animated=false and missing-params (already tested in 12.5 ATDD, but explicit story-level traceability needed)

If tests fail, the implementation has a regression — fix the implementation, not the test.

### Matrix spec compliance

Per Matrix spec Section 13.8.2 (Media content):
- `content.info` is an opaque object — the server MUST NOT modify it
- `blurhash` is a client-provided string in `content.info` — server never computes it
- `GET /thumbnail?animated=false` → server MUST NOT return an animated thumbnail (spec v1.18 MUST)
- `width` and `height` are REQUIRED query params for `/thumbnail` — missing → 400

### File locations

```
gateway/
  internal/
    matrix/
      rooms.go             ← SendEventHandler (PutSendEvent) — NO CHANGE needed
      rooms_test.go        ← ADD AT-5 + AT-6
      sync.go              ← GetSyncHandler — NO CHANGE needed
      sync_test.go         ← ADD AT-2 (blurhash in sync timeline event)
media/
  internal/
    thumbnail/
      handler.go           ← NO CHANGE needed
      handler_test.go      ← VERIFY AT-3 (animated=false) + AT-4 (missing params) exist from 12.5
      thumbnail.go         ← NO CHANGE needed
```

### Existing test patterns to follow

**rooms_test.go pattern** (from SendEventHandler tests):
```go
type fakeSendCoreClient struct {
    lastReq *pb.SendEventRequest
    err     error
}

func (f *fakeSendCoreClient) SendEvent(_ context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
    f.lastReq = req
    if f.err != nil {
        return nil, f.err
    }
    return &pb.SendEventResponse{EventId: "test-event-id"}, nil
}
```

Look for existing `fakeSendCoreClient` or similar in `rooms_test.go` — extend it rather than creating a new one.

**sync_test.go pattern**: Find the existing DB-backed test helpers that seed events and verify sync response content. Follow the same pattern.

### Regression guard: 12.5 ATDD tests

The Story 12.5 ATDD tests already cover AT-3 and AT-4:
- `TestThumbnailHandler_AnimatedFalse_GIFSource_ReturnsStaticJPEG` → AT-3
- `TestThumbnailHandler_MissingWidth_Returns400` + `TestThumbnailHandler_MissingHeight_Returns400` → AT-4

**These tests MUST still pass.** If they fail, the 12.5 implementation has regressed — fix it before adding new tests.

### DB migration: NOT required

The `events.content` column is JSONB. All fields in `content.info` (including `blurhash`) are already stored as-is. No schema changes needed.

---

## Dev Agent Implementation Notes

### Implementation plan

1. Run `make test-unit-go` — verify all 70 thumbnail tests from 12.5 still pass (AT-3 + AT-4 regression check)
2. Write AT-5 in `gateway/internal/matrix/rooms_test.go` (failing — new test)
3. Write AT-6 in `gateway/internal/matrix/rooms_test.go` (failing — new test)
4. Write AT-2 in `gateway/internal/matrix/sync_test.go` (failing — new test)
5. Run tests — expect AT-5, AT-6, AT-2 to pass immediately (pass-through already correct)
6. If any test fails, identify the implementation gap and fix it

### Expected outcome

All tests should pass WITHOUT any implementation changes. The tests are documentation + regression guards, not new features.

Exception: if `rooms_test.go` or `sync_test.go` do not have a suitable test infrastructure for these tests, add the minimal infrastructure (fake gRPC client, DB seeding helper) needed.

### Go test patterns

For AT-5 / AT-6 (rooms_test.go):
```go
func TestSendEvent_BlurhashInContentInfo_PassedToGRPC(t *testing.T) {
    fakeClient := &fakeSendCoreClient{}
    h := matrix.NewSendEventHandler(matrix.SendEventConfig{
        CoreClient: fakeClient,
        ServerName: "test.local",
    })
    // ... build request with blurhash in info
    // ... call handler
    // ... unmarshal fakeClient.lastReq.Content
    // ... assert info["blurhash"] == expected
}
```

For AT-2 (sync_test.go) — follow the existing sync test pattern that seeds events in a test DB and calls the sync handler.

---

## Previous Story Intelligence (12.5)

**From 12.5 dev notes:**
- `media/internal/thumbnail/handler.go` — fully implemented, 13 HTTP tests pass
- `media/internal/thumbnail/thumbnail.go` — fully implemented, 9 unit tests pass
- The `animated` param is parsed from query string; default is `false`
- Missing `width` or `height` returns 400 immediately before any DB/storage calls
- `TestThumbnailHandler_AnimatedFalse_GIFSource_ReturnsStaticJPEG` is the spec MUST test
- 70 total tests in media package — all green
- `disintegration/imaging v1.6.2` is the thumbnail library (pure Go, no cgo)

**No changes to media package expected in 12.6.**

---

## Definition of Done

- [ ] AT-5: `TestSendEvent_BlurhashInContentInfo_PassedToGRPC` passes
- [ ] AT-6: `TestSendEvent_BlurhashAbsent_ContentInfoPreserved` passes
- [ ] AT-2: sync blurhash test passes
- [ ] AT-3: `TestThumbnailHandler_AnimatedFalse_GIFSource_ReturnsStaticJPEG` passes (regression guard from 12.5)
- [ ] AT-4: `TestThumbnailHandler_MissingWidth_Returns400` + `TestThumbnailHandler_MissingHeight_Returns400` pass (regression guards)
- [ ] `make test-unit-go` passes (all media + gateway unit tests green)
- [ ] `make test-unit-elixir` passes (no regressions in Core)
- [ ] Story status updated to `done`
