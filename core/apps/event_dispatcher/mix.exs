defmodule Nebu.Event.MixProject do
  use Mix.Project

  def project do
    [
      app: :event_dispatcher,
      version: "0.1.0",
      build_path: "../../_build",
      config_path: "../../config/config.exs",
      deps_path: "../../deps",
      lockfile: "../../mix.lock",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger, :inets],
      mod: {Nebu.Event.Application, []}
    ]
  end

  defp deps do
    [
      {:grpc, "~> 0.8"},
      {:jason, "~> 1.4"},
      {:session_manager, in_umbrella: true},
      {:room_manager, in_umbrella: true},
      {:presence, in_umbrella: true}
    ]
  end
end
