defmodule Nebu.Room.Server do
  @moduledoc """
  Room GenServer — lifecycle + send-event + power-level enforcement + typing indicators
  (Stories 4-2, 4-4, 4-13, 4-17).

  Manages room membership state with PostgreSQL persistence.
  Rooms can be created, users can join and leave, and the current member
  list is always available in memory via a MapSet.

  Story 4-4 adds `send_event/5`: processes, signs (Ed25519), and persists send-event
  requests with full txnId idempotency via ETS `NebuTxnDedup`.

  Story 4-13 adds power level support:
  - `power_levels` map is loaded from DB on init (string keys throughout).
  - `send_event/5` enforces `events_default` threshold before processing.
  - `set_power_levels/3` validates caller has `change_state` power before persisting.
  - `default_power_levels/0` is a public function for use by EventDispatcher.

  Story 4-17 adds ephemeral typing state:
  - `set_typing/4` manages `typing_users: MapSet` in GenServer state.
  - Typing state is EPHEMERAL — Persistence Strategy: Option C (Stateless).
  - Auto-expiry via `Process.send_after/3` → `{:typing_expire, user_id}`.
  - Broadcasts `{:typing_update, user_id, typing}` via :pg room group.

  State structure:
    %{
      room_id:      String.t(),
      members:      MapSet.t(String.t()),
      power_levels: map(),          # string-key map — see Nebu.Room.PowerLevels
      created_at:   DateTime.t(),
      typing_users: MapSet.t(String.t())  # ephemeral — resets on restart
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

  @doc """
  Returns the default Matrix power levels map.

  Public so that EventDispatcher.Server and tests can build creator-boosted maps
  without duplicating the defaults. String keys throughout (architecture rule).
  """
  @spec default_power_levels() :: map()
  def default_power_levels, do: Nebu.Room.PowerLevels.default_levels()

  @doc """
  Updates the power levels for `room_id`.

  The `caller_id` must have `change_state` power (level >= `state_default`).
  Returns `:ok` on success, `{:error, :forbidden}` if the caller lacks power,
  or `{:error, reason}` on DB failure (state unchanged on DB error).
  """
  @spec set_power_levels(String.t(), String.t(), map()) :: :ok | {:error, term()}
  def set_power_levels(room_id, caller_id, new_levels) do
    GenServer.call(via(room_id), {:set_power_levels, new_levels, caller_id})
  end

  @doc """
  Sets or clears the typing indicator for `user_id` in `room_id`.

  When `typing: true`, schedules auto-clear after `timeout_ms` via `Process.send_after`.
  When `typing: false`, clears immediately.
  Broadcasts `{:typing_update, user_id, typing}` to all :pg room subscribers.
  State is ephemeral — NOT persisted to DB. No crash/restart recovery needed.
  (Persistence Strategy: Option C — Stateless)
  """
  @spec set_typing(String.t(), String.t(), boolean(), integer()) :: :ok
  def set_typing(room_id, user_id, typing, timeout_ms) do
    GenServer.call(via(room_id), {:set_typing, user_id, typing, timeout_ms})
  end

  @doc """
  Processes a send-event request for the given `room_id`.

  Checks txn_id idempotency via ETS `NebuTxnDedup` first. On cache hit,
  returns the existing event_id immediately without re-processing.

  On cache miss: builds the event map, generates a content-hash event_id
  via `Nebu.EventId.generate/1`, signs the canonical JSON with the server
  Ed25519 key, persists to PostgreSQL `events`, inserts into ETS, and
  broadcasts `{:new_event, signed_event}` via `:pg` room group.

  Returns `{:ok, event_id}` on success or `{:error, reason}` on DB failure.
  On DB failure: ETS is NOT updated and NO broadcast is sent.
  """
  @spec send_event(String.t(), String.t(), String.t(), map(), String.t()) ::
          {:ok, String.t()} | {:error, term()}
  def send_event(room_id, user_id, event_type, content, txn_id) do
    GenServer.call(via(room_id), {:send_event, user_id, event_type, content, txn_id})
  end

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
    # NOTE: Room.Server intentionally does NOT join the "room:#{room_id}" :pg group.
    # The :pg group is used by external subscribers (EventBus handler, get_sync_delta
    # handler) to receive broadcast events. The Room GenServer itself broadcasts TO the
    # group but does not need to receive its own broadcasts (handle_info ignores them).
    # Keeping the Room GenServer out of the group prevents interference with tests that
    # assert the group is empty after sync handlers clean up.
    case db_module().load_members(room_id) do
      {:ok, user_ids, created_at_ms, power_levels_json} ->
        members = MapSet.new(user_ids)
        created_at = DateTime.from_unix!(created_at_ms, :millisecond)
        power_levels = parse_power_levels(power_levels_json)
        {:ok, %{room_id: room_id, members: members, power_levels: power_levels, created_at: created_at, typing_users: MapSet.new()}}

      {:error, :not_found} ->
        case db_module().insert_room(room_id) do
          {:ok, created_at_ms} ->
            created_at = DateTime.from_unix!(created_at_ms, :millisecond)

            {:ok,
             %{
               room_id: room_id,
               members: MapSet.new(),
               power_levels: %{},
               created_at: created_at,
               typing_users: MapSet.new()
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

  @impl GenServer
  def handle_call({:set_power_levels, new_levels, caller_id}, _from, state) do
    # Allow the call when power_levels is empty (bootstrapping — new room, first assignment).
    # After initial power levels are set, the normal change_state check applies.
    allowed =
      state.power_levels == %{} or
        Nebu.Room.PowerLevels.can?(state.power_levels, caller_id, :change_state)

    if allowed do
      case db_module().set_power_levels(state.room_id, Jason.encode!(new_levels)) do
        :ok ->
          {:reply, :ok, %{state | power_levels: new_levels}}

        {:error, reason} ->
          {:reply, {:error, reason}, state}
      end
    else
      {:reply, {:error, :forbidden}, state}
    end
  end

  @impl GenServer
  def handle_call({:send_event, user_id, event_type, content, txn_id}, _from, state) do
    room_id = state.room_id

    # Step 0 — Power level check: reject before idempotency lookup.
    # An unauthorized user must not receive an event_id — not even a cached one.
    unless Nebu.Room.PowerLevels.can?(state.power_levels, user_id, :send_event) do
      {:reply, {:error, :forbidden}, state}
    else

    # Step 1 — Idempotency check: return existing event_id immediately if found.
    case :ets.lookup(:NebuTxnDedup, {room_id, user_id, txn_id}) do
      [{_, existing_event_id}] ->
        {:reply, {:ok, existing_event_id}, state}

      [] ->
        # Step 2 — Build event map with string keys only (architecture rule: no atom keys).
        event_map = %{
          "room_id" => room_id,
          "type" => event_type,
          "sender" => user_id,
          "content" => content,
          "origin_server_ts" => Nebu.DB.Helpers.now_ms()
        }

        # Step 3 — Generate content-hash event_id (architecture rule #7: always via Nebu.EventId).
        event_id = Nebu.EventId.generate(event_map)
        event_with_id = Map.put(event_map, "event_id", event_id)

        # Step 4 — Sign the canonical event JSON with server Ed25519 key.
        # Key is generated once at Application boot and stored in persistent_term.
        # Sign event_map (WITHOUT event_id and signatures) — Matrix convention:
        # event_id is the content hash, so signing must cover the same payload
        # that was hashed (without event_id). Verifiers rebuild the signed bytes
        # from the event fields, excluding event_id/signatures/unsigned.
        {_pub, priv} = :persistent_term.get(:nebu_signing_key)
        event_json = Nebu.CanonicalJson.encode!(event_map)
        signature = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
        sig_b64 = Base.encode64(signature)
        signed_event = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})

        # Step 5 — Persist to DB (append-only).
        case db_module().insert_event(signed_event) do
          :ok ->
            # Step 6 — Only on DB success: update ETS, broadcast, return ok.
            # TODO(Story 4-X): Add TTL-based pruning for NebuTxnDedup entries.
            # Currently entries grow unbounded over the lifetime of the VM.
            :ets.insert(:NebuTxnDedup, {{room_id, user_id, txn_id}, event_id})

            # Broadcast to all processes subscribed to this room's :pg group.
            # Fire-and-forget: no subscribers is a no-op (correct for MVP).
            members = :pg.get_local_members("room:#{room_id}")
            Enum.each(members, fn pid -> send(pid, {:new_event, signed_event}) end)

            {:reply, {:ok, event_id}, state}

          {:error, reason} ->
            # AC #3: On DB failure — do NOT insert ETS, do NOT broadcast.
            {:reply, {:error, reason}, state}
        end
    end
    end
  end

  # ─── handle_call for :set_typing ──────────────────────────────────────────

  @impl GenServer
  def handle_call({:set_typing, user_id, typing, timeout_ms}, _from, state) do
    new_typing_users =
      if typing do
        # Schedule auto-expire. Fire-and-forget: stale expire messages are
        # handled gracefully in handle_info({:typing_expire, ...}) below.
        Process.send_after(self(), {:typing_expire, user_id}, timeout_ms)
        MapSet.put(state.typing_users, user_id)
      else
        MapSet.delete(state.typing_users, user_id)
      end

    # Broadcast to :pg room group subscribers.
    members = :pg.get_local_members("room:#{state.room_id}")
    Enum.each(members, fn pid -> send(pid, {:typing_update, user_id, typing}) end)

    {:reply, :ok, %{state | typing_users: new_typing_users}}
  end

  # Handle incoming :new_event broadcasts from :pg group members (including self).
  # Story 4-4: fire-and-forget — Room GenServer receives its own broadcast.
  # Story 4-8 will add a real subscriber that forwards events to gRPC streams.
  @impl GenServer
  def handle_info({:new_event, _event}, state) do
    {:noreply, state}
  end

  # Handle :typing_expire — auto-clear a user's typing indicator after timeout.
  # Only broadcasts if the user is still in the typing set (prevents double-expire
  # from stale timer messages when typing was already cleared via set_typing=false).
  @impl GenServer
  def handle_info({:typing_expire, user_id}, state) do
    if MapSet.member?(state.typing_users, user_id) do
      new_typing_users = MapSet.delete(state.typing_users, user_id)
      members = :pg.get_local_members("room:#{state.room_id}")
      Enum.each(members, fn pid -> send(pid, {:typing_update, user_id, false}) end)
      {:noreply, %{state | typing_users: new_typing_users}}
    else
      # Stale timer message (user already cleared typing) — no-op.
      {:noreply, state}
    end
  end

  # ─── Private ───────────────────────────────────────────────────────────────

  defp via(room_id), do: {:via, Horde.Registry, {Nebu.Room.Registry, room_id}}

  # Parses power_levels_json from DB into an Elixir map with string keys.
  # Returns the defaults map when json is empty ("{}") or blank.
  defp parse_power_levels(json) when is_binary(json) do
    case Jason.decode(json) do
      {:ok, map} when map == %{} -> %{}
      {:ok, map} -> map
      {:error, _} -> %{}
    end
  end

  defp parse_power_levels(_), do: %{}
end
