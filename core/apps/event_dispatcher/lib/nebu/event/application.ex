defmodule Nebu.Event.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # Story 13-6: Start libcluster Cluster.Supervisor when topologies are configured.
    # In test env and single-node mode, :libcluster topologies is not set — skip it.
    libcluster_children =
      case Application.get_env(:libcluster, :topologies) do
        nil -> []
        [] -> []
        topologies -> [{Cluster.Supervisor, [topologies, [name: Nebu.ClusterSupervisor]]}]
      end

    children =
      libcluster_children ++
        [
          Nebu.Health.Server,
          {GRPC.Server.Supervisor,
           endpoint: Nebu.EventDispatcher.Endpoint, port: 9000, start_server: true}
        ]

    opts = [strategy: :one_for_one, name: Nebu.Event.Supervisor]
    result = Supervisor.start_link(children, opts)

    # Fire-and-forget: does not block or crash supervisor on failure (AC #4)
    Task.start(fn -> Nebu.NodeRegistration.register_with_gateway() end)

    result
  end
end
