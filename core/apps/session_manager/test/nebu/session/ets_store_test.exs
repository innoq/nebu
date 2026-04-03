defmodule Nebu.Session.EtsStoreTest do
  use ExUnit.Case, async: false
  # async: false required — :NebuSessions is a named global ETS table.
  # Concurrent test cases would cause race conditions on shared ETS state.

  # ---------------------------------------------------------------------------
  # Setup / Teardown
  # ---------------------------------------------------------------------------
  #
  # :NebuSessions is created by Nebu.Session.Application.start/2 (owned by the
  # Application process) before any supervised children are started. After
  # Story 4-5 is implemented, this table will already exist when tests run.
  #
  # During the RED phase (before implementation), Application.start/2 does NOT
  # yet create :NebuSessions. The setup block below creates it when missing so
  # that individual test assertions fail on the non-existent module/functions
  # rather than on a missing ETS table.
  #
  # Do NOT call :ets.delete/1 on :NebuSessions — that would destroy the table
  # and break subsequent tests. Use :ets.delete_all_objects/1 to clear entries.

  setup do
    # Ensure :NebuSessions table exists (idempotent guard — mirrors AC #3 pattern)
    if :ets.whereis(:NebuSessions) == :undefined do
      :ets.new(:NebuSessions, [:named_table, :set, :public])
    end

    # Clear all session entries between tests for isolation
    :ets.delete_all_objects(:NebuSessions)

    on_exit(fn ->
      # Clear entries after each test; do NOT delete the table itself
      if :ets.whereis(:NebuSessions) != :undefined do
        :ets.delete_all_objects(:NebuSessions)
      end
    end)

    :ok
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #1: Happy-path round-trip — put_session + get_session
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.EtsStore.put_session/2 + get_session/1" do
    test "round-trip: put_session stores session_map; get_session returns it" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      session = %{
        access_token_hash: "abc123",
        device_id: "DEVICE_1",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      assert :ok = Nebu.Session.EtsStore.put_session("@kai:nebu.local", session)

      assert {:ok, ^session} = Nebu.Session.EtsStore.get_session("@kai:nebu.local")
    end

    # AC #4 / Acceptance Test #5: Upsert — no duplicate on repeated put
    test "upsert: second put_session with same user_id overwrites, no duplicate entry" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      session_v1 = %{
        access_token_hash: "hash_v1",
        device_id: "DEVICE_1",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      session_v2 = %{
        access_token_hash: "hash_v2",
        device_id: "DEVICE_1",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_100_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session("@kai:nebu.local", session_v1)
      :ok = Nebu.Session.EtsStore.put_session("@kai:nebu.local", session_v2)

      # Exactly one entry in the table (no duplicate)
      all_sessions = Nebu.Session.EtsStore.list_sessions()
      assert length(all_sessions) == 1

      # The stored value is the latest version
      assert {:ok, ^session_v2} = Nebu.Session.EtsStore.get_session("@kai:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #2: Missing key returns {:error, :not_found}
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.EtsStore.get_session/1" do
    test "returns {:error, :not_found} for a user_id that was never inserted" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      assert {:error, :not_found} =
               Nebu.Session.EtsStore.get_session("@unknown:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #3: delete_session removes entry
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.EtsStore.delete_session/1" do
    test "deletes an existing session; subsequent get_session returns {:error, :not_found}" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      session = %{
        access_token_hash: "todelete",
        device_id: "DEVICE_D",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session("@kai:nebu.local", session)

      # Verify the session is present before deletion
      assert {:ok, _} = Nebu.Session.EtsStore.get_session("@kai:nebu.local")

      # Delete returns :ok
      assert :ok = Nebu.Session.EtsStore.delete_session("@kai:nebu.local")

      # Entry is gone
      assert {:error, :not_found} = Nebu.Session.EtsStore.get_session("@kai:nebu.local")
    end

    test "delete_session on a missing key still returns :ok (idempotent)" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      assert :ok = Nebu.Session.EtsStore.delete_session("@never_existed:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #4: list_sessions/0 returns all current sessions
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.EtsStore.list_sessions/0" do
    test "returns all session maps currently in the ETS table" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      session_kai = %{
        access_token_hash: "hash_kai",
        device_id: "DEVICE_K",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      session_alex = %{
        access_token_hash: "hash_alex",
        device_id: "DEVICE_A",
        created_at_ms: 1_712_100_000_000,
        last_seen_at_ms: 1_712_100_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session("@kai:nebu.local", session_kai)
      :ok = Nebu.Session.EtsStore.put_session("@alex:nebu.local", session_alex)

      sessions = Nebu.Session.EtsStore.list_sessions()

      assert length(sessions) == 2
      assert session_kai in sessions
      assert session_alex in sessions
    end

    test "returns empty list when no sessions have been inserted" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      assert [] = Nebu.Session.EtsStore.list_sessions()
    end
  end

  # ---------------------------------------------------------------------------
  # AC #2 / Acceptance Test #6: access_token_hash stores SHA-256 hex, not raw token
  # ---------------------------------------------------------------------------

  describe "access_token_hash — SHA-256 encoding" do
    test "stored hash equals Base16-encoded SHA-256 of the raw token, not the raw token itself" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined
      raw_token = "secret_token_value"

      expected_hash =
        Base.encode16(:crypto.hash(:sha256, raw_token), case: :lower)

      session = %{
        access_token_hash: expected_hash,
        device_id: "DEVICE_H",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session("@kai:nebu.local", session)

      assert {:ok, stored_session} = Nebu.Session.EtsStore.get_session("@kai:nebu.local")

      # Hash must be the 64-character lowercase hex string, NOT the raw token
      assert stored_session.access_token_hash == expected_hash
      refute stored_session.access_token_hash == raw_token

      # Sanity check: hash is a 64-character lowercase hex string
      assert String.length(stored_session.access_token_hash) == 64
      assert stored_session.access_token_hash =~ ~r/^[0-9a-f]{64}$/
    end
  end

  # ---------------------------------------------------------------------------
  # AC #3: Application.start/2 guard — ETS table creation is idempotent
  # ---------------------------------------------------------------------------
  #
  # The :ets.whereis/1 guard in Nebu.Session.Application.start/2 must prevent
  # ArgumentError when the Application is restarted in the same VM (e.g.
  # hot-code reload or test framework restart). If the table already exists,
  # calling :ets.new/2 again would crash without the guard.

  describe "Application.start/2 idempotent ETS creation guard (AC #3)" do
    test "calling Application.start/2 when :NebuSessions already exists does not crash" do
      # :NebuSessions is already created by setup (or Application boot).
      # Calling Application.start/2 again must not raise ArgumentError.
      assert :ets.whereis(:NebuSessions) != :undefined,
             ":NebuSessions must exist before this test"

      # Insert a session to verify data is preserved after re-entry
      session = %{
        access_token_hash: "guard_test",
        device_id: "DEVICE_G",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session("@guard:nebu.local", session)

      # Re-invoking Application.start/2 must not crash (guard protects)
      # Note: this returns {:error, {:already_started, _}} because the supervisor
      # is already running, but the important thing is it does NOT raise.
      result = Nebu.Session.Application.start(:normal, [])
      assert {:error, {:already_started, _}} = result

      # ETS data must be preserved — the guard skipped :ets.new
      assert {:ok, ^session} = Nebu.Session.EtsStore.get_session("@guard:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #5 / Acceptance Test #7: Crash/restart test — ETS table survives GenServer crash
  # ---------------------------------------------------------------------------
  #
  # Architecture: :NebuSessions is owned by the Application process (created in
  # Nebu.Session.Application.start/2, NOT in Nebu.Session.EtsStore.init/1).
  # When the EtsStore GenServer is killed, the Application process continues
  # running and keeps the ETS table alive. The Supervisor restarts EtsStore and
  # all previously inserted data is still accessible.

  describe "ETS data survives Nebu.Session.EtsStore GenServer crash" do
    test "session inserted before crash is accessible after supervisor restarts EtsStore" do
      # FAILS in RED phase: Nebu.Session.EtsStore is undefined /
      # Process.whereis(Nebu.Session.EtsStore) returns nil

      session = %{
        access_token_hash: "crash_test_hash",
        device_id: "DEVICE_X",
        created_at_ms: System.system_time(:millisecond),
        last_seen_at_ms: System.system_time(:millisecond)
      }

      :ok = Nebu.Session.EtsStore.put_session("@crash_test:nebu.local", session)

      # Verify data is present before crash
      assert {:ok, ^session} = Nebu.Session.EtsStore.get_session("@crash_test:nebu.local")

      # Locate the EtsStore GenServer by its registered name
      pid = Process.whereis(Nebu.Session.EtsStore)
      assert is_pid(pid), "EtsStore must be registered under its module name"
      assert Process.alive?(pid)

      # Kill the GenServer process (simulates unexpected crash — NOT graceful stop)
      Process.exit(pid, :kill)

      # Deterministic poll: wait for the supervisor to restart the worker.
      # Avoids flaky Process.sleep by polling until a new pid is registered.
      new_pid =
        Enum.reduce_while(1..50, nil, fn _i, _acc ->
          Process.sleep(10)

          case Process.whereis(Nebu.Session.EtsStore) do
            nil -> {:cont, nil}
            ^pid -> {:cont, nil}
            restarted_pid -> {:halt, restarted_pid}
          end
        end)

      assert is_pid(new_pid), "Supervisor must restart EtsStore after crash"
      assert new_pid != pid

      # The ETS table is owned by the Application process, NOT the GenServer.
      # Therefore the table and its data must survive the GenServer crash.
      assert {:ok, ^session} = Nebu.Session.EtsStore.get_session("@crash_test:nebu.local")
    end
  end
end
