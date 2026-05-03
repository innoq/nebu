defmodule Nebu.Room.DB do
  @moduledoc """
  PostgreSQL persistence layer for Room GenServers.

  Uses raw SQL via `Ecto.Adapters.SQL.query/3` — no Ecto schemas or changesets.
  All timestamps stored as BIGINT milliseconds per architecture enforcement rule #1.
  """

  @behaviour Nebu.Room.DBBehaviour

  @sql_load_members """
  SELECT user_id FROM room_members
  WHERE room_id = $1 AND left_at IS NULL
  """

  @sql_check_room_exists """
  SELECT room_id, created_at, power_levels_json FROM rooms WHERE room_id = $1
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

  Returns `{:ok, [user_id], created_at_ms, power_levels_json}` (possibly empty list) if the room exists.
  Returns `{:error, :not_found}` if the room does not exist in the `rooms` table.
  Returns `{:error, reason}` on DB error.
  """
  @spec load_members(String.t()) ::
          {:ok, [String.t()], integer(), String.t()} | {:error, :not_found | term()}
  def load_members(room_id) do
    # First check if room exists at all (also fetches created_at and power_levels_json)
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_check_room_exists, [room_id]) do
      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:ok, %{rows: [[_, created_at_ms, power_levels_json]]}} ->
        # Room exists — load members
        case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_load_members, [room_id]) do
          {:ok, %{rows: rows}} ->
            user_ids = Enum.map(rows, fn [uid] -> uid end)
            pl_json = power_levels_json || "{}"
            {:ok, user_ids, created_at_ms, pl_json}

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

  @sql_get_rooms_for_user """
  SELECT room_id FROM room_members
  WHERE user_id = $1 AND left_at IS NULL
  """

  @doc """
  Returns all room IDs where `user_id` is currently an active member.

  Returns `{:ok, [room_id]}` — empty list if user has no active rooms.
  Returns `{:error, reason}` on DB error.
  """
  @spec get_rooms_for_user(String.t()) :: {:ok, [String.t()]} | {:error, term()}
  def get_rooms_for_user(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_rooms_for_user, [user_id]) do
      {:ok, %{rows: rows}} -> {:ok, Enum.map(rows, fn [rid] -> rid end)}
      {:error, reason} -> {:error, reason}
    end
  end

  @sql_set_power_levels """
  UPDATE rooms SET power_levels_json = $2 WHERE room_id = $1
  """

  @doc """
  Persists the `power_levels_json` string for the given `room_id`.

  Returns `:ok` on success or `{:error, reason}` on DB failure.
  """
  @spec set_power_levels(String.t(), String.t()) :: :ok | {:error, term()}
  def set_power_levels(room_id, power_levels_json) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_set_power_levels, [room_id, power_levels_json]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
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

  @sql_fetch_events_since """
  SELECT event_id, room_id, sender, event_type, content, origin_server_ts
  FROM events
  WHERE room_id = $1 AND origin_server_ts > $2
  ORDER BY origin_server_ts ASC
  LIMIT $3
  """

  @doc """
  Returns events in `room_id` with `origin_server_ts` strictly greater than
  the timestamp of `last_event_id`. Returns up to `limit` events in
  chronological order (ASC).

  If `last_event_id` is `nil` or `""`, uses `since_ts = 0` (return all events).
  If `get_event_timestamp/1` returns `{:error, :not_found}`, uses `since_ts = 0`
  (conservative fallback — treat as full sync).

  Returns `{:ok, [event_map]}` — empty list if no new events.
  """
  @spec fetch_events_since(String.t(), String.t() | nil, pos_integer()) ::
          {:ok, [map()]} | {:error, term()}
  def fetch_events_since(room_id, last_event_id, limit) do
    since_ts =
      cond do
        is_nil(last_event_id) or last_event_id == "" ->
          0

        true ->
          case get_event_timestamp(last_event_id) do
            {:ok, ts} -> ts
            {:error, _} -> 0
          end
      end

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_fetch_events_since, [room_id, since_ts, limit]) do
      {:ok, %{columns: cols, rows: rows}} ->
        events =
          Enum.map(rows, fn row ->
            cols |> Enum.zip(row) |> Map.new()
          end)

        {:ok, events}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @sql_get_event_ts "SELECT origin_server_ts FROM events WHERE event_id = $1 LIMIT 1"

  @doc """
  Returns the `origin_server_ts` for the given `event_id`.

  Returns `{:ok, integer()}` on success.
  Returns `{:error, :not_found}` if the event does not exist.
  Returns `{:error, reason}` on DB error.
  """
  @spec get_event_timestamp(String.t()) :: {:ok, integer()} | {:error, :not_found | term()}
  def get_event_timestamp(event_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_event_ts, [event_id]) do
      {:ok, %{rows: [[ts]]}} -> {:ok, ts}
      {:ok, %{rows: []}} -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

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

  @sql_load_room_settings """
  SELECT COALESCE(max_members, 0) FROM rooms WHERE room_id = $1
  """

  @doc """
  Loads mutable room settings (currently only max_members) for the given `room_id`.

  Returns `{:ok, max_members}` where max_members=0 means no limit.
  Returns `{:error, reason}` on DB error.

  Story 6.8: Called by Room.Server.init/1 to recover max_members after a GenServer restart.
  Fail-open: if this call fails, the GenServer defaults to 0 (no limit).
  """
  @spec load_room_settings(String.t()) :: {:ok, non_neg_integer()} | {:error, term()}
  def load_room_settings(room_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_load_room_settings, [room_id]) do
      {:ok, %{rows: [[max_members]]}} -> {:ok, max_members}
      {:ok, %{rows: []}} -> {:ok, 0}
      {:error, reason} -> {:error, reason}
    end
  end

  # room_id is the PRIMARY KEY of the rooms table — no LIMIT needed.
  @sql_get_room_status "SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1"

  @doc """
  Returns the archival status of `room_id` from the `rooms` table.

  Returns `{:ok, "active"}` for active (non-archived) rooms.
  Returns `{:ok, "archived"}` for archived rooms.
  Returns `{:error, :not_found}` when the room does not exist in the table.
  Returns `{:error, reason}` on DB error.

  Story 6.9: Called by Room.Server.init/1 before initialising state.
  When the result is `{:ok, "archived"}`, init/1 returns `{:stop, :normal}`.
  """
  @spec get_room_status(String.t()) :: {:ok, String.t()} | {:error, :not_found | term()}
  def get_room_status(room_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_room_status, [room_id]) do
      {:ok, %{rows: [[status]]}} -> {:ok, status}
      {:ok, %{rows: []}} -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc """
  Returns the room name from the most recent m.room.name event, or {:error, :not_found}.

  Content is stored as a JSONB string (Postgrex encodes the map as a JSON string before
  inserting, so the JSONB column contains a string value, not an object). The query handles
  both forms:
    - JSONB object  {"name": "…"}  → content->>'name' works directly
    - JSONB string  "{"name":"…"}" → (content#>>'{}')::jsonb->>'name' extracts via the
                                     text path operator first
  """
  @spec get_room_creator(String.t()) :: {:ok, String.t()} | {:error, :not_found | term()}
  def get_room_creator(room_id) do
    sql = """
    SELECT sender FROM events
    WHERE room_id = $1 AND event_type = 'm.room.create'
    ORDER BY origin_server_ts ASC LIMIT 1
    """
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, [room_id]) do
      {:ok, %{rows: [[sender]]}} when not is_nil(sender) -> {:ok, sender}
      {:ok, _} -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  @spec get_room_name(String.t()) :: {:ok, String.t()} | {:error, :not_found | term()}
  def get_room_name(room_id) do
    sql = """
    SELECT CASE
      WHEN jsonb_typeof(content) = 'object' THEN content->>'name'
      ELSE ((content#>>'{}')::jsonb)->>'name'
    END
    FROM events
    WHERE room_id = $1 AND event_type = 'm.room.name'
    ORDER BY origin_server_ts DESC LIMIT 1
    """
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, [room_id]) do
      {:ok, %{rows: [[name]]}} when not is_nil(name) -> {:ok, name}
      {:ok, _} -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end
end
