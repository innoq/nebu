defmodule Compliance.UserDeletion do
  require Logger

  @moduledoc """
  Atomare DSGVO Key-Löschung für einen User (Story 5.7).

  delete_user_keys/3 prüft erst Guards außerhalb der TX, dann führt es
  Steps 2–5 in einer einzigen Repo-Transaktion aus:
    1. (Guard, outside TX) SELECT users — :user_not_found | :conflict guard
    2. UPDATE users SET deletion_status = 'deletion_in_progress'
    3. UPDATE user_keys SET private_key = NULL, deleted_at = <now_ms> WHERE key_type = 'signing'
    4. UPDATE user_keys SET private_key = NULL, deleted_at = <now_ms> WHERE key_type = 'encryption'
    5. UPDATE users SET deletion_status = 'keys_deleted', keys_deleted_at = <now_ms>

  Ecto Repo.transaction/1 Verhalten:
    - fun returns non-error → {:ok, value}
    - fun returns {:error, reason} → rollback, returns {:error, reason}

  Bei Transaktionsfehler (Steps 2–5 DB-Fehler) wird ein "user_keys_deletion_attempted"
  Audit-Eintrag in einer separaten AuditWriter-TX geschrieben (Failure Invariant, AC3).

  :user_not_found und :conflict sind Business-Logic-Guards (kein attempted-Audit).

  Persistenz-Strategie: Option B — PostgreSQL-persistent.
  Kein GenServer-State; kein Restart-Test erforderlich.
  """

  # Configurable repo for test injection.
  defp repo, do: Application.get_env(:compliance, :repo, Nebu.Repo)

  # Configurable AuditWriter for test injection.
  defp audit_writer, do: Application.get_env(:compliance, :audit_writer, Compliance.AuditWriter)

  @doc """
  Atomically soft-deletes the private signing and encryption keys for `target_user_id`.

  Returns:
    - `{:ok, %{keys_deleted_at: integer()}}` on success (epoch milliseconds)
    - `{:error, :user_not_found}` when the user does not exist
    - `{:error, :conflict}` when deletion_status is already 'deletion_in_progress' or 'keys_deleted'
    - `{:error, term()}` on any DB/transaction failure (writes attempted-audit as failure invariant)
  """
  def delete_user_keys(admin_user_id, target_user_id, reason) do
    now_ms = System.system_time(:millisecond)

    # Step 1: Guard check outside the transaction.
    # :user_not_found and :conflict are business-logic guards — NOT transaction failures.
    # No attempted-audit is emitted for these (AC3 spec).
    case check_user(target_user_id) do
      {:error, :user_not_found} ->
        {:error, :user_not_found}

      {:error, :conflict} ->
        {:error, :conflict}

      {:error, db_error} ->
        # DB error on the SELECT check itself — rare but treat as failure.
        # NOTE: do NOT shadow `reason` (the deletion-reason string passed by the caller);
        # the attempted-audit must record the original deletion reason AND the DB error.
        emit_attempted_audit(admin_user_id, target_user_id, reason, db_error)
        {:error, db_error}

      :ok ->
        # Steps 2–5 in a single Repo transaction.
        # Ecto rolls back automatically when the fun returns {:error, _}.
        # The deletion_in_progress marker (Step 2) is also rolled back on failure.
        tx_result =
          repo().transaction(fn ->
            with :ok <- mark_in_progress(target_user_id),
                 :ok <- delete_signing_key(target_user_id, now_ms),
                 :ok <- delete_encryption_key(target_user_id, now_ms),
                 :ok <- mark_keys_deleted(target_user_id, now_ms) do
              now_ms
            end
          end)

        case tx_result do
          {:ok, keys_deleted_at_ms} ->
            # Transaction committed successfully — emit success audit.
            audit_writer().log(
              admin_user_id,
              "user_keys_deleted",
              "user",
              target_user_id,
              %{reason: reason},
              "success"
            )

            {:ok, %{keys_deleted_at: keys_deleted_at_ms}}

          {:error, error_reason} ->
            # Transaction failed (DB error in Steps 2–5).
            # Emit attempted-audit in separate AuditWriter TX (failure invariant, AC3).
            emit_attempted_audit(admin_user_id, target_user_id, reason, error_reason)
            {:error, error_reason}
        end
    end
  end

  # ─── Step 1: Guard — check user existence ────────────────────────────────────
  #
  # FB-57-01: This SELECT checks existence and deletion_status for early-exit guards
  # (:user_not_found, :conflict when already in_progress/keys_deleted).
  # The TOCTOU window between this SELECT and Step 2 is closed by the atomic
  # conditional UPDATE in mark_in_progress/1 (which uses RETURNING to detect races).

  defp check_user(target_user_id) do
    sql = "SELECT user_id, deletion_status FROM users WHERE user_id = $1"

    case repo().query(sql, [target_user_id]) do
      {:ok, %{rows: []}} ->
        {:error, :user_not_found}

      {:ok, %{rows: [[_user_id, deletion_status] | _]}} ->
        if deletion_status in ["deletion_in_progress", "keys_deleted"] do
          {:error, :conflict}
        else
          :ok
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  # ─── Step 2: Mark deletion_in_progress (atomic conditional UPDATE) ───────────
  #
  # FB-57-01: Replace the old unconditional UPDATE with a single atomic
  # conditional UPDATE + RETURNING. This closes the TOCTOU race window between
  # check_user/1 and the status write:
  #
  #   UPDATE users
  #   SET deletion_status = 'deletion_in_progress'
  #   WHERE user_id = $1
  #     AND (deletion_status IS NULL OR deletion_status = 'active')
  #   RETURNING user_id
  #
  # If a concurrent caller already set deletion_in_progress between our SELECT
  # (step 1) and this UPDATE, the WHERE predicate finds 0 matching rows and
  # RETURNING returns an empty list → {:error, :conflict}.
  # This guarantees exactly-once semantics even under concurrent callers.

  defp mark_in_progress(target_user_id) do
    sql = """
    UPDATE users
    SET deletion_status = 'deletion_in_progress'
    WHERE user_id = $1
      AND (deletion_status IS NULL OR deletion_status = 'active')
    RETURNING user_id
    """

    case repo().query(sql, [target_user_id]) do
      {:ok, %{rows: []}} -> {:error, :conflict}
      {:ok, %{rows: [_ | _]}} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  # ─── Step 3: Soft-delete signing key ─────────────────────────────────────────

  defp delete_signing_key(target_user_id, now_ms) do
    sql = """
    UPDATE user_keys
    SET private_key = NULL, deleted_at = $1
    WHERE user_id = $2
      AND key_type = 'signing'
    """

    case repo().query(sql, [now_ms, target_user_id]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  # ─── Step 4: Soft-delete encryption key ──────────────────────────────────────

  defp delete_encryption_key(target_user_id, now_ms) do
    sql = """
    UPDATE user_keys
    SET private_key = NULL, deleted_at = $1
    WHERE user_id = $2
      AND key_type = 'encryption'
    """

    case repo().query(sql, [now_ms, target_user_id]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  # ─── Step 5: Mark keys_deleted ───────────────────────────────────────────────

  defp mark_keys_deleted(target_user_id, now_ms) do
    sql = """
    UPDATE users
    SET deletion_status = 'keys_deleted',
        keys_deleted_at = $1
    WHERE user_id = $2
    """

    case repo().query(sql, [now_ms, target_user_id]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  # ─── Failure Invariant: attempted-audit in own TX ────────────────────────────

  defp emit_attempted_audit(admin_user_id, target_user_id, reason, error_reason) do
    # AuditWriter.log/7 runs in its own Repo.transaction/1 — never inside the failing TX.
    # never-raise: AuditWriter returns {:error, :audit_write_failed} on DB failure.
    _ =
      audit_writer().log(
        admin_user_id,
        "user_keys_deletion_attempted",
        "user",
        target_user_id,
        %{reason: reason, error: inspect(error_reason)},
        "attempted"
      )

    :ok
  end
end
