defmodule Nebu.EventId do
  @moduledoc """
  Generates Matrix Room Version 6 content-hash Event IDs.

  Format: `"$" <> Base64url(SHA-256(canonical_json(event \\ {signatures, unsigned})))`

  Spec: Matrix Room Version 6+ (https://spec.matrix.org/v1.x/rooms/v6/)

  The event ID is derived exclusively from the event content — not assigned by the server.
  This makes every event ID deterministic, verifiable, and content-addressable.

  Architecture rule #7: All event IDs in Nebu MUST be generated via `Nebu.EventId.generate/1`.
  Architecture rule #8: This module is the single canonical JSON implementation in the system.
  """

  @doc """
  Generates a content-hash Event ID for a Matrix event.

  Strips `signatures` and `unsigned` fields (both atom and string key forms), serializes to
  canonical JSON (alphabetically sorted string keys, no whitespace), computes SHA-256, and
  returns `"$" <> Base64url_no_padding(hash)`.

  ## Examples

      iex> id = Nebu.EventId.generate(%{"type" => "m.room.message"})
      iex> String.starts_with?(id, "$")
      true

      iex> id1 = Nebu.EventId.generate(%{"b" => 2, "a" => 1})
      iex> id2 = Nebu.EventId.generate(%{"a" => 1, "b" => 2})
      iex> id1 == id2
      true

  """
  @spec generate(map()) :: String.t()
  def generate(event) when is_map(event) do
    hash =
      event
      |> Map.drop(["signatures", "unsigned", :signatures, :unsigned])
      |> canonical_json()
      |> then(&:crypto.hash(:sha256, &1))

    "$" <> Base.url_encode64(hash, padding: false)
  end

  @doc """
  Verifies that an event ID matches the event content.

  Recomputes the ID via `generate/1` and compares it to the given `event_id`.
  Returns `true` if they match, `false` otherwise.

  ## Examples

      iex> event = %{"type" => "m.room.message"}
      iex> id = Nebu.EventId.generate(event)
      iex> Nebu.EventId.verify(event, id)
      true

      iex> tampered = %{"type" => "m.room.message", "content" => "TAMPERED"}
      iex> Nebu.EventId.verify(tampered, id)
      false

  """
  @spec verify(map(), String.t()) :: boolean()
  def verify(event, event_id) when is_map(event) and is_binary(event_id) do
    generate(event) == event_id
  end

  # Converts the map to canonical JSON: string keys sorted alphabetically, no whitespace.
  # Uses Jason.OrderedObject to guarantee key ordering in the JSON output,
  # because Erlang maps do not preserve insertion order for maps with >32 keys.
  @spec canonical_json(map()) :: binary()
  defp canonical_json(map) do
    map
    |> normalize_keys()
    |> Jason.encode!()
  end

  # Recursively converts all map keys to strings and sorts them alphabetically.
  # Returns a Jason.OrderedObject to preserve sort order during JSON encoding,
  # since Map.new/1 does not guarantee iteration order for large maps (>32 keys).
  defp normalize_keys(map) when is_map(map) do
    values =
      map
      |> Enum.map(fn {k, v} -> {to_string(k), normalize_keys(v)} end)
      |> Enum.sort_by(fn {k, _} -> k end)

    %Jason.OrderedObject{values: values}
  end

  defp normalize_keys(list) when is_list(list), do: Enum.map(list, &normalize_keys/1)
  defp normalize_keys(value), do: value
end
