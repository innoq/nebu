defmodule Nebu.EventDispatcher.UpdateRoomSettingsTest do
  use ExUnit.Case, async: false

  # ─── Story 6.8 — Elixir update_room_settings/2 gRPC handler ──────────────────
  #
  # MAJOR-2 fix: the gRPC handler in server.ex had no tests.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS :NebuTxnDedup table are all process-global resources.
  #
  # Test strategy:
  #   - Call Nebu.EventDispatcher.Server.update_room_settings/2 directly
  #     (synchronous unary handler).
  #   - Fake stream: minimal map matching the real gRPC stream contract.
  #   - DB injection: Room.Server.init/1 uses Application.get_env(:room_manager, :db_module).
  #     FakeDB avoids PostgreSQL.
  #   - Two tests:
  #       1. Room GenServer running → update_settings cast dispatched,
  #          response %Core.UpdateRoomSettingsResponse{ok: true}.
  #       2. Room GenServer NOT running (:not_found) → no crash,
  #          response %Core.UpdateRoomSettingsResponse{ok: true} (best-effort).

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB ──────────────────────────────────────────────────────────────────

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:update_room_settings_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:update_room_settings_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:update_room_settings_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:update_room_settings_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:update_room_settings_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:update_room_settings_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:update_room_settings_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:update_room_settings_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:update_room_settings_test_db) != :undefined do
      :ets.delete(:update_room_settings_test_db)
    end

    :ets.new(:update_room_settings_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, FakeDB)

    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)

      if :ets.info(:update_room_settings_test_db) != :undefined do
        :ets.delete(:update_room_settings_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Helpers ─────────────────────────────────────────────────────────────────

  defp build_stream, do: %{http_request_headers: %{}}

  defp start_and_track_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, pid} ->
        on_exit(fn ->
          if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
            case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
              {:ok, p} -> Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, p)
              _ -> :ok
            end
          end
        end)

        {:ok, pid}

      error ->
        error
    end
  end

  # ─── Test 1: Room GenServer running → cast dispatched, response ok: true ──────
  #
  # Given: Room GenServer started for room_id
  # When: update_room_settings/2 is called with max_members: 5
  # Then: returns %Core.UpdateRoomSettingsResponse{ok: true}
  #       AND GenServer state reflects the new max_members (cast was processed)

  describe "Server.update_room_settings/2 — room GenServer running" do
    test "returns UpdateRoomSettingsResponse{ok: true} and dispatches cast to GenServer" do
      room_id = "!urs-running:test.local"

      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      start_and_track_room(room_id)

      request = %Core.UpdateRoomSettingsRequest{
        room_id: room_id,
        max_members: 5
      }

      response = Server.update_room_settings(request, build_stream())

      assert %Core.UpdateRoomSettingsResponse{ok: true} = response

      # Synchronize with the GenServer to confirm the cast was processed.
      state = Nebu.Room.Server.get_state(room_id)
      assert state.max_members == 5
    end
  end

  # ─── Test 2: Room GenServer NOT running → no crash, response ok: true ─────────
  #
  # Given: no Room GenServer running for room_id
  # When: update_room_settings/2 is called
  # Then: returns %Core.UpdateRoomSettingsResponse{ok: true} without raising
  #       (best-effort: settings will be loaded from DB on next GenServer init)

  describe "Server.update_room_settings/2 — room GenServer not running" do
    test "returns UpdateRoomSettingsResponse{ok: true} without crashing when room not found" do
      room_id = "!urs-not-running:test.local"

      # Confirm no GenServer is running for this room_id.
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)

      request = %Core.UpdateRoomSettingsRequest{
        room_id: room_id,
        max_members: 10
      }

      response = Server.update_room_settings(request, build_stream())

      assert %Core.UpdateRoomSettingsResponse{ok: true} = response
    end
  end
end
