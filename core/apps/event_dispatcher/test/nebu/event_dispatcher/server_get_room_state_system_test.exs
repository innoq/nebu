defmodule Nebu.EventDispatcher.ServerGetRoomStateSystemTest do
  use ExUnit.Case, async: false

  # ─── Story 7-33: Fix server-internal event fanout regression from Story 7-19 ──
  #
  # These tests are written FIRST (ATDD red-phase gate), before the fix is applied.
  #
  # Red-phase summary:
  #   - Test 1 (system-role bypass) FAILS before fix: current code checks
  #     MapSet.member?(state.members, nil) for every caller — the system caller has
  #     no x-user-id so caller_id is nil, the check always fails → permission_denied.
  #   - Test 2 (non-member regression guard) PASSES before and after fix: a regular
  #     user who is not a room member must always receive permission_denied (Story 7-19
  #     IDOR fix is preserved by the new bypass).
  #   - Test 3 (member happy path regression guard) PASSES before and after fix: a
  #     regular user who IS a room member gets the full GetRoomStateResponse.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS tables are all process-global resources.
  #
  # Test strategy:
  #   - Tests call Nebu.EventDispatcher.Server.get_room_state/2 directly
  #     (synchronous unary handler).
  #   - Fake stream: minimal map with http_request_headers.
  #     For system callers: only x-system-role = "system" (no x-user-id).
  #     For user callers: both x-user-id and x-system-role = "user".
  #     Matches Nebu.Grpc.Metadata.trusted_identity/1 contract.
  #   - FakeRoomDB: ETS-backed fake satisfying Nebu.Room.DB behaviour.
  #     Pattern follows server_moderation_metadata_test.exs exactly.
  #   - ETS table name: :get_room_state_system_test_db (unique — no collision).

  alias Nebu.EventDispatcher.Server

  # ─── FakeRoomDB ──────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeRoomDB).
  # Pattern follows server_moderation_metadata_test.exs exactly.

  defmodule FakeRoomDB do
    def load_members(room_id) do
      case :ets.lookup(:get_room_state_system_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:get_room_state_system_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:get_room_state_system_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:get_room_state_system_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:get_room_state_system_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:get_room_state_system_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:get_room_state_system_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:get_room_state_system_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Called by build_state_events/2 via messages_db_module().get_room_name/1.
    # Returns :not_found so no m.room.name state event is emitted (keeps test output clean).
    def get_room_name(_room_id), do: {:error, :not_found}

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create ETS table for FakeRoomDB.
    # Guard against stale tables from --watch reruns.
    if :ets.info(:get_room_state_system_test_db) != :undefined do
      :ets.delete(:get_room_state_system_test_db)
    end

    :ets.new(:get_room_state_system_test_db, [:named_table, :public, :set])

    # Inject fake DB module for Room GenServer init.
    Application.put_env(:room_manager, :db_module, FakeRoomDB)

    # Inject fake messages DB to avoid PostgreSQL for build_state_events/2 calls.
    Application.put_env(:event_dispatcher, :messages_db_module, FakeRoomDB)

    # Override server_name for deterministic event IDs.
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
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :server_name)

      if :ets.info(:get_room_state_system_test_db) != :undefined do
        :ets.delete(:get_room_state_system_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream helpers ─────────────────────────────────────────────────
  #
  # build_stream/2: regular user caller (x-user-id set, system_role explicit).
  # build_system_stream/0: internal system caller — no x-user-id, x-system-role="system".
  #   Matches the EventBus fanout goroutine context after the Story 7-33 Go fix.
  #   Nebu.Grpc.Metadata.trusted_identity/1 will return {nil, "system"} for this stream.

  defp build_stream(user_id, system_role) do
    %{
      http_request_headers: %{
        "x-user-id" => user_id,
        "x-system-role" => system_role
      }
    }
  end

  defp build_system_stream do
    # System caller: no x-user-id present, x-system-role = "system".
    # trusted_identity/1 returns {nil, "system"} for this map.
    %{http_request_headers: %{"x-system-role" => "system"}}
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

  # ─── AC #4: system-role bypass — no membership check ─────────────────────────
  #
  # RED phase: this test FAILS before Story 7-33 is implemented.
  #
  # Current behaviour (pre-fix):
  #   trusted_identity/1 returns {nil, "user"} when x-user-id is absent and
  #   system_role is ignored (bound to _system_role). The membership check then
  #   evaluates MapSet.member?(state.members, nil) → false → permission_denied raised.
  #
  # Expected behaviour (post-fix):
  #   trusted_identity/1 returns {nil, "system"}. The handler detects system_role == "system"
  #   and skips the membership check entirely. Returns %Core.GetRoomStateResponse{}.
  #
  # Scenario:
  #   - Room has @member:test.local joined.
  #   - System caller sends no x-user-id — it is the internal EventBus fanout goroutine.
  #   - System caller IS NOT a member of the room (system is not a Matrix user).
  #   - The handler must return the full response without raising.

  describe "get_room_state/2 — system-role bypass (AC #4)" do
    test "system caller with no x-user-id bypasses membership check and receives GetRoomStateResponse" do
      room_id = "!fanout-sys:test.local"
      :ok = setup_room_with_member(room_id, "@member:test.local")

      request = %Core.GetRoomStateRequest{room_id: room_id}

      # This call MUST NOT raise. Pre-fix: raises GRPC.RPCError permission_denied
      # because caller_id is nil and nil is not in state.members.
      response = Server.get_room_state(request, build_system_stream())

      assert %Core.GetRoomStateResponse{} = response,
             "expected %Core.GetRoomStateResponse{} for system caller, got: #{inspect(response)}"

      assert "@member:test.local" in response.members,
             "expected @member:test.local in response.members, got: #{inspect(response.members)}"
    end
  end

  # ─── AC #5: non-member user still receives permission_denied (7-19 regression guard) ─
  #
  # PASSES both before and after the fix.
  #
  # Verifies that the system-role bypass does NOT weaken the IDOR protection introduced
  # in Story 7-19. A regular user (system_role = "user") who is not a room member must
  # still receive permission_denied regardless of the 7-33 bypass logic.

  describe "get_room_state/2 — non-member still gets permission_denied (AC #5)" do
    test "regular non-member user with x-system-role=user receives permission_denied" do
      room_id = "!fanout-perm:test.local"
      :ok = setup_room_with_member(room_id, "@member:test.local")

      request = %Core.GetRoomStateRequest{room_id: room_id}
      stream = build_stream("@nonmember:test.local", "user")

      error =
        try do
          Server.get_room_state(request, stream)

          flunk(
            "expected GRPC.RPCError to be raised for non-member user, " <>
              "but no exception was raised — Story 7-19 IDOR fix may be broken"
          )
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected permission_denied (#{GRPC.Status.permission_denied()}) " <>
               "for non-member @nonmember:test.local, got status #{error.status} " <>
               "(message: #{error.message}). " <>
               "This indicates the Story 7-19 IDOR fix has been broken by the system-role bypass."
    end
  end

  # ─── Regression guard: member happy path ─────────────────────────────────────
  #
  # PASSES both before and after the fix.
  #
  # Verifies that a regular user who IS a room member still receives the full
  # GetRoomStateResponse after the system-role bypass is added. Ensures the bypass
  # does not disrupt the normal user path.

  describe "get_room_state/2 — member happy path regression guard" do
    test "room member with x-system-role=user receives full GetRoomStateResponse" do
      room_id = "!fanout-member:test.local"
      member = "@alice:test.local"
      :ok = setup_room_with_member(room_id, member)

      request = %Core.GetRoomStateRequest{room_id: room_id}
      stream = build_stream(member, "user")

      response = Server.get_room_state(request, stream)

      assert %Core.GetRoomStateResponse{} = response,
             "expected %Core.GetRoomStateResponse{} for room member, got: #{inspect(response)}"

      assert member in response.members,
             "expected #{member} in response.members, got: #{inspect(response.members)}"
    end
  end
end
