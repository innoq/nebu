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
      http_request_headers: %{},
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

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :room_registry_module)
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
