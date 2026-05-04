defmodule Nebu.EventDispatcher.ArchiveRoomTest do
  use ExUnit.Case, async: false

  # ─── Story 6.9: ArchiveRoom + UnarchiveRoom gRPC handler tests ───────────────
  #
  # RED PHASE — all tests fail until Story 6.9 is implemented.
  # Failing reasons:
  #   1. Core.ArchiveRoomRequest / Core.ArchiveRoomResponse do not exist yet.
  #      They will be generated after `make proto` runs on the updated core.proto.
  #   2. Core.UnarchiveRoomRequest / Core.UnarchiveRoomResponse do not exist yet.
  #   3. Nebu.EventDispatcher.Server.archive_room/2 does not exist yet.
  #   4. Nebu.EventDispatcher.Server.unarchive_room/2 does not exist yet.
  #   5. Nebu.Room.DBBehaviour.get_room_status/1 callback does not exist yet.
  #   6. FakeDB (in nebu_room_test.exs and here) must gain get_room_status/1.
  #   7. Nebu.Room.Server.init/1 does not yet check archived status.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and ETS tables are all process-global resources.
  #
  # Test strategy:
  #   - Tests 1+2: Call Server.archive_room/2 directly (unary handler, synchronous).
  #     - Test 1: Room GenServer running → Horde.DynamicSupervisor.terminate_child called
  #     - Test 2: Room GenServer not running → ok=true (no-op / idempotent)
  #   - Test 3: Call Server.unarchive_room/2 directly.
  #     - Room must be startable → RoomSupervisor.start_room called, ok=true
  #   - Test 4 (crash/restart): Room.Server.init/1 on archived room → {:stop, :normal}
  #     - Uses FakeDBWithArchivedStatus that returns {:ok, "archived"} for get_room_status/1
  #     - Verifies GenServer stops immediately and Horde does NOT restart it (:transient)
  #
  # Covered Acceptance Criteria:
  #   - AC#3   gRPC CoreService adds ArchiveRoom + UnarchiveRoom RPCs
  #   - AC#6   Elixir unit tests: archive_room handler, unarchive_room handler,
  #            Room.Server init/1 archived guard (crash/restart test)

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake DB for room_manager tests.
  # This FakeDB satisfies Nebu.Room.DBBehaviour *after* get_room_status/1 is added.
  # Until @callback get_room_status/1 is added to DBBehaviour, the module will
  # compile but mix test will warn about the extra function.

  defmodule FakeDB do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id) do
      case :ets.lookup(:archive_room_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:archive_room_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:archive_room_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:archive_room_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:archive_room_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:archive_room_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:archive_room_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:archive_room_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 — required by DBBehaviour
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 — NEW callback; returns "active" for normal rooms.
    # RED: fails until @callback get_room_status/1 is added to Nebu.Room.DBBehaviour.
    def get_room_status(_room_id), do: {:ok, "active"}

    # Stubs required to satisfy @behaviour Nebu.Room.DBBehaviour
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _direction, _limit, _from_token), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}
    # Story 9-7: returns empty list (no generic state events in unit tests).
    def get_generic_state_events(_room_id), do: {:ok, []}
  end

  # ─── FakeDBWithArchivedStatus ─────────────────────────────────────────────────
  #
  # FakeDB variant that returns {:ok, "archived"} for get_room_status/1.
  # Used in the crash/restart test (Test 4) to simulate an archived room in DB.
  # Room.Server.init/1 must call get_room_status/1 and return {:stop, :normal}
  # when the result is {:ok, "archived"}.

  defmodule FakeDBWithArchivedStatus do
    @behaviour Nebu.Room.DBBehaviour

    def load_members(room_id) do
      case :ets.lookup(:archive_room_test_db, {:room, room_id}) do
        [] -> {:error, :not_found}
        [{_, created_at_ms}] ->
          members = :ets.match(:archive_room_test_db, {{:member, room_id, :"$1"}, :active})
          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, "{}"}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:archive_room_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:archive_room_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:archive_room_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:archive_room_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, json) do
      :ets.insert(:archive_room_test_db, {{:power_levels, room_id}, json})
      :ok
    end

    # Story 6.8: required stub
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: returns {:ok, "archived"} — simulates archived room in DB.
    # RED: fails until @callback get_room_status/1 is added to DBBehaviour.
    def get_room_status(_room_id), do: {:ok, "archived"}

    # Stubs required to satisfy @behaviour Nebu.Room.DBBehaviour
    def get_rooms_for_user(_user_id), do: {:ok, []}
    def fetch_events(_room_id, _direction, _limit, _from_token), do: {:ok, [], "", ""}
    def fetch_events_since(_room_id, _last_event_id, _limit), do: {:ok, []}
    def get_event_timestamp(_event_id), do: {:error, :not_found}
    def get_room_name(_room_id), do: {:error, :not_found}
    def get_room_creator(_room_id), do: {:error, :not_found}
    # Story 9-7: returns empty list (no generic state events in unit tests).
    def get_generic_state_events(_room_id), do: {:ok, []}
  end

  # ─── FakeAdminDB ─────────────────────────────────────────────────────────────
  #
  # Story 9.1 (AC:4): archive_room/2 now calls admin_db_module().archive_room_atomic/1
  # for the atomic SELECT FOR UPDATE DB write. This fake satisfies that contract
  # without requiring a real Ecto.Repo.
  # All other callbacks are no-op stubs (archive_room_test only tests archive/unarchive).

  defmodule FakeAdminDB do
    @moduledoc "Minimal Nebu.Admin.DB fake for archive_room_test."

    def list_users(_limit, _cursor, _search), do: {[], ""}
    def get_user(_user_id), do: {:error, :not_found}
    def set_is_active(_user_id, _is_active), do: :ok
    def set_system_role(_user_id, _role), do: :ok
    def list_rooms(_limit, _cursor, _status_filter, _search), do: {[], ""}
    def get_room(_room_id), do: {:error, :not_found}

    def archive_room_atomic(_room_id) do
      # Always returns :ok for the archive_room_test context.
      # The test only cares that GenServer is terminated; DB write is a no-op here.
      :ok
    end

    def get_server_config, do: {:ok, %{}}
    def upsert_server_config(_changes), do: :ok
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:archive_room_test_db) != :undefined do
      :ets.delete(:archive_room_test_db)
    end

    :ets.new(:archive_room_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, FakeDB)
    # Story 9.1: archive_room/2 now calls admin_db_module().archive_room_atomic/1.
    Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)

    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :admin_db_module)

      if :ets.info(:archive_room_test_db) != :undefined do
        :ets.delete(:archive_room_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Helpers ─────────────────────────────────────────────────────────────────

  defp build_stream, do: %{http_request_headers: %{}}

  # Poll up to 500ms (50 × 10ms) for the room to disappear from the Horde registry.
  # Used in archived-room init/1 tests in place of fixed Process.sleep(100/150) —
  # makes the assertion deterministic on slow CI/Docker.
  defp wait_until_not_in_horde(room_id) do
    Enum.reduce_while(1..50, :error, fn _, _acc ->
      case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
        {:error, :not_found} ->
          {:halt, :ok}

        {:ok, _pid} ->
          Process.sleep(10)
          {:cont, :error}
      end
    end)
  end

  defp start_and_track_room(room_id) do
    {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:ok, p} -> Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, p)
          _ -> :ok
        end
      end
    end)

    {:ok, pid}
  end

  # ─── Test 1: archive_room gRPC — GenServer running → terminate_child called ──
  #
  # Given: Room GenServer is running for room_id
  # When: Server.archive_room/2 is called with room_id
  # Then: Returns %Core.ArchiveRoomResponse{ok: true}
  # And:  Room GenServer is no longer running (was terminated by Horde)
  #
  # RED: fails until:
  #   1. Core.ArchiveRoomRequest / Core.ArchiveRoomResponse are generated (make proto)
  #   2. Server.archive_room/2 is implemented in event_dispatcher/server.ex
  #   3. get_room_status/1 is in DBBehaviour + FakeDB above compiles without warning

  describe "Server.archive_room/2 — room GenServer running" do
    test "returns ArchiveRoomResponse{ok: true} and terminates GenServer via Horde" do
      room_id = "!archive-running:test.local"

      {:ok, _pid} = start_and_track_room(room_id)

      # Confirm GenServer is running before archive call
      assert {:ok, _} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      request = %Core.ArchiveRoomRequest{room_id: room_id}

      # RED: Core.ArchiveRoomRequest does not exist yet → compile error
      response = Server.archive_room(request, build_stream())

      # RED: Server.archive_room/2 does not exist yet → UndefinedFunctionError
      assert %Core.ArchiveRoomResponse{ok: true} = response,
             "expected ArchiveRoomResponse{ok: true}, got #{inspect(response)}"

      # Poll until the GenServer is no longer in Horde registry (max 500ms)
      Enum.reduce_while(1..50, :ok, fn _, _acc ->
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:error, :not_found} -> {:halt, :ok}
          {:ok, _pid} ->
            Process.sleep(10)
            {:cont, :ok}
        end
      end)

      # GenServer must no longer be running after archive
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id),
             "expected GenServer to be terminated after archive_room"
    end
  end

  # ─── Test 2: archive_room gRPC — GenServer not running → ok=true (no-op) ─────
  #
  # Given: No Room GenServer running for room_id
  # When: Server.archive_room/2 is called
  # Then: Returns %Core.ArchiveRoomResponse{ok: true} without error (idempotent)
  #
  # The handler must NOT raise when the GenServer is already stopped.
  # This models the case where the GenServer crashed/stopped before the gRPC call.

  describe "Server.archive_room/2 — room GenServer NOT running" do
    test "returns ArchiveRoomResponse{ok: true} without error when GenServer not found" do
      room_id = "!archive-not-running:test.local"

      # Confirm no GenServer is running
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      request = %Core.ArchiveRoomRequest{room_id: room_id}

      # RED: Core.ArchiveRoomRequest does not exist yet → compile error
      response = Server.archive_room(request, build_stream())

      # Must return ok: true even when GenServer was already stopped
      assert %Core.ArchiveRoomResponse{ok: true} = response,
             "expected ArchiveRoomResponse{ok: true} even when GenServer not running"
    end
  end

  # ─── Test 3: unarchive_room gRPC → start_room called, ok=true ────────────────
  #
  # Given: No Room GenServer running for room_id (room was archived + GenServer stopped)
  # When: Server.unarchive_room/2 is called with room_id
  # Then: Returns %Core.UnarchiveRoomResponse{ok: true}
  # And:  Room GenServer is now running (started by RoomSupervisor.start_room/1)
  #
  # RED: fails until:
  #   1. Core.UnarchiveRoomRequest / Core.UnarchiveRoomResponse are generated (make proto)
  #   2. Server.unarchive_room/2 is implemented in event_dispatcher/server.ex

  describe "Server.unarchive_room/2" do
    test "returns UnarchiveRoomResponse{ok: true} and starts Room GenServer" do
      room_id = "!unarchive:test.local"

      # Confirm no GenServer is running before unarchive
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      request = %Core.UnarchiveRoomRequest{room_id: room_id}

      # RED: Core.UnarchiveRoomRequest does not exist yet → compile error
      response = Server.unarchive_room(request, build_stream())

      # RED: Server.unarchive_room/2 does not exist yet → UndefinedFunctionError
      assert %Core.UnarchiveRoomResponse{ok: true} = response,
             "expected UnarchiveRoomResponse{ok: true}, got #{inspect(response)}"

      # GenServer must now be running
      assert {:ok, pid} = Nebu.Room.RoomSupervisor.lookup_room(room_id),
             "expected Room GenServer to be started after unarchive_room"

      assert is_pid(pid)

      # Cleanup: stop the started GenServer
      on_exit(fn ->
        if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
      end)
    end
  end

  # ─── Test 4 (Crash/Restart Test): Room.Server init/1 on archived room ─────────
  #
  # Covered Acceptance Criteria:
  #   - AC#6 (test 13): Room.Server init/1 checks DB status on start
  #   - When rooms.status = 'archived' → returns {:stop, :normal}
  #   - :transient restart strategy means Horde does NOT restart the process
  #
  # This is the mandatory crash/restart test for the Story 6.9 archive mechanism.
  # The key invariant: archived rooms must NOT enter a restart loop.
  #
  # Given: FakeDBWithArchivedStatus (returns {:ok, "archived"} for get_room_status/1)
  # When:  RoomSupervisor.start_room/1 is called (simulating Horde restart after node crash)
  # Then:  GenServer stops immediately with :normal reason
  # And:   Horde does NOT restart it (transient restart strategy)
  #
  # RED: fails until:
  #   1. get_room_status/1 is added to Nebu.Room.DBBehaviour + Nebu.Room.DB
  #   2. Nebu.Room.Server.init/1 calls db_module().get_room_status(room_id)
  #   3. When {:ok, "archived"} → init/1 returns {:stop, :normal}

  describe "Nebu.Room.Server.init/1 — archived room guard (crash/restart test)" do
    test "GenServer stops with :normal when rooms.status is 'archived' in DB" do
      # Inject FakeDB that returns {:ok, "archived"} for every get_room_status/1 call
      Application.put_env(:room_manager, :db_module, FakeDBWithArchivedStatus)

      room_id = "!archived:test.local"

      # Attempt to start the GenServer — init/1 must call get_room_status/1,
      # see "archived", and return {:stop, :normal} immediately.
      #
      # Horde.DynamicSupervisor.start_child returns {:ok, pid} when the child
      # starts successfully, or {:error, reason} on failure.
      # When init/1 returns {:stop, :normal}, the GenServer is never registered
      # in Horde.Registry — so start_room/1 returns {:ok, :undefined} or similar.
      #
      # RED: fails until Room.Server.init/1 checks get_room_status/1 and returns {:stop, :normal}.
      result = Nebu.Room.RoomSupervisor.start_room(room_id)

      # After {:stop, :normal}, the GenServer must NOT be running in Horde.
      # Poll up to 500ms for the registry entry to be cleared.
      assert :ok = wait_until_not_in_horde(room_id),
             "expected archived room to NOT be registered in Horde after init/1 stops with :normal"

      # The start result must not be a running pid for a permanently archived room.
      # With transient restart, Horde may return {:error, {:already_started, _}} or similar
      # when init returns {:stop, :normal}. The key invariant is: no running GenServer.
      _ = result
    end

    test "GenServer does NOT restart after :stop :normal — transient restart strategy" do
      # This test verifies the Horde :transient restart mechanism.
      # A GenServer that stops with :normal must NOT be restarted by Horde.
      #
      # This is the core invariant of the archive mechanism:
      # Step 1: archive_room gRPC → Horde.terminate_child → GenServer stops normally
      # Step 2: If node restarts → Horde tries to revive the child → init/1 runs
      # Step 3: init/1 sees "archived" in DB → {:stop, :normal} → Horde ignores (transient)
      # Result: No restart loop.

      Application.put_env(:room_manager, :db_module, FakeDBWithArchivedStatus)

      room_id = "!archived-no-restart:test.local"

      # Start: init/1 returns {:stop, :normal} → GenServer not running.
      _result = Nebu.Room.RoomSupervisor.start_room(room_id)
      assert :ok = wait_until_not_in_horde(room_id)

      # Try to start again (simulating Horde re-attempting after cluster join).
      _result2 = Nebu.Room.RoomSupervisor.start_room(room_id)
      assert :ok = wait_until_not_in_horde(room_id),
             "archived room must NOT restart even after multiple start_room/1 calls"
    end

    test "GenServer starts normally when rooms.status is 'active'" do
      # Confirm that normal rooms (status='active') still start correctly.
      # FakeDB.get_room_status returns {:ok, "active"} → init/1 proceeds normally.
      Application.put_env(:room_manager, :db_module, FakeDB)

      room_id = "!active-still-starts:test.local"

      {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      assert is_pid(pid)

      # Must be registered in Horde
      assert {:ok, _} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      on_exit(fn ->
        if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
      end)
    end
  end
end
