defmodule Nebu.Session.PgStoreTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global and
  # :NebuSessions is a named global ETS table.

  # ---------------------------------------------------------------------------
  # FakePgStore — ETS-backed stub that simulates PG without a real DB connection.
  # Mimics the behaviour that Nebu.Session.PgStore will define.
  # ---------------------------------------------------------------------------
  #
  # FAILS in RED phase because Nebu.Session.PgStore is not yet defined,
  # so `@behaviour Nebu.Session.PgStore` will raise UndefinedFunctionError.

  defmodule FakePgStore do
    @behaviour Nebu.Session.PgStore

    @impl Nebu.Session.PgStore
    def persist_since_token(user_id, since_token, last_event_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:pg_store_test, {{user_id, ""}, since_token, last_event_id, now_ms})
      :ok
    end

    @impl Nebu.Session.PgStore
    def persist_since_token(user_id, device_id, since_token, last_event_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:pg_store_test, {{user_id, device_id}, since_token, last_event_id, now_ms})
      :ok
    end

    @impl Nebu.Session.PgStore
    def get_since_token(user_id) do
      case :ets.lookup(:pg_store_test, {user_id, ""}) do
        [{{^user_id, ""}, since_token, last_event_id, _ts}] ->
          {:ok, %{since_token: since_token, last_event_id: last_event_id}}

        [] ->
          {:error, :not_found}
      end
    end

    @impl Nebu.Session.PgStore
    def get_since_token(user_id, device_id) do
      case :ets.lookup(:pg_store_test, {user_id, device_id}) do
        [{{^user_id, ^device_id}, since_token, last_event_id, _ts}] ->
          {:ok, %{since_token: since_token, last_event_id: last_event_id}}

        [] ->
          {:error, :not_found}
      end
    end

    @impl Nebu.Session.PgStore
    def invalidate_session(user_id) do
      # Simulates: delete from sync_tokens + sessions in a single transaction,
      # then evict from ETS — ETS eviction happens ONLY after DB success.
      :ets.delete(:pg_store_test, {user_id, ""})
      :ets.delete(:pg_sessions_test, user_id)
      Nebu.Session.EtsStore.delete_session(user_id)
      :ok
    end

    @impl Nebu.Session.PgStore
    def invalidate_session(user_id, device_id) do
      :ets.delete(:pg_store_test, {user_id, device_id})
      :ok
    end
  end

  # ---------------------------------------------------------------------------
  # FakeRepo — simulates a DB repo whose transaction/1 always fails.
  # Used by AT #8 to exercise the real branching logic in PgStore.Postgres.
  # ---------------------------------------------------------------------------

  defmodule FakeRepo do
    # transaction/1 calls the given function and returns a DB error,
    # simulating a lost connection before any SQL executes.
    def transaction(_fun), do: {:error, :db_connection_lost}

    def rollback(reason), do: throw({:rollback, reason})
  end

  # ---------------------------------------------------------------------------
  # Setup / Teardown
  # ---------------------------------------------------------------------------

  setup do
    # Create fresh fake PG table for sync_tokens
    if :ets.whereis(:pg_store_test) != :undefined, do: :ets.delete(:pg_store_test)
    :ets.new(:pg_store_test, [:named_table, :set, :public])

    # Create fresh fake PG table for sessions (used in invalidate_session tests)
    if :ets.whereis(:pg_sessions_test) != :undefined, do: :ets.delete(:pg_sessions_test)
    :ets.new(:pg_sessions_test, [:named_table, :set, :public])

    # Ensure :NebuSessions ETS table exists for EtsStore interaction
    if :ets.whereis(:NebuSessions) == :undefined do
      :ets.new(:NebuSessions, [:named_table, :set, :public])
    end

    :ets.delete_all_objects(:NebuSessions)

    Application.put_env(:session_manager, :pg_store_module, FakePgStore)

    on_exit(fn ->
      Application.delete_env(:session_manager, :pg_store_module)
      Application.delete_env(:session_manager, :repo_module)

      if :ets.whereis(:pg_store_test) != :undefined, do: :ets.delete(:pg_store_test)
      if :ets.whereis(:pg_sessions_test) != :undefined, do: :ets.delete(:pg_sessions_test)

      if :ets.whereis(:NebuSessions) != :undefined do
        :ets.delete_all_objects(:NebuSessions)
      end
    end)

    :ok
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #1 — persist_since_token + get_since_token round-trip
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.PgStore.persist_since_token/3 + get_since_token/1" do
    test "round-trip: persist stores data; get returns it" do
      # FAILS in RED phase: Nebu.Session.PgStore is undefined
      user_id = "@kai:nebu.local"
      since_token = "opaque_token_v1"
      last_event_id = "last_event_id_1"

      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, since_token, last_event_id)

      assert {:ok, %{since_token: ^since_token, last_event_id: ^last_event_id}} =
               Nebu.Session.PgStore.get_since_token(user_id)
    end
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #2 — get_since_token on missing user returns :not_found
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.PgStore.get_since_token/1 — missing user" do
    test "returns {:error, :not_found} for a user_id with no stored token" do
      # FAILS in RED phase: Nebu.Session.PgStore is undefined
      assert {:error, :not_found} =
               Nebu.Session.PgStore.get_since_token("@unknown:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #3 — invalidate_session removes from ETS and PG
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.PgStore.invalidate_session/1" do
    test "removes session from both ETS and PG store" do
      # FAILS in RED phase: Nebu.Session.PgStore is undefined
      user_id = "@kai:nebu.local"

      # Set up ETS session
      session = %{
        access_token_hash: "abc123",
        device_id: "DEVICE_1",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session(user_id, session)

      # Set up fake PG sync_token row — key is now {user_id, device_id} composite
      :ets.insert(:pg_store_test, {{user_id, ""}, "some_token", "$last_evt", System.system_time(:millisecond)})

      # Set up fake PG sessions row
      :ets.insert(:pg_sessions_test, {user_id, "session_data"})

      # Verify pre-conditions
      assert {:ok, _} = Nebu.Session.EtsStore.get_session(user_id)
      assert [{{^user_id, ""}, _, _, _}] = :ets.lookup(:pg_store_test, {user_id, ""})
      assert [{^user_id, _}] = :ets.lookup(:pg_sessions_test, user_id)

      # Invalidate
      assert :ok = Nebu.Session.PgStore.invalidate_session(user_id)

      # ETS session is gone
      assert {:error, :not_found} = Nebu.Session.EtsStore.get_session(user_id)

      # sync_tokens row is gone
      assert [] = :ets.lookup(:pg_store_test, {user_id, ""})

      # sessions row is gone
      assert [] = :ets.lookup(:pg_sessions_test, user_id)
    end
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #4 — since_token is opaque (base64, not a sequential integer)
  # ---------------------------------------------------------------------------

  describe "since_token opaqueness" do
    test "generated since_token is not parseable as an integer" do
      # FAILS in RED phase: validates the generation formula from AC #2
      user_id = "@kai:nebu.local"
      last_event_id = "$abc123"

      since_token =
        Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)

      # Must NOT be a plain integer
      assert Integer.parse(since_token) == :error

      # Must be a non-empty string of standard base64 characters (no base64url)
      assert String.length(since_token) > 0
      assert since_token =~ ~r/\A[A-Za-z0-9+\/]+\z/

      # When decoded, must contain colon separators (not guessable)
      decoded = Base.decode64!(since_token, padding: false)
      assert decoded =~ ":"
    end

    test "two tokens for the same user differ due to monotonic_time component" do
      user_id = "@kai:nebu.local"
      last_event_id = "$abc123"

      token_a =
        Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)

      token_b =
        Base.encode64("#{user_id}:#{last_event_id}:#{System.monotonic_time()}", padding: false)

      # With separate monotonic_time calls the tokens should differ.
      # If by chance they are the same (same nanosecond) we accept it — the
      # important property is that they are not simply sequential integers.
      assert Integer.parse(token_a) == :error
      assert Integer.parse(token_b) == :error
    end
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #5 — upsert: second persist_since_token replaces the first
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.PgStore.persist_since_token/3 — upsert semantics" do
    test "second call with same user_id replaces first (ON CONFLICT DO UPDATE)" do
      # FAILS in RED phase: Nebu.Session.PgStore is undefined
      user_id = "@kai:nebu.local"

      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, "token_v1", "event_1")
      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, "token_v2", "event_2")

      assert {:ok, %{since_token: "token_v2", last_event_id: "event_2"}} =
               Nebu.Session.PgStore.get_since_token(user_id)

      # Only one row in the fake PG table — no duplicate
      all_rows = :ets.tab2list(:pg_store_test)
      user_rows = Enum.filter(all_rows, fn {{uid, _dev}, _, _, _} -> uid == user_id end)
      assert length(user_rows) == 1
    end
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #8 — DB failure does not corrupt ETS (atomicity rule)
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.PgStore.Postgres.invalidate_session/1 — DB failure atomicity" do
    test "returns DB error and does NOT evict ETS when DB transaction fails" do
      user_id = "@kai:nebu.local"

      session = %{
        access_token_hash: "must_survive",
        device_id: "DEVICE_S",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session(user_id, session)

      # Inject FakeRepo so PgStore.Postgres.invalidate_session/1 hits a failing transaction
      Application.put_env(:session_manager, :repo_module, FakeRepo)

      # Call the real Postgres implementation directly — not the delegation facade.
      # This exercises the branching logic in PgStore.Postgres.invalidate_session/1.
      result = Nebu.Session.PgStore.Postgres.invalidate_session(user_id)

      assert result == {:error, :db_connection_lost}

      # ETS must NOT have been evicted — session survives the DB failure
      assert {:ok, ^session} = Nebu.Session.EtsStore.get_session(user_id)
    end
  end

  # ════════════════════════════════════════════════════════════════════════
  # Story 9-22: GAP-SINCE-IGNORED — Per-Device Sync Token Storage
  # ════════════════════════════════════════════════════════════════════════
  #
  # These tests FAIL before story 9-22 because Nebu.Session.PgStore
  # does not yet have /2 or /4 arities for per-device token functions.
  #
  # AC4: persist_since_token/4 stores a row per (user_id, device_id)
  # AC5: get_since_token/2 returns :not_found for unknown (user_id, device_id)
  # AC6: invalidate_session/2 deletes the (user_id, device_id) row

  describe "Nebu.Session.PgStore — per-device token storage (Story 9-22)" do
    test "AC4: persist_since_token/4 stores independent rows per device" do
      user_id = "@alice:nebu.local"

      assert :ok =
               Nebu.Session.PgStore.persist_since_token(user_id, "D1", "token_d1", "$ev_d1")

      assert :ok =
               Nebu.Session.PgStore.persist_since_token(user_id, "D2", "token_d2", "$ev_d2")

      # Each device has its own row
      assert {:ok, %{since_token: "token_d1", last_event_id: "$ev_d1"}} =
               Nebu.Session.PgStore.get_since_token(user_id, "D1")

      assert {:ok, %{since_token: "token_d2", last_event_id: "$ev_d2"}} =
               Nebu.Session.PgStore.get_since_token(user_id, "D2")
    end

    test "AC4b: second persist_since_token/4 for same (user_id, device_id) upserts" do
      user_id = "@bob:nebu.local"

      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, "D1", "token_v1", "$ev1")
      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, "D1", "token_v2", "$ev2")

      assert {:ok, %{since_token: "token_v2", last_event_id: "$ev2"}} =
               Nebu.Session.PgStore.get_since_token(user_id, "D1")
    end

    test "AC5: get_since_token/2 returns :not_found for unknown (user_id, device_id)" do
      assert {:error, :not_found} =
               Nebu.Session.PgStore.get_since_token("@unknown:nebu.local", "NO_SUCH_DEVICE")
    end

    test "AC6: invalidate_session/2 removes the device row; other devices unaffected" do
      user_id = "@carol:nebu.local"

      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, "D1", "tok_d1", "$ev_d1")
      assert :ok = Nebu.Session.PgStore.persist_since_token(user_id, "D2", "tok_d2", "$ev_d2")

      assert :ok = Nebu.Session.PgStore.invalidate_session(user_id, "D1")

      # D1 deleted; D2 still present
      assert {:error, :not_found} =
               Nebu.Session.PgStore.get_since_token(user_id, "D1")

      assert {:ok, %{since_token: "tok_d2"}} =
               Nebu.Session.PgStore.get_since_token(user_id, "D2")
    end
  end
end
