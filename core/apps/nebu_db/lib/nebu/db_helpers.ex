defmodule Nebu.DB.Helpers do
  @moduledoc "Shared DB utility functions used across all apps."

  @doc "Current UTC time in milliseconds (BIGINT for PostgreSQL)."
  @spec now_ms() :: integer()
  def now_ms, do: System.system_time(:millisecond)
end
