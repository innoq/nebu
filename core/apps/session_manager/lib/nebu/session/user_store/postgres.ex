defmodule Nebu.Session.UserStore.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.UserStore."

  @behaviour Nebu.Session.UserStore

  @sql """
  INSERT INTO users (user_id, system_role, created_at, is_active)
  VALUES ($1, $2, $3, true)
  ON CONFLICT (user_id) DO UPDATE SET last_seen_at = EXCLUDED.created_at
  RETURNING user_id
  """

  @impl Nebu.Session.UserStore
  def upsert_user(user_id, system_role) do
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql, [user_id, system_role, now_ms]) do
      {:ok, %{rows: [[^user_id]]}} -> {:ok, user_id}
      {:error, reason} -> {:error, reason}
    end
  end
end
