defmodule Nebu.Room.InviteDB do
  @moduledoc """
  PostgreSQL persistence layer for room invitations.

  Inserts invitation records into the `room_invitations` table.
  All timestamps stored as BIGINT milliseconds.
  """

  @sql_insert_invitation """
  INSERT INTO room_invitations (room_id, inviter_id, invitee_id, invited_at)
  VALUES ($1, $2, $3, $4)
  ON CONFLICT (room_id, invitee_id) DO NOTHING
  """

  @doc """
  Inserts an invitation record into the `room_invitations` table.

  If an invitation for the same room_id + invitee_id already exists,
  the insert is silently ignored (ON CONFLICT DO NOTHING).

  Returns `:ok` on success, `{:error, reason}` on DB error.
  """
  @spec insert_invitation(String.t(), String.t(), String.t()) :: :ok | {:error, term()}
  def insert_invitation(room_id, inviter_id, invitee_id) do
    now_ms = System.system_time(:millisecond)

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_insert_invitation, [
           room_id,
           inviter_id,
           invitee_id,
           now_ms
         ]) do
      {:ok, _result} ->
        :ok

      {:error, reason} ->
        {:error, reason}
    end
  end

  @sql_reject_invitation """
  UPDATE room_invitations
  SET rejected_at = $3
  WHERE room_id = $1 AND invitee_id = $2
    AND accepted_at IS NULL AND rejected_at IS NULL
  """

  @doc """
  Marks a pending invitation as rejected by setting `rejected_at`.
  Called when a user declines an invite via POST /rooms/{roomId}/leave on an invited room.
  Returns `:ok` on success.
  """
  @spec reject_invitation(String.t(), String.t()) :: :ok | {:error, term()}
  def reject_invitation(room_id, invitee_id) do
    now_ms = System.system_time(:millisecond)

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_reject_invitation, [
           room_id,
           invitee_id,
           now_ms
         ]) do
      {:ok, _result} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @sql_accept_invitation """
  UPDATE room_invitations
  SET accepted_at = $3
  WHERE room_id = $1 AND invitee_id = $2
    AND accepted_at IS NULL AND rejected_at IS NULL
  """

  @doc """
  Marks a pending invitation as accepted by setting `accepted_at`.
  Called when a user joins a room they were invited to, so the invite
  disappears from `rooms.invite` in subsequent sync responses.

  No-op if there is no pending invitation (e.g. public room join without invite).
  Returns `:ok` on success, `{:error, reason}` on DB error.
  """
  @spec accept_invitation(String.t(), String.t()) :: :ok | {:error, term()}
  def accept_invitation(room_id, invitee_id) do
    now_ms = System.system_time(:millisecond)

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_accept_invitation, [
           room_id,
           invitee_id,
           now_ms
         ]) do
      {:ok, _result} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @sql_get_pending_invite_rooms """
  SELECT room_id FROM room_invitations
  WHERE invitee_id = $1 AND accepted_at IS NULL AND rejected_at IS NULL
  """

  @doc "Returns room_ids where user has a pending (not accepted, not rejected) invitation."
  @spec get_pending_invite_rooms_for_user(String.t()) :: {:ok, [String.t()]} | {:error, term()}
  def get_pending_invite_rooms_for_user(invitee_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_pending_invite_rooms, [invitee_id]) do
      {:ok, %{rows: rows}} -> {:ok, Enum.map(rows, fn [rid] -> rid end)}
      {:error, reason} -> {:error, reason}
    end
  end

  @sql_get_declined_invite_rooms """
  SELECT room_id FROM room_invitations
  WHERE invitee_id = $1 AND rejected_at IS NOT NULL
  """

  @doc """
  Returns room_ids where the user has a declined invitation (rejected_at IS NOT NULL).

  Used by do_incremental_sync to include recently-declined rooms in fetch_delta_rooms.
  This ensures the m.room.member leave event emitted by emit_decline_event is found
  even when the sync task starts AFTER the decline (and pending-invite list is empty).
  fetch_events_since will return [] for old declines (their events predate since_ts),
  so only genuinely new decline events cause an immediate sync return.
  """
  @spec get_declined_invite_rooms_for_user(String.t()) :: {:ok, [String.t()]} | {:error, term()}
  def get_declined_invite_rooms_for_user(invitee_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_get_declined_invite_rooms, [invitee_id]) do
      {:ok, %{rows: rows}} -> {:ok, Enum.map(rows, fn [rid] -> rid end)}
      {:error, reason} -> {:error, reason}
    end
  end
end
