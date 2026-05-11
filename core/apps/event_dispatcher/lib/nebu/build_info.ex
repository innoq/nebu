defmodule Nebu.BuildInfo do
  @moduledoc """
  Build-time metadata for the Nebu core.

  Values are populated at Docker build time via ARG → ENV → Application env.
  Falls back to "unknown" when built/run locally without those env vars.
  """

  def get do
    info = Application.get_env(:event_dispatcher, :build_info, %{})
    %{
      component:  "core",
      version:    Map.get(info, :version,    System.get_env("RELEASE_VERSION", "unknown")),
      git_commit: Map.get(info, :git_commit, System.get_env("GIT_COMMIT",      "unknown")),
      build_time: Map.get(info, :build_time, System.get_env("BUILD_TIME",      "unknown"))
    }
  end
end
