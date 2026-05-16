---
status: done
epic: 14
story: 1a
security_review: required
matrix: false
ui: false
---

# Story 14.1a: Core — FAILED_PRECONDITION for matrix_user_id_claim Post-Bootstrap

Status: done

## Story

As an instance admin,
I want the Core `UpdateServerConfig` gRPC RPC to reject changes to `matrix_user_id_claim` after bootstrap is completed,
So that the server-side enforcement prevents accidental corruption of Matrix User IDs regardless of which client calls the API.

**Size:** S

**Background:**

The `oidc_user_id_claim` key in `server_config` controls which OIDC token claim is used to derive Matrix User IDs (e.g., `@sub:example.com` vs `@email:example.com`). Changing this claim after users have already logged in would corrupt their Matrix User IDs — existing rooms, events, and memberships would be orphaned.

The bootstrap flow uses a `bootstrap_completed` key in `server_config` (set to `'true'` by `bootstrap_checker/postgres.ex`) to record that at least one admin user has logged in. Once this key is present and non-null, changing `oidc_user_id_claim` must be blocked.

The proto field name in `UpdateServerConfigRequest` for this story is `matrix_user_id_claim` (new field to be added to the proto). The DB key is `oidc_user_id_claim`.

**Pre-Story context:**
- `bootstrap_completed` key is written by `Nebu.Session.BootstrapChecker.Postgres` via `@complete_sql`
- `oidc_user_id_claim` is stored in `server_config` table (written by migration `000044`)
- `UpdateServerConfig` gRPC handler is in `Nebu.EventDispatcher.Server.update_server_config/2`
- Admin DB callbacks are in `Nebu.Admin.DB` (behaviour + PostgreSQL implementation)

---

## Acceptance Criteria

**AC1 — Post-bootstrap claim change blocked:**
Given `bootstrap_completed` key IS NOT NULL in `server_config`,
When `UpdateServerConfig` gRPC is called with a `matrix_user_id_claim` field (non-empty),
Then the RPC returns `GRPC.RPCError` with status `GRPC.Status.failed_precondition()` and the database is NOT modified

**AC2 — Pre-bootstrap claim change allowed:**
Given `bootstrap_completed` key IS NULL / does not exist in `server_config`,
When `UpdateServerConfig` gRPC is called with a `matrix_user_id_claim` field (non-empty),
Then the update is accepted, `oidc_user_id_claim` is saved in DB, and the RPC returns `UpdateServerConfigResponse{ok: true}`

**AC3 — Non-claim field update succeeds post-bootstrap:**
Given `bootstrap_completed` key IS NOT NULL in `server_config`,
When `UpdateServerConfig` gRPC is called with only non-claim fields (e.g., `oidc_issuer`, `instance_name`),
Then the update succeeds and the RPC returns `UpdateServerConfigResponse{ok: true}`

**AC4 — ExUnit tests pass:**
Given the ExUnit test suite for `Nebu.EventDispatcher.Server.update_server_config/2`,
When `make test-unit-elixir` runs,
Then the following test cases all pass:
  - Claim change blocked post-bootstrap (AC1)
  - Claim change allowed pre-bootstrap (AC2)
  - Other fields changeable post-bootstrap (AC3)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. ExUnit test: `claim change blocked post-bootstrap` — FAILING**
- Given: `bootstrap_completed = 'true'` and `oidc_user_id_claim = 'sub'` in FakeAdminDB config
- When: `Server.update_server_config(%Core.UpdateServerConfigRequest{matrix_user_id_claim: "email"}, stream)` is called
- Then: raises `GRPC.RPCError` with `status: GRPC.Status.failed_precondition()` (code 9)
- And: `oidc_user_id_claim` in FakeAdminDB is still `'sub'` (unchanged)

**2. ExUnit test: `claim change allowed pre-bootstrap` — FAILING**
- Given: no `bootstrap_completed` key in FakeAdminDB config
- When: `Server.update_server_config(%Core.UpdateServerConfigRequest{matrix_user_id_claim: "email"}, stream)` is called
- Then: returns `%Core.UpdateServerConfigResponse{ok: true}`
- And: `oidc_user_id_claim` in FakeAdminDB is now `'email'`

**3. ExUnit test: `non-claim field update succeeds post-bootstrap` — FAILING**
- Given: `bootstrap_completed = 'true'` in FakeAdminDB config
- When: `Server.update_server_config(%Core.UpdateServerConfigRequest{oidc_issuer: "https://sso.example.com"}, stream)` is called
- Then: returns `%Core.UpdateServerConfigResponse{ok: true}`
- And: `oidc_issuer` in FakeAdminDB is now `'https://sso.example.com'`

### Test file location:

New test cases added to existing file:
`core/apps/event_dispatcher/test/nebu/event_dispatcher/admin_grpc_test.exs`

### Proto change required:

Add field `matrix_user_id_claim` to `UpdateServerConfigRequest` in `proto/core.proto`, then run `make proto`.

---

## Dev Notes

### Implementation plan:

1. **Proto change** (`proto/core.proto`):
   - Add `string matrix_user_id_claim = 7;` to `UpdateServerConfigRequest`
   - Run `make proto` to regenerate `core/apps/event_dispatcher/lib/pb/core.pb.ex` and Go stubs

2. **Elixir Core handler** (`core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`):
   - In `update_server_config/2`, after building the `changes` list, check:
     - If `changes` contains `"oidc_user_id_claim"` (i.e., `req.matrix_user_id_claim` is non-empty):
       - Call `admin_db_module().get_server_config()` to read current config
       - If `Map.get(config, "bootstrap_completed")` is non-nil → raise `GRPC.RPCError` with `status: GRPC.Status.failed_precondition()`, `message: "matrix_user_id_claim cannot be changed after bootstrap"`
   - Add `matrix_user_id_claim` to the `maybe_add_change` chain, mapped to DB key `"oidc_user_id_claim"`

3. **FakeAdminDB** (test fake in `admin_grpc_test.exs`):
   - `get_server_config/0` already returns ETS config — no changes needed

4. **No DB migration needed** — `oidc_user_id_claim` key already exists in `server_config` (migration 000044)

### GRPC.Status codes:
- `GRPC.Status.failed_precondition()` returns integer code `9`
- Pattern: `raise GRPC.RPCError, status: GRPC.Status.failed_precondition(), message: "..."`

### bootstrap_completed key handling:
- Set by `BootstrapChecker.Postgres` → stored as string `'true'` in value column
- A nil/missing `bootstrap_completed` key means not-yet-bootstrapped
- Check: `Map.get(config, "bootstrap_completed")` — nil means pre-bootstrap, any value means post-bootstrap
