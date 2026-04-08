defmodule Nebu.Receipt.DB do
  @moduledoc "PostgreSQL persistence for read receipts."

  @sql_upsert_receipt """
  INSERT INTO read_receipts (room_id, user_id, event_id, receipt_type, received_at)
  VALUES ($1, $2, $3, $4, $5)
  ON CONFLICT (room_id, user_id, receipt_type)
  DO UPDATE SET event_id = EXCLUDED.event_id, received_at = EXCLUDED.received_at
  """

  @doc """
  Upserts a read receipt for the given user in the room.

  If a receipt already exists for (room_id, user_id, receipt_type), updates
  event_id and received_at to the new values (moves the read marker forward).

  Returns `:ok` on success or `{:error, reason}` on DB failure.
  """
  @spec upsert_receipt(String.t(), String.t(), String.t(), String.t()) :: :ok | {:error, term()}
  def upsert_receipt(room_id, user_id, receipt_type, event_id) do
    now_ms = System.system_time(:millisecond)

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_upsert_receipt, [
           room_id,
           user_id,
           event_id,
           receipt_type,
           now_ms
         ]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end
end
