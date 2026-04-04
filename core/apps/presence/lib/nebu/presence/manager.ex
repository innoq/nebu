defmodule Nebu.Presence.Manager do
  @moduledoc """
  GenServer for tracking and broadcasting user presence state.

  State is stored in the `:NebuPresence` ETS table (owned by
  `Nebu.Presence.Application`) so that data survives GenServer crashes and
  supervisor restarts.

  ETS key structure:
    {user_id :: String.t(), status :: :online | :offline | :unavailable,
     last_active_at :: integer() | nil}

  Heartbeat transitions:
    - :online  → :unavailable  after `unavailable_threshold_ms` (default 60s)
    - :unavailable → :offline  after `offline_threshold_ms`      (default 300s)

  Broadcast:
    Presence updates are published to the `:pg` Process Group `"presence"` as
    `{:presence_update, user_id, status}` messages (ADR-002).
  """

  use GenServer

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Starts the Manager and registers it under its module name so that
  `Process.whereis(Nebu.Presence.Manager)` works for crash/restart tests.
  """
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
  end

  @doc """
  Upserts the presence entry for `user_id` with `status` and broadcasts the
  update to all subscribers in the `"presence"` :pg group.

  Non-blocking (GenServer.cast). Returns `:ok` immediately.
  """
  @spec set_presence(String.t(), :online | :offline | :unavailable) :: :ok
  def set_presence(user_id, status) do
    GenServer.cast(__MODULE__, {:set_presence, user_id, status})
  end

  @doc """
  Returns the current presence for `user_id`.

  Reads directly from the public ETS table — no GenServer round-trip.
  Missing users default to `{:ok, %{status: :offline, last_active_at: nil}}`
  (NEVER returns `{:error, :not_found}`).
  """
  @spec get_presence(String.t()) :: {:ok, %{status: atom(), last_active_at: integer() | nil}}
  def get_presence(user_id) do
    case :ets.lookup(:NebuPresence, user_id) do
      [{^user_id, status, last_active_at}] ->
        {:ok, %{status: status, last_active_at: last_active_at}}

      [] ->
        {:ok, %{status: :offline, last_active_at: nil}}
    end
  end

  @doc """
  Subscribes the calling process to presence broadcast messages by joining the
  `:pg` Process Group `"presence"`.
  """
  @spec subscribe() :: :ok
  def subscribe do
    :pg.join("presence", self())
  end

  @doc """
  Removes the calling process from the `"presence"` :pg group.
  """
  @spec unsubscribe() :: :ok
  def unsubscribe do
    :pg.leave("presence", self())
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(:ok) do
    Process.send_after(self(), :check_heartbeats, heartbeat_interval())
    {:ok, :no_state}
  end

  @impl true
  def handle_cast({:set_presence, user_id, status}, state) do
    last_active_at = System.system_time(:millisecond)
    :ets.insert(:NebuPresence, {user_id, status, last_active_at})

    # Broadcast to all subscribers in the "presence" :pg group (ADR-002)
    members = :pg.get_members("presence")

    Enum.each(members, fn pid ->
      send(pid, {:presence_update, user_id, status})
    end)

    {:noreply, state}
  end

  @impl true
  def handle_info(:check_heartbeats, state) do
    now_ms = System.system_time(:millisecond)
    unavailable_threshold_ms = Application.get_env(:presence, :unavailable_threshold_ms, 60_000)
    offline_threshold_ms = Application.get_env(:presence, :offline_threshold_ms, 300_000)

    :ets.tab2list(:NebuPresence)
    |> Enum.each(fn {user_id, status, last_active_at} ->
      age_ms = now_ms - last_active_at

      new_status =
        cond do
          status == :online and age_ms >= unavailable_threshold_ms -> :unavailable
          status == :unavailable and age_ms >= offline_threshold_ms -> :offline
          true -> status
        end

      if new_status != status do
        :ets.insert(:NebuPresence, {user_id, new_status, last_active_at})
      end
    end)

    # Reschedule the next heartbeat check
    Process.send_after(self(), :check_heartbeats, heartbeat_interval())
    {:noreply, state}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp heartbeat_interval do
    Application.get_env(:presence, :heartbeat_interval_ms, 60_000)
  end
end
