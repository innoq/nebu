defmodule Nebu.Session.SessionSupervisorTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global and
  # :NebuSessions is a named global ETS table.

  # ---------------------------------------------------------------------------
  # FakePgStore — ETS-backed stub injected via Application env.
  # Nebu.Session.SessionSupervisor delegates destroy_session to PgStore.
  # ---------------------------------------------------------------------------
  #
  # FAILS in RED phase because Nebu.Session.PgStore is not yet defined,
  # so `@behaviour Nebu.Session.PgStore` will raise UndefinedFunctionError.

  defmodule FakePgStore do
    @behaviour Nebu.Session.PgStore

    @impl Nebu.Session.PgStore
    def persist_since_token(user_id, since_token, last_event_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:supervisor_pg_test, {user_id, since_token, last_event_id, now_ms})
      :ok
    end

    @impl Nebu.Session.PgStore
    def get_since_token(user_id) do
      case :ets.lookup(:supervisor_pg_test, user_id) do
        [{^user_id, since_token, last_event_id, _ts}] ->
          {:ok, %{since_token: since_token, last_event_id: last_event_id}}

        [] ->
          {:error, :not_found}
      end
    end

    @impl Nebu.Session.PgStore
    def invalidate_session(user_id) do
      :ets.delete(:supervisor_pg_test, user_id)
      Nebu.Session.EtsStore.delete_session(user_id)
      :ok
    end
  end

  # ---------------------------------------------------------------------------
  # Setup / Teardown
  # ---------------------------------------------------------------------------

  setup do
    # Fake PG table for SessionSupervisor tests
    if :ets.whereis(:supervisor_pg_test) != :undefined, do: :ets.delete(:supervisor_pg_test)
    :ets.new(:supervisor_pg_test, [:named_table, :set, :public])

    # Ensure :NebuSessions ETS table exists
    if :ets.whereis(:NebuSessions) == :undefined do
      :ets.new(:NebuSessions, [:named_table, :set, :public])
    end

    :ets.delete_all_objects(:NebuSessions)

    Application.put_env(:session_manager, :pg_store_module, FakePgStore)

    on_exit(fn ->
      Application.delete_env(:session_manager, :pg_store_module)

      if :ets.whereis(:supervisor_pg_test) != :undefined,
        do: :ets.delete(:supervisor_pg_test)

      if :ets.whereis(:NebuSessions) != :undefined do
        :ets.delete_all_objects(:NebuSessions)
      end
    end)

    :ok
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #6 — SessionSupervisor.create_session writes to ETS
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.SessionSupervisor.create_session/2" do
    test "stores session in ETS so that EtsStore.get_session returns it" do
      # FAILS in RED phase: Nebu.Session.SessionSupervisor is undefined
      user_id = "@kai:nebu.local"

      session = %{
        access_token_hash: "h",
        device_id: "D1",
        created_at_ms: 1_000,
        last_seen_at_ms: 1_000
      }

      assert :ok = Nebu.Session.SessionSupervisor.create_session(user_id, session)

      assert {:ok, _session_map} = Nebu.Session.EtsStore.get_session(user_id)
    end

    test "create_session stores the exact session_map passed in" do
      # FAILS in RED phase: Nebu.Session.SessionSupervisor is undefined
      user_id = "@alex:nebu.local"

      session = %{
        access_token_hash: "sha256hexhash",
        device_id: "DEVICE_ALEX",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.SessionSupervisor.create_session(user_id, session)

      assert {:ok, ^session} = Nebu.Session.EtsStore.get_session(user_id)
    end
  end

  # ---------------------------------------------------------------------------
  # Acceptance Test #7 — SessionSupervisor.destroy_session removes from ETS
  # ---------------------------------------------------------------------------

  describe "Nebu.Session.SessionSupervisor.destroy_session/1" do
    test "removes session from ETS; subsequent get_session returns {:error, :not_found}" do
      # FAILS in RED phase: Nebu.Session.SessionSupervisor is undefined
      user_id = "@kai:nebu.local"

      session = %{
        access_token_hash: "todelete",
        device_id: "DEVICE_D",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session(user_id, session)

      # Pre-condition: session is present
      assert {:ok, _} = Nebu.Session.EtsStore.get_session(user_id)

      # Destroy via SessionSupervisor (delegates to FakePgStore)
      assert :ok = Nebu.Session.SessionSupervisor.destroy_session(user_id)

      # Post-condition: session is gone from ETS
      assert {:error, :not_found} = Nebu.Session.EtsStore.get_session(user_id)
    end

    test "destroy_session also removes from PG store (via PgStore.invalidate_session)" do
      # FAILS in RED phase: Nebu.Session.SessionSupervisor is undefined
      user_id = "@kai:nebu.local"

      session = %{
        access_token_hash: "hash_pg",
        device_id: "DEVICE_PG",
        created_at_ms: 1_712_000_000_000,
        last_seen_at_ms: 1_712_000_000_000
      }

      :ok = Nebu.Session.EtsStore.put_session(user_id, session)

      # Seed the fake PG sync_token row
      :ets.insert(
        :supervisor_pg_test,
        {user_id, "some_token", "$evt", System.system_time(:millisecond)}
      )

      assert :ok = Nebu.Session.SessionSupervisor.destroy_session(user_id)

      # PG sync_token row is gone
      assert [] = :ets.lookup(:supervisor_pg_test, user_id)

      # ETS session is gone
      assert {:error, :not_found} = Nebu.Session.EtsStore.get_session(user_id)
    end
  end
end
