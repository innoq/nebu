---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-05-05'
storyId: '9.18'
storyKey: '9-18-admin-room-detail-member-list'
storyFile: '_bmad-output/implementation-artifacts/9-18-admin-room-detail-member-list.md'
atddChecklistPath: '_bmad-output/test-artifacts/atdd-checklist-9-18-admin-room-detail-member-list.md'
generatedTestFiles:
  - 'gateway/internal/admin/rooms_detail_test.go'
  - 'gateway/internal/admin/admin_grpc_actor_identity_test.go'
  - 'gateway/internal/admin/auth_audit_test.go'
  - 'core/apps/event_dispatcher/test/nebu/event_dispatcher/admin_grpc_test.exs'
inputDocuments:
  - '_bmad-output/implementation-artifacts/9-18-admin-room-detail-member-list.md'
  - 'gateway/internal/admin/rooms.go'
  - 'gateway/internal/admin/rooms_detail_test.go'
  - 'gateway/internal/admin/stubs.go'
  - 'gateway/internal/admin/page_data.go'
  - 'gateway/internal/admin/admin_grpc_actor_identity_test.go'
  - 'gateway/internal/admin/auth_audit_test.go'
  - 'core/apps/event_dispatcher/test/nebu/event_dispatcher/admin_grpc_test.exs'
  - '_bmad/tea/config.yaml'
---

# ATDD Checklist — Story 9.18: Admin UI — Room Detail: Member List

## Step 1: Preflight & Context

**Story:** 9.18 — Admin UI: Room Detail: Member List  
**Status:** ready-for-dev  
**Stack detected:** fullstack (Go + Elixir backend, Go Templates UI)  
**Execution mode:** sequential (backend-focused, no browser recording needed)

### Acceptance Criteria Overview

| AC | Description | Test Level | Priority |
|----|-------------|-----------|---------|
| AC1 | Proto: `ListAdminRoomMembers` RPC + 3 messages | Compile-time (proto gen) | P0 |
| AC2 | Elixir: `list_admin_room_members/2` handler + DB query | ExUnit integration | P0 |
| AC3 | Go gRPC client wrapper method | Compile-time / unit | P1 |
| AC4 | `AdminRoomsClient` interface extended | Compile-time | P1 |
| AC5 | `DetailHandler` fetches + passes member list | Go httptest (gRPC path) | P0 |
| AC6 | `RoomMemberData` struct + `ActiveRoomMembers` field | Compile-time | P0 |
| AC7 | Template renders member list section | Go httptest (stub path) | P0 |
| AC8 | Stub fallback `stubRoomMembers` map | Go httptest (stub path) | P0 |
| AC9 | Unit test: member list renders | Go httptest — **the test itself** | P0 |
| AC10 | Unit test: no members renders gracefully | Go httptest — **the test itself** | P0 |

---

## Step 2: Generation Mode

**Mode:** AI Generation (sequential)  
**Rationale:** All acceptance criteria are clear; backend-only stack — no browser recording needed.

---

## Step 3: Test Strategy

### Mapped Scenarios

| Test ID | Scenario | Level | Framework | Priority | RED reason |
|---------|----------|-------|-----------|----------|-----------|
| AT#GO-1 | `TestRoomDetailMemberListRenders` — room-001 stub path renders Alice Müller + link | Unit/httptest | Go httptest | P0 | `stubRoomMembers`, `ActiveRoomMembers`, template section missing |
| AT#GO-2 | `TestRoomDetailNoMembers` — room-003 stub path: no Members heading | Unit/httptest | Go httptest | P0 | same as above |
| AT#GO-3 | `captureContextClient` no-op stub compiles | Compile-time | Go compiler | P1 | `pb.ListAdminRoomMembersRequest` missing until `make proto` |
| AT#GO-4 | `mockCoreClient` no-op stub compiles | Compile-time | Go compiler | P1 | `pb.ListAdminRoomMembersRequest` missing until `make proto` |
| AT#EX-1 | `ListAdminRoomMembers` returns 2 members with correct user_id + joined_at | ExUnit integration | ExUnit | P0 | proto struct + `Server.list_admin_room_members/2` missing |
| AT#EX-2 | `display_name` is binary string, not raw bytes | ExUnit integration | ExUnit | P1 | same as AT#EX-1 |
| AT#EX-3 | `ListAdminRoomMembers` returns empty list for room with 0 members — no error | ExUnit integration | ExUnit | P0 | same as AT#EX-1 |

---

## Step 4: Generated Test Files

### 🔴 TDD RED PHASE — All tests FAIL before implementation

---

### File 1: `gateway/internal/admin/rooms_detail_test.go`

**Added tests:**

- `TestRoomDetailMemberListRenders` (AC9)  
  RED because: `RoomMemberData`, `stubRoomMembers`, `ActiveRoomMembers`, template section do not exist.

- `TestRoomDetailNoMembers` (AC10)  
  RED because: same as above; even after partial implementation, the `{{ if .ActiveRoomMembers }}` guard cannot be verified until the template section is added.

---

### File 2: `gateway/internal/admin/admin_grpc_actor_identity_test.go`

**Added no-op stub:**

- `captureContextClient.ListAdminRoomMembers`  
  RED because: `pb.ListAdminRoomMembersRequest` / `pb.ListAdminRoomMembersResponse` do not exist until `make proto` regenerates stubs.

---

### File 3: `gateway/internal/admin/auth_audit_test.go`

**Added no-op stub:**

- `mockCoreClient.ListAdminRoomMembers`  
  RED because: same proto type issue as above.

---

### File 4: `core/apps/event_dispatcher/test/nebu/event_dispatcher/admin_grpc_test.exs`

**Added modules:**

- `FakeAdminDBWithMembers` — extends FakeAdminDB with `list_room_members/1` (ETS-backed)
- `FakeAdminDBEmptyRoom` — returns `{:ok, []}` for all `list_room_members/1` calls

**Added `list_room_members/1` to existing modules:**

- `FakeAdminDB.list_room_members/1` — ETS-backed, returns seeded rows
- `FakeAdminDBNotFound.list_room_members/1` — returns `{:ok, []}` (not `:not_found`, empty room is valid)

**Added helper:**

- `insert_member_rows/2` — seeds `{:members, room_id}` into ETS

**Added describe block `"ListAdminRoomMembers — AC2 (Story 9.18)"`:**

- AT#EX-1: `"returns 2 members with correct user_id and joined_at for a populated room"`  
  RED: `Core.ListAdminRoomMembersRequest` compile error + `Server.list_admin_room_members/2` UndefinedFunctionError

- AT#EX-2: `"display_name is a string (decrypted or empty on failure) — not raw binary"`  
  RED: same as AT#EX-1

- AT#EX-3: `"returns empty members list for a room with no joined members — no error"`  
  RED: same as AT#EX-1

---

## Acceptance Criteria Coverage Matrix

| AC | Test ID(s) | Coverage | Notes |
|----|-----------|----------|-------|
| AC1 (proto) | AT#GO-3, AT#GO-4, AT#EX-1..3 | Indirect — compile-time | All tests fail with compile error if proto not generated |
| AC2 (Elixir handler) | AT#EX-1, AT#EX-2, AT#EX-3 | Direct | |
| AC3 (Go gRPC client) | AT#GO-3, AT#GO-4 | Indirect | Tested via interface satisfaction |
| AC4 (interface) | AT#GO-1, AT#GO-2, AT#GO-3, AT#GO-4 | Indirect | Compile error if interface not extended |
| AC5 (DetailHandler gRPC path) | Not directly covered in RED phase | — | Covered when gRPC integration tests are written |
| AC6 (page_data) | AT#GO-1, AT#GO-2 | Indirect | Tests fail if struct/field missing |
| AC7 (template) | AT#GO-1, AT#GO-2 | Direct | String assertions verify rendered HTML |
| AC8 (stubRoomMembers) | AT#GO-1, AT#GO-2 | Direct | Stub path exercises the map |
| AC9 (unit test itself) | AT#GO-1 | The test IS the criterion | |
| AC10 (unit test itself) | AT#GO-2 | The test IS the criterion | |

**Coverage:** 9/10 ACs have at least one test. AC5 (gRPC path error-handling) is not covered by a new RED test — the implementation dev notes explicitly state the gRPC path is non-fatal (warn + continue with empty list); this can be verified with an existing-pattern integration test after proto generation.

---

## Implementation Order (suggested for dev)

1. `make proto` — generates Go + Elixir stubs → AT#GO-3, AT#GO-4 compile errors disappear
2. `gateway/internal/admin/page_data.go` — add `RoomMemberData` + `ActiveRoomMembers`
3. `gateway/internal/admin/stubs.go` — add `stubRoomMembers` map
4. `gateway/internal/admin/rooms.go` — extend `AdminRoomsClient` + `DetailHandler`
5. `gateway/internal/admin/templates/rooms.html` — add Members section → AT#GO-1, AT#GO-2 go GREEN
6. Elixir: `db.ex` + `server.ex` → AT#EX-1, AT#EX-2, AT#EX-3 go GREEN
