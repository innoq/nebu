defmodule Compliance.SessionExpiryWorker do
  @moduledoc """
  Background GenServer that scans for expired compliance sessions and emits
  an audit event for each one, then marks it as revoked.

  ## Persistenz-Strategie: Option C — Stateless

  The worker holds no state in ETS or DB. If it crashes, the supervisor
  restarts it and a new :tick is scheduled immediately via Process.send_after
  in init/1. Missed expiry checks (at most 1 hour delay) are acceptable for MVP.

  ## Scan cadence

  One :tick per @tick_ms (default: 3 600 000 ms = 1 hour). Each :tick runs a
  point-in-time scan — it processes all sessions where:
    expires_at <= NOW() AND revoked_at IS NULL

  After emitting audit for each expired session, revoked_at is set to NOW()
  to prevent double-auditing on the next tick (idempotency via revoked_at).

  ## Actor identity

  Audit records are written with actor_user_id = "system:compliance_worker"
  because no real user initiates the expiry — the event is time-driven.
  """

  use GenServer
  require Logger

  # 1 hour in milliseconds
  @tick_ms 3_600_000

  # Configurable audit writer for test injection. Production uses Compliance.AuditWriter.
  defp audit_writer, do: Application.get_env(:compliance, :audit_writer, Compliance.AuditWriter)

  # Configurable repo for test injection. Production uses Nebu.Repo.
  defp repo, do: Application.get_env(:compliance, :repo, Nebu.Repo)

  # ─── Public API ──────────────────────────────────────────────────────────────

  # opts is forwarded to GenServer.start_link/3.
  # Production callers (Compliance.Application) pass [name: Compliance.SessionExpiryWorker]
  # so the worker is discoverable by name. Test callers pass [] or omit opts entirely,
  # which starts an anonymous process — safe to run multiple instances concurrently.
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, opts)
  end


  # ─── GenServer callbacks ──────────────────────────────────────────────────────

  @impl true
  def init(_opts) do
    Process.send_after(self(), :tick, @tick_ms)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:tick, state) do
    scan_and_audit_expired_sessions()
    Process.send_after(self(), :tick, @tick_ms)
    {:noreply, state}
  end

  # ─── Private helpers ──────────────────────────────────────────────────────────

  defp scan_and_audit_expired_sessions do
    # SELECT expired, unrevoked sessions. LIMIT 1000 caps each scan pass.
    %{rows: rows} =
      repo().query!(
        """
        SELECT id FROM compliance_sessions
         WHERE expires_at <= NOW()
           AND revoked_at IS NULL
         LIMIT 1000
        """,
        []
      )

    Enum.each(rows, fn [session_id] ->
      # Emit audit before setting revoked_at. If this process crashes between the
      # audit and the UPDATE, the next tick will process the row again — double
      # audit is preferable to missed audit for a compliance trail.
      audit_writer().log(
        "system:compliance_worker",
        "compliance_session_expired",
        "compliance_session",
        session_id,
        %{},
        "success"
      )

      # Mark as revoked to prevent re-processing on the next tick.
      repo().query!(
        "UPDATE compliance_sessions SET revoked_at = NOW() WHERE id = $1",
        [session_id]
      )
    end)

    if length(rows) > 0 do
      Logger.info("SessionExpiryWorker: expired #{length(rows)} compliance session(s)")
    end
  rescue
    e ->
      Logger.error("SessionExpiryWorker: scan_and_audit_expired_sessions failed",
        error: inspect(e)
      )
  end
end
