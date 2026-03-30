defmodule Nebu.Session.TokenValidatorTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.TokenValidator

  defmodule FakeValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(user_id, system_role, display_name, _email) do
      :ets.insert(:validator_test, {user_id, system_role, display_name})

      {:ok, %{
        user_id: user_id,
        system_role: system_role,
        display_name: display_name,
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

  defmodule FailingValidator do
    @behaviour Nebu.Session.TokenValidator

    @impl Nebu.Session.TokenValidator
    def validate(_user_id, _system_role, _display_name, _email) do
      {:error, :db_error}
    end
  end

  setup do
    if :ets.whereis(:validator_test) != :undefined do
      :ets.delete(:validator_test)
    end

    :ets.new(:validator_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :validator_module, FakeValidator)

    on_exit(fn ->
      Application.delete_env(:session_manager, :validator_module)

      if :ets.whereis(:validator_test) != :undefined do
        :ets.delete(:validator_test)
      end
    end)

    :ok
  end

  describe "validate/4" do
    test "returns {:ok, user_data} for active user" do
      assert {:ok,
              %{
                user_id: "@kai:nebu.local",
                system_role: "user",
                display_name: "kai.mueller",
                is_active: true
              }} =
               TokenValidator.validate("@kai:nebu.local", "user", "kai.mueller", "kai@example.com")
    end

    test "records user_id, system_role, and display_name" do
      TokenValidator.validate("@kai:nebu.local", "user", "kai.mueller", "kai@example.com")

      assert [{"@kai:nebu.local", "user", "kai.mueller"}] =
               :ets.lookup(:validator_test, "@kai:nebu.local")
    end

    test "returns {:error, :deactivated} for deactivated user" do
      Application.put_env(:session_manager, :validator_module, DeactivatedValidator)

      assert {:error, :deactivated} =
               TokenValidator.validate("@alex:nebu.local", "user", "alex", "alex@example.com")
    end

    test "propagates {:error, reason} from DB module" do
      Application.put_env(:session_manager, :validator_module, FailingValidator)

      assert {:error, :db_error} =
               TokenValidator.validate("@bob:nebu.local", "user", "bob", "bob@example.com")
    end
  end
end
