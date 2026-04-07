defmodule Nebu.EventDispatcher.SyncTest do
  use ExUnit.Case, async: false

  # ─── Story 4-14: GET /sync — Initial Sync (Elixir side) ──────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-14 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # ETS tables, and the :NebuTxnDedup table are all process-global resources that
  # must not be shared concurrently between test cases.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.get_initial_sync/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map with http_request_headers (matches real gRPC
  #     stream contract used by all other handlers in this module).
  #   - DB injection (room_manager): Room.Server.init/1 uses
  #     Application.get_env(:room_manager, :db_module). We inject SyncTestFakeDB
  #     to avoid PostgreSQL. SyncTestFakeDB adds get_rooms_for_user/1 (new
  #     function needed by get_initial_sync/2) alongside the existing load_members,
  #     insert_room, insert_member, delete_member, insert_event functions.
  #   - DB injection (event_dispatcher messages): Application.put_env(
  #     :event_dispatcher, :messages_db_module, SyncTestFakeDB) provides
  #     fetch_events/4 for timeline events.
  #   - DB injection (event_dispatcher rooms): Application.put_env(
  #     :event_dispatcher, :rooms_db_module, SyncTestFakeDB) provides
  #     get_rooms_for_user/1 so the handler can query the user's rooms.
  #   - PgStore injection: Application.put_env(:event_dispatcher,
  #     :pg_store_module, SyncTestFakePgStore) tracks calls to
  #     persist_since_token/3. The fake stores to ETS for assertion.
  #   - Rooms are started via Nebu.Room.RoomSupervisor.start_room/1 and
  #     tracked with on_exit cleanup via Horde.DynamicSupervisor.terminate_child/2
  #     to prevent cross-test Horde pollution.
  #   - :NebuTxnDedup is cleared before each test to prevent cross-test leakage.
  #
  # NOTE: get_initial_sync/2 does NOT exist yet in Nebu.EventDispatcher.Server.
  # All tests here must fail with a compilation/function-clause error until
  # Story 4-14 adds the handler.

  alias Nebu.EventDispatcher.Server

  # ─── SyncTestFakeDB ──────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying both Nebu.Room.DB behaviour and the new
  # get_rooms_for_user/1 + fetch_events/4 functions called by the handler.
  #
  # Named table: :sync_test_db (avoids collision with other test modules).
  #
  # Member tracking key: {:member_for_user, user_id, room_id} → :active
  #   Used by get_rooms_for_user/1.
  #
  # Event tracking key: {:event, event_id} → event_map
  #   Used by fetch_events/4.
  #
  # Room tracking key: {:room, room_id} → created_at_ms
  #   Used by load_members/1 (Room.Server.init/1).
  #
  # Member tracking key: {:member, room_id, user_id} → :active
  #   Used by load_members/1 (Room.Server.init/1).

  defmodule SyncTestFakeDB do
    # ── Room.DB behaviour (Room.Server.init/1) ────────────────────────────────

    def load_members(room_id) do
      case :ets.lookup(:sync_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:sync_test_db, {{:member, room_id, :"$1"}, :active})

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:sync_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:sync_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:sync_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:sync_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    # ── New: get_rooms_for_user/1 (Story 4-14) ────────────────────────────────
    #
    # Returns {:ok, [room_id]} for all rooms where user_id is an active member.
    # Mirrors the production Nebu.Room.DB.get_rooms_for_user/1 query:
    #   SELECT room_id FROM room_members WHERE user_id = $1 AND left_at IS NULL
    #
    # In FakeDB, "active membership" is tracked via:
    #   {:member_for_user, user_id, room_id} → :active
    # This key is set by seed_user_rooms/2 (test helper below).

    def get_rooms_for_user(user_id) do
      rooms =
        :ets.match(:sync_test_db, {{:member_for_user, user_id, :"$1"}, :active})

      {:ok, Enum.map(rooms, fn [room_id] -> room_id end)}
    end

    # ── fetch_events/4 (existing signature — used by get_messages handler too) ─
    #
    # Simplified implementation: returns all events for the room in descending
    # timestamp order (newest first, as fetch_events/4 with direction "b" does).
    # Always returns {:ok, events, "", ""} — no cursor pagination needed for
    # sync tests (≤ 20 events per room in test scenarios).

    def fetch_events(room_id, _direction, limit, _from_token) do
      all_events =
        :ets.match(:sync_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev -> ev["room_id"] == room_id end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :desc)
        |> Enum.take(limit)

      {:ok, all_events, "", ""}
    end
  end

  # ─── SyncTestFakePgStore ──────────────────────────────────────────────────────
  #
  # ETS-backed fake for Nebu.Session.PgStore (injected via
  # Application.put_env(:event_dispatcher, :pg_store_module, SyncTestFakePgStore)).
  #
  # Stores persist_since_token/3 call arguments to ETS so tests can assert
  # the call was made with the correct user_id and a non-empty since_token.
  #
  # Named table: :sync_test_pg_store_calls
  # Key: {:persist_since_token, call_index} → {user_id, since_token, last_event_id}

  defmodule SyncTestFakePgStore do
    def persist_since_token(user_id, since_token, last_event_id) do
      # Record the call for assertion in tests.
      idx = :ets.info(:sync_test_pg_store_calls, :size)
      :ets.insert(:sync_test_pg_store_calls, {{:call, idx}, {user_id, since_token, last_event_id}})
      :ok
    end

    def get_since_token(_user_id) do
      {:error, :not_found}
    end

    def invalidate_session(_user_id) do
      :ok
    end

    # Helper: returns all recorded calls as a list of {user_id, since_token, last_event_id}.
    def recorded_calls do
      :ets.match(:sync_test_pg_store_calls, {{:call, :_}, :"$1"})
      |> Enum.map(fn [call] -> call end)
    end
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create ETS tables. Guard against stale tables from --watch reruns.
    for table <- [:sync_test_db, :sync_test_pg_store_calls] do
      if :ets.info(table) != :undefined do
        :ets.delete(table)
      end
    end

    :ets.new(:sync_test_db, [:named_table, :public, :set])
    :ets.new(:sync_test_pg_store_calls, [:named_table, :public, :set])

    # Inject fake DB for Room GenServer initialisation (avoids PostgreSQL).
    Application.put_env(:room_manager, :db_module, SyncTestFakeDB)

    # Inject fake DB for the get_initial_sync handler's fetch_events/4 calls.
    Application.put_env(:event_dispatcher, :messages_db_module, SyncTestFakeDB)

    # Inject fake DB for the get_initial_sync handler's get_rooms_for_user/1 calls.
    Application.put_env(:event_dispatcher, :rooms_db_module, SyncTestFakeDB)

    # Inject fake PgStore so persist_since_token/3 is testable without PostgreSQL.
    Application.put_env(:event_dispatcher, :pg_store_module, SyncTestFakePgStore)

    # Override server_name for deterministic assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # :NebuTxnDedup — clear before each test to prevent idempotency state leakage.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :rooms_db_module)
      Application.delete_env(:event_dispatcher, :pg_store_module)
      Application.delete_env(:event_dispatcher, :server_name)

      for table <- [:sync_test_db, :sync_test_pg_store_calls] do
        if :ets.info(table) != :undefined do
          :ets.delete(table)
        end
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ─────────────────────────────────────────────────────────

  defp build_stream do
    %{http_request_headers: %{}}
  end

  # ─── Room GenServer tracking ──────────────────────────────────────────────────
  #
  # Registers an on_exit callback to stop the Room GenServer via Horde.
  # Follows the send_event_test.exs pattern exactly.

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
  #
  # Starts the Room GenServer for room_id, joins user_id as a member, and
  # records an active membership in SyncTestFakeDB for get_rooms_for_user/1.
  #
  # The {:member_for_user, user_id, room_id} key mirrors what the real DB
  # query (SELECT room_id FROM room_members WHERE user_id = $1 AND left_at IS NULL)
  # would return. Without this entry, get_rooms_for_user/1 returns [].

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)

    # Record user → room membership for get_rooms_for_user/1.
    :ets.insert(:sync_test_db, {{:member_for_user, user_id, room_id}, :active})

    :ok
  end

  # ─── Helper: seed timeline events into FakeDB ─────────────────────────────────
  #
  # Inserts n events into :sync_test_db for room_id, starting from base_ts.

  defp seed_events(room_id, user_id, count) do
    base_ts = 1_700_000_000_000

    for idx <- 1..count do
      event = %{
        "event_id" => "$sync_ev#{idx}_#{room_id}",
        "room_id" => room_id,
        "sender" => user_id,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "message #{idx}"},
        "origin_server_ts" => base_ts + idx * 1000
      }

      SyncTestFakeDB.insert_event(event)
      event
    end
  end

  # ─── AT #4: get_initial_sync — user in 2 rooms → response contains both rooms ──
  #
  # AC #2, #3, #4, #10 — get_initial_sync/2 returns GetInitialSyncResponse with:
  #   - rooms list containing both room IDs
  #   - each room has state_events (membership + power_levels)
  #   - each room has timeline_events (≤ 20)
  #   - since_token is non-empty and persisted
  #
  # Given: @alice:test.local is a member of !sync1:test.local and !sync2:test.local;
  #        each room has 3 events seeded in FakeDB; FakePgStore accepts persist_since_token/3
  # When:  Server.get_initial_sync/2 is called with user_id: "@alice:test.local"
  # Then:  response is %Core.GetInitialSyncResponse{};
  #        both room IDs appear in response.rooms;
  #        each room has state_events (at least 1 m.room.member and 1 m.room.power_levels);
  #        each room has ≤ 20 timeline_events;
  #        response.since_token is non-empty

  describe "Server.get_initial_sync/2 — user in 2 rooms" do
    test "returns GetInitialSyncResponse with both room IDs, state_events, timeline_events, and since_token" do
      alice = "@alice:test.local"
      room1 = "!sync1:test.local"
      room2 = "!sync2:test.local"

      :ok = setup_room_with_member(room1, alice)
      :ok = setup_room_with_member(room2, alice)

      seed_events(room1, alice, 3)
      seed_events(room2, alice, 3)

      request = %Core.GetInitialSyncRequest{
        user_id: alice
      }

      response = Server.get_initial_sync(request, build_stream())

      assert %Core.GetInitialSyncResponse{} = response,
             "expected GetInitialSyncResponse struct, got: #{inspect(response)}"

      assert is_list(response.rooms),
             "expected rooms to be a list, got: #{inspect(response.rooms)}"

      assert length(response.rooms) == 2,
             "expected 2 rooms in response, got #{length(response.rooms)}: #{inspect(Enum.map(response.rooms, & &1.room_id))}"

      room_ids = Enum.map(response.rooms, & &1.room_id)

      assert room1 in room_ids,
             "expected #{room1} in rooms list, got: #{inspect(room_ids)}"

      assert room2 in room_ids,
             "expected #{room2} in rooms list, got: #{inspect(room_ids)}"

      # Each room must have state_events (m.room.member + m.room.power_levels)
      for room <- response.rooms do
        assert is_list(room.state_events),
               "expected state_events to be a list for #{room.room_id}"

        member_events =
          Enum.filter(room.state_events, fn ev -> ev.type == "m.room.member" end)

        pl_events =
          Enum.filter(room.state_events, fn ev -> ev.type == "m.room.power_levels" end)

        assert length(member_events) >= 1,
               "expected at least 1 m.room.member state event in #{room.room_id}, got #{length(member_events)}"

        assert length(pl_events) == 1,
               "expected exactly 1 m.room.power_levels state event in #{room.room_id}, got #{length(pl_events)}"

        # Each room must have ≤ 20 timeline_events
        assert is_list(room.timeline_events),
               "expected timeline_events to be a list for #{room.room_id}"

        assert length(room.timeline_events) <= 20,
               "expected ≤ 20 timeline events in #{room.room_id}, got #{length(room.timeline_events)}"

        # Assert chronological ordering (origin_ts increases) when > 1 event
        if length(room.timeline_events) > 1 do
          timestamps = Enum.map(room.timeline_events, & &1.origin_ts)

          assert timestamps == Enum.sort(timestamps),
                 "expected timeline events in chronological order for #{room.room_id}, got timestamps: #{inspect(timestamps)}"
        end
      end

      # since_token must be non-empty
      assert is_binary(response.since_token),
             "expected since_token to be a binary, got: #{inspect(response.since_token)}"

      assert response.since_token != "",
             "expected non-empty since_token in GetInitialSyncResponse"
    end
  end

  # ─── AT #5: get_initial_sync — user with no rooms → empty rooms list ──────────
  #
  # AC #5, #10 — when the user is not a member of any room, get_initial_sync/2
  # must still return a valid response with an empty rooms list and a valid since_token.
  #
  # Given: @bob:test.local has no room memberships (get_rooms_for_user returns [])
  # When:  Server.get_initial_sync/2 is called with user_id: "@bob:test.local"
  # Then:  response.rooms is [];
  #        response.since_token is non-empty (generated even with no rooms)

  describe "Server.get_initial_sync/2 — user with no rooms" do
    test "returns empty rooms list but generates a valid since_token" do
      bob = "@bob:test.local"

      # bob has NO room memberships — no rooms seeded for him
      # (get_rooms_for_user/1 will return {:ok, []} from SyncTestFakeDB)

      request = %Core.GetInitialSyncRequest{
        user_id: bob
      }

      response = Server.get_initial_sync(request, build_stream())

      assert %Core.GetInitialSyncResponse{} = response,
             "expected GetInitialSyncResponse struct, got: #{inspect(response)}"

      assert response.rooms == [],
             "expected empty rooms list for user with no memberships, got: #{inspect(response.rooms)}"

      assert is_binary(response.since_token) and response.since_token != "",
             "expected non-empty since_token even when user has no rooms"

      # Verify persist_since_token was still called even with no rooms
      assert SyncTestFakePgStore.recorded_calls() != [],
             "expected persist_since_token to be called even when user has no rooms"
    end
  end

  # ─── AT: room with no events → empty timeline, state still present ────────────
  #
  # AC #2 — a room with no events in DB must still appear in the response
  # with an empty timeline but with correct state events (member list + power levels).
  #
  # Given: @carol:test.local is a member of !emptyroom:test.local (0 events seeded)
  # When:  Server.get_initial_sync/2 is called with user_id: "@carol:test.local"
  # Then:  response.rooms has 1 entry for !emptyroom:test.local;
  #        that room's timeline_events is [];
  #        that room's state_events is non-empty (at least m.room.member for carol)

  describe "Server.get_initial_sync/2 — room with no events" do
    test "returns room with empty timeline but populated state_events" do
      carol = "@carol:test.local"
      room_id = "!emptyroom:test.local"

      :ok = setup_room_with_member(room_id, carol)
      # Deliberately do NOT seed any events for this room

      request = %Core.GetInitialSyncRequest{
        user_id: carol
      }

      response = Server.get_initial_sync(request, build_stream())

      assert %Core.GetInitialSyncResponse{} = response

      assert length(response.rooms) == 1,
             "expected 1 room in response, got #{length(response.rooms)}"

      [room] = response.rooms

      assert room.room_id == room_id,
             "expected room_id #{room_id}, got #{room.room_id}"

      assert room.timeline_events == [],
             "expected empty timeline_events for room with no events, got: #{inspect(room.timeline_events)}"

      assert is_list(room.state_events) and length(room.state_events) >= 1,
             "expected at least 1 state event (m.room.member for carol) in room with no events"
    end
  end

  # ─── AT: dead Room GenServer during get_initial_sync → skipped, live rooms returned ──
  #
  # MAJOR-1 regression test: if a Room GenServer dies between the DB query and
  # the get_state call, the handler must NOT crash — it should skip the dead room
  # and still return the remaining live rooms.
  #
  # Implementation: inject a FakeRoomRegistry that raises {:exit, {:noproc, _}}
  # for one room_id and returns normal state for another. Assert that:
  #   - no exception is raised
  #   - only the live room appears in response.rooms
  #   - the dead room is silently skipped

  describe "Server.get_initial_sync/2 — dead Room GenServer skipped (noproc)" do
    test "skips room whose GenServer is dead, returns other rooms normally" do
      alice = "@alice:test.local"
      live_room = "!live_noproc:test.local"
      dead_room = "!dead_noproc:test.local"

      # Set up live_room with a real GenServer.
      :ok = setup_room_with_member(live_room, alice)
      seed_events(live_room, alice, 2)

      # Seed DB membership for dead_room (simulates DB entry surviving GenServer crash).
      :ets.insert(:sync_test_db, {{:member_for_user, alice, dead_room}, :active})

      # Inject a custom room_registry_module that raises :noproc for dead_room.
      defmodule NoprocFakeRegistry do
        def get_state("!dead_noproc:test.local") do
          exit({:noproc, {GenServer, :call, ["!dead_noproc:test.local", :get_state, 5000]}})
        end

        def get_state(room_id) do
          Nebu.Room.Server.get_state(room_id)
        end
      end

      Application.put_env(:event_dispatcher, :room_registry_module, NoprocFakeRegistry)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :room_registry_module)
      end)

      request = %Core.GetInitialSyncRequest{user_id: alice}

      # Must not raise — dead room is skipped gracefully.
      response = Server.get_initial_sync(request, build_stream())

      assert %Core.GetInitialSyncResponse{} = response,
             "expected GetInitialSyncResponse, got: #{inspect(response)}"

      room_ids = Enum.map(response.rooms, & &1.room_id)

      assert live_room in room_ids,
             "expected live room #{live_room} in response, got: #{inspect(room_ids)}"

      refute dead_room in room_ids,
             "expected dead room #{dead_room} to be skipped, but it appeared in: #{inspect(room_ids)}"
    end
  end

  # ─── AT #6: since_token is persisted in sync_tokens via PgStore ───────────────
  #
  # AC #4 — after get_initial_sync/2 completes, persist_since_token/3 must have
  # been called exactly once with user_id=@alice:test.local and a non-empty
  # since_token. The last_event_id may be nil if no events exist.
  #
  # Given: @alice:test.local is in 1 room; FakePgStore tracks calls to
  #        persist_since_token/3
  # When:  Server.get_initial_sync/2 is called
  # Then:  SyncTestFakePgStore.recorded_calls() has exactly 1 entry;
  #        that entry has user_id "@alice:test.local" and a non-empty since_token

  describe "Server.get_initial_sync/2 — since_token persisted via PgStore" do
    test "calls persist_since_token/3 exactly once with correct user_id and non-empty token" do
      alice = "@alice:test.local"
      room_id = "!persist_test:test.local"

      :ok = setup_room_with_member(room_id, alice)
      seed_events(room_id, alice, 2)

      # Verify no calls yet before the handler runs
      assert SyncTestFakePgStore.recorded_calls() == [],
             "expected no persist_since_token calls before get_initial_sync"

      request = %Core.GetInitialSyncRequest{
        user_id: alice
      }

      _response = Server.get_initial_sync(request, build_stream())

      calls = SyncTestFakePgStore.recorded_calls()

      assert length(calls) == 1,
             "expected exactly 1 persist_since_token call, got #{length(calls)}"

      [{call_user_id, call_since_token, _call_last_event_id}] = calls

      assert call_user_id == alice,
             "expected persist_since_token called with user_id=#{alice}, got #{call_user_id}"

      assert is_binary(call_since_token) and call_since_token != "",
             "expected non-empty since_token in persist_since_token call, got: #{inspect(call_since_token)}"
    end
  end
end
