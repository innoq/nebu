defmodule Nebu.Presence.ManagerTest do
  use ExUnit.Case, async: false
  # async: false required — :NebuPresence is a named global ETS table and
  # :pg group membership is process-global. Concurrent test writes would
  # cause race conditions on shared state.

  # ---------------------------------------------------------------------------
  # Setup / Teardown
  # ---------------------------------------------------------------------------
  #
  # :NebuPresence is created by Nebu.Presence.Application.start/2 (owned by the
  # Application process) before any supervised children are started. After
  # Story 4-7 is implemented, this table will already exist when tests run.
  #
  # During the RED phase (before implementation), Application.start/2 does NOT
  # yet create :NebuPresence. The setup block below creates it when missing so
  # that individual test assertions fail on the non-existent module/functions
  # rather than on a missing ETS table.
  #
  # Do NOT call :ets.delete/1 on :NebuPresence — that would destroy the table
  # and break subsequent tests. Use :ets.delete_all_objects/1 to clear entries.

  setup do
    # Ensure :NebuPresence table exists (idempotent guard — mirrors AC #2 pattern)
    if :ets.whereis(:NebuPresence) == :undefined do
      :ets.new(:NebuPresence, [:named_table, :set, :public])
    end

    # Clear all presence entries between tests for isolation
    :ets.delete_all_objects(:NebuPresence)

    on_exit(fn ->
      # Clear entries after each test; do NOT delete the table itself
      if :ets.whereis(:NebuPresence) != :undefined do
        :ets.delete_all_objects(:NebuPresence)
      end

      # MINOR-2 fix: clean up Application.put_env overrides from heartbeat tests
      Application.delete_env(:presence, :unavailable_threshold_ms)
      Application.delete_env(:presence, :offline_threshold_ms)

      # MINOR-3 fix: ensure test process leaves :pg group to prevent cross-test interference
      :pg.leave("presence", self())
    end)

    :ok
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #1: set_presence online → get returns online
  # ---------------------------------------------------------------------------
  #
  # Verifies the basic happy-path round-trip: set_presence/2 upserts the ETS
  # entry; get_presence/1 reads it back as {:ok, %{status: :online, ...}}.
  # last_active_at must be a non-nil integer (milliseconds since epoch).

  describe "Nebu.Presence.Manager.set_presence/2 + get_presence/1" do
    test "set_presence :online → get_presence returns {:ok, %{status: :online, last_active_at: integer()}}" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      assert :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)

      # Synchronization fence: :sys.get_state blocks until all prior casts are processed
      :sys.get_state(Nebu.Presence.Manager)

      assert {:ok, result} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
      assert result.status == :online
      assert is_integer(result.last_active_at),
             "last_active_at must be an integer millisecond timestamp, got: #{inspect(result.last_active_at)}"
      assert result.last_active_at > 0
    end

    test "set_presence :offline → get_presence returns {:ok, %{status: :offline, last_active_at: integer()}}" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)
      :sys.get_state(Nebu.Presence.Manager)

      assert :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :offline)
      :sys.get_state(Nebu.Presence.Manager)

      assert {:ok, result} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
      assert result.status == :offline
      assert is_integer(result.last_active_at)
    end

    test "set_presence upserts — second call with same user_id overwrites status" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)
      :sys.get_state(Nebu.Presence.Manager)
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :unavailable)
      :sys.get_state(Nebu.Presence.Manager)

      assert {:ok, result} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
      assert result.status == :unavailable

      # Exactly one ETS entry — no duplicate
      entries = :ets.lookup(:NebuPresence, "@kai:nebu.local")
      assert length(entries) == 1
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #2: Missing user defaults to offline
  # ---------------------------------------------------------------------------
  #
  # get_presence/1 must NEVER return {:error, :not_found}. Unknown users default
  # to {:ok, %{status: :offline, last_active_at: nil}}. This is intentionally
  # different from EtsStore.get_session/1 (Story 4-5) which returns :not_found.

  describe "Nebu.Presence.Manager.get_presence/1 — unknown user" do
    test "returns {:ok, %{status: :offline, last_active_at: nil}} for a user never registered" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      assert {:ok, result} = Nebu.Presence.Manager.get_presence("@unknown:nebu.local")
      assert result.status == :offline
      assert result.last_active_at == nil
    end

    test "returns offline default after ETS entry is cleared between tests" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      # ETS was cleared in setup — no entry for this user exists
      assert {:ok, %{status: :offline, last_active_at: nil}} =
               Nebu.Presence.Manager.get_presence("@completely_new:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #3: Heartbeat — online → unavailable after 60s
  # ---------------------------------------------------------------------------
  #
  # Strategy: backdate last_active_at in ETS by 61 seconds, then send
  # :check_heartbeats directly to the GenServer. This avoids waiting 60 real
  # seconds while still exercising the production code path.
  #
  # The unavailable_threshold_ms is read from Application.get_env at runtime
  # (Story 4-6 config injection pattern). Tests override it via put_env.

  describe "Heartbeat: online → unavailable transition" do
    test "user in :online state with last_active_at >60s ago transitions to :unavailable on :check_heartbeats" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)
      :sys.get_state(Nebu.Presence.Manager)

      # Verify :online before backdating
      assert {:ok, %{status: :online}} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")

      # Backdate last_active_at to 61 seconds ago — exceeds 60s unavailable threshold
      now_ms = System.system_time(:millisecond)
      stale_ts = now_ms - 61_000
      :ets.insert(:NebuPresence, {"@kai:nebu.local", :online, stale_ts})

      # Override unavailable threshold for explicit test control
      Application.put_env(:presence, :unavailable_threshold_ms, 60_000)

      # Send :check_heartbeats directly to trigger the transition
      send(Process.whereis(Nebu.Presence.Manager), :check_heartbeats)

      # Synchronization fence: blocks until :check_heartbeats handle_info completes
      :sys.get_state(Nebu.Presence.Manager)

      assert {:ok, result} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
      assert result.status == :unavailable,
             "Expected :unavailable after 61s inactivity, got: #{inspect(result.status)}"
    end

    test "user already :unavailable is NOT re-transitioned by :check_heartbeats if within 5min window" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :unavailable)
      :sys.get_state(Nebu.Presence.Manager)

      # last_active_at 90 seconds ago — past unavailable threshold but within offline threshold
      now_ms = System.system_time(:millisecond)
      stale_ts = now_ms - 90_000
      :ets.insert(:NebuPresence, {"@kai:nebu.local", :unavailable, stale_ts})

      Application.put_env(:presence, :unavailable_threshold_ms, 60_000)
      Application.put_env(:presence, :offline_threshold_ms, 300_000)

      send(Process.whereis(Nebu.Presence.Manager), :check_heartbeats)
      :sys.get_state(Nebu.Presence.Manager)

      # Still :unavailable — not yet :offline (age 90s < 300s offline threshold)
      assert {:ok, %{status: :unavailable}} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #4: Heartbeat — unavailable → offline after 5 minutes
  # ---------------------------------------------------------------------------
  #
  # User is in :unavailable state with last_active_at more than 5 minutes ago.
  # :check_heartbeats must transition them to :offline.

  describe "Heartbeat: unavailable → offline transition" do
    test "user in :unavailable state with last_active_at >5min ago transitions to :offline on :check_heartbeats" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :unavailable)
      :sys.get_state(Nebu.Presence.Manager)

      # Backdate last_active_at to 301 seconds ago — exceeds 300s offline threshold
      now_ms = System.system_time(:millisecond)
      stale_ts = now_ms - 301_000
      :ets.insert(:NebuPresence, {"@kai:nebu.local", :unavailable, stale_ts})

      Application.put_env(:presence, :unavailable_threshold_ms, 60_000)
      Application.put_env(:presence, :offline_threshold_ms, 300_000)

      send(Process.whereis(Nebu.Presence.Manager), :check_heartbeats)
      :sys.get_state(Nebu.Presence.Manager)

      assert {:ok, result} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
      assert result.status == :offline,
             "Expected :offline after 301s inactivity in :unavailable, got: #{inspect(result.status)}"
    end

    test "online user with >5min inactivity skips :unavailable and goes directly to cond branch — only :online → :unavailable transition fires" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      # Spec: :online → :unavailable check uses unavailable_threshold_ms;
      # :unavailable → :offline check uses offline_threshold_ms.
      # An :online user with 400s inactivity hits only the :online branch.
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)
      :sys.get_state(Nebu.Presence.Manager)

      now_ms = System.system_time(:millisecond)
      stale_ts = now_ms - 400_000
      :ets.insert(:NebuPresence, {"@kai:nebu.local", :online, stale_ts})

      Application.put_env(:presence, :unavailable_threshold_ms, 60_000)
      Application.put_env(:presence, :offline_threshold_ms, 300_000)

      send(Process.whereis(Nebu.Presence.Manager), :check_heartbeats)
      :sys.get_state(Nebu.Presence.Manager)

      # cond matches :online branch first — transitions to :unavailable (not :offline)
      assert {:ok, %{status: :unavailable}} = Nebu.Presence.Manager.get_presence("@kai:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #2 / Acceptance Test #5: Crash/Restart — ETS data survives GenServer crash
  # ---------------------------------------------------------------------------
  #
  # Architecture invariant: :NebuPresence is owned by the Application process
  # (created in Nebu.Presence.Application.start/2, NOT in GenServer.init/1).
  # When the Manager GenServer is killed, the Application process continues
  # running and keeps the ETS table alive. The Supervisor restarts Manager and
  # all previously inserted data is still accessible.
  #
  # Uses the deterministic poll pattern from Story 4-5 (Enum.reduce_while) to
  # avoid flaky Process.sleep — polls until a new pid is registered.

  describe "ETS data survives Nebu.Presence.Manager GenServer crash" do
    test "presence inserted before crash is accessible after supervisor restarts Manager" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined /
      # Process.whereis(Nebu.Presence.Manager) returns nil
      :ok = Nebu.Presence.Manager.set_presence("@crash_test:nebu.local", :online)
      :sys.get_state(Nebu.Presence.Manager)

      # Verify data is present before crash
      assert {:ok, %{status: :online}} =
               Nebu.Presence.Manager.get_presence("@crash_test:nebu.local")

      # Locate the Manager process by its registered name
      pid = Process.whereis(Nebu.Presence.Manager)
      assert is_pid(pid), "Manager must be registered under Nebu.Presence.Manager"
      assert Process.alive?(pid)

      # Kill the GenServer process (simulates unexpected crash — NOT graceful stop).
      # :kill bypasses trap_exit — forces immediate termination.
      Process.exit(pid, :kill)

      # Deterministic poll: wait for the supervisor to restart the worker.
      # Avoids flaky Process.sleep by polling until a new, different pid is registered.
      new_pid =
        Enum.reduce_while(1..50, nil, fn _i, _acc ->
          Process.sleep(10)

          case Process.whereis(Nebu.Presence.Manager) do
            nil -> {:cont, nil}
            ^pid -> {:cont, nil}
            restarted_pid -> {:halt, restarted_pid}
          end
        end)

      assert is_pid(new_pid), "Supervisor must restart Nebu.Presence.Manager after crash"
      assert new_pid != pid, "Restarted pid must differ from crashed pid"

      # The ETS table is owned by the Application process, NOT the GenServer.
      # Therefore the table and its data must survive the GenServer crash.
      assert {:ok, %{status: :online}} =
               Nebu.Presence.Manager.get_presence("@crash_test:nebu.local")
    end
  end

  # ---------------------------------------------------------------------------
  # AC #1 / Acceptance Test #6: :pg broadcast on set_presence
  # ---------------------------------------------------------------------------
  #
  # Verifies ADR-002: presence updates are broadcast via :pg Process Groups
  # (OTP built-in, no external dependency). The test process joins group
  # "presence" and asserts it receives the {:presence_update, user_id, status}
  # message after set_presence/2 is called.

  describe ":pg broadcast — set_presence/2 broadcasts to :pg group \"presence\"" do
    test "subscriber receives {:presence_update, user_id, status} after set_presence :online" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined;
      # also fails if :pg scope is not started in Application.start/2

      # Subscribe test process to the "presence" :pg group
      :pg.join("presence", self())

      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)

      # assert_receive waits up to 500ms for the message — covers async cast + broadcast latency
      assert_receive {:presence_update, "@kai:nebu.local", :online}, 500

      # Cleanup: leave the group so other tests are not affected
      :pg.leave("presence", self())
    end

    test "subscriber receives {:presence_update, user_id, :offline} after set_presence :offline" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      :pg.join("presence", self())

      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :offline)

      assert_receive {:presence_update, "@kai:nebu.local", :offline}, 500

      :pg.leave("presence", self())
    end

    test "non-subscriber does NOT receive broadcast (message stays in own mailbox only)" do
      # FAILS in RED phase: Nebu.Presence.Manager is undefined
      # Test process does NOT join the "presence" group in this test —
      # verifies that :pg.join is required to receive broadcasts.
      :ok = Nebu.Presence.Manager.set_presence("@kai:nebu.local", :online)

      refute_receive {:presence_update, "@kai:nebu.local", :online}, 100
    end
  end

  # ---------------------------------------------------------------------------
  # AC #2 / Supplemental: Application ETS guard — idempotent creation
  # ---------------------------------------------------------------------------
  #
  # The :ets.whereis/1 guard in Nebu.Presence.Application.start/2 must prevent
  # ArgumentError when the Application is restarted in the same VM (e.g.
  # hot-code reload or test framework restart). If the table already exists,
  # calling :ets.new/2 again would crash without the guard.

  describe "Application.start/2 idempotent ETS creation guard (AC #2)" do
    test "calling Application.start/2 when :NebuPresence already exists does not crash" do
      # :NebuPresence is already created by setup (or Application boot).
      # Calling Application.start/2 again must not raise ArgumentError.
      assert :ets.whereis(:NebuPresence) != :undefined,
             ":NebuPresence must exist before this test (created in setup)"

      # Insert an entry to verify data is preserved after re-entry
      :ets.insert(:NebuPresence, {"@guard:nebu.local", :online, System.system_time(:millisecond)})

      # Re-invoking Application.start/2 must not crash (guard protects).
      # Returns {:error, {:already_started, _}} because the supervisor is already
      # running — the important thing is it does NOT raise ArgumentError.
      result = Nebu.Presence.Application.start(:normal, [])
      assert {:error, {:already_started, _}} = result

      # ETS data must be preserved — the guard skipped :ets.new
      assert [{"@guard:nebu.local", :online, _}] =
               :ets.lookup(:NebuPresence, "@guard:nebu.local")
    end
  end
end
