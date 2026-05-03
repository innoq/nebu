defmodule Nebu.Room.RoomSupervisor do
  @moduledoc """
  Public API for starting and looking up Room GenServer processes.

  Wraps `Horde.DynamicSupervisor` and `Horde.Registry` to provide a clean
  interface for room lifecycle operations. Full room state management is added
  in Story 4-2.
  """

  @doc """
  Starts a Room GenServer for the given `room_id` under the Horde DynamicSupervisor.

  Returns `{:ok, pid}` on success. If the room is already running, returns
  `{:ok, pid}` of the existing process.
  """
  @spec start_room(String.t()) :: {:ok, pid()} | {:error, term()}
  def start_room(room_id) do
    case Horde.DynamicSupervisor.start_child(
           Nebu.Room.HordeSupervisor,
           {Nebu.Room.Server, room_id}
         ) do
      {:ok, pid} -> await_registry(room_id, pid)
      {:error, {:already_started, pid}} -> {:ok, pid}
      {:error, reason} -> {:error, reason}
    end
  end

  # Horde.Registry uses CRDT replication — the pid may not be visible via lookup
  # immediately after start_child returns. Poll until the entry appears (max 500 ms).
  defp await_registry(room_id, pid, retries \\ 100) do
    case Horde.Registry.lookup(Nebu.Room.Registry, room_id) do
      [{^pid, _}] ->
        {:ok, pid}

      _ when retries > 0 ->
        Process.sleep(5)
        await_registry(room_id, pid, retries - 1)

      _ ->
        {:error, :registry_timeout}
    end
  end

  @doc """
  Looks up the PID of a running Room GenServer for the given `room_id`.

  Returns `{:ok, pid}` if the room process is running, or `{:error, :not_found}`
  if no process is registered under `room_id`.
  """
  @spec lookup_room(String.t()) :: {:ok, pid()} | {:error, :not_found}
  def lookup_room(room_id) do
    case Horde.Registry.lookup(Nebu.Room.Registry, room_id) do
      [{pid, _value}] -> {:ok, pid}
      [] -> {:error, :not_found}
    end
  end
end
