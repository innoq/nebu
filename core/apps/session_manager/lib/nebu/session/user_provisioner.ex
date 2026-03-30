defmodule Nebu.Session.UserProvisioner do
  @moduledoc """
  Orchestrates user provisioning on first login:
  - Generates Ed25519 signing keypair + X25519 encryption keypair
  - Stores both in user_keys table
  - Encrypts display_name (Tier 1) and email (Tier 2)
  - Updates users table with key_ids + encrypted PII
  - Runs all DB writes in a single PostgreSQL transaction

  Delegates to a configurable module for testability.
  Real implementation: Nebu.Session.UserProvisioner.Postgres
  Test implementation: configured via Application.put_env in test setup.

  Called by Story 2.14 ValidateToken handler for new users only.
  """

  @callback provision_user(
              user_id :: String.t(),
              display_name :: String.t(),
              email :: String.t(),
              server_key :: binary()
            ) :: {:ok, :provisioned} | {:error, term()}

  @doc """
  Provisions a new user with keypairs and encrypted PII.
  - user_id: Matrix user ID (e.g. "@kai:nebu.local")
  - display_name: from OIDC preferred_username claim
  - email: from OIDC email claim (Sensitive PII, Tier 2)
  - server_key: 32-byte binary from Application.get_env(:signature, :pii_encryption_key) (Tier 1)

  Returns {:ok, :provisioned} on success, {:error, reason} on failure.
  Idempotent: UPDATE uses WHERE signing_key_id IS NULL — safe to call twice.
  """
  @spec provision_user(String.t(), String.t(), String.t(), binary()) ::
          {:ok, :provisioned} | {:error, term()}
  def provision_user(user_id, display_name, email, server_key) do
    provisioner_module().provision_user(user_id, display_name, email, server_key)
  end

  defp provisioner_module do
    Application.get_env(:session_manager, :provisioner_module, Nebu.Session.UserProvisioner.Postgres)
  end
end
