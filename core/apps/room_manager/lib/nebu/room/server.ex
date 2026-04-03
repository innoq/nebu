defmodule Nebu.Room.Server do
  @moduledoc """
  Room GenServer — full lifecycle implementation for Story 4-2.

  Manages room membership state with PostgreSQL persistence.
  Rooms can be created, users can join and leave, and the current member
  list is always available in memory via a MapSet.

  State structure:
    %{
      room_id:      String.t(),
      members:      MapSet.t(String.t()),
      power_levels: map(),          # empty — filled in Story 4-13
      created_at:   DateTime.t()
    }

  DB writes go through the configurable db_module/0 helper (defaults to Nebu.Room.DB).
  Inject a fake via `Application.put_env(:room_manager, :db_module, FakeDB)` in tests.
  """

  use GenServer

  # Resolve DB module at runtime so tests can override via Application.put_env.
  # Using a private function instead of a compile-time attribute enables injection.
  defp db_module, do: Application.get_env(:room_manager, :db_module, Nebu.Room.DB)

  # ─── Public API ────────────────────────────────────────────────────────────

  @doc "Returns the full state map for `room_id`."
  @spec get_state(String.t()) :: map()
  def get_state(room_id), do: GenServer.call(via(room_id), :get_state)

  @doc """
  Adds `user_id` to the room's member set.

  Returns `:ok` on success, `{:error, :already_member}` if already joined,
  or `{:error, reason}` on DB failure (state unchanged).
  """
  @spec join(String.t(), String.t()) :: :ok | {:error, term()}
  def join(room_id, user_id), do: GenServer.call(via(room_id), {:join, user_id})

  @doc """
  Removes `user_id` from the room's member set (soft-delete in DB).

  Returns `:ok` on success, `{:error, :not_member}` if not currently joined,
  or `{:error, reason}` on DB failure (state unchanged).
  """
  @spec leave(String.t(), String.t()) :: :ok | {:error, term()}
  def leave(room_id, user_id), do: GenServer.call(via(room_id), {:leave, user_id})

  # ─── Child Spec ────────────────────────────────────────────────────────────

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
      name: via(room_id)
    )
  end

  # ─── GenServer Callbacks ───────────────────────────────────────────────────

  @impl GenServer
  def init(room_id) do
    case db_module().load_members(room_id) do
      {:ok, user_ids, created_at_ms} ->
        members = MapSet.new(user_ids)
        created_at = DateTime.from_unix!(created_at_ms, :millisecond)
        {:ok, %{room_id: room_id, members: members, power_levels: %{}, created_at: created_at}}

      {:error, :not_found} ->
        case db_module().insert_room(room_id) do
          {:ok, created_at_ms} ->
            created_at = DateTime.from_unix!(created_at_ms, :millisecond)

            {:ok,
             %{
               room_id: room_id,
               members: MapSet.new(),
               power_levels: %{},
               created_at: created_at
             }}

          {:error, reason} ->
            {:stop, reason}
        end

      {:error, reason} ->
        {:stop, reason}
    end
  end

  @impl GenServer
  def handle_call(:get_state, _from, state) do
    {:reply, state, state}
  end

  @impl GenServer
  def handle_call({:join, user_id}, _from, %{members: members} = state) do
    if MapSet.member?(members, user_id) do
      {:reply, {:error, :already_member}, state}
    else
      case db_module().insert_member(state.room_id, user_id) do
        :ok ->
          new_state = %{state | members: MapSet.put(members, user_id)}
          {:reply, :ok, new_state}

        {:error, reason} ->
          {:reply, {:error, reason}, state}
      end
    end
  end

  @impl GenServer
  def handle_call({:leave, user_id}, _from, %{members: members} = state) do
    if not MapSet.member?(members, user_id) do
      {:reply, {:error, :not_member}, state}
    else
      case db_module().delete_member(state.room_id, user_id) do
        :ok ->
          new_state = %{state | members: MapSet.delete(members, user_id)}
          {:reply, :ok, new_state}

        {:error, reason} ->
          {:reply, {:error, reason}, state}
      end
    end
  end

  # ─── Private ───────────────────────────────────────────────────────────────

  defp via(room_id), do: {:via, Horde.Registry, {Nebu.Room.Registry, room_id}}
end
