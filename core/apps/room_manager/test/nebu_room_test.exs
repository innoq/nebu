defmodule Nebu.RoomTest do
  use ExUnit.Case, async: false

  # Horde.Registry and Horde.DynamicSupervisor are started by the room_manager
  # Application (Nebu.Room.Application) on app boot. Tests run against those
  # already-running named processes.
  # async: false is required because Horde uses global named processes and
  # Application.put_env is process-global.

  # ─── Fake DB ────────────────────────────────────────────────────────────────

  # ETS-backed fake DB — no Postgrex connection needed for unit tests.
  # Simulates the Nebu.Room.DB behaviour via Application config injection.
  defmodule FakeDB do
    # ETS table name used by this fake: :fake_room_db
    # State per room_id: {:rooms, room_id, created_at_ms}
    # State per member: {:members, room_id, user_id, left_at | nil}

    @doc "Loads active members for a room. Returns {:error, :not_found} if room unknown."
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

    @doc "Inserts a new room record."
    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:fake_room_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    @doc "Inserts a member into a room."
    def insert_member(room_id, user_id) do
      case :ets.lookup(:fake_room_db, {:member, room_id, user_id}) do
        [{_, :active}] ->
          # Already an active member (ON CONFLICT DO NOTHING equivalent)
          :ok

        _ ->
          :ets.insert(:fake_room_db, {{:member, room_id, user_id}, :active})
          :ok
      end
    end

    @doc "Soft-deletes a member (marks as left)."
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

    # Inserts a signed event into the fake ETS-backed events store.
    def insert_event(event) do
      :ets.insert(:fake_room_db, {{:event, event["event_id"]}, event})
      :ok
    end

    @doc "Persists power levels JSON for a room."
    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:fake_room_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: returns {:ok, 0} (no limit) for all rooms in unit tests.
    # Production DB returns COALESCE(max_members, 0) FROM rooms WHERE room_id = $1.
    # RED: this function does not exist yet on DBBehaviour; adding it here ensures
    # FakeDB satisfies the updated behaviour once @callback load_room_settings/1 is added.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 — NEW callback added to Nebu.Room.DBBehaviour.
    # Returns {:ok, "active"} for all rooms in unit tests (normal rooms are active).
    # RED: fails to compile until @callback get_room_status/1 is added to DBBehaviour.
    # Once DBBehaviour is updated, this stub ensures FakeDB satisfies the contract.
    def get_room_status(_room_id), do: {:ok, "active"}
  end

  # Fake DB that always returns a DB error on writes — for testing fail-safe behavior
  defmodule FailingWriteDB do
    @behaviour Nebu.Room.DBBehaviour
    def load_members(_room_id), do: {:error, :not_found}
    def insert_room(_room_id), do: {:ok, System.system_time(:millisecond)}
    def insert_member(_room_id, _user_id), do: {:error, :db_connection_lost}
    def delete_member(_room_id, _user_id), do: {:error, :db_connection_lost}
    def insert_event(_event), do: {:error, :db_connection_lost}
    def set_power_levels(_room_id, _json), do: {:error, :db_connection_lost}
    # Story 6.8: fail-open — if load_room_settings fails, GenServer defaults to 0.
    def load_room_settings(_room_id), do: {:error, :db_connection_lost}
    # Story 6.9: get_room_status/1 — fail-open: return "active" so GenServer starts normally.
    # The archive guard in init/1 only stops when status is explicitly "archived".
    def get_room_status(_room_id), do: {:ok, "active"}
    # Unused by write-error tests — stubs required to satisfy @behaviour contract.
    def get_rooms_for_user(_user_id), do: {:error, :db_connection_lost}
    def fetch_events(_room_id, _dir, _limit, _from), do: {:error, :db_connection_lost}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:error, :db_connection_lost}
    def get_event_timestamp(_event_id), do: {:error, :db_connection_lost}
    def get_room_name(_room_id), do: {:error, :db_connection_lost}
  end

  # ─── Setup ──────────────────────────────────────────────────────────────────

  setup do
    # Fresh ETS table for each test
    if :ets.whereis(:fake_room_db) != :undefined do
      :ets.delete(:fake_room_db)
    end

    :ets.new(:fake_room_db, [:named_table, :set, :public])
    Application.put_env(:room_manager, :db_module, FakeDB)

    # :NebuTxnDedup is a named table created at Application boot (Nebu.Room.Application).
    # It CANNOT be deleted and recreated between tests. Clear all entries between tests
    # to prevent idempotency state from leaking across test cases.
    :ets.delete_all_objects(:NebuTxnDedup)

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)

      if :ets.whereis(:fake_room_db) != :undefined do
        :ets.delete(:fake_room_db)
      end

      # Clean up any ETS idempotency entries left by the test
      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  defp unique_room_id(prefix) do
    "#{prefix}-#{System.unique_integer([:positive])}"
  end

  defp start_and_track(room_id) do
    {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
    end)

    {:ok, pid}
  end

  # ─── Story 4-1 Regression Tests ─────────────────────────────────────────────

  describe "Nebu.Room.RoomSupervisor.start_room/1" do
    test "returns {:ok, pid} for a new room" do
      room_id = unique_room_id("start-room")
      {:ok, pid} = start_and_track(room_id)
      assert is_pid(pid)
    end

    test "returns {:ok, pid} of existing process when room already started" do
      room_id = unique_room_id("idempotent")
      {:ok, pid1} = start_and_track(room_id)
      {:ok, pid2} = Nebu.Room.RoomSupervisor.start_room(room_id)
      assert pid1 == pid2
    end
  end

  describe "Nebu.Room.RoomSupervisor.lookup_room/1" do
    test "returns {:ok, pid} after room is started" do
      room_id = unique_room_id("lookup")
      {:ok, started_pid} = start_and_track(room_id)
      assert {:ok, found_pid} = Nebu.Room.RoomSupervisor.lookup_room(room_id)
      assert started_pid == found_pid
    end

    test "returns {:error, :not_found} for a room that was never started" do
      room_id = unique_room_id("nonexistent")
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)
    end
  end

  describe "Nebu.Room.Manager delegates" do
    test "start_room/1 delegates to RoomSupervisor" do
      room_id = unique_room_id("manager-start")
      {:ok, pid} = start_and_track(room_id)
      assert is_pid(pid)
    end

    test "lookup_room/1 delegates to RoomSupervisor" do
      room_id = unique_room_id("manager-lookup")
      {:ok, _pid} = start_and_track(room_id)
      assert {:ok, _pid} = Nebu.Room.Manager.lookup_room(room_id)
    end
  end

  # ─── Story 4-2: Room Server Lifecycle Tests ──────────────────────────────────

  describe "Nebu.Room.Server.get_state/1" do
    test "returns state map with room_id, members, power_levels, created_at" do
      room_id = unique_room_id("get-state")
      {:ok, _pid} = start_and_track(room_id)

      state = Nebu.Room.Server.get_state(room_id)

      assert state.room_id == room_id
      assert %MapSet{} = state.members
      assert is_map(state.power_levels)
      assert %DateTime{} = state.created_at
    end

    test "new room starts with empty members and empty power_levels" do
      room_id = unique_room_id("empty-state")
      {:ok, _pid} = start_and_track(room_id)

      state = Nebu.Room.Server.get_state(room_id)

      assert MapSet.size(state.members) == 0
      assert state.power_levels == %{}
    end
  end

  describe "Nebu.Room.Server.join/2" do
    test "happy path: adds user to members MapSet" do
      room_id = unique_room_id("join-happy")
      {:ok, _pid} = start_and_track(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")

      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.member?(state.members, "@alice:nebu.local")
    end

    test "idempotency: second join returns {:error, :already_member}" do
      room_id = unique_room_id("join-idempotent")
      {:ok, _pid} = start_and_track(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      assert {:error, :already_member} = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
    end

    test "multiple users can join independently" do
      room_id = unique_room_id("join-multi")
      {:ok, _pid} = start_and_track(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      assert :ok = Nebu.Room.Server.join(room_id, "@bob:nebu.local")

      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.size(state.members) == 2
      assert MapSet.member?(state.members, "@alice:nebu.local")
      assert MapSet.member?(state.members, "@bob:nebu.local")
    end

    test "DB write error: state unchanged, returns {:error, reason}" do
      Application.put_env(:room_manager, :db_module, FailingWriteDB)
      room_id = unique_room_id("join-db-error")
      {:ok, _pid} = start_and_track(room_id)

      result = Nebu.Room.Server.join(room_id, "@alice:nebu.local")

      assert {:error, _reason} = result

      # State must be unchanged — no member added
      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.size(state.members) == 0
    end
  end

  describe "Nebu.Room.Server.leave/2" do
    test "happy path: removes user from members MapSet" do
      room_id = unique_room_id("leave-happy")
      {:ok, _pid} = start_and_track(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      assert :ok = Nebu.Room.Server.leave(room_id, "@alice:nebu.local")

      state = Nebu.Room.Server.get_state(room_id)
      refute MapSet.member?(state.members, "@alice:nebu.local")
    end

    test "leave from non-member returns {:error, :not_member}" do
      room_id = unique_room_id("leave-non-member")
      {:ok, _pid} = start_and_track(room_id)

      assert {:error, :not_member} = Nebu.Room.Server.leave(room_id, "@ghost:nebu.local")
    end

    test "DB write error on leave: state unchanged, returns {:error, reason}" do
      # Start with FakeDB for join
      room_id = unique_room_id("leave-db-error")
      {:ok, _pid} = start_and_track(room_id)

      # Join succeeds with FakeDB — alice is now in the MapSet
      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")

      # Switch to FailingWriteDB — next DB call will error
      # db_module/0 uses Application.get_env at runtime, so this takes effect immediately
      Application.put_env(:room_manager, :db_module, FailingWriteDB)

      # leave/2 should return error because FailingWriteDB.delete_member/2 fails
      result = Nebu.Room.Server.leave(room_id, "@alice:nebu.local")
      assert {:error, _reason} = result

      # Fail-safe: state must be unchanged — alice still in MapSet
      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.member?(state.members, "@alice:nebu.local")
    end
  end

  describe "Nebu.Room.Server init/1 — state recovery from DB" do
    test "restarting room restores members from DB (FakeDB simulation)" do
      room_id = unique_room_id("restart-recovery")

      # Start room and add a member
      {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")

      # Verify member is in state
      state_before = Nebu.Room.Server.get_state(room_id)
      assert MapSet.member?(state_before.members, "@alice:nebu.local")

      # Crash the GenServer process (simulates unexpected crash, not graceful stop).
      # Uses :kill (not :normal) to test that Horde's supervisor restarts the process
      # and that init/1 correctly reloads state from the DB on restart.
      Process.exit(pid, :kill)

      # Wait for Horde supervisor to detect the crash and restart the process
      Process.sleep(150)

      # Restart the room — init/1 should reload from FakeDB
      {:ok, new_pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

      on_exit(fn ->
        if Process.alive?(new_pid), do: GenServer.stop(new_pid, :normal, 5_000)
      end)

      # Member must be present after restart (recovered from FakeDB)
      state_after = Nebu.Room.Server.get_state(room_id)
      assert MapSet.member?(state_after.members, "@alice:nebu.local")
    end
  end

  # ─── Story 4-4: send_event Tests ─────────────────────────────────────────────

  describe "Nebu.Room.Server.send_event/5" do
    test "happy path: returns {:ok, event_id} with event_id starting with '$'" do
      room_id = unique_room_id("send-event-happy")
      {:ok, _pid} = start_and_track(room_id)

      result =
        Nebu.Room.Server.send_event(
          room_id,
          "@alice:nebu.local",
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Hello"},
          "txn-001"
        )

      assert {:ok, event_id} = result
      assert String.starts_with?(event_id, "$")
    end

    test "determinism: Nebu.EventId.generate/1 on the same event map yields the same event_id" do
      room_id = unique_room_id("send-event-determinism")
      {:ok, _pid} = start_and_track(room_id)

      content = %{"msgtype" => "m.text", "body" => "Deterministic"}

      {:ok, event_id} =
        Nebu.Room.Server.send_event(
          room_id,
          "@alice:nebu.local",
          "m.room.message",
          content,
          "txn-det-001"
        )

      # Retrieve the persisted event from the FakeDB ETS store to get the exact
      # event map (including the timestamp chosen by the GenServer).
      [{_, stored_event}] = :ets.lookup(:fake_room_db, {:event, event_id})

      # Re-generate the event_id from the stored event (stripping signatures/event_id
      # as Nebu.EventId.generate/1 does) and verify it matches — proving determinism.
      event_for_hash = Map.drop(stored_event, ["signatures", "event_id"])
      recomputed_id = Nebu.EventId.generate(event_for_hash)

      assert event_id == recomputed_id
    end

    test "idempotency: duplicate txn_id returns the same event_id without re-processing" do
      room_id = unique_room_id("send-event-idempotent")
      {:ok, _pid} = start_and_track(room_id)

      user_id = "@alice:nebu.local"
      txn_id = "txn-idem-001"
      content = %{"msgtype" => "m.text", "body" => "Idempotent message"}

      # First call — processes and persists the event
      {:ok, event_id_1} =
        Nebu.Room.Server.send_event(room_id, user_id, "m.room.message", content, txn_id)

      # Second call with the same txn_id — must return same event_id immediately
      {:ok, event_id_2} =
        Nebu.Room.Server.send_event(room_id, user_id, "m.room.message", content, txn_id)

      assert event_id_1 == event_id_2
    end

    test "idempotency: same txn_id is per {room_id, user_id, txn_id} — different room is a new event" do
      room_id_a = unique_room_id("send-event-idem-room-a")
      room_id_b = unique_room_id("send-event-idem-room-b")
      {:ok, _pid_a} = start_and_track(room_id_a)
      {:ok, _pid_b} = start_and_track(room_id_b)

      user_id = "@alice:nebu.local"
      txn_id = "txn-shared-001"
      content = %{"msgtype" => "m.text", "body" => "Same txn_id, different room"}

      {:ok, event_id_a} =
        Nebu.Room.Server.send_event(room_id_a, user_id, "m.room.message", content, txn_id)

      {:ok, event_id_b} =
        Nebu.Room.Server.send_event(room_id_b, user_id, "m.room.message", content, txn_id)

      # Different rooms produce different event_ids (room_id is part of content hash)
      refute event_id_a == event_id_b
    end

    test "DB failure: returns {:error, reason} and ETS NebuTxnDedup is NOT updated" do
      Application.put_env(:room_manager, :db_module, FailingWriteDB)
      room_id = unique_room_id("send-event-db-fail")
      {:ok, _pid} = start_and_track(room_id)

      user_id = "@alice:nebu.local"
      txn_id = "txn-fail-001"

      result =
        Nebu.Room.Server.send_event(
          room_id,
          user_id,
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Will fail"},
          txn_id
        )

      # Must return error
      assert {:error, _reason} = result

      # AC #3: ETS must NOT contain the txn key after a failed DB write
      assert :ets.lookup(:NebuTxnDedup, {room_id, user_id, txn_id}) == []
    end

    test "DB failure: no :new_event broadcast is sent (AC #3)" do
      Application.put_env(:room_manager, :db_module, FailingWriteDB)
      room_id = unique_room_id("send-event-no-bcast")
      {:ok, _pid} = start_and_track(room_id)

      # Subscribe to this room's :pg group to detect any accidental broadcasts
      :pg.join("room:#{room_id}", self())

      Nebu.Room.Server.send_event(
        room_id,
        "@alice:nebu.local",
        "m.room.message",
        %{"msgtype" => "m.text", "body" => "Will fail"},
        "txn-nobcast-001"
      )

      # AC #3: no broadcast must have been delivered on DB failure
      refute_receive {:new_event, _}, 100
    end
  end

  # ─── Story 6-8: max_members Enforcement + update_settings ───────────────────
  #
  # RED PHASE — all tests in this describe block FAIL until:
  #   - Nebu.Room.DBBehaviour gains @callback load_room_settings(room_id) callback
  #   - FakeDB (above) gains def load_room_settings(_), do: {:ok, 0}
  #   - Nebu.Room.Server.init/1 loads max_members from db_module().load_room_settings/1
  #   - max_members field is added to GenServer state (default 0 = no limit)
  #   - Nebu.Room.Server.handle_call({:join, user_id}, ...) enforces max_members
  #   - Nebu.Room.Server.update_settings/2 public function is added
  #   - Nebu.Room.Server.handle_cast({:update_settings, settings}, state) is added

  describe "Story 6.8 — max_members enforcement in join/2" do
    # FakeDB.load_room_settings/1 must be added to FakeDB above for these to compile.
    # Until then, tests fail with UndefinedFunctionError or function_clause.

    test "join succeeds when max_members=0 (no limit) regardless of member count" do
      # max_members=0 means "no limit" — any number of members can join.
      room_id = unique_room_id("6-8-join-no-limit")
      {:ok, _pid} = start_and_track(room_id)

      # FakeDB.load_room_settings returns {:ok, 0} → no limit applied
      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      assert :ok = Nebu.Room.Server.join(room_id, "@bob:nebu.local")

      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.size(state.members) == 2
    end

    test "join blocked when member_count >= max_members returns {:error, :room_full}" do
      # Arrange: room with max_members=2 already containing 2 members (alice, bob).
      # Act: charlie tries to join.
      # Assert: returns {:error, :room_full}.
      #
      # This test uses update_settings/2 to set max_members=2 after starting the room
      # (simulating what the Admin PATCH handler + gRPC would do in production).
      # If update_settings/2 does not exist yet, the test fails with UndefinedFunctionError
      # — that is the expected RED-phase failure.
      room_id = unique_room_id("6-8-join-room-full")
      {:ok, _pid} = start_and_track(room_id)

      # Join two members successfully (no limit yet)
      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      assert :ok = Nebu.Room.Server.join(room_id, "@bob:nebu.local")

      # Set max_members=2 via update_settings (mirrors Admin PATCH + gRPC flow)
      :ok = Nebu.Room.Server.update_settings(room_id, %{max_members: 2})

      # Synchronization barrier: GenServer.call flushes any preceding casts.
      _state = Nebu.Room.Server.get_state(room_id)

      # Third join must be blocked
      assert {:error, :room_full} = Nebu.Room.Server.join(room_id, "@charlie:nebu.local")

      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.size(state.members) == 2
      refute MapSet.member?(state.members, "@charlie:nebu.local")
    end

    test "join allowed when member_count < max_members" do
      # max_members=3, only 2 members present → third join is allowed.
      room_id = unique_room_id("6-8-join-under-limit")
      {:ok, _pid} = start_and_track(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      assert :ok = Nebu.Room.Server.join(room_id, "@bob:nebu.local")

      # Set limit to 3 — one slot still available
      :ok = Nebu.Room.Server.update_settings(room_id, %{max_members: 3})
      # Synchronization barrier: GenServer.call flushes any preceding casts.
      _state = Nebu.Room.Server.get_state(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@charlie:nebu.local")

      state = Nebu.Room.Server.get_state(room_id)
      assert MapSet.size(state.members) == 3
    end

    test "room_full check does not block already_member re-join attempt" do
      # A user already in the room who tries to join again must get :already_member,
      # not :room_full, even when the room is at capacity.
      room_id = unique_room_id("6-8-already-member-priority")
      {:ok, _pid} = start_and_track(room_id)

      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")

      # Set max_members=1 — room is now full
      :ok = Nebu.Room.Server.update_settings(room_id, %{max_members: 1})
      # Synchronization barrier: GenServer.call flushes any preceding casts.
      _state = Nebu.Room.Server.get_state(room_id)

      # Alice is already a member — must get :already_member, not :room_full
      assert {:error, :already_member} = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
    end
  end

  describe "Story 6.8 — update_settings/2 GenServer cast" do
    test "update_settings/2 updates max_members in GenServer state" do
      # Arrange: room starts with max_members=0 (loaded from FakeDB).
      # Act: update_settings/2 casts {:update_settings, %{max_members: 10}} to GenServer.
      # Assert: state.max_members == 10 after the cast is processed.
      #
      # RED: fails until handle_cast({:update_settings, settings}, state) is implemented.
      room_id = unique_room_id("6-8-update-settings")
      {:ok, _pid} = start_and_track(room_id)

      state_before = Nebu.Room.Server.get_state(room_id)
      # Before Story 6.8, state does not have max_members key → Map.get returns nil.
      # After Story 6.8, it must be 0 (the initial value from FakeDB.load_room_settings).
      assert Map.get(state_before, :max_members, 0) == 0

      :ok = Nebu.Room.Server.update_settings(room_id, %{max_members: 42})

      state_after = Nebu.Room.Server.get_state(room_id)
      assert state_after.max_members == 42
    end

    test "update_settings/2 with max_members=0 disables limit (no limit)" do
      # After a limit was set, setting max_members=0 must re-open the room.
      room_id = unique_room_id("6-8-remove-limit")
      {:ok, _pid} = start_and_track(room_id)

      # Fill the room to capacity 1
      assert :ok = Nebu.Room.Server.join(room_id, "@alice:nebu.local")
      :ok = Nebu.Room.Server.update_settings(room_id, %{max_members: 1})
      # Synchronization barrier: GenServer.call flushes any preceding casts.
      _state = Nebu.Room.Server.get_state(room_id)

      # Bob is blocked
      assert {:error, :room_full} = Nebu.Room.Server.join(room_id, "@bob:nebu.local")

      # Remove the limit
      :ok = Nebu.Room.Server.update_settings(room_id, %{max_members: 0})
      # Synchronization barrier: GenServer.call flushes any preceding casts.
      _state = Nebu.Room.Server.get_state(room_id)

      # Bob can now join
      assert :ok = Nebu.Room.Server.join(room_id, "@bob:nebu.local")
    end

    test "update_settings/2 is best-effort: returns :ok immediately (fire-and-forget cast)" do
      # GenServer.cast always returns :ok immediately, even if the process is busy.
      # This test verifies the function signature returns :ok and does not block.
      room_id = unique_room_id("6-8-cast-best-effort")
      {:ok, _pid} = start_and_track(room_id)

      result = Nebu.Room.Server.update_settings(room_id, %{max_members: 100})
      assert result == :ok
    end
  end

  describe "Story 6.8 — max_members loaded from DB on GenServer restart" do
    test "max_members is recovered from DB after crash-restart (via load_room_settings)" do
      # This test verifies that when the Room GenServer restarts, it loads max_members
      # from the DB (via load_room_settings/1), not just member list.
      #
      # RED: fails until:
      #   - load_room_settings/1 is added to DBBehaviour and FakeDB
      #   - init/1 calls db_module().load_room_settings(room_id) and stores result in state
      #
      # The FakeDB.load_room_settings stub currently always returns {:ok, 0}.
      # For this specific test we use a custom FakeDB that returns {:ok, 5}.
      defmodule FakeDBWithMaxMembers do
        def load_members(room_id) do
          case :ets.lookup(:fake_room_db, {:room, room_id}) do
            [] -> {:error, :not_found}
            [{_, created_at_ms}] ->
              members = :ets.match(:fake_room_db, {{:member, room_id, :"$1"}, :active})
              {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, "{}"}
          end
        end

        def insert_room(room_id) do
          now_ms = System.system_time(:millisecond)
          :ets.insert(:fake_room_db, {{:room, room_id}, now_ms})
          {:ok, now_ms}
        end

        def insert_member(room_id, user_id) do
          :ets.insert(:fake_room_db, {{:member, room_id, user_id}, :active})
          :ok
        end

        def delete_member(room_id, user_id) do
          case :ets.lookup(:fake_room_db, {:member, room_id, user_id}) do
            [{_, :active}] ->
              :ets.insert(:fake_room_db, {{:member, room_id, user_id}, {:left, 0}})
              :ok
            _ -> {:error, :not_member}
          end
        end

        def insert_event(event) do
          :ets.insert(:fake_room_db, {{:event, event["event_id"]}, event})
          :ok
        end

        def set_power_levels(room_id, json) do
          :ets.insert(:fake_room_db, {{:power_levels, room_id}, json})
          :ok
        end

        # Story 6.8: returns {:ok, 5} simulating a room with max_members=5 in the DB.
        def load_room_settings(_room_id), do: {:ok, 5}

        # Story 6.9: get_room_status/1 — returns {:ok, "active"} so init/1 proceeds normally.
        # This module is only used to test max_members recovery; rooms are active here.
        def get_room_status(_room_id), do: {:ok, "active"}

        # Stubs required by @behaviour Nebu.Room.DBBehaviour:
        def get_rooms_for_user(_user_id), do: {:ok, []}
        def fetch_events(_room_id, _dir, _limit, _from), do: {:ok, [], "", ""}
        def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
        def get_event_timestamp(_event_id), do: {:error, :not_found}
        def get_room_name(_room_id), do: {:error, :not_found}
      end

      Application.put_env(:room_manager, :db_module, FakeDBWithMaxMembers)

      room_id = unique_room_id("6-8-restart-max-members")
      {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

      on_exit(fn ->
        Application.put_env(:room_manager, :db_module, FakeDB)
      end)

      # Initial state should have max_members=5 (from FakeDBWithMaxMembers)
      state = Nebu.Room.Server.get_state(room_id)
      assert state.max_members == 5

      # Crash the GenServer
      Process.exit(pid, :kill)
      Process.sleep(150)

      # Restart — init/1 must reload max_members=5 from DB
      {:ok, new_pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

      on_exit(fn ->
        if Process.alive?(new_pid), do: GenServer.stop(new_pid, :normal, 5_000)
      end)

      state_after = Nebu.Room.Server.get_state(room_id)
      assert state_after.max_members == 5
    end
  end
end
