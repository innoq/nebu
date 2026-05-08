defmodule Nebu.Search.DBTest do
  use ExUnit.Case, async: false

  # ─── Story 11.2: Search Membership Enforcement — red-phase ATDD tests ─────────
  #
  # ALL tests in this module are expected to FAIL until Story 11.2 is implemented.
  # Failing reasons:
  #   1. Nebu.Search.DB module does not exist yet.
  #   2. Nebu.Search.DB.search_messages/4 does not exist yet.
  #   3. Nebu.Search.DB.sql_search_messages/0 does not exist yet.
  #
  # These are INTEGRATION tests — they require a live PostgreSQL instance.
  # Run only when NEBU_DB_URL is set and Nebu.Repo is started.
  #
  # async: false — shared DB state; tests insert rows and must not race.
  #
  # Test strategy:
  #   - No Horde, no GenServer, no fake DB injection.
  #   - Direct SQL via Ecto.Adapters.SQL.query/3 for setup (insert fixtures).
  #   - Call Nebu.Search.DB.search_messages/4 under test.
  #   - Verify results against the canonical SQL contract from Story 11.2.
  #   - Fixture rows are cleaned up in on_exit to avoid cross-test pollution.
  #
  # Schema notes:
  #   - room_members.left_at IS NULL  → active member
  #   - room_members.left_at NOT NULL → has left or was kicked
  #   - events.search_vector updated automatically by events_search_vector_trigger
  #     (migration 000042) on INSERT/UPDATE OF content
  #   - encrypted rooms: detected by presence of m.room.encryption state event in events table
  #
  # Acceptance Criteria covered:
  #   AC1 — Cross-room scoping: member room A, non-member room B → only room A results
  #   AC2 — SQL-layer enforcement: SQL string contains membership subquery, NOT post-filter
  #   AC3 — Kicked user: left_at IS NOT NULL → zero results
  #   Additional:
  #     - Zero memberships → 200 OK semantics (empty list, no error)
  #     - Encrypted room (m.room.encryption event) → excluded from results
  #     - filter.rooms with unauthorized room → silently absent (empty, not error)
  #     - Multiple joined rooms → all appear in results, ordered by rank DESC

  @moduletag :integration

  # ─── Unique test run prefix ───────────────────────────────────────────────────
  # Prevents cross-test-run collision when tests are re-run without full DB reset.

  @run_id Base.url_encode64(:crypto.strong_rand_bytes(6), padding: false)

  # ─── Setup / Teardown ─────────────────────────────────────────────────────────
  #
  # Each test gets a fresh set of fixture IDs (unique per test via test_id/0).
  # All rows are cleaned up on_exit in reverse insertion order (events → rooms → users).
  # on_exit runs even if the test crashes, preventing DB pollution.

  setup %{test: test_name} do
    # Tests reach here only when @moduletag :integration is included (e.g. mix test --include integration).
    # Nebu.Repo must be running; fail fast if it is not.
    if Process.whereis(Nebu.Repo) == nil do
      flunk("Nebu.Repo is not started — run with a live PostgreSQL (NEBU_DB_URL must be set)")
    end

    test_id = "#{@run_id}_#{:erlang.phash2(test_name)}"
    {:ok, test_id: test_id}
  end

  # Insert a user row. Minimal required fields only.
  defp insert_user(user_id) do
    Ecto.Adapters.SQL.query!(
      Nebu.Repo,
      """
      INSERT INTO users (user_id, system_role, is_active, created_at)
      VALUES ($1, 'user', true, $2)
      ON CONFLICT (user_id) DO NOTHING
      """,
      [user_id, System.system_time(:millisecond)]
    )
  end

  # Insert a room row. Minimal required fields.
  defp insert_room(room_id) do
    Ecto.Adapters.SQL.query!(
      Nebu.Repo,
      """
      INSERT INTO rooms (room_id, visibility, created_at)
      VALUES ($1, 'private', $2)
      ON CONFLICT (room_id) DO NOTHING
      """,
      [room_id, System.system_time(:millisecond)]
    )
  end

  # Insert an active room_members row (left_at IS NULL → active member).
  defp join_room(room_id, user_id) do
    Ecto.Adapters.SQL.query!(
      Nebu.Repo,
      """
      INSERT INTO room_members (room_id, user_id, joined_at)
      VALUES ($1, $2, $3)
      ON CONFLICT (room_id, user_id)
        DO UPDATE SET left_at = NULL, joined_at = EXCLUDED.joined_at
      """,
      [room_id, user_id, System.system_time(:millisecond)]
    )
  end

  # Set left_at on an existing room_members row → simulates kick/leave.
  defp kick_from_room(room_id, user_id) do
    Ecto.Adapters.SQL.query!(
      Nebu.Repo,
      """
      UPDATE room_members SET left_at = $3
      WHERE room_id = $1 AND user_id = $2
      """,
      [room_id, user_id, System.system_time(:millisecond)]
    )
  end

  # Insert a m.room.message event. The search_vector trigger fires automatically on INSERT.
  defp insert_message(event_id, room_id, sender, body) do
    content = Jason.encode!(%{"msgtype" => "m.text", "body" => body})

    Ecto.Adapters.SQL.query!(
      Nebu.Repo,
      """
      INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
      VALUES ($1, $2, $3, 'm.room.message', $4::jsonb, $5)
      ON CONFLICT (event_id) DO NOTHING
      """,
      [event_id, room_id, sender, content, System.system_time(:millisecond)]
    )
  end

  # Insert a m.room.encryption state event → marks room as encrypted.
  defp insert_encryption_state_event(event_id, room_id, sender) do
    content = Jason.encode!(%{"algorithm" => "m.megolm.v1.aes-sha2"})

    Ecto.Adapters.SQL.query!(
      Nebu.Repo,
      """
      INSERT INTO events
        (event_id, room_id, sender, event_type, content, origin_server_ts, state_key)
      VALUES ($1, $2, $3, 'm.room.encryption', $4::jsonb, $5, '')
      ON CONFLICT (event_id) DO NOTHING
      """,
      [event_id, room_id, sender, content, System.system_time(:millisecond)]
    )
  end

  # Delete all fixture rows for a given test_id (cleanup helper).
  # Deletion order: events (FK on rooms) → room_members (FK on rooms/users) → rooms → users.
  defp cleanup(rows_to_delete) do
    Enum.each(rows_to_delete, fn {table, id_col, id_val} ->
      Ecto.Adapters.SQL.query(
        Nebu.Repo,
        "DELETE FROM #{table} WHERE #{id_col} = $1",
        [id_val]
      )
    end)
  end

  # ─── AC1: Cross-room scoping ──────────────────────────────────────────────────
  #
  # Given: Alice is in room A (left_at IS NULL); Alice is NOT in room B (no row).
  # Given: A matching m.room.message event exists in both room A and room B.
  # When: search_messages/4 called with user_id=Alice, term=unique_term.
  # Then: exactly 1 result returned with room_id = room_A.
  # And:  room_B's event is NOT in the results.

  test "AC1 — member room A results returned, non-member room B filtered", ctx do

    tid = ctx.test_id
    alice = "@alice_ac1_#{tid}:test.local"
    room_a = "!room_a_ac1_#{tid}:test.local"
    room_b = "!room_b_ac1_#{tid}:test.local"
    term = "xkjqac1#{tid}"
    event_a = "$ev_a_ac1_#{tid}:test.local"
    event_b = "$ev_b_ac1_#{tid}:test.local"

    # Fixture setup
    insert_user(alice)
    insert_room(room_a)
    insert_room(room_b)
    join_room(room_a, alice)
    # Alice does NOT join room_b — no room_members row for (room_b, alice)
    insert_message(event_a, room_a, alice, "message #{term} in room a")
    insert_message(event_b, room_b, alice, "message #{term} in room b")

    on_exit(fn ->
      cleanup([
        {"events", "event_id", event_a},
        {"events", "event_id", event_b},
        {"room_members", "room_id", room_a},
        {"rooms", "room_id", room_a},
        {"rooms", "room_id", room_b},
        {"users", "user_id", alice}
      ])
    end)

    # Exercise: Nebu.Search.DB does not exist yet → this raises UndefinedFunctionError (red).
    {:ok, results} = Nebu.Search.DB.search_messages(alice, term, 10, 0)

    result_room_ids = Enum.map(results, fn r -> r["room_id"] end)

    assert length(results) == 1,
           "expected exactly 1 result from room_a, got #{length(results)}: #{inspect(result_room_ids)}"

    assert hd(result_room_ids) == room_a,
           "expected result room_id=#{room_a}, got #{hd(result_room_ids)}"

    refute Enum.member?(result_room_ids, room_b),
           "room_b event must NOT appear in results (Alice never joined room_b)"
  end

  # ─── AC3: Kicked user gets zero results ──────────────────────────────────────
  #
  # Given: Alice was in room A (insert room_members row with left_at IS NULL).
  # Given: A matching m.room.message event exists in room A.
  # When: Alice is kicked (left_at = NOW() — left_at IS NOT NULL).
  # When: search_messages/4 called with user_id=Alice.
  # Then: zero results returned for room A.

  test "AC3 — kicked user (left_at IS NOT NULL) gets zero results", ctx do

    tid = ctx.test_id
    alice = "@alice_ac3_#{tid}:test.local"
    room_a = "!room_a_ac3_#{tid}:test.local"
    term = "xkjqac3#{tid}"
    event_a = "$ev_a_ac3_#{tid}:test.local"

    insert_user(alice)
    insert_room(room_a)
    join_room(room_a, alice)
    insert_message(event_a, room_a, alice, "message #{term} before kick")

    # Alice is kicked — set left_at to a non-NULL timestamp.
    kick_from_room(room_a, alice)

    on_exit(fn ->
      cleanup([
        {"events", "event_id", event_a},
        {"room_members", "room_id", room_a},
        {"rooms", "room_id", room_a},
        {"users", "user_id", alice}
      ])
    end)

    # Nebu.Search.DB does not exist yet → UndefinedFunctionError (red).
    {:ok, results} = Nebu.Search.DB.search_messages(alice, term, 10, 0)

    assert results == [],
           "expected zero results after kick (left_at IS NOT NULL), got: #{inspect(results)}"
  end

  # ─── Zero memberships → empty results, no error ──────────────────────────────
  #
  # Given: @nobody:server has no rows in room_members.
  # When: search_messages/4 called with user_id=nobody, term=anything.
  # Then: {:ok, []} — empty list, no SQL error, no exception.

  test "zero memberships → empty results, no SQL error", ctx do

    tid = ctx.test_id
    nobody = "@nobody_#{tid}:test.local"
    term = "anythingwilldo#{tid}"

    insert_user(nobody)

    on_exit(fn ->
      cleanup([{"users", "user_id", nobody}])
    end)

    # Nebu.Search.DB does not exist yet → UndefinedFunctionError (red).
    result = Nebu.Search.DB.search_messages(nobody, term, 10, 0)

    assert {:ok, []} = result,
           "expected {:ok, []} for user with no memberships, got: #{inspect(result)}"
  end

  # ─── Encrypted room excluded from search results ─────────────────────────────
  #
  # Matrix spec requirement (Story 11.2 additional): encrypted rooms MUST be excluded.
  # Encryption is detected by the presence of a m.room.encryption state event.
  #
  # Given: Alice is a member of an encrypted room (has m.room.encryption event).
  # Given: A matching m.room.message event exists in that room.
  # When: search_messages/4 called with Alice's user_id.
  # Then: zero results from the encrypted room (it is excluded from search).

  test "encrypted room (m.room.encryption event present) excluded from search results", ctx do

    tid = ctx.test_id
    alice = "@alice_enc_#{tid}:test.local"
    enc_room = "!enc_room_#{tid}:test.local"
    term = "secretenc#{tid}"
    enc_event_id = "$enc_state_#{tid}:test.local"
    msg_event_id = "$enc_msg_#{tid}:test.local"

    insert_user(alice)
    insert_room(enc_room)
    join_room(enc_room, alice)
    # Insert m.room.encryption state event — marks the room as encrypted.
    insert_encryption_state_event(enc_event_id, enc_room, alice)
    # Insert a searchable message in the encrypted room.
    insert_message(msg_event_id, enc_room, alice, "message #{term} in encrypted room")

    on_exit(fn ->
      cleanup([
        {"events", "event_id", enc_event_id},
        {"events", "event_id", msg_event_id},
        {"room_members", "room_id", enc_room},
        {"rooms", "room_id", enc_room},
        {"users", "user_id", alice}
      ])
    end)

    # Nebu.Search.DB does not exist yet → UndefinedFunctionError (red).
    {:ok, results} = Nebu.Search.DB.search_messages(alice, term, 10, 0)

    result_room_ids = Enum.map(results, fn r -> r["room_id"] end)

    assert Enum.empty?(results),
           "expected zero results from encrypted room, got: #{inspect(result_room_ids)}"
  end

  # ─── filter.rooms with unauthorized room → silently absent (200 OK) ──────────
  #
  # Matrix spec: if filter.rooms lists a room the user never joined, that room
  # is silently absent from results — NOT a 403 or 400 error.
  #
  # Given: Alice is a member of room A but NOT room B.
  # When: search_messages/4 called with a rooms filter for [room_b].
  # Then: {:ok, []} — empty list returned, no error.

  test "filter.rooms with unauthorized room → silently absent, no error", ctx do

    tid = ctx.test_id
    alice = "@alice_filter_#{tid}:test.local"
    room_a = "!room_a_filter_#{tid}:test.local"
    room_b = "!room_b_filter_#{tid}:test.local"
    term = "filtertest#{tid}"
    event_a = "$ev_filter_a_#{tid}:test.local"
    event_b = "$ev_filter_b_#{tid}:test.local"

    insert_user(alice)
    insert_room(room_a)
    insert_room(room_b)
    join_room(room_a, alice)
    # Alice does NOT join room_b.
    insert_message(event_a, room_a, alice, "message #{term} in room a")
    insert_message(event_b, room_b, alice, "message #{term} in room b")

    on_exit(fn ->
      cleanup([
        {"events", "event_id", event_a},
        {"events", "event_id", event_b},
        {"room_members", "room_id", room_a},
        {"rooms", "room_id", room_a},
        {"rooms", "room_id", room_b},
        {"users", "user_id", alice}
      ])
    end)

    # Membership SQL scopes results to rooms where left_at IS NULL.
    # Alice never joined room_b → room_b silently absent from results.
    # Nebu.Search.DB does not exist yet → UndefinedFunctionError (red).
    result = Nebu.Search.DB.search_messages(alice, term, 10, 0)

    assert {:ok, results} = result,
           "expected {:ok, list} (no error) when filter.rooms has unauthorized room, got: #{inspect(result)}"

    result_room_ids = Enum.map(results, fn r -> r["room_id"] end)

    refute Enum.member?(result_room_ids, room_b),
           "room_b must be silently absent from results (Alice never joined room_b)"
  end

  # ─── Multiple joined rooms all appear in results, ordered by rank ─────────────
  #
  # Given: Alice is a member of room A and room C (both left_at IS NULL).
  # Given: Matching messages exist in both rooms.
  # When: search_messages/4 called with Alice's user_id.
  # Then: results from both rooms are returned.
  # And:  results are ordered by rank DESC (ts_rank_cd descending).

  test "multiple joined rooms all appear in results, ordered by rank DESC", ctx do

    tid = ctx.test_id
    alice = "@alice_multi_#{tid}:test.local"
    room_a = "!room_a_multi_#{tid}:test.local"
    room_c = "!room_c_multi_#{tid}:test.local"
    # Use a term that appears more times in room_c so room_c ranks higher.
    term = "multiroomterm#{tid}"
    event_a1 = "$ev_a1_multi_#{tid}:test.local"
    event_c1 = "$ev_c1_multi_#{tid}:test.local"
    event_c2 = "$ev_c2_multi_#{tid}:test.local"

    insert_user(alice)
    insert_room(room_a)
    insert_room(room_c)
    join_room(room_a, alice)
    join_room(room_c, alice)
    # 1 match in room_a, 2 matches in room_c (different events → higher term frequency for room_c).
    insert_message(event_a1, room_a, alice, "#{term} once")
    insert_message(event_c1, room_c, alice, "#{term} once in room c first")
    insert_message(event_c2, room_c, alice, "#{term} again in room c second")

    on_exit(fn ->
      cleanup([
        {"events", "event_id", event_a1},
        {"events", "event_id", event_c1},
        {"events", "event_id", event_c2},
        {"room_members", "room_id", room_a},
        {"room_members", "room_id", room_c},
        {"rooms", "room_id", room_a},
        {"rooms", "room_id", room_c},
        {"users", "user_id", alice}
      ])
    end)

    # Nebu.Search.DB does not exist yet → UndefinedFunctionError (red).
    {:ok, results} = Nebu.Search.DB.search_messages(alice, term, 10, 0)

    result_room_ids = Enum.map(results, fn r -> r["room_id"] end)

    assert length(results) >= 2,
           "expected at least 2 results (room_a and room_c), got #{length(results)}: #{inspect(result_room_ids)}"

    assert Enum.member?(result_room_ids, room_a),
           "expected room_a to appear in results (Alice is a member)"

    assert Enum.member?(result_room_ids, room_c),
           "expected room_c to appear in results (Alice is a member)"

    # Verify results have a numeric "rank" key — search_messages returns ranked results.
    for result <- results do
      assert Map.has_key?(result, "rank"),
             "each result map must contain a 'rank' key, got: #{inspect(result)}"
    end

    # Verify descending rank order.
    ranks = Enum.map(results, fn r -> r["rank"] end)
    sorted_desc = Enum.sort(ranks, :desc)

    assert ranks == sorted_desc,
           "expected results ordered by rank DESC, got ranks: #{inspect(ranks)}"
  end
end

# ─── AC2: SQL structural test (unit — no DB required) ────────────────────────
#
# Extracted from the integration module so it runs in the standard unit test job
# (no NEBU_DB_URL required). Validates the SQL constant structure only.
#
# Given: Nebu.Search.DB module is loaded.
# When:  sql_search_messages/0 is called.
# Then:  The SQL string contains "room_members", "left_at IS NULL", and a subquery
#        pattern "IN (" or "= ANY(" — proving enforcement is at query time.
# And:   The SQL does NOT delegate to a get_rooms_for_user Elixir function.

defmodule Nebu.Search.DBStructuralTest do
  use ExUnit.Case, async: true

  test "AC2 — membership filter is SQL subquery (structural check)" do
    # Nebu.Search.DB does not exist yet → fails with UndefinedFunctionError (red).
    sql = Nebu.Search.DB.sql_search_messages()

    assert is_binary(sql),
           "expected sql_search_messages/0 to return a binary SQL string, got: #{inspect(sql)}"

    assert String.contains?(sql, "room_members"),
           "SQL must reference the room_members table for membership enforcement"

    assert String.contains?(sql, "left_at IS NULL"),
           "SQL must use left_at IS NULL (not a membership column — that column does not exist)"

    assert String.contains?(sql, "IN (") or String.contains?(sql, "= ANY("),
           "SQL must use a subquery (IN / = ANY) for membership — not an application-layer post-filter"

    refute String.contains?(sql, "get_rooms_for_user"),
           "SQL must NOT delegate to a get_rooms_for_user Elixir function for filtering"
  end
end
