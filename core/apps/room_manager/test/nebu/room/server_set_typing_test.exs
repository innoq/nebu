defmodule Nebu.Room.ServerSetTypingTest do
  use ExUnit.Case, async: false

  # ─── Story 4-17: Room.Server.set_typing/4 — ephemeral typing state ─────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-17 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the :pg process groups are all process-global resources.
  #
  # Test strategy:
  #   - Tests call Nebu.Room.Server.set_typing/4 directly (public API added in
  #     this story). The function delegates to GenServer.call via(room_id).
  #   - Typing state is EPHEMERAL — lives only in Room.Server GenServer state
  #     (typing_users: MapSet). No DB write, no ETS. Persistence Strategy: Option C.
  #   - No crash/restart test required (typing_users resets to MapSet.new() on
  #     restart — this is correct and intentional per architecture spec).
  #   - :pg subscribers are registered in setup to capture {:typing_update, ...}
  #     broadcast messages. The subscriber process uses the test process's pid
  #     (self()) as a mailbox via :pg.join/3.
  #   - DB injection: Room.Server.init/1 uses Application.get_env(:room_manager, :db_module).
  #     Inject FakeDB to avoid PostgreSQL.
  #   - Rooms are started via Nebu.Room.RoomSupervisor.start_room/1 and tracked
  #     with on_exit cleanup (Horde.DynamicSupervisor.terminate_child/2).
  #   - TTL auto-expire test: sends {:typing_expire, user_id} directly to the
  #     Room.Server process for determinism (avoids Process.sleep fragility).

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeDB).

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:set_typing_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:set_typing_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:set_typing_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:set_typing_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:set_typing_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:set_typing_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:set_typing_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:set_typing_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create the ETS table for FakeDB. Guard against stale tables from --watch reruns.
    if :ets.info(:set_typing_test_db) != :undefined do
      :ets.delete(:set_typing_test_db)
    end

    :ets.new(:set_typing_test_db, [:named_table, :public, :set])

    # Inject fake DB so Room GenServers don't need PostgreSQL.
    Application.put_env(:room_manager, :db_module, FakeDB)

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # Clear :NebuTxnDedup between tests to prevent leakage.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)

      if :ets.info(:set_typing_test_db) != :undefined do
        :ets.delete(:set_typing_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Room GenServer tracking ──────────────────────────────────────────────────
  #
  # Registers an on_exit callback to stop the Room GenServer for room_id.
  # Prevents Horde-managed GenServers from accumulating across tests.

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

  # ─── Helper: start a room and join a member ───────────────────────────────────

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ok
  end

  # ─── Helper: subscribe test process to :pg room group ────────────────────────
  #
  # Joins the test process (self()) to the "room:{room_id}" :pg group so it
  # receives {:typing_update, user_id, typing} broadcast messages from Room.Server.
  # Registers cleanup to leave the group on test exit.

  defp subscribe_to_room_typing(room_id) do
    group = "room:#{room_id}"
    :pg.join(group, self())

    on_exit(fn ->
      :pg.leave(group, self())
    end)

    :ok
  end

  # ─── AT #1: set_typing/4 stores user in typing state + broadcasts via :pg ────
  #
  # AC #1 — set_typing/4 with typing=true adds the user to the GenServer's
  # typing_users MapSet and broadcasts {:typing_update, user_id, true} via :pg.
  #
  # Given: Room GenServer running for "!typingtest1:test.local" with @alice as member;
  #        test process subscribed to "room:!typingtest1:test.local" :pg group
  # When: Nebu.Room.Server.set_typing("!typingtest1:test.local", "@alice:test.local", true, 5000)
  # Then: returns :ok; typing_users MapSet contains @alice;
  #       {:typing_update, "@alice:test.local", true} is received by :pg subscriber

  describe "Room.Server.set_typing/4 — typing=true" do
    test "adds user to typing_users and broadcasts {:typing_update, user_id, true}" do
      room_id = "!typingtest1:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)
      :ok = subscribe_to_room_typing(room_id)

      # set_typing/4 does not exist yet — this call WILL FAIL (red phase).
      result = Nebu.Room.Server.set_typing(room_id, alice, true, 5000)

      assert result == :ok,
             "expected :ok from set_typing, got: #{inspect(result)}"

      # Verify typing_users was updated in GenServer state.
      state = Nebu.Room.Server.get_state(room_id)

      assert MapSet.member?(state.typing_users, alice),
             "expected #{alice} in typing_users after set_typing=true, got: #{inspect(state.typing_users)}"

      # Verify :pg broadcast was received by this test process.
      assert_receive {:typing_update, ^alice, true}, 500,
                     "expected to receive {:typing_update, #{alice}, true} via :pg broadcast"
    end
  end

  # ─── AT #2: set_typing/4 with typing=false removes user from typing state ────
  #
  # AC #1 — set_typing/4 with typing=false removes the user from typing_users and
  # broadcasts {:typing_update, user_id, false}.
  #
  # Given: Room GenServer running; @alice is currently typing (typing_users contains alice)
  # When: Nebu.Room.Server.set_typing(room_id, alice, false, 0)
  # Then: returns :ok; typing_users does NOT contain @alice;
  #       {:typing_update, "@alice:test.local", false} is received by :pg subscriber

  describe "Room.Server.set_typing/4 — typing=false" do
    test "removes user from typing_users and broadcasts {:typing_update, user_id, false}" do
      room_id = "!typingtest2:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)
      :ok = subscribe_to_room_typing(room_id)

      # First, set typing=true.
      :ok = Nebu.Room.Server.set_typing(room_id, alice, true, 5000)

      # Consume the typing=true broadcast so it doesn't bleed into the next assertion.
      assert_receive {:typing_update, ^alice, true}, 500

      # Now clear typing.
      result = Nebu.Room.Server.set_typing(room_id, alice, false, 0)

      assert result == :ok,
             "expected :ok from set_typing(false), got: #{inspect(result)}"

      state = Nebu.Room.Server.get_state(room_id)

      refute MapSet.member?(state.typing_users, alice),
             "expected #{alice} NOT in typing_users after set_typing=false, got: #{inspect(state.typing_users)}"

      assert_receive {:typing_update, ^alice, false}, 500,
                     "expected to receive {:typing_update, #{alice}, false} via :pg broadcast"
    end
  end

  # ─── AT #3: TTL expiry — :typing_expire removes user and broadcasts false ─────
  #
  # AC #1 — after timeout, Room.Server auto-removes the user from typing_users
  # and broadcasts {:typing_update, user_id, false} via :pg.
  #
  # Testing via direct message send (deterministic, avoids Process.sleep fragility):
  # we send {:typing_expire, user_id} directly to the Room.Server pid.
  #
  # Given: Room GenServer running; @alice set as typing (typing_users contains alice)
  # When: {:typing_expire, "@alice:test.local"} message sent to Room.Server pid
  # Then: typing_users no longer contains @alice;
  #       {:typing_update, "@alice:test.local", false} broadcast received

  describe "Room.Server — :typing_expire message" do
    test "removes user from typing_users and broadcasts {:typing_update, user_id, false} on expire" do
      room_id = "!typingtest3:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)
      :ok = subscribe_to_room_typing(room_id)

      # Set alice as typing.
      :ok = Nebu.Room.Server.set_typing(room_id, alice, true, 30_000)

      # Consume the typing=true broadcast.
      assert_receive {:typing_update, ^alice, true}, 500

      # Get the Room.Server pid and send the expire message directly for determinism.
      {:ok, server_pid} = Nebu.Room.RoomSupervisor.lookup_room(room_id)
      send(server_pid, {:typing_expire, alice})

      # Allow the handle_info callback to process.
      # assert_receive will block up to the timeout below — no sleep needed.
      assert_receive {:typing_update, ^alice, false}, 500,
                     "expected {:typing_update, #{alice}, false} after :typing_expire"

      state = Nebu.Room.Server.get_state(room_id)

      refute MapSet.member?(state.typing_users, alice),
             "expected #{alice} NOT in typing_users after :typing_expire, got: #{inspect(state.typing_users)}"
    end

    test ":typing_expire for user not currently typing is a no-op (no broadcast)" do
      room_id = "!typingtest4:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)
      :ok = subscribe_to_room_typing(room_id)

      # alice is NOT currently typing — GenServer must ignore the stale timer message.
      {:ok, server_pid} = Nebu.Room.RoomSupervisor.lookup_room(room_id)
      send(server_pid, {:typing_expire, alice})

      # No broadcast should be emitted for a stale expire.
      refute_receive {:typing_update, ^alice, false}, 200,
                     "expected no broadcast when :typing_expire arrives for non-typing user"
    end
  end
end
