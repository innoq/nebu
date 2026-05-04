---
status: ready-for-dev
epic: 9
story: 1
security_review: required
---

# Story 9.1: Admin gRPC RPCs in Core — User + Room Management

Status: ready-for-dev

## Story

As a developer,
I want the Elixir Core to expose gRPC RPCs for admin user and room management,
so that the Admin UI gateway handlers can delegate to Core instead of using in-memory stubs.

## Acceptance Criteria

1. `proto/core.proto` defines these RPCs: `ListAdminUsers`, `GetAdminUser`, `DeactivateUser`, `ReactivateUser`, `UpdateUserRole`, `ListAdminRooms`, `GetAdminRoom`, `GetServerConfig`, `UpdateServerConfig`, `GetMetrics` (real implementation replacing the stub)
2. Core gRPC server responds to `ListAdminUsers` with paginated users from PostgreSQL
3. `DeactivateUser` sets `is_active=false` and triggers `InvalidateUserSessions`
4. `ArchiveRoom` uses `SELECT FOR UPDATE` for atomic archived-status update
5. `make proto` generates Go and Elixir stubs without compile errors

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `ListAdminUsers returns paginated users` — ExUnit
   - Given: 3 users exist in PostgreSQL (2 active, 1 deactivated)
   - When: `ListAdminUsers` gRPC is called with `limit=2`
   - Then: response contains 2 users and a non-empty `next_cursor`

2. `GetAdminUser returns correct user detail` — ExUnit
   - Given: a user with `user_id="@alice:nebu.local"` exists in PostgreSQL
   - When: `GetAdminUser` gRPC is called with that user_id
   - Then: response contains the user's display_name, email (masked), is_active=true, system_role

3. `DeactivateUser sets is_active=false and calls InvalidateUserSessions` — ExUnit
   - Given: user `@bob:nebu.local` exists with `is_active=true`
   - When: `DeactivateUser` gRPC is called
   - Then: `users.is_active=false` in DB AND `InvalidateUserSessions` was called for that user_id

4. `ReactivateUser sets is_active=true` — ExUnit
   - Given: user `@bob:nebu.local` exists with `is_active=false`
   - When: `ReactivateUser` gRPC is called
   - Then: `users.is_active=true` in DB

5. `UpdateUserRole updates system_role` — ExUnit
   - Given: user `@carol:nebu.local` has `system_role="user"`
   - When: `UpdateUserRole` gRPC is called with `role="instance_admin"`
   - Then: `users.system_role="instance_admin"` in DB (or `role_overrides` row if using the role_overrides table)

6. `ListAdminRooms returns paginated rooms` — ExUnit
   - Given: 3 rooms exist (2 active, 1 archived), status filter = "active"
   - When: `ListAdminRooms` gRPC is called with `limit=2, status_filter="active"`
   - Then: response contains 2 active rooms and a next_cursor

7. `GetAdminRoom returns room detail with member_count` — ExUnit
   - Given: room `!abc:nebu.local` exists with 3 members
   - When: `GetAdminRoom` gRPC is called with that room_id
   - Then: response contains room name, status, member_count=3

8. `ArchiveRoom uses SELECT FOR UPDATE atomically` — ExUnit
   - Given: room `!xyz:nebu.local` exists with status="active"
   - When: `ArchiveRoom` gRPC is called
   - Then: `rooms.status="archived"` in DB (via `SELECT FOR UPDATE`)

9. `GetServerConfig returns current server config` — ExUnit
   - Given: server_config table has rows for instance_name, oidc_issuer
   - When: `GetServerConfig` gRPC is called
   - Then: response contains current values (oidc_client_secret must NOT be returned in plaintext)

10. `UpdateServerConfig persists config changes` — ExUnit
    - Given: server_config has `instance_name="old"`
    - When: `UpdateServerConfig` gRPC is called with `instance_name="new"`
    - Then: server_config row updated in DB

11. `GetMetrics returns real counts` — ExUnit
    - Given: 2 active sessions exist in ETS, 5 rooms registered
    - When: `GetMetrics` gRPC is called
    - Then: `active_sessions=2`, `room_count=5` (not all zeroes as in the current stub)

12. `make proto generates stubs without compile errors` — shell test
    - Given: new proto messages and RPCs are added
    - When: `make proto` runs
    - Then: exit code 0; `gateway/internal/grpc/pb/core.pb.go` and `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` both contain the new RPC names

## Tasks / Subtasks

- [ ] Task 1 — Add new proto messages and RPCs to `proto/core.proto` (AC: 1, 5)
  - [ ] Add `ListAdminUsers` RPC + `ListAdminUsersRequest`/`ListAdminUsersResponse` + `AdminUserProto` message
  - [ ] Add `GetAdminUser` RPC + `GetAdminUserRequest`/`GetAdminUserResponse`
  - [ ] Add `DeactivateUser` RPC + `DeactivateUserRequest`/`DeactivateUserResponse`
  - [ ] Add `ReactivateUser` RPC + `ReactivateUserRequest`/`ReactivateUserResponse`
  - [ ] Add `UpdateUserRole` RPC + `UpdateUserRoleRequest`/`UpdateUserRoleResponse`
  - [ ] Add `ListAdminRooms` RPC + `ListAdminRoomsRequest`/`ListAdminRoomsResponse` + `AdminRoomProto` message
  - [ ] Add `GetAdminRoom` RPC + `GetAdminRoomRequest`/`GetAdminRoomResponse` + `AdminRoomDetailProto` message
  - [ ] Add `GetServerConfig` RPC + `GetServerConfigRequest`/`GetServerConfigResponse` + `ServerConfigProto` message
  - [ ] Add `UpdateServerConfig` RPC + `UpdateServerConfigRequest`/`UpdateServerConfigResponse`
  - [ ] Run `make proto` and verify 0 compile errors

- [ ] Task 2 — Implement `ListAdminUsers` + `GetAdminUser` in Elixir Core (AC: 2)
  - [ ] Add `list_admin_users/2` handler in `Nebu.EventDispatcher.Server`
  - [ ] Query `users` table (paginated by `user_id` cursor, search via ILIKE on display_name)
  - [ ] Email masking: return `u***@domain` pattern (consistent with Go UserRepository logic)
  - [ ] Add `get_admin_user/2` handler — return full user row or GRPC not_found

- [ ] Task 3 — Implement `DeactivateUser` + `ReactivateUser` in Elixir Core (AC: 3)
  - [ ] Add `deactivate_user/2`: set `is_active=false` in `users` table, then call `InvalidateUserSessions`
  - [ ] Add `reactivate_user/2`: set `is_active=true` in `users` table
  - [ ] Use `Ecto.Multi` so the DB update and audit log are atomic (let it crash on failure)
  - [ ] Call `session_supervisor_module().destroy_session/1` for the target user (already exists in the server for `invalidate_user_sessions/2`)

- [ ] Task 4 — Implement `UpdateUserRole` in Elixir Core (AC: 1)
  - [ ] Add `update_user_role/2`: update `users.system_role` directly (NOT role_overrides — those are Gateway-side overrides with TTL cache)
  - [ ] Validate role value is one of: `"user"`, `"instance_admin"`, `"compliance_officer"`; raise `invalid_argument` otherwise

- [ ] Task 5 — Implement `ListAdminRooms` + `GetAdminRoom` in Elixir Core (AC: 1)
  - [ ] Add `list_admin_rooms/2`: paginated query of `rooms` table (cursor by room_id), filter by status
  - [ ] Add `get_admin_room/2`: fetch room detail including member_count (from Room GenServer if running, else DB fallback)

- [ ] Task 6 — Implement `ArchiveRoom` with `SELECT FOR UPDATE` in Elixir Core (AC: 4)
  - [ ] The existing `archive_room/2` in `server.ex` only terminates the GenServer; this new version must ALSO atomically update `rooms.status='archived'` in DB via `SELECT FOR UPDATE` inside an Ecto transaction
  - [ ] Use `Ecto.Multi` with `lock("FOR UPDATE")` on the rooms row
  - [ ] After DB commit, terminate the GenServer (same as existing impl)
  - [ ] **IMPORTANT**: The existing `ArchiveRoom`/`UnarchiveRoom` RPCs are already wired in the proto and server — this task MODIFIES the existing `archive_room/2` to add the `SELECT FOR UPDATE` atomic DB update (AC: 4). The proto definition does NOT change for these two RPCs.

- [ ] Task 7 — Implement `GetServerConfig` + `UpdateServerConfig` in Elixir Core (AC: 1)
  - [ ] Add `get_server_config/2`: query `server_config` table for all keys, return `ServerConfigProto`
  - [ ] `oidc_client_secret` MUST NOT be returned in the gRPC response (security invariant from Story 6.10 — the value is AES-256-GCM encrypted in DB and only the Gateway has the key)
  - [ ] Add `update_server_config/2`: upsert `server_config` table rows for provided fields
  - [ ] Note: The Gateway already calls `CoreClient.InvalidateAllAdminSessions` after OIDC config changes (Story 6.10); Core only persists the data here

- [ ] Task 8 — Implement real `GetMetrics` in Elixir Core (AC: 1)
  - [ ] Replace the stub `get_metrics/2` (currently `%Core.GetMetricsResponse{}` with zeroes) with a real implementation
  - [ ] `active_sessions`: count active ETS sessions via `Nebu.Session.EtsStore.list_user_ids/0` (already used in `invalidate_all_admin_sessions/2`)
  - [ ] `room_count`: count active rooms from Horde Registry via `Horde.Registry.select/2` (already used in `subscribe_to_all_rooms/0`)
  - [ ] `msg_per_sec`: keep as 0.0 for MVP (rolling window not yet implemented — document this explicitly)

- [ ] Task 9 — Wire new Go gRPC client wrapper methods in `gateway/internal/grpc/client.go` (AC: 5)
  - [ ] Add `ListAdminUsers`, `GetAdminUser`, `DeactivateUser`, `ReactivateUser`, `UpdateUserRole` methods
  - [ ] Add `ListAdminRooms`, `GetAdminRoom` methods
  - [ ] Add `GetServerConfig`, `UpdateServerConfig` methods
  - [ ] Follow the exact pattern used by existing methods (thin wrapper around `c.core.XxxRPC`)

- [ ] Task 10 — Add ExUnit tests in `event_dispatcher` (AC: all)
  - [ ] Create test file `core/apps/event_dispatcher/test/admin_grpc_test.exs`
  - [ ] Follow the test patterns from existing `*_test.exs` files in `event_dispatcher/test/`
  - [ ] Use `Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)` for DB dependency injection (same pattern as `rooms_db_module`, `pg_store_module`, etc.)
  - [ ] Write all 12 acceptance tests from the Acceptance Tests section above

## Dev Notes

### Critical Context: What Already Exists vs. What This Story Adds

**Already in `proto/core.proto`:**
- `ArchiveRoom` / `UnarchiveRoom` — fully wired (Story 6.9)
- `GetMetrics` — defined but stub-only in Core (returns zeroes)
- `InvalidateUserSessions` — fully implemented in Core (Story 6.5)
- `InvalidateAllAdminSessions` — fully implemented in Core (Story 6.10)

**Already in `gateway/internal/api/server.go`:**
- `ListAdminUsers`, `GetAdminUser` — real Go implementations backed by `UserRepository` (PostgreSQL)
- `DeactivateUser`, `ReactivateUser` — real Go implementations with `InvalidateUserSessions` gRPC call
- `UpdateUserRole` — real Go implementation
- `ListAdminRooms`, `GetAdminRoom` — real Go implementations backed by `RoomRepository`
- `GetAdminConfig`, `PatchAdminConfig`, `GetAdminMetrics` — real Go implementations backed by repositories

**The Admin UI gateway (`gateway/internal/admin/users.go`, `rooms.go`) still uses in-memory stubs** — those will be replaced in Stories 9.2-9.4. This story is about adding the Core gRPC layer so Stories 9.2+ have something to call.

**What this story adds:**
- New proto messages + RPCs: `ListAdminUsers`, `GetAdminUser`, `DeactivateUser`, `ReactivateUser`, `UpdateUserRole`, `ListAdminRooms`, `GetAdminRoom`, `GetServerConfig`, `UpdateServerConfig`
- Real Elixir Core implementations for all the above
- Real `GetMetrics` implementation (replacing the empty stub)
- Modified `ArchiveRoom` to include `SELECT FOR UPDATE` atomic DB update (AC: 4)
- Go client wrapper methods in `gateway/internal/grpc/client.go`

### Proto Schema Design

New messages to add to `proto/core.proto` (append after the existing `InvalidateAllAdminSessions` messages):

```protobuf
// AdminUserProto — user record for admin RPCs
message AdminUserProto {
  string user_id      = 1;
  string display_name = 2;
  string email_masked = 3;  // masked: u***@domain
  bool   is_active    = 4;
  string system_role  = 5;  // "user" | "instance_admin" | "compliance_officer"
  int64  created_at   = 6;  // Unix milliseconds
}

// ListAdminUsers
message ListAdminUsersRequest {
  int32  limit  = 1;   // 1–100, default 20
  string cursor = 2;   // opaque cursor (user_id:created_at); empty = first page
  string search = 3;   // ILIKE substring match on display_name; empty = no filter
}
message ListAdminUsersResponse {
  repeated AdminUserProto users       = 1;
  int32                   total       = 2;
  string                  next_cursor = 3;  // empty = no more pages
}

// GetAdminUser
message GetAdminUserRequest  { string user_id = 1; }
message GetAdminUserResponse { AdminUserProto user = 1; }

// DeactivateUser
message DeactivateUserRequest  { string user_id = 1; }
message DeactivateUserResponse { bool ok = 1; }

// ReactivateUser
message ReactivateUserRequest  { string user_id = 1; }
message ReactivateUserResponse { bool ok = 1; }

// UpdateUserRole
message UpdateUserRoleRequest  {
  string user_id = 1;
  string role    = 2;  // "user" | "instance_admin" | "compliance_officer"
}
message UpdateUserRoleResponse { bool ok = 1; }

// AdminRoomProto — room summary for admin RPCs
message AdminRoomProto {
  string room_id      = 1;
  string name         = 2;
  string status       = 3;  // "active" | "archived"
  int32  member_count = 4;
  int64  created_at   = 5;  // Unix milliseconds
}

// AdminRoomDetailProto — full room detail
message AdminRoomDetailProto {
  string room_id      = 1;
  string name         = 2;
  string status       = 3;
  int32  member_count = 4;
  int32  max_members  = 5;  // 0 = no limit
  string visibility   = 6;  // "public" | "private"
  int64  created_at   = 7;
}

// ListAdminRooms
message ListAdminRoomsRequest {
  int32  limit         = 1;
  string cursor        = 2;   // empty = first page
  string status_filter = 3;   // "active" | "archived" | "" (all)
  string search        = 4;
}
message ListAdminRoomsResponse {
  repeated AdminRoomProto rooms       = 1;
  int32                   total       = 2;
  string                  next_cursor = 3;
}

// GetAdminRoom
message GetAdminRoomRequest  { string room_id = 1; }
message GetAdminRoomResponse { AdminRoomDetailProto room = 1; }

// ServerConfigProto — server configuration (oidc_client_secret intentionally absent)
message ServerConfigProto {
  string instance_name            = 1;
  string oidc_issuer              = 2;
  string oidc_client_id           = 3;
  int32  room_default_max_members = 4;
  string room_default_visibility  = 5;
  int32  audit_log_retention_days = 6;
}

// GetServerConfig
message GetServerConfigRequest  {}
message GetServerConfigResponse { ServerConfigProto config = 1; }

// UpdateServerConfig — all fields optional (empty string = do not update)
message UpdateServerConfigRequest {
  string instance_name            = 1;
  string oidc_issuer              = 2;
  string oidc_client_id           = 3;
  int32  room_default_max_members = 4;
  string room_default_visibility  = 5;
  int32  audit_log_retention_days = 6;
}
message UpdateServerConfigResponse { bool ok = 1; }
```

### Elixir Core — gRPC Server Architecture

All handlers go in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (the single `Nebu.EventDispatcher.Server` module). This is the established pattern — do NOT create a separate admin server module.

The server uses injectable DB modules via `Application.get_env`:
```elixir
# Pattern for new admin DB module:
defp admin_db_module do
  Application.get_env(:event_dispatcher, :admin_db_module, Nebu.Admin.DB)
end
```

Create a new module `Nebu.Admin.DB` in the `event_dispatcher` OTP app (not session_manager or room_manager), since event_dispatcher already owns the gRPC server.

**File location:** `core/apps/event_dispatcher/lib/nebu/admin/db.ex`

### PostgreSQL Tables Available for Admin Queries

From existing migrations:
- `users` table (migration 000001): `user_id`, `display_name_encrypted` (Tier 1 PII), `email_encrypted` (Tier 2 PII), `is_active` (migration 000034), `system_role` (from token_validator), `created_at`
- `rooms` table (migration 000036 `rooms_admin_columns`): `room_id`, `name`, `status` ("active"/"archived"), `max_members`, `visibility`, `created_at`
- `room_defaults` table (migration 000037): `max_members`, `visibility`
- `server_config` table (migration 000005): `key`, `value` (all text KV pairs)
- `role_overrides` table (migration 000035): `user_id`, `role`, `granted_at`

**Critical PII note:** `display_name_encrypted` and `email_encrypted` are AES-256-GCM encrypted (Story 2.10/2.11). In Elixir, use `Nebu.Crypto.decrypt_operational_pii/1` for display_name (Tier 1 / operational PII) and `Nebu.Crypto.decrypt_sensitive_pii/1` for email (Tier 2). See existing usage in `Nebu.Session.UserStore.Postgres`.

**Email masking:** The existing Go `UserRepository` masks email as `u***@domain`. Match this pattern in Core for `ListAdminUsers`.

### DB Dependency Injection Pattern (Mandatory)

All DB calls in the server must use the injectable module pattern — no hardcoded module calls:

```elixir
defp admin_db_module do
  Application.get_env(:event_dispatcher, :admin_db_module, Nebu.Admin.DB)
end
```

In tests, override via:
```elixir
Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)
```

This pattern is used for `rooms_db_module`, `pg_store_module`, `session_supervisor_module`, etc. — follow it without deviation.

### ArchiveRoom Atomic Update (AC: 4)

The existing `archive_room/2` handler (Story 6.9) only terminates the GenServer. AC: 4 requires `SELECT FOR UPDATE` for the DB write:

```elixir
# Atomic archive using Ecto.Multi + SELECT FOR UPDATE:
def archive_room_atomically(room_id) do
  Nebu.DB.Repo.transaction(fn ->
    room = from(r in "rooms",
      where: r.room_id == ^room_id,
      lock: "FOR UPDATE",
      select: r.status
    ) |> Nebu.DB.Repo.one()

    case room do
      nil -> Nebu.DB.Repo.rollback(:not_found)
      "archived" -> :ok  # idempotent
      _ ->
        from(r in "rooms", where: r.room_id == ^room_id)
        |> Nebu.DB.Repo.update_all(set: [status: "archived"])
    end
  end)
end
```

This replaces the existing best-effort-only `archive_room/2`. After the transaction commits, terminate the GenServer as before.

**NOTE:** The existing `ArchiveRoom` proto and handler were added for the Go Gateway to call after it has already done the DB update. AC: 4 inverts this: Core now does the atomic DB write itself. This is a contract change. Story 9.2 (Admin UI integration) will need to adapt the gateway call sequence accordingly. Document this in the handler with a comment.

### GetMetrics Real Implementation

Replace the stub in `server.ex`:
```elixir
# Before (stub):
def get_metrics(_request, _stream) do
  %Core.GetMetricsResponse{}
end

# After (real):
def get_metrics(_request, _stream) do
  active_sessions = Nebu.Session.EtsStore.list_user_ids() |> length()
  room_count =
    try do
      Horde.Registry.select(Nebu.Room.Registry, [{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
      |> length()
    rescue _ -> 0
    catch _, _ -> 0
    end
  %Core.GetMetricsResponse{
    active_sessions: active_sessions,
    room_count: room_count,
    msg_per_sec: 0.0  # rolling window not yet implemented
  }
end
```

### Go Client Wrapper Pattern

All new methods in `gateway/internal/grpc/client.go` follow the exact same pattern:

```go
// ListAdminUsers calls the Elixir core to list admin users with pagination.
func (c *Client) ListAdminUsers(ctx context.Context, req *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error) {
    return c.core.ListAdminUsers(ctx, req)
}
```

Add methods for all 9 new RPCs. Do not add business logic in client.go — it is a thin wrapper only.

### `make proto` Generation

The `buf.gen.yaml` configuration generates:
- Go stubs → `gateway/internal/grpc/pb/core.pb.go` (Go Gateway client)
- Elixir stubs → `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (Core server skeleton)

Run `make proto` after adding new RPCs. The generated Elixir `core_grpc.pb.ex` will contain the new RPC registration — the `Nebu.EventDispatcher.Server` module MUST implement all registered RPCs or the gRPC server will fail to compile/load.

Check existing `core_grpc.pb.ex` to see how RPCs are listed — each new RPC needs a corresponding `def rpc_name/2` in the server.

### Auth Interceptor — RPCs Need Registration

From Story 7-16d: all RPCs go through the `Nebu.Grpc.AuthInterceptor` — the interceptor was patched in that story to cover all RPCs. Since all new RPCs are added to the same `CoreService`, they are automatically covered by the interceptor. No extra registration needed.

For admin RPCs, the caller identity check should verify `system_role == "instance_admin"` (extracted via `Nebu.Grpc.Metadata.trusted_identity/1`). The Go Gateway's `RequireRole` middleware already enforces this at the HTTP layer, so the gRPC check is defense-in-depth.

### Security Checklist (SEC Gate 1)

This story touches gRPC handlers and admin auth patterns. Key security invariants:

1. `oidc_client_secret` MUST NOT appear in `GetServerConfig` response (it is AES-256-GCM encrypted in DB and only the Go Gateway has the decryption key)
2. `email_encrypted` MUST be masked in `ListAdminUsers` (return `u***@domain`, not plaintext)
3. `system_role` check: admin RPCs must verify `system_role == "instance_admin"` from gRPC metadata
4. `DeactivateUser` must call `InvalidateUserSessions` AFTER the DB commit (not before) — use `Ecto.Multi` to sequence correctly
5. `UpdateUserRole` must validate role value — raise `invalid_argument` for unknown roles

### File List (Create / Update)

**proto (UPDATE):**
- `proto/core.proto` — add 9 new RPC definitions + message types

**Elixir Core (CREATE):**
- `core/apps/event_dispatcher/lib/nebu/admin/db.ex` — new `Nebu.Admin.DB` module (user + room + config queries)

**Elixir Core (UPDATE):**
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — add 9 new handlers + replace `get_metrics/2` stub + modify `archive_room/2`

**Elixir Core (CREATE):**
- `core/apps/event_dispatcher/test/admin_grpc_test.exs` — acceptance tests for all new handlers

**Go Gateway (UPDATE):**
- `gateway/internal/grpc/client.go` — add 9 new wrapper methods
- `gateway/internal/grpc/pb/core.pb.go` — auto-generated by `make proto` (do not edit manually)

**Elixir (UPDATE, auto-generated):**
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — auto-generated by `make proto` (do not edit manually)

### Project Structure Notes

- All Elixir gRPC handlers live in the single `Nebu.EventDispatcher.Server` module — do NOT create a separate admin module or a second GRPC.Server
- The `event_dispatcher` OTP app already depends on `session_manager` and `room_manager` — no new `mix.exs` dependencies needed
- `Nebu.Admin.DB` belongs in `event_dispatcher` (not a new OTP app), since event_dispatcher is the gRPC server app
- Proto message names must not conflict with existing names — check `core.proto` before adding

### References

- `proto/core.proto` — existing RPCs: ArchiveRoom (line 95), UnarchiveRoom (line 100), GetMetrics (line 25), InvalidateUserSessions (line ~76), InvalidateAllAdminSessions (line ~103)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — `archive_room/2` (line 1604), `unarchive_room/2` (line 1635), `get_metrics/2` (line 766), `invalidate_user_sessions/2` (line 721), `invalidate_all_admin_sessions/2` (line 746)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex` — gRPC endpoint with AuthInterceptor registration
- `gateway/internal/grpc/client.go` — Go client wrapper methods (thin wrappers, ~5 lines each)
- `gateway/internal/api/server.go` — `ListAdminUsers` (line 470), `GetAdminUser` (line 527), `DeactivateUser` (line ~630), gRPC pattern for `InvalidateUserSessions` call (line ~672)
- `gateway/internal/admin/users.go` — in-memory stub pattern (will be replaced in Story 9.2)
- `gateway/migrations/000034_users_deactivation.up.sql` — `is_active` column
- `gateway/migrations/000035_role_overrides.up.sql` — `role_overrides` table
- `gateway/migrations/000036_rooms_admin_columns.up.sql` — `rooms` admin columns
- `gateway/migrations/000037_room_defaults.up.sql` — `room_defaults` table
- Story 6.5 (`_bmad-output/implementation-artifacts/6-5-*.md`) — `DeactivateUser` + `InvalidateUserSessions` pattern for Go gateway side
- Story 7-16d (`done`) — gRPC auth interceptor covers all RPCs automatically

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

- `proto/core.proto` (UPDATE)
- `core/apps/event_dispatcher/lib/nebu/admin/db.ex` (CREATE)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (UPDATE)
- `core/apps/event_dispatcher/test/admin_grpc_test.exs` (CREATE)
- `gateway/internal/grpc/client.go` (UPDATE)
- `gateway/internal/grpc/pb/core.pb.go` (auto-generated, do not edit)
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (auto-generated, do not edit)
