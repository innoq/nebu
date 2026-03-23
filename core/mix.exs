defmodule Nebu.MixProject do
  use Mix.Project

  def project do
    [
      apps_path: "apps",
      version: "0.1.0",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: [
        nebu: [
          applications: [
            event_dispatcher: :permanent,
            permissions: :permanent,
            presence: :permanent,
            room_manager: :permanent,
            session_manager: :permanent,
            signature: :permanent
          ]
        ]
      ]
    ]
  end

  defp deps do
    []
    # Dependencies added in subsequent stories:
    # Story 1.3: ecto_sql, postgrex (database)
    # Story 1.6: grpc, protobuf (gRPC)
    # Story 4.x: horde, libcluster (clustering)
  end
end
