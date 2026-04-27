defmodule Compliance.SessionExpiryWorkerTest do
  use ExUnit.Case, async: false

  # session_expiry_worker_test.exs — Story 5.5: Compliance.SessionExpiryWorker — RED-phase tests
  #
  # ALL tests in this module are expected to FAIL until Story 5.5 is implemented.
  # Failing reason: Compliance.SessionExpiryWorker does not exist yet.
  # Tests will fail with UndefinedFunctionError / module not found (red phase).
  #
  # Persistenz-Strategie: Option C — Stateless.
  # The worker holds no state in ETS or DB. If it crashes, the supervisor restarts it
  # and the next :tick is rescheduled immediately via Process.send_after in init/1.
  # No crash/state-recovery test is needed for lost ticks — at most 1 hour delay
  # is acceptable for MVP (documented in AC9).
  #
  # Test strategy:
  #   - FakeRepo is shared with audit_writer_test.exs (declared inline here as FakeRepoW).
  #     It executes transactions immediately and tracks inserts via an Agent.
  #   - FakeAuditWriter replaces Compliance.AuditWriter so tests can assert log/6 calls
  #     without touching PostgreSQL.
  #   - :tick messages are sent directly to the worker pid to trigger immediate scans
  #     (bypassing the 3600s timer) — deterministic, no sleep required.
  #   - The crash/restart test starts a real supervisor with SessionExpiryWorker as child,
  #     kills the worker, and verifies supervisor restarts it within 100ms.
  #
  # AC coverage:
  #   AC9 (start_link, tick, crash/restart) — Tests 17, 18, 19

  # ─── FakeAuditWriter ──────────────────────────────────────────────────────────
  #
  # Replaces Compliance.AuditWriter in tests to record log/6 calls without DB access.
  # Each test injects this module via Application.put_env(:compliance, :audit_writer, FakeAuditWriter).

  defmodule FakeAuditWriter do
    @moduledoc "In-process fake for Compliance.AuditWriter. Test-private."

    def log(actor_user_id, action, target_type, target_id, metadata, outcome) do
      bucket = Application.get_env(:compliance, :__test_audit_writer_bucket__)
      if bucket do
        call = %{
          actor_user_id: actor_user_id,
          action: action,
          target_type: target_type,
          target_id: target_id,
          metadata: metadata,
          outcome: outcome
        }
        Agent.update(bucket, fn calls -> [call | calls] end)
      end
      :ok
    end
  end

  # ─── FakeRepo (for SessionExpiryWorker) ───────────────────────────────────────
  #
  # Provides Nebu.Repo.query!/2 which the worker uses to SELECT expired sessions.
  # DSN-style configuration via Application env:
  #   :compliance, :__test_expired_session_ids__ — list of session UUIDs to return

  defmodule FakeRepoW do
    @moduledoc "In-process fake for Nebu.Repo used by SessionExpiryWorker. Test-private."

    def query!(_sql, _params) do
      session_ids = Application.get_env(:compliance, :__test_expired_session_ids__, [])
      # Return in the format Nebu.Repo.query! returns: %{rows: [[binary], ...]}
      rows = Enum.map(session_ids, fn id -> [id] end)
      %{rows: rows}
    end

    def transaction(fun) when is_function(fun, 0) do
      {:ok, fun.()}
    end

    def insert(struct, _opts \\ []), do: {:ok, struct}
    def insert!(struct, _opts \\ []), do: struct
  end

  # ─── Setup / Teardown ─────────────────────────────────────────────────────────

  setup do
    # Start an Agent to collect AuditWriter.log calls
    {:ok, bucket} = Agent.start_link(fn -> [] end)
    Application.put_env(:compliance, :__test_audit_writer_bucket__, bucket)
    Application.put_env(:compliance, :audit_writer, FakeAuditWriter)
    Application.put_env(:compliance, :repo, FakeRepoW)
    Application.put_env(:compliance, :__test_expired_session_ids__, [])

    on_exit(fn ->
      Application.delete_env(:compliance, :__test_audit_writer_bucket__)
      Application.delete_env(:compliance, :audit_writer)
      Application.delete_env(:compliance, :repo)
      Application.delete_env(:compliance, :__test_expired_session_ids__)
    end)

    {:ok, bucket: bucket}
  end

  # ─── Test 17: start_link succeeds and worker is alive ─────────────────────────
  #
  # AC9 — start_link/1 starts successfully; worker pid is alive.
  # The worker schedules a :tick via Process.send_after — we do not wait for the
  # real 3600s timer; the test merely verifies the worker starts and is responsive.

  test "session_expiry_worker starts and is alive" do
    # Compliance.SessionExpiryWorker does not exist yet → fails with UndefinedFunctionError
    {:ok, pid} = Compliance.SessionExpiryWorker.start_link([])

    assert is_pid(pid), "start_link/1 must return {:ok, pid}"
    assert Process.alive?(pid), "worker process must be alive after start_link"
  end

  # ─── Test 18: tick emits audit for expired unrevoked sessions only ─────────────
  #
  # AC9 — When :tick is received:
  #   - DB returns 2 expired sessions (one with revoked_at set, one without)
  #   - Worker calls AuditWriter.log/6 exactly once with action=compliance_session_expired
  #     for the unrevoked session
  #   - The revoked session (simulated by not including it in the returned rows,
  #     since the DB query already filters WHERE revoked_at IS NULL) is NOT audited
  #
  # Implementation note: the DB query in the real worker already filters
  #   WHERE expires_at <= NOW() AND revoked_at IS NULL
  # so FakeRepoW only returns unrevoked expired sessions. The "2 expired sessions,
  # 1 revoked" scenario is implemented by having FakeRepoW return exactly 1 row
  # (the unrevoked one), matching the query semantics.

  test "session_expiry_worker_emits_audit_for_expired_unrevoked", %{bucket: bucket} do
    # Configure FakeRepoW to return 1 expired unrevoked session id
    unrevoked_id = "session-uuid-unrevoked-001"
    Application.put_env(:compliance, :__test_expired_session_ids__, [unrevoked_id])

    {:ok, pid} = Compliance.SessionExpiryWorker.start_link([])

    # Send :tick directly to trigger immediate scan without waiting 3600s
    send(pid, :tick)

    # Allow the handle_info to process (deterministic: no sleep needed since
    # FakeRepoW and FakeAuditWriter are synchronous; use Process.sleep(10) as
    # a minimal yield so the handle_info callback has run)
    Process.sleep(10)

    calls = Agent.get(bucket, & &1)

    assert length(calls) == 1,
           "expected exactly 1 AuditWriter.log call for 1 expired session, got #{length(calls)}"

    [call] = calls

    assert call.action == "compliance_session_expired",
           "expected action=compliance_session_expired, got #{inspect(call.action)}"

    assert call.target_type == "compliance_session",
           "expected target_type=compliance_session, got #{inspect(call.target_type)}"

    assert call.target_id == unrevoked_id,
           "expected target_id=#{unrevoked_id}, got #{inspect(call.target_id)}"

    assert call.actor_user_id == "system:compliance_worker",
           "expected actor_user_id=system:compliance_worker, got #{inspect(call.actor_user_id)}"

    assert call.outcome == "success",
           "expected outcome=success, got #{inspect(call.outcome)}"
  end

  # ─── Test 19: crash and restart resumes — Stateless (Option C) ───────────────
  #
  # AC9 — Persistenz-Strategie: Option C — Stateless.
  #
  # When the worker crashes (Process.exit(:kill)), the supervisor must restart it.
  # The restarted worker must:
  #   - Be alive (Process.alive?)
  #   - Accept a subsequent :tick (verify no error is raised)
  #
  # Because the worker is stateless, no state recovery is tested — the restart
  # merely verifies the supervisor's one_for_one strategy brings it back.
  #
  # Strategy: use start_supervised/2 so ExUnit handles teardown atomically,
  # avoiding the TOCTOU race of a manual on_exit + Supervisor.stop call.
  # The worker is started anonymous (no name) so parallel test runs don't clash.
  # We use Process.monitor + assert_receive for deterministic crash detection,
  # then poll Supervisor.which_children (max 10 × 10ms) instead of a hard sleep.

  test "session_expiry_worker_crash_restart_resumes" do
    # Start a local supervisor via start_supervised — ExUnit cleans it up after
    # the test function returns, so there is no on_exit race with a named atom.
    sup_spec = %{
      id: :test_expiry_sup,
      start: {Supervisor, :start_link, [
        [{Compliance.SessionExpiryWorker, []}],
        [strategy: :one_for_one]
      ]},
      type: :supervisor
    }
    sup_pid = start_supervised!(sup_spec)

    # Find the initial worker pid from the supervisor's children list
    [{_, worker_pid, :worker, _}] = Supervisor.which_children(sup_pid)

    assert is_pid(worker_pid), "SessionExpiryWorker must be a child of the test supervisor"
    assert Process.alive?(worker_pid), "worker must be alive before crash"

    # Monitor the worker before killing it to get a deterministic DOWN signal
    ref = Process.monitor(worker_pid)
    Process.exit(worker_pid, :kill)

    # Wait for the DOWN signal — confirms the crash completed before we poll
    assert_receive {:DOWN, ^ref, :process, ^worker_pid, :killed}, 500

    # Poll until the supervisor restarts a new worker (max 10 × 10ms = 100ms).
    # This is deterministic: we only proceed once the child is actually alive,
    # so there is no fixed-time sleep that could be too short.
    new_pid =
      Enum.reduce_while(1..10, nil, fn _i, _acc ->
        case Supervisor.which_children(sup_pid) do
          [{_, pid, :worker, _}] when is_pid(pid) and pid != worker_pid ->
            {:halt, pid}
          _ ->
            Process.sleep(10)
            {:cont, nil}
        end
      end)

    assert is_pid(new_pid), "supervisor must have restarted SessionExpiryWorker within 100ms"
    assert Process.alive?(new_pid), "restarted worker must be alive"
    assert new_pid != worker_pid, "restarted worker must be a different pid"

    # Send :tick and synchronise via :sys.get_state/1 (blocks until handle_info completes)
    send(new_pid, :tick)
    :sys.get_state(new_pid)

    assert Process.alive?(new_pid),
           "restarted worker must remain alive after processing a :tick"
  end

  # ─── Test 20 (AC13): tick with no expired sessions emits no audit ─────────────
  #
  # AC13 — When :tick is received and no sessions are expired, AuditWriter.log/6
  # must NOT be called. This verifies the worker is a no-op on an empty scan result.

  test "tick with no expired sessions emits no audit", %{bucket: bucket} do
    # Ensure FakeRepoW returns an empty list (already the default, but make explicit)
    Application.put_env(:compliance, :__test_expired_session_ids__, [])

    {:ok, pid} = Compliance.SessionExpiryWorker.start_link([])

    # Trigger an immediate tick
    send(pid, :tick)

    # :sys.get_state/1 blocks until handle_info(:tick, _) has returned,
    # so by the time this returns the callback has fully completed — no sleep needed.
    :sys.get_state(pid)

    calls = Agent.get(bucket, & &1)

    assert calls == [],
           "expected no AuditWriter.log calls when no sessions expire, got #{length(calls)}"
  end
end
