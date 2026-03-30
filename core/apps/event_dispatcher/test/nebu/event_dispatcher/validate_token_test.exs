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

  defp build_stream(headers) do
    %{adapter: %{payload: %{headers: headers}}}
  end

  defp build_request(display_name \\ "kai.mueller", email \\ "kai@example.com") do
    %Core.ValidateTokenRequest{display_name: display_name, email: email}
  end

  setup do
    Application.put_env(:session_manager, :validator_module, SuccessValidator)

    on_exit(fn ->
      Application.delete_env(:session_manager, :validator_module)
    end)

    :ok
  end

  describe "validate_token/2" do
    test "returns ValidateTokenResponse for active user" do
      stream = build_stream([{"x-user-id", "@kai:nebu.local"}, {"x-system-role", "user"}])
      request = build_request()

      assert {:ok,
              %Core.ValidateTokenResponse{
                user_id: "@kai:nebu.local",
                system_role: "user",
                display_name: "kai.mueller",
                is_active: true
              }} = Server.validate_token(request, stream)
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
  end
end
