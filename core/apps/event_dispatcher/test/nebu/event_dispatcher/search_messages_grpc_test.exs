defmodule Nebu.EventDispatcher.SearchMessagesGrpcTest do
  use ExUnit.Case, async: false

  # ─── Story 11.3: SearchMessages gRPC Handler — red-phase ATDD tests ───────────
  #
  # ALL tests in this module are expected to FAIL until Story 11.3 is implemented.
  # Failing reasons:
  #   1. Core.SearchMessagesRequest proto message does not exist yet.
  #   2. Core.SearchMessagesResponse proto message does not exist yet.
  #   3. Nebu.EventDispatcher.Server.search_messages/2 does not exist yet.
  #   4. Application.get_env(:event_dispatcher, :search_db_module) injection not wired yet.
  #
  # async: false — Application.put_env is process-global; ETS table shared.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.search_messages/2 directly
  #     (synchronous unary handler — no gRPC network transport involved).
  #   - Fake gRPC stream: minimal map with http_request_headers matching the contract
  #     of Nebu.Grpc.Metadata.trusted_identity/1.
  #   - FakeSearchDB is injected via
  #     Application.put_env(:event_dispatcher, :search_db_module, FakeSearchDB).
  #   - No Horde, no Room GenServer, no PostgreSQL required.
  #   - No :integration tag — pure unit tests using fake DB injection.
  #
  # Acceptance Criteria covered:
  #   AC2 — user_id comes from gRPC metadata, NOT from request.user_id (SECURITY)
  #   AC3 — special chars in search term do not raise
  #   AC4 — missing x-user-id returns GRPC.RPCError UNAUTHENTICATED
  #   AC5 — room_filter is passed through to the DB call (SQL intersection enforced at DB layer)
  #   Additional: DB error returns sanitized GRPC.Status.internal() (no schema detail leak)

  alias Nebu.EventDispatcher.Server

  # ─── FakeSearchDB ─────────────────────────────────────────────────────────────
  #
  # ETS-backed fake for Nebu.Search.DB.
  # Table: :search_db_test (created/destroyed per test in setup/teardown).
  #
  # Captures:
  #   {:last_user_id, user_id}      — records which user_id was passed
  #   {:last_room_filter, list}     — records which room_filter was passed
  #   {:results, rows}              — seeded result rows to return
  #   {:error, reason}              — seed an error to return
  #
  # search_messages/5 signature matches the intended server.ex call:
  #   search_messages(user_id, search_term, limit, offset, room_filter \\ [])

  defmodule FakeSearchDB do
    def search_messages(user_id, _search_term, _limit, _offset, room_filter \\ []) do
      :ets.insert(:search_db_test, {:last_user_id, user_id})
      :ets.insert(:search_db_test, {:last_room_filter, room_filter})

      cond do
        :ets.lookup(:search_db_test, :error) != [] ->
          [{:error, reason}] = :ets.lookup(:search_db_test, :error)
          {:error, reason}

        :ets.lookup(:search_db_test, :results) != [] ->
          [{:results, rows}] = :ets.lookup(:search_db_test, :results)
          {:ok, rows}

        true ->
          {:ok, []}
      end
    end
  end

  # ─── Setup / Teardown ─────────────────────────────────────────────────────────

  setup do
    :ets.new(:search_db_test, [:named_table, :public, :set])

    Application.put_env(:event_dispatcher, :search_db_module, FakeSearchDB)

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :search_db_module)

      if :ets.whereis(:search_db_test) != :undefined do
        :ets.delete(:search_db_test)
      end
    end)

    :ok
  end

  # ─── Helpers ─────────────────────────────────────────────────────────────────

  # Builds a fake gRPC stream with x-user-id in metadata.
  # Matches the contract of Nebu.Grpc.Metadata.trusted_identity/1.
  defp build_stream(user_id) do
    %{http_request_headers: %{"x-user-id" => user_id}}
  end

  # Builds a fake gRPC stream with NO x-user-id (unauthenticated).
  defp build_stream_unauthenticated do
    %{http_request_headers: %{}}
  end

  # ─── Acceptance Tests ────────────────────────────────────────────────────────

  # Test 1 — AC2 / SECURITY: handler uses user_id from metadata, ignores request.user_id
  #
  # This is the PRIMARY security regression test for the trust boundary.
  # If the handler reads user_id from request.user_id instead of metadata,
  # a malicious authenticated user can search any other user's rooms by setting
  # that field — bypassing all membership enforcement.
  #
  # RED phase: fails because search_messages/2 does not exist yet.
  test "AC2 — user_id comes from metadata, not request field (security regression)" do
    stream = build_stream("@alice:test.local")

    # request.user_id is attacker-controlled and MUST be ignored by the handler.
    request = %Core.SearchMessagesRequest{
      user_id:     "@mallory:test.local",
      search_term: "hello",
      limit:       10
    }

    Server.search_messages(request, stream)

    # The handler MUST call FakeSearchDB with "@alice:test.local" (from metadata),
    # NEVER with "@mallory:test.local" (from request body).
    assert [{:last_user_id, actual_user_id}] = :ets.lookup(:search_db_test, :last_user_id)
    assert actual_user_id == "@alice:test.local",
           "Handler used request.user_id ('#{actual_user_id}') instead of metadata user_id ('@alice:test.local')"
  end

  # Test 2 — AC4: missing x-user-id header returns GRPC.RPCError UNAUTHENTICATED
  #
  # RED phase: fails because search_messages/2 does not exist yet.
  test "AC4 — unauthenticated request returns GRPC.RPCError UNAUTHENTICATED" do
    stream = build_stream_unauthenticated()

    request = %Core.SearchMessagesRequest{
      search_term: "hello",
      limit:       10
    }

    assert_raise GRPC.RPCError, fn ->
      Server.search_messages(request, stream)
    end

    # Verify the specific status code is UNAUTHENTICATED.
    err =
      try do
        Server.search_messages(request, stream)
        :no_error
      rescue
        e in GRPC.RPCError -> e
      end

    assert err != :no_error, "Expected GRPC.RPCError to be raised"
    assert err.status == GRPC.Status.unauthenticated(),
           "Expected UNAUTHENTICATED (#{GRPC.Status.unauthenticated()}), got #{err.status}"
  end

  # Test 3 — AC2 happy-path: handler returns SearchMessagesResponse with results
  #
  # RED phase: fails because search_messages/2 + Core.SearchMessagesResponse do not exist yet.
  test "AC2/happy-path — returns SearchMessagesResponse with results from DB" do
    stream = build_stream("@alice:test.local")

    seeded_row = %{
      "event_id"         => "$ev1:test.local",
      "room_id"          => "!room1:test.local",
      "sender"           => "@alice:test.local",
      "event_type"       => "m.room.message",
      "content"          => %{"msgtype" => "m.text", "body" => "hello world"},
      "origin_server_ts" => 1_000_000,
      "rank"             => 0.9
    }
    :ets.insert(:search_db_test, {:results, [seeded_row]})

    request = %Core.SearchMessagesRequest{
      search_term: "hello",
      limit:       10
    }

    response = Server.search_messages(request, stream)

    assert %Core.SearchMessagesResponse{} = response,
           "Expected SearchMessagesResponse struct, got: #{inspect(response)}"

    assert is_list(response.results),
           "Expected response.results to be a list"

    assert length(response.results) == 1,
           "Expected 1 result, got #{length(response.results)}"

    [result] = response.results
    assert %Core.SearchResult{} = result,
           "Expected SearchResult struct in results"

    assert result.rank == 0.9 or abs(result.rank - 0.9) < 0.001,
           "Expected rank ≈ 0.9, got #{result.rank}"

    assert is_binary(result.event),
           "Expected result.event to be a binary (raw JSON)"

    assert is_list(result.events_before),
           "Expected events_before to be a list (may be empty for MVP)"

    assert is_list(result.events_after),
           "Expected events_after to be a list (may be empty for MVP)"

    assert is_map(result.profile_info),
           "Expected profile_info to be a map (may be empty for MVP)"
  end

  # Test 4 — AC5: room_filter in request is passed through to the DB call
  #
  # The intersection with membership is enforced at SQL layer (Nebu.Search.DB).
  # The handler must not silently drop the room_filter.
  #
  # RED phase: fails because search_messages/2 does not exist yet.
  test "AC5 — room_filter is passed through to DB call" do
    stream = build_stream("@bob:test.local")

    room_filter = ["!roomA:test.local", "!roomB:test.local"]

    request = %Core.SearchMessagesRequest{
      search_term: "hello",
      room_filter: room_filter,
      limit:       10
    }

    Server.search_messages(request, stream)

    assert [{:last_room_filter, actual_filter}] =
             :ets.lookup(:search_db_test, :last_room_filter),
           "FakeSearchDB was not called (room_filter not recorded)"

    assert actual_filter == room_filter,
           "Expected room_filter #{inspect(room_filter)}, got #{inspect(actual_filter)}"
  end

  # Test 5 — AC3: special chars in search term do not raise
  #
  # websearch_to_tsquery in PostgreSQL handles these safely.
  # The handler must not transform or reject special characters.
  #
  # RED phase: fails because search_messages/2 does not exist yet.
  test "AC3 — special chars in search term do not raise (websearch_to_tsquery safety)" do
    stream = build_stream("@carol:test.local")

    # FakeSearchDB returns empty list for any term — simulating websearch_to_tsquery behaviour.
    request = %Core.SearchMessagesRequest{
      search_term: "hello' & ): world:",
      limit:       10
    }

    # Must not raise — websearch_to_tsquery handles special chars gracefully.
    response = Server.search_messages(request, stream)

    assert %Core.SearchMessagesResponse{} = response,
           "Expected SearchMessagesResponse, got: #{inspect(response)}"

    assert response.results == [] or is_list(response.results),
           "Expected empty results list for no-match query"
  end

  # Test 6 — DB error is sanitized: raw Postgrex.Error NOT propagated to client
  #
  # Kassandra Finding LOW-3: log full reason server-side, return generic
  # GRPC.Status.internal() to client with NO schema detail in the message.
  #
  # RED phase: fails because search_messages/2 does not exist yet.
  test "DB error is sanitized — raw error not propagated to client (Kassandra LOW-3)" do
    stream = build_stream("@dave:test.local")

    # Seed FakeSearchDB to return an error with sensitive schema detail.
    postgrex_error = %Postgrex.Error{
      message:  "schema detail leak",
      postgres: %{code: :undefined_table, message: "relation \"events\" does not exist"}
    }
    :ets.insert(:search_db_test, {:error, postgrex_error})

    request = %Core.SearchMessagesRequest{
      search_term: "hello",
      limit:       10
    }

    err =
      try do
        Server.search_messages(request, stream)
        :no_error
      rescue
        e in GRPC.RPCError -> e
      end

    assert err != :no_error,
           "Expected GRPC.RPCError to be raised on DB error"

    assert err.status == GRPC.Status.internal(),
           "Expected INTERNAL (#{GRPC.Status.internal()}), got status #{err.status}"

    # CRITICAL: the raw error detail MUST NOT be in the client-facing message.
    refute String.contains?(err.message, "schema detail leak"),
           "DB error detail leaked to client: #{inspect(err.message)}"

    refute String.contains?(err.message, "relation"),
           "DB error detail leaked to client: #{inspect(err.message)}"
  end
end
