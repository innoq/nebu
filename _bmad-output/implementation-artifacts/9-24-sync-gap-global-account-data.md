---
status: ready-for-dev
epic: 9
story: 24
security_review: not-needed
---

# Story 9.24: GAP-GLOBAL-ACCOUNT-DATA — Top-Level account_data Missing from Sync Response

Status: ready-for-dev

## Story

As a Matrix client (Element Web),
I want the top-level `account_data.events` array in the sync response to contain global account data (e.g. `m.push_rules`, `m.direct`, `m.ignored_user_list`),
so that client features depending on global account data receive this data via the standard sync loop and not only through explicit GET requests.

**Size:** M

---

## Background

The Matrix spec §6.3 states that the top-level `account_data.events` SHOULD contain global account data events. Currently the `syncResponse` struct in `gateway/internal/matrix/sync.go` has no top-level `AccountData` field — only per-room account data (under `rooms.join.{roomId}.account_data.events`) is delivered.

Global account data is already stored in the `room_account_data` table (migration `000029`) using `room_id = ''` (empty string) as a sentinel. The `PUT/GET /_matrix/client/v3/user/{userId}/account_data/{type}` endpoints already work correctly. The gap is purely in the sync response: the top-level `account_data` key is missing.

The `AccountDataDB` interface (in `account_data.go`) already supports global lookups via `GetAccountData(ctx, userID, "", eventType)`. However, `injectAccountData` in `sync.go` only iterates over joined rooms — it never queries for `room_id = ''`.

The existing `PostgresAccountDataDB` stores global rows in `room_account_data` with `room_id = ''`. A new DB query function is needed to list ALL global account data rows for a user (instead of fetching a single row by event type).

**Impact:** Element Web features that depend on `m.push_rules` or `m.direct` (DM room mapping) never receive this data via sync, causing incorrect push rule application and missing DM detection.

**Severity:** SHOULD (spec §6.3)

---

## Acceptance Criteria

**AC1 — Top-level account_data in initial sync:**
`GET /sync` (no `?since`) returns a JSON body where the top-level `account_data.events` array contains all global account data rows stored for the authenticated user. If no global account data exists, the field is `{"events": []}` (never absent, never null).

**AC2 — Top-level account_data in incremental sync:**
`GET /sync?since=<token>` returns a JSON body where the top-level `account_data.events` array contains all global account data rows for the authenticated user. The same always-present, never-null guarantee applies.

**AC3 — After a global PUT, next sync delivers the new event:**
After `PUT /_matrix/client/v3/user/{userId}/account_data/m.direct` (or any type), the next `/sync` response includes that event in the top-level `account_data.events` array with the correct `type` and `content`.

**AC4 — Per-room account_data unaffected:**
The existing per-room `account_data` under `rooms.join.{roomId}.account_data.events` continues to work as before (no regression). Global data (room_id = '') must NOT appear in per-room sections.

**AC5 — syncResponse struct has AccountData field:**
The `syncResponse` Go struct includes a top-level `AccountData syncAccountDataSection \`json:"account_data"\`` field. The JSON key is `account_data`.

**AC6 — Graceful degradation when DB unavailable:**
If the DB query for global account data fails, `account_data.events` falls back to `[]` (empty array). The sync response is still returned with HTTP 200 — the DB error is logged as WARN but not surfaced to the client.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **`[P0] TestGetSync_GlobalAccountData_InitialSync`** — Go httptest (`gateway/internal/matrix/sync_test.go`)
   - Given: `accountDataDB` returns `[{type: "m.direct", content: {"@bob:nebu.test":["!room1:nebu.test"]}}]` for `ListGlobalAccountData(ctx, userID)`
   - When: GET `/sync` (no `?since`) is called with a valid JWT
   - Then: response body contains top-level `account_data.events` array with one entry: `{"type":"m.direct","content":{"@bob:nebu.test":["!room1:nebu.test"]}}`

2. **`[P0] TestGetSync_GlobalAccountData_IncrementalSync`** — Go httptest (`gateway/internal/matrix/sync_test.go`)
   - Given: `accountDataDB` returns `[{type: "m.push_rules", content: {...}}]` for global account data
   - When: GET `/sync?since=s1_token` is called with a valid JWT
   - Then: response body contains top-level `account_data.events` with the `m.push_rules` entry

3. **`[P0] TestGetSync_GlobalAccountData_Empty`** — Go httptest (`gateway/internal/matrix/sync_test.go`)
   - Given: `accountDataDB` returns `[]` (no global account data)
   - When: GET `/sync` is called
   - Then: response body contains `"account_data":{"events":[]}` (not absent, not null)

4. **`[P0] TestGetSync_GlobalAccountData_DBError_Degrades`** — Go httptest (`gateway/internal/matrix/sync_test.go`)
   - Given: `accountDataDB.ListGlobalAccountData` returns an error
   - When: GET `/sync` is called
   - Then: response is HTTP 200 with `"account_data":{"events":[]}` (graceful degradation)

5. **`[P1] TestGetSync_PerRoomAccountData_NotAffected`** — Go httptest (`gateway/internal/matrix/sync_test.go`)
   - Given: global account data `m.direct` is stored (room_id = ''), per-room `m.fully_read` is stored (room_id = '!room1:nebu.test')
   - When: GET `/sync` is called
   - Then: top-level `account_data.events` contains `m.direct` only; `rooms.join.!room1:nebu.test.account_data.events` contains `m.fully_read` only

6. **`[P1] Godog — GET sync delivers global account_data after PUT`** — Godog integration (`gateway/features/account_data.feature`)
   - Given: kai is authenticated, kai PUT `m.direct` with body `{"@bob:nebu.test":["!room1:nebu.test"]}`
   - When: kai calls GET `/sync` (initial)
   - Then: response body top-level `account_data.events` contains an entry with `type` = `"m.direct"`

---

## Technical Implementation Plan

### Files to modify

| File | Change |
|---|---|
| `gateway/internal/matrix/sync.go` | Add `AccountData syncAccountDataSection` to `syncResponse`; add `GlobalAccountDataDB` interface; add `injectGlobalAccountData` helper; call it in initial, incremental, fallback, and buffer code paths |
| `gateway/internal/matrix/sync_test.go` | Add unit tests AC1–AC5 (5 new test functions) |
| `gateway/internal/db/account_data_store.go` | Add `ListGlobalAccountData(ctx, userID) ([]matrix.GlobalAccountDataRow, error)` to `PostgresAccountDataDB` |
| `gateway/internal/matrix/account_data.go` | Add `GlobalAccountDataDB` interface + `GlobalAccountDataRow` struct |
| `gateway/cmd/gateway/main.go` | Wire `GlobalAccountDataDB` into `GetSyncConfig` |
| `gateway/features/account_data.feature` | Add Godog scenario for AC3 |
| `gateway/test/integration/account_data_steps_test.go` | Add step definition for the new Godog scenario |

### Step 1 — New interface and struct in account_data.go

Add to `gateway/internal/matrix/account_data.go`:

```go
// GlobalAccountDataRow represents a single global account data event.
type GlobalAccountDataRow struct {
    EventType string
    Content   json.RawMessage
}

// GlobalAccountDataDB is the consumer-defined interface for listing all global
// account data for a user. Defined separately from AccountDataDB to keep
// interfaces minimal (ADR-009 / Go interface convention).
type GlobalAccountDataDB interface {
    // ListGlobalAccountData returns all global account data rows (room_id = '')
    // for the given userID. Returns an empty slice (not nil) when no rows exist.
    ListGlobalAccountData(ctx context.Context, userID string) ([]GlobalAccountDataRow, error)
}
```

### Step 2 — DB implementation in account_data_store.go

Add to `PostgresAccountDataDB` in `gateway/internal/db/account_data_store.go`:

```go
// ListGlobalAccountData returns all global account data rows for userID
// (room_id = '' in room_account_data). Returns an empty slice on no rows.
func (p *PostgresAccountDataDB) ListGlobalAccountData(ctx context.Context, userID string) ([]matrix.GlobalAccountDataRow, error) {
    rows, err := p.db.QueryContext(ctx,
        `SELECT event_type, content FROM room_account_data
         WHERE user_id = $1 AND room_id = ''`,
        userID)
    if err != nil {
        return []matrix.GlobalAccountDataRow{}, err
    }
    defer rows.Close()
    var result []matrix.GlobalAccountDataRow
    for rows.Next() {
        var r matrix.GlobalAccountDataRow
        var content []byte
        if err := rows.Scan(&r.EventType, &content); err != nil {
            continue
        }
        r.Content = json.RawMessage(content)
        result = append(result, r)
    }
    if result == nil {
        result = []matrix.GlobalAccountDataRow{}
    }
    return result, rows.Err()
}
```

Note: `PostgresAccountDataDB` already uses `withUserDB` for single-row lookups. For `ListGlobalAccountData`, use a direct query without `withUserDB` because the RLS `app.user_id` GUC wiring (migration `000033`) requires a transaction — list queries with cursors are simpler with a direct connection. Use the pattern established in `buildLeaveRooms` (direct `QueryContext` on `p.db`).

### Step 3 — Add GlobalAccountDataDB to GetSyncHandler

In `gateway/internal/matrix/sync.go`:

```go
type GetSyncHandler struct {
    coreClient         GetSyncCoreClient
    serverName         string
    timeout            time.Duration
    buffer             *buffer.MessageBuffer
    db                 *sql.DB
    accountDataDB      AccountDataDB      // per-room account data
    globalAccountDataDB GlobalAccountDataDB // Story 9-24: global account data
}

type GetSyncConfig struct {
    CoreClient          GetSyncCoreClient
    ServerName          string
    Timeout             time.Duration
    Buffer              *buffer.MessageBuffer
    DB                  *sql.DB
    AccountDataDB       AccountDataDB
    GlobalAccountDataDB GlobalAccountDataDB // Story 9-24
}
```

### Step 4 — Add AccountData field to syncResponse

```go
type syncResponse struct {
    NextBatch               string                 `json:"next_batch"`
    Rooms                   syncRooms              `json:"rooms"`
    Presence                syncPresence           `json:"presence"`
    AccountData             syncAccountDataSection `json:"account_data"` // Story 9-24: §6.3 global account data
    DeviceOneTimeKeysCount  map[string]int         `json:"device_one_time_keys_count"`
    DeviceUnusedFallbackKeys []string              `json:"device_unused_fallback_key_types"`
    DeviceLists             syncDeviceLists        `json:"device_lists"`
}
```

### Step 5 — injectGlobalAccountData helper

```go
// injectGlobalAccountData queries global account data (room_id = '') for userID
// and returns a syncAccountDataSection for the top-level account_data field.
// Degrades gracefully to an empty events slice on DB error (AC6).
func (h *GetSyncHandler) injectGlobalAccountData(ctx context.Context, userID string) syncAccountDataSection {
    if h.globalAccountDataDB == nil {
        return syncAccountDataSection{Events: []syncAccountDataEvent{}}
    }
    rows, err := h.globalAccountDataDB.ListGlobalAccountData(ctx, userID)
    if err != nil {
        slog.Warn("injectGlobalAccountData: DB error", "user_id", userID, "err", err)
        return syncAccountDataSection{Events: []syncAccountDataEvent{}}
    }
    events := make([]syncAccountDataEvent, 0, len(rows))
    for _, r := range rows {
        events = append(events, syncAccountDataEvent{Type: r.EventType, Content: r.Content})
    }
    return syncAccountDataSection{Events: events}
}
```

### Step 6 — Call injectGlobalAccountData in all sync paths

In `GetSync` (initial sync), `handleIncrementalSync` (delta and FallbackToInitial), and `buildResponseFromBufferedEvents` (buffer fast path), set:

```go
AccountData: h.injectGlobalAccountData(r.Context(), userID),
```

For `buildResponseFromBufferedEvents`, the method currently does not have access to the context cleanly — pass it as a parameter, or accept the degraded empty response for the buffer fast path (buffer events are new message events, not account data changes; global account data changes are rare).

**Recommended approach for buffer path:** Skip global account data injection in `buildResponseFromBufferedEvents` (leave `AccountData` as empty section). The next full sync cycle will pick up the data. This keeps the buffer fast path O(0) DB queries. Document the trade-off in a comment.

### Step 7 — Wire GlobalAccountDataDB in main.go

`PostgresAccountDataDB` already implements `AccountDataDB`. Extend it to also implement `GlobalAccountDataDB` (via Step 2 above). In `main.go` where `NewGetSyncHandler` is configured:

```go
postgresAccountDataDB := db.NewPostgresAccountDataDB(bootstrapDB)
// ...
syncHandler := matrix.NewGetSyncHandler(matrix.GetSyncConfig{
    // existing fields ...
    AccountDataDB:       postgresAccountDataDB,
    GlobalAccountDataDB: postgresAccountDataDB, // same instance, Story 9-24
})
```

### Step 8 — Godog scenario in account_data.feature

Add to `gateway/features/account_data.feature`:

```gherkin
Scenario: Sync_GlobalAccountData — global PUT visible in top-level account_data after sync
  Given kai is a registered and authenticated user
  When kai puts global account data type "m.direct" with body {"@bob:nebu.test":["!room1:nebu.test"]}
  And kai calls GET /sync
  Then the sync response top-level account_data.events contains an entry with type "m.direct"
```

Add corresponding step definition in `gateway/test/integration/account_data_steps_test.go`.

---

## Dev Notes

### Key invariants

1. **No migration needed:** Global account data already uses `room_id = ''` in `room_account_data` (migration `000029`). The DB schema is correct.

2. **RLS and `app.user_id`:** The `room_account_data` RLS policy requires `SET LOCAL app.user_id` before queries (migration `000033` / `withUserDB` pattern). `ListGlobalAccountData` must run within the same `withUserDB` transaction wrapper to satisfy RLS. Review `withUserDB` in `account_data_store.go` to confirm it wraps `QueryContext` results correctly, or use `db.BeginTx + SET LOCAL + QueryContext` inline.

3. **`syncAccountDataSection` reuse:** The same `syncAccountDataSection` struct used for per-room data can be reused for the top-level field — both have the same shape (`{"events": [...]}`).

4. **Buffer fast path:** `buildResponseFromBufferedEvents` is the hot path for local event delivery. Adding a DB round-trip here would degrade latency. Return `syncAccountDataSection{Events: []syncAccountDataEvent{}}` for the buffer path. Document this clearly.

5. **`PostgresAccountDataDB` implements both interfaces:** After Step 2, `*PostgresAccountDataDB` satisfies both `matrix.AccountDataDB` and `matrix.GlobalAccountDataDB`. No new type is needed. Compile-time interface satisfaction check can be added: `var _ matrix.GlobalAccountDataDB = (*PostgresAccountDataDB)(nil)`.

6. **`rows.Err()` check:** Always check `rows.Err()` after iterating in `ListGlobalAccountData` (Go SQL hygiene). Any error after partial iteration degrades to returning whatever was collected — log a WARN.

### Where to find existing patterns

- `injectAccountData` (per-room): `gateway/internal/matrix/sync.go` line ~696
- `withUserDB` usage: `gateway/internal/db/account_data_store.go`
- `buildLeaveRooms` (direct QueryContext without withUserDB): `gateway/internal/matrix/sync.go` line ~120
- Existing Godog step for global PUT: `kaiPutsGlobalAccountData` in `gateway/test/integration/account_data_steps_test.go`
- `syncAccountDataSection` struct: `gateway/internal/matrix/sync.go` line ~330
