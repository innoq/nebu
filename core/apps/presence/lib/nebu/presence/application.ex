defmodule Nebu.Presence.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # ETS table for presence state.
    # Created here (owned by Application process) so it survives
    # Nebu.Presence.Manager GenServer crashes/restarts.
    # Type :set auto-upserts on same key. Access :public allows any process.
    # Guard prevents ArgumentError if the Application is restarted in the same VM
    # (e.g. hot-code reload or test framework restart).
    if :ets.whereis(:NebuPresence) == :undefined do
      :ets.new(:NebuPresence, [:named_table, :set, :public])
    end

    # Start :pg scope for presence broadcast (ADR-002, ADR-005).
    # :pg is OTP built-in (OTP 23+) — no external dependency.
    # Handle {:error, {:already_started, _}} gracefully in case another
    # umbrella app starts :pg first (e.g. Nebu.Room.Application).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
    end

    children = [
      Nebu.Presence.Manager
    ]

    opts = [strategy: :one_for_one, name: Nebu.Presence.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
