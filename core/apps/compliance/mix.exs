defmodule Compliance.MixProject do
  use Mix.Project

  def project do
    [
      app: :compliance,
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
      extra_applications: [:logger],
      mod: {Compliance.Application, []}
    ]
  end

  defp deps do
    [{:nebu_db, in_umbrella: true}]
  end
end
