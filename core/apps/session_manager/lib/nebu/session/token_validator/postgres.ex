defmodule Nebu.Session.TokenValidator.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.TokenValidator."

  @behaviour Nebu.Session.TokenValidator

  @lookup_sql """
  SELECT user_id, system_role, display_name_encrypted, display_name_nonce, is_active, signing_key_id
  FROM users
  WHERE user_id = $1
  """

  @impl Nebu.Session.TokenValidator
  def validate(user_id, system_role, display_name, email) do
    case lookup_user(user_id) do
      {:ok, nil} ->
        provision_new_user(user_id, system_role, display_name, email)

      {:ok, %{is_active: false}} ->
        {:error, :deactivated}

      {:ok, %{signing_key_id: nil}} ->
        case provision_existing_user(user_id, display_name, email) do
          {:ok, :provisioned} -> read_and_decrypt(user_id)
          {:error, reason} -> {:error, reason}
        end

      {:ok, user} ->
        decrypt_and_return(user)

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp lookup_user(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @lookup_sql, [user_id]) do
      {:ok, %{rows: []}} ->
        {:ok, nil}

      {:ok, %{rows: [[uid, role, dn_enc, dn_nonce, active, sign_key_id]]}} ->
        {:ok, %{
          user_id: uid,
          system_role: role,
          display_name_encrypted: dn_enc,
          display_name_nonce: dn_nonce,
          is_active: active,
          signing_key_id: sign_key_id
        }}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp provision_new_user(user_id, system_role, display_name, email) do
    server_key = Application.get_env(:signature, :pii_encryption_key)

    with {:ok, {^user_id, resolved_role}} <-
           Nebu.Session.BootstrapChecker.upsert_with_bootstrap(user_id, system_role),
         {:ok, :provisioned} <-
           Nebu.Session.UserProvisioner.provision_user(user_id, display_name, email, server_key) do
      {:ok, %{
        user_id: user_id,
        system_role: resolved_role,
        display_name: display_name,
        is_active: true
      }}
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp provision_existing_user(user_id, display_name, email) do
    server_key = Application.get_env(:signature, :pii_encryption_key)
    Nebu.Session.UserProvisioner.provision_user(user_id, display_name, email, server_key)
  end

  defp read_and_decrypt(user_id) do
    case lookup_user(user_id) do
      {:ok, nil} -> {:error, :user_not_found}
      {:ok, %{is_active: false}} -> {:error, :deactivated}
      {:ok, user} -> decrypt_and_return(user)
      {:error, reason} -> {:error, reason}
    end
  end

  defp decrypt_and_return(%{
         user_id: user_id,
         system_role: system_role,
         display_name_encrypted: dn_enc,
         display_name_nonce: dn_nonce,
         is_active: true
       }) do
    server_key = Application.get_env(:signature, :pii_encryption_key)

    case Nebu.Signature.decrypt_operational_pii(dn_enc, dn_nonce, server_key) do
      {:ok, display_name} ->
        {:ok, %{
          user_id: user_id,
          system_role: system_role,
          display_name: display_name,
          is_active: true
        }}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
