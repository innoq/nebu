defmodule Nebu.Session.PgStore.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.PgStore."

  @behaviour Nebu.Session.PgStore

  @upsert_since_token_sql """
  INSERT INTO sync_tokens (user_id, device_id, since_token, last_event_id, updated_at)
  VALUES ($1, '', $2, $3, $4)
  ON CONFLICT (user_id, device_id) DO UPDATE
    SET since_token   = EXCLUDED.since_token,
        last_event_id = EXCLUDED.last_event_id,
        updated_at    = EXCLUDED.updated_at
  """

  @upsert_since_token_device_sql """
  INSERT INTO sync_tokens (user_id, device_id, since_token, last_event_id, updated_at)
  VALUES ($1, $2, $3, $4, $5)
  ON CONFLICT (user_id, device_id) DO UPDATE
    SET since_token   = EXCLUDED.since_token,
        last_event_id = EXCLUDED.last_event_id,
        updated_at    = EXCLUDED.updated_at
  """

  @get_since_token_sql """
  SELECT since_token, last_event_id FROM sync_tokens WHERE user_id = $1 AND device_id = ''
  """

  @get_since_token_device_sql """
  SELECT since_token, last_event_id FROM sync_tokens WHERE user_id = $1 AND device_id = $2
  """

  @delete_sync_token_sql """
  DELETE FROM sync_tokens WHERE user_id = $1
  """

  @delete_sync_token_device_sql """
  DELETE FROM sync_tokens WHERE user_id = $1 AND device_id = $2
  """

  @delete_session_sql """
  DELETE FROM sessions WHERE user_id = $1
  """

  @delete_session_device_sql """
  DELETE FROM sessions WHERE user_id = $1 AND device_id = $2
  """

  @impl Nebu.Session.PgStore
  def persist_since_token(user_id, since_token, last_event_id) do
    repo = Application.get_env(:session_manager, :repo_module, Nebu.Repo)
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(repo, @upsert_since_token_sql, [
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
  def persist_since_token(user_id, device_id, since_token, last_event_id) do
    repo = Application.get_env(:session_manager, :repo_module, Nebu.Repo)
    now_ms = Nebu.DB.Helpers.now_ms()

    case Ecto.Adapters.SQL.query(repo, @upsert_since_token_device_sql, [
           user_id,
           device_id,
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
    repo = Application.get_env(:session_manager, :repo_module, Nebu.Repo)

    case Ecto.Adapters.SQL.query(repo, @get_since_token_sql, [user_id]) do
      {:ok, %{rows: [[since_token, last_event_id]]}} ->
        {:ok, %{since_token: since_token, last_event_id: last_event_id}}

      {:ok, %{rows: []}} ->
        {:error, :not_found}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @impl Nebu.Session.PgStore
  def get_since_token(user_id, device_id) do
    repo = Application.get_env(:session_manager, :repo_module, Nebu.Repo)

    case Ecto.Adapters.SQL.query(repo, @get_since_token_device_sql, [user_id, device_id]) do
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

  @impl Nebu.Session.PgStore
  def invalidate_session(user_id, device_id) do
    # AC4 (Story 9-22): Delete both sync_tokens and sessions rows for (user_id, device_id)
    # in a single transaction. Use the configurable :repo_module for testability.
    # ETS eviction is NOT performed here — per-device logout does not invalidate the
    # in-memory user session (another device may still be active).
    repo = Application.get_env(:session_manager, :repo_module, Nebu.Repo)

    result =
      repo.transaction(fn ->
        with {:ok, _} <- query(repo, @delete_sync_token_device_sql, [user_id, device_id]),
             {:ok, _} <- query(repo, @delete_session_device_sql, [user_id, device_id]) do
          :ok
        else
          {:error, reason} -> repo.rollback(reason)
        end
      end)

    case result do
      {:ok, :ok} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp query(repo, sql, params) do
    case Ecto.Adapters.SQL.query(repo, sql, params) do
      {:ok, _} = ok -> ok
      {:error, _} = err -> err
    end
  end
end
