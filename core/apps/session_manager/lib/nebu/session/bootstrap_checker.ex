defmodule Nebu.Session.BootstrapChecker do
  @moduledoc """
  Checks bootstrap mode and resolves the effective system_role for a new user.

  Bootstrap mode is active when:
  - No 'bootstrap_completed' row exists in server_config (forward-compat for Story 2.16)
  - No users exist in the users table

  When bootstrap triggers: inserts 'bootstrap_active' = 'true' into server_config,
  and returns 'instance_admin' as the resolved role.

  Uses pg_advisory_xact_lock for race condition safety.
  """

  @callback upsert_with_bootstrap(
              user_id :: String.t(),
              system_role :: String.t()
            ) :: {:ok, {String.t(), String.t()}} | {:error, term()}

  @spec upsert_with_bootstrap(String.t(), String.t()) ::
          {:ok, {String.t(), String.t()}} | {:error, term()}
  def upsert_with_bootstrap(user_id, system_role) do
    impl_module().upsert_with_bootstrap(user_id, system_role)
  end

  defp impl_module do
    Application.get_env(:session_manager, :bootstrap_checker_module,
      Nebu.Session.BootstrapChecker.Postgres)
  end
end
