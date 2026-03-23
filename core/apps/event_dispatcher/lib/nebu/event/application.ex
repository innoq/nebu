defmodule Nebu.Event.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {GRPC.Server.Supervisor, endpoint: Nebu.EventDispatcher.Endpoint, port: 9000, start_server: true}
    ]

    opts = [strategy: :one_for_one, name: Nebu.Event.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
