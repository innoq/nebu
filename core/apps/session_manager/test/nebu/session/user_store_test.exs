defmodule Nebu.Session.UserStoreTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.UserStore

  # ETS-backed fake DB — no Postgrex connection needed for unit tests
  defmodule FakeUserDB do
    @behaviour Nebu.Session.UserStore

    @impl Nebu.Session.UserStore
    def upsert_user(user_id, _system_role) do
      now_ms = System.system_time(:millisecond)

      case :ets.lookup(:user_store_test, user_id) do
        [] ->
          :ets.insert(:user_store_test, {user_id, now_ms})
          {:ok, user_id}

        [{^user_id, _created_at}] ->
          # Simulate ON CONFLICT DO UPDATE SET last_seen_at
          :ets.update_element(:user_store_test, user_id, {2, now_ms})
          {:ok, user_id}
      end
    end
  end

  setup do
    # Create fresh ETS table for each test
    if :ets.whereis(:user_store_test) != :undefined do
      :ets.delete(:user_store_test)
    end

    :ets.new(:user_store_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :db_module, FakeUserDB)

    on_exit(fn ->
      Application.delete_env(:session_manager, :db_module)

      if :ets.whereis(:user_store_test) != :undefined do
        :ets.delete(:user_store_test)
      end
    end)

    :ok
  end

  describe "upsert_user/2" do
    test "inserts new user record on first login" do
      assert {:ok, "@kai:nebu.local"} = UserStore.upsert_user("@kai:nebu.local", "user")
      assert [{"@kai:nebu.local", _}] = :ets.lookup(:user_store_test, "@kai:nebu.local")
    end

    test "idempotent: two calls with same user_id result in exactly one record" do
      assert {:ok, "@kai:nebu.local"} = UserStore.upsert_user("@kai:nebu.local", "user")
      assert {:ok, "@kai:nebu.local"} = UserStore.upsert_user("@kai:nebu.local", "user")

      # ON CONFLICT: exactly one row, not two
      assert 1 == length(:ets.tab2list(:user_store_test))
    end

    test "second call updates last_seen_at (upsert, no duplicate)" do
      assert {:ok, "@alex:nebu.local"} = UserStore.upsert_user("@alex:nebu.local", "instance_admin")
      [{_, ts1}] = :ets.lookup(:user_store_test, "@alex:nebu.local")

      Process.sleep(1)

      assert {:ok, "@alex:nebu.local"} = UserStore.upsert_user("@alex:nebu.local", "instance_admin")
      [{_, ts2}] = :ets.lookup(:user_store_test, "@alex:nebu.local")

      assert ts2 >= ts1
    end
  end
end
