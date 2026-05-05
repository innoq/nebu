defmodule Nebu.EventDispatcher.GrpcHandlerTest do
  use ExUnit.Case, async: false

  # ─── Story 4-8: gRPC EventBus Server-Streaming + GetRoomState Unary ────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-8 is implemented.
  #
  # async: false — Horde.Registry, :pg groups, and Application env are process-global.
  #
  # Test strategy:
  #   - event_bus/2 is tested by spawning the handler in a separate process,
  #     then sending {:new_event, event_map} to it and observing GRPC.Server.send_reply/2
  #     via a fake stream that echoes sent events back to the test process.
  #   - :pg cleanup is tested by killing the handler process and checking group membership.
  #   - get_room_state/2 is a synchronous unary call; tested directly on the Server module.
  #
  # Fake GRPC stream:
  #   We cannot instantiate a real GRPC.Server.Stream at unit-test level without a live
  #   gRPC server. Instead, we use a minimal map that satisfies the stream contract used
  #   by the real implementation and intercepts GRPC.Server.send_reply/2 calls.
  #   The :grpc_reply_interceptor key holds the test PID; the implementation is expected
  #   to call GRPC.Server.send_reply(stream, event) which internally sends
  #   {:grpc_reply, event} back to the interceptor PID.
  #
  #   See the FakeStream helper below for the mock boundary.

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB (room_manager injection) ────────────────────────────────────────
  # event_dispatcher does not currently depend on room_manager, but Story 4-8
  # adds that dependency so get_room_state/2 can call Nebu.Room.Server.get_state/1.
  # FakeRoomRegistry below simulates the Room.Server response.

  defmodule FakeRoomRegistry do
    @moduledoc """
    Replaces the room_manager lookup used by get_room_state/2.
    Inject via Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry).
    """

    def get_state("!room1:nebu.local") do
      %{
        room_id: "!room1:nebu.local",
        members: MapSet.new(["@kai:nebu.local"]),
        power_levels: %{},
        created_at: ~U[2025-01-01 00:00:00Z]
      }
    end

    def get_state(room_id) do
      # Simulate GenServer.call to an unregistered process — exits with {:noproc, _}
      # just like the real Nebu.Room.Server.get_state/1 would.
      exit({:noproc, {GenServer, :call, [{:via, Horde.Registry, {Nebu.Room.Registry, room_id}}, :get_state, 5000]}})
    end
  end

  # ─── FakeMessagesDB ──────────────────────────────────────────────────────────
  # Prevents Ecto dependency in get_room_state tests.
  # build_state_events/2 calls messages_db_module().get_room_name/1 — this stub
  # returns :not_found so no m.room.name state event is emitted (Story 7-33 fix).

  defmodule FakeMessagesDB do
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}
    # Story 9-7: returns empty list (no generic state events in unit tests).
    def get_generic_state_events(_room_id), do: {:ok, []}
    # MAJOR-2 fix: no persisted create event in these unit tests; synthesized fallback used.
    def get_room_create_event(_room_id), do: {:error, :not_found}
  end

  # ─── FakeStream ─────────────────────────────────────────────────────────────
  # A minimal fake that the implementation can call GRPC.Server.send_reply/2 on.
  # Story 4-8's implementation is expected to check for the :grpc_reply_interceptor
  # field when in test mode, or the test can mock GRPC.Server.send_reply at the module
  # level via Mox / process dictionary interception.
  #
  # Since we cannot link against Mox without adding it as a dep, we use a simpler
  # approach: the test process registers itself under a known name, and the fake stream
  # carries the test PID so the implementation can route replies.

  defp build_fake_stream(test_pid) do
    %{
      # x-user-id matches the member in FakeRoomRegistry so the Story 7-19 membership
      # check passes. x-system-role is omitted — trusted_identity/1 defaults to "user".
      http_request_headers: %{"x-user-id" => "@kai:nebu.local"},
      # Implementation reads this to forward GRPC.Server.send_reply/2 outputs in tests
      grpc_reply_interceptor: test_pid
    }
  end

  # ─── Setup ──────────────────────────────────────────────────────────────────

  setup do
    # Ensure :pg is started (it is an OTP built-in, normally started by BEAM)
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # Inject fake room registry for get_room_state tests
    Application.put_env(:event_dispatcher, :room_registry_module, FakeRoomRegistry)

    # Inject fake messages DB so build_state_events/2 does not hit Ecto (Story 7-33)
    Application.put_env(:event_dispatcher, :messages_db_module, FakeMessagesDB)

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :room_registry_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
    end)

    :ok
  end

  # ─── AC #2 — event_bus/2: sends Core.Event on stream for {:new_event, event_map} ──

  describe "Server.event_bus/2 — streaming EventBus handler" do
    test "sends %Core.Event{} on stream when {:new_event, event_map} is received from :pg" do
      test_pid = self()
      fake_stream = build_fake_stream(test_pid)

      request = %Core.EventBusRequest{node_id: "gw-node-1"}

      event_map = %{
        "room_id" => "!abc:nebu.local",
        "event_id" => "$ev1",
        "type" => "m.room.message",
        "sender" => "@kai:nebu.local",
        "content" => %{},
        "origin_server_ts" => 1_700_000_000_000
      }

      # Start the event_bus handler in a separate process — it will block (stream loop)
      handler_pid =
        spawn_link(fn ->
          Server.event_bus(request, fake_stream)
        end)

      # Wait deterministically until the handler has joined the :pg group
      wait_for_pg_member("event_bus:gateways", handler_pid)

      # Simulate a :pg broadcast — send the message directly to the handler process
      # (in production, :pg.broadcast delivers to all members of the group)
      send(handler_pid, {:new_event, event_map})

      # Expect the handler to have called GRPC.Server.send_reply/2 which routes back
      # to us via the fake stream interceptor
      assert_receive {:grpc_reply, %Core.Event{} = event}, 500

      assert event.event_id == "$ev1"
      assert event.room_id == "!abc:nebu.local"
      assert event.sender_id == "@kai:nebu.local"
      assert event.event_type == "m.room.message"
      assert event.origin_ts == 1_700_000_000_000
    end

    test "handler starts and joins :pg group without crash" do
      # This test verifies the handler does not crash and enters the receive loop.
      # It passes if the handler starts and joins the :pg group without raising.
      test_pid = self()
      fake_stream = build_fake_stream(test_pid)
      request = %Core.EventBusRequest{node_id: "gw-node-logging-test"}

      handler_pid =
        spawn(fn ->
          Server.event_bus(request, fake_stream)
        end)

      # Wait deterministically until the handler has joined the :pg group
      # (confirms process started AND entered the receive loop)
      wait_for_pg_member("event_bus:gateways", handler_pid)

      # Handler must be alive (didn't crash on startup)
      assert Process.alive?(handler_pid)

      # Cleanup
      Process.exit(handler_pid, :kill)
    end
  end

  # ─── AC #2 — :pg group cleanup on stream handler process exit ───────────────

  describe "Server.event_bus/2 — :pg cleanup on process exit" do
    test "pg group membership is removed after handler process is killed" do
      test_pid = self()
      fake_stream = build_fake_stream(test_pid)
      request = %Core.EventBusRequest{node_id: "gw-node-cleanup"}

      # The handler joins :pg groups for all active rooms on startup.
      # We need at least one known room group for this test.
      # Start the handler in a monitored process.
      handler_pid =
        spawn(fn ->
          Server.event_bus(request, fake_stream)
        end)

      ref = Process.monitor(handler_pid)

      # Wait deterministically until the handler has joined the :pg group
      wait_for_pg_member("event_bus:gateways", handler_pid)

      # The event_bus handler must have joined at least the process-level :pg group
      # for the gateway node. We test cleanup of the general EventBus subscription.
      # After shutdown, no dead PID should remain in any :pg group for "room:*".
      all_members_before_kill = :pg.get_members("event_bus:gateways") ++ []

      # Send :shutdown (trappable) so the {:EXIT, _, _} clause in event_bus_loop/1
      # executes and leave_all_room_groups/0 is called for clean :pg membership removal.
      # :kill is untrappable and would bypass Process.flag(:trap_exit, true) entirely.
      Process.exit(handler_pid, :shutdown)

      # Wait for process DOWN signal
      receive do
        {:DOWN, ^ref, :process, ^handler_pid, _reason} -> :ok
      after
        1_000 -> flunk("handler process did not go DOWN in time")
      end

      # Wait deterministically for the process to fully exit before asserting
      wait_for_exit(handler_pid)

      # Dead PID must NOT remain in any :pg group
      members_after_kill = :pg.get_members("event_bus:gateways")

      refute Enum.member?(members_after_kill, handler_pid),
             "dead PID #{inspect(handler_pid)} still present in :pg group after kill — cleanup failed"

      # Also verify no dead PIDs remain in any room:* group
      # (implementation must trap exits and clean up memberships)
      all_room_groups =
        :pg.which_groups()
        |> Enum.filter(fn g -> is_binary(g) and String.starts_with?(g, "room:") end)

      for group <- all_room_groups do
        members = :pg.get_members(group)

        refute Enum.member?(members, handler_pid),
               "dead PID still present in :pg group #{group} after kill"
      end

      _ = all_members_before_kill
      :ok
    end
  end

  # ─── AC #3 — get_room_state/2: happy path ────────────────────────────────────

  describe "Server.get_room_state/2 — unary handler" do
    test "returns GetRoomStateResponse with members list for an existing room" do
      # Given: FakeRoomRegistry knows !room1:nebu.local with @kai as member
      request = %Core.GetRoomStateRequest{room_id: "!room1:nebu.local"}
      stream = build_fake_stream(self())

      response = Server.get_room_state(request, stream)

      assert %Core.GetRoomStateResponse{} = response
      assert "@kai:nebu.local" in response.members
      assert response.power_levels_json == "{}"
      assert response.room_name == ""
    end

    test "returns exactly the members from the Room GenServer state" do
      request = %Core.GetRoomStateRequest{room_id: "!room1:nebu.local"}
      stream = build_fake_stream(self())

      response = Server.get_room_state(request, stream)

      assert length(response.members) == 1
      assert hd(response.members) == "@kai:nebu.local"
    end
  end

  # ─── AC #3 — get_room_state/2: NOT_FOUND for non-existent room ───────────────

  describe "Server.get_room_state/2 — NOT_FOUND error" do
    test "raises GRPC.RPCError with NOT_FOUND status when room does not exist" do
      # Given: FakeRoomRegistry raises for !ghost:nebu.local
      request = %Core.GetRoomStateRequest{room_id: "!ghost:nebu.local"}
      stream = build_fake_stream(self())

      assert_raise GRPC.RPCError, fn ->
        Server.get_room_state(request, stream)
      end
    end

    test "raised GRPC.RPCError has NOT_FOUND status code" do
      request = %Core.GetRoomStateRequest{room_id: "!ghost:nebu.local"}
      stream = build_fake_stream(self())

      error =
        try do
          Server.get_room_state(request, stream)
          nil
        rescue
          e in GRPC.RPCError -> e
        end

      refute is_nil(error), "expected GRPC.RPCError to be raised but got nil"
      assert error.status == GRPC.Status.not_found()
    end
  end

  # ─── Story 9-7: state_key propagation in send_event gRPC handler ────────────
  #
  # AC: When Server.send_event/2 receives a SendEventRequest with a non-empty
  # state_key, it must pass state_key through to Nebu.Room.Server.send_event/6
  # and the resulting persisted event must carry that state_key.
  #
  # Strategy: call Server.send_event/2 with a real Room GenServer running
  # (via the room_manager Application already started), inject FakeDB via
  # Application.put_env so no PostgreSQL is needed, then assert the stored
  # event in the FakeDB ETS table has the correct state_key value.
  #
  # Note: this describe block depends on the room_manager Application being
  # started as part of the umbrella test run. Room GenServer and :NebuTxnDedup
  # ETS table are available.

  describe "Server.send_event/2 — state_key propagation (Story 9-7)" do
    # Module-level FakeDB for this describe block (same pattern as send_event_test.exs).
    defmodule FakeRoomDB do
      def load_members(room_id) do
        case :ets.lookup(:grpc_handler_state_key_db, {:room, room_id}) do
          [] ->
            {:error, :not_found}

          [{_, created_at_ms}] ->
            members = :ets.match(:grpc_handler_state_key_db, {{:member, room_id, :"$1"}, :active})
            # SEC Gate 1 fix: load power levels from ETS if set, otherwise return empty "{}".
            pl_json =
              case :ets.lookup(:grpc_handler_state_key_db, {:power_levels, room_id}) do
                [{_, json}] -> json
                [] -> "{}"
              end
            {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
        end
      end

      def insert_room(room_id) do
        now_ms = System.system_time(:millisecond)
        :ets.insert(:grpc_handler_state_key_db, {{:room, room_id}, now_ms})
        {:ok, now_ms}
      end

      def insert_member(room_id, user_id) do
        :ets.insert(:grpc_handler_state_key_db, {{:member, room_id, user_id}, :active})
        :ok
      end

      def delete_member(_room_id, _user_id), do: :ok

      def insert_event(event) do
        :ets.insert(:grpc_handler_state_key_db, {{:event, event["event_id"]}, event})
        :ok
      end

      def set_power_levels(room_id, json) do
        :ets.insert(:grpc_handler_state_key_db, {{:power_levels, room_id}, json})
        :ok
      end
      def load_room_settings(_room_id), do: {:ok, 0}
      def get_room_status(_room_id), do: {:ok, "active"}
      # Story 9-9: TOCTOU fix — returns {:ok, "active"} for normal rooms.
      def check_room_status_for_update(_room_id), do: {:ok, "active"}
      # Remaining callbacks required by @behaviour Nebu.Room.DBBehaviour:
      def get_rooms_for_user(_user_id), do: {:ok, []}
      def fetch_events(_room_id, _dir, _limit, _from), do: {:ok, [], "", ""}
      def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
      def get_event_timestamp(_event_id), do: {:error, :not_found}
      def get_room_name(_room_id), do: {:error, :not_found}
      def get_room_creator(_room_id), do: {:error, :not_found}
      def get_generic_state_events(_room_id), do: {:ok, []}
      def get_room_create_event(_room_id), do: {:error, :not_found}
    end

    setup do
      # Create a fresh ETS table for this test group.
      if :ets.info(:grpc_handler_state_key_db) != :undefined do
        :ets.delete(:grpc_handler_state_key_db)
      end

      :ets.new(:grpc_handler_state_key_db, [:named_table, :public, :set])

      Application.put_env(:room_manager, :db_module, FakeRoomDB)
      Application.put_env(:event_dispatcher, :server_name, "test.local")

      # Override room_registry_module to use the REAL Nebu.Room.Server.get_state/1
      # so the membership check in Server.send_event/2 uses the actual running GenServer
      # (not the FakeRoomRegistry defined in the outer setup which only knows !room1:nebu.local).
      Application.put_env(:event_dispatcher, :room_registry_module, Nebu.Room.Server)

      case :pg.start_link() do
        {:ok, _pid} -> :ok
        {:error, {:already_started, _}} -> :ok
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end

      on_exit(fn ->
        Application.delete_env(:room_manager, :db_module)
        Application.delete_env(:event_dispatcher, :server_name)
        Application.delete_env(:event_dispatcher, :room_registry_module)

        if :ets.info(:grpc_handler_state_key_db) != :undefined do
          :ets.delete(:grpc_handler_state_key_db)
        end

        if :ets.whereis(:NebuTxnDedup) != :undefined do
          :ets.delete_all_objects(:NebuTxnDedup)
        end
      end)

      :ok
    end

    test "SendEventRequest with state_key is passed through to Room.Server and stored in event" do
      # Given: a Room GenServer running with the sender as a member AND admin power level.
      # SEC Gate 1 fix: state events require :change_state power (state_default=50).
      # The sender must have power >= 50 to change state events.
      room_id = "!state-key-test:test.local"
      sender_id = "@kai:test.local"

      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

      on_exit(fn ->
        if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
          case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
            {:ok, pid} -> Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
            _ -> :ok
          end
        end
      end)

      :ok = Nebu.Room.Server.join(room_id, sender_id)

      # Grant sender admin power (100 >= state_default 50) so the :change_state check passes.
      admin_power_levels = Nebu.Room.PowerLevels.default_levels()
        |> Map.put("users", %{sender_id => 100})
      :ok = Nebu.Room.Server.set_power_levels(room_id, sender_id, admin_power_levels)

      content_bytes = Jason.encode!(%{"name" => "State Key Room"})

      # When: Server.send_event/2 is called with state_key = "custom-key" and is_state_event=true.
      # SEC Gate 1: is_state_event=true signals the event dispatcher to pass state_key as-is
      # (not nil), which triggers the :change_state power check in Room.Server.
      request = %Core.SendEventRequest{
        room_id: room_id,
        sender_id: sender_id,
        event_type: "m.room.name",
        state_key: "custom-key",
        is_state_event: true,
        txn_id: "txn-state-key-grpc-1",
        content: content_bytes,
        origin_ts: System.system_time(:millisecond)
      }

      stream = build_fake_stream(self())
      response = Server.send_event(request, stream)

      assert %Core.SendEventResponse{} = response
      assert String.starts_with?(response.event_id, "$"),
             "expected event_id to start with '$', got: #{response.event_id}"

      # Then: the persisted event must contain state_key = "custom-key"
      [{_, stored_event}] = :ets.lookup(:grpc_handler_state_key_db, {:event, response.event_id})
      assert stored_event["state_key"] == "custom-key",
             "expected state_key \"custom-key\" in stored event, got: #{inspect(stored_event["state_key"])}"
      assert stored_event["type"] == "m.room.name",
             "expected event type \"m.room.name\", got: #{inspect(stored_event["type"])}"
    end

    test "SendEventRequest with empty state_key stores empty string in event" do
      # Given: a Room GenServer running with the sender as a member AND admin power level.
      # SEC Gate 1 fix: even state events with state_key="" (like m.room.name) require
      # :change_state power. The sender must be an admin to change state events.
      room_id = "!state-key-empty:test.local"
      sender_id = "@kai:test.local"

      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

      on_exit(fn ->
        if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
          case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
            {:ok, pid} -> Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
            _ -> :ok
          end
        end
      end)

      :ok = Nebu.Room.Server.join(room_id, sender_id)

      # Grant sender admin power (100 >= state_default 50).
      admin_power_levels = Nebu.Room.PowerLevels.default_levels()
        |> Map.put("users", %{sender_id => 100})
      :ok = Nebu.Room.Server.set_power_levels(room_id, sender_id, admin_power_levels)

      content_bytes = Jason.encode!(%{"name" => "Default State Key"})

      # When: Server.send_event/2 is called with state_key = "" (m.room.name uses empty state_key)
      # is_state_event=true signals this is a state event even though state_key="".
      request = %Core.SendEventRequest{
        room_id: room_id,
        sender_id: sender_id,
        event_type: "m.room.name",
        state_key: "",
        is_state_event: true,
        txn_id: "txn-state-key-grpc-2",
        content: content_bytes,
        origin_ts: System.system_time(:millisecond)
      }

      stream = build_fake_stream(self())
      response = Server.send_event(request, stream)

      assert %Core.SendEventResponse{} = response

      # Then: the persisted event must contain state_key = ""
      [{_, stored_event}] = :ets.lookup(:grpc_handler_state_key_db, {:event, response.event_id})
      assert stored_event["state_key"] == "",
             "expected state_key \"\" (empty) in stored event, got: #{inspect(stored_event["state_key"])}"
    end
  end

  # ─── Deterministic synchronization helpers ───────────────────────────────────

  # Poll until `pid` appears in the :pg group, or flunk after timeout_ms.
  # Use instead of Process.sleep when waiting for a handler to join a :pg group.
  defp wait_for_pg_member(group, pid, timeout_ms \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    do_wait_for_pg_member(group, pid, deadline)
  end

  defp do_wait_for_pg_member(group, pid, deadline) do
    if pid in :pg.get_members(group) do
      :ok
    else
      if System.monotonic_time(:millisecond) >= deadline do
        flunk("PID #{inspect(pid)} never joined group #{group} within timeout")
      else
        Process.sleep(5)
        do_wait_for_pg_member(group, pid, deadline)
      end
    end
  end

  # Poll until `pid` is no longer alive, or flunk after timeout_ms.
  # Use after Process.exit to ensure cleanup assertions run after the process is gone.
  defp wait_for_exit(pid, timeout_ms \\ 500) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    do_wait_for_exit(pid, deadline)
  end

  defp do_wait_for_exit(pid, deadline) do
    if Process.alive?(pid) do
      if System.monotonic_time(:millisecond) >= deadline do
        flunk("Process #{inspect(pid)} did not exit within timeout")
      else
        Process.sleep(5)
        do_wait_for_exit(pid, deadline)
      end
    else
      :ok
    end
  end
end
