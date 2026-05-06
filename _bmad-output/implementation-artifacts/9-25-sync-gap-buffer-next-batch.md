---
status: ready-for-dev
epic: 9
story: 25
security_review: not-needed
---

# Story 9.25: GAP-BUFFER-NEXT-BATCH — Buffer path returns since-token as next_batch

Status: ready-for-dev

## Story

As a Matrix client (Element Web / matrix-js-sdk),
I want each incremental sync response to carry a monotonically advancing next_batch token,
So that the client can issue a fresh since-token on every poll and never receives duplicate events from the buffer fast-path.

**Size:** S (hours)

---

## Background

Story 4-16 introduced a local ring buffer fast-path for incremental sync.  When events are
already buffered locally, `handleIncrementalSync` drains the buffer and returns immediately —
skipping the gRPC round-trip to Elixir Core.

The current implementation in `buildResponseFromBufferedEvents` (sync.go line ~613) uses the
client's incoming `sinceToken` as `NextBatch`:

```go
// sinceToken is used as the next_batch value (events are fresh, not a new server token).
return syncResponse{
    NextBatch: sinceToken,   // ← client sends same since-token again
    ...
}
```

This means the server-side `sync_tokens.updated_at` record is **not** advanced, and the client
receives the same since-token it just sent.  On the next poll the client sends the identical
token → the buffer may deliver the same delta again → potential duplicate events.

In practice, matrix-js-sdk deduplicates events by `event_id`, so end-users rarely see
duplicates.  However the stuck-token loop wastes CPU (repeated DB reads and buffer drains)
and violates the Matrix spec requirement that `next_batch` MUST advance monotonically.

### Root cause (sync.go)

```go
// handleIncrementalSync, buffer pre-check path:
if events := h.buffer.DrainFor(userID, 50); len(events) > 0 {
    resp := h.buildResponseFromBufferedEvents(events, sinceToken)  // sinceToken = next_batch!
    ...
}
```

```go
// buildResponseFromBufferedEvents:
return syncResponse{
    NextBatch: sinceToken,   // ← same token echoed back
    ...
}
```

### Fix strategy

Generate a synthetic, monotonically increasing `next_batch` token for every buffer-path
response.  The token only needs to be:

1. **Unique per response** — prevents the client from seeing the same token twice.
2. **Opaque to the client** — the client treats it as a string and sends it back as `?since=`.
3. **Distinguishable from Elixir tokens** — so the server can detect a stale synthetic token
   on the next request if needed (optional, but good hygiene).

Chosen format: `buf_<unix_timestamp_ms>` (e.g. `buf_1746518400000`).  This is:
- Monotonically increasing within the same process (time only goes forward).
- Clearly synthetic (prefix `buf_`) — won't collide with Elixir's content-hash event IDs.
- Zero-dependency — no UUID library needed.

The synthetic token is **not** persisted to `sync_tokens`.  On the next request the client
sends `?since=buf_<ts>`.  The Elixir `GetSyncDelta` handler will not find a matching
`sync_tokens` row → returns `fallback_to_initial = true` → full re-sync.  This is the same
safe-fallback path already exercised by the `FallbackToInitial` branch.

> **Note:** An alternative fix would be to update `sync_tokens.updated_at` directly from Go
> on every buffer-path drain.  This would allow the Elixir delta to resume from the correct
> position.  However it introduces a write-conflict risk with Elixir's own
> `persist_since_token` call.  The synthetic-token approach avoids that coupling and keeps
> the buffer path stateless.  If resume-from-buffer becomes important for correctness, a
> dedicated story (GAP-BUFFER-PERSIST-TOKEN) should design the coordination protocol.

---

## Acceptance Criteria

**AC1 — Buffer-path response carries a new synthetic next_batch token:**
When `buildResponseFromBufferedEvents` is called, the returned `syncResponse.NextBatch`
MUST NOT equal the `sinceToken` argument.  It MUST be of the form `buf_<unix_ms>` where
`unix_ms` is a positive integer.

**AC2 — Token is monotonically increasing within a single process:**
Two successive calls to `buildResponseFromBufferedEvents` (within the same process, possibly
different users) must each return a distinct token.  The second token MUST sort lexicographically
after the first when compared as strings (guaranteed by Unix-ms monotonicity under NTP
constraints, which are acceptable for this use-case).

**AC3 — Non-buffer path (Elixir delta and initial sync) is unaffected:**
`handleIncrementalSync` and `GetSync` must continue to use `deltaResp.GetSinceToken()` and
`initialResp.GetSinceToken()` respectively as `NextBatch`.  No changes to those code paths.

**AC4 — Unit tests cover the happy path and non-regression:**
- `TestBuildResponseFromBufferedEvents_NextBatchAdvances`: verifies that calling
  `buildResponseFromBufferedEvents` with `sinceToken = "s42_1"` returns a `NextBatch` that
  is NOT `"s42_1"` and starts with `"buf_"`.
- `TestHandleIncrementalSync_BufferPath_NextBatchAdvances`: integration-level httptest
  verifying that when the buffer returns events, the HTTP response JSON `next_batch` starts
  with `"buf_"`.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`TestBuildResponseFromBufferedEvents_NextBatchAdvances`** — Go unit test (sync_test.go)
   - Given: a `GetSyncHandler` with a mock buffer returning one event, `sinceToken = "s42_1"`
   - When: `buildResponseFromBufferedEvents(events, "s42_1")` is called
   - Then: `resp.NextBatch != "s42_1"` AND `strings.HasPrefix(resp.NextBatch, "buf_")` is true

2. **`TestBuildResponseFromBufferedEvents_NextBatchIsMonotonic`** — Go unit test (sync_test.go)
   - Given: two sequential calls to `buildResponseFromBufferedEvents`
   - When: both calls complete within the same test (sub-millisecond apart)
   - Then: the two returned `NextBatch` values are distinct (implementation may use
     a monotonic counter or time + sequence to guarantee uniqueness)

3. **`TestHandleIncrementalSync_BufferPath_NextBatchAdvances`** — Go httptest test (sync_test.go)
   - Given: a `GetSyncHandler` wired with a mock buffer that returns one `*pb.Event`
   - When: `GET /_matrix/client/v3/sync?since=s42_1` is sent with a valid JWT
   - Then: HTTP 200, `next_batch` in response JSON starts with `"buf_"` and is not `"s42_1"`

4. **`TestHandleIncrementalSync_CorePath_NextBatchUnchanged`** — Go httptest test (sync_test.go, non-regression)
   - Given: a `GetSyncHandler` with a nil or empty buffer, mock Core client returns `SinceToken = "s99_2"`
   - When: `GET /_matrix/client/v3/sync?since=s42_1` is sent
   - Then: HTTP 200, `next_batch == "s99_2"` (Core token, not the synthetic one)

---

## Technical Implementation Plan

### Files to modify

| File | Change |
|---|---|
| `gateway/internal/matrix/sync.go` | Replace `NextBatch: sinceToken` with `NextBatch: syntheticNextBatch()` in `buildResponseFromBufferedEvents`; add `syntheticNextBatch()` helper |
| `gateway/internal/matrix/sync_test.go` | Add 4 unit/httptest tests listed above |

### Step 1 — Add `syntheticNextBatch()` helper to sync.go

Insert the following helper immediately above `buildResponseFromBufferedEvents`:

```go
// syntheticNextBatch generates a monotonically advancing, opaque next_batch token
// for responses served from the local ring buffer (Story 9-25, GAP-BUFFER-NEXT-BATCH).
//
// Format: "buf_<unix_ms>" — clearly synthetic, not a real Elixir since-token.
// If the client sends this token on the next request, Elixir's GetSyncDelta will
// not find a matching sync_tokens row → FallbackToInitial → safe full re-sync.
//
// Monotonicity guarantee: time.Now().UnixMilli() is monotonically increasing within
// a process under normal NTP conditions. For sub-millisecond bursts the counter suffix
// ensures uniqueness (see syntheticBatchSeq below).
var syntheticBatchSeq atomic.Int64

func syntheticNextBatch() string {
    seq := syntheticBatchSeq.Add(1)
    return fmt.Sprintf("buf_%d_%d", time.Now().UnixMilli(), seq)
}
```

> Note: `atomic.Int64` is available from `sync/atomic` (Go 1.19+). The sequence counter
> ensures uniqueness even when two goroutines call `syntheticNextBatch` within the same
> millisecond.  The `seq` suffix is an additional discriminator; clients treat the token
> as opaque, so the internal format does not matter.

### Step 2 — Replace `sinceToken` in `buildResponseFromBufferedEvents`

In `buildResponseFromBufferedEvents` (sync.go, ~line 631), change:

```go
// Before:
return syncResponse{
    NextBatch: sinceToken,
    ...
}
```

to:

```go
// After:
return syncResponse{
    NextBatch: syntheticNextBatch(),
    ...
}
```

The `sinceToken` parameter can be removed from the function signature since it is no longer
used inside the body.  Update both call sites in `handleIncrementalSync` accordingly:

```go
// Before:
resp := h.buildResponseFromBufferedEvents(events, sinceToken)

// After:
resp := h.buildResponseFromBufferedEvents(events)
```

### Step 3 — Add unit tests to sync_test.go

Add the four acceptance tests listed in the Acceptance Tests section.  Use the existing
`buildAuthedSyncHandler` helper and `mockMessageBuffer` (or inline a minimal buffer stub if
the existing mock doesn't support pre-loaded events).

---

## Dev Notes

### Import additions required

`sync.go` will need the following import additions:
- `"fmt"` — already imported
- `"sync/atomic"` — new; add to import block

### Comment alignment

The existing comment on `buildResponseFromBufferedEvents` says:
> `sinceToken is used as the next_batch value (events are fresh, not a new server token).`

Remove or replace this comment with:
> `syntheticNextBatch() generates a buf_<ms>_<seq> token. The Elixir delta handler will
> not recognise it, triggering FallbackToInitial on the next request — which is the
> correct behaviour for a client that resumes from a synthetic token.`

### FallbackToInitial is already safe

The existing `FallbackToInitial` branch in `handleIncrementalSync` handles the case where
Elixir cannot find the client's since-token.  It calls `GetInitialSync` and returns a full
re-sync with a fresh real token.  This is already tested and production-proven.  The synthetic
token approach intentionally leverages this path rather than adding new state.

### No migration needed

No database schema change is required.  The synthetic token is never written to
`sync_tokens`.

### Severity & impact

Severity: SHOULD (from tmp/sync-issues.md).  Impact is low in practice because
matrix-js-sdk deduplicates by event_id.  The fix is purely in Go, small in scope
(two lines changed + one helper added), and carries no risk to existing sync paths.

### Existing test patterns to follow

- Buffer mock: look for `mockMessageBuffer` or `bufferStub` usage in `sync_test.go`.
- httptest wiring: `buildAuthedSyncHandler` in `sync_test.go`.
- JSON assertion: `json.NewDecoder(rec.Body).Decode(&result)` + field assertion.

---

## Status

Status: ready-for-dev
