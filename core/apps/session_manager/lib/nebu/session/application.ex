defmodule Nebu.Session.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Placeholder — Session Manager ETS store added in Epic 4
    ]

    opts = [strategy: :one_for_one, name: Nebu.Session.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
