# Story 2.14: ValidateToken gRPC Handler

Status: done

## Story

As a developer,
I want the Elixir `ValidateToken` gRPC handler to look up or provision users and return their identity,
so that the Go gateway can confirm user identity on every Matrix API request.

## Acceptance Criteria

1. **Given** a `ValidateToken` gRPC request with `user_id` and `system_role` in metadata,
   **When** the user exists in the DB with `is_active = true`,
   **Then** the handler returns a `ValidateTokenResponse` with `user_id`, `system_role`, `display_name` (decrypted), and `is_active: true`

2. **Given** a `ValidateToken` request for a user that does not yet exist,
   **When** processed,
   **Then** the handler triggers Stories 2.12 + 2.13 provisioning and returns the new user's data

3. **Given** a `ValidateToken` request for a user with `is_active = false`,
   **When** processed,
   **Then** the handler returns a gRPC `PERMISSION_DENIED` error with message `"user account is deactivated"`

4. **Given** a gRPC unit test,
   **When** `ValidateToken` is called with a known `user_id`,
   **Then** it returns the correct user data without hitting the database (mock DB in test)

## Tasks / Subtasks

- [x] Task 1: Update proto `ValidateTokenRequest` and `ValidateTokenResponse` (AC: #1, #2, #3)
  - [x] Modify `proto/core.proto` — update message fields
  - [x] Run `make proto` to regenerate Go + Elixir stubs

- [x] Task 2: Create `Nebu.Session.TokenValidator` behaviour + delegation (AC: #1–#4)
  - [x] Create `core/apps/session_manager/lib/nebu/session/token_validator.ex`

- [x] Task 3: Create `Nebu.Session.TokenValidator.Postgres` implementation (AC: #1–#3)
  - [x] Create `core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex`

- [x] Task 4: Implement `Nebu.EventDispatcher.Server.validate_token/2` handler (AC: #1–#3)
  - [x] Modify `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`

- [x] Task 5: Add `{:session_manager, in_umbrella: true}` to event_dispatcher deps (AC: #1–#4)
  - [x] Modify `core/apps/event_dispatcher/mix.exs`

- [x] Task 6: Wire Go gRPC client to actually call Elixir core (AC: #1)
  - [x] Modify `gateway/internal/grpc/client.go` — one-line change

- [x] Task 7: Write unit tests (AC: #4)
  - [x] Create `core/apps/session_manager/test/nebu/session/token_validator_test.exs`
  - [x] Create `core/apps/event_dispatcher/test/nebu/event_dispatcher/validate_token_test.exs`
  - [x] Confirm `mix test apps/session_manager --warnings-as-errors` passes
  - [x] Confirm `mix test apps/event_dispatcher --warnings-as-errors` passes

## Dev Notes

### Scope

This story does **four things**:
1. Updates the proto contract for `ValidateToken` RPC (request + response messages)
2. Creates `Nebu.Session.TokenValidator` behaviour + Postgres implementation (user lookup + decrypt + provision orchestration)
3. Implements the real `validate_token/2` gRPC handler in `Nebu.EventDispatcher.Server`
4. Wires the Go gRPC client's `ValidateToken` method to actually call Elixir core

**NOT in this story:**
- Bootstrap mode / `instance_admin` auto-assignment — Story 2.15
- Calling `ValidateToken` from Go HTTP middleware — Story 2.18 wires that
- Session creation / ETS session state — Epic 4
- Any changes to `UserStore` or `UserProvisioner` — Story 2.12 + 2.13 code is frozen

### Step 1: Proto Update

The current proto messages are incomplete for the real flow. The architecture mandates: "Auth-Token nie an Elixir weitergeben — nur user_id + system_role via gRPC-Metadata". The `token` field in `ValidateTokenRequest` is vestigial. The `valid` field in `ValidateTokenResponse` is redundant (success = valid, errors use gRPC status codes).

**Update `proto/core.proto` — replace both messages:**

```protobuf
// ValidateToken — Go validates OIDC token, Elixir trusts Go fully (ADR G2)
// user_id and system_role arrive via gRPC metadata (x-user-id, x-system-role).
// display_name and email are for new-user provisioning only.
message ValidateTokenRequest {
  reserved 1;               // was: token (removed per ADR G2 — no token forwarding)
  string display_name = 2;  // OIDC preferred_username claim (Operational PII, Tier 1)
  string email        = 3;  // OIDC email claim (Sensitive PII, Tier 2)
}

message ValidateTokenResponse {
  reserved 1;               // was: valid (redundant — success = valid, errors use gRPC status codes)
  string user_id      = 2;  // @{sub}:{server_name}
  string system_role  = 3;  // "user" | "instance_admin" | "compliance_officer"
  string display_name = 4;  // Decrypted from Operational PII (Tier 1)
  bool   is_active    = 5;  // User account status
}
```

**Why `reserved 1` on both messages:** Protobuf field numbers are part of the wire format. The old field 1 (`token` in request, `valid` in response) had different wire types than potential new field 1 values. Using `reserved` prevents accidental reuse and makes the breaking change explicit. Since this is early development with no production deployment, all Go + Elixir stubs are regenerated atomically via `make proto`. **Do NOT deploy gateway and core from different proto versions.**

**Why remove `token` from request:** Architecture Enforcement Rule #1 — auth token NEVER forwarded to Elixir. Go validates the OIDC JWT, extracts claims, sends only `user_id` + `system_role` via gRPC metadata and `display_name` + `email` in the request body for provisioning.

**Why remove `valid` from response:** Redundant. A successful gRPC response IS valid. Deactivated users get `PERMISSION_DENIED` error. Internal failures get `INTERNAL` error.

**Run `make proto`** to regenerate:
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` (Elixir structs)
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (Elixir service module)
- `gateway/internal/grpc/pb/core.pb.go` (Go structs)
- `gateway/internal/grpc/pb/core_grpc.pb.go` (Go client/server interfaces)

**After regeneration, verify:**
- `Core.ValidateTokenRequest` has fields: `display_name` (field 2), `email` (field 3), and `reserved 1` (no `token`)
- `Core.ValidateTokenResponse` has fields: `user_id` (field 2), `system_role` (field 3), `display_name` (field 4), `is_active` (field 5), and `reserved 1` (no `valid`)

### Step 2: `Nebu.Session.TokenValidator` Behaviour + Delegation

Follows the established `UserStore` / `UserProvisioner` pattern exactly.

**`core/apps/session_manager/lib/nebu/session/token_validator.ex`:**

```elixir
defmodule Nebu.Session.TokenValidator do
  @moduledoc """
  Validates a user's identity for the ValidateToken gRPC handler.

  Orchestrates: user lookup → provision if new → decrypt display_name → return user data.
  Returns {:ok, user_map} or {:error, :deactivated} or {:error, reason}.

  Delegates to a configurable module for testability.
  Real implementation: Nebu.Session.TokenValidator.Postgres
  Test implementation: configured via Application.put_env in test setup.
  """

  @type user_data :: %{
          user_id: String.t(),
          system_role: String.t(),
          display_name: String.t(),
          is_active: boolean()
        }

  @callback validate(
              user_id :: String.t(),
              system_role :: String.t(),
              display_name :: String.t(),
              email :: String.t()
            ) :: {:ok, user_data()} | {:error, :deactivated} | {:error, term()}

  @doc """
  Validates user identity: looks up user, provisions if new, decrypts display_name.

  - user_id: Matrix user ID from gRPC metadata (e.g. "@kai:nebu.local")
  - system_role: from gRPC metadata (e.g. "user", "instance_admin")
  - display_name: from ValidateTokenRequest (OIDC preferred_username)
  - email: from ValidateTokenRequest (OIDC email claim)

  Returns {:ok, %{user_id, system_role, display_name, is_active}} on success.
  Returns {:error, :deactivated} if user exists but is_active = false.
  Returns {:error, reason} on DB failure.
  """
  @spec validate(String.t(), String.t(), String.t(), String.t()) ::
          {:ok, user_data()} | {:error, :deactivated} | {:error, term()}
  def validate(user_id, system_role, display_name, email) do
    validator_module().validate(user_id, system_role, display_name, email)
  end

  defp validator_module do
    Application.get_env(:session_manager, :validator_module, Nebu.Session.TokenValidator.Postgres)
  end
end
```

### Step 3: `Nebu.Session.TokenValidator.Postgres` Implementation

**`core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex`:**

```elixir
defmodule Nebu.Session.TokenValidator.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.TokenValidator."

  @behaviour Nebu.Session.TokenValidator

  @lookup_sql """
  SELECT user_id, system_role, display_name_encrypted, display_name_nonce, is_active, signing_key_id
  FROM users
  WHERE user_id = $1
  """

  @impl Nebu.Session.TokenValidator
  def validate(user_id, system_role, display_name, email) do
    case lookup_user(user_id) do
      {:ok, nil} ->
        # New user — upsert + provision, then return provisioned data
        provision_new_user(user_id, system_role, display_name, email)

      {:ok, %{is_active: false}} ->
        {:error, :deactivated}

      {:ok, %{signing_key_id: nil} = _user} ->
        # User exists but not yet provisioned (race condition edge case)
        case provision_existing_user(user_id, display_name, email) do
          {:ok, :provisioned} -> read_and_decrypt(user_id)
          {:error, reason} -> {:error, reason}
        end

      {:ok, user} ->
        decrypt_and_return(user)

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp lookup_user(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @lookup_sql, [user_id]) do
      {:ok, %{rows: []}} ->
        {:ok, nil}

      {:ok, %{rows: [[uid, role, dn_enc, dn_nonce, active, sign_key_id]]}} ->
        {:ok, %{
          user_id: uid,
          system_role: role,
          display_name_encrypted: dn_enc,
          display_name_nonce: dn_nonce,
          is_active: active,
          signing_key_id: sign_key_id
        }}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp provision_new_user(user_id, system_role, display_name, email) do
    server_key = Application.get_env(:signature, :pii_encryption_key)

    with {:ok, ^user_id} <- Nebu.Session.UserStore.upsert_user(user_id, system_role),
         {:ok, :provisioned} <- Nebu.Session.UserProvisioner.provision_user(user_id, display_name, email, server_key) do
      {:ok, %{
        user_id: user_id,
        system_role: system_role,
        display_name: display_name,
        is_active: true
      }}
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp provision_existing_user(user_id, display_name, email) do
    server_key = Application.get_env(:signature, :pii_encryption_key)
    Nebu.Session.UserProvisioner.provision_user(user_id, display_name, email, server_key)
  end

  defp read_and_decrypt(user_id) do
    case lookup_user(user_id) do
      {:ok, nil} -> {:error, :user_not_found}
      {:ok, %{is_active: false}} -> {:error, :deactivated}
      {:ok, user} -> decrypt_and_return(user)
      {:error, reason} -> {:error, reason}
    end
  end

  defp decrypt_and_return(%{
         user_id: user_id,
         system_role: system_role,
         display_name_encrypted: dn_enc,
         display_name_nonce: dn_nonce,
         is_active: true
       }) do
    server_key = Application.get_env(:signature, :pii_encryption_key)

    case Nebu.Signature.decrypt_operational_pii(dn_enc, dn_nonce, server_key) do
      {:ok, display_name} ->
        {:ok, %{
          user_id: user_id,
          system_role: system_role,
          display_name: display_name,
          is_active: true
        }}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
```

**Critical design notes:**

- **Lookup BEFORE upsert**: For existing active users (hot path), we do one SELECT only. No upsert on every request — `upsert_user` is only called for genuinely new users.

- **`signing_key_id IS NULL` edge case**: If a user was upserted (Story 2.12) but provisioning (Story 2.13) hasn't completed yet (race condition), we trigger provisioning. This is safe — `UserProvisioner.Postgres` has `WHERE signing_key_id IS NULL` idempotency guard.

- **New user returns `display_name` directly**: After provisioning a new user, we already have the plaintext `display_name` in memory — no need to decrypt. We return it directly instead of re-reading and decrypting from DB. **Note for Story 2.15:** Bootstrap mode will need to override `system_role` for the first user to `instance_admin`. When that story is implemented, `provision_new_user` may need to re-read the DB role instead of returning the OIDC claim directly.

- **`server_key` from Application env**: `Application.get_env(:signature, :pii_encryption_key)` — set by `runtime.exs` from `NEBU_PII_ENCRYPTION_KEY` env var (64-char hex → 32-byte binary). Already configured since Story 2.10.

- **DB role is authoritative**: The `system_role` returned comes from the DB (set on first login, changeable by admin). For new users, we use the OIDC claim role (from gRPC metadata) which gets stored via `upsert_user/2`.

### Step 4: Implement gRPC Handler

**Modify `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — replace the `validate_token` stub:**

```elixir
def validate_token(request, stream) do
  {user_id, system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

  if is_nil(user_id) or user_id == "" do
    raise GRPC.RPCError,
      status: GRPC.Status.unauthenticated(),
      message: "missing x-user-id metadata"
  end

  case Nebu.Session.TokenValidator.validate(
         user_id,
         system_role,
         request.display_name,
         request.email
       ) do
    {:ok, user} ->
      {:ok,
       %Core.ValidateTokenResponse{
         user_id: user.user_id,
         system_role: user.system_role,
         display_name: user.display_name,
         is_active: user.is_active
       }}

    {:error, :deactivated} ->
      raise GRPC.RPCError,
        status: GRPC.Status.permission_denied(),
        message: "user account is deactivated"

    {:error, reason} ->
      Logger.error("validate_token failed", user_id: user_id, error: inspect(reason))

      raise GRPC.RPCError,
        status: GRPC.Status.internal(),
        message: "internal error"
  end
end
```

**Handler is deliberately thin:** Extract metadata → guard nil user_id → call TokenValidator → convert result to proto response or gRPC error. All business logic lives in `TokenValidator.Postgres`.

**gRPC error pattern:**
- `GRPC.Status.unauthenticated()` returns integer `16` — maps to gRPC `UNAUTHENTICATED`
- `GRPC.Status.permission_denied()` returns integer `7` — maps to gRPC `PERMISSION_DENIED`
- `GRPC.Status.internal()` returns integer `13` — maps to gRPC `INTERNAL`
- `raise GRPC.RPCError` is the grpc-elixir convention for returning gRPC errors from server handlers

**Logging:** Error-level only for unexpected failures. Never log PII (email, display_name). User_id is safe to log (it's a Matrix ID, not PII).

### Step 5: Update event_dispatcher deps

**Modify `core/apps/event_dispatcher/mix.exs` — add session_manager dependency:**

```elixir
defp deps do
  [
    {:grpc, "~> 0.8"},
    {:jason, "~> 1.4"},
    {:session_manager, in_umbrella: true}
  ]
end
```

**Why:** The handler in `server.ex` calls `Nebu.Session.TokenValidator.validate/4` which lives in the `session_manager` app. This is the correct dependency direction: event_dispatcher (gRPC transport) → session_manager (business logic).

**OTP boot order:** The umbrella release in `core/mix.exs` lists apps as: `nebu_db: :permanent, event_dispatcher: :permanent, ..., session_manager: :permanent, signature: :permanent`. OTP starts apps in dependency order. Since `session_manager` depends on `nebu_db` + `signature`, and `event_dispatcher` depends on `session_manager`, the boot order is: `nebu_db` → `signature` → `session_manager` → `event_dispatcher`. This ensures `Application.get_env(:signature, :pii_encryption_key)` is available when `TokenValidator.Postgres` needs it.

### Step 6: Wire Go gRPC Client

**Modify `gateway/internal/grpc/client.go` — replace the ValidateToken stub:**

```go
// ValidateToken calls the Elixir core to validate/provision a user.
func (c *Client) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	return c.core.ValidateToken(ctx, req)
}
```

**One-line change.** The generated `c.core` client already has the `ValidateToken` method. The Go caller (Story 2.18) will set gRPC metadata via `WithUserMetadata(ctx, userID, systemRole)` before calling this.

### Step 7: Unit Tests

**`core/apps/session_manager/test/nebu/session/token_validator_test.exs`:**

```elixir
defmodule Nebu.Session.TokenValidatorTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.TokenValidator

  defmodule FakeValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(user_id, system_role, display_name, _email) do
      :ets.insert(:validator_test, {user_id, system_role, display_name})

      {:ok, %{
        user_id: user_id,
        system_role: system_role,
        display_name: display_name,
        is_active: true
      }}
    end
  end

  defmodule DeactivatedValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :deactivated}
    end
  end

  defmodule FailingValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :db_error}
    end
  end

  setup do
    if :ets.whereis(:validator_test) != :undefined do
      :ets.delete(:validator_test)
    end

    :ets.new(:validator_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :validator_module, FakeValidator)

    on_exit(fn ->
      Application.delete_env(:session_manager, :validator_module)
      if :ets.whereis(:validator_test) != :undefined do
        :ets.delete(:validator_test)
      end
    end)

    :ok
  end

  describe "validate/4" do
    test "returns {:ok, user_data} for active user" do
      assert {:ok, %{user_id: "@kai:nebu.local", system_role: "user", display_name: "kai.mueller", is_active: true}} =
               TokenValidator.validate("@kai:nebu.local", "user", "kai.mueller", "kai@example.com")
    end

    test "records user_id, system_role, and display_name" do
      TokenValidator.validate("@kai:nebu.local", "user", "kai.mueller", "kai@example.com")

      assert [{"@kai:nebu.local", "user", "kai.mueller"}] =
               :ets.lookup(:validator_test, "@kai:nebu.local")
    end

    test "returns {:error, :deactivated} for deactivated user" do
      Application.put_env(:session_manager, :validator_module, DeactivatedValidator)

      assert {:error, :deactivated} =
               TokenValidator.validate("@alex:nebu.local", "user", "alex", "alex@example.com")
    end

    test "propagates {:error, reason} from DB module" do
      Application.put_env(:session_manager, :validator_module, FailingValidator)

      assert {:error, :db_error} =
               TokenValidator.validate("@bob:nebu.local", "user", "bob", "bob@example.com")
    end
  end
end
```

**Handler test — `core/apps/event_dispatcher/test/nebu/event_dispatcher/validate_token_test.exs`:**

```elixir
defmodule Nebu.EventDispatcher.ValidateTokenTest do
  use ExUnit.Case, async: false

  alias Nebu.EventDispatcher.Server

  defmodule SuccessValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(user_id, _system_role, _display_name, _email) do
      {:ok, %{
        user_id: user_id,
        system_role: "user",
        display_name: "kai.mueller",
        is_active: true
      }}
    end
  end

  defmodule DeactivatedValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :deactivated}
    end
  end

  defmodule ErrorValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :db_error}
    end
  end

  defp build_stream(headers) do
    %{adapter: %{payload: %{headers: headers}}}
  end

  defp build_request(display_name \\ "kai.mueller", email \\ "kai@example.com") do
    %Core.ValidateTokenRequest{display_name: display_name, email: email}
  end

  setup do
    Application.put_env(:session_manager, :validator_module, SuccessValidator)

    on_exit(fn ->
      Application.delete_env(:session_manager, :validator_module)
    end)

    :ok
  end

  describe "validate_token/2" do
    test "returns ValidateTokenResponse for active user" do
      stream = build_stream([{"x-user-id", "@kai:nebu.local"}, {"x-system-role", "user"}])
      request = build_request()

      assert {:ok, %Core.ValidateTokenResponse{
               user_id: "@kai:nebu.local",
               system_role: "user",
               display_name: "kai.mueller",
               is_active: true
             }} = Server.validate_token(request, stream)
    end

    test "raises PERMISSION_DENIED for deactivated user" do
      Application.put_env(:session_manager, :validator_module, DeactivatedValidator)
      stream = build_stream([{"x-user-id", "@alex:nebu.local"}, {"x-system-role", "user"}])
      request = build_request("alex", "alex@example.com")

      assert_raise GRPC.RPCError, "user account is deactivated", fn ->
        Server.validate_token(request, stream)
      end
    end

    test "raises UNAUTHENTICATED when x-user-id is missing" do
      stream = build_stream([])
      request = build_request()

      assert_raise GRPC.RPCError, "missing x-user-id metadata", fn ->
        Server.validate_token(request, stream)
      end
    end

    test "raises INTERNAL on DB error" do
      Application.put_env(:session_manager, :validator_module, ErrorValidator)
      stream = build_stream([{"x-user-id", "@bob:nebu.local"}, {"x-system-role", "user"}])
      request = build_request("bob", "bob@example.com")

      assert_raise GRPC.RPCError, "internal error", fn ->
        Server.validate_token(request, stream)
      end
    end
  end
end
```

**Expected test count after this story:**
- `session_manager`: `10 tests, 0 failures` (3 from user_store_test + 3 from user_provisioner_test + 4 new from token_validator_test)
- `event_dispatcher`: `9 tests, 0 failures` (5 existing + 4 new from validate_token_test)

### Schema Reference

**`users` table (relevant columns for this story's SELECT):**

```sql
user_id                   TEXT    PRIMARY KEY   -- Matrix ID "@sub:server_name"
system_role               TEXT    NOT NULL DEFAULT 'user'
display_name_encrypted    BYTEA               -- AES-256-GCM ciphertext+tag (Tier 1)
display_name_nonce        BYTEA               -- 12-byte nonce
is_active                 BOOLEAN NOT NULL DEFAULT true
signing_key_id            TEXT                -- NULL = not yet provisioned
encryption_key_id         TEXT                -- FK-like ref to user_keys
```

### gRPC Metadata Transport

**Existing headers (set by Go via `grpc.WithUserMetadata`):**
- `x-user-id` → `"@kai:nebu.local"` (Matrix user ID)
- `x-system-role` → `"user"` | `"instance_admin"` | `"compliance_officer"`

**Elixir reads via `Nebu.Grpc.Metadata`:**
- `Nebu.Grpc.Metadata.trusted_identity(stream)` → `{"@kai:nebu.local", "user"}`

**No new metadata headers needed.** Display name and email go in `ValidateTokenRequest` proto fields (provisioning-specific data, not per-request identity).

### Crypto Functions Reference (used by TokenValidator.Postgres)

| Function | Signature | Returns | Used for |
|----------|-----------|---------|---------|
| `decrypt_operational_pii/3` | `(ciphertext_with_tag, nonce, server_key)` | `{:ok, plaintext}` or `{:error, :decryption_failed}` | Decrypting display_name for existing users |
| `encrypt_operational_pii/2` | `(plaintext, server_key)` | `{ciphertext_with_tag, nonce}` | NOT called here — Story 2.13 encrypts |
| `encrypt_sensitive_pii/2` | `(plaintext, recipient_pub)` | `{ciphertext_with_tag, ephemeral_pub, nonce}` | NOT called here — Story 2.13 encrypts |

**Server key acquisition:**
```elixir
server_key = Application.get_env(:signature, :pii_encryption_key)
# Set by runtime.exs from NEBU_PII_ENCRYPTION_KEY (64-char hex → 32 bytes)
```

### Architecture Compliance

**AI-Constraint #1:** Auth token NEVER forwarded — handler receives `user_id` + `system_role` from metadata, `display_name` + `email` from request body (derived claims, not raw token) ✓

**AI-Constraint #2 (Error handling):** All functions return `{:ok, result}` / `{:error, reason}`. gRPC errors via `raise GRPC.RPCError` (the grpc-elixir convention, not a business logic throw) ✓

**AI-Constraint #10 (PII encryption):** PII decrypted via X25519-derived key (Tier 1 `decrypt_operational_pii/3`). Never log email or display_name ✓

**Architecture V4:** Go validates OIDC token → extracts claims → sends to Elixir via gRPC metadata + request body → Elixir trusts Go fully ✓

**gRPC Status Codes (from architecture):**
- Missing `x-user-id` metadata → `UNAUTHENTICATED` (guard at handler top)
- Deactivated user → `PERMISSION_DENIED` (AC #3)
- DB failure → `INTERNAL`

### Critical Anti-Patterns to Avoid

**NEVER define `Ecto.Schema` in Elixir** — use raw SQL only:
```elixir
# ❌ WRONG — Go owns schema
defmodule Nebu.User do
  use Ecto.Schema
  schema "users" do ...
end

# ✅ CORRECT — raw SQL via Ecto.Adapters.SQL.query/3
Ecto.Adapters.SQL.query(Nebu.Repo, @lookup_sql, [user_id])
```

**NEVER forward auth token to Elixir:**
```elixir
# ❌ WRONG — violates Architecture Enforcement Rule #1
def validate_token(request, _stream) do
  verify_jwt(request.token)  # Elixir must never see the token
end

# ✅ CORRECT — trust Go's metadata
def validate_token(request, stream) do
  {user_id, system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
end
```

**NEVER log PII:**
```elixir
# ❌ WRONG — email is Sensitive PII
Logger.info("validating user", email: email, display_name: display_name)

# ✅ CORRECT — only log user_id (Matrix ID, not PII)
Logger.error("validate_token failed", user_id: user_id, error: inspect(reason))
```

**NEVER decrypt display_name using Ed25519 key:**
```elixir
# ❌ WRONG — display_name is Tier 1 (server key), not Tier 2 (user key)
Nebu.Signature.decrypt_sensitive_pii(dn_enc, dn_nonce, user_priv_key)

# ✅ CORRECT — Tier 1 uses server-side AES key
Nebu.Signature.decrypt_operational_pii(dn_enc, dn_nonce, server_key)
```

**NEVER call upsert_user for every request:**
```elixir
# ❌ WRONG — upsert on every request, wastes writes
def validate(user_id, system_role, display_name, email) do
  UserStore.upsert_user(user_id, system_role)
  ...
end

# ✅ CORRECT — lookup first, only upsert if user is genuinely new
def validate(user_id, system_role, display_name, email) do
  case lookup_user(user_id) do
    {:ok, nil} -> provision_new_user(...)
    {:ok, user} -> decrypt_and_return(user)
  end
end
```

**NEVER inline `DateTime.utc_now()` for timestamps:**
```elixir
# ❌ WRONG
created_at = DateTime.utc_now() |> DateTime.to_unix(:millisecond)

# ✅ CORRECT
now_ms = Nebu.DB.Helpers.now_ms()
```

### Previous Story Intelligence (2.13)

- **Behaviour + delegation pattern**: Exact same as `UserStore` and `UserProvisioner` — `Application.get_env(:session_manager, :xxx_module, XXX.Postgres)` for swappable implementations
- **`async: false` required**: `Application.put_env(:session_manager, :validator_module, ...)` is global state
- **ETS table pattern**: Create named table in `setup`, clean up in `on_exit`. Use `[:set]` semantics
- **`--warnings-as-errors` discipline**: Prefix all unused variables with `_`
- **`@behaviour` compiler enforcement**: `@behaviour Nebu.Session.TokenValidator` on `Postgres`, `FakeValidator`, `DeactivatedValidator`, `FailingValidator`
- **Raw SQL only pattern**: Established by `UserStore.Postgres` and `UserProvisioner.Postgres` — continue it
- **Nebu.Repo**: Lives in `core/apps/nebu_db/lib/nebu/repo.ex`, started by `Nebu.DB.Application`. No test DB config (unit tests use fake modules)
- **Crypto before transaction**: Not applicable here (no transaction in TokenValidator — it calls UserStore/UserProvisioner which have their own transaction handling)

### Git Intelligence

Recent commits show Stories 2.8–2.12 completed. Key patterns:
- Commit messages follow: `"Story X-Y"` or `"Complete Story X-Y: description"`
- Files follow established naming convention: `behaviour.ex` + `behaviour/postgres.ex`
- Tests follow established pattern: `Fake*` + `Failing*` modules with ETS backing

### Build & Test Commands

```bash
# Regenerate proto stubs:
make proto

# Run session_manager tests only:
cd core && mix test apps/session_manager --warnings-as-errors

# Run event_dispatcher tests only:
cd core && mix test apps/event_dispatcher --warnings-as-errors

# Run all Elixir unit tests (regression check):
make test-unit-elixir

# Run Go unit tests (compilation check after proto regen):
make test-unit-go
```

**Expected outputs:**
- `session_manager`: `10 tests, 0 failures` (3 user_store + 3 user_provisioner + 4 new token_validator)
- `event_dispatcher`: `9 tests, 0 failures` (5 existing + 4 new validate_token handler tests)
- `make test-unit-elixir`: all apps green, 0 failures
- `make test-unit-go`: `PASS` (proto-generated code compiles, client wired)

### Files to Create / Modify

**CREATE:**
```
core/apps/session_manager/lib/nebu/session/
  token_validator.ex
  token_validator/
    postgres.ex
core/apps/session_manager/test/nebu/session/
  token_validator_test.exs
core/apps/event_dispatcher/test/nebu/event_dispatcher/
  validate_token_test.exs
```

**MODIFY:**
```
proto/core.proto                                              ← update ValidateTokenRequest + ValidateTokenResponse messages
core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex ← implement validate_token/2 handler
core/apps/event_dispatcher/mix.exs                            ← add {:session_manager, in_umbrella: true}
gateway/internal/grpc/client.go                               ← wire ValidateToken to actual gRPC call
```

**REGENERATED (by `make proto`):**
```
core/apps/event_dispatcher/lib/pb/core.pb.ex                 ← Elixir proto structs
core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex            ← Elixir gRPC service module
gateway/internal/grpc/pb/core.pb.go                           ← Go proto structs
gateway/internal/grpc/pb/core_grpc.pb.go                      ← Go gRPC client/server interfaces
```

**DO NOT TOUCH:**
- `core/apps/session_manager/lib/nebu/session/user_store.ex` — Story 2.12's code
- `core/apps/session_manager/lib/nebu/session/user_store/postgres.ex` — Story 2.12's code
- `core/apps/session_manager/lib/nebu/session/user_provisioner.ex` — Story 2.13's code
- `core/apps/session_manager/lib/nebu/session/user_provisioner/postgres.ex` — Story 2.13's code
- `core/apps/signature/` — no changes (crypto functions already exist and are complete)
- `gateway/internal/grpc/metadata.go` — no new headers needed
- `gateway/internal/middleware/auth.go` — Go HTTP middleware not wired in this story
- `core/config/runtime.exs` — no new env vars needed

### Project Structure Notes

- `token_validator.ex` and `token_validator/postgres.ex` mirror the established `user_store.ex` + `user_store/postgres.ex` and `user_provisioner.ex` + `user_provisioner/postgres.ex` patterns exactly
- The `session_manager` app is the right home for TokenValidator (it owns user identity logic)
- Adding `{:session_manager, in_umbrella: true}` to `event_dispatcher/mix.exs` creates: event_dispatcher → session_manager → nebu_db + signature. This dependency chain is correct per architecture
- The gRPC handler in event_dispatcher stays thin — all business logic in session_manager

### References

- [Source: epics.md#Story-2.14] Authoritative user story, acceptance criteria
- [Source: epics.md#Story-2.15] Scope boundary — bootstrap mode is next story
- [Source: epics.md#Story-2.13] "Proto changes for email/display_name in ValidateTokenRequest — Story 2.14's concern"
- [Source: architecture.md#Auth-Token-Flow-V4] OIDC → Go → Elixir via gRPC metadata
- [Source: architecture.md#Enforcement-rule-1] Auth token NEVER forwarded to Elixir
- [Source: architecture.md#Enforcement-rule-6] {:ok, result} / {:error, reason} mandate
- [Source: architecture.md#Enforcement-rule-10] PII encryption via X25519 (Encryption Key) only
- [Source: architecture.md#gRPC-Status-Codes] PERMISSION_DENIED, INTERNAL, UNAUTHENTICATED
- [Source: architecture.md#Logging] Log levels, no PII in logs
- [Source: proto/core.proto] Current ValidateTokenRequest/Response definitions (to be updated)
- [Source: core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex] Current stub handler
- [Source: core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex] trusted_identity/1 API
- [Source: core/apps/session_manager/lib/nebu/session/user_store.ex] upsert_user/2 callback
- [Source: core/apps/session_manager/lib/nebu/session/user_provisioner.ex] provision_user/4 callback
- [Source: core/apps/signature/lib/nebu/signature.ex] decrypt_operational_pii/3 function
- [Source: implementation-artifacts/2-13-user-provisioning-orchestration-keypairs-pii-encryption.md] Previous story patterns and learnings
- [Source: implementation-artifacts/2-12-user-record-db-write-on-first-login.md] UserStore pattern, raw SQL, async:false

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

None — clean implementation, no blocking issues encountered.

### Completion Notes List

- Task 1: Updated `proto/core.proto` — replaced vestigial `token` field (request) and `valid` field (response) with `reserved 1`. Added `display_name` + `email` to request, `display_name` + `is_active` to response. Ran `make proto` to regenerate all Go + Elixir stubs.
- Task 2: Created `Nebu.Session.TokenValidator` behaviour with `validate/4` callback and delegation via `Application.get_env(:session_manager, :validator_module)`. Follows established `UserStore`/`UserProvisioner` pattern.
- Task 3: Created `Nebu.Session.TokenValidator.Postgres` with lookup-first strategy (SELECT before upsert), deactivated user check, signing_key_id NULL edge case handling, and `decrypt_operational_pii/3` for existing users.
- Task 4: Replaced `validate_token/2` stub in `Nebu.EventDispatcher.Server` with real handler: extracts metadata via `trusted_identity/1`, guards missing `x-user-id`, calls `TokenValidator.validate/4`, maps results to proto response or gRPC error codes (UNAUTHENTICATED, PERMISSION_DENIED, INTERNAL).
- Task 5: Added `{:session_manager, in_umbrella: true}` to `event_dispatcher/mix.exs` deps.
- Task 6: Wired Go `ValidateToken` client method to `c.core.ValidateToken(ctx, req)`. Updated Go test to expect connection error instead of nil/nil for wired method.
- Task 7: Created 4 TokenValidator behaviour tests (active user, ETS recording, deactivated, DB error) and 4 validate_token handler tests (success, PERMISSION_DENIED, UNAUTHENTICATED, INTERNAL). All tests pass: session_manager 10/0, event_dispatcher 25/0, Go all PASS.

### Change Log

- 2026-03-30: Story 2.14 implemented — ValidateToken gRPC handler with proto update, TokenValidator behaviour + Postgres impl, handler wiring, Go client wiring, and unit tests.

### File List

**Created:**
- core/apps/session_manager/lib/nebu/session/token_validator.ex
- core/apps/session_manager/lib/nebu/session/token_validator/postgres.ex
- core/apps/session_manager/test/nebu/session/token_validator_test.exs
- core/apps/event_dispatcher/test/nebu/event_dispatcher/validate_token_test.exs

**Modified:**
- proto/core.proto
- core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex
- core/apps/event_dispatcher/mix.exs
- gateway/internal/grpc/client.go
- gateway/internal/grpc/client_test.go

**Regenerated (by make proto):**
- core/apps/event_dispatcher/lib/pb/core.pb.ex
- core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex
- gateway/internal/grpc/pb/core.pb.go
- gateway/internal/grpc/pb/core_grpc.pb.go
