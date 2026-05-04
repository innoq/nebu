defmodule Nebu.EventDispatcher.AuditArchiveOpsTest do
  use ExUnit.Case, async: false

  # ─── Story 9-3: Audit log coverage for archive_room/2 and unarchive_room/2 ───
  #
  # These tests verify that:
  #   - archive_room/2 calls Compliance.AuditWriter.log/6 with action="room_archived"
  #   - unarchive_room/2 calls Compliance.AuditWriter.log/6 with action="room_unarchived"
  #
  # MAJOR finding from test-review: AC3-archive and AC3-unarchive had zero audit test coverage.
  #
  # Test strategy:
  #   - FakeAuditWriter: Process.send-based spy (same pattern as audit_room_ops_test.exs)
  #     Injected via Application.put_env(:compliance, :audit_writer, FakeAuditWriter)
  #   - FakeDB: ETS-backed, satisfies Nebu.Room.DBBehaviour (same pattern as archive_room_test.exs)
  #     Returns {:ok, "active"} for get_room_status so rooms start normally
  #   - FakeAdminDB: provides archive_room_atomic/1 no-op (same as archive_room_test.exs)
  #   - Stream: built with x-user-id header so actor_id is non-nil and assertable
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and ETS tables are all process-global resources.

  alias Nebu.EventDispatcher.Server

  # ─── FakeAuditWriter ─────────────────────────────────────────────────────────
  #
  # Spy that records all log/6 and log/7 calls.
  # Each call sends {:audit_log, actor, action, target_type, target_id, metadata, outcome}
  # to the registered test process (stored in Application config).
  # Pattern identical to audit_room_ops_test.exs FakeAuditWriter.

  defmodule FakeAuditWriter do
    def log(actor, action, target_type, target_id, metadata, outcome, error_detail \\ nil) do
      test_pid = Application.get_env(:compliance, :__audit_test_pid__)
      if test_pid && Process.alive?(test_pid) do
        send(test_pid, {:audit_log, actor, action, target_type, target_id, metadata, outcome, error_detail})
      end
      :ok
    end
  end

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Room.DBBehaviour.
  # Copied from archive_room_test.exs — returns {:ok, "active"} for get_room_status/1
  # so Room GenServers start normally (required by unarchive_room/2 via start_room/1).

  defmodule FakeDB do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id) do
      case :ets.lookup(:audit_archive_ops_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:audit_archive_ops_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:audit_archive_ops_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:audit_archive_ops_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:audit_archive_ops_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:audit_archive_ops_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:audit_archive_ops_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:audit_archive_ops_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    def load_room_settings(_room_id), do: {:ok, 0}
    def get_room_status(_room_id), do: {:ok, "active"}
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _direction, _limit, _from_token), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
  end

  # ─── FakeAdminDB ─────────────────────────────────────────────────────────────
  #
  # Minimal Nebu.Admin.DB fake.
  # archive_room_atomic/1 is called by archive_room/2 before the audit log.
  # unarchive_room/2 does NOT call admin_db — it only calls start_room/1.

  defmodule FakeAdminDB do
    def archive_room_atomic(_room_id), do: :ok
    def list_users(_limit, _cursor, _search), do: {[], ""}
    def get_user(_user_id), do: {:error, :not_found}
    def set_is_active(_user_id, _is_active), do: :ok
    def set_system_role(_user_id, _role), do: :ok
    def list_rooms(_limit, _cursor, _status_filter, _search), do: {[], ""}
    def get_room(_room_id), do: {:error, :not_found}
    def get_server_config, do: {:ok, %{}}
    def upsert_server_config(_changes), do: :ok
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:audit_archive_ops_test_db) != :undefined do
      :ets.delete(:audit_archive_ops_test_db)
    end
    :ets.new(:audit_archive_ops_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, FakeDB)
    Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)
    Application.put_env(:event_dispatcher, :server_name, "test.local")
    Application.put_env(:compliance, :audit_writer, FakeAuditWriter)
    Application.put_env(:compliance, :__audit_test_pid__, self())

    case :pg.start_link() do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :admin_db_module)
      Application.delete_env(:event_dispatcher, :server_name)
      Application.delete_env(:compliance, :audit_writer)
      Application.delete_env(:compliance, :__audit_test_pid__)

      if :ets.info(:audit_archive_ops_test_db) != :undefined do
        :ets.delete(:audit_archive_ops_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Helpers ─────────────────────────────────────────────────────────────────

  # Build a stream with x-user-id header so actor_id is assertable (non-nil).
  defp build_stream(actor_id) do
    %{http_request_headers: %{"x-user-id" => actor_id}}
  end

  defp stop_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, pid} ->
        if Process.alive?(pid) do
          Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
        end
      _ -> :ok
    end
  end

  # ─── AC: archive_room/2 emits audit log ──────────────────────────────────────
  #
  # Given: FakeAuditWriter instrumented; a Room GenServer is running for room_id
  # When:  Server.archive_room/2 is called with actor_id in stream metadata
  # Then:  FakeAuditWriter.log/6 is called with:
  #        - actor = actor_id from x-user-id header
  #        - action = "room_archived"
  #        - target_type = "room"
  #        - target_id = room_id
  #        - metadata = %{}
  #        - outcome = "success"

  describe "Server.archive_room/2 — audit log emission" do
    test "archive_room emits audit log entry with action=room_archived and correct actor_id" do
      actor_id = "@admin:test.local"
      room_id = "!audit-archive:test.local"

      # Start a room GenServer so the archive call has something to terminate
      {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      on_exit(fn -> stop_room(room_id) end)
      assert is_pid(pid)

      request = %Core.ArchiveRoomRequest{room_id: room_id}
      response = Server.archive_room(request, build_stream(actor_id))

      assert %Core.ArchiveRoomResponse{ok: true} = response,
             "expected ArchiveRoomResponse{ok: true}, got #{inspect(response)}"

      assert_receive {:audit_log, ^actor_id, "room_archived", "room", ^room_id, %{}, "success", nil},
                     500,
                     "expected AuditWriter.log/6 called with action='room_archived' after archive_room"
    end

    test "archive_room emits audit log even when room GenServer is not running (idempotent path)" do
      actor_id = "@admin:test.local"
      room_id = "!audit-archive-no-genserver:test.local"

      # Confirm no GenServer is running
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      request = %Core.ArchiveRoomRequest{room_id: room_id}
      response = Server.archive_room(request, build_stream(actor_id))

      assert %Core.ArchiveRoomResponse{ok: true} = response,
             "expected ArchiveRoomResponse{ok: true} even when GenServer not running"

      assert_receive {:audit_log, ^actor_id, "room_archived", "room", ^room_id, %{}, "success", nil},
                     500,
                     "expected audit log even for idempotent archive_room (GenServer already stopped)"
    end
  end

  # ─── AC: unarchive_room/2 emits audit log ────────────────────────────────────
  #
  # Given: FakeAuditWriter instrumented; no Room GenServer running (room was archived)
  # When:  Server.unarchive_room/2 is called with actor_id in stream metadata
  # Then:  FakeAuditWriter.log/6 is called with:
  #        - actor = actor_id from x-user-id header
  #        - action = "room_unarchived"
  #        - target_type = "room"
  #        - target_id = room_id
  #        - metadata = %{}
  #        - outcome = "success"

  describe "Server.unarchive_room/2 — audit log emission" do
    test "unarchive_room emits audit log entry with action=room_unarchived and correct actor_id" do
      actor_id = "@admin:test.local"
      room_id = "!audit-unarchive:test.local"

      # Confirm no GenServer running (simulates post-archive state)
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      request = %Core.UnarchiveRoomRequest{room_id: room_id}
      response = Server.unarchive_room(request, build_stream(actor_id))

      assert %Core.UnarchiveRoomResponse{ok: true} = response,
             "expected UnarchiveRoomResponse{ok: true}, got #{inspect(response)}"

      # Cleanup the GenServer started by unarchive_room
      on_exit(fn -> stop_room(room_id) end)

      assert_receive {:audit_log, ^actor_id, "room_unarchived", "room", ^room_id, %{}, "success", nil},
                     500,
                     "expected AuditWriter.log/6 called with action='room_unarchived' after unarchive_room"
    end
  end
end
