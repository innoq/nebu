defmodule Nebu.Session.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # ETS table for active user sessions (hot-path for /sync lookups).
    # Created here (owned by Application process) so it survives
    # Nebu.Session.EtsStore GenServer crashes/restarts.
    # Type :set auto-upserts on same key. Access :public allows any process.
    # Guard prevents ArgumentError if the Application is restarted in the same
    # VM (e.g. hot-code reload or test framework restart) — Story 4-4 pattern.
    if :ets.whereis(:NebuSessions) == :undefined do
      :ets.new(:NebuSessions, [:named_table, :set, :public])
    end

    children = [
      Nebu.Session.EtsStore
    ]

    opts = [strategy: :one_for_one, name: Nebu.Session.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
