---
status: ready-for-dev
epic: 14
story: 3a
security_review: required
matrix: false
ui: false
---

# Story 14.3a: BulkImportUsers gRPC RPC + Core Provisioning

Status: ready-for-dev

## Story

As an instance admin,
I want a `BulkImportUsers` gRPC RPC in Core that provisions users from OIDC claim maps with the same flow as first login,
So that bulk user import produces identical user records to organic first-login provisioning.

**Size:** S
**security_review:** required

---

## Acceptance Criteria

**AC1 — Proto compiled successfully:**
Given `proto/core.proto` is updated with `BulkImportUsers`,
When `make proto` runs,
Then `BulkImportUsers(BulkImportUsersRequest) returns (BulkImportUsersResponse)` is generated without errors; the request contains a list of OIDC claim maps (repeated `OIDCUserClaims`) and the response contains `imported`, `skipped`, and `failed` counts (int32).

**AC2 — User provisioning identical to validate_token flow:**
Given `BulkImportUsers` is called with a list of OIDC user claim maps,
When Core processes each user,
Then for each user: a user record is created in PostgreSQL via `UserStore.upsert_user`, Ed25519 + X25519 keypairs are generated, PII is encrypted — identical to the flow in `validate_token/2` / `provision_new_user`.

**AC3 — Duplicate users are skipped:**
Given a user already exists in the Nebu DB (previously logged in — has `signing_key_id` set),
When their claims appear in the import list,
Then their record is skipped (no error, no duplicate insert) and the response shows `skipped: N`.

**AC4 — ExUnit tests pass:**
Given ExUnit tests for the Core bulk importer,
When `make test-unit-elixir` runs,
Then the following test cases pass: single user import, duplicate skip, bulk import of 10 users, keypair generation correctness.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. test "BulkImportUsers — single user import creates user record and keypairs" — ExUnit**
Location: `core/apps/event_dispatcher/test/event_dispatcher/bulk_import_users_test.exs`
- Given: `FakeBulkImporter` is configured; DB has no user for `@alice:nebu.local`
- When: `Nebu.Session.BulkImporter.import_users([%{user_id: "@alice:nebu.local", system_role: "user", display_name: "Alice", email: "alice@example.com"}])` is called
- Then: returns `{:ok, %{imported: 1, skipped: 0, failed: 0}}`

**2. test "BulkImportUsers — duplicate user is skipped, not an error" — ExUnit**
Location: `core/apps/event_dispatcher/test/event_dispatcher/bulk_import_users_test.exs`
- Given: DB already has user `@bob:nebu.local` with `signing_key_id` set (fully provisioned)
- When: `import_users([%{user_id: "@bob:nebu.local", system_role: "user", display_name: "Bob", email: "bob@example.com"}])` is called
- Then: returns `{:ok, %{imported: 0, skipped: 1, failed: 0}}`

**3. test "BulkImportUsers — bulk import of 10 users returns correct counts" — ExUnit**
Location: `core/apps/event_dispatcher/test/event_dispatcher/bulk_import_users_test.exs`
- Given: 8 new users + 2 existing (with signing_key_id set) in the import list
- When: `import_users(10 user claim maps)` is called
- Then: returns `{:ok, %{imported: 8, skipped: 2, failed: 0}}`

**4. test "BulkImportUsers — keypair generation correctness" — ExUnit**
Location: `core/apps/event_dispatcher/test/event_dispatcher/bulk_import_users_test.exs`
- Given: Postgres-backed provisioner (integration test with DB) provisions a new user
- When: `UserProvisioner.Postgres.provision_user/4` runs for `@carol:nebu.local`
- Then: a signing keypair (Ed25519, 64-byte public key) and encryption keypair (X25519, 32-byte public key) are stored in `user_keys` table; `users.signing_key_id` and `users.encryption_key_id` are non-nil

---

## Dev Notes

### Architecture Overview

This story adds:

1. **Proto change**: `BulkImportUsers` RPC + `OIDCUserClaims` message + `BulkImportUsersRequest` / `BulkImportUsersResponse` in `proto/core.proto`
2. **Elixir module**: `Nebu.Session.BulkImporter` — thin orchestrator that calls the same `UserStore.upsert_user` + `UserProvisioner.provision_user` flow as `TokenValidator.Postgres.provision_new_user`
3. **gRPC handler**: `bulk_import_users/2` added to `Nebu.EventDispatcher.Server`

### Proto Changes — Exact Field Names

Add to `proto/core.proto` in the `CoreService` service block:

```protobuf
// BulkImportUsers — Story 14-3a: provisions OIDC users identical to first-login flow.
// Each entry is processed in sequence; duplicates (already in DB with keypairs) are skipped.
// Returns aggregate counts: imported + skipped + failed = len(users).
rpc BulkImportUsers(BulkImportUsersRequest) returns (BulkImportUsersResponse);
```

And add these messages near the end of the file:

```protobuf
// OIDCUserClaims — one user's OIDC claim map for bulk import.
// user_id: Matrix user ID derived from the configured matrix_user_id_claim
//          (e.g. "@alice:example.com") — computed by the Go gateway before sending.
// system_role: always "user" for bulk import (no admin bootstrap path).
// display_name: from preferred_username OIDC claim (Tier 1 PII).
// email: from email OIDC claim (Tier 2 PII).
message OIDCUserClaims {
  string user_id      = 1;
  string system_role  = 2;
  string display_name = 3;
  string email        = 4;
}

// BulkImportUsersRequest — Story 14-3a
message BulkImportUsersRequest {
  repeated OIDCUserClaims users = 1;
}

// BulkImportUsersResponse — Story 14-3a
// imported: users newly created in this call.
// skipped: users already in DB with full keypairs (no-op, not an error).
// failed: users that encountered a DB/crypto error (partial success allowed).
message BulkImportUsersResponse {
  int32 imported = 1;
  int32 skipped  = 2;
  int32 failed   = 3;
}
```

### New Elixir Module: Nebu.Session.BulkImporter

File: `core/apps/session_manager/lib/nebu/session/bulk_importer.ex`

```elixir
defmodule Nebu.Session.BulkImporter do
  @moduledoc """
  Provisions a list of OIDC users with the same flow as first login.

  For each user:
  1. Lookup user in DB — if already provisioned (signing_key_id NOT NULL) → skip.
  2. Upsert user record (UserStore.upsert_user/2) → insert on first import.
  3. Provision keypairs + encrypt PII (UserProvisioner.provision_user/4) → identical to validate_token.

  Returns {:ok, %{imported: N, skipped: N, failed: N}}.
  Partial success: an error on one user does not abort others.
  """

  @type claims :: %{
    user_id: String.t(),
    system_role: String.t(),
    display_name: String.t(),
    email: String.t()
  }

  @type result :: {:ok, %{imported: non_neg_integer(), skipped: non_neg_integer(), failed: non_neg_integer()}}

  @spec import_users([claims()]) :: result()
  def import_users(users) when is_list(users) do
    server_key = Application.get_env(:signature, :pii_encryption_key)
    init = %{imported: 0, skipped: 0, failed: 0}

    result =
      Enum.reduce(users, init, fn user, acc ->
        case import_one(user, server_key) do
          :imported -> Map.update!(acc, :imported, &(&1 + 1))
          :skipped  -> Map.update!(acc, :skipped, &(&1 + 1))
          :failed   -> Map.update!(acc, :failed, &(&1 + 1))
        end
      end)

    {:ok, result}
  end

  defp import_one(%{user_id: user_id, system_role: role, display_name: dn, email: email}, server_key) do
    case lookup_provisioned(user_id) do
      :already_provisioned ->
        :skipped

      :not_provisioned ->
        with {:ok, _} <- user_store_module().upsert_user(user_id, role),
             {:ok, :provisioned} <- provisioner_module().provision_user(user_id, dn, email, server_key) do
          :imported
        else
          {:error, reason} ->
            Logger.error("BulkImporter: failed to import #{user_id}: #{inspect(reason)}")
            :failed
        end

      {:error, reason} ->
        Logger.error("BulkImporter: lookup failed for #{user_id}: #{inspect(reason)}")
        :failed
    end
  end

  defp lookup_provisioned(user_id) do
    # Same SQL as TokenValidator.Postgres — reuse to stay consistent.
    sql = "SELECT signing_key_id FROM users WHERE user_id = $1"
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, [user_id]) do
      {:ok, %{rows: []}} -> :not_provisioned
      {:ok, %{rows: [[nil]]}} -> :not_provisioned        # exists but not yet provisioned
      {:ok, %{rows: [[_key_id]]}} -> :already_provisioned # fully provisioned → skip
      {:error, reason} -> {:error, reason}
    end
  end

  defp user_store_module do
    Application.get_env(:session_manager, :bulk_importer_user_store_module, Nebu.Session.UserStore.Postgres)
  end

  defp provisioner_module do
    Application.get_env(:session_manager, :bulk_importer_provisioner_module, Nebu.Session.UserProvisioner.Postgres)
  end
end
```

**Important:** Use `require Logger` at the top of the module.

### gRPC Handler in EventDispatcher.Server

Add a configurable module accessor for `BulkImporter` at the top of `server.ex` (same pattern as all other modules):

```elixir
defp bulk_importer_module do
  Application.get_env(:event_dispatcher, :bulk_importer_module, Nebu.Session.BulkImporter)
end
```

Add the handler function:

```elixir
# BulkImportUsers — Story 14-3a: admin bulk user provisioning
def bulk_import_users(%Core.BulkImportUsersRequest{} = request, _stream) do
  users =
    Enum.map(request.users, fn claims ->
      %{
        user_id:      claims.user_id,
        system_role:  claims.system_role,
        display_name: claims.display_name,
        email:        claims.email
      }
    end)

  case bulk_importer_module().import_users(users) do
    {:ok, %{imported: imported, skipped: skipped, failed: failed}} ->
      %Core.BulkImportUsersResponse{
        imported: imported,
        skipped:  skipped,
        failed:   failed
      }
  end
end
```

**Note:** The handler returns success even if some users failed (partial import). The failed count is surfaced to the caller. Only a catastrophic error (unrecoverable exception) would raise a GRPC.RPCError.

### Proto Generation

After editing `proto/core.proto`, run:

```bash
make proto
```

This runs `buf generate` in a container. The generated Elixir module `Core.BulkImportUsersRequest`, `Core.BulkImportUsersResponse`, and `Core.OIDCUserClaims` will appear in `core/apps/event_dispatcher/lib/pb/core.pb.ex`.

**Do not manually edit `core.pb.ex` or `core_grpc.pb.ex`** — they are auto-generated.

After generation, also check the Go side: `gateway/internal/grpc/pb/core.pb.go` will contain the Go struct `BulkImportUsersRequest` etc. The Go gateway does not need to use this RPC yet (Story 14.3b adds the HTTP handler), but the generated stubs must compile cleanly.

### Provisioning Flow — Identical to First Login

The existing validate_token path in `TokenValidator.Postgres.provision_new_user` does:
1. `BootstrapChecker.upsert_with_bootstrap(user_id, system_role)` → creates `users` row, handles bootstrap admin logic
2. `UserProvisioner.provision_user(user_id, display_name, email, server_key)` → keypairs + encrypted PII

For `BulkImporter`, we skip the bootstrap check (bulk import always uses role "user", never triggers bootstrap). Instead:
1. `UserStore.upsert_user(user_id, system_role)` → same `INSERT ... ON CONFLICT` SQL as `UserStore.Postgres`
2. `UserProvisioner.provision_user(user_id, display_name, email, server_key)` → same keypair + PII flow

The `UserProvisioner.provision_user` is **idempotent** (`UPDATE ... WHERE signing_key_id IS NULL`), so calling it twice on an existing user is safe — the `UPDATE` is a no-op and no error is returned.

### Duplicate Detection Strategy

A user is "already provisioned" if `signing_key_id IS NOT NULL` in the `users` table. This is the same invariant `TokenValidator.Postgres` uses:
- `signing_key_id IS NULL` → `provision_existing_user` (edge case: user row exists but no keypairs)
- `signing_key_id IS NOT NULL` → `decrypt_and_return` (fully provisioned)

In `BulkImporter`, a user whose `signing_key_id IS NOT NULL` is counted as **skipped**. A user with `signing_key_id IS NULL` (exists but not yet provisioned) is re-provisioned.

### Test Structure

Tests must follow the existing `async: false` pattern (Application.put_env is process-global).

File: `core/apps/event_dispatcher/test/event_dispatcher/bulk_import_users_test.exs`

Test module structure:
```elixir
defmodule Nebu.Session.BulkImporterTest do
  use ExUnit.Case, async: false

  # FakeUserStore — tracks upsert calls
  # FakeProvisioner — tracks provision_user calls
  # FakeAlreadyProvisionedStore — returns signing_key_id set

  setup do
    Application.put_env(:session_manager, :bulk_importer_user_store_module, FakeUserStore)
    Application.put_env(:session_manager, :bulk_importer_provisioner_module, FakeProvisioner)
    on_exit(fn ->
      Application.delete_env(:session_manager, :bulk_importer_user_store_module)
      Application.delete_env(:session_manager, :bulk_importer_provisioner_module)
    end)
    :ok
  end
end
```

For the **keypair generation correctness** test (test 4 above), it requires a real DB connection. Check if existing integration tests in `core/apps/session_manager/test/` use `Nebu.Repo` — if so, follow the same `DataCase` / `Nebu.RepoCase` setup. If no DB test infrastructure exists for this module, write it as a unit test using `Nebu.Signature` directly (call `generate_signing_keypair/0` and `generate_encryption_keypair/0` and verify key sizes).

### File Locations

| File | Action |
|------|--------|
| `proto/core.proto` | UPDATE — add RPC + 3 messages |
| `core/apps/session_manager/lib/nebu/session/bulk_importer.ex` | NEW |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | UPDATE — add `bulk_importer_module/0` accessor + `bulk_import_users/2` handler |
| `core/apps/event_dispatcher/lib/pb/core.pb.ex` | AUTO-GENERATED by `make proto` — do not edit |
| `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` | AUTO-GENERATED by `make proto` — do not edit |
| `gateway/internal/grpc/pb/core.pb.go` | AUTO-GENERATED by `make proto` — do not edit |
| `core/apps/event_dispatcher/test/event_dispatcher/bulk_import_users_test.exs` | NEW |

### Security Constraints (SEC Gate 1 applies)

- **PII handling**: `display_name` is Tier 1 (operational PII, AES-256-GCM with server key). `email` is Tier 2 (sensitive PII, X25519 ECDH + AES-256-GCM). Both encrypted before DB write via existing `Nebu.Signature` functions — identical to first-login.
- **No token forwarding**: `BulkImportUsers` does not accept or forward OIDC tokens. Claims arrive already validated by Go gateway.
- **Rate limiting / auth**: The Go gateway handler (Story 14.3b) is responsible for admin-only access gate. The Core RPC trusts the Go gateway (same trust model as all other RPCs — ADR G2).
- **No logging of PII**: Never log `display_name`, `email`, or `user_id` in error paths beyond what is already logged by existing provisioner code.
- **Partial success**: Do not abort the entire batch on a single user failure. Count, log, continue.

### Conventions from Existing Code

- **Module config pattern**: Always use `Application.get_env(:app_name, :module_key, DefaultModule)` — never hardcode module names in production code. This enables test injection.
- **Error logging**: `Logger.error("context: #{inspect(reason)}")` — never `raise` in the reduce loop.
- **Transaction scope**: `Nebu.Repo.transaction` is handled inside `UserProvisioner.Postgres.provision_user` — no need to wrap in another transaction in `BulkImporter`.
- **SQL style**: Use raw `Ecto.Adapters.SQL.query(Nebu.Repo, sql, params)` (same as all other Core DB modules). No Ecto schema structs.

---

## Prerequisites / Dependencies

- Story 14.2a must be complete (`oidc_directory_enabled` + `oidc_directory_endpoint` in DB) ✅ Done
- Story 14.2b must be complete (OIDCDirectoryService) ✅ Done
- Story 14.2c must be complete (Admin UI user search) ✅ Done
- `Nebu.Session.UserProvisioner` (behaviour + Postgres impl) ✅ Exists
- `Nebu.Session.UserStore` (behaviour + Postgres impl) ✅ Exists
- `Nebu.Signature.generate_signing_keypair/0` + `generate_encryption_keypair/0` ✅ Exists

---

## Out of Scope

- Go gateway HTTP handler for triggering bulk import (Story 14.3b)
- Bootstrap Wizard UI for bulk import (Story 14.3b)
- SCIM 2.0 protocol (separate story 14.4+)
- Role escalation during bulk import (always "user")
- Progress streaming / WebSocket updates
