defmodule Nebu.Permissions.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Placeholder — System roles and room power levels added in Epic 4
    ]

    opts = [strategy: :one_for_one, name: Nebu.Permissions.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
