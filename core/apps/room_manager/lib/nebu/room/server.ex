defmodule Nebu.Room.Server do
  @moduledoc """
  Room GenServer — stub implementation for Story 4-1.

  Registers itself in `Nebu.Room.Registry` on startup and holds minimal state.
  Full room lifecycle (create, join, leave, event dispatch) is added in Story 4-2.
  """

  use GenServer

  @doc """
  Overrides the default child spec so that each Room GenServer has a unique
  child id based on `room_id`. Without this override all rooms would share the
  id `Nebu.Room.Server` and Horde would refuse to start a second one.
  """
  def child_spec(room_id) do
    %{
      id: {__MODULE__, room_id},
      start: {__MODULE__, :start_link, [room_id]},
      restart: :transient,
      shutdown: 5_000,
      type: :worker
    }
  end

  @doc """
  Starts the Room GenServer, registering it in `Nebu.Room.Registry` under `room_id`.
  """
  def start_link(room_id) do
    GenServer.start_link(
      __MODULE__,
      room_id,
      name: {:via, Horde.Registry, {Nebu.Room.Registry, room_id}}
    )
  end

  @impl GenServer
  def init(room_id) do
    {:ok, %{room_id: room_id}}
  end
end
