defmodule Nebu.Room.DBBehaviour do
  @moduledoc """
  Behaviour contract for the Room DB persistence layer.

  Story 5.29d AC1 (FB-E5-03) / AC2: Defines compile-time-checked callbacks that
  all production and test implementations of Nebu.Room.DB must satisfy.

  ## Usage

  Production module:
    - `Nebu.Room.DB` — Ecto/PostgreSQL-backed implementation
    - Must declare `@behaviour Nebu.Room.DBBehaviour`

  Test fakes used by event_dispatcher tests:
    - FakeDB modules in create_room_test.exs, join_room_test.exs, sync_test.exs, etc.
    - Must declare `@behaviour Nebu.Room.DBBehaviour` to get compile-time drift detection.

  Any fake that is missing a callback listed here will cause a compile-time warning
  (or error when `@dialyzer {:no_behaviours, _}` is absent) — surfacing interface
  drift before tests run.
  """

  @doc """
  Loads all active members for `room_id`.

  Returns `{:ok, [user_id], created_at_ms, power_levels_json}` if the room exists.
  Returns `{:error, :not_found}` if the room does not exist.
  Returns `{:error, reason}` on DB error.
  """
  @callback load_members(room_id :: String.t()) ::
              {:ok, [String.t()], integer(), String.t()} | {:error, :not_found | term()}

  @doc """
  Inserts a new room into the rooms table.

  Returns `{:ok, created_at_ms}` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @callback insert_room(room_id :: String.t()) :: {:ok, integer()} | {:error, term()}

  @doc """
  Inserts a member into room_members.

  Returns `:ok` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @callback insert_member(room_id :: String.t(), user_id :: String.t()) ::
              :ok | {:error, term()}

  @doc """
  Soft-deletes a member from room_members by setting left_at.

  Returns `:ok` on success.
  Returns `{:error, :not_member}` if no active row matched.
  Returns `{:error, reason}` on DB failure.
  """
  @callback delete_member(room_id :: String.t(), user_id :: String.t()) ::
              :ok | {:error, :not_member | term()}

  @doc """
  Inserts a signed event into the events append-only table.

  Returns `:ok` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @callback insert_event(event :: map()) :: :ok | {:error, term()}

  @doc """
  Persists the power_levels_json string for the given room_id.

  Returns `:ok` on success.
  Returns `{:error, reason}` on DB failure.
  """
  @callback set_power_levels(room_id :: String.t(), power_levels_json :: String.t()) ::
              :ok | {:error, term()}

  @doc """
  Returns all room IDs where user_id is currently an active member.

  Returns `{:ok, [room_id]}` — empty list if user has no active rooms.
  Returns `{:error, reason}` on DB error.
  """
  @callback get_rooms_for_user(user_id :: String.t()) ::
              {:ok, [String.t()]} | {:error, term()}

  @doc """
  Returns all room IDs where user_id has left (left_at IS NOT NULL).

  Used by do_incremental_sync to include recently-left rooms in the initial
  fetch_delta_rooms check. Closes the race window where the {new_leave} broadcast
  fires before the sync task subscribes, causing a 30 s long-poll delay.

  Returns `{:ok, [room_id]}` — empty list if user has no left rooms.
  Returns `{:error, reason}` on DB error.
  """
  @callback get_recently_left_rooms_for_user(user_id :: String.t()) ::
              {:ok, [String.t()]} | {:error, term()}

  @doc """
  Returns paginated events from the events table for the given room.

  Returns `{:ok, [event_map], next_batch, prev_batch}`.
  Returns `{:error, reason}` on DB error.
  """
  @callback fetch_events(
              room_id :: String.t(),
              direction :: String.t(),
              limit :: pos_integer(),
              from_token :: String.t()
            ) :: {:ok, [map()], String.t(), String.t()} | {:error, term()}

  @doc """
  Returns events in room_id with origin_server_ts strictly greater than
  the timestamp of last_event_id. Returns up to `limit` events in
  chronological order (ASC).

  Returns `{:ok, [event_map]}`.
  Returns `{:error, reason}` on DB error.
  """
  @callback fetch_events_since(
              room_id :: String.t(),
              last_event_id :: String.t() | nil,
              limit :: pos_integer()
            ) :: {:ok, [map()]} | {:error, term()}

  @doc """
  Returns the origin_server_ts for the given event_id.

  Returns `{:ok, integer()}` on success.
  Returns `{:error, :not_found}` if the event does not exist.
  Returns `{:error, reason}` on DB error.
  """
  @callback get_event_timestamp(event_id :: String.t()) ::
              {:ok, integer()} | {:error, :not_found | term()}

  @doc """
  Returns the room name from the most recent m.room.name event.

  Returns `{:ok, String.t()}` on success.
  Returns `{:error, :not_found}` if no m.room.name event exists.
  Returns `{:error, reason}` on DB error.
  """
  @callback get_room_name(room_id :: String.t()) ::
              {:ok, String.t()} | {:error, :not_found | term()}

  @doc """
  Loads the mutable settings for `room_id` — currently only max_members.

  Returns `{:ok, max_members}` where `max_members` is 0 (no limit) or a positive integer.
  Returns `{:error, reason}` on DB error.

  Story 6.8: Called by Room.Server.init/1 to restore max_members after a GenServer restart.
  Fail-open: if this call fails, GenServer defaults to 0 (no limit).
  """
  @callback load_room_settings(room_id :: String.t()) ::
              {:ok, non_neg_integer()} | {:error, term()}

  @doc """
  Returns the current archival status of `room_id` from the `rooms` table.

  Returns `{:ok, "active"}` for active (non-archived) rooms.
  Returns `{:ok, "archived"}` for archived rooms.
  Returns `{:error, :not_found}` when the room does not exist in the table.
  Returns `{:error, reason}` on DB error.

  Story 6.9: Called by Room.Server.init/1 before initialising state. When the result
  is `{:ok, "archived"}`, init/1 returns `{:stop, :normal}` so that Horde's `:transient`
  restart strategy does not restart the GenServer, preventing a restart loop. Any
  other return value (including `{:error, :not_found}` and DB errors) falls through
  to the normal init flow.
  """
  @callback get_room_status(room_id :: String.t()) ::
              {:ok, String.t()} | {:error, :not_found | term()}

  @doc """
  Atomically checks if a room is archived using SELECT FOR UPDATE.
  Called by Room.Server.send_event/6 before inserting an event.

  Must be called inside an Ecto transaction (the transaction context is
  created inside db.ex — caller does NOT need to manage a transaction).

  Returns {:ok, "active"} for active rooms.
  Returns {:ok, "archived"} for archived rooms.
  Returns {:error, :not_found} when the room does not exist.
  Returns {:error, reason} on DB error.

  Story 9-9: closes the TOCTOU race window between archive_room_atomic/1
  and send_event insert.
  """
  @callback check_room_status_for_update(room_id :: String.t()) ::
              {:ok, String.t()} | {:error, :not_found | term()}

  @doc """
  Returns the user_id of the room creator from the most recent m.room.create event.

  Returns `{:ok, String.t()}` on success.
  Returns `{:error, :not_found}` if no m.room.create event exists.
  Returns `{:error, reason}` on DB error.
  """
  @callback get_room_creator(room_id :: String.t()) ::
              {:ok, String.t()} | {:error, :not_found | term()}

  @doc """
  Returns the most recent state event per (event_type, state_key) for `room_id`,
  excluding types assembled from GenServer state (member, power_levels, create, name).

  Story 9-7: used by build_state_events/2 in EventDispatcher.Server to include
  state events set via PUT /rooms/{roomId}/state/{eventType}.

  Returns `{:ok, [%{type, state_key, content_json, sender}]}` on success.
  Returns `{:error, reason}` on DB error.
  """
  @callback get_generic_state_events(room_id :: String.t()) ::
              {:ok, list(map())} | {:error, term()}

  @doc """
  Returns the content of the m.room.create event for `room_id` (the first such event
  by origin_server_ts ASC, i.e. the authoritative create event).

  Used by build_state_events/2 so that upgraded rooms return their persisted
  m.room.create content (which includes the `predecessor` field) rather than a
  synthesized fallback that would omit predecessor.

  Returns `{:ok, content_map}` on success.
  Returns `{:error, :not_found}` if no m.room.create event exists (new room not yet
  persisted, or pre-create-persist legacy room).
  Returns `{:error, reason}` on DB error.
  """
  @callback get_room_create_event(room_id :: String.t()) ::
              {:ok, map()} | {:error, :not_found | term()}
end
