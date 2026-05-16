defmodule Nebu.Session.BulkImporter do
  @moduledoc """
  Provisions a list of OIDC users using the same flow as first login.

  Story 14-3a: BulkImportUsers gRPC RPC + Core Provisioning.

  For each user in the input list:
    1. Lookup the user in the DB (via injected lookup module).
       - If already provisioned (signing_key_id IS NOT NULL) → skip (no error, no duplicate).
       - If not provisioned → proceed.
    2. Upsert user record (UserStore.upsert_user/2) — INSERT ON CONFLICT DO UPDATE.
    3. Provision keypairs + encrypt PII (UserProvisioner.provision_user/4).
       Identical to the flow in TokenValidator.Postgres.provision_new_user.

  Returns {:ok, %{imported: N, skipped: N, failed: N}}.
  Partial success: an error on one user does not abort the rest.

  All module dependencies are injectable via Application.put_env for testability:
    :session_manager, :bulk_importer_lookup_module    — lookup module (lookup/1)
    :session_manager, :bulk_importer_user_store_module — UserStore behaviour
    :session_manager, :bulk_importer_provisioner_module — UserProvisioner behaviour

  The real lookup module (BulkImporter.Postgres) queries the users table directly.
  """

  require Logger

  @type claims :: %{
          user_id: String.t(),
          system_role: String.t(),
          display_name: String.t(),
          email: String.t()
        }

  @type result ::
          {:ok, %{imported: non_neg_integer(), skipped: non_neg_integer(), failed: non_neg_integer()}}

  @doc """
  Provisions a list of OIDC users with the same flow as first login.

  Each user in the list is processed in sequence. Duplicates (signing_key_id IS NOT NULL)
  are counted as skipped — not an error. Errors on individual users are counted as failed
  and do not abort the batch.

  Returns {:ok, %{imported: N, skipped: N, failed: N}}.
  """
  @spec import_users([claims()]) :: result()
  def import_users(users) when is_list(users) do
    server_key = Application.get_env(:signature, :pii_encryption_key)
    init = %{imported: 0, skipped: 0, failed: 0}

    result =
      Enum.reduce(users, init, fn user, acc ->
        case import_one(user, server_key) do
          :imported -> Map.update!(acc, :imported, &(&1 + 1))
          :skipped -> Map.update!(acc, :skipped, &(&1 + 1))
          :failed -> Map.update!(acc, :failed, &(&1 + 1))
        end
      end)

    {:ok, result}
  end

  defp import_one(%{user_id: user_id, system_role: role, display_name: dn, email: email}, server_key) do
    try do
      case lookup_module().lookup(user_id) do
        :already_provisioned ->
          :skipped

        :not_provisioned ->
          with {:ok, _} <- user_store_module().upsert_user(user_id, role),
               {:ok, :provisioned} <- provisioner_module().provision_user(user_id, dn, email, server_key) do
            :imported
          else
            {:error, reason} ->
              Logger.error("BulkImporter: failed to import #{user_id}: #{inspect(reason)}")
              :failed
          end

        {:error, reason} ->
          Logger.error("BulkImporter: lookup failed for #{user_id}: #{inspect(reason)}")
          :failed
      end
    rescue
      exception ->
        Logger.error("BulkImporter: unexpected error for #{user_id}: #{inspect(exception)}")
        :failed
    end
  end

  defp lookup_module do
    Application.get_env(:session_manager, :bulk_importer_lookup_module, Nebu.Session.BulkImporter.Postgres)
  end

  defp user_store_module do
    Application.get_env(:session_manager, :bulk_importer_user_store_module, Nebu.Session.UserStore.Postgres)
  end

  defp provisioner_module do
    Application.get_env(:session_manager, :bulk_importer_provisioner_module, Nebu.Session.UserProvisioner.Postgres)
  end
end
