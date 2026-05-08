defmodule Nebu.Search.DB do
  @moduledoc """
  PostgreSQL persistence for full-text search queries.

  Story 11.2: defines the canonical SQL contract for membership-scoped search.
  Story 11.3: wires this module to the SearchMessages gRPC handler.

  Uses raw SQL via Ecto.Adapters.SQL.query/3 — consistent with Nebu.Room.DB pattern.
  Membership filter: left_at IS NULL (NOT membership = 'join' — there is no membership column).
  Encrypted rooms (rooms with a m.room.encryption state event) are excluded from search results.
  """

  @sql_search_messages """
  SELECT
    e.event_id,
    e.room_id,
    e.sender,
    e.event_type,
    e.content,
    e.origin_server_ts,
    ts_rank_cd(e.search_vector, websearch_to_tsquery('pg_catalog.simple', $2)) AS rank
  FROM events e
  WHERE
    e.search_vector @@ websearch_to_tsquery('pg_catalog.simple', $2)
    AND e.event_type = 'm.room.message'
    AND e.room_id IN (
      SELECT room_id FROM room_members
      WHERE user_id = $1 AND left_at IS NULL
    )
    AND NOT EXISTS (
      SELECT 1 FROM events enc
      WHERE enc.room_id = e.room_id
        AND enc.event_type = 'm.room.encryption'
        AND (enc.state_key = '' OR enc.state_key IS NULL)
    )
  ORDER BY rank DESC, e.origin_server_ts DESC
  LIMIT $3
  OFFSET $4
  """

  @doc """
  Returns the canonical SQL string used for membership-scoped full-text search.

  This function exists primarily to support structural testing (AC2): callers can
  inspect the SQL to verify that membership enforcement happens at the SQL layer
  (a subquery on room_members WHERE left_at IS NULL) and not as an application-layer
  post-filter.
  """
  @spec sql_search_messages() :: String.t()
  def sql_search_messages, do: @sql_search_messages

  @doc """
  Executes a full-text search scoped to rooms where `user_id` is an active member
  (left_at IS NULL). Encrypted rooms (rooms that have an m.room.encryption state event)
  are excluded.

  SECURITY: `user_id` MUST come from the validated session (gRPC metadata or JWT claim),
  never from the request payload. Passing a caller-supplied user_id bypasses all
  membership enforcement and enables cross-room IDOR.

  Parameters:
    - user_id: the Matrix user ID of the searcher (from session, NOT from request body)
    - term: the search term (passed through websearch_to_tsquery)
    - limit: max results to return (caller should clamp to ≤ 100)
    - offset: pagination offset (caller should validate ≥ 0)

  Returns `{:ok, [map()]}` on success where each map has string keys:
    event_id, room_id, sender, event_type, content, origin_server_ts, rank

  Returns `{:error, reason}` on DB error.
  """
  @spec search_messages(String.t(), String.t(), pos_integer(), non_neg_integer()) ::
          {:ok, [map()]} | {:error, term()}
  def search_messages(user_id, term, limit, offset) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_search_messages, [user_id, term, limit, offset]) do
      {:ok, %{columns: cols, rows: rows}} ->
        results = Enum.map(rows, fn row -> cols |> Enum.zip(row) |> Map.new() end)
        {:ok, results}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
