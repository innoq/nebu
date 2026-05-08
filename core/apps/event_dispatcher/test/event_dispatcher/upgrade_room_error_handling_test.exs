defmodule Nebu.EventDispatcher.UpgradeRoomErrorHandlingTest do
  use ExUnit.Case, async: false

  # ─── Story 9-27: Room Upgrade 500 Error — error handling unit tests ──────────
  #
  # Verifies the four bug fixes in upgrade_room/2 (server.ex):
  #   - AC1: successful upgrade returns non-empty new_room_id
  #   - AC2: join failure → GRPC.RPCError with Status.internal(), not MatchError
  #   - AC3: m.room.create emit failure → GRPC.RPCError with Status.internal()
  #   - Additional: set_power_levels failure → GRPC.RPCError with Status.internal()
  #   - AC5: archive_room_atomic/1 is called for old room after upgrade
  #   - AC5: old room GenServer is stopped after upgrade
  #
  # DB injection:
  #   Application.put_env(:room_manager, :db_module, ...)
  #   Application.put_env(:event_dispatcher, :messages_db_module, ...)
  #   Application.put_env(:event_dispatcher, :admin_db_module, ...)
  #
  # async: false — Horde, Application.put_env, and ETS are process-global.

  alias Nebu.EventDispatcher.Server

  # ─── NoOpAuditWriter ─────────────────────────────────────────────────────────

  defmodule NoOpAuditWriter do
    def log(_, _, _, _, _, _, _ \\ nil), do: :ok
  end

  # ─── NoOpAdminDB ──────────────────────────────────────────────────────────────
  #
  # Used in tests that do NOT test the archive path — avoids coupling.

  defmodule NoOpAdminDB do
    def list_users(_limit, _cursor, _search), do: {[], ""}
    def get_user(_user_id), do: {:error, :not_found}
    def set_is_active(_user_id, _is_active), do: :ok
    def set_system_role(_user_id, _role), do: :ok
    def list_rooms(_limit, _cursor, _status_filter, _search), do: {[], ""}
    def get_room(_room_id), do: {:error, :not_found}
    def archive_room_atomic(_room_id), do: :ok
    def get_server_config, do: {:ok, %{}}
    def upsert_server_config(_changes), do: :ok
  end

  # ─── SpyAdminDB ───────────────────────────────────────────────────────────────
  #
  # Spy that records archive_room_atomic/1 calls in ETS so tests can assert
  # that the old room was archived after upgrade.

  defmodule SpyAdminDB do
    def list_users(_limit, _cursor, _search), do: {[], ""}
    def get_user(_user_id), do: {:error, :not_found}
    def set_is_active(_user_id, _is_active), do: :ok
    def set_system_role(_user_id, _role), do: :ok
    def list_rooms(_limit, _cursor, _status_filter, _search), do: {[], ""}
    def get_room(_room_id), do: {:error, :not_found}

    def archive_room_atomic(room_id) do
      prev =
        case :ets.lookup(:upgrade_err_test_db, :archived_rooms) do
          [{_, list}] -> list
          [] -> []
        end
      :ets.insert(:upgrade_err_test_db, {:archived_rooms, prev ++ [room_id]})
      :ok
    end

    def get_server_config, do: {:ok, %{}}
    def upsert_server_config(_changes), do: :ok
  end

  # ─── BaseDB ───────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying Nebu.Room.DBBehaviour — used as the room_manager
  # DB for all tests (Room GenServer creation and membership tracking).
  # Table: :upgrade_err_test_db

  defmodule BaseDB do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id) do
      case :ets.lookup(:upgrade_err_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:upgrade_err_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:upgrade_err_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:upgrade_err_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:upgrade_err_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:upgrade_err_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:upgrade_err_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:upgrade_err_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    def load_room_settings(_room_id), do: {:ok, 0}
    def get_room_status(_room_id), do: {:ok, "active"}
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _from), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}

    def get_room_create_event(room_id) do
      all_events =
        :ets.match(:upgrade_err_test_db, {{:event, :_}, :"$1"})
        |> Enum.map(fn [e] -> e end)

      case Enum.find(all_events, fn e ->
        e["type"] == "m.room.create" and e["room_id"] == room_id
      end) do
        nil -> {:error, :not_found}
        event -> {:ok, event["content"]}
      end
    end

    # Story 9-28: no thread relations in upgrade error tests — return empty / 0.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── DBJoinError ──────────────────────────────────────────────────────────────
  #
  # Overrides insert_member to return {:error, :db_error}.
  # Used as room_manager :db_module so that Nebu.Room.Server.join/2 returns
  # {:error, :db_error} instead of :ok when the new room tries to add the requester.
  #
  # This triggers the bug: `:ok = Nebu.Room.Server.join(...)` causes MatchError.
  # The fix must replace the bare match with a case expression that raises
  # GRPC.RPCError with GRPC.Status.internal().

  defmodule DBJoinError do
    @behaviour Nebu.Room.DBBehaviour

    # load_members/1: return :not_found for unknown rooms so GenServer init fails
    # gracefully before insert_member, but the NEW room created by upgrade_room
    # will have BaseDB data from the initial create_room call.
    # We only override insert_member — all other callbacks delegate to BaseDB.
    def load_members(room_id) do
      # For the OLD room (already set up), delegate to BaseDB.
      # For the NEW room (started during upgrade), also delegate — but the join call
      # will fail at insert_member level.
      BaseDB.load_members(room_id)
    end

    def insert_room(room_id), do: BaseDB.insert_room(room_id)

    # This is the failing insert: returns {:error, :db_error} instead of :ok.
    # Room.Server.join/2 calls this, propagating {:error, :db_error} upward.
    # With the bug, upgrade_room/2 does `:ok = {:error, :db_error}` → MatchError.
    def insert_member(_room_id, _user_id) do
      {:error, :db_error}
    end

    def delete_member(room_id, user_id), do: BaseDB.delete_member(room_id, user_id)
    def insert_event(event), do: BaseDB.insert_event(event)
    def set_power_levels(room_id, pl_json), do: BaseDB.set_power_levels(room_id, pl_json)
    def load_room_settings(_room_id), do: {:ok, 0}
    def get_room_status(_room_id), do: {:ok, "active"}
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _from), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}

    def get_room_create_event(room_id) do
      BaseDB.get_room_create_event(room_id)
    end

    # Story 9-28: no thread relations in upgrade error tests.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── DBSetPowerLevelsError ────────────────────────────────────────────────────
  #
  # Overrides set_power_levels to return {:error, :db_error}.
  # Used as room_manager :db_module so that Nebu.Room.Server.set_power_levels/3
  # returns {:error, :db_error} when called for the new room.
  #
  # This triggers the bug: `:ok = Nebu.Room.Server.set_power_levels(...)` causes MatchError.
  # The fix must replace the bare match with a case that raises GRPC.RPCError.

  defmodule DBSetPowerLevelsError do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id), do: BaseDB.load_members(room_id)
    def insert_room(room_id), do: BaseDB.insert_room(room_id)

    # Allow join/insert_member to succeed so we reach set_power_levels.
    def insert_member(room_id, user_id) do
      BaseDB.insert_member(room_id, user_id)
    end

    def delete_member(room_id, user_id), do: BaseDB.delete_member(room_id, user_id)
    def insert_event(event), do: BaseDB.insert_event(event)

    # This is the failing path: set_power_levels returns {:error, :db_error}.
    # Room.Server.set_power_levels/3 calls this, propagating the error upward.
    # With the bug, upgrade_room/2 does `:ok = {:error, :db_error}` → MatchError.
    def set_power_levels(_room_id, _power_levels_json) do
      {:error, :db_error}
    end

    def load_room_settings(_room_id), do: {:ok, 0}
    def get_room_status(_room_id), do: {:ok, "active"}
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _from), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}

    def get_room_create_event(room_id) do
      BaseDB.get_room_create_event(room_id)
    end

    # Story 9-28: no thread relations in upgrade error tests.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── MessagesDBCreateEventError ───────────────────────────────────────────────
  #
  # Used as messages_db_module. Overrides insert_event to return {:error, :db_error}
  # only for "m.room.create" events in new rooms (to simulate a DB failure during
  # the m.room.create emit in the new room).
  #
  # This triggers the bug: the return value of
  #   emit_state_event(new_room_id, ..., "m.room.create", ...) is ignored.
  # The fix must check the return value and raise GRPC.RPCError on {:error, _}.
  #
  # Note: we allow the old room's tombstone (m.room.tombstone) to succeed so the
  # test can isolate the m.room.create failure path specifically.

  defmodule MessagesDBCreateEventError do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id), do: BaseDB.load_members(room_id)
    def insert_room(room_id), do: BaseDB.insert_room(room_id)
    def insert_member(room_id, user_id), do: BaseDB.insert_member(room_id, user_id)
    def delete_member(room_id, user_id), do: BaseDB.delete_member(room_id, user_id)

    # Fail the insert for m.room.create events, succeed for everything else.
    def insert_event(event) do
      if event["type"] == "m.room.create" do
        {:error, :db_error}
      else
        BaseDB.insert_event(event)
      end
    end

    def set_power_levels(room_id, pl_json), do: BaseDB.set_power_levels(room_id, pl_json)
    def load_room_settings(_room_id), do: {:ok, 0}
    def get_room_status(_room_id), do: {:ok, "active"}
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _from), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}

    def get_room_create_event(room_id) do
      BaseDB.get_room_create_event(room_id)
    end

    # Story 9-28: no thread relations in upgrade error tests.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── FakeInviteDB ─────────────────────────────────────────────────────────────

  defmodule FakeInviteDB do
    def insert_invitation(_room_id, _inviter, _invitee), do: :ok
    def accept_invitation(_room_id, _invitee_id), do: :ok
    def reject_invitation(_room_id, _invitee_id), do: :ok
    def get_pending_invite_rooms_for_user(_user_id), do: {:ok, []}
    def get_declined_invite_rooms_for_user(_user_id), do: {:ok, []}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:upgrade_err_test_db) != :undefined do
      :ets.delete(:upgrade_err_test_db)
    end
    :ets.new(:upgrade_err_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager,     :db_module,           BaseDB)
    Application.put_env(:event_dispatcher, :messages_db_module,  BaseDB)
    Application.put_env(:event_dispatcher, :invite_db_module,    FakeInviteDB)
    Application.put_env(:event_dispatcher, :admin_db_module,     NoOpAdminDB)
    Application.put_env(:event_dispatcher, :server_name,         "test.local")
    Application.put_env(:compliance,       :audit_writer,        NoOpAuditWriter)

    case :pg.start_link() do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager,     :db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :invite_db_module)
      Application.delete_env(:event_dispatcher, :admin_db_module)
      Application.delete_env(:event_dispatcher, :server_name)
      Application.delete_env(:compliance,       :audit_writer)

      if :ets.info(:upgrade_err_test_db) != :undefined do
        :ets.delete(:upgrade_err_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Helpers ─────────────────────────────────────────────────────────────────

  defp build_stream(user_id) do
    %{http_request_headers: %{"x-user-id" => user_id}}
  end

  # Create a room via Server.create_room/2 so the old room GenServer is running
  # and the upgrader has power_level 100. Returns {old_room_id, upgrader_id}.
  defp setup_old_room(upgrader_id) do
    create_req = %Core.CreateRoomRequest{
      creator_id: upgrader_id,
      name:       "err-test-room-#{System.unique_integer([:positive])}"
    }
    response = Server.create_room(create_req, build_stream(upgrader_id))
    old_room_id = response.room_id

    on_exit(fn ->
      case Nebu.Room.RoomSupervisor.lookup_room(old_room_id) do
        {:ok, pid} ->
          if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
        _ -> :ok
      end
    end)

    {old_room_id, upgrader_id}
  end

  # ─── AC1 — upgrade_room/2: successful upgrade returns new_room_id ────────────
  #
  # Regression guard: verifies the happy path still works after AC2–AC5 error-handling
  # fixes are applied. Owner upgrades successfully and receives a non-empty new_room_id.

  describe "AC1 — upgrade_room/2: successful upgrade returns new_room_id" do
    test "owner upgrades room and receives non-empty replacement_room id" do
      upgrader_id = "@kai:test.local"
      {old_room_id, _} = setup_old_room(upgrader_id)

      request = %Core.UpgradeRoomRequest{
        old_room_id:  old_room_id,
        requester_id: upgrader_id,
        new_version:  "10"
      }

      response = Server.upgrade_room(request, build_stream(upgrader_id))

      on_exit(fn ->
        case Nebu.Room.RoomSupervisor.lookup_room(response.new_room_id) do
          {:ok, pid} -> if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
          _ -> :ok
        end
      end)

      assert is_binary(response.new_room_id), "expected new_room_id to be a string"
      assert String.length(response.new_room_id) > 0, "expected non-empty new_room_id"
      assert response.new_room_id != old_room_id, "expected new_room_id to differ from old_room_id"
    end
  end

  # ─── AC2 — join failure → GRPC.RPCError internal ─────────────────────────────
  #
  # Verifies: when Room.Server.join/2 returns {:error, :db_error} (via FakeDB),
  # upgrade_room/2 raises GRPC.RPCError with Status.internal() (code 13),
  # NOT MatchError (which would produce codes.Unknown).

  describe "AC2 — upgrade_room/2: join failure → GRPC.RPCError internal (not MatchError)" do
    test "join returning {:error, :db_error} raises GRPC.RPCError with internal status" do
      upgrader_id = "@kai:test.local"
      {old_room_id, _} = setup_old_room(upgrader_id)

      # Swap room_manager DB so new room joins fail at the DB level.
      Application.put_env(:room_manager, :db_module, DBJoinError)

      request = %Core.UpgradeRoomRequest{
        old_room_id:  old_room_id,
        requester_id: upgrader_id,
        new_version:  "10"
      }

      error = assert_raise GRPC.RPCError, fn ->
        Server.upgrade_room(request, build_stream(upgrader_id))
      end

      assert error.status == GRPC.Status.internal(),
             "expected GRPC.Status.internal() (13), got status #{error.status}"
    end
  end

  # ─── AC2 (set_power_levels) — set_power_levels returns {:error, :db_error} ───
  #
  # Verifies: when Room.Server.set_power_levels/3 returns {:error, :db_error},
  # upgrade_room/2 raises GRPC.RPCError with Status.internal() (code 13).

  describe "AC2 — upgrade_room/2: set_power_levels failure → GRPC.RPCError internal" do
    test "set_power_levels returning {:error, :db_error} raises GRPC.RPCError with internal status" do
      upgrader_id = "@kai:test.local"
      {old_room_id, _} = setup_old_room(upgrader_id)

      # Switch room_manager DB so the new room's set_power_levels DB write fails.
      # DBSetPowerLevelsError allows join (insert_member) to succeed but fails
      # the set_power_levels call.
      Application.put_env(:room_manager, :db_module, DBSetPowerLevelsError)

      request = %Core.UpgradeRoomRequest{
        old_room_id:  old_room_id,
        requester_id: upgrader_id,
        new_version:  "10"
      }

      error = assert_raise GRPC.RPCError, fn ->
        Server.upgrade_room(request, build_stream(upgrader_id))
      end

      assert error.status == GRPC.Status.internal(),
             "expected GRPC.Status.internal() (13), got status #{error.status}"
    end
  end

  # ─── AC3 — emit_state_event for m.room.create returns {:error, :db_error} ─────
  #
  # Verifies: when the DB insert for m.room.create fails, upgrade_room/2 raises
  # GRPC.RPCError with Status.internal() instead of silently swallowing the error.

  describe "AC3 — upgrade_room/2: emit_state_event(m.room.create) failure → GRPC.RPCError" do
    test "DB failure during m.room.create emit raises GRPC.RPCError with internal status" do
      upgrader_id = "@kai:test.local"
      {old_room_id, _} = setup_old_room(upgrader_id)

      # Switch messages_db_module to one that fails insert_event for m.room.create.
      Application.put_env(:event_dispatcher, :messages_db_module, MessagesDBCreateEventError)

      request = %Core.UpgradeRoomRequest{
        old_room_id:  old_room_id,
        requester_id: upgrader_id,
        new_version:  "10"
      }

      error = assert_raise GRPC.RPCError, fn ->
        Server.upgrade_room(request, build_stream(upgrader_id))
      end

      assert error.status == GRPC.Status.internal(),
             "expected GRPC.Status.internal() (13) when m.room.create DB insert fails, got #{error.status}"
    end
  end

  # ─── AC5 — old room is archived after successful upgrade ─────────────────────
  #
  # Verifies Matrix spec §11.35.1: after upgrade, archive_room_atomic/1 must be
  # called for the old room and its GenServer must be terminated.

  describe "AC5 — old room is archived after upgrade (Matrix spec §11.35.1)" do
    test "archive_room_atomic/1 is called for the old room after successful upgrade" do
      upgrader_id = "@kai:test.local"
      {old_room_id, _} = setup_old_room(upgrader_id)

      # Inject SpyAdminDB so we can observe archive_room_atomic calls.
      Application.put_env(:event_dispatcher, :admin_db_module, SpyAdminDB)

      request = %Core.UpgradeRoomRequest{
        old_room_id:  old_room_id,
        requester_id: upgrader_id,
        new_version:  "10"
      }

      response = Server.upgrade_room(request, build_stream(upgrader_id))
      new_room_id = response.new_room_id

      on_exit(fn ->
        case Nebu.Room.RoomSupervisor.lookup_room(new_room_id) do
          {:ok, pid} ->
            if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
          _ -> :ok
        end
      end)

      archived_rooms =
        case :ets.lookup(:upgrade_err_test_db, :archived_rooms) do
          [{_, list}] -> list
          [] -> []
        end

      assert old_room_id in archived_rooms,
             "expected archive_room_atomic/1 to be called with old_room_id=#{old_room_id} " <>
             "after upgrade, but it was not called. Matrix spec §11.35.1 requires the old " <>
             "room to be archived after tombstone. Recorded archive calls: #{inspect(archived_rooms)}"
    end

    test "old room GenServer is stopped after upgrade (no new writes possible)" do
      upgrader_id = "@kai:test.local"
      {old_room_id, _} = setup_old_room(upgrader_id)

      Application.put_env(:event_dispatcher, :admin_db_module, SpyAdminDB)

      # Monitor the old room PID *before* calling upgrade so we get the :DOWN signal.
      {:ok, old_pid} = Nebu.Room.RoomSupervisor.lookup_room(old_room_id)
      ref = Process.monitor(old_pid)

      request = %Core.UpgradeRoomRequest{
        old_room_id:  old_room_id,
        requester_id: upgrader_id,
        new_version:  "10"
      }

      response = Server.upgrade_room(request, build_stream(upgrader_id))
      new_room_id = response.new_room_id

      on_exit(fn ->
        case Nebu.Room.RoomSupervisor.lookup_room(new_room_id) do
          {:ok, pid} ->
            if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
          _ -> :ok
        end
      end)

      assert_receive {:DOWN, ^ref, :process, ^old_pid, _reason}, 500,
        "expected old room GenServer for #{old_room_id} to terminate within 500ms after upgrade " <>
        "(Horde.DynamicSupervisor.terminate_child must be called per Matrix spec §11.35.1)"
    end
  end
end
