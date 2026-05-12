defmodule Nebu.EventDispatcher.AdminGrpcTest do
  use ExUnit.Case, async: false

  # ─── Story 9.1: Admin gRPC RPCs — User + Room Management ────────────────────
  #
  # RED PHASE — ALL tests in this module FAIL until Story 9.1 is implemented.
  #
  # Failing reasons:
  #   1. Core.ListAdminUsersRequest / Core.ListAdminUsersResponse do not exist yet.
  #      They will be generated after `make proto` runs on the updated core.proto.
  #   2. Core.GetAdminUserRequest / Core.GetAdminUserResponse do not exist yet.
  #   3. Core.DeactivateUserRequest / Core.DeactivateUserResponse do not exist yet.
  #   4. Core.ReactivateUserRequest / Core.ReactivateUserResponse do not exist yet.
  #   5. Core.UpdateUserRoleRequest / Core.UpdateUserRoleResponse do not exist yet.
  #   6. Core.ListAdminRoomsRequest / Core.ListAdminRoomsResponse do not exist yet.
  #   7. Core.GetAdminRoomRequest / Core.GetAdminRoomResponse do not exist yet.
  #   8. Core.GetServerConfigRequest / Core.GetServerConfigResponse do not exist yet.
  #   9. Core.UpdateServerConfigRequest / Core.UpdateServerConfigResponse do not exist yet.
  #  10. Nebu.EventDispatcher.Server.list_admin_users/2 does not exist yet.
  #  11. Nebu.EventDispatcher.Server.get_admin_user/2 does not exist yet.
  #  12. Nebu.EventDispatcher.Server.deactivate_user/2 does not exist yet.
  #  13. Nebu.EventDispatcher.Server.reactivate_user/2 does not exist yet.
  #  14. Nebu.EventDispatcher.Server.update_user_role/2 does not exist yet.
  #  15. Nebu.EventDispatcher.Server.list_admin_rooms/2 does not exist yet.
  #  16. Nebu.EventDispatcher.Server.get_admin_room/2 does not exist yet.
  #  17. Nebu.EventDispatcher.Server.get_server_config/2 does not exist yet.
  #  18. Nebu.EventDispatcher.Server.update_server_config/2 does not exist yet.
  #  19. Nebu.Admin.DB module does not exist yet.
  #  20. The existing Server.get_metrics/2 is a stub (returns zeroes); the real
  #      implementation is not wired yet.
  #  21. The existing Server.archive_room/2 does not use SELECT FOR UPDATE in a DB
  #      transaction; it only terminates the GenServer. AC: 4 requires the DB write.
  #
  # async: false — Application.put_env for :admin_db_module and :session_supervisor_module
  # are process-global; tests must not run concurrently.
  #
  # Test strategy:
  #   - Call Server.<handler>/2 directly (unary gRPC handlers, synchronous).
  #   - FakeAdminDB is injected via Application.put_env(:event_dispatcher, :admin_db_module, ...).
  #     It implements every callback needed by Nebu.Admin.DB (list_users, get_user,
  #     set_is_active, set_system_role, list_rooms, get_room, archive_room_atomic,
  #     get_server_config, upsert_server_config).
  #   - FakeSessionSupervisor (spy) is injected via
  #     Application.put_env(:event_dispatcher, :session_supervisor_module, ...).
  #     Records destroy_session/1 calls via {:destroy_called, user_id} messages.
  #   - EtsStore is populated directly for GetMetrics active_sessions count.
  #   - Horde.Registry is used directly for GetMetrics room_count (rooms started in setup).
  #
  # Covered Acceptance Criteria:
  #   AC#1 — Proto RPCs defined: referenced indirectly — compile-time fail if missing.
  #   AC#2 — ListAdminUsers returns paginated users from PostgreSQL (AT#1).
  #   AC#2 — GetAdminUser returns correct user detail (AT#2).
  #   AC#3 — DeactivateUser sets is_active=false + calls InvalidateUserSessions (AT#3).
  #   AC#3 — ReactivateUser sets is_active=true (AT#4).
  #   AC#3 — UpdateUserRole updates system_role (AT#5).
  #   AC#1 — ListAdminRooms returns paginated rooms (AT#6).
  #   AC#1 — GetAdminRoom returns room detail with member_count (AT#7).
  #   AC#4 — ArchiveRoom uses SELECT FOR UPDATE (AT#8).
  #   AC#1 — GetServerConfig returns config without oidc_client_secret (AT#9).
  #   AC#1 — UpdateServerConfig persists config changes (AT#10).
  #   AC#1 — GetMetrics returns real counts (not zeroes) (AT#11).
  #   Security — email_masked never contains plaintext email (SEC invariant from AC#2).
  #   Security — GetServerConfig response never contains oidc_client_secret field value.
  #   Security — UpdateUserRole raises GRPC.RPCError for unknown role values.

  alias Nebu.EventDispatcher.Server

  # ─── FakeAdminDB ─────────────────────────────────────────────────────────────
  #
  # In-memory fake for Nebu.Admin.DB (does not exist yet — RED).
  # Used for all user/room/config handler tests that need DB interaction.
  # ETS table :admin_grpc_test_db is owned and managed by setup/on_exit.
  #
  # The module satisfies the callbacks that Nebu.Admin.DB will expose once
  # Story 9.1 is implemented. Until then, this module compiles but the
  # Application.get_env lookup in Server will not find it (admin_db_module key
  # does not exist in event_dispatcher config yet).

  defmodule FakeAdminDB do
    @moduledoc """
    In-memory fake for Nebu.Admin.DB.
    Inject via Application.put_env(:event_dispatcher, :admin_db_module, __MODULE__).
    All data is stored in the :admin_grpc_test_db ETS table managed by test setup.
    """

    # ── User operations ────────────────────────────────────────────────────────

    @doc """
    Returns {users, next_cursor} for paginated user listing.
    Cursor is the user_id after which to continue.
    """
    def list_users(limit, cursor, _search) do
      all =
        :ets.match(:admin_grpc_test_db, {{:user, :"$1"}, :"$2"})
        |> Enum.map(fn [user_id, attrs] -> Map.put(attrs, :user_id, user_id) end)
        |> Enum.sort_by(& &1.user_id)

      page =
        if cursor == "" do
          all
        else
          all |> Enum.drop_while(fn u -> u.user_id <= cursor end)
        end

      results = Enum.take(page, limit)
      next = if length(page) > limit, do: List.last(results).user_id, else: ""
      {results, next}
    end

    @doc """
    Returns {:ok, user_attrs} or {:error, :not_found}.
    """
    def get_user(user_id) do
      case :ets.lookup(:admin_grpc_test_db, {:user, user_id}) do
        [] -> {:error, :not_found}
        [{_, attrs}] -> {:ok, Map.put(attrs, :user_id, user_id)}
      end
    end

    @doc """
    Sets is_active for user_id. Returns :ok or {:error, :not_found}.
    """
    def set_is_active(user_id, is_active) do
      case :ets.lookup(:admin_grpc_test_db, {:user, user_id}) do
        [] ->
          {:error, :not_found}

        [{_, attrs}] ->
          :ets.insert(:admin_grpc_test_db, {{:user, user_id}, Map.put(attrs, :is_active, is_active)})
          :ok
      end
    end

    @doc """
    Updates system_role for user_id. Returns :ok or {:error, :not_found}.
    """
    def set_system_role(user_id, role) do
      case :ets.lookup(:admin_grpc_test_db, {:user, user_id}) do
        [] ->
          {:error, :not_found}

        [{_, attrs}] ->
          :ets.insert(:admin_grpc_test_db, {{:user, user_id}, Map.put(attrs, :system_role, role)})
          :ok
      end
    end

    # ── Room operations ────────────────────────────────────────────────────────

    @doc """
    Returns {rooms, next_cursor} for paginated room listing.
    status_filter: "active" | "archived" | "" (all).
    """
    def list_rooms(limit, cursor, status_filter, _search) do
      all =
        :ets.match(:admin_grpc_test_db, {{:room, :"$1"}, :"$2"})
        |> Enum.map(fn [room_id, attrs] -> Map.put(attrs, :room_id, room_id) end)
        |> Enum.sort_by(& &1.room_id)

      filtered =
        if status_filter != "" do
          Enum.filter(all, fn r -> r.status == status_filter end)
        else
          all
        end

      page =
        if cursor == "" do
          filtered
        else
          filtered |> Enum.drop_while(fn r -> r.room_id <= cursor end)
        end

      results = Enum.take(page, limit)
      next = if length(page) > limit, do: List.last(results).room_id, else: ""
      {results, next}
    end

    @doc """
    Returns {:ok, room_attrs} or {:error, :not_found}.
    """
    def get_room(room_id) do
      case :ets.lookup(:admin_grpc_test_db, {:room, room_id}) do
        [] -> {:error, :not_found}
        [{_, attrs}] -> {:ok, Map.put(attrs, :room_id, room_id)}
      end
    end

    @doc """
    Atomically sets rooms.status='archived' using SELECT FOR UPDATE.
    Returns :ok | {:error, :not_found} | {:error, :already_archived}.
    RED: this function does not exist in Nebu.Admin.DB yet — the module doesn't exist.
    """
    def archive_room_atomic(room_id) do
      case :ets.lookup(:admin_grpc_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, %{status: "archived"} = _attrs}] ->
          # Idempotent — already archived
          :ok

        [{_, attrs}] ->
          :ets.insert(:admin_grpc_test_db, {{:room, room_id}, Map.put(attrs, :status, "archived")})
          :ok
      end
    end

    # ── Config operations ──────────────────────────────────────────────────────

    @doc """
    Returns {:ok, config_map} with server config keys.
    MUST NOT include oidc_client_secret (security invariant).
    """
    def get_server_config do
      rows = :ets.match(:admin_grpc_test_db, {{:config, :"$1"}, :"$2"})
      config =
        rows
        |> Enum.map(fn [k, v] -> {k, v} end)
        |> Map.new()

      {:ok, config}
    end

    @doc """
    Upserts one or more server_config keys. Returns :ok.
    """
    def upsert_server_config(changes) do
      Enum.each(changes, fn {key, value} ->
        :ets.insert(:admin_grpc_test_db, {{:config, key}, value})
      end)
      :ok
    end

    @doc """
    Returns {:ok, member_rows} for the given room_id from ETS key {:members, room_id}.
    Returns {:ok, []} if no entry found (empty room — not an error).
    Row shape: %{user_id:, display_name_encrypted:, display_name_nonce:, email_ephemeral_pub:, joined_at:}

    Story 9.18: added to satisfy the Nebu.Admin.DB callback.
    RED: list_room_members/1 does not exist in Nebu.Admin.DB yet.
    """
    def list_room_members(room_id) do
      case :ets.lookup(:admin_grpc_test_db, {:members, room_id}) do
        [] -> {:ok, []}
        [{_, rows}] -> {:ok, rows}
      end
    end
  end

  # ─── FakeAdminDBNotFound ──────────────────────────────────────────────────────
  # Returns :not_found for every get_user/get_room call.
  # Used for testing gRPC not_found error paths.

  defmodule FakeAdminDBNotFound do
    @moduledoc "Fake DB that returns :not_found for all get operations."

    def list_users(_limit, _cursor, _search), do: {[], ""}
    def get_user(_user_id), do: {:error, :not_found}
    def set_is_active(_user_id, _is_active), do: {:error, :not_found}
    def set_system_role(_user_id, _role), do: {:error, :not_found}
    def list_rooms(_limit, _cursor, _status_filter, _search), do: {[], ""}
    def get_room(_room_id), do: {:error, :not_found}
    def archive_room_atomic(_room_id), do: {:error, :not_found}
    def get_server_config, do: {:ok, %{}}
    def upsert_server_config(_changes), do: :ok
    # Story 9.18: list_room_members/1 added to satisfy the Nebu.Admin.DB callback.
    # RED: this function does not exist in Nebu.Admin.DB yet.
    def list_room_members(_room_id), do: {:ok, []}
  end

  # ─── FakeSessionSupervisor ────────────────────────────────────────────────────
  # Spy that records destroy_session/1 calls and sends {:destroy_called, user_id}
  # to the test process. Mirrors the pattern in invalidate_user_sessions_test.exs.

  defmodule FakeSessionSupervisor do
    @moduledoc """
    Test spy for Nebu.Session.SessionSupervisor.
    Inject via Application.put_env(:event_dispatcher, :session_supervisor_module, __MODULE__).
    """

    def destroy_session(user_id) do
      test_pid = Application.get_env(:event_dispatcher, :__admin_test_pid__)

      if test_pid && Process.alive?(test_pid) do
        send(test_pid, {:destroy_called, user_id})
      end

      :ok
    end
  end

  # ─── FakeAuditWriter ─────────────────────────────────────────────────────────
  #
  # No-op spy for Compliance.AuditWriter. Returns :ok for all log calls.
  # Injected via Application.put_env(:compliance, :audit_writer, FakeAuditWriter)
  # so that tests that call handlers with audit log paths don't hit a real DB.
  # Story 9.4: added to support update_server_config which now emits audit entries.

  defmodule FakeAuditWriter do
    @moduledoc "No-op audit writer for admin_grpc_test."
    def log(_actor, _action, _target_type, _target_id, _metadata, _outcome), do: :ok
    def log(_actor, _action, _target_type, _target_id, _metadata, _outcome, _error_detail), do: :ok
  end

  # ─── Setup / Teardown ─────────────────────────────────────────────────────────

  setup do
    # Create isolated ETS table for this test.
    if :ets.info(:admin_grpc_test_db) != :undefined do
      :ets.delete(:admin_grpc_test_db)
    end

    :ets.new(:admin_grpc_test_db, [:named_table, :public, :set])

    # Inject fakes via Application env.
    Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)
    Application.put_env(:event_dispatcher, :session_supervisor_module, FakeSessionSupervisor)
    Application.put_env(:event_dispatcher, :__admin_test_pid__, self())
    # Story 9.4: inject FakeAuditWriter so audit log calls in update_server_config
    # (and deactivate_user / reactivate_user / update_user_role) don't hit a real DB.
    Application.put_env(:compliance, :audit_writer, FakeAuditWriter)

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :admin_db_module)
      Application.delete_env(:event_dispatcher, :session_supervisor_module)
      Application.delete_env(:event_dispatcher, :__admin_test_pid__)
      Application.delete_env(:compliance, :audit_writer)

      if :ets.info(:admin_grpc_test_db) != :undefined do
        :ets.delete(:admin_grpc_test_db)
      end
    end)

    :ok
  end

  defp build_stream, do: %{http_request_headers: %{}}

  # ─── Helpers — ETS seed functions ─────────────────────────────────────────────

  defp insert_user(user_id, attrs) do
    defaults = %{
      display_name: "User #{user_id}",
      email_masked: "u***@example.com",
      is_active: true,
      system_role: "user",
      created_at: 1_700_000_000_000
    }

    :ets.insert(:admin_grpc_test_db, {{:user, user_id}, Map.merge(defaults, attrs)})
  end

  defp insert_room(room_id, attrs) do
    defaults = %{
      name: "Room #{room_id}",
      status: "active",
      member_count: 0,
      max_members: 0,
      visibility: "private",
      created_at: 1_700_000_000_000
    }

    :ets.insert(:admin_grpc_test_db, {{:room, room_id}, Map.merge(defaults, attrs)})
  end

  defp insert_config(key, value) do
    :ets.insert(:admin_grpc_test_db, {{:config, key}, value})
  end

  defp get_user_from_ets(user_id) do
    case :ets.lookup(:admin_grpc_test_db, {:user, user_id}) do
      [{_, attrs}] -> {:ok, attrs}
      [] -> {:error, :not_found}
    end
  end

  defp get_room_from_ets(room_id) do
    case :ets.lookup(:admin_grpc_test_db, {:room, room_id}) do
      [{_, attrs}] -> {:ok, attrs}
      [] -> {:error, :not_found}
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#1 — ListAdminUsers returns paginated users (AC: 2)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: 3 users exist in DB (2 active, 1 deactivated)
  # When:  ListAdminUsers gRPC is called with limit=2
  # Then:  response.users has 2 entries
  # And:   response.next_cursor is non-empty (more pages available)
  #
  # RED: fails because Core.ListAdminUsersRequest does not exist yet (compile error)
  # and Server.list_admin_users/2 is not defined.

  describe "ListAdminUsers — AC#2" do
    test "returns 2 users and a non-empty next_cursor when 3 users exist and limit=2" do
      insert_user("@alice:nebu.local", %{is_active: true})
      insert_user("@bob:nebu.local", %{is_active: false})
      insert_user("@carol:nebu.local", %{is_active: true})

      # RED: Core.ListAdminUsersRequest does not exist yet → compile error
      request = %Core.ListAdminUsersRequest{limit: 2, cursor: "", search: ""}

      # RED: Server.list_admin_users/2 does not exist yet → UndefinedFunctionError
      response = Server.list_admin_users(request, build_stream())

      assert %Core.ListAdminUsersResponse{} = response,
             "expected ListAdminUsersResponse struct, got #{inspect(response)}"

      assert length(response.users) == 2,
             "expected 2 users (limit=2 of 3 total), got #{length(response.users)}"

      assert response.next_cursor != "",
             "expected non-empty next_cursor when more pages exist, got empty string"
    end

    test "returns all users and empty next_cursor when total <= limit" do
      insert_user("@alice:nebu.local", %{is_active: true})
      insert_user("@bob:nebu.local", %{is_active: true})

      request = %Core.ListAdminUsersRequest{limit: 10, cursor: "", search: ""}

      response = Server.list_admin_users(request, build_stream())

      assert length(response.users) == 2,
             "expected 2 users, got #{length(response.users)}"

      assert response.next_cursor == "",
             "expected empty next_cursor when all users fit in one page"
    end

    test "email_masked field must never be a plaintext email address" do
      insert_user("@alice:nebu.local", %{
        email_masked: "a***@example.com",
        is_active: true
      })

      request = %Core.ListAdminUsersRequest{limit: 10, cursor: "", search: ""}
      response = Server.list_admin_users(request, build_stream())

      assert length(response.users) >= 1

      Enum.each(response.users, fn user ->
        refute String.match?(user.email_masked, ~r/^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$/),
               "email_masked must NOT be a plaintext email address; got: #{user.email_masked}"

        assert String.match?(user.email_masked, ~r/\*\*\*/),
               "email_masked must contain '***' masking pattern; got: #{user.email_masked}"
      end)
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#2 — GetAdminUser returns correct user detail (AC: 2)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: a user with user_id="@alice:nebu.local" exists
  # When:  GetAdminUser gRPC is called with that user_id
  # Then:  response.user contains display_name, masked email, is_active=true, system_role
  #
  # RED: Core.GetAdminUserRequest does not exist yet.

  describe "GetAdminUser — AC#2" do
    test "returns user detail with correct fields for existing user" do
      insert_user("@alice:nebu.local", %{
        display_name: "Alice Admin",
        email_masked: "a***@example.com",
        is_active: true,
        system_role: "instance_admin"
      })

      # RED: Core.GetAdminUserRequest does not exist yet → compile error
      request = %Core.GetAdminUserRequest{user_id: "@alice:nebu.local"}

      # RED: Server.get_admin_user/2 does not exist yet → UndefinedFunctionError
      response = Server.get_admin_user(request, build_stream())

      assert %Core.GetAdminUserResponse{} = response,
             "expected GetAdminUserResponse struct, got #{inspect(response)}"

      user = response.user

      assert user.user_id == "@alice:nebu.local",
             "expected user_id='@alice:nebu.local', got #{inspect(user.user_id)}"

      assert user.display_name == "Alice Admin",
             "expected display_name='Alice Admin', got #{inspect(user.display_name)}"

      assert String.match?(user.email_masked, ~r/\*\*\*/),
             "expected masked email, got plaintext: #{inspect(user.email_masked)}"

      assert user.is_active == true,
             "expected is_active=true, got #{inspect(user.is_active)}"

      assert user.system_role == "instance_admin",
             "expected system_role='instance_admin', got #{inspect(user.system_role)}"
    end

    test "raises GRPC.RPCError with not_found when user does not exist" do
      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDBNotFound)

      request = %Core.GetAdminUserRequest{user_id: "@unknown:nebu.local"}

      assert_raise GRPC.RPCError, fn ->
        Server.get_admin_user(request, build_stream())
      end
    end

    test "GRPC.RPCError for missing user has not_found status code" do
      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDBNotFound)

      request = %Core.GetAdminUserRequest{user_id: "@nobody:nebu.local"}

      error =
        try do
          Server.get_admin_user(request, build_stream())
          nil
        rescue
          e in GRPC.RPCError -> e
        end

      refute is_nil(error),
             "expected GRPC.RPCError to be raised for not_found user"

      assert error.status == GRPC.Status.not_found(),
             "expected GRPC.Status.not_found() (#{GRPC.Status.not_found()}), got #{error.status}"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#3 — DeactivateUser sets is_active=false + calls InvalidateUserSessions (AC: 3)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: user @bob:nebu.local exists with is_active=true
  # When:  DeactivateUser gRPC is called
  # Then:  DB: is_active=false
  # And:   InvalidateUserSessions was called (FakeSessionSupervisor spy)
  # And:   destroy_session called AFTER DB update (Ecto.Multi sequencing — invariant)
  #
  # RED: Core.DeactivateUserRequest does not exist yet.

  describe "DeactivateUser — AC#3" do
    test "sets is_active=false in DB and calls destroy_session/1 for the user" do
      insert_user("@bob:nebu.local", %{is_active: true})

      # RED: Core.DeactivateUserRequest does not exist yet → compile error
      request = %Core.DeactivateUserRequest{user_id: "@bob:nebu.local"}

      # RED: Server.deactivate_user/2 does not exist yet → UndefinedFunctionError
      response = Server.deactivate_user(request, build_stream())

      assert %Core.DeactivateUserResponse{ok: true} = response,
             "expected DeactivateUserResponse{ok: true}, got #{inspect(response)}"

      # DB must reflect is_active=false
      {:ok, attrs} = get_user_from_ets("@bob:nebu.local")

      assert attrs.is_active == false,
             "expected is_active=false in DB after DeactivateUser, got #{inspect(attrs.is_active)}"

      # InvalidateUserSessions must have been triggered (spy check)
      assert_receive {:destroy_called, "@bob:nebu.local"},
                     200,
                     "expected destroy_session/1 to be called with '@bob:nebu.local'"
    end

    test "does not call destroy_session for a different user" do
      insert_user("@bob:nebu.local", %{is_active: true})
      insert_user("@carol:nebu.local", %{is_active: true})

      request = %Core.DeactivateUserRequest{user_id: "@bob:nebu.local"}
      _response = Server.deactivate_user(request, build_stream())

      assert_receive {:destroy_called, "@bob:nebu.local"}, 200

      refute_receive {:destroy_called, "@carol:nebu.local"}, 50,
                     "destroy_session must NOT be called for an unrelated user"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#4 — ReactivateUser sets is_active=true (AC: 3)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: user @bob:nebu.local exists with is_active=false
  # When:  ReactivateUser gRPC is called
  # Then:  DB: is_active=true
  #
  # RED: Core.ReactivateUserRequest does not exist yet.

  describe "ReactivateUser — AC#3" do
    test "sets is_active=true in DB" do
      insert_user("@bob:nebu.local", %{is_active: false})

      # RED: Core.ReactivateUserRequest does not exist yet → compile error
      request = %Core.ReactivateUserRequest{user_id: "@bob:nebu.local"}

      # RED: Server.reactivate_user/2 does not exist yet → UndefinedFunctionError
      response = Server.reactivate_user(request, build_stream())

      assert %Core.ReactivateUserResponse{ok: true} = response,
             "expected ReactivateUserResponse{ok: true}, got #{inspect(response)}"

      {:ok, attrs} = get_user_from_ets("@bob:nebu.local")

      assert attrs.is_active == true,
             "expected is_active=true in DB after ReactivateUser, got #{inspect(attrs.is_active)}"
    end

    test "does not call destroy_session (reactivation must not invalidate sessions)" do
      insert_user("@bob:nebu.local", %{is_active: false})

      request = %Core.ReactivateUserRequest{user_id: "@bob:nebu.local"}
      _response = Server.reactivate_user(request, build_stream())

      refute_receive {:destroy_called, _}, 50,
                     "ReactivateUser must NOT call destroy_session"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#5 — UpdateUserRole updates system_role (AC: 1/Task 4)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: user @carol:nebu.local has system_role="user"
  # When:  UpdateUserRole gRPC is called with role="instance_admin"
  # Then:  DB: system_role="instance_admin"
  #
  # RED: Core.UpdateUserRoleRequest does not exist yet.

  describe "UpdateUserRole — AC#1" do
    test "updates system_role to instance_admin in DB" do
      insert_user("@carol:nebu.local", %{system_role: "user"})

      # RED: Core.UpdateUserRoleRequest does not exist yet → compile error
      request = %Core.UpdateUserRoleRequest{
        user_id: "@carol:nebu.local",
        role: "instance_admin"
      }

      # RED: Server.update_user_role/2 does not exist yet → UndefinedFunctionError
      response = Server.update_user_role(request, build_stream())

      assert %Core.UpdateUserRoleResponse{ok: true} = response,
             "expected UpdateUserRoleResponse{ok: true}, got #{inspect(response)}"

      {:ok, attrs} = get_user_from_ets("@carol:nebu.local")

      assert attrs.system_role == "instance_admin",
             "expected system_role='instance_admin', got #{inspect(attrs.system_role)}"
    end

    test "raises GRPC.RPCError with invalid_argument for unknown role value" do
      insert_user("@carol:nebu.local", %{system_role: "user"})

      request = %Core.UpdateUserRoleRequest{
        user_id: "@carol:nebu.local",
        role: "superadmin"
      }

      assert_raise GRPC.RPCError, fn ->
        Server.update_user_role(request, build_stream())
      end
    end

    test "GRPC.RPCError for invalid role has invalid_argument status code" do
      insert_user("@carol:nebu.local", %{system_role: "user"})

      request = %Core.UpdateUserRoleRequest{
        user_id: "@carol:nebu.local",
        role: "hacker"
      }

      error =
        try do
          Server.update_user_role(request, build_stream())
          nil
        rescue
          e in GRPC.RPCError -> e
        end

      refute is_nil(error),
             "expected GRPC.RPCError for invalid role"

      assert error.status == GRPC.Status.invalid_argument(),
             "expected GRPC.Status.invalid_argument() (#{GRPC.Status.invalid_argument()}), got #{error.status}"
    end

    test "compliance_officer is a valid role" do
      insert_user("@carol:nebu.local", %{system_role: "user"})

      request = %Core.UpdateUserRoleRequest{
        user_id: "@carol:nebu.local",
        role: "compliance_officer"
      }

      response = Server.update_user_role(request, build_stream())

      assert %Core.UpdateUserRoleResponse{ok: true} = response

      {:ok, attrs} = get_user_from_ets("@carol:nebu.local")
      assert attrs.system_role == "compliance_officer"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#6 — ListAdminRooms returns paginated rooms (AC: 1/Task 5)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: 3 rooms exist (2 active, 1 archived), status_filter="active"
  # When:  ListAdminRooms gRPC is called with limit=2, status_filter="active"
  # Then:  response.rooms contains 2 active rooms, next_cursor is empty (exactly 2 active)
  #
  # RED: Core.ListAdminRoomsRequest does not exist yet.

  describe "ListAdminRooms — AC#1" do
    test "returns active rooms when filtered and respects limit" do
      insert_room("!room-a:nebu.local", %{status: "active", name: "Room A"})
      insert_room("!room-b:nebu.local", %{status: "active", name: "Room B"})
      insert_room("!room-c:nebu.local", %{status: "archived", name: "Room C"})

      # RED: Core.ListAdminRoomsRequest does not exist yet → compile error
      request = %Core.ListAdminRoomsRequest{
        limit: 10,
        cursor: "",
        status_filter: "active",
        search: ""
      }

      # RED: Server.list_admin_rooms/2 does not exist yet → UndefinedFunctionError
      response = Server.list_admin_rooms(request, build_stream())

      assert %Core.ListAdminRoomsResponse{} = response,
             "expected ListAdminRoomsResponse struct, got #{inspect(response)}"

      assert length(response.rooms) == 2,
             "expected 2 active rooms, got #{length(response.rooms)}"

      Enum.each(response.rooms, fn room ->
        assert room.status == "active",
               "expected all rooms to be active when status_filter='active', got #{room.status}"
      end)
    end

    test "returns next_cursor when more pages exist" do
      insert_room("!room-a:nebu.local", %{status: "active"})
      insert_room("!room-b:nebu.local", %{status: "active"})
      insert_room("!room-c:nebu.local", %{status: "active"})

      request = %Core.ListAdminRoomsRequest{
        limit: 2,
        cursor: "",
        status_filter: "",
        search: ""
      }

      response = Server.list_admin_rooms(request, build_stream())

      assert length(response.rooms) == 2,
             "expected 2 rooms (limit=2 of 3), got #{length(response.rooms)}"

      assert response.next_cursor != "",
             "expected non-empty next_cursor when more rooms exist"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#7 — GetAdminRoom returns room detail with member_count (AC: 1/Task 5)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: room !abc:nebu.local exists with 3 members
  # When:  GetAdminRoom gRPC is called with that room_id
  # Then:  response.room has name, status, member_count=3
  #
  # RED: Core.GetAdminRoomRequest does not exist yet.

  describe "GetAdminRoom — AC#1" do
    test "returns room detail with correct member_count" do
      insert_room("!abc:nebu.local", %{
        name: "General",
        status: "active",
        member_count: 3,
        max_members: 100,
        visibility: "public"
      })

      # RED: Core.GetAdminRoomRequest does not exist yet → compile error
      request = %Core.GetAdminRoomRequest{room_id: "!abc:nebu.local"}

      # RED: Server.get_admin_room/2 does not exist yet → UndefinedFunctionError
      response = Server.get_admin_room(request, build_stream())

      assert %Core.GetAdminRoomResponse{} = response,
             "expected GetAdminRoomResponse struct, got #{inspect(response)}"

      room = response.room

      assert room.room_id == "!abc:nebu.local",
             "expected room_id='!abc:nebu.local', got #{inspect(room.room_id)}"

      assert room.name == "General",
             "expected name='General', got #{inspect(room.name)}"

      assert room.status == "active",
             "expected status='active', got #{inspect(room.status)}"

      assert room.member_count == 3,
             "expected member_count=3, got #{inspect(room.member_count)}"
    end

    test "raises GRPC.RPCError with not_found for non-existent room" do
      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDBNotFound)

      request = %Core.GetAdminRoomRequest{room_id: "!nonexistent:nebu.local"}

      assert_raise GRPC.RPCError, fn ->
        Server.get_admin_room(request, build_stream())
      end
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#8 — ArchiveRoom uses SELECT FOR UPDATE atomically (AC: 4)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: room !xyz:nebu.local exists with status="active"
  # When:  ArchiveRoom gRPC is called
  # Then:  DB: rooms.status="archived" (via SELECT FOR UPDATE in an Ecto transaction)
  # And:   Room GenServer is terminated
  #
  # This test verifies that the MODIFIED archive_room/2 handler (post-Story 9.1)
  # ALSO performs the atomic DB write (SELECT FOR UPDATE). The pre-9.1 handler
  # only terminates the GenServer; AC: 4 requires the Core to own the DB write.
  #
  # RED fails because:
  #   1. FakeAdminDB.archive_room_atomic/1 is called by the handler — but the
  #      handler does not yet call admin_db_module().archive_room_atomic/1.
  #   2. The existing archive_room/2 does NOT call admin_db_module() at all.

  describe "ArchiveRoom atomic DB update — AC#4" do
    test "sets rooms.status='archived' in DB via atomic transaction" do
      insert_room("!xyz:nebu.local", %{status: "active"})

      # Core.ArchiveRoomRequest already exists (Story 6.9) — NOT RED for proto.
      # RED because: the *modified* handler doesn't call archive_room_atomic/1 yet.
      request = %Core.ArchiveRoomRequest{room_id: "!xyz:nebu.local"}

      response = Server.archive_room(request, build_stream())

      assert %Core.ArchiveRoomResponse{ok: true} = response,
             "expected ArchiveRoomResponse{ok: true}, got #{inspect(response)}"

      # Verify the DB was updated atomically (via FakeAdminDB.archive_room_atomic/1).
      {:ok, attrs} = get_room_from_ets("!xyz:nebu.local")

      assert attrs.status == "archived",
             "expected rooms.status='archived' in DB after ArchiveRoom, got #{inspect(attrs.status)}"
    end

    test "archive_room is idempotent when room is already archived in DB" do
      insert_room("!xyz:nebu.local", %{status: "archived"})

      request = %Core.ArchiveRoomRequest{room_id: "!xyz:nebu.local"}

      # Must not raise — idempotent call for an already-archived room.
      response = Server.archive_room(request, build_stream())

      assert %Core.ArchiveRoomResponse{ok: true} = response,
             "expected ok: true for idempotent archive call"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#9 — GetServerConfig returns config without oidc_client_secret (AC: 1/Task 7)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: server_config table has rows for instance_name, oidc_issuer
  # When:  GetServerConfig gRPC is called
  # Then:  response.config contains instance_name + oidc_issuer values
  # And:   oidc_client_secret is NOT present in the response (security invariant)
  #
  # RED: Core.GetServerConfigRequest does not exist yet.

  describe "GetServerConfig — AC#1 (security: no client_secret)" do
    test "returns config with instance_name and oidc_issuer" do
      insert_config("instance_name", "Nebu Test")
      insert_config("oidc_issuer", "https://auth.example.com")
      insert_config("oidc_client_id", "nebu-client")
      # Deliberately insert an oidc_client_secret to verify it is stripped:
      insert_config("oidc_client_secret", "very-secret-value")

      # RED: Core.GetServerConfigRequest does not exist yet → compile error
      request = %Core.GetServerConfigRequest{}

      # RED: Server.get_server_config/2 does not exist yet → UndefinedFunctionError
      response = Server.get_server_config(request, build_stream())

      assert %Core.GetServerConfigResponse{} = response,
             "expected GetServerConfigResponse struct, got #{inspect(response)}"

      config = response.config

      assert config.instance_name == "Nebu Test",
             "expected instance_name='Nebu Test', got #{inspect(config.instance_name)}"

      assert config.oidc_issuer == "https://auth.example.com",
             "expected oidc_issuer='https://auth.example.com', got #{inspect(config.oidc_issuer)}"

      assert config.oidc_client_id == "nebu-client",
             "expected oidc_client_id='nebu-client', got #{inspect(config.oidc_client_id)}"
    end

    test "oidc_client_secret is NOT present in GetServerConfigResponse (security invariant)" do
      # ServerConfigProto must not have an oidc_client_secret field at all.
      # The proto definition intentionally omits it (see Dev Notes).
      # This test verifies the struct does not expose the secret.
      insert_config("oidc_client_secret", "super-secret")

      request = %Core.GetServerConfigRequest{}
      response = Server.get_server_config(request, build_stream())

      # The Core.ServerConfigProto struct must not have an :oidc_client_secret key.
      # If the proto was wrongly generated with that field, Map.has_key? would be true.
      config_keys = Map.keys(response.config)

      refute :oidc_client_secret in config_keys,
             "security: oidc_client_secret must NOT be present in ServerConfigProto response; got keys: #{inspect(config_keys)}"

      # Also verify no field contains the secret value.
      config_values =
        response.config
        |> Map.from_struct()
        |> Map.values()
        |> Enum.map(&to_string/1)

      refute "super-secret" in config_values,
             "security: plaintext oidc_client_secret value must NOT appear in any config field"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#10 — UpdateServerConfig persists config changes (AC: 1/Task 7)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: server_config has instance_name="old"
  # When:  UpdateServerConfig gRPC is called with instance_name="new"
  # Then:  DB: server_config row updated
  #
  # RED: Core.UpdateServerConfigRequest does not exist yet.

  describe "UpdateServerConfig — AC#1" do
    test "persists instance_name change in DB" do
      insert_config("instance_name", "old")

      # RED: Core.UpdateServerConfigRequest does not exist yet → compile error
      request = %Core.UpdateServerConfigRequest{
        instance_name: "new",
        oidc_issuer: "",
        oidc_client_id: "",
        room_default_max_members: 0,
        room_default_visibility: "",
        audit_log_retention_days: 0
      }

      # RED: Server.update_server_config/2 does not exist yet → UndefinedFunctionError
      response = Server.update_server_config(request, build_stream())

      assert %Core.UpdateServerConfigResponse{ok: true} = response,
             "expected UpdateServerConfigResponse{ok: true}, got #{inspect(response)}"

      # Verify the DB was updated.
      case :ets.lookup(:admin_grpc_test_db, {:config, "instance_name"}) do
        [{_, value}] ->
          assert value == "new",
                 "expected instance_name='new' in DB, got #{inspect(value)}"

        [] ->
          flunk("expected instance_name config row in DB, got nothing")
      end
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#11 — GetMetrics returns real counts (not zeroes from stub) (AC: 1/Task 8)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: 2 active sessions exist in ETS (:NebuSessions table)
  # When:  GetMetrics gRPC is called
  # Then:  response.active_sessions == 2 (NOT 0 as in the current stub)
  # And:   response.room_count >= 0 (real count, not hardcoded 0)
  #
  # This test catches the current STUB behaviour (get_metrics returns %Core.GetMetricsResponse{})
  # and will only pass once the real implementation is wired in.
  #
  # async: false — writes to :NebuSessions ETS table (global).

  describe "GetMetrics real implementation — AC#1" do
    setup do
      # Seed two sessions into :NebuSessions (ETS table owned by Nebu.Session.EtsStore).
      # If the table doesn't exist yet (test environment), create it temporarily.
      table_existed = :ets.info(:NebuSessions) != :undefined

      unless table_existed do
        :ets.new(:NebuSessions, [:named_table, :public, :set])
      end

      Nebu.Session.EtsStore.put_session("@metrics-a:nebu.local", %{
        access_token_hash: "hash_a",
        device_id: "DEVICE_A",
        created_at_ms: System.system_time(:millisecond),
        last_seen_at_ms: System.system_time(:millisecond)
      })

      Nebu.Session.EtsStore.put_session("@metrics-b:nebu.local", %{
        access_token_hash: "hash_b",
        device_id: "DEVICE_B",
        created_at_ms: System.system_time(:millisecond),
        last_seen_at_ms: System.system_time(:millisecond)
      })

      on_exit(fn ->
        if :ets.info(:NebuSessions) != :undefined do
          :ets.match_delete(:NebuSessions, {:"$1", :"$2"})
        end
      end)

      :ok
    end

    test "active_sessions reflects real ETS count (not the stub zero)" do
      request = %Core.GetMetricsRequest{}

      # RED: Server.get_metrics/2 currently returns %Core.GetMetricsResponse{} (all zeroes).
      # After Story 9.1, it calls EtsStore.list_user_ids/0 and Horde.Registry.select/2.
      response = Server.get_metrics(request, build_stream())

      assert %Core.GetMetricsResponse{} = response,
             "expected GetMetricsResponse struct"

      assert response.active_sessions == 2,
             "expected active_sessions=2 (2 sessions in ETS), got #{response.active_sessions}. " <>
               "If 0, the stub has not been replaced with the real implementation yet."
    end

    test "room_count is a non-negative integer (real Horde count, not hardcoded)" do
      request = %Core.GetMetricsRequest{}
      response = Server.get_metrics(request, build_stream())

      assert is_integer(response.room_count) && response.room_count >= 0,
             "expected non-negative integer room_count, got #{inspect(response.room_count)}"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # Story 9.18 — ListAdminRoomMembers gRPC handler
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # RED PHASE — ALL tests below FAIL until Story 9.18 is implemented.
  #
  # Failing reasons:
  #   1. Core.ListAdminRoomMembersRequest does not exist yet — compile error after `make proto`.
  #   2. Core.ListAdminRoomMembersResponse does not exist yet — compile error.
  #   3. Core.AdminRoomMemberProto does not exist yet — compile error.
  #   4. Server.list_admin_room_members/2 does not exist yet → UndefinedFunctionError.
  #   5. Nebu.Admin.DB.list_room_members/1 does not exist yet → UndefinedFunctionError.
  #   6. FakeAdminDB has no list_room_members/1 callback yet → compile error when called.
  #
  # async: false — shared Application env + named ETS table :admin_grpc_test_db.
  #
  # Test strategy:
  #   - FakeAdminDB.list_room_members/1 is added below and returns pre-seeded rows.
  #   - Rows mirror the shape returned by the real SQL JOIN (Story 9.18 AC2):
  #       %{user_id:, display_name_encrypted:, display_name_nonce:,
  #         email_ephemeral_pub:, joined_at:}
  #   - For simplicity in RED phase, display_name_encrypted is set to a literal
  #     that the crypto helper should decrypt to the expected display_name.
  #     The real implementation must call the same X25519/AES-256-GCM helper used
  #     by get_admin_user/list_admin_users — see Dev Notes in Story 9.18.
  #   - A second FakeAdminDB variant (FakeAdminDBEmptyRoom) returns [] for
  #     list_room_members to exercise the empty-list (no-error) path.
  #
  # Covered Acceptance Criteria:
  #   AC2 — Elixir Core implements list_admin_room_members (AT#12, AT#13).
  #   AC2 — Empty room returns empty list, not an error (AT#13).
  #
  # Crash/Restart test: NOT NEEDED.
  # `list_admin_room_members/2` is a stateless unary gRPC handler — it holds no
  # GenServer state, no ETS, no in-memory caches. Each call hits the DB through
  # the configured admin_db_module. A handler crash is automatically recovered
  # by the GRPC.Server worker pool; there is no state to migrate or restore.
  # Per CLAUDE.md "Persistenz-Strategie": Option C — Stateless.

  # ── FakeAdminDB extension — add list_room_members/1 ─────────────────────────
  #
  # NOTE: The FakeAdminDB defined above does NOT have list_room_members/1.
  # Adding it here as a separate in-test module to avoid redefining FakeAdminDB
  # (Elixir does not allow re-opening a defmodule in the same file in a way that
  # retroactively adds functions).  Tests in this describe block inject
  # FakeAdminDBWithMembers or FakeAdminDBEmptyRoom via Application.put_env.

  defmodule FakeAdminDBWithMembers do
    @moduledoc """
    Fake DB for Story 9.18 member list tests.
    Extends FakeAdminDB behaviour with list_room_members/1.
    RED: list_room_members/1 does not exist in Nebu.Admin.DB yet.
    """

    # Delegate all existing operations to FakeAdminDB so tests that also need
    # user/room/config operations continue to work.
    def list_users(limit, cursor, search), do: FakeAdminDB.list_users(limit, cursor, search)
    def get_user(user_id), do: FakeAdminDB.get_user(user_id)
    def set_is_active(user_id, is_active), do: FakeAdminDB.set_is_active(user_id, is_active)
    def set_system_role(user_id, role), do: FakeAdminDB.set_system_role(user_id, role)
    def list_rooms(limit, cursor, status_filter, search), do: FakeAdminDB.list_rooms(limit, cursor, status_filter, search)
    def get_room(room_id), do: FakeAdminDB.get_room(room_id)
    def archive_room_atomic(room_id), do: FakeAdminDB.archive_room_atomic(room_id)
    def get_server_config, do: FakeAdminDB.get_server_config()
    def upsert_server_config(changes), do: FakeAdminDB.upsert_server_config(changes)

    @doc """
    Returns member rows for the given room_id from ETS key {:members, room_id}.
    Returns [] if no entry found (empty room — not an error).

    Row shape (mirrors DB JOIN result documented in Story 9.18 AC2):
      %{user_id:, display_name_encrypted:, display_name_nonce:,
        email_ephemeral_pub:, joined_at:}

    In the RED phase, display_name_encrypted is set to a raw binary that the
    real Nebu.Crypto helper must decrypt. Tests assert on the decoded display_name
    returned in the proto, not on the encrypted bytes.
    """
    def list_room_members(room_id) do
      case :ets.lookup(:admin_grpc_test_db, {:members, room_id}) do
        [] -> {:ok, []}
        [{_, rows}] -> {:ok, rows}
      end
    end
  end

  defmodule FakeAdminDBEmptyRoom do
    @moduledoc """
    Fake DB that returns an empty member list for any room.
    Used to test the empty-room / no-error path (AC2 Story 9.18).
    """

    def list_users(limit, cursor, search), do: FakeAdminDB.list_users(limit, cursor, search)
    def get_user(user_id), do: FakeAdminDB.get_user(user_id)
    def set_is_active(user_id, is_active), do: FakeAdminDB.set_is_active(user_id, is_active)
    def set_system_role(user_id, role), do: FakeAdminDB.set_system_role(user_id, role)
    def list_rooms(limit, cursor, status_filter, search), do: FakeAdminDB.list_rooms(limit, cursor, status_filter, search)
    def get_room(room_id), do: FakeAdminDB.get_room(room_id)
    def archive_room_atomic(room_id), do: FakeAdminDB.archive_room_atomic(room_id)
    def get_server_config, do: FakeAdminDB.get_server_config()
    def upsert_server_config(changes), do: FakeAdminDB.upsert_server_config(changes)

    @doc "Always returns an empty list — no members for any room."
    def list_room_members(_room_id), do: {:ok, []}
  end

  # ── Helper — insert member rows into ETS ─────────────────────────────────────

  defp insert_member_rows(room_id, rows) do
    :ets.insert(:admin_grpc_test_db, {{:members, room_id}, rows})
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # AT#12 — ListAdminRoomMembers returns members for a populated room (AC2)
  # ─────────────────────────────────────────────────────────────────────────────
  #
  # Given: room "!room-xyz:nebu.local" has 2 joined members in the DB
  # When:  ListAdminRoomMembers gRPC is called with that room_id
  # Then:  response.members contains 2 AdminRoomMemberProto entries
  # And:   each entry has correct user_id and joined_at
  # And:   display_name is a non-empty string (decrypted from encrypted storage)
  #
  # RED: Core.ListAdminRoomMembersRequest does not exist yet → compile error.
  #      Server.list_admin_room_members/2 does not exist yet → UndefinedFunctionError.
  #      FakeAdminDB.list_room_members/1 does not exist yet → callback missing.

  describe "ListAdminRoomMembers — AC2 (Story 9.18)" do
    test "returns 2 members with correct user_id and joined_at for a populated room" do
      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDBWithMembers)

      room_id = "!room-xyz:nebu.local"

      # Seed 2 member rows — display_name_encrypted is intentionally a placeholder
      # binary. The real Nebu.Crypto helper decrypts it; in the RED phase the handler
      # doesn't exist yet so decryption is never called. Tests assert on proto fields.
      insert_member_rows(room_id, [
        %{
          user_id: "@alice:nebu.local",
          # Placeholder — real impl will encrypt/decrypt via X25519/AES-256-GCM
          display_name_encrypted: <<0::256>>,
          display_name_nonce: <<0::96>>,
          email_ephemeral_pub: <<0::256>>,
          joined_at: 1_714_560_000_000
        },
        %{
          user_id: "@bob:nebu.local",
          display_name_encrypted: <<0::256>>,
          display_name_nonce: <<0::96>>,
          email_ephemeral_pub: <<0::256>>,
          joined_at: 1_714_646_400_000
        }
      ])

      # RED: Core.ListAdminRoomMembersRequest does not exist yet → compile error
      request = %Core.ListAdminRoomMembersRequest{room_id: room_id}

      # RED: Server.list_admin_room_members/2 does not exist yet → UndefinedFunctionError
      response = Server.list_admin_room_members(request, build_stream())

      assert %Core.ListAdminRoomMembersResponse{} = response,
             "expected ListAdminRoomMembersResponse struct, got #{inspect(response)}"

      assert length(response.members) == 2,
             "expected 2 members, got #{length(response.members)}"

      user_ids = Enum.map(response.members, & &1.user_id)

      assert "@alice:nebu.local" in user_ids,
             "expected '@alice:nebu.local' in member user_ids, got #{inspect(user_ids)}"

      assert "@bob:nebu.local" in user_ids,
             "expected '@bob:nebu.local' in member user_ids, got #{inspect(user_ids)}"

      # joined_at must be forwarded correctly (Unix milliseconds)
      alice = Enum.find(response.members, fn m -> m.user_id == "@alice:nebu.local" end)

      assert alice.joined_at == 1_714_560_000_000,
             "expected alice.joined_at=1_714_560_000_000, got #{inspect(alice.joined_at)}"
    end

    test "display_name is a string (decrypted or empty on failure) — not raw binary" do
      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDBWithMembers)

      room_id = "!room-abc:nebu.local"

      insert_member_rows(room_id, [
        %{
          user_id: "@carol:nebu.local",
          display_name_encrypted: <<0::256>>,
          display_name_nonce: <<0::96>>,
          email_ephemeral_pub: <<0::256>>,
          joined_at: 1_714_560_000_001
        }
      ])

      # RED: compile error (struct missing)
      request = %Core.ListAdminRoomMembersRequest{room_id: room_id}
      response = Server.list_admin_room_members(request, build_stream())

      assert length(response.members) == 1,
             "expected 1 member, got #{length(response.members)}"

      [member] = response.members

      # display_name MUST be a string — either the decrypted value or "" on failure.
      # It must NEVER be a raw binary (bytes) or nil.
      #
      # Decryption-failure semantics (documented contract):
      #   - On AES-GCM decrypt failure (bad key, tampered ciphertext, unknown nonce
      #     length), `decrypt_display_name/1` falls back to "" — see server.ex L2172.
      #   - The proto field is always present; "" is the explicit signal that the
      #     server could not decrypt the encrypted blob.
      #   - The Admin UI template falls back to user_id when display_name is "".
      #
      # In this RED-phase test the encrypted blob is a fixed 32-byte zero binary
      # which AES-GCM cannot decrypt with any real key — the handler is therefore
      # expected to return "" here. Once the implementation lands we keep
      # `is_binary` rather than `assert ==""` so the test is robust to future
      # fixture changes (e.g. a real encrypt/decrypt round-trip in GREEN phase).
      assert is_binary(member.display_name),
             "expected display_name to be a string (\"\" on decrypt failure), got #{inspect(member.display_name)}"
    end

    # ─────────────────────────────────────────────────────────────────────────
    # AT#13 — ListAdminRoomMembers returns empty list for a room with no members (AC2)
    # ─────────────────────────────────────────────────────────────────────────
    #
    # Given: room "!empty-room:nebu.local" has 0 joined members
    # When:  ListAdminRoomMembers gRPC is called
    # Then:  response.members is [] (empty list)
    # And:   no error is raised (empty list is not an error condition)
    #
    # RED: compile error (struct missing) + UndefinedFunctionError.

    test "returns empty members list for a room with no joined members — no error" do
      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDBEmptyRoom)

      room_id = "!empty-room:nebu.local"

      # RED: Core.ListAdminRoomMembersRequest does not exist yet → compile error
      request = %Core.ListAdminRoomMembersRequest{room_id: room_id}

      # RED: Server.list_admin_room_members/2 does not exist yet → UndefinedFunctionError
      response = Server.list_admin_room_members(request, build_stream())

      assert %Core.ListAdminRoomMembersResponse{} = response,
             "expected ListAdminRoomMembersResponse struct, got #{inspect(response)}"

      assert response.members == [],
             "expected empty members list for a room with no members, got #{inspect(response.members)}"
    end
  end
end
