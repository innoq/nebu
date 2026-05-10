---
status: ready-for-dev
epic: 11
story: 4
security_review: required
matrix: true
ui: false
---

# Story 11.4: Gateway `POST /search` Handler

Status: ready-for-dev

## Story

As a Matrix client user,
I want `POST /_matrix/client/v3/search` to return properly formatted results,
so that Element Web can display search results with context, highlights, and room grouping.

**Size:** M

---

## Acceptance Criteria

**AC1 — Matrix spec §11.14 response format:**
Given a client sends `POST /search` with `{"search_categories": {"room_events": {"search_term": "hello", "order_by": "rank"}}}`,
When the handler processes it,
Then the response is HTTP 200 with JSON matching Matrix spec §11.14.1:
`search_categories.room_events` contains `count`, `results` (each with `rank`, `result`, `context.events_before`, `context.events_after`, `context.profile_info`, `highlights`), `next_batch`, `groups`, `state`

**AC2 — room_filter forwarded and enforced:**
Given the filter block in the request contains `rooms: ["!room1:server"]`,
When the handler processes it,
Then only results from `!room1:server` are returned (Core enforces membership intersection)

**AC3 — results grouped by room_id:**
Given the response is assembled,
When `groups` is inspected,
Then results are grouped by `room_id` with per-group `results` arrays and `next_batch` tokens

**AC4 — unauthenticated request rejected:**
Given the JWT middleware is applied to `POST /search`,
When an unauthenticated request arrives (no Bearer token),
Then the server returns `401 M_UNKNOWN_TOKEN`
(Note: enforced by jwtMiddleware in main.go, NOT in the handler itself)

**AC5 — gRPC ResourceExhausted → 429 M_LIMIT_EXCEEDED:**
Given the Core gRPC call returns `codes.ResourceExhausted`,
When the handler handles it,
Then the response is `429 M_LIMIT_EXCEEDED` with `retry_after_ms` field

**AC6 — gRPC PermissionDenied → 403 M_FORBIDDEN:**
Given the Core gRPC call returns `codes.PermissionDenied`,
When the handler handles it,
Then the response is `403 M_FORBIDDEN`

**AC7 — gRPC Internal → 500 M_UNKNOWN:**
Given the Core gRPC call returns `codes.Internal` or any other unexpected error,
When the handler handles it,
Then the response is `500 M_UNKNOWN`

**AC8 — empty search_term rejected:**
Given the request body has an empty or whitespace-only `search_term`,
When the handler validates input,
Then the response is `400 M_INVALID_PARAM`

**AC9 — user_id forwarded via gRPC metadata (SECURITY CRITICAL):**
Given the handler receives an authenticated request,
When it calls Core.SearchMessages,
Then `user_id` is set via `coregrpc.WithUserMetadata(ctx, userID, systemRole)` (x-user-id gRPC header),
And `SearchMessagesRequest.UserId` is NOT set (or left as empty string) — the Core handler ignores it

**AC10 — handler registered in main.go:**
Given main.go is updated,
When `POST /_matrix/client/v3/search` is called,
Then the request is routed to the search handler with jwtWithStatusCheck + bodyLimit1MiB middleware

---

## Security Trust Boundary — CRITICAL

> **Inherited from Kassandra Review (Stories 11.2/11.3, Finding MEDIUM-2):**
> The `user_id` MUST come from the authenticated JWT context (`middleware.ContextKeyUserID`),
> forwarded to gRPC via `coregrpc.WithUserMetadata()`.
> NEVER set `SearchMessagesRequest.UserId` from the request body or any client-supplied field.
>
> The Core handler (Story 11.3) already enforces this by reading ONLY from gRPC metadata
> (x-user-id header). The Gateway's job is to ensure the correct user_id flows via metadata.

Pattern (already established in rooms.go, event_context.go, public_rooms.go):
```go
userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
// Then: coreClient.SearchMessages(grpcCtx, &pb.SearchMessagesRequest{...})
// DO NOT: set SearchTerm: req.UserId or UserId: userID in the proto request
```

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

These are Go `net/http/httptest` unit tests in:
`gateway/internal/matrix/search_test.go`

Use `mockSearchCoreClient` (same pattern as `mockEventContextCoreClient` in event_context_test.go).
Use `contextWithUser` helper from public_rooms_test.go (already in the `matrix` package).

**1. `TestPostSearch_HappyPath` (AC1)**
- Given: mock returns `SearchMessagesResponse` with 2 results (rank 0.9, 0.7), `next_batch: "dGVzdA=="`
- Given: request has `{"search_categories": {"room_events": {"search_term": "hello"}}}`
- Given: context has userID `@alice:test.local`, role `user`
- When: `POST /_matrix/client/v3/search` is called
- Then: HTTP 200
- Then: body has `search_categories.room_events.results` with 2 items
- Then: each result has `rank`, `result` (event JSON), `context`, `highlights`
- Then: body has `search_categories.room_events.count == 2`
- Then: body has `search_categories.room_events.next_batch == "dGVzdA=="`
- Then: body has `search_categories.room_events.groups.room_id` with per-room grouping

**2. `TestPostSearch_Unauthenticated` (AC4)**
- Given: no userID in context (simulates missing JWT — jwtMiddleware writes 401 before handler)
- When: `POST /search` is called without context user
- Then: HTTP 401 with `M_UNKNOWN_TOKEN`
- Note: Test the middleware chain. The handler itself should write 401 if userID is empty (defense-in-depth)

**3. `TestPostSearch_EmptySearchTerm` (AC8)**
- Given: request body is `{"search_categories": {"room_events": {"search_term": ""}}}`
- Given: context has valid userID
- When: handler processes request
- Then: HTTP 400 with `M_INVALID_PARAM`

**4. `TestPostSearch_ResourceExhausted_429` (AC5)**
- Given: mock returns `status.Error(codes.ResourceExhausted, "rate limit exceeded")`
- Given: request has valid search term
- When: handler calls gRPC
- Then: HTTP 429 with `M_LIMIT_EXCEEDED`
- Then: body contains `retry_after_ms` field (positive integer)

**5. `TestPostSearch_PermissionDenied_403` (AC6)**
- Given: mock returns `status.Error(codes.PermissionDenied, "not a member")`
- When: handler calls gRPC
- Then: HTTP 403 with `M_FORBIDDEN`

**6. `TestPostSearch_InternalError_500` (AC7)**
- Given: mock returns `status.Error(codes.Internal, "search failed")`
- When: handler calls gRPC
- Then: HTTP 500 with `M_UNKNOWN`

**7. `TestPostSearch_RoomFilter_Forwarded` (AC2)**
- Given: request body contains `"filter": {"rooms": ["!room1:test.local"]}`
- Given: mock records the gRPC request it receives
- When: handler calls gRPC
- Then: mock received `SearchMessagesRequest.RoomFilter == ["!room1:test.local"]`

**8. `TestPostSearch_UserIDFromContext_NotFromBody` (AC9 — security regression)**
- Given: request body DOES NOT contain any user_id field (Matrix spec doesn't include it)
- Given: context has `userID = "@alice:test.local"` (from jwtMiddleware)
- Given: mock records the gRPC context it receives
- When: handler calls gRPC
- Then: `SearchMessagesRequest.UserId` is empty string (field not set by Gateway)
- Then: gRPC context contains outgoing metadata `x-user-id: "@alice:test.local"` (via WithUserMetadata)

**Persistence-Strategy note:** This handler is stateless (no GenServer state). No crash/restart test required.

---

## Tasks / Subtasks

- [ ] Task 1: Write failing ATDD tests first (AC1–AC10)
  - [ ] Create `gateway/internal/matrix/search_test.go`
  - [ ] Implement `mockSearchCoreClient` struct with `SearchMessages` method
  - [ ] Write all 8 acceptance tests above
  - [ ] Verify tests FAIL (red phase — `SearchHandler` doesn't exist yet)

- [ ] Task 2: Add `SearchMessages` method to `gateway/internal/grpc/client.go` (AC9)
  - [ ] Add wrapper method: `func (c *Client) SearchMessages(ctx context.Context, req *pb.SearchMessagesRequest) (*pb.SearchMessagesResponse, error)`
  - [ ] Pattern: identical to `GetEventContext`, `ListPublicRooms`, etc. — one-liner delegating to `c.core.SearchMessages(ctx, req)`

- [ ] Task 3: Create `gateway/internal/matrix/search.go` (AC1–AC9)
  - [ ] Define `SearchCoreClient` interface (consumer-defined, minimal: only `SearchMessages`)
  - [ ] Define `SearchHandler` struct with `coreClient SearchCoreClient`
  - [ ] Define `SearchConfig` + `NewSearchHandler` constructor
  - [ ] Implement `PostSearch(w http.ResponseWriter, r *http.Request)`
    - [ ] `requireJSON` check
    - [ ] Decode JSON body (see request structure below)
    - [ ] Validate `search_term` not empty (AC8)
    - [ ] Extract `userID`, `systemRole` from context via `middleware.ContextKeyUserID` / `middleware.ContextKeySystemRole`
    - [ ] If `userID == ""` → 401 `M_UNKNOWN_TOKEN` (defense-in-depth)
    - [ ] Build `grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)`
    - [ ] Build `pb.SearchMessagesRequest` with search_term, limit, next_batch, room_filter (DO NOT set UserId)
    - [ ] Call `h.coreClient.SearchMessages(grpcCtx, req)`
    - [ ] Map gRPC errors: `ResourceExhausted → 429`, `PermissionDenied → 403`, others → `500`
    - [ ] Assemble Matrix spec §11.14 response (see structure below)
    - [ ] Return 200 JSON

- [ ] Task 4: Register handler in `gateway/cmd/gateway/main.go` (AC10)
  - [ ] Instantiate `searchHandler := matrix.NewSearchHandler(matrix.SearchConfig{CoreClient: coreClient})`
  - [ ] Register: `mux.Handle("POST /_matrix/client/v3/search", bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(searchHandler.PostSearch))))`
  - [ ] Add comment: `// Story 11.4: Full-text search — POST /_matrix/client/v3/search`

- [ ] Task 5: Run tests and verify green
  - [ ] `make test-unit-go` passes (all 8 new tests green)
  - [ ] `make build-gateway` succeeds

---

## Dev Notes

### Request JSON structure (Matrix spec §11.14.1)

```go
// Matrix CS API POST /search request body
type searchRequest struct {
    NextBatch        string `json:"next_batch,omitempty"` // top-level pagination token
    SearchCategories struct {
        RoomEvents *roomEventsSearch `json:"room_events,omitempty"`
    } `json:"search_categories"`
}

type roomEventsSearch struct {
    SearchTerm string              `json:"search_term"`
    OrderBy    string              `json:"order_by,omitempty"` // "rank" or "recent" — MVP: always rank
    Limit      int32               `json:"limit,omitempty"`    // 0 = default 10
    Filter     *roomEventsFilter   `json:"filter,omitempty"`
}

type roomEventsFilter struct {
    Rooms   []string `json:"rooms,omitempty"`   // room_id allowlist
    Senders []string `json:"senders,omitempty"` // sender_id allowlist
}
```

**Validation:** Only `room_events` category is required for MVP. If `search_categories.room_events` is nil, return `400 M_INVALID_PARAM`.

### Response JSON structure (Matrix spec §11.14.1)

```go
// Assembly function maps SearchMessagesResponse → Matrix response
type searchResponse struct {
    SearchCategories struct {
        RoomEvents roomEventsResult `json:"room_events"`
    } `json:"search_categories"`
}

type roomEventsResult struct {
    Count     int32              `json:"count"`
    Results   []searchResultItem `json:"results"`
    NextBatch string             `json:"next_batch,omitempty"`
    Groups    map[string]any     `json:"groups,omitempty"`  // keyed by "room_id" → per-room group
    State     map[string]any     `json:"state,omitempty"`   // MVP: empty map
    Highlights []string          `json:"highlights,omitempty"` // MVP: empty slice (or extracted from search_term words)
}

type searchResultItem struct {
    Rank    float32        `json:"rank"`
    Result  map[string]any `json:"result"`   // deserialized event JSON
    Context searchContext  `json:"context"`
}

type searchContext struct {
    EventsBefore []map[string]any       `json:"events_before"` // from gRPC SearchResult.EventsBefore
    EventsAfter  []map[string]any       `json:"events_after"`  // from gRPC SearchResult.EventsAfter
    ProfileInfo  map[string]profileInfo `json:"profile_info"`  // from gRPC SearchResult.ProfileInfo
}

type profileInfo struct {
    DisplayName string `json:"displayname"`
    AvatarURL   string `json:"avatar_url"`
}
```

**Groups assembly:** After mapping results, group by `room_id`:
```go
groups := map[string]any{}
for _, result := range results {
    roomID, _ := result.Result["room_id"].(string)
    if roomID == "" { continue }
    group, ok := groups[roomID].(map[string]any)
    if !ok {
        group = map[string]any{"results": []map[string]any{}, "next_batch": ""}
    }
    group["results"] = append(group["results"].([]map[string]any), map[string]any{"event_id": result.Result["event_id"]})
    groups[roomID] = group
}
```
Top-level `groups` key format: `{"room_id": {"!roomA:server": {"results": [...], "next_batch": ""}}}` — key is literal `"room_id"`.

**Highlights:** For MVP, return the search_term words split by whitespace as the highlights array. This is better than an empty array and helps clients bold matched terms. Example: `search_term = "hello world"` → `highlights: ["hello", "world"]`.

**next_batch:** Pass through directly from `SearchMessagesResponse.NextBatch`. If empty, omit from response (Matrix clients treat absent next_batch as "no more pages").

### gRPC error mapping

```go
st, _ := status.FromError(err)
switch st.Code() {
case codes.ResourceExhausted:
    // 429 M_LIMIT_EXCEEDED — rate limit from Core (Story 11.5 will add Gateway-side RL)
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusTooManyRequests)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "errcode":        "M_LIMIT_EXCEEDED",
        "error":          "Search rate limit exceeded",
        "retry_after_ms": 60000,
    })
case codes.PermissionDenied:
    writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of any searched room")
default:
    slog.Error("SearchMessages gRPC failed", "code", st.Code(), "msg", st.Message())
    writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
}
```

### SearchMessagesRequest field mapping

```go
grpcReq := &pb.SearchMessagesRequest{
    // DO NOT set UserId — Core reads user_id from x-user-id gRPC metadata (trusted_identity pattern)
    SearchTerm: req.SearchCategories.RoomEvents.SearchTerm,
    Limit:      req.SearchCategories.RoomEvents.Limit, // 0 = Core default (10); Core clamps 1–100
    NextBatch:  req.NextBatch,                          // top-level next_batch is passed through
}
if req.SearchCategories.RoomEvents.Filter != nil {
    grpcReq.RoomFilter   = req.SearchCategories.RoomEvents.Filter.Rooms
    grpcReq.SenderFilter = req.SearchCategories.RoomEvents.Filter.Senders
}
```

### SearchResult deserialization

`pb.SearchResult.Event` is `[]byte` containing raw JSON (serialized by Elixir Core via `Jason.encode!/1`):
```go
for _, r := range resp.Results {
    var eventMap map[string]any
    if err := json.Unmarshal(r.Event, &eventMap); err != nil {
        slog.Warn("search: failed to unmarshal event JSON", "err", err)
        continue // skip malformed result, don't 500
    }
    // Unmarshal EventsBefore and EventsAfter similarly
    var before, after []map[string]any
    for _, b := range r.EventsBefore {
        var ev map[string]any
        if err := json.Unmarshal(b, &ev); err == nil {
            before = append(before, ev)
        }
    }
    for _, a := range r.EventsAfter {
        var ev map[string]any
        if err := json.Unmarshal(a, &ev); err == nil {
            after = append(after, ev)
        }
    }
    // Map ProfileInfo
    profileMap := make(map[string]profileInfo)
    for uid, pi := range r.ProfileInfo {
        profileMap[uid] = profileInfo{DisplayName: pi.GetDisplayname(), AvatarURL: pi.GetAvatarUrl()}
    }
    results = append(results, searchResultItem{Rank: r.Rank, Result: eventMap, Context: searchContext{...}})
}
```

### Consumer-defined interface (Go ADR-009 convention)

```go
// SearchCoreClient is the consumer-defined interface for the SearchMessages gRPC call.
// Minimal — only what this handler needs.
type SearchCoreClient interface {
    SearchMessages(ctx context.Context, req *pb.SearchMessagesRequest) (*pb.SearchMessagesResponse, error)
}
```

The concrete `*grpc.Client` satisfies this interface after Task 2 adds the method.

### File to add handler method in client.go

Add after `ListAdminRoomMembers` (end of the methods block, before `CoreServiceClient()`):

```go
// SearchMessages calls the Elixir core to perform a full-text search over rooms
// the caller is a member of. user_id is forwarded via gRPC metadata (x-user-id),
// NOT from SearchMessagesRequest.UserId — Core enforces this.
// Story 11.4: Gateway POST /_matrix/client/v3/search.
func (c *Client) SearchMessages(ctx context.Context, req *pb.SearchMessagesRequest) (*pb.SearchMessagesResponse, error) {
    return c.core.SearchMessages(ctx, req)
}
```

### main.go registration pattern (follow existing handlers)

Insert after the notifications handler registration (around line 733), following the Story 11.4 comment convention:

```go
// Story 11.4: Full-text search — POST /_matrix/client/v3/search.
// JWT required. Body limit 1 MiB. Rate limiting is enforced by Core (Story 11.5 adds gateway-side RL).
searchHandler := matrix.NewSearchHandler(matrix.SearchConfig{
    CoreClient: coreClient,
})
mux.Handle("POST /_matrix/client/v3/search",
    bodyLimit1MiB(jwtWithStatusCheck(http.HandlerFunc(searchHandler.PostSearch))))
```

---

## Files to Create / Modify

| File | Action | Notes |
|---|---|---|
| `gateway/internal/matrix/search.go` | NEW | `SearchHandler`, `SearchCoreClient` interface, `PostSearch` handler |
| `gateway/internal/matrix/search_test.go` | NEW | 8 acceptance tests — written FIRST (red phase) |
| `gateway/internal/grpc/client.go` | MODIFY | Add `SearchMessages` method to `*Client` |
| `gateway/cmd/gateway/main.go` | MODIFY | Instantiate + register search handler for `POST /_matrix/client/v3/search` |

No Elixir changes. No proto changes. No migrations. No Playwright tests (ui: false).

---

## Project Structure Notes

### Established patterns in the `matrix` package

All handlers follow the same structure — `search.go` MUST follow these exactly:

1. **Consumer-defined interface:** `type SearchCoreClient interface { SearchMessages(...) }` — never use `*grpc.Client` directly in the handler file
2. **Config struct + constructor:** `SearchConfig{CoreClient: SearchCoreClient}` + `NewSearchHandler(cfg SearchConfig)`
3. **Package-level helper functions:** `requireJSON`, `writeMatrixError` are already in the `matrix` package (defined in `validate.go` and `login.go`) — do NOT redefine them
4. **gRPC metadata:** Always use `coregrpc.WithUserMetadata(r.Context(), userID, systemRole)` — never construct metadata manually
5. **Import aliases:** `coregrpc "github.com/nebu/nebu/internal/grpc"` and `pb "github.com/nebu/nebu/internal/grpc/pb"` — same aliases as rooms.go, event_context.go

### What NOT to do

- Do NOT set `SearchMessagesRequest.UserId` — Core ignores it and the Gateway must not trust client data for identity
- Do NOT define a new `writeMatrixError` or `requireJSON` function — they already exist in the `matrix` package
- Do NOT add rate limiting in this story — Story 11.5 handles Gateway-side rate limiting
- Do NOT add Godog scenarios in this story — Story 11.6 handles E2E tests
- Do NOT add `sender_filter` enforcement in the handler — forward to gRPC as-is (Core handles it)
- Do NOT return `401` from the handler for missing JWT — `jwtWithStatusCheck` middleware handles this before the handler is reached; add `userID == ""` guard only as defense-in-depth
- Do NOT use `int64` for `next_batch` in the response — pass through the string token from gRPC unchanged
- Do NOT add complexity for the `state` field in the response — return an empty map `{}` for MVP (Story 11.6 Godog scenarios do not test state events)

---

## Previous Story Intelligence (11.3)

From Story 11.3 completion notes (directly relevant to this story):

**Proto types (already generated, no changes needed):**
- `pb.SearchMessagesRequest` — fields: `UserId` (ignored), `SearchTerm`, `RoomFilter`, `SenderFilter`, `Limit`, `NextBatch`
- `pb.SearchMessagesResponse` — fields: `Results` (`[]*pb.SearchResult`), `NextBatch`, `TotalCount`
- `pb.SearchResult` — fields: `Rank` (float32), `Event` ([]byte — raw event JSON), `EventsBefore` ([][]byte), `EventsAfter` ([][]byte), `ProfileInfo` (map[string]*pb.ProfileInfo)
- `pb.ProfileInfo` — fields: `Displayname`, `AvatarUrl`
- `core_grpc.pb.go` — `SearchMessages` RPC fully wired; `c.core.SearchMessages(ctx, req)` is callable

**Security contract from 11.2/11.3 Kassandra reviews:**
- MEDIUM-2: user_id MUST come from gRPC metadata, never from request body
- The Go Gateway's role: extract user_id from JWT context, forward via `WithUserMetadata`, never set `SearchMessagesRequest.UserId`

**Pattern confirmation:** `coregrpc.WithUserMetadata` is the correct function. It sets `x-user-id` and `x-system-role` in gRPC outgoing metadata. The Elixir handler reads `x-user-id` via `Nebu.Grpc.Metadata.trusted_identity(stream)`.

---

## Architecture References

- [Source: gateway/internal/matrix/event_context.go] — consumer-defined interface + handler pattern to follow exactly
- [Source: gateway/internal/matrix/event_context_test.go] — `mockEventContextCoreClient` + test structure to replicate
- [Source: gateway/internal/matrix/public_rooms_test.go:503-507] — `contextWithUser` helper (already in `matrix` package)
- [Source: gateway/internal/matrix/rooms.go:88-90] — `WithUserMetadata` usage pattern
- [Source: gateway/internal/matrix/login.go:55-59] — `writeMatrixError` definition (do not redefine)
- [Source: gateway/internal/matrix/validate.go:57-69] — `requireJSON` definition (do not redefine)
- [Source: gateway/internal/grpc/client.go:350-352] — `ListAdminRoomMembers` — template for the new `SearchMessages` wrapper method
- [Source: gateway/internal/grpc/metadata.go:18-26] — `WithUserMetadata` function (sets x-user-id + x-system-role)
- [Source: gateway/cmd/gateway/main.go:726-733] — notifications handler registration — insert search handler registration after this block
- [Source: gateway/internal/grpc/pb/core.pb.go:5458-5550] — `SearchMessagesRequest` and `SearchMessagesResponse` Go types
- [Source: gateway/internal/grpc/pb/core.pb.go:5376-5446] — `SearchResult` Go type (Rank float32, Event []byte, EventsBefore [][]byte, EventsAfter [][]byte, ProfileInfo map[string]*ProfileInfo)
- [Source: docs/architecture/adr/ADR-010-fts-strategy.md] — ADR Accepted 2026-05-08 (tsvector, GIN, websearch_to_tsquery, scope enforcement)
- [Source: docs/stories/phase2/epic-11/11-3-search-messages-grpc-handler.md] — Story 11.3 completion notes: proto fields, security contract

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

## Change Log

| Date | Change |
|---|---|
| 2026-05-08 | Story created: ready-for-dev |
