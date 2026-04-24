defmodule Compliance.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Compliance.AuditWriter is stateless (Option C — no GenServer state, no ETS).
    # Each log/6 call opens its own Repo.transaction/1. Supervisor therefore
    # starts with no children; the Application entry-point exists only so the
    # umbrella OTP app can be started/stopped cleanly.
    children = []

    opts = [strategy: :one_for_one, name: Compliance.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
