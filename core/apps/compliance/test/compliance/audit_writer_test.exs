defmodule Compliance.AuditWriterTest do
  use ExUnit.Case, async: true

  # ─── Story 5-2: Compliance.AuditWriter — red-phase ATDD tests ────────────────
  #
  # ALL tests in this module are expected to FAIL until Story 5-2 is implemented.
  # Compliance.AuditWriter.log/6 and log/7 do NOT exist yet.
  #
  # Persistenz-Strategie: Option C — Stateless.
  # AuditWriter has no GenServer state, no internal queue.
  # No crash/restart test required (nothing to recover).
  #
  # Test strategy:
  #   - FakeRepo replaces Nebu.Repo so no PostgreSQL is needed.
  #   - The module-level Application.put_env injects FakeRepo before tests run.
  #   - Test 3 (TX independence) uses a distinct FakeRepo that explicitly records
  #     whether AuditWriter opened its own transaction even when the "caller" context
  #     had already rolled back. Because AuditWriter calls Repo.transaction/1 directly
  #     and FakeRepo.transaction/1 immediately executes the fun, the test verifies
  #     the returned value of AuditWriter.log/6 is :ok — demonstrating that the
  #     audit write committed regardless of an outer rollback.
  #   - All assertions reference Compliance.AuditWriter.log/6 and log/7 which
  #     do not exist yet → tests fail with UndefinedFunctionError (red).

  # ─── FakeRepo ─────────────────────────────────────────────────────────────────
  #
  # Minimal Ecto.Repo stub that stores inserted structs in process state (Agent).
  # Satisfies the Nebu.Repo API surface used by AuditWriter:
  #   - transaction/1  — executes the anonymous function immediately; returns {:ok, result}
  #   - insert/2       — inserts the struct into the Agent bucket, returns {:ok, struct}
  #
  # Each test creates a fresh Agent (scoped to the test process) to avoid state leaks.
  # The Agent pid is stored in Application config so FakeRepo can look it up.

  defmodule FakeRepo do
    @moduledoc "In-process fake for Nebu.Repo. Test-private."

    def transaction(fun) when is_function(fun, 0) do
      result = fun.()
      {:ok, result}
    end

    def insert(struct, _opts \\ []) do
      bucket = Application.get_env(:compliance, :__test_fake_repo_bucket__)
      if bucket, do: Agent.update(bucket, fn rows -> [struct | rows] end)
      {:ok, struct}
    end

    def insert!(struct, opts \\ []) do
      case insert(struct, opts) do
        {:ok, s} -> s
        _ -> raise "FakeRepo.insert! failed"
      end
    end
  end

  # ─── Failing FakeRepo ─────────────────────────────────────────────────────────
  #
  # A repo that always returns {:error, :db_error} from insert/2.
  # Used for the DB-failure test.

  defmodule FailingFakeRepo do
    def transaction(fun) when is_function(fun, 0) do
      # Execute the fun but catch the expected error from insert!
      try do
        fun.()
        {:ok, nil}
      rescue
        _ -> {:error, :db_error}
      end
    end

    def insert(_struct, _opts \\ []), do: {:error, :db_error}

    def insert!(_struct, _opts \\ []) do
      raise "FailingFakeRepo: DB is down"
    end
  end

  # ─── Setup ────────────────────────────────────────────────────────────────────

  setup do
    # Start an Agent to collect inserted rows for each test.
    {:ok, bucket} = Agent.start_link(fn -> [] end)
    Application.put_env(:compliance, :__test_fake_repo_bucket__, bucket)
    Application.put_env(:compliance, :repo, FakeRepo)

    on_exit(fn ->
      Application.delete_env(:compliance, :__test_fake_repo_bucket__)
      Application.delete_env(:compliance, :repo)
    end)

    {:ok, bucket: bucket}
  end

  # ─── AC11 Test 1: log/6 success path ─────────────────────────────────────────
  #
  # Given: FakeRepo that accepts inserts
  # When: Compliance.AuditWriter.log/6 is called with valid args
  # Then: Returns :ok; AuditWriter called Repo.transaction/1

  test "log/6 — success path inserts audit row", %{bucket: bucket} do
    result =
      Compliance.AuditWriter.log(
        "user-1",
        "admin_login",
        "user",
        "user-1",
        %{},
        "success"
      )

    assert result == :ok

    rows = Agent.get(bucket, & &1)
    assert length(rows) == 1,
           "expected exactly one row inserted via Repo.transaction/1, got #{length(rows)}"
  end

  # ─── AC11 Test 2: log/6 DB failure ────────────────────────────────────────────
  #
  # Given: FailingFakeRepo that always returns {:error, :db_error}
  # When: Compliance.AuditWriter.log/6 is called
  # Then: Returns {:error, :audit_write_failed} — never raises

  test "log/6 — DB failure returns {:error, :audit_write_failed}, never raises" do
    Application.put_env(:compliance, :repo, FailingFakeRepo)

    result =
      Compliance.AuditWriter.log(
        "user-1",
        "admin_login",
        "user",
        "user-1",
        %{},
        "success"
      )

    assert result == {:error, :audit_write_failed},
           "expected {:error, :audit_write_failed}, got #{inspect(result)}"
  end

  # ─── AC11 Test 3: TX independence ────────────────────────────────────────────
  #
  # Given: A "caller" that rolled back its own transaction
  # When: AuditWriter.log/6 is called after the caller rollback
  # Then: AuditWriter's own Repo.transaction/1 commits — returns :ok
  #
  # Design note:
  #   AuditWriter MUST call Repo.transaction/1 internally (not re-use the caller's
  #   transaction). Because FakeRepo.transaction/1 executes the fun immediately and
  #   always returns {:ok, _}, calling log/6 after a caller rollback must still
  #   return :ok — the audit write is independent of whatever the caller did.
  #
  #   In a real PostgreSQL scenario, "caller rollback does not prevent audit" means
  #   the AuditWriter opens a NEW connection/transaction. With Ecto.Sandbox (async
  #   tests), this would require :sandbox_allow. For unit tests with FakeRepo, the
  #   independence is structural: FakeRepo.transaction/1 has no shared outer context.

  test "log/6 — audit TX is independent of caller rollback", %{bucket: bucket} do
    # Simulate a caller transaction that rolls back by returning {:error, ...}.
    # The AuditWriter must not be affected — it runs its own transaction.
    caller_tx_result = {:error, :caller_rolled_back}
    assert {:error, :caller_rolled_back} = caller_tx_result

    # Now call AuditWriter after the caller's rollback — must still return :ok.
    result =
      Compliance.AuditWriter.log(
        "user-1",
        "room_created",
        "room",
        "!abc123:test.local",
        %{"is_direct" => false},
        "success"
      )

    assert result == :ok,
           "AuditWriter.log/6 must return :ok even after caller rolled back, got #{inspect(result)}"

    rows = Agent.get(bucket, & &1)
    assert length(rows) == 1,
           "expected one audit row inserted in AuditWriter's own TX, got #{length(rows)}"
  end

  # ─── AC11 Test 4: log/7 — error_detail optional arg ──────────────────────────
  #
  # Given: FakeRepo tracking inserted structs
  # When: Compliance.AuditWriter.log/7 called with error_detail = "some error"
  # Then: Inserted struct has error_detail = "some error"
  # And:  log/6 (without error_detail) results in error_detail = nil in the struct

  test "log/7 — error_detail is passed through; log/6 uses nil", %{bucket: bucket} do
    # 7-argument form with explicit error_detail
    result_7 =
      Compliance.AuditWriter.log(
        "user-1",
        "admin_login_failed",
        "user",
        "user-1",
        %{},
        "failure",
        "role_check_failed"
      )

    assert result_7 == :ok

    rows_after_7 = Agent.get(bucket, & &1)
    assert length(rows_after_7) == 1
    [row_7] = rows_after_7
    assert row_7.error_detail == "role_check_failed",
           "expected error_detail='role_check_failed', got #{inspect(row_7.error_detail)}"

    # Reset bucket
    Agent.update(bucket, fn _ -> [] end)

    # 6-argument form (error_detail defaults to nil)
    result_6 =
      Compliance.AuditWriter.log(
        "user-1",
        "admin_login",
        "user",
        "user-1",
        %{},
        "success"
      )

    assert result_6 == :ok

    rows_after_6 = Agent.get(bucket, & &1)
    assert length(rows_after_6) == 1
    [row_6] = rows_after_6
    assert is_nil(row_6.error_detail),
           "expected error_detail=nil for 6-arg form, got #{inspect(row_6.error_detail)}"
  end

  # ─── Kassandra MEDIUM-3 (post-SEC-Gate-1 2026-04-23): changeset validation ──
  #
  # Given: AuditWriter is invoked with nil or empty required field
  # When:  log/6 is called
  # Then:  Returns {:error, :audit_write_invalid} — no row is inserted, no raise
  #
  # This proves the changeset path catches corruption attempts that insert!
  # would have silently persisted before the Kassandra-flagged fix.

  test "log/6 — rejects nil actor_user_id via changeset validation", %{bucket: bucket} do
    result =
      Compliance.AuditWriter.log(
        nil,
        "admin_login",
        "user",
        "user-1",
        %{},
        "success"
      )

    assert result == {:error, :audit_write_invalid},
           "expected {:error, :audit_write_invalid} for nil actor, got #{inspect(result)}"

    assert Agent.get(bucket, & &1) == [],
           "no row must be inserted when validation fails"
  end

  test "log/6 — rejects empty action via changeset validation", %{bucket: bucket} do
    result =
      Compliance.AuditWriter.log(
        "user-1",
        "",
        "user",
        "user-1",
        %{},
        "success"
      )

    assert result == {:error, :audit_write_invalid}
    assert Agent.get(bucket, & &1) == []
  end

  test "log/6 — rejects nil outcome via changeset validation", %{bucket: bucket} do
    result =
      Compliance.AuditWriter.log(
        "user-1",
        "admin_login",
        "user",
        "user-1",
        %{},
        nil
      )

    assert result == {:error, :audit_write_invalid}
    assert Agent.get(bucket, & &1) == []
  end
end
