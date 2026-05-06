defmodule Nebu.Session.PgStore do
  @moduledoc """
  Behaviour and delegation module for PostgreSQL since-token persistence.

  Handles:
  - Persisting since-tokens for incremental /sync checkpointing
  - Retrieving since-tokens for /sync resume after gateway restarts
  - Session invalidation (deletes from sync_tokens + sessions in a single
    PostgreSQL transaction, then evicts from ETS)

  Real implementation: `Nebu.Session.PgStore.Postgres` (uses Nebu.Repo).
  Test implementation: configured via Application.put_env in test setup.

  ## Atomicity rule

  `invalidate_session/1` MUST evict from ETS *only after* the PostgreSQL
  transaction commits. If the DB transaction fails, the ETS entry must NOT
  be deleted — ETS is the hot-path view of what is currently active;
  PostgreSQL is the authoritative store (ADR-002).
  """

  @callback persist_since_token(
              user_id :: String.t(),
              since_token :: String.t(),
              last_event_id :: String.t() | nil
            ) :: :ok | {:error, term()}

  @callback persist_since_token(
              user_id :: String.t(),
              device_id :: String.t(),
              since_token :: String.t(),
              last_event_id :: String.t() | nil
            ) :: :ok | {:error, term()}

  @callback get_since_token(user_id :: String.t()) ::
              {:ok, %{since_token: String.t(), last_event_id: String.t() | nil}}
              | {:error, :not_found}

  @callback get_since_token(user_id :: String.t(), device_id :: String.t()) ::
              {:ok, %{since_token: String.t(), last_event_id: String.t() | nil}}
              | {:error, :not_found}

  @callback invalidate_session(user_id :: String.t()) ::
              :ok | {:error, term()}

  @callback invalidate_session(user_id :: String.t(), device_id :: String.t()) ::
              :ok | {:error, term()}

  @doc """
  Upserts a since-token row in the `sync_tokens` table for `user_id`.

  `since_token` is an opaque base64-encoded string — never a sequential integer.
  `last_event_id` is nullable (nil until the first event is processed).

  Returns `:ok` on success, `{:error, reason}` on DB failure.
  """
  @spec persist_since_token(String.t(), String.t(), String.t() | nil) ::
          :ok | {:error, term()}
  def persist_since_token(user_id, since_token, last_event_id) do
    pg_store_module().persist_since_token(user_id, since_token, last_event_id)
  end

  @doc "Per-device variant: upserts a since-token row for `(user_id, device_id)`."
  @spec persist_since_token(String.t(), String.t(), String.t(), String.t() | nil) ::
          :ok | {:error, term()}
  def persist_since_token(user_id, device_id, since_token, last_event_id) do
    pg_store_module().persist_since_token(user_id, device_id, since_token, last_event_id)
  end

  @doc """
  Returns `{:ok, %{since_token: t, last_event_id: id}}` for `user_id`, or
  `{:error, :not_found}` if no row exists in `sync_tokens`.
  """
  @spec get_since_token(String.t()) ::
          {:ok, %{since_token: String.t(), last_event_id: String.t() | nil}}
          | {:error, :not_found}
  def get_since_token(user_id) do
    pg_store_module().get_since_token(user_id)
  end

  @doc "Per-device variant: looks up by `(user_id, device_id)` composite key."
  @spec get_since_token(String.t(), String.t()) ::
          {:ok, %{since_token: String.t(), last_event_id: String.t() | nil}}
          | {:error, :not_found}
  def get_since_token(user_id, device_id) do
    pg_store_module().get_since_token(user_id, device_id)
  end

  @doc """
  Invalidates all sessions for `user_id`:

  1. Deletes from `sync_tokens` (since-token row)
  2. Deletes from `sessions` (all device sessions)

  Both deletes run in a single PostgreSQL transaction. If the transaction
  commits, `Nebu.Session.EtsStore.delete_session/1` is called to evict
  the in-memory cache entry.

  If the DB transaction fails, returns `{:error, reason}` and the ETS
  entry is NOT modified (atomicity guarantee).
  """
  @spec invalidate_session(String.t()) :: :ok | {:error, term()}
  def invalidate_session(user_id) do
    pg_store_module().invalidate_session(user_id)
  end

  @doc "Per-device variant: deletes the `(user_id, device_id)` row from `sync_tokens` AND the matching row from `sessions`, both in a single PostgreSQL transaction."
  @spec invalidate_session(String.t(), String.t()) :: :ok | {:error, term()}
  def invalidate_session(user_id, device_id) do
    pg_store_module().invalidate_session(user_id, device_id)
  end

  defp pg_store_module do
    Application.get_env(:session_manager, :pg_store_module, Nebu.Session.PgStore.Postgres)
  end
end
