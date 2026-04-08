defmodule Nebu.Profile.DB do
  @moduledoc "PostgreSQL persistence for user profiles."

  @sql_upsert """
  INSERT INTO profiles (user_id, displayname, avatar_url, updated_at)
  VALUES ($1, $2, $3, $4)
  ON CONFLICT (user_id)
  DO UPDATE SET
    displayname = COALESCE(EXCLUDED.displayname, profiles.displayname),
    avatar_url  = COALESCE(EXCLUDED.avatar_url, profiles.avatar_url),
    updated_at  = EXCLUDED.updated_at
  """

  @doc """
  Upserts a profile row for the given user.

  Pass `nil` for a field to leave the existing value unchanged (COALESCE logic ensures
  nil values do not overwrite existing data — only non-nil values are written).

  Returns `:ok` on success or `{:error, reason}` on DB failure.
  """
  @spec upsert_profile(String.t(), String.t() | nil, String.t() | nil) :: :ok | {:error, term()}
  def upsert_profile(user_id, displayname, avatar_url) do
    now_ms = System.system_time(:millisecond)

    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_upsert, [user_id, displayname, avatar_url, now_ms]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end
end
