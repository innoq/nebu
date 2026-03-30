defmodule Nebu.Session.BootstrapCheckerTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.BootstrapChecker

  # ETS-backed fake — simulates bootstrap logic without PostgreSQL
  defmodule FakeBootstrapChecker do
    @behaviour Nebu.Session.BootstrapChecker

    @impl Nebu.Session.BootstrapChecker
    def upsert_with_bootstrap(user_id, system_role) do
      bootstrap_active = :ets.lookup(:bootstrap_test, :bootstrap_active)

      resolved_role =
        case bootstrap_active do
          [{:bootstrap_active, true}] ->
            :ets.insert(:bootstrap_test, {:bootstrap_active, false})
            "instance_admin"

          _ ->
            system_role
        end

      :ets.insert(:bootstrap_test, {user_id, resolved_role})
      {:ok, {user_id, resolved_role}}
    end
  end

  defmodule FailingBootstrapChecker do
    @behaviour Nebu.Session.BootstrapChecker

    @impl Nebu.Session.BootstrapChecker
    def upsert_with_bootstrap(_user_id, _system_role) do
      {:error, :db_error}
    end
  end

  setup do
    if :ets.whereis(:bootstrap_test) != :undefined do
      :ets.delete(:bootstrap_test)
    end

    :ets.new(:bootstrap_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :bootstrap_checker_module, FakeBootstrapChecker)

    on_exit(fn ->
      Application.delete_env(:session_manager, :bootstrap_checker_module)

      if :ets.whereis(:bootstrap_test) != :undefined do
        :ets.delete(:bootstrap_test)
      end
    end)

    :ok
  end

  describe "upsert_with_bootstrap/2" do
    test "bootstrap active — first user gets instance_admin regardless of OIDC role" do
      :ets.insert(:bootstrap_test, {:bootstrap_active, true})

      assert {:ok, {"@kai:nebu.local", "instance_admin"}} =
               BootstrapChecker.upsert_with_bootstrap("@kai:nebu.local", "user")
    end

    test "bootstrap not active — OIDC role 'user' is preserved" do
      :ets.insert(:bootstrap_test, {:bootstrap_active, false})

      assert {:ok, {"@kai:nebu.local", "user"}} =
               BootstrapChecker.upsert_with_bootstrap("@kai:nebu.local", "user")
    end

    test "bootstrap not active — OIDC role 'instance_admin' is preserved" do
      :ets.insert(:bootstrap_test, {:bootstrap_active, false})

      assert {:ok, {"@alex:nebu.local", "instance_admin"}} =
               BootstrapChecker.upsert_with_bootstrap("@alex:nebu.local", "instance_admin")
    end

    test "bootstrap triggers only once — second user gets OIDC role" do
      :ets.insert(:bootstrap_test, {:bootstrap_active, true})

      assert {:ok, {"@first:nebu.local", "instance_admin"}} =
               BootstrapChecker.upsert_with_bootstrap("@first:nebu.local", "user")

      # Second user — bootstrap already consumed
      assert {:ok, {"@second:nebu.local", "user"}} =
               BootstrapChecker.upsert_with_bootstrap("@second:nebu.local", "user")
    end

    test "delegation respects configured module" do
      Application.put_env(:session_manager, :bootstrap_checker_module, FailingBootstrapChecker)

      assert {:error, :db_error} =
               BootstrapChecker.upsert_with_bootstrap("@bob:nebu.local", "user")
    end
  end
end
