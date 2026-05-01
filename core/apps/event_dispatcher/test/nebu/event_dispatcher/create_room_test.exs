defmodule Nebu.EventDispatcher.CreateRoomTest do
  use ExUnit.Case, async: false

  # ─── Story 4-9: Elixir create_room/2 gRPC handler ────────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-9 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS :NebuTxnDedup table are all process-global resources.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.create_room/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map with http_request_headers (matches real gRPC
  #     stream contract used by the implementation, see validate_token_test.exs).
  #   - DB injection: Room.Server.init/1 uses Application.get_env(:room_manager, :db_module).
  #     We inject the FakeDB from the room_manager test to avoid PostgreSQL.
  #   - server_name injection: generate_room_id/0 reads
  #     Application.get_env(:event_dispatcher, :server_name, "nebu.local").
  #     We override with "test.local" for deterministic assertions.
  #   - Horde and :pg are started by Nebu.Room.Application on app boot.
  #     Both are available when running `mix test` inside the umbrella.
  #   - After each test, all room GenServers created during the test are
  #     terminated via Horde.DynamicSupervisor to prevent cross-test pollution.

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake that satisfies the Nebu.Room.DB behaviour. Injects via
  # Application.put_env(:room_manager, :db_module, FakeDB). No PostgreSQL
  # connection is required.

  # No-op audit writer — Story 5-2 wired Server.create_room/2 to emit
  # an audit log entry; this test does not verify that emission (covered
  # in audit_room_ops_test.exs) and must not depend on Nebu.Repo.
  defmodule NoOpAuditWriter do
    def log(_, _, _, _, _, _, _ \\ nil), do: :ok
  end

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:create_room_test_db, {:room, room_id}) do
        [] -> {:error, :not_found}
        [{_, created_at_ms}] ->
          members =
            :ets.match(:create_room_test_db, {{:member, room_id, :"$1"}, :active})
          pl_json =
            case :ets.lookup(:create_room_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end
          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:create_room_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:create_room_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:create_room_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:create_room_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:create_room_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create the ETS table for FakeDB (public so both the test process and Room
    # GenServer processes can access it). If the table already exists from a
    # previous test run (e.g. on mix test --watch), delete it first.
    if :ets.info(:create_room_test_db) != :undefined do
      :ets.delete(:create_room_test_db)
    end
    :ets.new(:create_room_test_db, [:named_table, :public, :set])

    # Inject fake DB so Room GenServers don't need PostgreSQL.
    Application.put_env(:room_manager, :db_module, FakeDB)

    # Story 5.29d AC1 (FB-E5-03): also inject FakeDB as the messages_db_module so
    # Server.create_room/2's insert_event call does not hit Nebu.Repo.
    Application.put_env(:event_dispatcher, :messages_db_module, FakeDB)

    # Override server_name for deterministic room_id assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Story 5-2: Server.create_room/2 now calls Compliance.AuditWriter.log/6
    # after the room is created. Inject NoOpAuditWriter here so this test
    # does not depend on Nebu.Repo being started in the :test env.
    Application.put_env(:compliance, :audit_writer, NoOpAuditWriter)

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # :NebuTxnDedup is a named table created at Application boot (Nebu.Room.Application).
    # It CANNOT be deleted and recreated between tests. Clear all entries before each
    # test to prevent idempotency state from leaking across test cases.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      # Remove Application overrides.
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :server_name)
      Application.delete_env(:compliance, :audit_writer)

      # Drop the ETS table created for this test (guard against double-delete).
      if :ets.info(:create_room_test_db) != :undefined do
        :ets.delete(:create_room_test_db)
      end

      # Clear :NebuTxnDedup entries left by this test.
      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ─────────────────────────────────────────────────────────

  defp build_stream do
    %{http_request_headers: %{}}
  end

  # ─── Room GenServer tracking ──────────────────────────────────────────────────
  #
  # Registers an on_exit callback to stop the Room GenServer created for room_id.
  # Prevents Horde-managed GenServers from accumulating across tests.

  defp start_and_track_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, pid} ->
        on_exit(fn ->
          if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
        end)
        {:ok, pid}
      error -> error
    end
  end

  # ─── AC #5 (Story 4-9 AT): create_room returns CreateRoomResponse ────────────
  #
  # Given: no Room GenServer running for the new room
  # When: create_room/2 is called with a valid CreateRoomRequest
  # Then: returns %Core.CreateRoomResponse{room_id: room_id} where room_id
  #       matches "!<hex>:test.local" and the GenServer is running in Horde.

  describe "Server.create_room/2 — happy path" do
    test "returns CreateRoomResponse with a non-empty room_id" do
      request = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "My Room",
        topic: "A test room"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      assert %Core.CreateRoomResponse{} = response
      assert is_binary(response.room_id)
      assert response.room_id != ""
    end

    test "room_id starts with '!' and contains the configured server_name" do
      request = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "Room Format Test"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      assert String.starts_with?(response.room_id, "!")
      assert String.contains?(response.room_id, ":test.local")
    end

    test "room_id matches pattern !<hex>:test.local" do
      request = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "Regex Room"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      # Format: !{16 lowercase hex chars}:test.local
      # (8 bytes from :crypto.strong_rand_bytes/1 → 16 hex chars)
      assert response.room_id =~ ~r/^![a-f0-9]+:test\.local$/
    end

    test "each create_room call generates a unique room_id" do
      request = %Core.CreateRoomRequest{creator_id: "@alice:test.local", name: "R1"}

      r1 = Server.create_room(request, build_stream())
      start_and_track_room(r1.room_id)

      r2 = Server.create_room(%{request | name: "R2"}, build_stream())
      start_and_track_room(r2.room_id)

      refute r1.room_id == r2.room_id,
             "two consecutive create_room calls must produce distinct room_ids"
    end
  end

  # ─── AC #5 (Story 4-9 AT): Room GenServer is running in Horde after call ─────
  #
  # Given: create_room/2 succeeds
  # When: Nebu.Room.RoomSupervisor.lookup_room/1 is called with the returned room_id
  # Then: returns {:ok, pid} — the GenServer is registered in Horde.Registry

  describe "Server.create_room/2 — Horde registration" do
    test "Room GenServer is running in Horde after create_room" do
      request = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "Horde Registration Test"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      assert {:ok, pid} = Nebu.Room.RoomSupervisor.lookup_room(response.room_id)
      assert is_pid(pid)
      assert Process.alive?(pid)
    end
  end

  # ─── AC #6 (Story 4-9 AT): creator is auto-joined as member ─────────────────
  #
  # Given: create_room/2 succeeds for @alice:test.local
  # When: Nebu.Room.Server.get_state/1 is called with the returned room_id
  # Then: state.members contains "@alice:test.local"

  describe "Server.create_room/2 — creator auto-join" do
    test "creator is in the room's member list after create_room" do
      creator_id = "@alice:test.local"

      request = %Core.CreateRoomRequest{
        creator_id: creator_id,
        name: "Creator Join Test"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)
      state = Nebu.Room.Server.get_state(response.room_id)

      assert MapSet.member?(state.members, creator_id),
             "expected #{creator_id} to be in members after create_room, got: #{inspect(state.members)}"
    end

    test "only the creator is a member immediately after creation" do
      request = %Core.CreateRoomRequest{
        creator_id: "@bob:test.local",
        name: "Solo Member Test"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)
      state = Nebu.Room.Server.get_state(response.room_id)

      assert MapSet.size(state.members) == 1,
             "expected exactly 1 member after creation, got: #{inspect(state.members)}"

      assert MapSet.member?(state.members, "@bob:test.local")
    end
  end

  # ─── server_name config injection ────────────────────────────────────────────
  #
  # Verifies that generate_room_id/0 reads from Application.get_env/3 and does
  # NOT hard-code "nebu.local". This test changes the override mid-test.

  describe "Server.create_room/2 — server_name config" do
    test "room_id uses the server_name from Application config" do
      Application.put_env(:event_dispatcher, :server_name, "custom.example.org")

      on_exit(fn ->
        # Restore the standard test.local override set in setup so the global
        # on_exit cleanup does not inadvertently re-delete an already-absent key.
        Application.put_env(:event_dispatcher, :server_name, "test.local")
      end)

      request = %Core.CreateRoomRequest{
        creator_id: "@alice:custom.example.org",
        name: "Config Test Room"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      assert String.ends_with?(response.room_id, ":custom.example.org"),
             "expected room_id to end with :custom.example.org, got #{response.room_id}"
    end
  end

  # ─── MAJOR-4 (AT#7 Story 4-13): creator gets power level 100 at room creation ─
  #
  # AC #2 (Story 4-13) — After create_room/2, the creator must have power level 100
  # in state.power_levels["users"]. Default Matrix power levels must also be present.
  #
  # Given: create_room/2 called with creator_id: "@bob:test.local"
  # When: Nebu.Room.Server.get_state(room_id) called on the newly created room
  # Then: state.power_levels["users"]["@bob:test.local"] == 100 and all default
  #       power level keys are present in state.power_levels

  describe "Server.create_room/2 — creator power level 100" do
    test "creator has power level 100 in state.power_levels after create_room" do
      creator_id = "@bob:test.local"

      request = %Core.CreateRoomRequest{
        creator_id: creator_id,
        name: "Power Level Test Room"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      state = Nebu.Room.Server.get_state(response.room_id)

      assert is_map(state.power_levels),
             "expected power_levels to be a map, got: #{inspect(state.power_levels)}"

      creator_level = get_in(state.power_levels, ["users", creator_id])

      assert creator_level == 100,
             "expected creator #{creator_id} to have power level 100, got: #{inspect(creator_level)}"
    end

    test "all default power level keys are present after create_room" do
      request = %Core.CreateRoomRequest{
        creator_id: "@alice:test.local",
        name: "Default Keys Test Room"
      }

      response = Server.create_room(request, build_stream())
      start_and_track_room(response.room_id)

      state = Nebu.Room.Server.get_state(response.room_id)
      pl = state.power_levels

      required_keys = ["ban", "kick", "invite", "redact", "state_default", "events_default", "users_default", "users", "events"]

      for key <- required_keys do
        assert Map.has_key?(pl, key),
               "expected power_levels to contain key #{key}, got: #{inspect(pl)}"
      end
    end
  end
end
