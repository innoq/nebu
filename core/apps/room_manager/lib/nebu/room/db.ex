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

  @sql_get_recently_left_rooms_for_user """
  SELECT room_id FROM room_members
  WHERE user_id = $1 AND left_at IS NOT NULL
  """

  @doc """
  Returns all room IDs where `user_id` has left (left_at IS NOT NULL).

  Used by do_incremental_sync to include recently-left rooms in the initial
  fetch_delta_rooms check. Closes the race window where {new_leave} fires before
  the sync task subscribes to :pg groups, causing a 30 s long-poll delay.

  Returns `{:ok, [room_id]}` — empty list if user has no left rooms.
  Returns `{:error, reason}` on DB error.
  """
  @spec get_recently_left_rooms_for_user(String.t()) :: {:ok, [String.t()]} | {:error, term()}
  def get_recently_left_rooms_for_user(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_recently_left_rooms_for_user, [user_id]) do
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
  INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts, signatures, state_key)
  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
  """

  @doc """
  Inserts a signed event into the `events` append-only table.

  Expects the event map to have string keys: `"event_id"`, `"room_id"`, `"sender"`,
  `"type"`, `"content"`, `"origin_server_ts"`, and optionally `"signatures"` and
  `"state_key"` (Story 9-7: state events; nil/absent for regular events).

  JSONB columns (`content`, `signatures`) are JSON-encoded before passing to Postgrex.

  Returns `:ok` on success or `{:error, reason}` on DB failure.
  """
  @spec insert_event(map()) :: :ok | {:error, term()}
  def insert_event(event) do
    # Story 9-7: state_key is nil for regular (non-state) events and is stored
    # as NULL in the DB. State events always have a state_key — either "" (empty
    # string, the default for m.room.name, m.room.topic, m.room.join_rules, etc.)
    # or a user/server ID (e.g. for m.room.member). We pass the value through
    # unchanged so that "" is stored as "" (NOT NULL), making it visible to the
    # WHERE state_key IS NOT NULL query in get_generic_state_events/1.
    # (MAJOR-2 fix, code review story 9-7 — removing the erroneous "" -> nil branch)
    state_key = Map.get(event, "state_key")

    # Pass maps directly to Postgrex — Ecto+Postgrex encodes Elixir maps as JSONB
    # objects. Pre-encoding to a JSON string then passing to Postgrex causes double-
    # encoding: Postgrex treats the string as a JSON string literal (adds outer
    # quotes), so the DB stores a JSONB string instead of a JSONB object. That breaks
    # any JSONB path query (content->'m.relates_to'->>'event_id' etc.).
    case Ecto.Adapters.SQL.query(
           Nebu.Repo,
           @sql_insert_event,
           [
             event["event_id"],
             event["room_id"],
             event["sender"],
             event["type"],
             event["content"],
             event["origin_server_ts"],
             event["signatures"],
             state_key
           ]
         ) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @sql_fetch_events_since """
  SELECT event_id, room_id, sender, event_type, content, origin_server_ts, state_key
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

  @sql_fetch_event """
  SELECT event_id, room_id, sender, event_type, content, origin_server_ts, state_key
  FROM events
  WHERE event_id = $1 AND room_id = $2
  LIMIT 1
  """

  @doc """
  Fetches a single event by `event_id` scoped to `room_id`.

  Returns `{:ok, event_map}` on success.
  Returns `{:error, :not_found}` if the event does not exist in this room.
  Returns `{:error, reason}` on DB error.
  """
  @spec fetch_event(String.t(), String.t()) :: {:ok, map()} | {:error, :not_found | term()}
  def fetch_event(event_id, room_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_fetch_event, [event_id, room_id]) do
      {:ok, %{columns: cols, rows: [row]}} ->
        {:ok, cols |> Enum.zip(row) |> Map.new()}

      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @sql_event_in_room "SELECT 1 FROM events WHERE event_id = $1 AND room_id = $2 LIMIT 1"

  @doc """
  Returns `true` if `event_id` belongs to `room_id`, `false` otherwise.

  Used by `get_relations` to prevent cross-room event-existence probing.
  Fail-closed: DB errors return `false` so missing events yield 404, not 500.
  """
  @spec event_in_room?(String.t(), String.t()) :: boolean()
  def event_in_room?(event_id, room_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_event_in_room, [event_id, room_id]) do
      {:ok, %{rows: [_]}} -> true
      _                   -> false
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
    SELECT event_id, room_id, sender, event_type, content, origin_server_ts, state_key
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
  Atomically checks if a room is archived using SELECT FOR UPDATE inside a transaction.

  Serialises this check with archive_room_atomic/1's UPDATE transaction, closing
  the TOCTOU race window between archive_room_atomic/1 and send_event insert.

  Returns `{:ok, "active"}` for active rooms.
  Returns `{:ok, "archived"}` for archived rooms.
  Returns `{:error, :not_found}` when the room does not exist.
  Returns `{:error, reason}` on DB error.

  Story 9-9: TOCTOU fix — called by Room.Server.handle_call({:send_event, ...})
  before building the event map on a cache miss.
  """
  @spec check_room_status_for_update(String.t()) ::
          {:ok, String.t()} | {:error, :not_found | term()}
  def check_room_status_for_update(room_id) do
    result =
      Nebu.Repo.transaction(fn ->
        case Ecto.Adapters.SQL.query!(
               Nebu.Repo,
               "SELECT COALESCE(status, 'active') FROM rooms WHERE room_id = $1 FOR UPDATE",
               [room_id]
             ) do
          %{rows: [[status]]} -> status
          %{rows: []} -> Nebu.Repo.rollback(:not_found)
        end
      end)

    case result do
      {:ok, status} -> {:ok, status}
      {:error, :not_found} -> {:error, :not_found}
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

  @doc """
  Returns the content map of the authoritative m.room.create event for `room_id`.

  Queries the events table for the first m.room.create event (ASC origin_server_ts)
  and decodes its JSONB content. Handles both Postgrex JSONB forms:
    - JSONB object  {"creator":"…","room_version":"10","predecessor":{…}}
    - JSONB string  "{\"creator\":\"…\"}"  — doubly-encoded JSON string

  Returns `{:ok, content_map}` on success.
  Returns `{:error, :not_found}` when no m.room.create event exists.
  Returns `{:error, reason}` on DB / decode error.
  """
  @spec get_room_create_event(String.t()) :: {:ok, map()} | {:error, :not_found | term()}
  def get_room_create_event(room_id) do
    sql = """
    SELECT CASE
      WHEN jsonb_typeof(content) = 'object' THEN content::text
      ELSE (content#>>'{}')
    END
    FROM events
    WHERE room_id = $1 AND event_type = 'm.room.create'
    ORDER BY origin_server_ts ASC LIMIT 1
    """
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, [room_id]) do
      {:ok, %{rows: [[content_text]]}} when not is_nil(content_text) ->
        case Jason.decode(content_text) do
          {:ok, map} when is_map(map) -> {:ok, map}
          _                           -> {:error, :not_found}
        end

      {:ok, _} ->
        {:error, :not_found}

      {:error, reason} ->
        {:error, reason}
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

  @doc """
  Returns events in `room_id` that relate to `event_id`, optionally filtered by `rel_type`.

  Story 9-28: original 4-arity variant — always filters by rel_type, newest-first (DESC).
  Story 9-29: 5-arity variant with opts map for extended behaviour:
    - `rel_type` empty string = no rel_type filter (return all relation types)
    - opts `event_type`: filter by event_type; empty = all event types
    - opts `dir`: "b" (DESC, default) or "f" (ASC)

  Returns `{:ok, [event_map]}` — empty list if no matching events.
  Returns `{:error, reason}` on DB error.

  Story 9-28: used by GetRelations gRPC handler and attach_thread_aggregations.
  Story 9-29: extended with opts for dir and event_type filtering.
  """
  @spec fetch_events_by_relation(String.t(), String.t(), String.t(), pos_integer(), map()) ::
          {:ok, [map()]} | {:error, term()}
  # `limit` is clamped to a minimum of 1 internally — a caller passing 0 yields LIMIT 1.
  def fetch_events_by_relation(room_id, event_id, rel_type, limit, opts \\ %{}) do
    limit      = max(1, limit)
    event_type = Map.get(opts, :event_type, "")

    # dir is sanitised here: only "f" produces ASC; any other value (including unexpected
    # values) defaults to "DESC" so the DB layer is never exposed to an invalid ORDER BY.
    order_dir  =
      case Map.get(opts, :dir, "b") do
        "f" -> "ASC"
        _   -> "DESC"
      end

    # Build WHERE clauses dynamically.
    # Fixed params: $1=room_id, $2=event_id. Optional filter params follow.
    # LIMIT param index is determined after all optional filters are assigned.
    {extra_where, base_params, limit_idx} =
      []
      |> add_rel_type_clause(rel_type)
      |> add_event_type_clause(event_type)
      |> build_where_and_params([room_id, event_id], 3)

    sql = """
    SELECT event_id, room_id, sender, event_type, content, origin_server_ts, state_key
    FROM events
    WHERE room_id = $1
      AND content->'m.relates_to'->>'event_id' = $2
      #{extra_where}
    ORDER BY origin_server_ts #{order_dir}, event_id #{order_dir}
    LIMIT $#{limit_idx}
    """

    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, base_params ++ [limit]) do
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

  # ── Private helpers for fetch_events_by_relation SQL building ──────────────

  # Returns a clause list with a rel_type filter appended (unless rel_type is empty).
  defp add_rel_type_clause(clauses, ""), do: clauses
  defp add_rel_type_clause(clauses, rel_type), do: clauses ++ [{:rel_type, rel_type}]

  # Returns a clause list with an event_type filter appended (unless event_type is empty).
  defp add_event_type_clause(clauses, ""), do: clauses
  defp add_event_type_clause(clauses, event_type), do: clauses ++ [{:event_type, event_type}]

  # Converts a list of {kind, value} tuples into a SQL WHERE fragment and params list.
  # fixed_params contains the already-bound positional params ($1..$N-1).
  # next_idx starts as length(fixed_params) + 1.
  defp build_where_and_params(clauses, fixed_params, next_idx) do
    {where_parts, extra_params, final_idx} =
      Enum.reduce(clauses, {[], [], next_idx}, fn {kind, val}, {parts, params, idx} ->
        sql_fragment =
          case kind do
            :rel_type   -> "AND content->'m.relates_to'->>'rel_type' = $#{idx}"
            :event_type -> "AND event_type = $#{idx}"
          end
        {parts ++ [sql_fragment], params ++ [val], idx + 1}
      end)

    extra_sql = Enum.join(where_parts, "\n  ")
    {extra_sql, fixed_params ++ extra_params, final_idx}
  end

  @sql_count_thread_children """
  SELECT COUNT(*)
  FROM events
  WHERE room_id = $1
    AND content->'m.relates_to'->>'rel_type' = 'm.thread'
    AND content->'m.relates_to'->>'event_id' = $2
  """

  @doc """
  Returns the number of `m.thread` replies to `event_id` in `room_id`.

  Returns `{:ok, count}` — 0 when no thread replies exist.
  Returns `{:error, reason}` on DB error.

  Story 9-28: used by attach_thread_aggregations to build bundled aggregations.
  """
  @spec count_thread_children(String.t(), String.t()) ::
          {:ok, non_neg_integer()} | {:error, term()}
  def count_thread_children(room_id, event_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_count_thread_children, [room_id, event_id]) do
      {:ok, %{rows: [[count]]}} -> {:ok, count}
      {:ok, _} -> {:ok, 0}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc """
  Returns the most recent state event per (event_type, state_key) for `room_id`,
  excluding event types handled separately (m.room.member, m.room.power_levels,
  m.room.create, m.room.name — those have dedicated DB helpers or are assembled
  from GenServer state).

  Story 9-7: extends build_state_events in EventDispatcher.Server to include
  state events set via PUT /rooms/{roomId}/state/{eventType}.

  Returns `{:ok, [%{type, state_key, content_json, sender}]}` or `{:error, reason}`.
  """
  @spec get_generic_state_events(String.t()) ::
          {:ok, list(map())} | {:error, term()}
  def get_generic_state_events(room_id) do
    sql = """
    SELECT DISTINCT ON (event_type, state_key)
      event_type, state_key, content::text, sender
    FROM events
    WHERE room_id = $1
      AND state_key IS NOT NULL
      AND event_type NOT IN (
        'm.room.member',
        'm.room.power_levels',
        'm.room.create',
        'm.room.name'
      )
    ORDER BY event_type, state_key, origin_server_ts DESC
    """

    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, [room_id]) do
      {:ok, %{rows: rows}} ->
        events =
          Enum.map(rows, fn [event_type, state_key, content_json, sender] ->
            %{
              type: event_type,
              state_key: state_key || "",
              content_json: content_json || "{}",
              sender: sender || ""
            }
          end)
        {:ok, events}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
