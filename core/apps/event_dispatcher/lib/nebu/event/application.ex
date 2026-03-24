defmodule Nebu.Event.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Nebu.Health.Server,
      {GRPC.Server.Supervisor, endpoint: Nebu.EventDispatcher.Endpoint, port: 9000, start_server: true}
    ]

    opts = [strategy: :one_for_one, name: Nebu.Event.Supervisor]
    result = Supervisor.start_link(children, opts)

    # Fire-and-forget: does not block or crash supervisor on failure (AC #4)
    Task.start(fn -> Nebu.NodeRegistration.register_with_gateway() end)

    result
  end
end
