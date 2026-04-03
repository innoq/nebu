defmodule Nebu.RoomTest do
  use ExUnit.Case, async: false

  # Horde.Registry and Horde.DynamicSupervisor are started by the room_manager
  # Application (Nebu.Room.Application) on app boot. Tests run against those
  # already-running named processes.
  # async: false is required because Horde uses global named processes.

  defp unique_room_id(prefix) do
    "#{prefix}-#{System.unique_integer([:positive])}"
  end

  defp start_and_track(room_id) do
    {:ok, pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      if Process.alive?(pid), do: GenServer.stop(pid, :normal, 5_000)
    end)

    {:ok, pid}
  end

  describe "Nebu.Room.RoomSupervisor.start_room/1" do
    test "returns {:ok, pid} for a new room" do
      room_id = unique_room_id("start-room")
      {:ok, pid} = start_and_track(room_id)
      assert is_pid(pid)
    end

    test "returns {:ok, pid} of existing process when room already started" do
      room_id = unique_room_id("idempotent")
      {:ok, pid1} = start_and_track(room_id)
      {:ok, pid2} = Nebu.Room.RoomSupervisor.start_room(room_id)
      assert pid1 == pid2
    end
  end

  describe "Nebu.Room.RoomSupervisor.lookup_room/1" do
    test "returns {:ok, pid} after room is started" do
      room_id = unique_room_id("lookup")
      {:ok, started_pid} = start_and_track(room_id)
      assert {:ok, found_pid} = Nebu.Room.RoomSupervisor.lookup_room(room_id)
      assert started_pid == found_pid
    end

    test "returns {:error, :not_found} for a room that was never started" do
      room_id = unique_room_id("nonexistent")
      assert {:error, :not_found} = Nebu.Room.RoomSupervisor.lookup_room(room_id)
    end
  end

  describe "Nebu.Room.Manager delegates" do
    test "start_room/1 delegates to RoomSupervisor" do
      room_id = unique_room_id("manager-start")
      {:ok, pid} = start_and_track(room_id)
      assert is_pid(pid)
    end

    test "lookup_room/1 delegates to RoomSupervisor" do
      room_id = unique_room_id("manager-lookup")
      {:ok, _pid} = start_and_track(room_id)
      assert {:ok, _pid} = Nebu.Room.Manager.lookup_room(room_id)
    end
  end
end
