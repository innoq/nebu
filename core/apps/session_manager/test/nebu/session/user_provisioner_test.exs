defmodule Nebu.Session.UserProvisionerTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.UserProvisioner

  defmodule FakeProvisioner do
    @behaviour Nebu.Session.UserProvisioner

    @impl Nebu.Session.UserProvisioner
    def provision_user(user_id, display_name, email, server_key) do
      :ets.insert(:provisioner_test, {user_id, display_name, email, byte_size(server_key)})
      {:ok, :provisioned}
    end
  end

  defmodule FailingProvisioner do
    @behaviour Nebu.Session.UserProvisioner

    @impl Nebu.Session.UserProvisioner
    def provision_user(_user_id, _display_name, _email, _server_key) do
      {:error, :db_error}
    end
  end

  setup do
    if :ets.whereis(:provisioner_test) != :undefined do
      :ets.delete(:provisioner_test)
    end

    :ets.new(:provisioner_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :provisioner_module, FakeProvisioner)

    on_exit(fn ->
      Application.delete_env(:session_manager, :provisioner_module)

      if :ets.whereis(:provisioner_test) != :undefined do
        :ets.delete(:provisioner_test)
      end
    end)

    :ok
  end

  describe "provision_user/4" do
    test "returns {:ok, :provisioned} on success" do
      server_key = :crypto.strong_rand_bytes(32)

      assert {:ok, :provisioned} =
               UserProvisioner.provision_user("@kai:nebu.local", "kai.mueller", "kai@example.com", server_key)
    end

    test "records user_id, display_name, email and server_key size" do
      server_key = :crypto.strong_rand_bytes(32)
      UserProvisioner.provision_user("@kai:nebu.local", "kai.mueller", "kai@example.com", server_key)

      assert [{"@kai:nebu.local", "kai.mueller", "kai@example.com", 32}] =
               :ets.lookup(:provisioner_test, "@kai:nebu.local")
    end

    test "propagates {:error, reason} from DB module" do
      Application.put_env(:session_manager, :provisioner_module, FailingProvisioner)
      server_key = :crypto.strong_rand_bytes(32)

      assert {:error, :db_error} =
               UserProvisioner.provision_user("@alex:nebu.local", "alex", "alex@example.com", server_key)
    end
  end
end
