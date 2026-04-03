defmodule Nebu.Room.Server do
  @moduledoc """
  Room GenServer — lifecycle + send-event implementation for Stories 4-2 and 4-4.

  Manages room membership state with PostgreSQL persistence.
  Rooms can be created, users can join and leave, and the current member
  list is always available in memory via a MapSet.

  Story 4-4 adds `send_event/5`: processes, signs (Ed25519), and persists send-event
  requests with full txnId idempotency via ETS `NebuTxnDedup`.

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
    # Join the :pg process group for this room so the GenServer receives
    # broadcast messages. Story 4-8 builds the gRPC EventBus on top of this.
    :pg.join("room:#{room_id}", self())

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

  @impl GenServer
  def handle_call({:send_event, user_id, event_type, content, txn_id}, _from, state) do
    room_id = state.room_id

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

  # Handle incoming :new_event broadcasts from :pg group members (including self).
  # Story 4-4: fire-and-forget — Room GenServer receives its own broadcast.
  # Story 4-8 will add a real subscriber that forwards events to gRPC streams.
  @impl GenServer
  def handle_info({:new_event, _event}, state) do
    {:noreply, state}
  end

  # ─── Private ───────────────────────────────────────────────────────────────

  defp via(room_id), do: {:via, Horde.Registry, {Nebu.Room.Registry, room_id}}
end
