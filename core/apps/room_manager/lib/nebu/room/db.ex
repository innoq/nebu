defmodule Nebu.Room.DB do
  @moduledoc """
  PostgreSQL persistence layer for Room GenServers.

  Uses raw SQL via `Ecto.Adapters.SQL.query/3` — no Ecto schemas or changesets.
  All timestamps stored as BIGINT milliseconds per architecture enforcement rule #1.
  """

  @sql_load_members """
  SELECT user_id FROM room_members
  WHERE room_id = $1 AND left_at IS NULL
  """

  @sql_check_room_exists """
  SELECT room_id, created_at FROM rooms WHERE room_id = $1
  """

  @sql_insert_room """
  INSERT INTO rooms (room_id, visibility, created_at)
  VALUES ($1, 'private', $2)
  ON CONFLICT (room_id) DO NOTHING
  RETURNING created_at
  """

  @sql_insert_member """
  INSERT INTO room_members (room_id, user_id, joined_at)
  VALUES ($1, $2, $3)
  ON CONFLICT (room_id, user_id) DO UPDATE
  SET left_at = NULL, joined_at = EXCLUDED.joined_at
  WHERE room_members.left_at IS NOT NULL
  RETURNING user_id
  """

  @sql_soft_delete_member """
  UPDATE room_members SET left_at = $3
  WHERE room_id = $1 AND user_id = $2 AND left_at IS NULL
  RETURNING user_id
  """

  @doc """
  Loads all active members for the given `room_id`.

  Returns `{:ok, [user_id], created_at_ms}` (possibly empty list) if the room exists.
  Returns `{:error, :not_found}` if the room does not exist in the `rooms` table.
  Returns `{:error, reason}` on DB error.
  """
  @spec load_members(String.t()) :: {:ok, [String.t()], integer()} | {:error, :not_found | term()}
  def load_members(room_id) do
    # First check if room exists at all (also fetches created_at)
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_check_room_exists, [room_id]) do
      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:ok, %{rows: [[_, created_at_ms]]}} ->
        # Room exists — load members
        case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_load_members, [room_id]) do
          {:ok, %{rows: rows}} ->
            user_ids = Enum.map(rows, fn [uid] -> uid end)
            {:ok, user_ids, created_at_ms}

          {:error, reason} ->
            {:error, reason}
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  @doc """
  Inserts a new room into the `rooms` table.

  Returns `{:ok, created_at_ms}` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @spec insert_room(String.t()) :: {:ok, integer()} | {:error, term()}
  def insert_room(room_id) do
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_insert_room, [room_id, now_ms]) do
      {:ok, %{rows: [[created_at_ms]]}} ->
        {:ok, created_at_ms}

      {:ok, %{rows: []}} ->
        # ON CONFLICT DO NOTHING — room already exists (race condition); treat as ok
        {:ok, now_ms}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @doc """
  Inserts a member into `room_members`.

  Returns `:ok` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @spec insert_member(String.t(), String.t()) :: :ok | {:error, term()}
  def insert_member(room_id, user_id) do
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_insert_member, [room_id, user_id, now_ms]) do
      {:ok, %{rows: [[_]]}} ->
        :ok

      {:ok, %{rows: []}} ->
        # ON CONFLICT DO NOTHING — already an active member
        :ok

      {:error, reason} ->
        {:error, reason}
    end
  end

  @doc """
  Soft-deletes a member from `room_members` by setting `left_at`.

  Returns `:ok` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @spec delete_member(String.t(), String.t()) :: :ok | {:error, term()}
  def delete_member(room_id, user_id) do
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_soft_delete_member, [room_id, user_id, now_ms]) do
      {:ok, %{rows: [[_]]}} ->
        :ok

      {:ok, %{rows: []}} ->
        # No active row matched — user was not an active member
        {:error, :not_member}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @sql_insert_event """
  INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts, signatures)
  VALUES ($1, $2, $3, $4, $5, $6, $7)
  """

  @doc """
  Inserts a signed event into the `events` append-only table.

  Expects the event map to have string keys: `"event_id"`, `"room_id"`, `"sender"`,
  `"type"`, `"content"`, `"origin_server_ts"`, and optionally `"signatures"`.

  JSONB columns (`content`, `signatures`) are JSON-encoded before passing to Postgrex.

  Returns `:ok` on success or `{:error, reason}` on DB failure.
  """
  @spec insert_event(map()) :: :ok | {:error, term()}
  def insert_event(event) do
    with {:ok, content_json} <- Jason.encode(event["content"]),
         {:ok, sigs_json} <- encode_nullable(event["signatures"]) do
      case Ecto.Adapters.SQL.query(
             Nebu.Repo,
             @sql_insert_event,
             [
               event["event_id"],
               event["room_id"],
               event["sender"],
               event["type"],
               content_json,
               event["origin_server_ts"],
               sigs_json
             ]
           ) do
        {:ok, _} -> :ok
        {:error, reason} -> {:error, reason}
      end
    end
  end

  # Encodes a value to JSON string, or passes nil through unchanged.
  # Used for optional JSONB columns like `signatures`.
  defp encode_nullable(nil), do: {:ok, nil}
  defp encode_nullable(value), do: Jason.encode(value)

  @doc """
  Fetches paginated events from the `events` table for the given room.

  Pagination is keyset-based on `(origin_server_ts, event_id)` — both fields
  together provide a stable, unique cursor that avoids duplicates across pages.

  Token format: `"v1_" <> Base.url_encode64(ts_str <> ":" <> event_id, padding: false)`
  Empty token means "start from the beginning" (direction-dependent).

  Direction "b" (backward): newest first (DESC origin_server_ts, DESC event_id).
  Direction "f" (forward):  oldest first (ASC  origin_server_ts, ASC  event_id).

  Returns `{:ok, events, next_batch, prev_batch}` where:
  - `events` is a list of maps with string keys matching the `events` table columns
  - `next_batch` is the cursor for the next page (empty string if no more pages)
  - `prev_batch` is the cursor for the previous page (the from_token echoed back, or
    a token for the first event returned)
  """
  @spec fetch_events(String.t(), String.t(), integer(), String.t()) ::
          {:ok, [map()], String.t(), String.t()} | {:error, term()}
  def fetch_events(room_id, direction, limit, from_token) do
    {order, compare_op} =
      if direction == "f" do
        {"ASC", ">"}
      else
        {"DESC", "<"}
      end

    {where_clause, params} =
      case decode_token(from_token) do
        {:ok, cursor_ts, cursor_event_id} ->
          # Keyset: fetch events strictly before/after the cursor.
          # direction "b": WHERE (origin_server_ts, event_id) < (cursor_ts, cursor_event_id)
          # direction "f": WHERE (origin_server_ts, event_id) > (cursor_ts, cursor_event_id)
          clause = "(origin_server_ts, event_id) #{compare_op} ($2, $3)"
          {clause, [room_id, cursor_ts, cursor_event_id]}

        :empty ->
          {"TRUE", [room_id]}
      end

    param_offset = length(params)
    limit_param = "$#{param_offset + 1}"

    sql = """
    SELECT event_id, room_id, sender, event_type, content, origin_server_ts
    FROM events
    WHERE room_id = $1 AND #{where_clause}
    ORDER BY origin_server_ts #{order}, event_id #{order}
    LIMIT #{limit_param}
    """

    # Fetch limit+1 to detect whether a next page exists.
    all_params = params ++ [limit + 1]

    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, all_params) do
      {:ok, %{columns: cols, rows: rows}} ->
        all_events =
          Enum.map(rows, fn row ->
            cols
            |> Enum.zip(row)
            |> Map.new()
          end)

        {page_events, has_more} =
          if length(all_events) > limit do
            {Enum.take(all_events, limit), true}
          else
            {all_events, false}
          end

        next_batch =
          if has_more do
            last = List.last(page_events)
            encode_token(last["origin_server_ts"], last["event_id"])
          else
            ""
          end

        prev_batch =
          case page_events do
            [] ->
              from_token

            events ->
              # Use the last event in the returned list as the prev_batch cursor.
              # For dir="b" (newest first), last = oldest; for dir="f" (oldest first),
              # last = newest. In both cases this is the "boundary" for the next page.
              last = List.last(events)
              encode_token(last["origin_server_ts"], last["event_id"])
          end

        {:ok, page_events, next_batch, prev_batch}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp encode_token(ts, event_id) when is_integer(ts) and is_binary(event_id) do
    raw = "#{ts}:#{event_id}"
    "v1_" <> Base.url_encode64(raw, padding: false)
  end

  defp decode_token(""), do: :empty
  defp decode_token(nil), do: :empty

  defp decode_token("v1_" <> encoded) do
    case Base.url_decode64(encoded, padding: false) do
      {:ok, raw} ->
        case String.split(raw, ":", parts: 2) do
          [ts_str, event_id] ->
            case Integer.parse(ts_str) do
              {ts, ""} -> {:ok, ts, event_id}
              _ -> :empty
            end

          _ ->
            :empty
        end

      _ ->
        :empty
    end
  end

  defp decode_token(_), do: :empty
end
