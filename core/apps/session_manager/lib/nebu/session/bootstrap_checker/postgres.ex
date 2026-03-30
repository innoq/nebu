defmodule Nebu.Session.BootstrapChecker.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.BootstrapChecker."

  @behaviour Nebu.Session.BootstrapChecker

  @bootstrap_lock_id 2015

  @check_sql """
  SELECT
    NOT EXISTS(SELECT 1 FROM server_config WHERE key = 'bootstrap_completed')
    AND NOT EXISTS(SELECT 1 FROM users)
  AS is_bootstrap
  """

  @upsert_sql """
  INSERT INTO users (user_id, system_role, created_at, is_active)
  VALUES ($1, $2, $3, true)
  ON CONFLICT (user_id) DO UPDATE SET last_seen_at = EXCLUDED.created_at
  RETURNING user_id, system_role
  """

  @flag_sql """
  INSERT INTO server_config (key, value, set_at)
  VALUES ('bootstrap_active', 'true', $1)
  ON CONFLICT (key) DO NOTHING
  """

  @impl Nebu.Session.BootstrapChecker
  def upsert_with_bootstrap(user_id, system_role) do
    Nebu.Repo.transaction(fn ->
      # Acquire advisory lock — serializes concurrent bootstrap checks
      Ecto.Adapters.SQL.query!(Nebu.Repo, "SELECT pg_advisory_xact_lock($1)", [@bootstrap_lock_id])

      # Check bootstrap conditions
      %{rows: [[is_bootstrap]]} = Ecto.Adapters.SQL.query!(Nebu.Repo, @check_sql, [])

      resolved_role = if is_bootstrap, do: "instance_admin", else: system_role

      now_ms = Nebu.DB.Helpers.now_ms()

      # Upsert user with resolved role
      %{rows: [[^user_id, stored_role]]} =
        Ecto.Adapters.SQL.query!(Nebu.Repo, @upsert_sql, [user_id, resolved_role, now_ms])

      # Record bootstrap activation flag if bootstrap triggered
      if is_bootstrap do
        Ecto.Adapters.SQL.query!(Nebu.Repo, @flag_sql, [now_ms])
      end

      {user_id, stored_role}
    end)
  end
end
