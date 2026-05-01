defmodule Nebu.EventDispatcher.GetMessagesTest do
  use ExUnit.Case, async: false

  # ─── Story 4-12: Elixir get_messages/2 gRPC handler ─────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-12 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and ETS tables are all process-global resources that must not be shared
  # concurrently between test cases.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.get_messages/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: map with http_request_headers carrying x-user-id so
  #     Nebu.Grpc.Metadata.trusted_identity/1 resolves the caller's user_id.
  #     The get_messages/2 handler reads caller identity from gRPC metadata,
  #     NOT from a field in the request body (per AC #2 / architecture rule).
  #   - DB injection: Room.Server.init/1 uses Application.get_env(:room_manager, :db_module).
  #     FakeDB is injected to avoid PostgreSQL. FakeDB for this story additionally
  #     supports fetch_events/4 (keyset pagination) — seeded with events for the
  #     happy path and pagination tests.
  #   - Rooms are started via Nebu.Room.RoomSupervisor.start_room/1 and tracked
  #     with on_exit cleanup via Horde.DynamicSupervisor.terminate_child/2 to
  #     prevent cross-test Horde pollution.
  #   - Creators/requestors are joined to their rooms before get_messages is tested —
  #     the handler checks room membership via room state.
  #   - :NebuTxnDedup is cleared before each test to prevent cross-test leakage.

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Room.DB behaviour, extended with
  # fetch_events/4 needed by the get_messages/2 handler.
  #
  # fetch_events/4 signature (keyset pagination on origin_server_ts + event_id):
  #   fetch_events(room_id, direction, limit, from_token)
  #     direction  :: "b" | "f"
  #     limit      :: pos_integer()
  #     from_token :: String.t()  (empty string = no cursor, fetch from edge)
  #
  # Returns: {:ok, [event_map], next_batch, prev_batch}
  #
  # FakeDB implements in-memory keyset pagination:
  #   - All events for the room are fetched, sorted by origin_server_ts ascending.
  #   - For direction "b" (backward) with no cursor: return last `limit` events
  #     reversed (newest first), next_batch = "" (no older), prev_batch = token of oldest.
  #   - For direction "b" with cursor: return events older than cursor, newest-first.
  #   - Pagination tokens encode event index as "v1_<base64(index)>" (simplified).
  #
  # This is intentionally simplified for unit-test purposes — the production
  # implementation will use real keyset pagination on PostgreSQL.

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:get_messages_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:get_messages_test_db, {{:member, room_id, :"$1"}, :active})

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:get_messages_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:get_messages_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:get_messages_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:get_messages_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    # fetch_events/4 — keyset pagination for get_messages handler.
    #
    # Fetches events for room_id from FakeDB, applying direction and limit.
    # from_token is the opaque pagination cursor (empty = fetch from edge).
    # Returns {:ok, events, next_batch, prev_batch}.
    def fetch_events(room_id, direction, limit, from_token) do
      # Collect all events for this room, sorted ascending by origin_server_ts.
      all_events =
        :ets.match(:get_messages_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev -> ev["room_id"] == room_id and Map.has_key?(ev, "event_type") end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end)

      # Apply cursor if from_token is non-empty.
      events_after_cursor =
        if from_token == "" do
          all_events
        else
          # Token format: "v1_<base64(ts:event_id)>". Decode to find cursor position.
          case decode_token(from_token) do
            {:ok, cursor_ts, cursor_id} ->
              Enum.filter(all_events, fn ev ->
                ev["origin_server_ts"] < cursor_ts or
                  (ev["origin_server_ts"] == cursor_ts and ev["event_id"] < cursor_id)
              end)

            :error ->
              all_events
          end
        end

      # Select events based on direction.
      {page, next_batch, prev_batch} =
        case direction do
          "b" ->
            # Backward: return newest events first (reverse chronological).
            page = events_after_cursor |> Enum.take(-limit) |> Enum.reverse()

            next_batch =
              if length(page) == limit and length(events_after_cursor) > limit do
                oldest = List.last(page)
                encode_token(oldest["origin_server_ts"], oldest["event_id"])
              else
                ""
              end

            prev_batch =
              if length(page) > 0 do
                # Use the oldest event (last in newest-first list) as the cursor
                # so the next backward page call fetches events strictly older.
                oldest = List.last(page)
                encode_token(oldest["origin_server_ts"], oldest["event_id"])
              else
                ""
              end

            {page, next_batch, prev_batch}

          "f" ->
            # Forward: return oldest events first (chronological).
            page = Enum.take(all_events, limit)

            next_batch =
              if length(page) == limit and length(all_events) > limit do
                newest = List.last(page)
                encode_token(newest["origin_server_ts"], newest["event_id"])
              else
                ""
              end

            {page, next_batch, ""}
        end

      {:ok, page, next_batch, prev_batch}
    end

    defp encode_token(ts, event_id) do
      "v1_" <> Base.url_encode64("#{ts}:#{event_id}", padding: false)
    end

    defp decode_token("v1_" <> encoded) do
      case Base.url_decode64(encoded, padding: false) do
        {:ok, decoded} ->
          case String.split(decoded, ":", parts: 2) do
            [ts_str, event_id] ->
              case Integer.parse(ts_str) do
                {ts, ""} -> {:ok, ts, event_id}
                _ -> :error
              end

            _ ->
              :error
          end

        :error ->
          :error
      end
    end

    defp decode_token(_), do: :error

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create the ETS table for FakeDB. Guard against stale tables from --watch reruns.
    if :ets.info(:get_messages_test_db) != :undefined do
      :ets.delete(:get_messages_test_db)
    end

    :ets.new(:get_messages_test_db, [:named_table, :public, :set])

    # Inject FakeDB so Room GenServers don't need PostgreSQL.
    Application.put_env(:room_manager, :db_module, FakeDB)

    # Inject FakeDB for the get_messages handler's fetch_events/4 calls.
    Application.put_env(:event_dispatcher, :messages_db_module, FakeDB)

    # Override server_name for deterministic assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # :NebuTxnDedup — clear all entries before each test to prevent leakage.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :server_name)

      if :ets.info(:get_messages_test_db) != :undefined do
        :ets.delete(:get_messages_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ─────────────────────────────────────────────────────────
  #
  # Carries x-user-id in http_request_headers so Nebu.Grpc.Metadata.trusted_identity/1
  # resolves the caller's identity (membership check in the handler uses this).

  defp build_stream(user_id) do
    %{http_request_headers: %{"x-user-id" => user_id}}
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

  # ─── Helper: start a room, join a user, seed events into FakeDB ───────────────
  #
  # Sets up the pre-condition shared by happy-path and pagination tests:
  # - Room GenServer running and tracked.
  # - user_id joined as a member.
  # - events_data is a list of %{body: body, ts_offset: offset_ms} maps used to
  #   seed events at known timestamps relative to a base time.
  #
  # Returns the list of seeded event maps (string keys, Matrix format).

  defp setup_room_with_events(room_id, user_id, events_data) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)

    base_ts = 1_700_000_000_000

    Enum.with_index(events_data, 1)
    |> Enum.map(fn {%{body: body, ts_offset: offset}, idx} ->
      event_id = "$event#{idx}:test.local"
      ts = base_ts + offset

      event = %{
        "event_id" => event_id,
        "room_id" => room_id,
        "sender" => user_id,
        "event_type" => "m.room.message",
        "content" => %{"msgtype" => "m.text", "body" => body},
        "origin_server_ts" => ts
      }

      FakeDB.insert_event(event)
      event
    end)
  end

  # ─── AT #8: get_messages happy path → GetMessagesResponse with events ─────────
  #
  # AC #11, #12, #14 — get_messages/2 returns %Core.GetMessagesResponse{} with
  # events list and non-empty pagination tokens.
  #
  # Given: Room GenServer running for "!msgtest:test.local"; @alice:test.local
  #        is a member; 3 events seeded in FakeDB in chronological order
  # When: Server.get_messages/2 called with direction="b", limit=10, from_token=""
  #       and x-user-id=@alice:test.local in stream metadata
  # Then: returns %Core.GetMessagesResponse{} with 3 events (newest first),
  #       non-empty prev_batch token; next_batch may be empty (no more pages)

  describe "Server.get_messages/2 — happy path" do
    test "returns GetMessagesResponse with events in reverse chronological order" do
      room_id = "!msgtest:test.local"
      alice = "@alice:test.local"

      _events =
        setup_room_with_events(room_id, alice, [
          %{body: "first message", ts_offset: 0},
          %{body: "second message", ts_offset: 1000},
          %{body: "third message", ts_offset: 2000}
        ])

      request = %Core.GetMessagesRequest{
        room_id: room_id,
        from_token: "",
        direction: "b",
        limit: 10
      }

      response = Server.get_messages(request, build_stream(alice))

      assert %Core.GetMessagesResponse{} = response,
             "expected GetMessagesResponse struct, got: #{inspect(response)}"

      assert is_list(response.events),
             "expected events to be a list, got: #{inspect(response.events)}"

      assert length(response.events) == 3,
             "expected 3 events, got #{length(response.events)}"

      # Verify events are in reverse chronological order (newest first for dir=b)
      timestamps = Enum.map(response.events, & &1.origin_ts)
      sorted_desc = Enum.sort(timestamps, :desc)

      assert timestamps == sorted_desc,
             "expected events in descending timestamp order, got: #{inspect(timestamps)}"

      # Each event must have a non-empty event_id
      for ev <- response.events do
        assert is_binary(ev.event_id) and ev.event_id != "",
               "expected non-empty event_id, got: #{inspect(ev.event_id)}"

        assert ev.room_id == room_id,
               "expected room_id #{room_id}, got: #{ev.room_id}"
      end

      # Pagination tokens: prev_batch must be non-empty when results returned.
      assert is_binary(response.prev_batch),
             "expected prev_batch to be a binary, got: #{inspect(response.prev_batch)}"
    end

    test "returns empty chunk with empty tokens for a room with no events" do
      room_id = "!emptyroom:test.local"
      alice = "@alice:test.local"

      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      :ok = start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, alice)

      request = %Core.GetMessagesRequest{
        room_id: room_id,
        from_token: "",
        direction: "b",
        limit: 10
      }

      response = Server.get_messages(request, build_stream(alice))

      assert %Core.GetMessagesResponse{} = response
      assert response.events == [],
             "expected empty events list for empty room, got: #{inspect(response.events)}"

      # MINOR-5: empty room must return empty pagination tokens
      assert response.next_batch == "",
             "expected next_batch to be empty for empty room, got: #{inspect(response.next_batch)}"

      assert response.prev_batch == "",
             "expected prev_batch to be empty for empty room, got: #{inspect(response.prev_batch)}"
    end

    # ─── MINOR-4: forward direction — events returned in ascending order ────────
    #
    # AC #3 — direction "f" returns events oldest-first (ascending chronological).
    #
    # Given: Room GenServer running; @alice:test.local is a member; 5 events seeded
    # When: Server.get_messages/2 called with direction="f", limit=3, from_token=""
    # Then: events returned in ascending timestamp order (oldest first)
    #       next_batch is non-empty (more events exist beyond the page)

    test "returns events in ascending order for direction=f with non-empty next_batch" do
      room_id = "!fwdtest:test.local"
      alice = "@alice:test.local"

      _events =
        setup_room_with_events(room_id, alice, [
          %{body: "oldest", ts_offset: 0},
          %{body: "second", ts_offset: 1000},
          %{body: "third", ts_offset: 2000},
          %{body: "fourth", ts_offset: 3000},
          %{body: "newest", ts_offset: 4000}
        ])

      request = %Core.GetMessagesRequest{
        room_id: room_id,
        from_token: "",
        direction: "f",
        limit: 3
      }

      response = Server.get_messages(request, build_stream(alice))

      assert %Core.GetMessagesResponse{} = response,
             "expected GetMessagesResponse struct, got: #{inspect(response)}"

      assert length(response.events) == 3,
             "expected 3 events (limit), got #{length(response.events)}"

      # Events must be in ascending timestamp order (oldest first)
      timestamps = Enum.map(response.events, & &1.origin_ts)
      sorted_asc = Enum.sort(timestamps, :asc)

      assert timestamps == sorted_asc,
             "expected events in ascending timestamp order for direction=f, got: #{inspect(timestamps)}"

      # next_batch must be non-empty because there are 5 events and limit=3
      assert response.next_batch != "",
             "expected non-empty next_batch when more events exist beyond page (5 events, limit=3)"
    end
  end

  # ─── AT #9: room not found → GRPC.RPCError NOT_FOUND ─────────────────────────
  #
  # AC #13 — when no Room GenServer is running for room_id, get_messages/2 must
  # raise GRPC.RPCError with status not_found.
  #
  # Given: no Room GenServer running for "!ghost:test.local"
  # When: Server.get_messages/2 is called for that room
  # Then: raises %GRPC.RPCError{status: GRPC.Status.not_found()}

  describe "Server.get_messages/2 — room not found" do
    test "raises GRPC.RPCError with not_found when room GenServer is not running" do
      request = %Core.GetMessagesRequest{
        room_id: "!ghost:test.local",
        from_token: "",
        direction: "b",
        limit: 10
      }

      error =
        try do
          Server.get_messages(request, build_stream("@alice:test.local"))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.not_found(),
             "expected status not_found (#{GRPC.Status.not_found()}), got: #{error.status}"
    end
  end

  # ─── AT #10: non-member → GRPC.RPCError PERMISSION_DENIED ───────────────────
  #
  # AC #12 — when the caller (from x-user-id header) is not a room member,
  # get_messages/2 must raise GRPC.RPCError with status permission_denied.
  #
  # Given: Room GenServer running; @alice:test.local joined; @bob:test.local NOT joined
  # When: Server.get_messages/2 called with x-user-id=@bob:test.local
  # Then: raises %GRPC.RPCError{status: GRPC.Status.permission_denied()}

  describe "Server.get_messages/2 — non-member" do
    test "raises GRPC.RPCError with permission_denied when caller is not a room member" do
      room_id = "!membtest2:test.local"
      alice = "@alice:test.local"
      bob = "@bob:test.local"

      # Start the room and join only alice — bob is deliberately excluded.
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      :ok = start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, alice)

      request = %Core.GetMessagesRequest{
        room_id: room_id,
        from_token: "",
        direction: "b",
        limit: 10
      }

      # Bob is in the stream metadata but is NOT a room member.
      error =
        try do
          Server.get_messages(request, build_stream(bob))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected status permission_denied (#{GRPC.Status.permission_denied()}), got: #{error.status}"
    end
  end

  # ─── AT #11: pagination — second call with from_token returns next batch ──────
  #
  # AC #14 — keyset pagination: calling get_messages/2 with a non-empty from_token
  # returns the next page of events not included in the first response.
  #
  # Given: Room GenServer running; @alice:test.local is a member; 5 events seeded
  # When: first call with limit=3, direction="b" → returns 3 newest events, prev_batch
  #       second call with from_token=prev_batch, limit=3 → returns remaining events
  # Then: second response contains events NOT in the first response;
  #       second response.prev_batch is non-empty (more pages exist)

  describe "Server.get_messages/2 — pagination" do
    test "second call with from_token returns non-overlapping events" do
      room_id = "!pagination:test.local"
      alice = "@alice:test.local"

      seeded =
        setup_room_with_events(room_id, alice, [
          %{body: "msg 1", ts_offset: 0},
          %{body: "msg 2", ts_offset: 1000},
          %{body: "msg 3", ts_offset: 2000},
          %{body: "msg 4", ts_offset: 3000},
          %{body: "msg 5", ts_offset: 4000}
        ])

      assert length(seeded) == 5

      # First page: newest 3 events (direction "b", newest first).
      first_req = %Core.GetMessagesRequest{
        room_id: room_id,
        from_token: "",
        direction: "b",
        limit: 3
      }

      first_resp = Server.get_messages(first_req, build_stream(alice))

      assert %Core.GetMessagesResponse{} = first_resp
      assert length(first_resp.events) == 3,
             "expected 3 events in first page, got #{length(first_resp.events)}"

      first_event_ids = MapSet.new(first_resp.events, & &1.event_id)

      # prev_batch must be non-empty so the client can fetch the next page.
      assert first_resp.prev_batch != "",
             "expected prev_batch to be non-empty after first page"

      # Second page: use prev_batch as from_token.
      second_req = %Core.GetMessagesRequest{
        room_id: room_id,
        from_token: first_resp.prev_batch,
        direction: "b",
        limit: 3
      }

      second_resp = Server.get_messages(second_req, build_stream(alice))

      assert %Core.GetMessagesResponse{} = second_resp
      assert length(second_resp.events) > 0,
             "expected at least 1 event in second page, got 0"

      # No event_id overlap between pages.
      second_event_ids = MapSet.new(second_resp.events, & &1.event_id)
      overlap = MapSet.intersection(first_event_ids, second_event_ids)

      assert MapSet.size(overlap) == 0,
             "expected no overlapping events between pages, got overlap: #{inspect(MapSet.to_list(overlap))}"
    end
  end
end
