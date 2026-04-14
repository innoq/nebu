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
end
