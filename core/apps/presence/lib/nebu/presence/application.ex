defmodule Nebu.Presence.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Placeholder — Presence Manager added in Epic 4
    ]

    opts = [strategy: :one_for_one, name: Nebu.Presence.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
