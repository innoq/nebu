defmodule Nebu.Session.UserProvisioner.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.UserProvisioner."

  @behaviour Nebu.Session.UserProvisioner

  @insert_key_sql """
  INSERT INTO user_keys (key_id, user_id, key_type, algorithm, public_key, private_key, created_at)
  VALUES ($1, $2, $3, $4, $5, $6, $7)
  """

  @update_user_sql """
  UPDATE users
  SET signing_key_id         = $1,
      encryption_key_id      = $2,
      display_name_encrypted = $3,
      display_name_nonce     = $4,
      email_encrypted        = $5,
      email_nonce            = $6,
      email_ephemeral_pub    = $7
  WHERE user_id = $8
    AND signing_key_id IS NULL
  """

  @impl Nebu.Session.UserProvisioner
  def provision_user(user_id, display_name, email, server_key) do
    now_ms = Nebu.DB.Helpers.now_ms()

    # Generate crypto material outside the transaction — keeps transaction window short
    {sign_pub, sign_priv} = Nebu.Signature.generate_signing_keypair()
    {enc_pub, enc_priv} = Nebu.Signature.generate_encryption_keypair()
    signing_key_id = Ecto.UUID.generate()
    encryption_key_id = Ecto.UUID.generate()

    {dn_encrypted, dn_nonce} = Nebu.Signature.encrypt_operational_pii(display_name, server_key)
    {email_encrypted, email_ephemeral_pub, email_nonce} =
      Nebu.Signature.encrypt_sensitive_pii(email, enc_pub)

    Nebu.Repo.transaction(fn ->
      with {:ok, _} <-
             query(@insert_key_sql, [signing_key_id, user_id, "signing", "ed25519", sign_pub, sign_priv, now_ms]),
           {:ok, _} <-
             query(@insert_key_sql, [encryption_key_id, user_id, "encryption", "x25519", enc_pub, enc_priv, now_ms]),
           {:ok, _} <-
             query(@update_user_sql, [
               signing_key_id, encryption_key_id,
               dn_encrypted, dn_nonce,
               email_encrypted, email_nonce, email_ephemeral_pub,
               user_id
             ]) do
        :provisioned
      else
        {:error, reason} -> Nebu.Repo.rollback(reason)
      end
    end)
  end

  defp query(sql, params) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, params) do
      {:ok, _} = ok -> ok
      {:error, _} = err -> err
    end
  end
end
