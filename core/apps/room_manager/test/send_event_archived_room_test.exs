defmodule Nebu.Room.SendEventArchivedRoomTest do
  use ExUnit.Case, async: false

  # ─── Story 9-9: Archive TOCTOU Fix — send_event tests ───────────────────────
  #
  # RED PHASE — all tests in this module FAIL until Story 9-9 is implemented.
  # Failing reasons:
  #   1. Nebu.Room.DBBehaviour does not yet declare @callback check_room_status_for_update/1
  #   2. Nebu.Room.Server.handle_call({:send_event, ...}) does not yet call
  #      db_module().check_room_status_for_update/1 between idempotency lookup and event build
  #   3. FakeDB modules below will compile with warnings until callback is declared in DBBehaviour
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and ETS tables (:NebuTxnDedup, test-specific) are all process-global resources.
  #
  # Test strategy:
  #   - AC1/AC3: FakeDBWithArchivedForSend returns {:ok, "archived"} for
  #     check_room_status_for_update/1 → send_event returns {:error, :room_archived}
  #     and NO event is written to ETS.
  #   - AC4: FakeDB returns {:ok, "active"} for check_room_status_for_update/1 →
  #     send_event succeeds (regression guard).
  #   - TOCTOU race test: FakeDB allows GenServer to start (get_room_status = "active"),
  #     then check_room_status_for_update returns "archived" to simulate the race window
  #     being closed by the DB-level SELECT FOR UPDATE check.
  #   - Persistency strategy: GenServer state (Option C for this check — stateless;
  #     the check is always re-evaluated per-call via DB round-trip).

  # ─── FakeDB — active room (default for most tests) ───────────────────────────
  #
  # Returns {:ok, "active"} for both get_room_status/1 (init/1 guard)
  # and check_room_status_for_update/1 (send_event TOCTOU guard).
  #
  # RED: check_room_status_for_update/1 will cause a compile warning until
  # @callback check_room_status_for_update/1 is added to Nebu.Room.DBBehaviour.

  defmodule FakeDB do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id) do
      case :ets.lookup(:send_archived_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:send_archived_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:send_archived_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:send_archived_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:send_archived_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:send_archived_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:send_archived_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:send_archived_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: no member limit in unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: init/1 guard — "active" so GenServer starts normally.
    def get_room_status(_room_id), do: {:ok, "active"}

    # Story 9-9 (TOCTOU fix): returns {:ok, "active"} — normal path.
    # RED: this callback does not exist in DBBehaviour yet; adding it here ensures
    # that FakeDB satisfies the updated behaviour once @callback is added.
    def check_room_status_for_update(_room_id), do: {:ok, "active"}

    # Required @behaviour stubs
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _direction, _limit, _from_token), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_room_create_event(_room_id), do: {:error, :not_found}
    # Story 9-28: no thread relations in send_event_archived_room tests.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
    def fetch_event(_event_id, _room_id), do: {:error, :not_found}
  end

  # ─── FakeDBWithArchivedForSend ─────────────────────────────────────────────
  #
  # Simulates the TOCTOU race window:
  #   - get_room_status/1 returns {:ok, "active"} so Room.Server.init/1 starts
  #     the GenServer successfully (room was active when it started).
  #   - check_room_status_for_update/1 returns {:ok, "archived"} simulating the
  #     moment archive_room_atomic/1 completed between the init/1 call and the
  #     send_event SELECT FOR UPDATE check.
  #
  # This is the core TOCTOU scenario: memory says "active", DB says "archived".
  # The correct implementation must trust the DB check, not the in-memory state.

  defmodule FakeDBWithArchivedForSend do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id) do
      case :ets.lookup(:send_archived_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:send_archived_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:send_archived_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:send_archived_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:send_archived_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:send_archived_test_db, {:member, room_id, user_id})
      :ok
    end

    # insert_event must FAIL with an assertion if called — the archived check
    # must prevent us from ever reaching the DB write step.
    # RED: if insert_event/1 is called, this fake returns :ok (which would then
    # allow the test assertion on event count to catch the bug).
    def insert_event(event) do
      :ets.insert(:send_archived_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:send_archived_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: init/1 guard — returns "active" so GenServer starts normally.
    # The room is "active" at startup; it will be "archived" by the time
    # send_event/6 calls check_room_status_for_update/1.
    def get_room_status(_room_id), do: {:ok, "active"}

    # Story 9-9 (TOCTOU fix): returns {:ok, "archived"} to simulate the race window.
    # At the moment send_event's SELECT FOR UPDATE check runs, the room is already
    # archived in the DB — even though the GenServer started with "active" status.
    # RED: this callback does not exist in DBBehaviour yet.
    def check_room_status_for_update(_room_id), do: {:ok, "archived"}

    # Required @behaviour stubs
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _direction, _limit, _from_token), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_room_create_event(_room_id), do: {:error, :not_found}
    # Story 9-28: no thread relations in send_event_archived_room tests.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
    def fetch_event(_event_id, _room_id), do: {:error, :not_found}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:send_archived_test_db) != :undefined do
      :ets.delete(:send_archived_test_db)
    end

    :ets.new(:send_archived_test_db, [:named_table, :public, :set])

    # Default FakeDB injection — individual tests override per-test.
    Application.put_env(:room_manager, :db_module, FakeDB)

    # :NebuTxnDedup — named table created at Application boot.
    # Cannot be deleted/recreated; clear between tests to prevent leakage.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)

      if :ets.info(:send_archived_test_db) != :undefined do
        :ets.delete(:send_archived_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  defp unique_room_id(prefix) do
    "!#{prefix}-#{System.unique_integer([:positive])}:test.local"
  end

  # Starts a Room GenServer and registers Horde-level cleanup via on_exit.
  # Use this instead of GenServer.stop/1 to correctly clean up Horde state.
  defp start_and_track(room_id) do
    {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:ok, existing_pid} ->
            Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, existing_pid)

          _ ->
            :ok
        end
      end
    end)

    {:ok, pid}
  end

  # ─── AC3: send_event after room archived in DB is rejected ───────────────────
  #
  # Story 9-9 AC3 — TOCTOU race window test:
  # The GenServer starts while the room is "active" in DB (get_room_status).
  # Then check_room_status_for_update returns "archived" (simulating archive_room_atomic
  # completing between init and send_event).
  # Expected: send_event returns {:error, :room_archived} and NO m.room.message
  # event is written to the ETS event store.
  #
  # RED: fails until handle_call({:send_event, ...}) calls
  # db_module().check_room_status_for_update(room_id) and returns {:error, :room_archived}.

  describe "Story 9-9 AC3 — send_event on archived room is rejected (TOCTOU fix)" do
    test "send_event after room archived in DB returns {:error, :room_archived}" do
      # Arrange: use FakeDBWithArchivedForSend — room starts "active" (init succeeds),
      # but check_room_status_for_update returns "archived" (simulates race window).
      Application.put_env(:room_manager, :db_module, FakeDBWithArchivedForSend)
      room_id = unique_room_id("toctou-archived")

      {:ok, _pid} = start_and_track(room_id)

      # Act: send_event on a room that is "archived" in the DB at check time.
      result =
        Nebu.Room.Server.send_event(
          room_id,
          "@alice:test.local",
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "This must not be written"},
          "txn-toctou-1"
        )

      # Assert 1: send_event must return {:error, :room_archived}.
      # RED: fails until Room.Server.handle_call({:send_event, ...}) calls
      # check_room_status_for_update and returns {:error, :room_archived}.
      assert result == {:error, :room_archived},
             "expected {:error, :room_archived} but got: #{inspect(result)}"

      # Assert 2: ETS FakeDB must have 0 m.room.message events.
      # Filter out m.room.member events (from emit_membership_event in join, if any).
      all_events = :ets.match_object(:send_archived_test_db, {{:event, :_}, :_})

      message_events =
        Enum.filter(all_events, fn {{:event, _id}, ev} ->
          ev["type"] == "m.room.message"
        end)

      assert length(message_events) == 0,
             "expected 0 m.room.message events in ETS after archived-room send_event, " <>
               "got #{length(message_events)}: #{inspect(message_events)}"
    end

    test "ETS NebuTxnDedup is NOT updated when send_event is rejected due to archived room" do
      # AC3 corollary: the txn_id must not be cached if the event was rejected.
      # If it were cached, a future send after unarchive would be incorrectly deduplicated.
      Application.put_env(:room_manager, :db_module, FakeDBWithArchivedForSend)
      room_id = unique_room_id("toctou-ets-dedup")
      user_id = "@alice:test.local"
      txn_id = "txn-toctou-ets-1"

      {:ok, _pid} = start_and_track(room_id)

      result =
        Nebu.Room.Server.send_event(
          room_id,
          user_id,
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Must not be cached"},
          txn_id
        )

      assert result == {:error, :room_archived},
             "expected {:error, :room_archived}, got: #{inspect(result)}"

      # ETS dedup table must NOT contain the txn_id.
      assert :ets.lookup(:NebuTxnDedup, {room_id, user_id, txn_id}) == [],
             "expected :NebuTxnDedup to be empty for #{inspect({room_id, user_id, txn_id})}"
    end
  end

  # ─── AC4: Happy path unaffected — active room send_event still works ──────────
  #
  # Story 9-9 AC4 — regression guard:
  # check_room_status_for_update returns {:ok, "active"} → send_event proceeds normally.
  #
  # RED: fails until check_room_status_for_update is wired into the handler AND
  # the {:ok, _} branch falls through to the existing event-persist logic.

  describe "Story 9-9 AC4 — active room send_event is unaffected (regression guard)" do
    test "send_event on active room succeeds: returns {:ok, event_id} and event is in ETS" do
      # Arrange: FakeDB (active room) — check_room_status_for_update returns "active".
      room_id = unique_room_id("active-room-send")

      {:ok, _pid} = start_and_track(room_id)

      # Act
      result =
        Nebu.Room.Server.send_event(
          room_id,
          "@alice:test.local",
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Hello from active room"},
          "txn-active-1"
        )

      # Assert 1: returns {:ok, event_id}
      assert {:ok, event_id} = result,
             "expected {:ok, event_id} for active room, got: #{inspect(result)}"

      assert String.starts_with?(event_id, "$"),
             "expected event_id to start with '$', got: #{inspect(event_id)}"

      # Assert 2: event is present in ETS FakeDB
      events = :ets.lookup(:send_archived_test_db, {:event, event_id})
      assert length(events) == 1,
             "expected 1 event in ETS for event_id #{event_id}, got #{length(events)}"
    end

    test "existing send_event idempotency still works for active rooms" do
      # Regression guard: idempotency must not be broken by the new TOCTOU check.
      room_id = unique_room_id("active-idempotent")
      user_id = "@bob:test.local"
      txn_id = "txn-idem-active-1"

      {:ok, _pid} = start_and_track(room_id)

      content = %{"msgtype" => "m.text", "body" => "Idempotent"}

      {:ok, event_id_1} =
        Nebu.Room.Server.send_event(room_id, user_id, "m.room.message", content, txn_id)

      {:ok, event_id_2} =
        Nebu.Room.Server.send_event(room_id, user_id, "m.room.message", content, txn_id)

      assert event_id_1 == event_id_2,
             "idempotency broken: first #{event_id_1}, second #{event_id_2}"

      # Only one message event written (idempotency dedup worked).
      all_events = :ets.match_object(:send_archived_test_db, {{:event, :_}, :_})

      message_events =
        Enum.filter(all_events, fn {{:event, _id}, ev} ->
          ev["type"] == "m.room.message"
        end)

      assert length(message_events) == 1,
             "expected exactly 1 m.room.message event in ETS after idempotent sends, " <>
               "got #{length(message_events)}"
    end
  end

  # ─── TOCTOU Race Simulation ────────────────────────────────────────────────
  #
  # Story 9-9 — The core race window being closed:
  # GenServer starts with "active" in memory (from get_room_status at init).
  # Between init and send_event, archive_room_atomic/1 sets status="archived" in DB.
  # The SELECT FOR UPDATE check (check_room_status_for_update) must see "archived"
  # and reject the send, even though in-memory state says "active".
  #
  # This test explicitly names the race scenario for clarity in CI output.

  describe "Story 9-9 — TOCTOU race window: in-memory state says active, DB says archived" do
    test "send_event is rejected when DB reports archived (regardless of in-memory state)" do
      # Given: Room.Server started with FakeDBWithArchivedForSend.
      # get_room_status/1 (for init) returns {:ok, "active"} → GenServer starts.
      # check_room_status_for_update/1 (for send_event) returns {:ok, "archived"}.
      # This simulates archive_room_atomic/1 completing after init but before send_event.
      Application.put_env(:room_manager, :db_module, FakeDBWithArchivedForSend)
      room_id = unique_room_id("toctou-race")

      {:ok, _pid} = start_and_track(room_id)

      # Verify GenServer is running (in-memory state = "active" since init succeeded)
      state = Nebu.Room.Server.get_state(room_id)
      assert state.room_id == room_id,
             "GenServer must be running for the TOCTOU test to be meaningful"

      # When: send_event is called — at this point, check_room_status_for_update
      # will return {:ok, "archived"}, simulating the race window closed.
      result =
        Nebu.Room.Server.send_event(
          room_id,
          "@charlie:test.local",
          "m.room.message",
          %{"msgtype" => "m.text", "body" => "Race condition message"},
          "txn-race-1"
        )

      # Then: the event is rejected, not written
      assert result == {:error, :room_archived},
             "TOCTOU race: expected {:error, :room_archived} when DB reports archived, " <>
               "got: #{inspect(result)}"

      # No m.room.message event must exist in ETS
      all_events = :ets.match_object(:send_archived_test_db, {{:event, :_}, :_})

      message_events =
        Enum.filter(all_events, fn {{:event, _id}, ev} ->
          ev["type"] == "m.room.message"
        end)

      assert length(message_events) == 0,
             "TOCTOU race: expected 0 m.room.message events written to ETS, " <>
               "got #{length(message_events)}"
    end
  end
end
