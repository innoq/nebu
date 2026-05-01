defmodule Nebu.EventDispatcher.InvalidateAllAdminSessionsTest do
  use ExUnit.Case, async: false

  # ─── Story 6.10: gRPC InvalidateAllAdminSessions handler tests ───────────────
  #
  # RED PHASE — all tests fail until Story 6.10 is implemented.
  # Failing reasons:
  #   1. Core.InvalidateAllAdminSessionsRequest / Core.InvalidateAllAdminSessionsResponse
  #      do not exist yet. They will be generated after `make proto` runs on the
  #      updated core.proto (which adds the InvalidateAllAdminSessions RPC).
  #   2. Nebu.EventDispatcher.Server.invalidate_all_admin_sessions/2 does not exist yet.
  #   3. Nebu.Session.EtsStore.list_user_ids/0 does not exist yet.
  #      It must be added to ets_store.ex:
  #        def list_user_ids, do: :ets.tab2list(:NebuSessions) |> Enum.map(fn {uid, _} -> uid end)
  #
  # Test strategy:
  #   - Use async: false because tests share the :NebuSessions ETS table.
  #   - Call Server.invalidate_all_admin_sessions/2 directly (unary gRPC handler, synchronous).
  #   - FakeSessionSupervisorForAll spy (injected via Application.put_env) records all
  #     destroy_session/1 calls and sends {:destroy_called, user_id} to the test process.
  #   - Tests populate the ETS table directly via Nebu.Session.EtsStore.put_session/2.
  #   - Each test cleans up ETS entries in on_exit to prevent cross-test pollution.
  #
  # Covered Acceptance Criteria:
  #   - AC#6   Elixir: 2 sessions in ETS → destroy_session called twice; returns ok: true
  #   - AC#7   Elixir: empty ETS → returns ok: true (no-op)

  alias Nebu.EventDispatcher.Server
  alias Nebu.Session.EtsStore

  # ─── FakeSessionSupervisorForAll ─────────────────────────────────────────────
  # Spy that records every destroy_session/1 call.
  # The test process PID is stored in Application env under :__all_sessions_test_pid__
  # so the spy can send a message back for assertion.

  defmodule FakeSessionSupervisorForAll do
    @moduledoc """
    Test spy replacing Nebu.Session.SessionSupervisor in handler tests.
    Inject via Application.put_env(:event_dispatcher, :session_supervisor_module, __MODULE__).
    Records each destroy_session/1 call by sending {:destroy_called, user_id} to
    the test process stored under :__all_sessions_test_pid__.
    """

    def destroy_session(user_id) do
      test_pid = Application.get_env(:event_dispatcher, :__all_sessions_test_pid__)

      if test_pid && Process.alive?(test_pid) do
        send(test_pid, {:destroy_called, user_id})
      end

      :ok
    end
  end

  # ─── Setup ────────────────────────────────────────────────────────────────────

  setup do
    Application.put_env(:event_dispatcher, :session_supervisor_module, FakeSessionSupervisorForAll)
    Application.put_env(:event_dispatcher, :__all_sessions_test_pid__, self())

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :session_supervisor_module)
      Application.delete_env(:event_dispatcher, :__all_sessions_test_pid__)
      # Clean up all ETS entries added by this test (not just the two hard-coded IDs).
      :ets.delete_all_objects(:NebuSessions)
    end)

    :ok
  end

  defp build_stream, do: %{http_request_headers: %{}}

  defp fake_session_map(user_id) do
    %{
      access_token_hash: "hash_for_#{user_id}",
      device_id: "DEVICE_#{user_id}",
      created_at_ms: System.system_time(:millisecond),
      last_seen_at_ms: System.system_time(:millisecond)
    }
  end

  # ─── AC#6: 2 sessions in ETS → destroy_session called twice ─────────────────
  #
  # Given: ETS has sessions for @user_a:example.com and @user_b:example.com
  # When:  Server.invalidate_all_admin_sessions/2 is called
  # Then:  SessionSupervisor.destroy_session/1 called for both users
  # And:   Returns %Core.InvalidateAllAdminSessionsResponse{ok: true}

  test "invalidate_all_admin_sessions — 2 sessions in ETS → destroy_session called twice, returns ok: true" do
    # Given: seed two sessions in ETS.
    EtsStore.put_session("@user_a:example.com", fake_session_map("@user_a:example.com"))
    EtsStore.put_session("@user_b:example.com", fake_session_map("@user_b:example.com"))

    request = %Core.InvalidateAllAdminSessionsRequest{}

    response = Server.invalidate_all_admin_sessions(request, build_stream())

    # Then: handler returns ok: true.
    assert %Core.InvalidateAllAdminSessionsResponse{ok: true} = response,
           "AC#6: expected InvalidateAllAdminSessionsResponse{ok: true}, got #{inspect(response)}"

    # And: destroy_session/1 was called for both users.
    # Collect all {:destroy_called, user_id} messages.
    destroyed_users =
      Enum.reduce_while(1..2, [], fn _i, acc ->
        receive do
          {:destroy_called, uid} -> {:cont, [uid | acc]}
        after
          200 -> {:halt, acc}
        end
      end)

    assert length(destroyed_users) == 2,
           "AC#6: expected destroy_session/1 to be called exactly twice (once per session), " <>
             "got #{length(destroyed_users)} call(s): #{inspect(destroyed_users)}"

    assert "@user_a:example.com" in destroyed_users,
           "AC#6: expected destroy_session/1 called for '@user_a:example.com'; got: #{inspect(destroyed_users)}"

    assert "@user_b:example.com" in destroyed_users,
           "AC#6: expected destroy_session/1 called for '@user_b:example.com'; got: #{inspect(destroyed_users)}"
  end

  # ─── AC#6: returns ok: true (result is always best-effort) ───────────────────
  #
  # Verify that the response is the correct struct type with ok=true,
  # not a bare boolean or different struct.

  test "invalidate_all_admin_sessions — response struct has ok field set to true" do
    # Given: one session so the handler has something to iterate over.
    EtsStore.put_session("@user_a:example.com", fake_session_map("@user_a:example.com"))

    request = %Core.InvalidateAllAdminSessionsRequest{}
    response = Server.invalidate_all_admin_sessions(request, build_stream())

    assert is_struct(response, Core.InvalidateAllAdminSessionsResponse),
           "AC#6: response must be a %Core.InvalidateAllAdminSessionsResponse{} struct, got #{inspect(response)}"

    assert response.ok == true,
           "AC#6: response.ok must be true, got #{inspect(response.ok)}"
  end

  # ─── AC#7: empty ETS → returns ok: true (no-op) ──────────────────────────────
  #
  # Given: ETS table is empty (no active admin sessions)
  # When:  Server.invalidate_all_admin_sessions/2 is called
  # Then:  Returns %Core.InvalidateAllAdminSessionsResponse{ok: true} without error
  # And:   destroy_session/1 is never called (nothing to destroy)

  test "invalidate_all_admin_sessions — empty ETS → returns ok: true (no-op, no destroy calls)" do
    # Given: ensure ETS is clean for this test (no sessions for test users).
    # The setup on_exit cleans up; here we simply do NOT add any sessions.

    request = %Core.InvalidateAllAdminSessionsRequest{}

    response = Server.invalidate_all_admin_sessions(request, build_stream())

    # Then: returns ok: true even with empty ETS.
    assert %Core.InvalidateAllAdminSessionsResponse{ok: true} = response,
           "AC#7: expected InvalidateAllAdminSessionsResponse{ok: true} for empty ETS, got #{inspect(response)}"

    # And: destroy_session/1 must not be called at all.
    refute_receive {:destroy_called, _}, 50,
                   "AC#7: destroy_session/1 must NOT be called when ETS is empty"
  end

  # ─── AC#7: empty ETS — no crash, no error raised ─────────────────────────────
  #
  # The handler must be safe even on a completely empty ETS table.
  # It must not raise, pattern-match error, or return an error tuple.

  test "invalidate_all_admin_sessions — empty ETS does not raise or error" do
    request = %Core.InvalidateAllAdminSessionsRequest{}

    # Must not raise any exception.
    result =
      try do
        Server.invalidate_all_admin_sessions(request, build_stream())
      rescue
        e -> {:error, e}
      catch
        kind, reason -> {:caught, kind, reason}
      end

    refute match?({:error, _}, result),
           "AC#7: handler raised an exception on empty ETS: #{inspect(result)}"

    refute match?({:caught, _, _}, result),
           "AC#7: handler threw on empty ETS: #{inspect(result)}"

    assert match?(%Core.InvalidateAllAdminSessionsResponse{ok: true}, result),
           "AC#7: expected ok: true response, got #{inspect(result)}"
  end
end
