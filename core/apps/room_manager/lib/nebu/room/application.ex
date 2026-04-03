defmodule Nebu.Room.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Horde.Registry,
       [name: Nebu.Room.Registry, keys: :unique, members: :auto]},
      {Horde.DynamicSupervisor,
       [name: Nebu.Room.HordeSupervisor, strategy: :one_for_one, members: :auto]}
    ]

    opts = [strategy: :one_for_one, name: Nebu.Room.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
