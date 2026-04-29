defmodule Compliance.UserDeletionTest do
  use ExUnit.Case, async: true

  # ─── Story 5-7: Compliance.UserDeletion — red-phase ATDD tests ───────────────
  #
  # ALL tests in this module are expected to FAIL until Story 5.7 is implemented.
  # Compliance.UserDeletion.delete_user_keys/3 does NOT exist yet.
  #
  # Persistenz-Strategie: Option B — PostgreSQL persistent.
  #   Each step in the Ecto.Multi writes to the DB. The Multi rolls back atomically
  #   when any step returns {:error, reason}. No GenServer state, no restart test.
  #
  # Test strategy:
  #   - FakeRepo replaces Nebu.Repo so no real PostgreSQL is needed.
  #   - FakeRepo bucket (Agent) captures all SQL statements + args for assertion.
  #   - FakeAuditWriter captures AuditWriter.log/7 calls without hitting DB.
  #   - Application.put_env injects both before each test.
  #   - Concurrent-deletion scenario: pre-seed FakeRepo state before calling delete_user_keys/3.
  #   - DB-failure scenario: FailingStepFakeRepo returns error for the specific step.

  # ─── FakeRepo ─────────────────────────────────────────────────────────────────
  #
  # Minimal Ecto.Repo stub. Stores {sql, args} pairs in an Agent bucket.
  # transaction/1 executes the fun immediately; simulates commit on {:ok, _}.
  #
  # Default behaviour (controlled via Application.put_env):
  #   :compliance, :__test_fake_repo_bucket__ → Agent pid for SQL log
  #   :compliance, :__test_user_row__         → map with user state for SELECT
  #   :compliance, :__test_fail_step__        → atom: :delete_signing | :delete_encryption | nil

  defmodule FakeRepo do
    @moduledoc "In-process fake for Nebu.Repo. Test-private."

    # ── transaction/1 ────────────────────────────────────────────────────────

    def transaction(fun) when is_function(fun, 0) do
      result = fun.()
      case result do
        {:error, _} = err -> err
        other -> {:ok, other}
      end
    end

    # ── query/2 — raw SQL execution stub (used by Ecto.Multi run steps) ──────

    def query(sql, args, _opts \\ []) do
      bucket = Application.get_env(:compliance, :__test_fake_repo_bucket__)
      if bucket, do: Agent.update(bucket, fn rows -> [{sql, args} | rows] end)

      cond do
        # Step 1: SELECT user — check existence and deletion_status
        String.contains?(sql, "deletion_status") and String.contains?(sql, "FROM users WHERE") ->
          user_row = Application.get_env(:compliance, :__test_user_row__, %{exists: true, deletion_status: nil})
          if user_row[:exists] == false do
            {:ok, %{rows: [], num_rows: 0}}
          else
            deletion_status = Map.get(user_row, :deletion_status)
            {:ok, %{rows: [[hd(args), deletion_status]], num_rows: 1}}
          end

        # Step 2: UPDATE users SET deletion_status = 'deletion_in_progress'
        String.contains?(sql, "deletion_in_progress") and String.contains?(sql, "UPDATE users") and not String.contains?(sql, "keys_deleted") ->
          {:ok, %{rows: [], num_rows: 1}}

        # Step 3: UPDATE user_keys SET private_key = NULL ... signing
        String.contains?(sql, "signing") ->
          fail_step = Application.get_env(:compliance, :__test_fail_step__)
          if fail_step == :delete_signing do
            {:error, :simulated_db_error}
          else
            {:ok, %{rows: [], num_rows: 1}}
          end

        # Step 4: UPDATE user_keys SET private_key = NULL ... encryption
        String.contains?(sql, "encryption") ->
          fail_step = Application.get_env(:compliance, :__test_fail_step__)
          if fail_step == :delete_encryption do
            {:error, :simulated_db_error}
          else
            {:ok, %{rows: [], num_rows: 1}}
          end

        # Step 5: UPDATE users SET deletion_status = 'keys_deleted', keys_deleted_at = ...
        String.contains?(sql, "keys_deleted") and String.contains?(sql, "UPDATE users") ->
          {:ok, %{rows: [], num_rows: 1}}

        true ->
          {:ok, %{rows: [], num_rows: 0}}
      end
    end

    def insert(struct, _opts \\ []) do
      bucket = Application.get_env(:compliance, :__test_fake_repo_bucket__)
      if bucket, do: Agent.update(bucket, fn rows -> [struct | rows] end)
      {:ok, struct}
    end

    def insert!(_struct, _opts \\ []) do
      raise "FakeRepo.insert! called unexpectedly in user_deletion_test"
    end
  end

  # ─── FailingTransactionFakeRepo ───────────────────────────────────────────────
  #
  # A FakeRepo variant whose transaction/1 captures the fun but returns {:error, step}
  # when the fun encounters the configured failing step.
  # Ecto.Multi normally rolls back; here we simulate by returning {:error, ...}.

  defmodule FailingTransactionFakeRepo do
    @moduledoc "FakeRepo that simulates a Multi step failure with rollback."

    def transaction(fun) when is_function(fun, 0) do
      # Execute fun; if it triggers the fail_step, the fun returns {:error, ...}
      # which gets propagated as-is (simulating Ecto.Multi rollback).
      try do
        result = fun.()
        case result do
          {:error, _} = err -> err
          other -> {:ok, other}
        end
      rescue
        _ -> {:error, :simulated_db_error}
      end
    end

    def query(sql, args, _opts \\ []) do
      # Delegate to FakeRepo for most steps; fail on configured step
      FakeRepo.query(sql, args)
    end

    def insert(struct, opts \\ []), do: FakeRepo.insert(struct, opts)
    def insert!(_struct, _opts \\ []), do: raise("FailingTransactionFakeRepo.insert! called")
  end

  # ─── FakeAuditWriter ─────────────────────────────────────────────────────────
  #
  # Records AuditWriter.log/7 calls without DB access.
  # Stored in Agent bucket keyed by :__test_audit_bucket__.

  defmodule FakeAuditWriter do
    @moduledoc "In-process fake for Compliance.AuditWriter. Test-private."

    def log(actor_user_id, action, target_type, target_id, metadata, outcome, _error_detail \\ nil) do
      bucket = Application.get_env(:compliance, :__test_audit_bucket__)
      entry = %{
        actor_user_id: actor_user_id,
        action: action,
        target_type: target_type,
        target_id: target_id,
        metadata: metadata,
        outcome: outcome
      }
      if bucket, do: Agent.update(bucket, fn rows -> [entry | rows] end)
      :ok
    end
  end

  # ─── Setup ────────────────────────────────────────────────────────────────────

  setup do
    {:ok, sql_bucket} = Agent.start_link(fn -> [] end)
    {:ok, audit_bucket} = Agent.start_link(fn -> [] end)

    Application.put_env(:compliance, :repo, FakeRepo)
    Application.put_env(:compliance, :audit_writer, FakeAuditWriter)
    Application.put_env(:compliance, :__test_fake_repo_bucket__, sql_bucket)
    Application.put_env(:compliance, :__test_audit_bucket__, audit_bucket)
    Application.put_env(:compliance, :__test_user_row__, %{exists: true, deletion_status: nil})
    Application.delete_env(:compliance, :__test_fail_step__)

    on_exit(fn ->
      Application.delete_env(:compliance, :repo)
      Application.delete_env(:compliance, :audit_writer)
      Application.delete_env(:compliance, :__test_fake_repo_bucket__)
      Application.delete_env(:compliance, :__test_audit_bucket__)
      Application.delete_env(:compliance, :__test_user_row__)
      Application.delete_env(:compliance, :__test_fail_step__)
    end)

    {:ok, sql_bucket: sql_bucket, audit_bucket: audit_bucket}
  end

  # ─── Test 8: Happy path ───────────────────────────────────────────────────────
  #
  # Given: user exists in DB, has signing + encryption key rows, deletion_status = nil
  # When:  Compliance.UserDeletion.delete_user_keys(admin_id, user_id, reason)
  # Then:  {:ok, %{keys_deleted_at: ms}}
  #        Both signing and encryption keys soft-deleted (SQL captured)
  #        users.deletion_status → 'keys_deleted'
  #        Audit 'user_keys_deleted' with outcome='success' emitted

  test "happy path: deletes both private keys, retains public, updates users, audits success",
       %{sql_bucket: sql_bucket, audit_bucket: audit_bucket} do
    result =
      Compliance.UserDeletion.delete_user_keys(
        "admin-sub-1",
        "target-user-42",
        "DSGVO deletion request ref GDPR-2026-042"
      )

    assert {:ok, %{keys_deleted_at: keys_deleted_at_ms}} = result,
           "expected {:ok, %{keys_deleted_at: ms}}, got #{inspect(result)}"

    assert is_integer(keys_deleted_at_ms) and keys_deleted_at_ms > 0,
           "keys_deleted_at must be a positive integer (epoch ms), got #{inspect(keys_deleted_at_ms)}"

    # Verify SQL statements captured — signing and encryption key soft-deletes
    sql_log = Agent.get(sql_bucket, & &1)
    sql_strings = Enum.map(sql_log, fn
      {sql, _args} -> sql
      other -> inspect(other)
    end)

    # Step 3: signing key soft-delete
    assert Enum.any?(sql_strings, &(String.contains?(&1, "signing") and String.contains?(&1, "private_key"))),
           "expected SQL to soft-delete signing key (private_key = NULL, key_type = 'signing'), got: #{inspect(sql_strings)}"

    # Step 4: encryption key soft-delete
    assert Enum.any?(sql_strings, &(String.contains?(&1, "encryption") and String.contains?(&1, "private_key"))),
           "expected SQL to soft-delete encryption key (private_key = NULL, key_type = 'encryption'), got: #{inspect(sql_strings)}"

    # Step 5: mark keys_deleted on users table
    assert Enum.any?(sql_strings, &(String.contains?(&1, "keys_deleted") and String.contains?(&1, "users"))),
           "expected SQL to update users.deletion_status = 'keys_deleted', got: #{inspect(sql_strings)}"

    # AC4 negative invariant: NO physical DELETE on user_keys (only soft-delete via UPDATE).
    # Public keys must remain — the row stays, only private_key is set NULL.
    refute Enum.any?(sql_strings, &(String.contains?(&1, "DELETE FROM user_keys"))),
           "expected NO physical DELETE on user_keys (AC4: public keys retained), got: #{inspect(sql_strings)}"
    refute Enum.any?(sql_strings, &(String.contains?(&1, "DELETE FROM users"))),
           "expected NO physical DELETE on users, got: #{inspect(sql_strings)}"

    # Verify audit: action='user_keys_deleted', outcome='success'
    audit_log = Agent.get(audit_bucket, & &1)
    assert length(audit_log) == 1, "expected exactly 1 audit entry, got #{length(audit_log)}"
    [audit_entry] = audit_log

    assert audit_entry.action == "user_keys_deleted",
           "expected audit action='user_keys_deleted', got #{inspect(audit_entry.action)}"
    assert audit_entry.outcome == "success",
           "expected audit outcome='success', got #{inspect(audit_entry.outcome)}"
    assert audit_entry.target_id == "target-user-42",
           "expected audit target_id='target-user-42', got #{inspect(audit_entry.target_id)}"
    assert Map.has_key?(audit_entry.metadata, :reason) or Map.has_key?(audit_entry.metadata, "reason"),
           "expected audit metadata to contain :reason, got #{inspect(audit_entry.metadata)}"
  end

  # ─── Test 9: Step 3 failure → TX rolled back + attempted audit ───────────────
  #
  # Given: user exists, step 3 (signing key delete) raises DB error
  # When:  delete_user_keys/3 called
  # Then:  {:error, _}
  #        deletion_in_progress NOT in DB (whole TX rolled back — Step 2 also undone)
  #        AuditWriter.log called with action='user_keys_deletion_attempted',
  #          outcome='attempted', metadata contains :reason AND :error keys

  test "step 3 failure rolls back and writes attempted audit in own TX",
       %{audit_bucket: audit_bucket} do
    Application.put_env(:compliance, :__test_fail_step__, :delete_signing)
    Application.put_env(:compliance, :repo, FailingTransactionFakeRepo)

    result =
      Compliance.UserDeletion.delete_user_keys(
        "admin-sub-1",
        "target-user-fail",
        "DSGVO deletion request ref GDPR-2026-042"
      )

    assert {:error, _} = result,
           "expected {:error, _} on step 3 DB failure, got #{inspect(result)}"

    # Attempted audit must be emitted in own TX
    audit_log = Agent.get(audit_bucket, & &1)
    assert length(audit_log) == 1,
           "expected exactly 1 attempted-audit entry, got #{length(audit_log)}: #{inspect(audit_log)}"

    [audit_entry] = audit_log
    assert audit_entry.action == "user_keys_deletion_attempted",
           "expected action='user_keys_deletion_attempted', got #{inspect(audit_entry.action)}"
    assert audit_entry.outcome == "attempted",
           "expected outcome='attempted', got #{inspect(audit_entry.outcome)}"

    # metadata must carry both :reason (or "reason") AND :error (or "error")
    has_reason = Map.has_key?(audit_entry.metadata, :reason) or Map.has_key?(audit_entry.metadata, "reason")
    has_error = Map.has_key?(audit_entry.metadata, :error) or Map.has_key?(audit_entry.metadata, "error")

    assert has_reason,
           "expected audit metadata to contain :reason key, got #{inspect(audit_entry.metadata)}"
    assert has_error,
           "expected audit metadata to contain :error key, got #{inspect(audit_entry.metadata)}"
  end

  # ─── Test 10: Concurrent deletion → {:error, :conflict} ─────────────────────
  #
  # Given: user has deletion_status = 'deletion_in_progress'
  # When:  delete_user_keys/3 called
  # Then:  {:error, :conflict}
  #        No attempted-audit emitted (conflict is not a TX failure)

  test "concurrent deletion: deletion_in_progress already true → returns {:error, :conflict}",
       %{audit_bucket: audit_bucket} do
    Application.put_env(:compliance, :__test_user_row__, %{
      exists: true,
      deletion_status: "deletion_in_progress"
    })

    result =
      Compliance.UserDeletion.delete_user_keys(
        "admin-sub-1",
        "target-user-concurrent",
        "DSGVO deletion request ref GDPR-2026-042"
      )

    assert result == {:error, :conflict},
           "expected {:error, :conflict} for concurrent deletion, got #{inspect(result)}"

    # No attempted audit must be emitted for conflict (it's a guard, not a TX failure)
    audit_log = Agent.get(audit_bucket, & &1)
    assert audit_log == [],
           "no audit must be emitted for conflict guard, got #{inspect(audit_log)}"
  end

  # ─── Test 10b: User not found → {:error, :user_not_found} ──────────────────
  #
  # Given: target user_id does not exist in DB (FakeRepo returns 0 rows)
  # When:  delete_user_keys/3 called
  # Then:  {:error, :user_not_found}
  #        No attempted-audit emitted (guard, not a transaction failure — AC3)
  #        No UPDATE SQL is issued (early return from Step 1 guard)

  test "user not found: returns {:error, :user_not_found} without attempted audit",
       %{sql_bucket: sql_bucket, audit_bucket: audit_bucket} do
    Application.put_env(:compliance, :__test_user_row__, %{exists: false})

    result =
      Compliance.UserDeletion.delete_user_keys(
        "admin-sub-1",
        "non-existent-user-99",
        "DSGVO deletion request ref GDPR-2026-042"
      )

    assert result == {:error, :user_not_found},
           "expected {:error, :user_not_found} for missing user, got #{inspect(result)}"

    # No attempted audit must be emitted (guard, not a TX failure — AC3)
    audit_log = Agent.get(audit_bucket, & &1)
    assert audit_log == [],
           "no audit must be emitted for :user_not_found guard, got #{inspect(audit_log)}"

    # No UPDATE SQL was issued — early return from Step 1 guard
    sql_log = Agent.get(sql_bucket, & &1)
    sql_strings = Enum.map(sql_log, fn
      {sql, _args} -> sql
      other -> inspect(other)
    end)
    refute Enum.any?(sql_strings, &String.contains?(&1, "UPDATE")),
           "expected NO UPDATE SQL when user not found, got: #{inspect(sql_strings)}"
  end

  # ─── Test 11: Subsequent decrypt_sensitive_pii with nil private_key ──────────
  #
  # AC5 / Regression-Guard: after key deletion private_key = nil;
  # Nebu.Signature.decrypt_sensitive_pii/4 must return {:error, :no_private_key}
  # for any nil private_key argument.
  #
  # This test calls the EXISTING Signature module (already implemented in Story 2.11).
  # It does NOT test UserDeletion directly — it proves the nil-guard at line 132 of
  # signature.ex is intact and will remain so.

  test "subsequent decrypt returns {:error, :no_private_key} — nil private_key guard in Signature" do
    # After key deletion, private_key = nil.
    # decrypt_sensitive_pii/4 with nil private_key must return {:error, :no_private_key}.
    # (The guard at signature.ex line 132: def decrypt_sensitive_pii(_, _, _, nil))
    ciphertext = "some-encrypted-data"
    ephemeral_public_key = "ephemeral-key"
    nonce = "nonce-value"
    private_key = nil

    result = Nebu.Signature.decrypt_sensitive_pii(ciphertext, ephemeral_public_key, nonce, private_key)

    assert result == {:error, :no_private_key},
           "expected {:error, :no_private_key} when private_key is nil (AC5 guard), got #{inspect(result)}"
  end

  # ─── Story 5.29b — AC5 (FB-57-01): Atomic guard — concurrent calls ──────────
  #
  # ALL tests below are expected to FAIL until Story 5.29b is implemented.
  #
  # AC5 spec:
  #   Replace SELECT+UPDATE guard with a single conditional UPDATE:
  #     UPDATE users
  #     SET deletion_status = 'deletion_in_progress'
  #     WHERE id = $1
  #       AND (deletion_status IS NULL OR deletion_status = 'active')
  #     RETURNING id
  #   0 rows returned → {:error, :conflict}
  #
  # Failing reason: current implementation uses a two-step SELECT+UPDATE which has
  # a TOCTOU window. The new implementation must use a single conditional UPDATE.
  #
  # Test: two concurrent Task.async calls to delete_user_keys/3.
  #   - Exactly one call must return {:ok, _}.
  #   - Exactly one call must return {:error, :conflict}.
  #   - Exactly one audit entry with action='user_keys_deleted' must exist.
  #
  # Note: this test uses a real concurrent scenario via Task.async/1.
  # The FakeRepo for this test must simulate the atomic-UPDATE race:
  #   - First caller's UPDATE matches and returns 1 row.
  #   - Second caller's UPDATE finds no matching row (deletion_status already changed)
  #     and returns 0 rows → :conflict.

  defmodule AtomicUpdateFakeRepo do
    @moduledoc """
    FakeRepo variant that simulates the atomic conditional UPDATE for deletion_in_progress.
    Uses an Agent to track whether the first caller has already set deletion_in_progress.
    The first UPDATE call succeeds (1 row); subsequent calls return 0 rows (conflict).
    """

    # Shared Agent pid is stored in :compliance app env before the test.
    # This simulates the DB row state that the conditional UPDATE observes.

    def transaction(fun) when is_function(fun, 0) do
      result = fun.()
      case result do
        {:error, _} = err -> err
        other -> {:ok, other}
      end
    end

    def query(sql, args, _opts \\ []) do
      cond do
        # Atomic conditional UPDATE:
        #   UPDATE users SET deletion_status='deletion_in_progress'
        #   WHERE id=$1 AND (deletion_status IS NULL OR deletion_status='active')
        #   RETURNING id
        #
        # First caller: state is nil/active → matches → returns 1 row.
        # Second caller: state is now 'deletion_in_progress' → no match → 0 rows.
        String.contains?(sql, "deletion_in_progress") and
          String.contains?(sql, "WHERE") and
            String.contains?(sql, "RETURNING") ->
          agent = Application.get_env(:compliance, :__test_atomic_agent__)

          if agent do
            claimed = Agent.get_and_update(agent, fn claimed ->
              {claimed, true}
            end)

            if claimed do
              # Second caller: conditional UPDATE found 0 matching rows → conflict.
              {:ok, %{rows: [], num_rows: 0}}
            else
              # First caller: UPDATE succeeded.
              user_id = List.first(args) || "target-user-concurrent"
              {:ok, %{rows: [[user_id]], num_rows: 1}}
            end
          else
            # Fallback: not configured for atomic test.
            user_id = List.first(args) || "target-user-concurrent"
            {:ok, %{rows: [[user_id]], num_rows: 1}}
          end

        # Guard SELECT: check user existence and current deletion_status.
        String.contains?(sql, "deletion_status") and String.contains?(sql, "FROM users WHERE") ->
          {:ok, %{rows: [["target-user-concurrent", nil]], num_rows: 1}}

        # Signing key soft-delete.
        String.contains?(sql, "signing") ->
          {:ok, %{rows: [], num_rows: 1}}

        # Encryption key soft-delete.
        String.contains?(sql, "encryption") ->
          {:ok, %{rows: [], num_rows: 1}}

        # keys_deleted status update.
        String.contains?(sql, "keys_deleted") and String.contains?(sql, "UPDATE users") ->
          {:ok, %{rows: [], num_rows: 1}}

        true ->
          {:ok, %{rows: [], num_rows: 0}}
      end
    end

    def insert(struct, _opts \\ []) do
      bucket = Application.get_env(:compliance, :__test_fake_repo_bucket__)
      if bucket, do: Agent.update(bucket, fn rows -> [struct | rows] end)
      {:ok, struct}
    end

    def insert!(_struct, _opts \\ []), do: raise("AtomicUpdateFakeRepo.insert! unexpected")
  end

  # ─── Test 13: Concurrent calls — exactly one wins, one gets :conflict ─────────
  #
  # Given: user exists, deletion_status = nil (active)
  # When:  two simultaneous calls to delete_user_keys/3 via Task.async
  # Then:  exactly 1 {:ok, _} and exactly 1 {:error, :conflict}
  #        exactly 1 audit entry with action='user_keys_deleted'
  #        (no :conflict audit entry — conflict is a guard, not a TX failure)
  #
  # RED-PHASE: this test will FAIL until the atomic conditional UPDATE is implemented.
  # With the current SELECT+UPDATE implementation, both calls may see deletion_status=nil
  # in the SELECT guard and both proceed, producing 2 {:ok, _} responses and 2 audits.

  test "concurrent calls — exactly one wins, one gets :conflict, exactly one audit",
       %{audit_bucket: audit_bucket} do
    # Start an Agent to simulate the DB's atomic row-level lock.
    {:ok, atomic_agent} = Agent.start_link(fn -> false end)

    Application.put_env(:compliance, :repo, AtomicUpdateFakeRepo)
    Application.put_env(:compliance, :__test_atomic_agent__, atomic_agent)

    # Launch two concurrent calls using Task.async.
    task_a =
      Task.async(fn ->
        Compliance.UserDeletion.delete_user_keys(
          "admin-sub-concurrent",
          "target-user-concurrent",
          "DSGVO concurrent deletion test A"
        )
      end)

    task_b =
      Task.async(fn ->
        Compliance.UserDeletion.delete_user_keys(
          "admin-sub-concurrent",
          "target-user-concurrent",
          "DSGVO concurrent deletion test B"
        )
      end)

    result_a = Task.await(task_a, 5000)
    result_b = Task.await(task_b, 5000)

    results = [result_a, result_b]

    ok_count =
      Enum.count(results, fn
        {:ok, _} -> true
        _ -> false
      end)

    conflict_count =
      Enum.count(results, fn
        {:error, :conflict} -> true
        _ -> false
      end)

    # RED-PHASE ASSERTION: exactly one winner, one conflict.
    assert ok_count == 1,
           "expected exactly 1 {:ok, _} from concurrent calls, got #{ok_count} — " <>
             "results: #{inspect(results)} — " <>
             "Story 5.29b AC5 atomic conditional UPDATE not yet implemented"

    assert conflict_count == 1,
           "expected exactly 1 {:error, :conflict} from concurrent calls, got #{conflict_count} — " <>
             "results: #{inspect(results)} — " <>
             "Story 5.29b AC5 atomic conditional UPDATE not yet implemented"

    # Exactly 1 audit entry (only the winner emits 'user_keys_deleted').
    audit_log = Agent.get(audit_bucket, & &1)
    assert length(audit_log) == 1,
           "expected exactly 1 audit entry (winner only), got #{length(audit_log)}: #{inspect(audit_log)}"

    [audit_entry] = audit_log

    assert audit_entry.action == "user_keys_deleted",
           "expected audit action='user_keys_deleted', got #{inspect(audit_entry.action)}"

    assert audit_entry.outcome == "success",
           "expected audit outcome='success', got #{inspect(audit_entry.outcome)}"

    # Cleanup.
    Agent.stop(atomic_agent)
  end

  # ─── Test 12: Audit metadata — success vs. attempted shape ───────────────────
  #
  # Given: success path → audit metadata has only :reason (no :error key)
  # Given: attempted path → audit metadata has both :reason and :error keys
  #
  # This test is intentionally structural — it verifies the metadata shape contract
  # that the gRPC handler and consuming tools rely on.

  test "audit metadata: success path has only :reason; attempted has :reason + :error",
       %{audit_bucket: audit_bucket} do
    # ── Success path ──────────────────────────────────────────────────────────

    result =
      Compliance.UserDeletion.delete_user_keys(
        "admin-sub-meta",
        "target-user-meta",
        "DSGVO deletion request for metadata test"
      )

    assert {:ok, _} = result, "expected success path to return {:ok, _}, got #{inspect(result)}"

    audit_log = Agent.get(audit_bucket, & &1)
    assert length(audit_log) == 1, "expected 1 audit entry on success, got #{length(audit_log)}"
    [success_audit] = audit_log

    assert success_audit.action == "user_keys_deleted"
    meta = success_audit.metadata
    has_reason = Map.has_key?(meta, :reason) or Map.has_key?(meta, "reason")
    has_error = Map.has_key?(meta, :error) or Map.has_key?(meta, "error")
    assert has_reason, "success audit metadata must have :reason, got #{inspect(meta)}"
    refute has_error, "success audit metadata must NOT have :error key, got #{inspect(meta)}"

    # ── Attempted path ────────────────────────────────────────────────────────

    # Reset bucket + configure failing step
    Agent.update(audit_bucket, fn _ -> [] end)
    Application.put_env(:compliance, :__test_fail_step__, :delete_encryption)
    Application.put_env(:compliance, :repo, FailingTransactionFakeRepo)

    result2 =
      Compliance.UserDeletion.delete_user_keys(
        "admin-sub-meta",
        "target-user-meta-fail",
        "DSGVO deletion request for metadata test"
      )

    assert {:error, _} = result2, "expected failure path, got #{inspect(result2)}"

    audit_log2 = Agent.get(audit_bucket, & &1)
    assert length(audit_log2) == 1, "expected 1 attempted-audit entry, got #{length(audit_log2)}"
    [attempted_audit] = audit_log2

    assert attempted_audit.action == "user_keys_deletion_attempted"
    meta2 = attempted_audit.metadata
    has_reason2 = Map.has_key?(meta2, :reason) or Map.has_key?(meta2, "reason")
    has_error2 = Map.has_key?(meta2, :error) or Map.has_key?(meta2, "error")
    assert has_reason2, "attempted audit metadata must have :reason, got #{inspect(meta2)}"
    assert has_error2, "attempted audit metadata must have :error, got #{inspect(meta2)}"
  end
end
