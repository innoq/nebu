defmodule Nebu.Session.PgStore.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.PgStore."

  @behaviour Nebu.Session.PgStore

  @upsert_since_token_sql """
  INSERT INTO sync_tokens (user_id, since_token, last_event_id, updated_at)
  VALUES ($1, $2, $3, $4)
  ON CONFLICT (user_id) DO UPDATE
    SET since_token   = EXCLUDED.since_token,
        last_event_id = EXCLUDED.last_event_id,
        updated_at    = EXCLUDED.updated_at
  """

  @get_since_token_sql """
  SELECT since_token, last_event_id FROM sync_tokens WHERE user_id = $1
  """

  @delete_sync_token_sql """
  DELETE FROM sync_tokens WHERE user_id = $1
  """

  @delete_session_sql """
  DELETE FROM sessions WHERE user_id = $1
  """

  @impl Nebu.Session.PgStore
  def persist_since_token(user_id, since_token, last_event_id) do
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(Nebu.Repo, @upsert_since_token_sql, [
           user_id,
           since_token,
           last_event_id,
           now_ms
         ]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  @impl Nebu.Session.PgStore
  def get_since_token(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @get_since_token_sql, [user_id]) do
      {:ok, %{rows: [[since_token, last_event_id]]}} ->
        {:ok, %{since_token: since_token, last_event_id: last_event_id}}

      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @impl Nebu.Session.PgStore
  def invalidate_session(user_id) do
    repo = Application.get_env(:session_manager, :repo_module, Nebu.Repo)

    result =
      repo.transaction(fn ->
        with {:ok, _} <- query(repo, @delete_sync_token_sql, [user_id]),
             {:ok, _} <- query(repo, @delete_session_sql, [user_id]) do
          :ok
        else
          {:error, reason} -> repo.rollback(reason)
        end
      end)

    case result do
      {:ok, :ok} ->
        # Both DB deletes succeeded — now evict from ETS (atomicity: after TX commit)
        Nebu.Session.EtsStore.delete_session(user_id)
        :ok

      {:error, reason} ->
        # Transaction rolled back — do NOT evict from ETS
        {:error, reason}
    end
  end

  defp query(repo, sql, params) do
    case Ecto.Adapters.SQL.query(repo, sql, params) do
      {:ok, _} = ok -> ok
      {:error, _} = err -> err
    end
  end
end
