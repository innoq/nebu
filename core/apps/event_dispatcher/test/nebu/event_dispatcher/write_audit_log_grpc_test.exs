defmodule Nebu.EventDispatcher.WriteAuditLogGrpcTest do
  use ExUnit.Case, async: true

  # ─── Story 5-2: gRPC WriteAuditLog handler tests ─────────────────────────────
  #
  # ALL tests in this module are expected to FAIL until Story 5-2 is implemented.
  # Failing reasons:
  #   1. Core.WriteAuditLogRequest / Core.WriteAuditLogResponse do not exist yet
  #      (proto WriteAuditLog RPC not defined → compiled pb modules missing)
  #   2. Nebu.EventDispatcher.Server.write_audit_log/2 does not exist yet
  #   3. Compliance.AuditWriter.log/7 does not exist yet
  #
  # Test strategy:
  #   - Call Server.write_audit_log/2 directly (unary handler, synchronous).
  #   - FakeAuditWriter spy (injected via Application.put_env) records log calls.
  #   - async: true — no Horde/global ETS needed for these pure handler tests.

  alias Nebu.EventDispatcher.Server

  # ─── FakeAuditWriter ─────────────────────────────────────────────────────────

  defmodule FakeAuditWriter do
    def log(actor, action, target_type, target_id, metadata, outcome, error_detail \\ nil) do
      test_pid = Application.get_env(:compliance, :__grpc_test_pid__)
      if test_pid && Process.alive?(test_pid) do
        send(test_pid, {:audit_call, actor, action, target_type, target_id, metadata, outcome, error_detail})
      end
      :ok
    end
  end

  # ─── FailingFakeAuditWriter ───────────────────────────────────────────────────

  defmodule FailingFakeAuditWriter do
    def log(_actor, _action, _target_type, _target_id, _metadata, _outcome, _error_detail \\ nil) do
      {:error, :audit_write_failed}
    end
  end

  # ─── Setup ────────────────────────────────────────────────────────────────────

  setup do
    Application.put_env(:compliance, :audit_writer, FakeAuditWriter)
    Application.put_env(:compliance, :__grpc_test_pid__, self())

    on_exit(fn ->
      Application.delete_env(:compliance, :audit_writer)
      Application.delete_env(:compliance, :__grpc_test_pid__)
    end)

    :ok
  end

  defp build_stream, do: %{http_request_headers: %{}}

  # ─── Test 5: write_audit_log gRPC happy path ──────────────────────────────────
  #
  # Given: Valid WriteAuditLogRequest; FakeAuditWriter returns :ok
  # When: Server.write_audit_log/2 called
  # Then: Returns %Core.WriteAuditLogResponse{ok: true}
  # And:  FakeAuditWriter.log/7 was called with the correct fields

  test "write_audit_log — happy path returns WriteAuditLogResponse{ok: true}" do
    request = %Core.WriteAuditLogRequest{
      actor_user_id: "user-1",
      action: "admin_login",
      target_type: "user",
      target_id: "user-1",
      metadata_json: ~s({"ip":"127.0.0.1"}),
      outcome: "success",
      error_detail: ""
    }

    response = Server.write_audit_log(request, build_stream())

    assert %Core.WriteAuditLogResponse{ok: true} = response,
           "expected WriteAuditLogResponse{ok: true}, got #{inspect(response)}"

    assert_receive {:audit_call, "user-1", "admin_login", "user", "user-1", _meta, "success", nil},
                   200,
                   "expected AuditWriter.log/7 to be called with correct fields"
  end

  # ─── Test 6: write_audit_log — AuditWriter failure returns ok: false ─────────
  #
  # Given: FailingFakeAuditWriter that returns {:error, :audit_write_failed}
  # When: Server.write_audit_log/2 called
  # Then: Returns %Core.WriteAuditLogResponse{ok: false}
  #
  # Policy: The gRPC handler surfaces AuditWriter errors to the Go caller via
  # ok=false, so Go can decide whether to log a warning. Go's LogEvent always
  # returns nil regardless — but the handler correctly propagates the failure signal.

  test "write_audit_log — AuditWriter failure returns WriteAuditLogResponse{ok: false}" do
    Application.put_env(:compliance, :audit_writer, FailingFakeAuditWriter)

    request = %Core.WriteAuditLogRequest{
      actor_user_id: "user-1",
      action: "admin_login",
      target_type: "user",
      target_id: "user-1",
      metadata_json: "{}",
      outcome: "success",
      error_detail: ""
    }

    response = Server.write_audit_log(request, build_stream())

    assert %Core.WriteAuditLogResponse{ok: false} = response,
           "expected WriteAuditLogResponse{ok: false} when AuditWriter fails, got #{inspect(response)}"
  end

  # ─── Bonus: empty error_detail is converted to nil before AuditWriter call ───
  #
  # AC3 spec: error_detail "" → nil passed to AuditWriter.
  # This prevents the DB from storing empty string instead of NULL.

  test "write_audit_log — empty error_detail string is passed as nil to AuditWriter" do
    request = %Core.WriteAuditLogRequest{
      actor_user_id: "user-1",
      action: "admin_logout",
      target_type: "user",
      target_id: "user-1",
      metadata_json: "{}",
      outcome: "success",
      error_detail: ""
    }

    _response = Server.write_audit_log(request, build_stream())

    assert_receive {:audit_call, "user-1", "admin_logout", "user", "user-1", _meta, "success", nil},
                   200,
                   "expected error_detail=nil when gRPC request has error_detail=''"
  end
end
