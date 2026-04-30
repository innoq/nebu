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

  Story 5.29c — AC8 (FB-52-02): unknown action strings are rejected with
  {:error, :audit_unknown_action} before any DB call. The @known_actions
  allowlist covers all Epic-5 vocabulary. Adding a new audit action requires
  updating this list first.
  """

  alias Compliance.AuditLogEntry

  # AC8 (Story 5.29c): allowlist of all valid audit action strings in Epic 5.
  # compliance_session_revoked was added by Story 5.29c (AC2).
  @known_actions ~w(
    admin_login
    admin_login_failed
    admin_logout
    bootstrap_completed
    bootstrap_failed
    room_created
    room_joined
    compliance_access_requested
    compliance_access_approved
    compliance_access_rejected
    compliance_session_issued
    compliance_session_expired
    compliance_session_revoked
    compliance_export_downloaded
    user_keys_deleted
    user_keys_deletion_attempted
    user_anonymized
  )

  # Configurable repo for test injection. Production uses Nebu.Repo.
  defp repo, do: Application.get_env(:compliance, :repo, Nebu.Repo)

  def log(actor_user_id, action, target_type, target_id, metadata, outcome, error_detail \\ nil) do
    # AC8 (Story 5.29c): reject unknown action strings before changeset validation.
    # Unknown actions indicate a bug (typo, missing allowlist entry) rather than
    # a validation error — use a distinct error atom so callers can distinguish.
    if is_binary(action) and action != "" and action not in @known_actions do
      Logger.error("AuditWriter: rejected unknown audit action",
        action: inspect(action),
        actor: actor_user_id
      )
      {:error, :audit_unknown_action}
    else
      do_log(actor_user_id, action, target_type, target_id, metadata, outcome, error_detail)
    end
  end

  defp do_log(actor_user_id, action, target_type, target_id, metadata, outcome, error_detail) do
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
