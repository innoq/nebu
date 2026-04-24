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
            nebu_db: :permanent,
            event_dispatcher: :permanent,
            permissions: :permanent,
            presence: :permanent,
            room_manager: :permanent,
            session_manager: :permanent,
            signature: :permanent,
            compliance: :permanent
          ]
        ]
      ]
    ]
  end

  defp deps do
    [
      {:ecto_sql, "~> 3.12"},
      {:postgrex, "~> 0.19"}
    ]
  end
end
