defmodule Nebu.Room.PowerLevels do
  @moduledoc """
  Pure functions for Matrix power level evaluation.
  All maps use string keys (architecture rule: no atom keys in DB-sourced maps).
  """

  @default_levels %{
    "ban" => 50,
    "kick" => 50,
    "invite" => 0,
    "redact" => 50,
    "state_default" => 50,
    "events_default" => 0,
    "users_default" => 0,
    "users" => %{},
    "events" => %{}
  }

  @doc "Returns the default Matrix power levels map (string keys)."
  @spec default_levels() :: map()
  def default_levels, do: @default_levels

  @doc """
  Returns the effective power level for `user_id` in the given `power_levels` map.

  Checks the `users` sub-map for a per-user override; falls back to `users_default`
  (default 0) when no override is present.
  """
  @spec get_user_level(map(), String.t()) :: integer()
  def get_user_level(power_levels, user_id) do
    users = Map.get(power_levels, "users", %{})
    Map.get(users, user_id, Map.get(power_levels, "users_default", 0))
  end

  @doc """
  Returns `true` if `user_id` has sufficient power to perform `action`.

  Actions and their required levels (taken from `power_levels` with defaults):
  - `:send_event`   — `events_default` (default 0)
  - `:invite`       — `invite`         (default 0)
  - `:kick`         — `kick`           (default 50)
  - `:ban`          — `ban`            (default 50)
  - `:change_state` — `state_default`  (default 50)
  """
  @spec can?(map(), String.t(), atom()) :: boolean()
  def can?(power_levels, user_id, action) do
    user_power = get_user_level(power_levels, user_id)
    required = required_level(power_levels, action)
    user_power >= required
  end

  defp required_level(power_levels, :send_event),
    do: Map.get(power_levels, "events_default", 0)

  defp required_level(power_levels, :invite),
    do: Map.get(power_levels, "invite", 0)

  defp required_level(power_levels, :kick),
    do: Map.get(power_levels, "kick", 50)

  defp required_level(power_levels, :ban),
    do: Map.get(power_levels, "ban", 50)

  defp required_level(power_levels, :change_state),
    do: Map.get(power_levels, "state_default", 50)
end
