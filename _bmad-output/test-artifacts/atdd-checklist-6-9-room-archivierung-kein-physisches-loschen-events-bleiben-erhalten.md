---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation']
lastStep: 'step-02-generation'
lastSaved: '2026-05-01'
storyId: '6.9'
storyKey: '6-9-room-archivierung-kein-physisches-loschen-events-bleiben-erhalten'
storyFile: '_bmad-output/implementation-artifacts/6-9-room-archivierung-kein-physisches-loschen-events-bleiben-erhalten.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-6-9-room-archivierung-kein-physisches-loschen-events-bleiben-erhalten.md'
generatedTestFiles:
  - gateway/internal/api/rooms_archive_handler_test.go
  - gateway/internal/matrix/rooms_test.go (extended)
  - gateway/internal/api/router_test.go (extended)
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs
  - core/apps/room_manager/test/nebu_room_test.exs (extended)
inputDocuments:
  - _bmad-output/implementation-artifacts/6-9-room-archivierung-kein-physisches-loschen-events-bleiben-erhalten.md
  - gateway/internal/api/rooms_patch_handler_test.go
  - gateway/internal/api/rooms_repo.go
  - gateway/internal/api/server.go
  - gateway/internal/api/router_test.go
  - gateway/internal/matrix/rooms_test.go
  - gateway/internal/matrix/rooms.go
  - gateway/internal/grpc/pb/core_grpc.pb.go
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/update_room_settings_test.exs
  - core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs
  - core/apps/room_manager/test/nebu_room_test.exs
---

# ATDD Checklist — Story 6.9: Room Archivierung

## Stack Detection

- Go (gateway): `backend`
- Elixir (core): `backend`
- Combined: fullstack backend (no browser tests for this story)

## Generated Test Files

### 1. `gateway/internal/api/rooms_archive_handler_test.go` (NEW)

**13 Go unit tests covering AC#1, AC#2, AC#5**

| # | Test Name | AC | Priority | RED Reason |
|---|-----------|-----|----------|-----------|
| 1 | `TestArchiveAdminRoom_HappyPath_Returns200` | AC#1, AC#5 | P0 | `api.ArchiveResult` + `RoomRepository.ArchiveRoom` + `pb.ArchiveRoomRequest` don't exist |
| 2 | `TestArchiveAdminRoom_AlreadyArchived_Returns409` | AC#1, AC#5 | P0 | `api.ErrRoomWrongStatus` doesn't exist |
| 3 | `TestArchiveAdminRoom_UnknownRoom_Returns404` | AC#1, AC#5 | P0 | `api.ErrRoomNotFound` doesn't exist |
| 4 | `TestArchiveAdminRoom_ShortReason_Returns400` | AC#1, AC#5 | P0 | Handler not implemented |
| 5 | `TestUnarchiveAdminRoom_HappyPath_Returns200` | AC#2, AC#5 | P0 | `api.UnarchiveResult` + `RoomRepository.UnarchiveRoom` + `pb.UnarchiveRoomRequest` don't exist |
| 6 | `TestUnarchiveAdminRoom_NotArchived_Returns409` | AC#2, AC#5 | P0 | Handler not implemented |
| 7 | `TestArchiveAdminRoom_AuditLogEmitted` | AC#1, AC#5 | P0 | Handler not implemented |
| 8 | `TestUnarchiveAdminRoom_AuditLogEmitted` | AC#2, AC#5 | P0 | Handler not implemented |
| 9 | `TestArchiveAdminRoom_NilRepository_Returns501` | AC#5 | P0 | Route not registered in router.go |
| 10 | `TestUnarchiveAdminRoom_NilRepository_Returns501` | AC#5 | P0 | Route not registered in router.go |
| 11 | `TestArchiveAdminRoom_gRPCFailure_BestEffort_Returns200` | AC#1 | P1 | Handler not implemented |
| 12 | `TestUnarchiveAdminRoom_UnknownRoom_Returns404` | AC#2 | P0 | Handler not implemented |
| 13 | `TestArchiveAdminRoom_MissingReason_Returns400` | AC#1 | P1 | Handler not implemented |

### 2. `gateway/internal/matrix/rooms_test.go` (EXTENDED — 3 tests appended)

**3 Go unit tests covering AC#4, AC#5**

| # | Test Name | AC | Priority | RED Reason |
|---|-----------|-----|----------|-----------|
| 14 | `TestPutSendEvent_ArchivedRoom_Returns403` | AC#4, AC#5 | P0 | `RoomStatusChecker` interface + `SendEventConfig.StatusChecker` don't exist |
| 15 | `TestPutSendEvent_ActiveRoom_CallsCore` | AC#4 | P1 | `SendEventConfig.StatusChecker` field doesn't exist |
| 16 | `TestPutSendEvent_StatusCheckerError_FailOpen` | AC#4 | P1 | `SendEventConfig.StatusChecker` field doesn't exist |

### 3. `gateway/internal/api/router_test.go` (EXTENDED — 2 tests appended)

**2 Go router registration tests covering AC#5**

| # | Test Name | AC | Priority | RED Reason |
|---|-----------|-----|----------|-----------|
| 17 | `TestRegisterAdminRoutes_ArchiveRoom_RouteRegistered` | AC#5 | P0 | Route not registered → 404 |
| 18 | `TestRegisterAdminRoutes_UnarchiveRoom_RouteRegistered` | AC#5 | P0 | Route not registered → 404 |

### 4. `core/apps/event_dispatcher/test/nebu/event_dispatcher/archive_room_test.exs` (NEW)

**4 Elixir test describes (7 test cases) covering AC#3, AC#6**

| # | Test Name | AC | Priority | RED Reason |
|---|-----------|-----|----------|-----------|
| 19 | `archive_room/2 — running — returns ok: true and terminates GenServer` | AC#3, AC#6 | P0 | `Core.ArchiveRoomRequest` doesn't exist (make proto needed) |
| 20 | `archive_room/2 — not running — returns ok: true without error` | AC#6 | P0 | `Core.ArchiveRoomRequest` doesn't exist |
| 21 | `unarchive_room/2 — returns ok: true and starts GenServer` | AC#3, AC#6 | P0 | `Core.UnarchiveRoomRequest` doesn't exist |
| 22 | `Room.Server.init/1 — archived — stops with :normal (crash/restart test)` | AC#6 | P0 | `get_room_status/1` not in DBBehaviour; `init/1` guard not implemented |
| 23 | `Room.Server.init/1 — no restart after :stop :normal` | AC#6 | P0 | Same as above |
| 24 | `Room.Server.init/1 — active room still starts` | AC#6 | P1 | Regression guard after guard is added |

### 5. `core/apps/room_manager/test/nebu_room_test.exs` (EXTENDED)

**FakeDB and FailingWriteDB extended with `get_room_status/1` stub**

- `FakeDB.get_room_status/1` → `{:ok, "active"}` (normal rooms)
- `FailingWriteDB.get_room_status/1` → `{:ok, "active"}` (fail-open on DB error in archive guard)
- RED: compile warning until `@callback get_room_status/1` is added to `Nebu.Room.DBBehaviour`

## Acceptance Criteria Coverage

| AC | Description | Test(s) |
|----|-------------|---------|
| AC#1 | POST /archive — 200, 400, 404, 409; gRPC ArchiveRoom; audit | #1–#4, #7, #11, #13 |
| AC#2 | POST /unarchive — 200, 404, 409; gRPC UnarchiveRoom; audit | #5, #6, #8, #12 |
| AC#3 | gRPC ArchiveRoom + UnarchiveRoom proto RPCs | #19–#21 |
| AC#4 | PutSendEvent checks room status before gRPC | #14–#16 |
| AC#5 | Go unit tests (all scenarios) | #1–#18 |
| AC#6 | Elixir unit tests + crash/restart test | #19–#24 |
| AC#7 | build + test-unit-go + test-unit-elixir pass | Implementation gate |

## Key RED-Phase Compile Blockers (Implementation Order)

1. **`make proto`** — Adds `ArchiveRoom` + `UnarchiveRoom` to `pb.CoreServiceClient`. Without this, `rooms_archive_handler_test.go` won't compile (references `pb.ArchiveRoomRequest`).
2. **`RoomRepository` extension** — Add `ArchiveRoom`, `UnarchiveRoom`, `GetRoomStatus` methods + `ArchiveResult`, `UnarchiveResult` types + `ErrRoomNotFound`, `ErrRoomWrongStatus` sentinels to `rooms_repo.go`.
3. **`make gen-api`** — Add OpenAPI paths first; regenerates `api_gen.go` with `ArchiveAdminRoom`, `UnarchiveAdminRoom` on `StrictServerInterface`.
4. **`AdminServer` stubs** — Add 501 stubs to satisfy `StrictServerInterface` compile check.
5. **`Nebu.Room.DBBehaviour.get_room_status/1`** — Add callback; all FakeDB modules in Elixir tests need the stub.
6. **`Nebu.Room.Server.init/1` guard** — Add archived-status check; `archive_room_test.exs` Test 4 covers this.

## Notes

- The existing `mockCoreClientForRoomPatch` in `rooms_patch_handler_test.go` embeds `pb.CoreServiceClient`. After `make proto`, it must gain `ArchiveRoom` + `UnarchiveRoom` stub methods to avoid compile errors. The new `mockCoreClientForArchive` in `rooms_archive_handler_test.go` demonstrates the correct pattern.
- All Elixir FakeDB modules (est. 14+ across test files) must gain `get_room_status/1`. Use `grep -r "@behaviour Nebu.Room.DBBehaviour" core/apps/ -l` to find them all.
- The crash/restart test (Test 22/23) is the mandatory GenServer state test required by CLAUDE.md conventions.
