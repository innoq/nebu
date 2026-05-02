defmodule Nebu.EventDispatcher.ValidateTokenTest do
  use ExUnit.Case, async: false

  alias Nebu.EventDispatcher.Server

  defmodule SuccessValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(user_id, _system_role, _display_name, _email) do
      {:ok, %{
        user_id: user_id,
        system_role: "user",
        display_name: "kai.mueller",
        is_active: true
      }}
    end
  end

  defmodule DeactivatedValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :deactivated}
    end
  end

  defmodule ErrorValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :db_error}
    end
  end

  # ─── Spy ProfileDB (Story 8-10a ATDD) ────────────────────────────────────────
  # Records calls to upsert_profile/3 for assertion.
  defmodule SpyProfileDB do
    def upsert_profile(user_id, displayname, avatar_url) do
      :ets.insert(:validate_token_spy, {:upsert_profile_call, user_id, displayname, avatar_url})
      :ok
    end
  end

  defmodule NoOpProfileDB do
    def upsert_profile(_user_id, _displayname, _avatar_url), do: :ok
  end

  defmodule FailingProfileDB do
    def upsert_profile(_user_id, _displayname, _avatar_url), do: {:error, :db_error}
  end

  defp build_stream(headers) do
    %{http_request_headers: Map.new(headers)}
  end

  defp build_request(display_name \\ "kai.mueller", email \\ "kai@example.com") do
    %Core.ValidateTokenRequest{display_name: display_name, email: email}
  end

  setup do
    Application.put_env(:session_manager, :validator_module, SuccessValidator)
    # Default to no-op so existing tests don't hit Ecto. Tests that need
    # to assert on profile upsert override this with SpyProfileDB.
    Application.put_env(:event_dispatcher, :profile_db_module, NoOpProfileDB)

    # Create ETS spy table for profile upsert assertions.
    if :ets.info(:validate_token_spy) != :undefined, do: :ets.delete(:validate_token_spy)
    :ets.new(:validate_token_spy, [:named_table, :public, :set])

    on_exit(fn ->
      Application.delete_env(:session_manager, :validator_module)
      Application.delete_env(:event_dispatcher, :profile_db_module)
      if :ets.info(:validate_token_spy) != :undefined, do: :ets.delete(:validate_token_spy)
    end)

    :ok
  end

  describe "validate_token/2" do
    test "returns ValidateTokenResponse for active user" do
      stream = build_stream([{"x-user-id", "@kai:nebu.local"}, {"x-system-role", "user"}])
      request = build_request()

      assert %Core.ValidateTokenResponse{
               user_id: "@kai:nebu.local",
               system_role: "user",
               display_name: "kai.mueller",
               is_active: true
             } = Server.validate_token(request, stream)
    end

    test "raises PERMISSION_DENIED for deactivated user" do
      Application.put_env(:session_manager, :validator_module, DeactivatedValidator)
      stream = build_stream([{"x-user-id", "@alex:nebu.local"}, {"x-system-role", "user"}])
      request = build_request("alex", "alex@example.com")

      assert_raise GRPC.RPCError, "user account is deactivated", fn ->
        Server.validate_token(request, stream)
      end
    end

    test "raises UNAUTHENTICATED when x-user-id is missing" do
      stream = build_stream([])
      request = build_request()

      assert_raise GRPC.RPCError, "missing x-user-id metadata", fn ->
        Server.validate_token(request, stream)
      end
    end

    test "raises INTERNAL on DB error" do
      Application.put_env(:session_manager, :validator_module, ErrorValidator)
      stream = build_stream([{"x-user-id", "@bob:nebu.local"}, {"x-system-role", "user"}])
      request = build_request("bob", "bob@example.com")

      assert_raise GRPC.RPCError, "internal error", fn ->
        Server.validate_token(request, stream)
      end
    end

    # ─── Story 8-10a: profile upsert on successful login ─────────────────────────
    #
    # RED PHASE: These tests fail until validate_token/2 calls
    # profile_db_module().upsert_profile/3 after successful TokenValidator.validate.
    #
    # Root cause: GET /profile/{userId} returns 404 for bootstrap users because
    # the profiles table row is never created during OIDC login provisioning.

    test "calls profile_db_module().upsert_profile/3 with display_name on success" do
      Application.put_env(:event_dispatcher, :profile_db_module, SpyProfileDB)
      :ets.delete_all_objects(:validate_token_spy)

      stream = build_stream([{"x-user-id", "@kai:nebu.local"}, {"x-system-role", "user"}])
      request = build_request("kai.mueller", "kai@example.com")

      Server.validate_token(request, stream)

      calls = :ets.match(:validate_token_spy, {:upsert_profile_call, :"$1", :"$2", :"$3"})

      assert length(calls) == 1,
             "Expected exactly one upsert_profile call, got #{length(calls)}. " <>
             "Fix: add profile_db_module().upsert_profile(user_id, display_name, nil) " <>
             "inside the {:ok, user} branch of validate_token/2. (Story 8-10a RED)"

      [[user_id, displayname, _avatar_url]] = calls
      assert user_id == "@kai:nebu.local"
      assert displayname == "kai.mueller",
             "upsert_profile must receive the display_name from the request, got: #{displayname}"
    end

    test "profile upsert failure does NOT block login (non-fatal)" do
      Application.put_env(:event_dispatcher, :profile_db_module, FailingProfileDB)

      stream = build_stream([{"x-user-id", "@kai:nebu.local"}, {"x-system-role", "user"}])
      request = build_request("kai.mueller", "kai@example.com")

      # Login must succeed even if profile upsert fails
      assert %Core.ValidateTokenResponse{user_id: "@kai:nebu.local"} =
               Server.validate_token(request, stream)
    end

    test "profile upsert uses nil display_name when request.display_name is empty string" do
      Application.put_env(:event_dispatcher, :profile_db_module, SpyProfileDB)
      :ets.delete_all_objects(:validate_token_spy)

      stream = build_stream([{"x-user-id", "@kai:nebu.local"}, {"x-system-role", "user"}])
      # Empty display_name → should pass nil to upsert (COALESCE logic preserves existing)
      request = build_request("", "kai@example.com")

      Server.validate_token(request, stream)

      calls = :ets.match(:validate_token_spy, {:upsert_profile_call, :"$1", :"$2", :"$3"})
      assert length(calls) == 1
      [[_user_id, displayname, _]] = calls
      assert is_nil(displayname),
             "Empty display_name string must become nil for profile upsert (preserves existing row)"
    end
  end
end
