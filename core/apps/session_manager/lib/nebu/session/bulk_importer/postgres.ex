defmodule Nebu.Session.BulkImporter.Postgres do
  @moduledoc """
  PostgreSQL lookup module for BulkImporter.

  Checks whether a user is already fully provisioned (signing_key_id IS NOT NULL).
  Called by Nebu.Session.BulkImporter.import_one/2 before attempting provisioning.

  Returns:
    :already_provisioned — user exists in DB with keypairs; skip this user.
    :not_provisioned     — user does not exist, or exists with signing_key_id IS NULL.
    {:error, reason}     — DB query failed.

  Uses the same SQL as TokenValidator.Postgres for consistency.
  """

  @lookup_sql "SELECT signing_key_id FROM users WHERE user_id = $1"

  @doc """
  Checks if the user already has a signing keypair in the DB.
  """
  @spec lookup(String.t()) :: :already_provisioned | :not_provisioned | {:error, term()}
  def lookup(user_id) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, @lookup_sql, [user_id]) do
      {:ok, %{rows: []}} ->
        :not_provisioned

      {:ok, %{rows: [[nil]]}} ->
        # User row exists but signing_key_id is NULL — not yet provisioned.
        :not_provisioned

      {:ok, %{rows: [[_signing_key_id]]}} ->
        # User row exists with a non-null signing_key_id — fully provisioned, skip.
        :already_provisioned

      {:error, reason} ->
        {:error, reason}
    end
  end
end
