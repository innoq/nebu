defmodule Nebu.Session.UserStore do
  @moduledoc """
  Manages user record persistence on first login.

  Delegates to a configurable DB module for testability.
  Real implementation: `Nebu.Session.UserStore.Postgres` (uses Nebu.Repo).
  Test implementation: configured via Application.put_env in test setup.
  """

  @callback upsert_user(user_id :: String.t(), system_role :: String.t()) ::
              {:ok, String.t()} | {:error, term()}

  @doc """
  Upserts a user record on login.
  - Inserts on first login (created_at = now_ms, is_active = true).
  - Updates last_seen_at on subsequent logins.
  - ON CONFLICT (user_id) resolves concurrent first-login race conditions.

  Returns {:ok, user_id} on success, {:error, reason} on failure.
  """
  @spec upsert_user(String.t(), String.t()) :: {:ok, String.t()} | {:error, term()}
  def upsert_user(user_id, system_role) do
    db_module().upsert_user(user_id, system_role)
  end

  defp db_module do
    Application.get_env(:session_manager, :db_module, Nebu.Session.UserStore.Postgres)
  end
end
