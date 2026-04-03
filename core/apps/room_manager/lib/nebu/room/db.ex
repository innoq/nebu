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
end
