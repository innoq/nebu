defmodule Nebu.Room.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Placeholder — Room GenServer processes added in Epic 4
    ]

    opts = [strategy: :one_for_one, name: Nebu.Room.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
