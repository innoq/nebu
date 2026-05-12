defmodule Nebu.Room.PowerLevelEnforcementTest do
  use ExUnit.Case, async: false

  # Integration tests for power level enforcement in Nebu.Room.Server.
  # async: false — Horde uses global named processes; Application.put_env is process-global.
  #
  # These tests MUST FAIL until the following are implemented:
  #   - Nebu.Room.PowerLevels module (power_level.ex)
  #   - Nebu.Room.Server.set_power_levels/3 (new public API)
  #   - Power level check in Nebu.Room.Server.send_event/5
  #   - FakeDB extensions: set_power_levels/2, updated load_members/1 (4-tuple)
  #   - DB migration 000013 adding rooms.power_levels_json column

  # ─── Extended FakeDB ────────────────────────────────────────────────────────
  #
  # This FakeDB extends the one in nebu_room_test.exs with:
  #   - load_members/1 returning 4-tuple (including power_levels_json)
  #   - set_power_levels/2 persisting to ETS under {:power_levels, room_id}
  #
  # IMPORTANT: The real Nebu.Room.DB.load_members/1 must be updated to return
  # the same 4-tuple signature before the integration tests pass.

  defmodule FakeDB do
    @doc """
    Loads active members for a room.
    Returns {:ok, [user_id], created_at_ms, power_levels_json} if room exists.
    Returns {:error, :not_found} if room unknown.
    """
    def load_members(room_id) do
      case :ets.lookup(:fake_room_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:fake_room_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:fake_room_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:fake_room_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      case :ets.lookup(:fake_room_db, {:member, room_id, user_id}) do
        [{_, :active}] -> :ok
        _ ->
          :ets.insert(:fake_room_db, {{:member, room_id, user_id}, :active})
          :ok
      end
    end

    def delete_member(room_id, user_id) do
      case :ets.lookup(:fake_room_db, {:member, room_id, user_id}) do
        [{_, :active}] ->
          now_ms = System.system_time(:millisecond)
          :ets.insert(:fake_room_db, {{:member, room_id, user_id}, {:left, now_ms}})
          :ok

        _ ->
          {:error, :not_member}
      end
    end

    def insert_event(event) do
      :ets.insert(:fake_room_db, {{:event, event["event_id"]}, event})
      :ok
    end

    @doc """
    Persists power levels JSON for a room.
    Stores under {:power_levels, room_id} in :fake_room_db ETS table.
    Survives Room GenServer crash/restart because ETS is owned by the test process.
    """
    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:fake_room_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 returns {:ok, "active"} — normal rooms start correctly.
    def get_room_status(_room_id), do: {:ok, "active"}

    # Story 9-9: TOCTOU fix — returns {:ok, "active"} for normal rooms.
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
  end

  defmodule FailingWriteDB do
    def load_members(_room_id), do: {:error, :not_found}
    def insert_room(_room_id), do: {:ok, System.system_time(:millisecond)}
    def insert_member(_room_id, _user_id), do: {:error, :db_connection_lost}
    def delete_member(_room_id, _user_id), do: {:error, :db_connection_lost}
    def insert_event(_event), do: {:error, :db_connection_lost}
    def set_power_levels(_room_id, _json), do: {:error, :db_connection_lost}
    # Story 6.8: fail-open — if load_room_settings fails, GenServer defaults to 0.
    def load_room_settings(_room_id), do: {:error, :db_connection_lost}
    # Story 6.9: fail-open — if get_room_status errors, GenServer starts normally.
    def get_room_status(_room_id), do: {:ok, "active"}
    # Story 9-9: TOCTOU fix — fail-open semantics, proceed on DB error.
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
  end

  # ─── Setup ──────────────────────────────────────────────────────────────────

  setup do
    if :ets.whereis(:fake_room_db) != :undefined do
      :ets.delete(:fake_room_db)
    end

    :ets.new(:fake_room_db, [:named_table, :set, :public])
    Application.put_env(:room_manager, :db_module, FakeDB)

    :ets.delete_all_objects(:NebuTxnDedup)

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)

      if :ets.whereis(:fake_room_db) != :undefined do
        :ets.delete(:fake_room_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  defp unique_room_id(prefix) do
    "#{prefix}-#{System.unique_integer([:positive])}"
  end

  # Starts a room via Horde and registers a cleanup handler.
  # Cleanup attempts a graceful stop; if the process is already dead (e.g. after
  # a :kill test), the on_exit is a no-op.
  defp start_and_track_room(room_id) do
    {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      # Terminate via Horde supervisor so it does not restart the process
      Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
    end)

    {:ok, pid}
  end

  # Deterministic poll: waits until lookup_room returns a NEW pid (not old_pid).
  # Uses Enum.reduce_while to avoid fixed Process.sleep durations.
  # Asserts within 5 000 ms (500 iterations x 10 ms) — CI needs more headroom.
  defp await_new_pid(room_id, old_pid) do
    result =
      Enum.reduce_while(1..500, nil, fn _i, _acc ->
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:ok, pid} when pid != old_pid -> {:halt, {:ok, pid}}
          _ ->
            Process.sleep(10)
            {:cont, nil}
        end
      end)

    case result do
      {:ok, new_pid} -> new_pid
      nil -> flunk("Horde did not restart room #{room_id} within 5000ms")
    end
  end

  # Constructs default power levels with creator at 100.
  # Mirrors the logic that create_room flow will use in EventDispatcher.Server.
  defp creator_power_levels(creator_id) do
    Nebu.Room.Server.default_power_levels()
    |> put_in(["users", creator_id], 100)
  end

  # ─── AC#1: Room creator gets power level 100 after creation ─────────────────

  describe "room creator power level on creation" do
    test "creator has power 100 in power_levels.users after set_power_levels/3" do
      room_id = unique_room_id("creator-pl")
      creator_id = "@bob:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)

      creator_pl = creator_power_levels(creator_id)
      assert :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      state = Nebu.Room.Server.get_state(room_id)
      assert state.power_levels["users"][creator_id] == 100
    end

    test "power_levels map contains all required default keys after set_power_levels/3" do
      room_id = unique_room_id("creator-pl-keys")
      creator_id = "@bob:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)

      creator_pl = creator_power_levels(creator_id)
      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      state = Nebu.Room.Server.get_state(room_id)
      pl = state.power_levels

      assert pl["ban"] == 50
      assert pl["kick"] == 50
      assert pl["invite"] == 0
      assert pl["redact"] == 50
      assert pl["state_default"] == 50
      assert pl["events_default"] == 0
      assert pl["users_default"] == 0
      assert is_map(pl["users"])
      assert is_map(pl["events"])
    end
  end

  # ─── AC#2: Regular member (level 0) cannot send when threshold > 0 ──────────

  describe "send_event power level enforcement" do
    test "user with power 0 cannot send when events_default is 50" do
      room_id = unique_room_id("send-forbidden")
      creator_id = "@admin:test.local"
      alice_id = "@alice:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)
      :ok = Nebu.Room.Server.join(room_id, alice_id)

      # Raise events_default to 50; Alice has no per-user override (power 0)
      restricted_pl =
        creator_power_levels(creator_id)
        |> Map.put("events_default", 50)

      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, restricted_pl)

      result =
        Nebu.Room.Server.send_event(
          room_id,
          alice_id,
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Should be blocked"},
          "txn-forbidden-001"
        )

      assert result == {:error, :forbidden}
    end

    test "forbidden user cannot get idempotent response for a rejected event" do
      # Power check must fire BEFORE the ETS idempotency lookup.
      # An unauthorized user must never receive an event_id — not even a cached one.
      room_id = unique_room_id("send-forbidden-no-idem")
      creator_id = "@admin:test.local"
      alice_id = "@alice:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)
      :ok = Nebu.Room.Server.join(room_id, alice_id)

      restricted_pl =
        creator_power_levels(creator_id)
        |> Map.put("events_default", 50)

      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, restricted_pl)

      txn_id = "txn-forbidden-idem-001"

      # First call
      assert {:error, :forbidden} =
               Nebu.Room.Server.send_event(
                 room_id,
                 alice_id,
                 "m.room.message",
                 %{"msgtype" => "m.text", "body" => "Blocked"},
                 txn_id
               )

      # Second call with same txn_id — must also be forbidden, no cached event_id
      assert {:error, :forbidden} =
               Nebu.Room.Server.send_event(
                 room_id,
                 alice_id,
                 "m.room.message",
                 %{"msgtype" => "m.text", "body" => "Blocked"},
                 txn_id
               )

      # ETS must contain no entry for this txn
      assert :ets.lookup(:NebuTxnDedup, {room_id, alice_id, txn_id}) == []
    end
  end

  # ─── AC#3: User with power 50 can send when threshold is 50 ─────────────────

  describe "send_event allowed at matching threshold" do
    test "user with power 50 can send when events_default is 50" do
      room_id = unique_room_id("send-allowed-50")
      creator_id = "@admin:test.local"
      mod_id = "@mod:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)
      :ok = Nebu.Room.Server.join(room_id, mod_id)

      # Raise events_default to 50 and grant mod power 50
      restricted_pl =
        creator_power_levels(creator_id)
        |> Map.put("events_default", 50)
        |> put_in(["users", mod_id], 50)

      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, restricted_pl)

      result =
        Nebu.Room.Server.send_event(
          room_id,
          mod_id,
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Mod message"},
          "txn-mod-001"
        )

      assert {:ok, event_id} = result
      assert String.starts_with?(event_id, "$")
    end

    test "default user can send when events_default is 0 (default)" do
      room_id = unique_room_id("send-allowed-default")
      creator_id = "@admin:test.local"
      alice_id = "@alice:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)
      :ok = Nebu.Room.Server.join(room_id, alice_id)

      creator_pl = creator_power_levels(creator_id)
      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      result =
        Nebu.Room.Server.send_event(
          room_id,
          alice_id,
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Hello"},
          "txn-alice-001"
        )

      assert {:ok, event_id} = result
      assert String.starts_with?(event_id, "$")
    end
  end

  # ─── AC#4: set_power_levels/3 updates state + persists to DB ────────────────

  describe "Nebu.Room.Server.set_power_levels/3" do
    test "updates in-memory power_levels state" do
      room_id = unique_room_id("set-pl-state")
      creator_id = "@creator:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)

      new_pl =
        creator_power_levels(creator_id)
        |> Map.put("ban", 75)

      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, new_pl)

      state = Nebu.Room.Server.get_state(room_id)
      assert state.power_levels["ban"] == 75
      assert state.power_levels["users"][creator_id] == 100
    end

    test "persists power_levels_json to DB via db_module().set_power_levels/2" do
      room_id = unique_room_id("set-pl-db")
      creator_id = "@creator:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)

      creator_pl = creator_power_levels(creator_id)
      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      # Verify ETS (FakeDB backing store) has the persisted power_levels_json
      assert [{_, json_str}] = :ets.lookup(:fake_room_db, {:power_levels, room_id})
      assert is_binary(json_str)

      decoded = Jason.decode!(json_str)
      assert decoded["users"][creator_id] == 100
    end

    test "non-admin caller (power 0) cannot set power levels — returns {:error, :forbidden}" do
      room_id = unique_room_id("set-pl-forbidden")
      creator_id = "@creator:test.local"
      alice_id = "@alice:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)
      :ok = Nebu.Room.Server.join(room_id, alice_id)

      creator_pl = creator_power_levels(creator_id)
      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      # Alice has power 0 — state_default is 50 → should be rejected
      tampered_pl = put_in(creator_pl, ["users", alice_id], 100)
      result = Nebu.Room.Server.set_power_levels(room_id, alice_id, tampered_pl)

      assert result == {:error, :forbidden}

      # In-memory state must be unchanged
      state = Nebu.Room.Server.get_state(room_id)
      refute Map.get(state.power_levels["users"], alice_id) == 100
    end

    test "set_power_levels/3 DB failure returns {:error, reason} and state unchanged" do
      room_id = unique_room_id("set-pl-db-fail")
      creator_id = "@creator:test.local"
      {:ok, _pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)

      creator_pl = creator_power_levels(creator_id)
      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      # Capture current state before injecting DB failure
      state_before = Nebu.Room.Server.get_state(room_id)

      # Switch to failing DB — next write will fail
      Application.put_env(:room_manager, :db_module, FailingWriteDB)

      modified_pl = Map.put(creator_pl, "ban", 99)
      result = Nebu.Room.Server.set_power_levels(room_id, creator_id, modified_pl)

      assert {:error, _reason} = result

      # State must be unchanged (fail-safe)
      state_after = Nebu.Room.Server.get_state(room_id)
      assert state_after.power_levels["ban"] == state_before.power_levels["ban"]
    end
  end

  # ─── AC#5: Crash/restart — power levels survive Process.exit(:kill) ─────────
  #
  # MANDATORY for any GenServer state story (CLAUDE.md standard).
  # Power levels are persisted to DB (FakeDB ETS) so they survive process restart.
  # Horde restarts the GenServer; init/1 loads power_levels_json from DB.

  describe "crash/restart — power levels survive :kill" do
    test "power levels are recovered from DB after Horde restart" do
      room_id = unique_room_id("crash-pl")
      creator_id = "@creator:test.local"
      {:ok, old_pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)

      creator_pl = creator_power_levels(creator_id)
      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

      # Verify state before crash
      state_before = Nebu.Room.Server.get_state(room_id)
      assert state_before.power_levels["users"][creator_id] == 100

      # Kill GenServer — Horde must restart it
      Process.exit(old_pid, :kill)

      # Deterministic poll: wait for Horde to register a new pid (not old_pid)
      new_pid = await_new_pid(room_id, old_pid)
      assert new_pid != old_pid

      on_exit(fn ->
        Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, new_pid)
      end)

      # Power levels must be recovered from FakeDB ETS (simulating PostgreSQL)
      state_after = Nebu.Room.Server.get_state(room_id)
      assert state_after.power_levels["users"][creator_id] == 100
      assert state_after.power_levels["ban"] == 50
      assert state_after.power_levels["events_default"] == 0
    end

    test "send_event enforces power levels correctly after crash/restart" do
      room_id = unique_room_id("crash-pl-enforce")
      creator_id = "@creator:test.local"
      alice_id = "@alice:test.local"
      {:ok, old_pid} = start_and_track_room(room_id)

      :ok = Nebu.Room.Server.join(room_id, creator_id)
      :ok = Nebu.Room.Server.join(room_id, alice_id)

      # Set restricted power levels: events_default = 50
      restricted_pl =
        creator_power_levels(creator_id)
        |> Map.put("events_default", 50)

      :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, restricted_pl)

      # Kill and wait for restart
      Process.exit(old_pid, :kill)
      new_pid = await_new_pid(room_id, old_pid)

      on_exit(fn ->
        Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, new_pid)
      end)

      # After restart, power level restriction must still apply
      result =
        Nebu.Room.Server.send_event(
          room_id,
          alice_id,
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Should still be blocked"},
          "txn-post-crash-001"
        )

      assert result == {:error, :forbidden}
    end
  end

  # ─── AC: default_power_levels/0 is public API ────────────────────────────────

  describe "Nebu.Room.Server.default_power_levels/0" do
    test "is callable as a public function and returns the standard defaults map" do
      levels = Nebu.Room.Server.default_power_levels()

      assert levels["ban"] == 50
      assert levels["kick"] == 50
      assert levels["invite"] == 0
      assert levels["redact"] == 50
      assert levels["state_default"] == 50
      assert levels["events_default"] == 0
      assert levels["users_default"] == 0
      assert levels["users"] == %{}
      assert levels["events"] == %{}
    end
  end
end
