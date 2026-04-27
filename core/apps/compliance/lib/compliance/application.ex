defmodule Compliance.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Compliance.AuditWriter is stateless (Option C — no GenServer state, no ETS).
    # Each log/6 call opens its own Repo.transaction/1.
    #
    # Compliance.SessionExpiryWorker (Story 5.5) scans for expired compliance sessions
    # once per hour and emits audit events. It is stateless (Option C) — a supervisor
    # restart schedules a new tick immediately.
    #
    # Workers are configured via Application env so tests can inject an empty list
    # (see config/test.exs) without the worker occupying the global name slot.
    # Production uses the default list defined here.
    children =
      Application.get_env(:compliance, :workers, [
        {Compliance.SessionExpiryWorker, [name: Compliance.SessionExpiryWorker]}
      ])

    opts = [strategy: :one_for_one, name: Compliance.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
