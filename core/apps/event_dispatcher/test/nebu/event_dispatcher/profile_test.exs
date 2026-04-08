defmodule Nebu.EventDispatcher.ProfileTest do
  use ExUnit.Case, async: false

  # ─── Story 4-18: Elixir update_profile/2 and get_profile/2 gRPC handlers ─────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-18 is implemented.
  #
  # async: false — Application.put_env(:event_dispatcher, :profile_db_module, ...)
  # is a process-global resource shared with other event_dispatcher tests.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.update_profile/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map with http_request_headers that includes
  #     x-user-id and x-system-role metadata (matches Nebu.Grpc.Metadata.trusted_identity/1
  #     contract used by update_profile/2 — which reads x-user-id for ownership check).
  #   - DB injection (profiles): update_profile/2 uses
  #     Application.get_env(:event_dispatcher, :profile_db_module, Nebu.Profile.DB).
  #     Inject FakeProfileDB, which captures upsert_profile/3 call args in an
  #     ETS table for assertion.
  #   - COALESCE test: send displayname=nil (empty string in proto) to verify that
  #     the handler converts "" → nil before calling upsert_profile, so the DB
  #     COALESCE(EXCLUDED.displayname, profiles.displayname) preserves existing value.
  #   - No room setup needed: profile operations do not involve Room GenServers.

  alias Nebu.EventDispatcher.Server

  # ─── FakeProfileDB ────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Profile.DB behaviour (upsert_profile/3).
  # Injected via Application.put_env(:event_dispatcher, :profile_db_module, FakeProfileDB).
  # Captures calls in :profile_test_db ETS for assertion.

  defmodule FakeProfileDB do
    @doc "Captures the upsert_profile/3 call; returns :ok."
    def upsert_profile(user_id, displayname, avatar_url) do
      :ets.insert(:profile_test_db, {:last_upsert, user_id, displayname, avatar_url})
      :ok
    end
  end

  # ─── Fake stream ─────────────────────────────────────────────────────────────

  defp build_stream(user_id, system_role \\ "user") do
    %{
      http_request_headers: %{
        "x-user-id" => user_id,
        "x-system-role" => system_role
      }
    }
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create ETS table for FakeProfileDB.
    # Guard against stale tables from --watch reruns.
    if :ets.info(:profile_test_db) != :undefined do
      :ets.delete(:profile_test_db)
    end

    :ets.new(:profile_test_db, [:named_table, :public, :set])

    # Inject fake profile DB module.
    Application.put_env(:event_dispatcher, :profile_db_module, FakeProfileDB)

    # Override server_name for deterministic assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :profile_db_module)
      Application.delete_env(:event_dispatcher, :server_name)

      if :ets.info(:profile_test_db) != :undefined do
        :ets.delete(:profile_test_db)
      end
    end)

    :ok
  end

  # ─── Test 1: update_profile — displayname update → upsert called ─────────────
  #
  # Given: FakeProfileDB injected; user "@alice:test.local"; displayname = "Alice Nebu"
  # When: Server.update_profile/2 called with UpdateProfileRequest
  # Then: FakeProfileDB.upsert_profile/3 was called with
  #       ("@alice:test.local", "Alice Nebu", nil);
  #       returns %Core.UpdateProfileResponse{}.
  #
  # Note: avatar_url="" (empty string in proto) → handler converts to nil before
  # calling upsert_profile (so COALESCE preserves existing avatar_url in DB).

  describe "Server.update_profile/2 — displayname update" do
    test "upserts profile with displayname, nil avatar_url, returns UpdateProfileResponse" do
      alice = "@alice:test.local"

      request = %Core.UpdateProfileRequest{
        user_id: alice,
        displayname: "Alice Nebu",
        avatar_url: ""
      }

      response = Server.update_profile(request, build_stream(alice))

      assert %Core.UpdateProfileResponse{} = response,
             "expected %Core.UpdateProfileResponse{}, got: #{inspect(response)}"

      # Assert FakeProfileDB.upsert_profile/3 was called with correct args.
      case :ets.lookup(:profile_test_db, :last_upsert) do
        [{:last_upsert, ^alice, "Alice Nebu", nil}] ->
          :ok

        [{:last_upsert, uid, dn, av}] ->
          flunk(
            "upsert_profile called with unexpected args: user_id=#{uid}, displayname=#{inspect(dn)}, avatar_url=#{inspect(av)}"
          )

        [] ->
          flunk("upsert_profile was not called (ETS entry missing)")
      end
    end
  end

  # ─── Test 2: update_profile — avatar_url only → displayname nil (COALESCE) ───
  #
  # Given: FakeProfileDB injected; user "@alice:test.local";
  #        displayname="" (empty string, meaning "do not update");
  #        avatar_url = "mxc://test.local/abc"
  # When: Server.update_profile/2 called with UpdateProfileRequest
  # Then: FakeProfileDB.upsert_profile/3 was called with
  #       ("@alice:test.local", nil, "mxc://test.local/abc");
  #       nil for displayname ensures COALESCE preserves existing value in DB.
  #       Returns %Core.UpdateProfileResponse{}.

  describe "Server.update_profile/2 — avatar_url only (displayname nil → COALESCE)" do
    test "converts empty displayname to nil so COALESCE preserves existing DB value" do
      alice = "@alice:test.local"

      request = %Core.UpdateProfileRequest{
        user_id: alice,
        displayname: "",
        avatar_url: "mxc://test.local/abc"
      }

      response = Server.update_profile(request, build_stream(alice))

      assert %Core.UpdateProfileResponse{} = response,
             "expected %Core.UpdateProfileResponse{}, got: #{inspect(response)}"

      # displayname must be nil (not ""), avatar_url must be preserved.
      case :ets.lookup(:profile_test_db, :last_upsert) do
        [{:last_upsert, ^alice, nil, "mxc://test.local/abc"}] ->
          :ok

        [{:last_upsert, uid, dn, av}] ->
          flunk(
            "upsert_profile called with unexpected args: user_id=#{uid}, displayname=#{inspect(dn)}, avatar_url=#{inspect(av)}" <>
              " — expected displayname=nil so COALESCE preserves existing DB value"
          )

        [] ->
          flunk("upsert_profile was not called (ETS entry missing)")
      end
    end
  end

  # ─── Test 3: update_profile — DB returns error → raises GRPC.RPCError ────────
  #
  # Given: FakeProfileDB overridden to return {:error, :db_failure};
  #        user "@alice:test.local"; displayname = "Alice"
  # When: Server.update_profile/2 called
  # Then: raises %GRPC.RPCError{status: GRPC.Status.internal()}
  #       (handler must not swallow DB errors)

  describe "Server.update_profile/2 — DB failure" do
    test "raises GRPC.RPCError internal when profile_db_module returns error" do
      # Override with a DB module that always fails.
      defmodule FailingProfileDB do
        def upsert_profile(_user_id, _displayname, _avatar_url) do
          {:error, :db_failure}
        end
      end

      Application.put_env(:event_dispatcher, :profile_db_module, FailingProfileDB)

      alice = "@alice:test.local"

      request = %Core.UpdateProfileRequest{
        user_id: alice,
        displayname: "Alice",
        avatar_url: ""
      }

      error =
        try do
          Server.update_profile(request, build_stream(alice))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.internal(),
             "expected status internal (#{GRPC.Status.internal()}), got: #{error.status}"
    end
  end
end
