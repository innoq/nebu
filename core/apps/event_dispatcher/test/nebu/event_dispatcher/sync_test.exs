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

          pl_json =
            case :ets.lookup(:sync_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
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

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:sync_test_db, {{:power_levels, room_id}, power_levels_json})
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

    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}

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
        |> Enum.filter(fn ev -> ev["room_id"] == room_id and Map.has_key?(ev, "event_type") end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :desc)
        |> Enum.take(limit)

      {:ok, all_events, "", ""}
    end

    # ── fetch_events_since/3 (Story 4-15 / 5.29d AC1) ────────────────────────
    def fetch_events_since(room_id, last_event_id, limit) do
      cutoff_ts =
        case last_event_id do
          nil -> 0
          "" -> 0
          id ->
            case get_event_timestamp(id) do
              {:ok, ts} -> ts
              {:error, _} -> 0
            end
        end

      events =
        :ets.match(:sync_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev ->
          ev["room_id"] == room_id and
            Map.has_key?(ev, "event_type") and
            ev["origin_server_ts"] > cutoff_ts
        end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :asc)
        |> Enum.take(limit)

      {:ok, events}
    end

    # ── get_event_timestamp/1 (Story 4-15 / 5.29d AC1) ───────────────────────
    def get_event_timestamp(event_id) do
      case :ets.lookup(:sync_test_db, {:event, event_id}) do
        [{_, event_map}] -> {:ok, event_map["origin_server_ts"]}
        [] -> {:error, :not_found}
      end
    end

    # ── get_room_name/1 (Story 5.29d AC1 — interface sync with Nebu.Room.DB) ──
    def get_room_name(room_id) do
      events =
        :ets.match(:sync_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev ->
          ev["room_id"] == room_id and ev["type"] == "m.room.name"
        end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :desc)

      case events do
        [ev | _] ->
          name = get_in(ev, ["content", "name"])
          if name, do: {:ok, name}, else: {:error, :not_found}
        [] ->
          {:error, :not_found}
      end
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 — returns {:ok, "active"} so normal rooms start correctly.
    def get_room_status(_room_id), do: {:ok, "active"}
    # Story 9-9: TOCTOU fix — returns {:ok, "active"} for normal rooms.
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
    def get_room_creator(_room_id), do: {:error, :not_found}
    # Story 9-7: returns empty list (no generic state events in unit tests).
    def get_generic_state_events(_room_id), do: {:ok, []}
    # MAJOR-2 fix: no persisted create event in sync unit tests; synthesized fallback used.
    def get_room_create_event(_room_id), do: {:error, :not_found}
    # Story 9-28: no thread relations in sync unit tests — return empty list.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
    # Story 9-28: no thread children in sync unit tests — return 0.
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── SyncTestFakeInviteDB ────────────────────────────────────────────────────
  #
  # No-op fake for Nebu.Room.InviteDB. Injected via
  # Application.put_env(:event_dispatcher, :invite_db_module, SyncTestFakeInviteDB).
  # Prevents do_incremental_sync from hitting Ecto when fetching invite rooms.

  defmodule SyncTestFakeInviteDB do
    def get_pending_invite_rooms_for_user(_user_id), do: {:ok, []}
    def get_declined_invite_rooms_for_user(_user_id), do: {:ok, []}
    def insert_invitation(_room_id, _inviter, _invitee), do: :ok
    def accept_invitation(_room_id, _invitee_id), do: :ok
    def reject_invitation(_room_id, _invitee_id), do: :ok
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

    # Inject fake InviteDB so do_incremental_sync doesn't hit Ecto for pending invites.
    Application.put_env(:event_dispatcher, :invite_db_module, SyncTestFakeInviteDB)

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
      Application.delete_env(:event_dispatcher, :invite_db_module)
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

  # ═══════════════════════════════════════════════════════════════════════════════
  # Story 4-15: get_sync_delta/2 — Incremental Sync + Long-Polling
  # ═══════════════════════════════════════════════════════════════════════════════
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests below are expected to FAIL until Story 4-15 is implemented.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.get_sync_delta/2 directly.
  #   - SyncDeltaFakePgStore extends SyncTestFakePgStore with get_since_token/1
  #     returning configurable responses. Uses the same :sync_test_pg_store_calls
  #     ETS table as the existing fake.
  #   - SyncDeltaFakeDB extends SyncTestFakeDB with fetch_events_since/3 (new DB
  #     function required by the incremental sync handler) and get_event_timestamp/1.
  #   - Long-poll timeout is configured via:
  #       Application.put_env(:event_dispatcher, :sync_long_poll_timeout_ms, 100)
  #     so tests complete quickly (100 ms instead of the production 30 000 ms).
  #   - :pg subscription and cleanup is verified by inspecting :pg.get_members/1.
  #
  # NOTE: get_sync_delta/2 does NOT exist yet in Nebu.EventDispatcher.Server.
  # All tests here must fail with a function_clause or UndefinedFunctionError
  # until Story 4-15 adds the handler.

  # ─── SyncDeltaFakePgStore ─────────────────────────────────────────────────────
  #
  # Extends the existing SyncTestFakePgStore with a configurable get_since_token/1.
  # Tests inject this module via:
  #   Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
  #
  # The ETS table :sync_test_pg_store_calls is already created in setup/0 above.

  defmodule SyncDeltaFakePgStore do
    # Configurable response for get_since_token/1 and get_since_token/2.
    # Set via: :ets.insert(:sync_delta_pg_store_config, {:get_since_token_response, result})
    # Default: {:error, :not_found}
    def get_since_token(_user_id) do
      case :ets.lookup(:sync_delta_pg_store_config, :get_since_token_response) do
        [{_, response}] -> response
        [] -> {:error, :not_found}
      end
    end

    # Per-device variant (Story 9-22): delegates to the same configurable response.
    def get_since_token(_user_id, _device_id) do
      case :ets.lookup(:sync_delta_pg_store_config, :get_since_token_response) do
        [{_, response}] -> response
        [] -> {:error, :not_found}
      end
    end

    def persist_since_token(user_id, since_token, last_event_id) do
      idx = :ets.info(:sync_test_pg_store_calls, :size)
      :ets.insert(:sync_test_pg_store_calls, {{:call, idx}, {user_id, since_token, last_event_id}})
      :ok
    end

    # Per-device variant (Story 9-22): records the 4-arg call form.
    def persist_since_token(user_id, device_id, since_token, last_event_id) do
      idx = :ets.info(:sync_test_pg_store_calls, :size)
      :ets.insert(:sync_test_pg_store_calls, {{:call, idx}, {user_id, device_id, since_token, last_event_id}})
      :ok
    end

    def invalidate_session(_user_id) do
      :ok
    end

    def invalidate_session(_user_id, _device_id) do
      :ok
    end

    def recorded_calls do
      :ets.match(:sync_test_pg_store_calls, {{:call, :_}, :"$1"})
      |> Enum.map(fn [call] -> call end)
    end
  end

  # ─── SyncDeltaFakeDB ──────────────────────────────────────────────────────────
  #
  # Extends SyncTestFakeDB with fetch_events_since/3 and get_event_timestamp/1,
  # both required by the get_sync_delta/2 handler.
  #
  # fetch_events_since/3 — returns events with origin_server_ts > a timestamp.
  #   Signature: fetch_events_since(room_id, last_event_id, limit) :: {:ok, [event_map]}
  #   Implementation: looks up last_event_id timestamp, then returns events after it.
  #
  # get_event_timestamp/1 — returns origin_server_ts for a given event_id.
  #   Signature: get_event_timestamp(event_id) :: {:ok, integer()} | {:error, :not_found}

  defmodule SyncDeltaFakeDB do
    # Delegate existing functions to SyncTestFakeDB.
    defdelegate load_members(room_id), to: SyncTestFakeDB
    defdelegate insert_room(room_id), to: SyncTestFakeDB
    defdelegate insert_member(room_id, user_id), to: SyncTestFakeDB
    defdelegate delete_member(room_id, user_id), to: SyncTestFakeDB
    defdelegate insert_event(event), to: SyncTestFakeDB
    defdelegate set_power_levels(room_id, power_levels_json), to: SyncTestFakeDB
    defdelegate get_rooms_for_user(user_id), to: SyncTestFakeDB
    defdelegate get_recently_left_rooms_for_user(user_id), to: SyncTestFakeDB
    defdelegate fetch_events(room_id, direction, limit, from_token), to: SyncTestFakeDB
    defdelegate get_room_name(room_id), to: SyncTestFakeDB
    # Story 6.8: delegate load_room_settings/1 to SyncTestFakeDB (returns {:ok, 0}).
    defdelegate load_room_settings(room_id), to: SyncTestFakeDB
    # Story 6.9: delegate get_room_status/1 to SyncTestFakeDB (returns {:ok, "active"}).
    defdelegate get_room_status(room_id), to: SyncTestFakeDB
    defdelegate get_room_creator(room_id), to: SyncTestFakeDB
    # Story 9-7: delegate get_generic_state_events/1 to SyncTestFakeDB (returns {:ok, []}).
    defdelegate get_generic_state_events(room_id), to: SyncTestFakeDB
    # MAJOR-2 fix: delegate get_room_create_event/1 to SyncTestFakeDB (returns {:error, :not_found}).
    defdelegate get_room_create_event(room_id), to: SyncTestFakeDB

    # ── New: fetch_events_since/3 (Story 4-15) ────────────────────────────────
    #
    # Returns events for room_id with origin_server_ts strictly greater than
    # the origin_server_ts of last_event_id. If last_event_id is nil, returns
    # all events (same as initial sync). Returns at most `limit` events sorted
    # by origin_server_ts ascending (chronological order).

    def fetch_events_since(room_id, last_event_id, limit) do
      cutoff_ts =
        case last_event_id do
          nil ->
            0

          id ->
            case get_event_timestamp(id) do
              {:ok, ts} -> ts
              {:error, :not_found} -> 0
            end
        end

      events =
        :ets.match(:sync_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev ->
          ev["room_id"] == room_id and
            Map.has_key?(ev, "event_type") and
            ev["origin_server_ts"] > cutoff_ts
        end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :asc)
        |> Enum.take(limit)

      {:ok, events}
    end

    # ── New: get_event_timestamp/1 (Story 4-15) ───────────────────────────────
    #
    # Returns {:ok, origin_server_ts} for a given event_id, or
    # {:error, :not_found} if the event does not exist in FakeDB.

    def get_event_timestamp(event_id) do
      case :ets.lookup(:sync_test_db, {:event, event_id}) do
        [{_, event_map}] ->
          {:ok, event_map["origin_server_ts"]}

        [] ->
          {:error, :not_found}
      end
    end

    # Story 9-28: no thread relations in sync delta unit tests — return empty list / 0.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── Story 4-15 AT #1: get_sync_delta — pending events returned immediately ───
  #
  # AC #2 (Story 4-15): when events exist with origin_server_ts > last_event_id
  # timestamp, the handler returns them immediately (no long-poll wait).
  #
  # AC #13 (Story 4-15, Elixir tests): GetSyncDelta with pending events → returns
  # delta immediately.
  #
  # Given: @alice:test.local is a member of !delta1:test.local;
  #        event $ev_old exists at ts 1_700_000_000_000 (= last_event_id);
  #        event $ev_new exists at ts 1_700_000_001_000 (> last_event_id);
  #        SyncDeltaFakePgStore.get_since_token returns {:ok, %{since_token: "s_old", last_event_id: "$ev_old"}}
  # When:  Server.get_sync_delta/2 called with user_id: alice, since_token: "s_old", timeout_ms: 5000
  # Then:  response is %Core.GetSyncDeltaResponse{};
  #        response.rooms contains !delta1:test.local with $ev_new in timeline_events;
  #        response.since_token != "s_old" (freshly generated);
  #        returns immediately (does not wait for timeout_ms)

  describe "Server.get_sync_delta/2 — pending events returned immediately" do
    test "returns delta with new events without waiting for timeout" do
      alice = "@alice:test.local"
      room_id = "!delta1:test.local"

      # Configure SyncDeltaFakePgStore and SyncDeltaFakeDB
      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # Seed the "old" event (= last_event_id) and "new" event (= delta)
      old_event = %{
        "event_id" => "$ev_old_delta1",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "old"},
        "origin_server_ts" => 1_700_000_000_000
      }

      new_event = %{
        "event_id" => "$ev_new_delta1",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "new"},
        "origin_server_ts" => 1_700_000_001_000
      }

      SyncDeltaFakeDB.insert_event(old_event)
      SyncDeltaFakeDB.insert_event(new_event)

      # Configure get_since_token to return old event's position
      :ets.insert(:sync_delta_pg_store_config, {:get_since_token_response, {:ok, %{since_token: "s_old", last_event_id: "$ev_old_delta1"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_old",
        timeout_ms: 5000
      }

      # Time the call: must return well before the 5000 ms timeout
      start_ms = System.monotonic_time(:millisecond)
      response = Server.get_sync_delta(request, build_stream())
      elapsed_ms = System.monotonic_time(:millisecond) - start_ms

      assert %Core.GetSyncDeltaResponse{} = response,
             "expected GetSyncDeltaResponse struct, got: #{inspect(response)}"

      assert elapsed_ms < 1000,
             "expected immediate return (< 1 s) when events are pending, took #{elapsed_ms} ms"

      assert is_list(response.rooms),
             "expected response.rooms to be a list"

      room_ids = Enum.map(response.rooms, & &1.room_id)

      assert room_id in room_ids,
             "expected #{room_id} in response.rooms, got: #{inspect(room_ids)}"

      delta_room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      timeline_event_ids = Enum.map(delta_room.timeline_events, & &1.event_id)

      assert "$ev_new_delta1" in timeline_event_ids,
             "expected new event $ev_new_delta1 in timeline_events, got: #{inspect(timeline_event_ids)}"

      refute "$ev_old_delta1" in timeline_event_ids,
             "expected old event $ev_old_delta1 NOT in timeline_events (it is before the since_token)"

      # since_token in response must be a NEW token — not the incoming "s_old"
      assert response.since_token != "s_old",
             "response.since_token must be a new token, not the incoming since_token"

      assert is_binary(response.since_token) and response.since_token != "",
             "expected non-empty since_token in response"

      # fallback_to_initial must be false (token was valid)
      assert response.fallback_to_initial == false,
             "expected fallback_to_initial=false when since_token is valid"
    end
  end

  # ─── Story 4-15 AT #2: get_sync_delta — no events, timeout fires → empty delta ─
  #
  # AC #4 (Story 4-15): when no new events exist and timeout_ms > 0, the handler
  # waits and returns an empty delta when the timeout fires.
  #
  # AC #13 (Story 4-15): GetSyncDelta with no events, timeout 100 ms → returns
  # empty delta after timeout.
  #
  # Given: @alice:test.local is a member of !delta2:test.local;
  #        no events after last_event_id;
  #        SyncDeltaFakePgStore.get_since_token returns stored token;
  #        :sync_long_poll_timeout_ms overridden to 100 ms via Application.put_env
  # When:  Server.get_sync_delta/2 called with timeout_ms: 100
  # Then:  after ~100 ms, returns GetSyncDeltaResponse with empty rooms;
  #        response.since_token is a NEW token (persisted via SyncDeltaFakePgStore);
  #        response.fallback_to_initial is false

  describe "Server.get_sync_delta/2 — no events, timeout fires" do
    test "waits for timeout then returns empty delta with new since_token" do
      alice = "@alice:test.local"
      room_id = "!delta2:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # Seed only an "old" event — nothing newer than it
      old_event = %{
        "event_id" => "$ev_old_delta2",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "old only"},
        "origin_server_ts" => 1_700_000_000_000
      }

      SyncDeltaFakeDB.insert_event(old_event)

      :ets.insert(:sync_delta_pg_store_config, {:get_since_token_response, {:ok, %{since_token: "s_old_2", last_event_id: "$ev_old_delta2"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_old_2",
        timeout_ms: 100
      }

      start_ms = System.monotonic_time(:millisecond)
      response = Server.get_sync_delta(request, build_stream())
      elapsed_ms = System.monotonic_time(:millisecond) - start_ms

      assert %Core.GetSyncDeltaResponse{} = response,
             "expected GetSyncDeltaResponse struct, got: #{inspect(response)}"

      # Should have waited approximately timeout_ms (100 ms) before returning
      assert elapsed_ms >= 90,
             "expected handler to wait ~100 ms for long-poll, but returned after #{elapsed_ms} ms"

      # Should not wait much longer than timeout_ms
      assert elapsed_ms < 2000,
             "handler took too long (#{elapsed_ms} ms); expected near-100 ms timeout"

      assert response.rooms == [],
             "expected empty rooms in empty delta response, got: #{inspect(response.rooms)}"

      assert is_binary(response.since_token) and response.since_token != "",
             "expected non-empty since_token in empty delta response"

      assert response.since_token != "s_old_2",
             "response.since_token must be a NEW token, not the incoming since_token"

      assert response.fallback_to_initial == false,
             "expected fallback_to_initial=false (token was valid)"
    end
  end

  # ─── Story 4-15 AT #3: get_sync_delta — unknown since_token → fallback ─────────
  #
  # AC #6 (Story 4-15): when get_since_token returns {:error, :not_found}, the
  # handler falls back to initial sync and sets fallback_to_initial: true.
  #
  # AC #13 (Story 4-15): GetSyncDelta with unknown since_token → falls back to
  # initial sync.
  #
  # Given: SyncDeltaFakePgStore.get_since_token returns {:error, :not_found};
  #        @alice:test.local is a member of !delta3:test.local with 2 events
  # When:  Server.get_sync_delta/2 called with since_token: "unknown_token"
  # Then:  response.fallback_to_initial is true;
  #        response.rooms contains !delta3:test.local with all its events (full sync);
  #        response.since_token is non-empty

  describe "Server.get_sync_delta/2 — unknown since_token falls back to initial sync" do
    test "returns full initial sync response with fallback_to_initial: true" do
      alice = "@alice:test.local"
      room_id = "!delta3:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)
      seed_events(room_id, alice, 2)

      # Note: get_since_token/1 is keyed by user_id, not by token value.
      # The fallback triggers when no token is stored for this user_id, regardless of the since_token value provided.
      # get_since_token returns :not_found for the unknown token
      :ets.insert(:sync_delta_pg_store_config, {:get_since_token_response, {:error, :not_found}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "unknown_token",
        timeout_ms: 0
      }

      response = Server.get_sync_delta(request, build_stream())

      assert %Core.GetSyncDeltaResponse{} = response,
             "expected GetSyncDeltaResponse struct, got: #{inspect(response)}"

      assert response.fallback_to_initial == true,
             "expected fallback_to_initial=true when since_token is unknown, got: #{inspect(response.fallback_to_initial)}"

      room_ids = Enum.map(response.rooms, & &1.room_id)

      assert room_id in room_ids,
             "expected #{room_id} in fallback response rooms, got: #{inspect(room_ids)}"

      assert is_binary(response.since_token) and response.since_token != "",
             "expected non-empty since_token in fallback response"
    end
  end

  # ─── Story 9-22 MAJOR-T: get_sync_delta — token mismatch → fallback ──────────
  #
  # AC2 (Story 9-22): when get_since_token returns a stored token that does NOT
  # match the since_token sent by the client, the server returns a full initial
  # sync with fallback_to_initial: true (stale/replayed token detected).
  #
  # Given: SyncDeltaFakePgStore.get_since_token returns {:ok, %{since_token: "stored_v1", …}};
  #        @alice:test.local is a member of !delta_mismatch:test.local with 2 events
  # When:  Server.get_sync_delta/2 called with since_token: "stale_client_token" (different)
  # Then:  response.fallback_to_initial is true;
  #        response.rooms is non-empty (full sync);
  #        response.since_token is a freshly-minted token (not "stored_v1")

  describe "Server.get_sync_delta/2 — client token mismatch falls back to initial sync" do
    test "returns full initial sync with fallback_to_initial: true when stored token differs from client token" do
      alice = "@alice:test.local"
      room_id = "!delta_mismatch:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)
      seed_events(room_id, alice, 2)

      # The server has stored "stored_v1" for this user; the client sends a different token.
      :ets.insert(
        :sync_delta_pg_store_config,
        {:get_since_token_response, {:ok, %{since_token: "stored_v1", last_event_id: nil}}}
      )

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "stale_client_token",
        timeout_ms: 0
      }

      response = Server.get_sync_delta(request, build_stream())

      assert %Core.GetSyncDeltaResponse{} = response,
             "expected GetSyncDeltaResponse struct, got: #{inspect(response)}"

      assert response.fallback_to_initial == true,
             "expected fallback_to_initial=true on token mismatch, got: #{inspect(response.fallback_to_initial)}"

      room_ids = Enum.map(response.rooms, & &1.room_id)

      assert room_id in room_ids,
             "expected #{room_id} in fallback response rooms (full sync), got: #{inspect(room_ids)}"

      assert is_binary(response.since_token) and response.since_token != "",
             "expected non-empty since_token in fallback response"

      assert response.since_token != "stored_v1",
             "expected a freshly-minted since_token, not the stored value"
    end
  end

  # ─── Story 4-15 AT #4: :pg cleanup after handler exits ────────────────────────
  #
  # AC #13 (Story 4-15): :pg cleanup — after get_sync_delta handler exits, the
  # process is no longer in any room :pg group.
  #
  # The handler subscribes to :pg groups for room monitoring BEFORE checking for
  # pending events (prevents missed-event race). After the handler exits (normally
  # or via timeout), it must leave the :pg groups so the membership is clean.
  #
  # :pg auto-removes dead processes, so we verify membership is gone after a
  # normal return (which exits the handler process).
  #
  # Given: @alice:test.local is a member of !delta4:test.local;
  #        no new events (forces long-poll path → timeout fires);
  #        timeout_ms: 100 (short for test speed)
  # When:  Server.get_sync_delta/2 returns (timeout)
  # Then:  :pg.get_members("room:!delta4:test.local") is [] (or does not contain
  #        any dead PIDs)

  describe "Server.get_sync_delta/2 — :pg group membership cleaned up on exit" do
    test ":pg group contains no dead PIDs after handler returns" do
      alice = "@alice:test.local"
      room_id = "!delta4:test.local"
      pg_group = "room:#{room_id}"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # No events seeded — handler will enter long-poll path and timeout
      :ets.insert(:sync_delta_pg_store_config, {:get_since_token_response, {:ok, %{since_token: "s_pg_test", last_event_id: nil}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_pg_test",
        timeout_ms: 100
      }

      # The call is synchronous — when it returns, the handler process has exited
      _response = Server.get_sync_delta(request, build_stream())

      # After the handler returns, no live PIDs should remain in the :pg group.
      # :pg.get_members/1 returns only live PIDs (auto-cleaned by OTP).
      members =
        try do
          :pg.get_members(:pg, pg_group)
        rescue
          _ -> []
        catch
          _, _ -> []
        end

      # Filter to only live PIDs (belt-and-suspenders, :pg should do this automatically)
      live_members = Enum.filter(members, &Process.alive?/1)

      assert live_members == [],
             "expected no live PIDs in :pg group #{pg_group} after handler returns, got: #{inspect(live_members)}"
    end
  end

  # ─── Story 4-15 AT #10: next_batch is always a new token (nil last_event_id) ──
  #
  # AC #10 (Story 4-15): the response since_token must always be a freshly
  # generated token — never an echo of the incoming since_token — even when the
  # user has 0 rooms and last_event_id is nil (no events ever stored).
  #
  # This ensures a client that calls GET /sync?since=<stale_token> with no rooms
  # will receive a new, advanceable token so it can make progress.
  #
  # Given: user has 0 rooms; SyncDeltaFakePgStore.get_since_token returns
  #        {:ok, %{since_token: "s_old_token", last_event_id: nil}}
  # When:  Server.get_sync_delta/2 is called with since_token: "s_old_token",
  #        timeout_ms: 0
  # Then:  response.since_token != "s_old_token" (always a new token);
  #        response.since_token is a non-empty binary

  describe "Server.get_sync_delta/2 — next_batch is always a new token (AT #10)" do
    test "next_batch differs from since_token even when no events and last_event_id is nil" do
      alice = "@alice_freshtoken:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      # User has 0 rooms — no memberships seeded in FakeDB
      # pg_store returns a valid token but with nil last_event_id (no events ever)
      :ets.insert(:sync_delta_pg_store_config, {:get_since_token_response, {:ok, %{since_token: "s_old_token", last_event_id: nil}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_old_token",
        timeout_ms: 0
      }

      response = Server.get_sync_delta(request, build_stream())

      assert %Core.GetSyncDeltaResponse{} = response,
             "expected GetSyncDeltaResponse struct, got: #{inspect(response)}"

      # The response since_token must always be a NEW token — not an echo of the input
      assert response.since_token != "s_old_token",
             "response.since_token must differ from the incoming since_token, got: #{inspect(response.since_token)}"

      assert is_binary(response.since_token) and response.since_token != "",
             "expected non-empty binary since_token, got: #{inspect(response.since_token)}"
    end
  end

  # ─── Story 8-10a AC4b: do_incremental_sync wakes on {:new_invite, _} ─────────
  #
  # Bug 4-29f fix: the sync long-poll must wake immediately when a new invite
  # arrives, instead of sleeping the full 30 s.
  #
  # Mechanism: do_incremental_sync subscribes to :pg group "user:#{user_id}" BEFORE
  # entering the receive loop. invite_user/2 sends {:new_invite, room_id} to that
  # group. The receive block handles {:new_invite, _} by cancelling the timer and
  # returning an empty delta (Go gateway's buildInviteRooms fetches the invite data).
  #
  # Given: alice has no pending events (long-poll would normally wait timeout_ms)
  # When:  get_sync_delta is called with timeout_ms: 500
  #        AND after 50 ms an external process sends {:new_invite, room_id} to "user:alice"
  # Then:  the handler returns within ~200 ms (much less than 500 ms)
  #        AND response.rooms == [] (empty delta — invite data fetched by Go gateway)

  describe "Server.get_sync_delta/2 — wakes on {:new_invite, _} (Bug 4-29f)" do
    test "long-poll returns early when {:new_invite, room_id} is broadcast to user :pg group" do
      alice = "@alice_invite_wake:test.local"
      room_id = "!invite-wake:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      # alice has no rooms and no events → long-poll path is taken.
      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response, {:ok, %{since_token: "s_invite_wake", last_event_id: nil}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_invite_wake",
        timeout_ms: 500
      }

      # Spawn sender: polls until get_sync_delta's internal Task has joined the user
      # :pg group, then broadcasts {:new_invite, room_id} to all members.
      # get_sync_delta runs do_incremental_sync in a Task.async, so the :pg member is
      # that Task process — not the test process.  Polling instead of a fixed sleep
      # prevents the race where the message is sent before :pg.join/2 executes.
      Task.start(fn ->
        deadline = System.monotonic_time(:millisecond) + 400
        wait = fn wait ->
          members = :pg.get_local_members("user:#{alice}")
          if members != [] do
            Enum.each(members, &send(&1, {:new_invite, room_id}))
          else
            if System.monotonic_time(:millisecond) >= deadline do
              :noop
            else
              Process.sleep(5)
              wait.(wait)
            end
          end
        end
        wait.(wait)
      end)

      start_ms = System.monotonic_time(:millisecond)
      response = Server.get_sync_delta(request, build_stream())
      elapsed_ms = System.monotonic_time(:millisecond) - start_ms

      # Must return well before the 500 ms timeout — invite woke the long-poll.
      assert elapsed_ms < 450,
             "Expected early return on {:new_invite, _} but took #{elapsed_ms} ms (timeout was 500 ms)"

      # Invite wakeup returns empty rooms — Go gateway fetches invite data separately.
      assert response.rooms == [],
             "Expected empty rooms on {:new_invite, _} wakeup, got: #{inspect(response.rooms)}"

      assert %Core.GetSyncDeltaResponse{} = response
    end
  end

  # ═══════════════════════════════════════════════════════════════════════════════
  # Story 9-20: GAP-PREV-BATCH — prev_batch correctness in fetch_delta_rooms
  # ═══════════════════════════════════════════════════════════════════════════════
  #
  # These tests FAIL before the story 9-20 fix because fetch_delta_rooms always
  # sets prev_batch: "" even when limited: true.
  #
  # AC1: limited:true (≥20 events) → prev_batch == event_id of oldest event in batch
  # AC2: limited:false (<20 events) → prev_batch == ""  (regression guard)
  # AC3: no new events → room NOT included in response (existing behaviour guard)

  describe "Server.get_sync_delta/2 — prev_batch correctness (Story 9-20)" do
    test "AC1: sets prev_batch to oldest event_id when limited: true (20 events)" do
      alice = "@alice:test.local"
      room_id = "!prevbatch1:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$pb_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # Seed exactly 20 new events — triggers limited: true
      for idx <- 1..20 do
        SyncDeltaFakeDB.insert_event(%{
          "event_id" => "$pb_new#{idx}",
          "room_id" => room_id,
          "sender" => alice,
          "event_type" => "m.room.message",
          "content" => %{"msgtype" => "m.text", "body" => "msg #{idx}"},
          "origin_server_ts" => 1_700_000_001_000 + idx * 1000
        })
      end

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_pb_anchor", last_event_id: "$pb_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_pb_anchor",
        timeout_ms: 5000
      }

      response = Server.get_sync_delta(request, build_stream())
      room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      assert room != nil, "expected room #{room_id} in response"

      assert room.limited == true,
             "expected limited=true when 20 events returned, got: #{inspect(room.limited)}"

      # $pb_new1 has the lowest origin_server_ts among the 20 new events —
      # it is the correct backward-pagination anchor (Spec §6.3.3).
      assert room.prev_batch == "$pb_new1",
             "expected prev_batch=$pb_new1 (oldest event in batch), got: #{inspect(room.prev_batch)}"
    end

    test "AC2: sets prev_batch to empty string when limited: false (fewer than 20 events)" do
      alice = "@alice:test.local"
      room_id = "!prevbatch2:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$pb2_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # Seed 5 new events — not enough to trigger limited: true
      for idx <- 1..5 do
        SyncDeltaFakeDB.insert_event(%{
          "event_id" => "$pb2_ev#{idx}",
          "room_id" => room_id,
          "sender" => alice,
          "event_type" => "m.room.message",
          "content" => %{"msgtype" => "m.text", "body" => "msg #{idx}"},
          "origin_server_ts" => 1_700_000_001_000 + idx * 1000
        })
      end

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_pb2_anchor", last_event_id: "$pb2_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_pb2_anchor",
        timeout_ms: 5000
      }

      response = Server.get_sync_delta(request, build_stream())
      room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      assert room != nil, "expected room #{room_id} in response"

      assert room.limited == false,
             "expected limited=false when 5 events returned, got: #{inspect(room.limited)}"

      assert room.prev_batch == "",
             "expected prev_batch=\"\" when not limited, got: #{inspect(room.prev_batch)}"
    end

    test "AC3: room not included in response when no new events (empty list guard)" do
      alice = "@alice:test.local"
      room_id = "!prevbatch3:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # Only the anchor event — no newer events exist
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$pb3_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_pb3_anchor", last_event_id: "$pb3_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_pb3_anchor",
        timeout_ms: 100
      }

      response = Server.get_sync_delta(request, build_stream())
      room_ids = Enum.map(response.rooms, & &1.room_id)

      refute room_id in room_ids,
             "expected room #{room_id} NOT in response when no new events"
    end
  end

  # ═══════════════════════════════════════════════════════════════════════════════
  # Story 9-21: GAP-STATE-POSITION — state.events must not duplicate timeline state
  # ═══════════════════════════════════════════════════════════════════════════════
  #
  # Matrix Spec §6.3.3: state.events = room state BEFORE the timeline window.
  # Any {type, state_key} pair in timeline_events must NOT appear in state_events.
  #
  # These tests FAIL before the story 9-21 fix because build_state_events returns
  # the current (post-timeline) state — the same state event appears in both
  # state_events and timeline_events, causing the SDK to compute "no change".
  #
  # AC1: m.room.power_levels in timeline → NOT in state_events
  # AC2: m.room.name in timeline → NOT in state_events
  # AC3: unrelated state events (m.room.create) are KEPT in state_events
  # AC4: m.room.member dedup regression guard (still excluded via existing path)

  describe "Server.get_sync_delta/2 — state.events position correctness (Story 9-21)" do
    test "AC1: excludes m.room.power_levels from state when it appears in timeline" do
      alice = "@alice:test.local"
      room_id = "!statepos1:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # Anchor event (last_event_id reference point)
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp1_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # New power_levels state event in the timeline
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp1_pl_change",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.power_levels",
        "state_key" => "",
        "content" => %{"users_default" => 0},
        "origin_server_ts" => 1_700_000_001_000
      })

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_sp1_anchor", last_event_id: "$sp1_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_sp1_anchor",
        timeout_ms: 5000
      }

      response = Server.get_sync_delta(request, build_stream())
      room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      assert room != nil, "expected room #{room_id} in response"

      pl_in_timeline =
        Enum.any?(room.timeline_events, fn ev -> ev.event_type == "m.room.power_levels" end)

      assert pl_in_timeline, "expected m.room.power_levels in timeline_events (precondition)"

      pl_in_state =
        Enum.any?(room.state_events, fn ev -> ev.type == "m.room.power_levels" end)

      refute pl_in_state,
             "expected m.room.power_levels NOT in state_events when it appears in timeline (Spec §6.3.3)"
    end

    test "AC2: excludes m.room.name from state when it appears in timeline" do
      alice = "@alice:test.local"
      room_id = "!statepos2:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # A pre-existing name event — stored with "type" key so get_room_name/1 finds it,
      # but WITHOUT "event_type" so fetch_events_since/3 ignores it (not a timeline event).
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp2_name_old",
        "room_id" => room_id,
        "type" => "m.room.name",
        "content" => %{"name" => "Old Room Name"},
        "origin_server_ts" => 1_699_000_000_000
      })

      # Anchor event
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp2_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # Name change in timeline — has "event_type" AND "state_key" to mark it as a state event
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp2_name_change",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.name",
        "state_key" => "",
        "content" => %{"name" => "New Room Name"},
        "origin_server_ts" => 1_700_000_001_000
      })

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_sp2_anchor", last_event_id: "$sp2_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_sp2_anchor",
        timeout_ms: 5000
      }

      response = Server.get_sync_delta(request, build_stream())
      room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      assert room != nil, "expected room #{room_id} in response"

      name_in_state =
        Enum.any?(room.state_events, fn ev -> ev.type == "m.room.name" end)

      refute name_in_state,
             "expected m.room.name NOT in state_events when it appears in timeline (Spec §6.3.3)"
    end

    test "AC3: keeps unrelated state events in state when they do not appear in timeline" do
      alice = "@alice:test.local"
      room_id = "!statepos3:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp3_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # Only a power_levels state event in the timeline — m.room.create is unrelated
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp3_pl_change",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.power_levels",
        "state_key" => "",
        "content" => %{"users_default" => 0},
        "origin_server_ts" => 1_700_000_001_000
      })

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_sp3_anchor", last_event_id: "$sp3_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_sp3_anchor",
        timeout_ms: 5000
      }

      response = Server.get_sync_delta(request, build_stream())
      room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      assert room != nil, "expected room #{room_id} in response"

      create_in_state =
        Enum.any?(room.state_events, fn ev -> ev.type == "m.room.create" end)

      assert create_in_state,
             "expected m.room.create still in state_events (unrelated to power_levels timeline change)"

      pl_in_state =
        Enum.any?(room.state_events, fn ev -> ev.type == "m.room.power_levels" end)

      refute pl_in_state,
             "expected m.room.power_levels NOT in state_events (it changed in timeline)"
    end

    test "AC4: m.room.member dedup regression — member in timeline excluded from state" do
      alice = "@alice:test.local"
      bob = "@bob:test.local"
      room_id = "!statepos4:test.local"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      # Both alice (existing) and bob (newly joined) are members
      :ok = setup_room_with_member(room_id, alice)
      :ok = Nebu.Room.Server.join(room_id, bob)
      :ets.insert(:sync_test_db, {{:member_for_user, bob, room_id}, :active})

      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp4_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # Bob's join event in the timeline
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$sp4_bob_join",
        "room_id" => room_id,
        "sender" => bob,
        "event_type" => "m.room.member",
        "state_key" => bob,
        "content" => %{"membership" => "join"},
        "origin_server_ts" => 1_700_000_001_000
      })

      :ets.insert(:sync_delta_pg_store_config,
        {:get_since_token_response,
         {:ok, %{since_token: "s_sp4_anchor", last_event_id: "$sp4_anchor"}}})

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_sp4_anchor",
        timeout_ms: 5000
      }

      response = Server.get_sync_delta(request, build_stream())
      room = Enum.find(response.rooms, fn r -> r.room_id == room_id end)

      assert room != nil, "expected room #{room_id} in response"

      bob_in_state =
        Enum.any?(room.state_events, fn ev ->
          ev.type == "m.room.member" and ev.state_key == bob
        end)

      refute bob_in_state,
             "expected bob's m.room.member NOT in state_events when bob's join appears in timeline"

      alice_in_state =
        Enum.any?(room.state_events, fn ev ->
          ev.type == "m.room.member" and ev.state_key == alice
        end)

      assert alice_in_state,
             "expected alice's m.room.member still in state_events (alice not in timeline)"
    end
  end

  # ─── Story 9-22 MINOR-3: incremental sync with device_id calls persist_since_token/4 ──
  #
  # AC1 (Story 9-22): when get_sync_delta runs a successful incremental sync with
  # device_id != "", it must call persist_since_token/4 (user_id, device_id,
  # new_token, last_event_id), NOT the 3-arity legacy form.
  #
  # Given: SyncDeltaFakePgStore returns a matching token for (user_id, device_id);
  #        @alice:test.local is in !delta_device:test.local with 1 new event;
  #        request.device_id = "DEVICE_D1"
  # When:  Server.get_sync_delta/2 is called
  # Then:  SyncDeltaFakePgStore.recorded_calls() contains a 4-tuple
  #        {user_id, "DEVICE_D1", new_token, last_event_id} — not a 3-tuple

  describe "Server.get_sync_delta/2 — incremental sync with device_id calls persist_since_token/4 (MINOR-3)" do
    test "calls persist_since_token/4 with (user_id, device_id, token, event_id) when device_id != \"\"" do
      alice = "@alice:test.local"
      room_id = "!delta_device:test.local"
      device_id = "DEVICE_D1"

      :ets.new(:sync_delta_pg_store_config, [:named_table, :public, :set])

      on_exit(fn ->
        if :ets.info(:sync_delta_pg_store_config) != :undefined do
          :ets.delete(:sync_delta_pg_store_config)
        end
      end)

      Application.put_env(:event_dispatcher, :pg_store_module, SyncDeltaFakePgStore)
      Application.put_env(:event_dispatcher, :messages_db_module, SyncDeltaFakeDB)
      Application.put_env(:event_dispatcher, :rooms_db_module, SyncDeltaFakeDB)

      on_exit(fn ->
        Application.delete_env(:event_dispatcher, :pg_store_module)
        Application.delete_env(:event_dispatcher, :messages_db_module)
        Application.delete_env(:event_dispatcher, :rooms_db_module)
      end)

      :ok = setup_room_with_member(room_id, alice)

      # Anchor event (= last_event_id)
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$dev_anchor",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "anchor"},
        "origin_server_ts" => 1_700_000_000_000
      })

      # New event after the anchor
      SyncDeltaFakeDB.insert_event(%{
        "event_id" => "$dev_new",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => "new"},
        "origin_server_ts" => 1_700_000_001_000
      })

      # Configure stored token to match what the client will send (valid incremental path)
      :ets.insert(
        :sync_delta_pg_store_config,
        {:get_since_token_response, {:ok, %{since_token: "s_dev_anchor", last_event_id: "$dev_anchor"}}}
      )

      request = %Core.GetSyncDeltaRequest{
        user_id: alice,
        since_token: "s_dev_anchor",
        timeout_ms: 5000,
        device_id: device_id
      }

      _response = Server.get_sync_delta(request, build_stream())

      calls = SyncDeltaFakePgStore.recorded_calls()

      # There must be at least one 4-arity call with the correct device_id
      four_arity_calls =
        Enum.filter(calls, fn
          {^alice, ^device_id, _token, _event_id} -> true
          _ -> false
        end)

      assert length(four_arity_calls) >= 1,
             "expected at least one 4-arity persist_since_token call with device_id=#{device_id}, got: #{inspect(calls)}"

      # There must be NO 3-arity call for this user when device_id != ""
      three_arity_calls =
        Enum.filter(calls, fn
          {^alice, _token, _event_id} -> true
          _ -> false
        end)

      assert three_arity_calls == [],
             "expected no 3-arity persist_since_token call when device_id is set, got: #{inspect(three_arity_calls)}"
    end
  end
end
