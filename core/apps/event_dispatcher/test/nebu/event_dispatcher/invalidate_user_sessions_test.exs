defmodule Nebu.EventDispatcher.InvalidateUserSessionsTest do
  use ExUnit.Case, async: true

  # ─── Story 6.5: gRPC InvalidateUserSessions handler test ─────────────────────
  #
  # RED PHASE — all tests fail until Story 6.5 is implemented.
  # Failing reasons:
  #   1. Core.InvalidateUserSessionsRequest / Core.InvalidateUserSessionsResponse
  #      do not exist yet (proto rpc not defined → compiled pb modules missing).
  #      They will be generated after `make proto` runs on the updated core.proto.
  #   2. Nebu.EventDispatcher.Server.invalidate_user_sessions/2 does not exist yet.
  #   3. The :session_supervisor_module Application.get_env key is not used by
  #      Nebu.EventDispatcher.Server yet (configurable-module pattern not wired).
  #
  # Test strategy:
  #   - Call Server.invalidate_user_sessions/2 directly (unary gRPC handler, synchronous).
  #   - FakeSessionSupervisor spy (injected via Application.put_env) records calls
  #     and sends {:destroy_called, user_id} to the test process.
  #   - FailingSessionSupervisor simulates DB failure → raises GRPC.RPCError.
  #   - async: true — no Horde/global ETS needed for these pure handler tests.
  #
  # Covered Acceptance Criteria:
  #   - AC#5   Elixir handler calls SessionSupervisor.destroy_session/1
  #   - AC#11  Session invalidation test (Acceptance Test #9):
  #             Given sessions in ETS + DB, When handler called,
  #             Then ETS entry removed, sessions row deleted, sync_tokens row deleted.

  alias Nebu.EventDispatcher.Server

  # ─── FakeSessionSupervisor ────────────────────────────────────────────────────
  # Injects a test spy that records destroy_session/1 calls.
  # The test process PID is stored in Application env under :__test_pid__ so
  # the spy can send a message back for assertion.

  defmodule FakeSessionSupervisor do
    @moduledoc """
    Test spy replacing Nebu.Session.SessionSupervisor in handler tests.
    Inject via Application.put_env(:event_dispatcher, :session_supervisor_module, __MODULE__).
    """

    def destroy_session(user_id) do
      test_pid = Application.get_env(:event_dispatcher, :__test_pid__)

      if test_pid && Process.alive?(test_pid) do
        send(test_pid, {:destroy_called, user_id})
      end

      :ok
    end
  end

  # ─── FailingSessionSupervisor ─────────────────────────────────────────────────
  # Simulates a DB failure in destroy_session/1 to verify GRPC.RPCError handling.

  defmodule FailingSessionSupervisor do
    @moduledoc """
    Simulates a failure in SessionSupervisor.destroy_session/1.
    Inject via Application.put_env(:event_dispatcher, :session_supervisor_module, __MODULE__).
    """

    def destroy_session(_user_id) do
      {:error, :db_connection_failed}
    end
  end

  # ─── Setup ────────────────────────────────────────────────────────────────────

  setup do
    Application.put_env(:event_dispatcher, :session_supervisor_module, FakeSessionSupervisor)
    Application.put_env(:event_dispatcher, :__test_pid__, self())

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :session_supervisor_module)
      Application.delete_env(:event_dispatcher, :__test_pid__)
    end)

    :ok
  end

  defp build_stream, do: %{http_request_headers: %{}}

  # ─── AC#5 + AC#11: invalidate_user_sessions happy path ───────────────────────
  #
  # Given: FakeSessionSupervisor is injected; ETS session exists for @alice:example.com
  # When:  Server.invalidate_user_sessions/2 is called with user_id="@alice:example.com"
  # Then:  Returns %Core.InvalidateUserSessionsResponse{ok: true}
  # And:   FakeSessionSupervisor.destroy_session/1 was called with "@alice:example.com"

  test "invalidate_user_sessions — returns InvalidateUserSessionsResponse{ok: true} and calls destroy_session/1" do
    request = %Core.InvalidateUserSessionsRequest{
      user_id: "@alice:example.com"
    }

    response = Server.invalidate_user_sessions(request, build_stream())

    assert %Core.InvalidateUserSessionsResponse{ok: true} = response,
           "expected InvalidateUserSessionsResponse{ok: true}, got #{inspect(response)}"

    assert_receive {:destroy_called, "@alice:example.com"},
                   200,
                   "expected SessionSupervisor.destroy_session/1 to be called with '@alice:example.com'"
  end

  # ─── AC#5: destroy_session called with correct user_id ───────────────────────
  #
  # Given: Request for @bob:example.com
  # When:  invalidate_user_sessions/2 is called
  # Then:  destroy_session/1 receives exactly "@bob:example.com" (not a different ID)

  test "invalidate_user_sessions — destroy_session/1 receives the exact user_id from request" do
    request = %Core.InvalidateUserSessionsRequest{
      user_id: "@bob:example.com"
    }

    _response = Server.invalidate_user_sessions(request, build_stream())

    assert_receive {:destroy_called, "@bob:example.com"},
                   200,
                   "expected destroy_session/1 to be called with '@bob:example.com'"

    # Ensure no spurious calls for a different user ID
    refute_receive {:destroy_called, "@alice:example.com"}, 50,
                   "destroy_session/1 must not be called for a different user ID"
  end

  # ─── AC#5: DB failure → GRPC.RPCError with internal status ──────────────────
  #
  # Given: FailingSessionSupervisor returns {:error, :db_connection_failed}
  # When:  invalidate_user_sessions/2 is called
  # Then:  Raises GRPC.RPCError with status=GRPC.Status.internal()
  #
  # The handler must NOT return ok=false on DB failure — it must raise a gRPC error
  # so the Go gateway can surface the failure correctly (best-effort, logged as warning).

  test "invalidate_user_sessions — DB failure raises GRPC.RPCError with internal status" do
    Application.put_env(:event_dispatcher, :session_supervisor_module, FailingSessionSupervisor)

    request = %Core.InvalidateUserSessionsRequest{
      user_id: "@alice:example.com"
    }

    assert_raise GRPC.RPCError, fn ->
      Server.invalidate_user_sessions(request, build_stream())
    end
  end

  test "invalidate_user_sessions — raised GRPC.RPCError has internal status code on DB failure" do
    Application.put_env(:event_dispatcher, :session_supervisor_module, FailingSessionSupervisor)

    request = %Core.InvalidateUserSessionsRequest{
      user_id: "@alice:example.com"
    }

    error =
      try do
        Server.invalidate_user_sessions(request, build_stream())
        nil
      rescue
        e in GRPC.RPCError -> e
      end

    refute is_nil(error), "expected GRPC.RPCError to be raised but got nil"

    assert error.status == GRPC.Status.internal(),
           "expected GRPC.Status.internal() (#{GRPC.Status.internal()}), got #{error.status}"
  end

  # ─── AC#11: Session fully invalidated (ETS + DB) ─────────────────────────────
  #
  # Acceptance Test #9 from story:
  # Given: ETS contains a session for @alice:example.com; sessions + sync_tokens rows exist
  # When:  InvalidateUserSessions gRPC handler is called
  # Then:  ETS entry removed, sessions row deleted, sync_tokens row deleted
  #
  # At unit-test level, we verify the contract boundary (destroy_session/1 is called
  # with the correct user_id). The full ETS + DB removal is covered by the
  # Nebu.Session.SessionSupervisor integration tests (already implemented in Story 4.6).
  # This test verifies the gRPC handler correctly delegates to SessionSupervisor.

  test "invalidate_user_sessions — AC#11/AT#9: handler delegates to SessionSupervisor.destroy_session/1 for ETS+DB cleanup" do
    # Given: FakeSessionSupervisor simulates the full destroy_session/1 contract
    # (real implementation calls PgStore.invalidate_session/1 → ETS eviction)
    request = %Core.InvalidateUserSessionsRequest{
      user_id: "@alice:example.com"
    }

    response = Server.invalidate_user_sessions(request, build_stream())

    # Then: handler returns ok=true (ETS + DB cleanup delegated to SessionSupervisor)
    assert %Core.InvalidateUserSessionsResponse{ok: true} = response,
           "AC#11/AT#9: expected ok=true indicating successful session invalidation"

    # And: destroy_session/1 was called (ETS + DB cleanup triggered)
    assert_receive {:destroy_called, "@alice:example.com"},
                   200,
                   "AC#11/AT#9: expected destroy_session/1 to be called for ETS+DB cleanup"
  end
end
