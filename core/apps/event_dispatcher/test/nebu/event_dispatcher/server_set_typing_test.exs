defmodule Nebu.EventDispatcher.ServerSetTypingTest do
  use ExUnit.Case, async: false

  # ─── Story 4-17: Elixir set_typing/2 gRPC handler ────────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-17 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS tables are all process-global resources.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.set_typing/2 directly
  #     (synchronous unary handler).
  #   - Fake stream: minimal map with http_request_headers that includes
  #     x-user-id and x-system-role metadata (matches Nebu.Grpc.Metadata.trusted_identity/1
  #     contract used by the implementation).
  #   - DB injection (Room): Room.Server.init/1 uses
  #     Application.get_env(:room_manager, :db_module). Inject FakeRoomDB.
  #   - Rooms are started via Nebu.Room.RoomSupervisor.start_room/1 and tracked
  #     with on_exit cleanup (Horde.DynamicSupervisor.terminate_child/2).
  #   - Uses real Room.Server so :pg broadcast works for typing events.
  #   - The non-member test: starts a room but does NOT add @bob to members.
  #   - The room-not-found test: no Room GenServer running for the given room_id.

  alias Nebu.EventDispatcher.Server

  # ─── FakeRoomDB ──────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeRoomDB).
  # Mirrors the pattern from server_receipts_test.exs exactly.

  defmodule FakeRoomDB do
    def load_members(room_id) do
      case :ets.lookup(:set_typing_test_room_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:set_typing_test_room_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:set_typing_test_room_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:set_typing_test_room_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:set_typing_test_room_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:set_typing_test_room_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:set_typing_test_room_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:set_typing_test_room_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create ETS table for FakeRoomDB.
    # Guard against stale tables from --watch reruns.
    if :ets.info(:set_typing_test_room_db) != :undefined do
      :ets.delete(:set_typing_test_room_db)
    end

    :ets.new(:set_typing_test_room_db, [:named_table, :public, :set])

    # Inject fake DB module.
    Application.put_env(:room_manager, :db_module, FakeRoomDB)

    # Override server_name for deterministic assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # Clear :NebuTxnDedup between tests.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :server_name)

      if :ets.info(:set_typing_test_room_db) != :undefined do
        :ets.delete(:set_typing_test_room_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ────────────────────────────────────────────────────────
  #
  # Builds a minimal fake stream that satisfies Nebu.Grpc.Metadata.trusted_identity/1.
  # The set_typing/2 handler uses trusted_identity/1 to extract user_id from
  # stream metadata (x-user-id header).

  defp build_stream(user_id, system_role \\ "user") do
    %{
      http_request_headers: %{
        "x-user-id" => user_id,
        "x-system-role" => system_role
      }
    }
  end

  # ─── Room GenServer tracking ──────────────────────────────────────────────────

  defp start_and_track_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, _pid} ->
        on_exit(fn ->
          if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
            case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
              {:ok, pid} ->
                Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)

              _ ->
                :ok
            end
          end
        end)

        :ok

      error ->
        error
    end
  end

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ok
  end

  # ─── Happy path: set_typing for a room member ────────────────────────────────
  #
  # Given: Room GenServer running for "!typingtest:test.local" with @alice as member
  # When: Server.set_typing/2 called with room_id, user_id "@alice:test.local",
  #       typing: true, timeout_ms: 5000
  # Then: returns %Core.SetTypingResponse{}; uses real Room.Server so :pg broadcast works

  describe "Server.set_typing/2 — happy path" do
    test "valid room member typing=true returns SetTypingResponse" do
      room_id = "!typingtest:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)

      request = %Core.SetTypingRequest{
        room_id: room_id,
        user_id: alice,
        typing: true,
        timeout_ms: 5000
      }

      response = Server.set_typing(request, build_stream(alice))

      assert %Core.SetTypingResponse{} = response,
             "expected %Core.SetTypingResponse{}, got: #{inspect(response)}"
    end
  end

  # ─── Non-member → GRPC.RPCError PERMISSION_DENIED ────────────────────────────
  #
  # Given: Room GenServer running for "!typingperm:test.local";
  #        @bob:test.local is NOT a member
  # When: Server.set_typing/2 called for @bob
  # Then: raises %GRPC.RPCError{status: GRPC.Status.permission_denied()}

  describe "Server.set_typing/2 — not a room member" do
    test "raises GRPC.RPCError permission_denied when user is not a member" do
      room_id = "!typingperm:test.local"
      alice = "@alice:test.local"
      bob = "@bob:test.local"

      # Start the room and join only alice — bob is deliberately excluded.
      :ok = setup_room_with_member(room_id, alice)

      request = %Core.SetTypingRequest{
        room_id: room_id,
        user_id: bob,
        typing: true,
        timeout_ms: 5000
      }

      error =
        try do
          Server.set_typing(request, build_stream(bob))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected status permission_denied (#{GRPC.Status.permission_denied()}), got: #{error.status}"
    end
  end

  # ─── Room not found → GRPC.RPCError NOT_FOUND ────────────────────────────────
  #
  # Given: no Room GenServer running for "!ghosttyping:test.local"
  # When: Server.set_typing/2 called
  # Then: raises %GRPC.RPCError{status: GRPC.Status.not_found()}

  describe "Server.set_typing/2 — room not found" do
    test "raises GRPC.RPCError not_found when room does not exist" do
      request = %Core.SetTypingRequest{
        room_id: "!ghosttyping:test.local",
        user_id: "@alice:test.local",
        typing: true,
        timeout_ms: 5000
      }

      error =
        try do
          Server.set_typing(request, build_stream("@alice:test.local"))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.not_found(),
             "expected status not_found (#{GRPC.Status.not_found()}), got: #{error.status}"
    end
  end

  # ─── KNOWN LIMITATION: fire-and-forget timer regression note ─────────────────
  #
  # MINOR-4 — Documents a known limitation of the fire-and-forget timer approach.
  # Skipped because this is a Phase 2 fix, not blocking MVP.

  @tag :skip
  test "KNOWN_LIMITATION: re-set typing with longer timeout still gets cancelled by first timer" do
    # Fire-and-forget timer: first set_typing/4 schedules a :typing_expire.
    # Second set_typing/4 (with longer timeout) does NOT cancel the first timer.
    # When the first timer fires, it incorrectly clears the typing state even during the second window.
    # Fix: store timer refs and cancel before re-scheduling (Phase 2).
    flunk("This is a known limitation - implement timer cancellation to fix")
  end
end
