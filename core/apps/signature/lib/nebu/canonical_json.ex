defmodule Nebu.CanonicalJson do
  @moduledoc """
  Canonical JSON encoding for Nebu event signing and hashing.

  Produces deterministic JSON output regardless of map insertion order:
  - All map keys are converted to strings and sorted alphabetically
  - Nested maps are recursively normalized
  - Uses `Jason.OrderedObject` to guarantee key order during encoding,
    because Erlang maps do not preserve insertion order for maps with >32 keys

  Architecture Rule #8: This module is the single canonical JSON implementation
  in the system. All code that needs deterministic JSON (event IDs, signatures)
  MUST use this module.
  """

  @doc """
  Encodes a map to canonical JSON with alphabetically sorted string keys.

  ## Examples

      iex> Nebu.CanonicalJson.encode!(%{"b" => 2, "a" => 1})
      ~S({"a":1,"b":2})

      iex> Nebu.CanonicalJson.encode!(%{b: 2, a: 1})
      ~S({"a":2,"b":2})

  """
  @spec encode!(map() | list() | term()) :: binary()
  def encode!(value) do
    value
    |> normalize()
    |> Jason.encode!()
  end

  # Recursively converts all map keys to strings and sorts them alphabetically.
  # Returns a Jason.OrderedObject to preserve sort order during JSON encoding.
  @spec normalize(term()) :: term()
  defp normalize(map) when is_map(map) do
    values =
      map
      |> Enum.map(fn {k, v} -> {to_string(k), normalize(v)} end)
      |> Enum.sort_by(fn {k, _} -> k end)

    %Jason.OrderedObject{values: values}
  end

  defp normalize(list) when is_list(list), do: Enum.map(list, &normalize/1)
  defp normalize(value), do: value
end
