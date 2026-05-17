defmodule Nebu.Health do
  @moduledoc """
  Health check module for the Nebu Elixir core.

  Returns a health map with overall status, load_factor, version, node name,
  cluster_nodes (Story 13-6), and per-component statuses.
  Used by the HTTP health endpoint on port 4000.
  """

  @version "0.1.0"

  @doc """
  Returns a health map for the current node.

  Overall status:
  - "UP"       — all components healthy
  - "DEGRADED" — at least one component degraded, none down
  - "DOWN"     — at least one critical component down

  `cluster_nodes` lists all known Erlang nodes connected to this node
  (Story 13-6 AC4 / Godog cluster smoke check). Empty list in single-node mode.
  """
  def check do
    components = %{
      database: check_database(),
      room_registry: check_room_registry(),
      event_bus: check_event_bus()
    }

    %{
      status: overall_status(components),
      load_factor: 1.0,
      version: @version,
      node: to_string(node()),
      cluster_nodes: cluster_nodes(),
      components: components
    }
  end

  @doc false
  def overall_status(components) do
    statuses = components |> Map.values() |> Enum.map(& &1.status)

    cond do
      "DOWN" in statuses -> "DOWN"
      "DEGRADED" in statuses -> "DEGRADED"
      true -> "UP"
    end
  end

  # Story 13-6: Returns a list of connected Erlang node names as strings.
  # In single-node mode (no libcluster), Node.list() returns [].
  # In a 2-node cluster, returns the peer node(s), e.g. ["nebu@core2"].
  defp cluster_nodes do
    Node.list() |> Enum.map(&to_string/1)
  end

  # MVP stubs — real checks wired in Epic 2/4 stories
  defp check_database, do: %{status: "UP"}
  defp check_room_registry, do: %{status: "UP", room_count: 0}
  defp check_event_bus, do: %{status: "UP", connected_gateways: 0}
end
