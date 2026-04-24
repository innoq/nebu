defmodule Compliance.AuditWriter do
  require Logger

  @moduledoc """
  Generic audit log writer. Every call to log/6 or log/7 inserts exactly one
  row into `audit_log` via a separate Repo.transaction/1 — never inside the
  caller's transaction. A caller rollback cannot suppress the audit record.

  Error policy: never raises. On DB failure returns {:error, :audit_write_failed}
  and logs via Logger.error/2.

  Persistenz-Strategie: Option C — Stateless. No GenServer state, no queue.
  Each log/6 call is atomic in its own Repo.transaction/1.
  """

  alias Compliance.AuditLogEntry

  # Configurable repo for test injection. Production uses Nebu.Repo.
  defp repo, do: Application.get_env(:compliance, :repo, Nebu.Repo)

  def log(actor_user_id, action, target_type, target_id, metadata, outcome, error_detail \\ nil) do
    # Kassandra MEDIUM-3 (2026-04-23): validate via changeset so required fields
    # (actor_user_id, action, outcome) cannot be silently persisted as nil or "".
    # Without this, insert! would have written "actor_user_id=nil" and corrupted
    # the audit trail.
    changeset =
      AuditLogEntry.changeset(%AuditLogEntry{}, %{
        actor_user_id: actor_user_id,
        action:        action,
        target_type:   target_type,
        target_id:     target_id,
        metadata:      metadata,
        outcome:       outcome,
        error_detail:  error_detail
      })

    if changeset.valid? do
      entry = Ecto.Changeset.apply_changes(changeset)

      case repo().transaction(fn -> repo().insert!(entry) end) do
        {:ok, _} ->
          :ok

        {:error, reason} ->
          Logger.error("AuditWriter: failed to write audit log",
            action: action,
            actor: actor_user_id,
            reason: inspect(reason)
          )
          {:error, :audit_write_failed}
      end
    else
      Logger.error("AuditWriter: refused to write audit log — validation failed",
        action: action,
        actor: actor_user_id,
        errors: inspect(changeset.errors)
      )
      {:error, :audit_write_invalid}
    end
  end
end
