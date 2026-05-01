defmodule Nebu.EventDispatcher.AuditRoomOpsTest do
  use ExUnit.Case, async: false

  # ─── Story 5-2: Elixir audit integration for create_room/join_room ───────────
  #
  # ALL tests in this module are expected to FAIL until Story 5-2 is implemented.
  # Failing reason: Compliance.AuditWriter module does not exist yet.
  #
  # These tests verify that:
  #   - AC8 (create_room): Compliance.AuditWriter.log/6 is called after a
  #     successful create_room/2 call with action="room_created"
  #   - AC9 (join_room): Compliance.AuditWriter.log/6 is called after a
  #     successful join_room/2 call (ok-branch) with action="room_joined"
  #   - No audit log is emitted for the :already_member branch of join_room
  #
  # Test strategy:
  #   - FakeAuditWriter: Process.send-based spy that records all log/6 calls.
  #     Injected via Application.put_env(:compliance, :audit_writer, FakeAuditWriter).
  #     The implementation MUST read from this env key (or use a module attribute
  #     @audit_writer Application.compile_env(:compliance, :audit_writer, Compliance.AuditWriter))
  #     to allow test injection.
  #   - FakeDB (copied from create_room_test.exs): ETS-backed room database stub.
  #   - async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  #     and :NebuTxnDedup are all process-global resources.
  #   - Room GenServers created during tests are stopped on_exit.
  #
  # NOTE: The FakeAuditWriter injection mechanism requires the server.ex
  # implementation to call audit via a configurable module reference. The exact
  # injection strategy (Application.get_env vs. behaviour mock) is left to the
  # Dev agent — the test here uses Application.put_env as the agreed pattern.

  alias Nebu.EventDispatcher.Server

  # ─── FakeAuditWriter ─────────────────────────────────────────────────────────
  #
  # Spy that records all log/6 and log/7 calls.
  # Each call sends {:audit_log, actor, action, target_type, target_id, metadata, outcome, error_detail}
  # to the registered test process (stored in Application config).

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
  # ETS-backed fake satisfying the Nebu.Room.DB behaviour.
  # Identical to FakeDB in create_room_test.exs.

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:audit_room_ops_test_db, {:room, room_id}) do
        [] -> {:error, :not_found}
        [{_, created_at_ms}] ->
          members =
            :ets.match(:audit_room_ops_test_db, {{:member, room_id, :"$1"}, :active})
          pl_json =
            case :ets.lookup(:audit_room_ops_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end
          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:audit_room_ops_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:audit_room_ops_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:audit_room_ops_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:audit_room_ops_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:audit_room_ops_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── FakeInviteDB ────────────────────────────────────────────────────────────
  #
  # No-op stub for Nebu.Room.InviteDB — accept_invitation is called by join_room.

  defmodule FakeInviteDB do
    def accept_invitation(_room_id, _user_id), do: :ok
    def insert_invitation(_room_id, _inviter, _invitee), do: :ok
    def reject_invitation(_room_id, _user_id), do: :ok
    def get_pending_invite_rooms_for_user(_user_id), do: {:ok, []}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:audit_room_ops_test_db) != :undefined do
      :ets.delete(:audit_room_ops_test_db)
    end
    :ets.new(:audit_room_ops_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, FakeDB)
    Application.put_env(:event_dispatcher, :messages_db_module, FakeDB)
    Application.put_env(:event_dispatcher, :invite_db_module, FakeInviteDB)
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
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :invite_db_module)
      Application.delete_env(:event_dispatcher, :server_name)
      Application.delete_env(:compliance, :audit_writer)
      Application.delete_env(:compliance, :__audit_test_pid__)

      if :ets.info(:audit_room_ops_test_db) != :undefined do
        :ets.delete(:audit_room_ops_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  defp build_stream, do: %{http_request_headers: %{}}

  defp stop_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, pid} -> if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
      _ -> :ok
    end
  end

  # ─── AC8: create_room emits audit log ────────────────────────────────────────
  #
  # Given: FakeAuditWriter instrumented
  # When: Server.create_room/2 succeeds
  # Then: FakeAuditWriter.log/6 is called with:
  #       - actor_user_id = creator_id
  #       - action = "room_created"
  #       - target_type = "room"
  #       - target_id = the returned room_id
  #       - metadata = %{"is_direct" => false}
  #       - outcome = "success"

  describe "Server.create_room/2 — audit log emission" do
    test "create_room emits audit log entry with action=room_created" do
      creator_id = "@alice:test.local"

      request = %Core.CreateRoomRequest{
        creator_id: creator_id,
        name: "Audit Test Room",
        is_direct: false
      }

      response = Server.create_room(request, build_stream())
      room_id = response.room_id
      on_exit(fn -> stop_room(room_id) end)

      assert_receive {:audit_log, ^creator_id, "room_created", "room", ^room_id, _metadata, "success", nil},
                     500,
                     "expected AuditWriter.log/6 called with action='room_created' after create_room success"
    end

    test "create_room audit log includes is_direct in metadata" do
      creator_id = "@bob:test.local"

      request = %Core.CreateRoomRequest{
        creator_id: creator_id,
        name: "DM Room",
        is_direct: true
      }

      response = Server.create_room(request, build_stream())
      room_id = response.room_id
      on_exit(fn -> stop_room(room_id) end)

      assert_receive {:audit_log, ^creator_id, "room_created", "room", ^room_id, metadata, "success", nil},
                     500,
                     "expected audit log entry for create_room with is_direct=true"

      assert metadata["is_direct"] == true,
             "expected metadata.is_direct=true, got #{inspect(metadata)}"
    end
  end

  # ─── AC9: join_room emits audit log ──────────────────────────────────────────
  #
  # Given: A room exists; FakeAuditWriter instrumented
  # When: Server.join_room/2 succeeds (ok-branch, new member)
  # Then: FakeAuditWriter.log/6 is called with:
  #       - actor_user_id = user_id from request
  #       - action = "room_joined"
  #       - target_type = "room"
  #       - target_id = room_id
  #       - outcome = "success"

  describe "Server.join_room/2 — audit log emission" do
    test "join_room emits audit log entry with action=room_joined" do
      # First create the room
      create_req = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "Join Audit Room"
      }
      create_resp = Server.create_room(create_req, build_stream())
      room_id = create_resp.room_id
      on_exit(fn -> stop_room(room_id) end)

      # Clear any create_room audit messages from the mailbox
      receive do
        {:audit_log, _, "room_created", _, _, _, _, _} -> :ok
      after
        0 -> :ok
      end

      joiner_id = "@charlie:test.local"

      join_req = %Core.JoinRoomRequest{
        user_id: joiner_id,
        room_id_or_alias: room_id
      }

      _join_resp = Server.join_room(join_req, build_stream())

      assert_receive {:audit_log, ^joiner_id, "room_joined", "room", ^room_id, _metadata, "success", nil},
                     500,
                     "expected AuditWriter.log/6 called with action='room_joined' after successful join"
    end

    test "join_room does NOT emit audit log when user is already a member" do
      # Create room and join a user first
      create_req = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "Idempotent Join Room"
      }
      create_resp = Server.create_room(create_req, build_stream())
      room_id = create_resp.room_id
      on_exit(fn -> stop_room(room_id) end)

      joiner_id = "@dave:test.local"
      join_req = %Core.JoinRoomRequest{user_id: joiner_id, room_id_or_alias: room_id}

      # First join (should emit audit)
      _first = Server.join_room(join_req, build_stream())
      assert_receive {:audit_log, ^joiner_id, "room_joined", _, _, _, _, _}, 500

      # Second join (idempotent, already_member — must NOT emit audit)
      _second = Server.join_room(join_req, build_stream())
      refute_receive {:audit_log, ^joiner_id, "room_joined", _, _, _, _, _}, 200,
                     "join_room :already_member branch must not emit audit log (idempotent call)"
    end
  end
end
