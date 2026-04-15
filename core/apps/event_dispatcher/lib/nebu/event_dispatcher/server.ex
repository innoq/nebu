defmodule Nebu.EventDispatcher.Server do
  use GRPC.Server, service: Core.CoreService.Service

  require Logger

  # ─── Configurable room registry module for testability ─────────────────────
  # Override via Application.put_env(:event_dispatcher, :room_registry_module, FakeModule) in tests.
  defp room_registry_module do
    Application.get_env(:event_dispatcher, :room_registry_module, Nebu.Room.Server)
  end

  # ─── Configurable invite DB module for testability ──────────────────────────
  # Override via Application.put_env(:event_dispatcher, :invite_db_module, FakeInviteDB) in tests.
  defp db_module_invite do
    Application.get_env(:event_dispatcher, :invite_db_module, Nebu.Room.InviteDB)
  end

  # ─── Configurable messages DB module for testability ─────────────────────────
  # Override via Application.put_env(:event_dispatcher, :messages_db_module, FakeDB) in tests.
  defp messages_db_module do
    Application.get_env(:event_dispatcher, :messages_db_module, Nebu.Room.DB)
  end

  # ─── Configurable rooms DB module for testability ─────────────────────────────
  # Override via Application.put_env(:event_dispatcher, :rooms_db_module, FakeDB) in tests.
  defp rooms_db_module do
    Application.get_env(:event_dispatcher, :rooms_db_module, Nebu.Room.DB)
  end

  # ─── Configurable receipt DB module for testability ──────────────────────────
  # Override via Application.put_env(:event_dispatcher, :receipt_db_module, FakeReceiptDB) in tests.
  defp receipt_db_module do
    Application.get_env(:event_dispatcher, :receipt_db_module, Nebu.Receipt.DB)
  end

  # ─── Configurable profile DB module for testability ──────────────────────────
  # Override via Application.put_env(:event_dispatcher, :profile_db_module, FakeProfileDB) in tests.
  defp profile_db_module do
    Application.get_env(:event_dispatcher, :profile_db_module, Nebu.Profile.DB)
  end

  # ─── Configurable presence module for testability ────────────────────────────
  # Override via Application.put_env(:event_dispatcher, :presence_module, FakePresenceModule) in tests.
  defp presence_module do
    Application.get_env(:event_dispatcher, :presence_module, Nebu.Presence.Manager)
  end

  # ─── Configurable PgStore module for testability ──────────────────────────────
  # Override via Application.put_env(:event_dispatcher, :pg_store_module, FakePgStore) in tests.
  defp pg_store_module do
    Application.get_env(:event_dispatcher, :pg_store_module, Nebu.Session.PgStore)
  end

  def send_event(request, _stream) do
    room_id = request.room_id
    sender_id = request.sender_id
    event_type = request.event_type
    txn_id = request.txn_id

    # Decode content bytes from protobuf (bytes field → binary → decode to map).
    content =
      case Jason.decode(request.content) do
        {:ok, map} -> map
        {:error, _} -> %{}
      end

    # Verify the room exists (Room GenServer must be running).
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        # Membership check: sender must be a room member before sending.
        state = room_registry_module().get_state(room_id)

        unless MapSet.member?(state.members, sender_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "#{sender_id} is not a member of #{room_id}"
        end

        # Delegate to Room.Server — handles idempotency, signing, persistence, broadcast.
        case Nebu.Room.Server.send_event(room_id, sender_id, event_type, content, txn_id) do
          {:ok, event_id} ->
            %Core.SendEventResponse{event_id: event_id}

          {:error, :forbidden} ->
            raise GRPC.RPCError,
              status: GRPC.Status.permission_denied(),
              message: "#{sender_id} lacks power level to send events in #{room_id}"

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "send_event failed: #{inspect(reason)}"
        end
    end
  end

  def create_room(request, _stream) do
    room_id = generate_room_id()
    creator_id = request.creator_id

    case Nebu.Room.RoomSupervisor.start_room(room_id) do
      {:ok, _pid} ->
        :ok = Nebu.Room.Server.join(room_id, creator_id)

        default_pl = Nebu.Room.Server.default_power_levels()
        creator_pl = put_in(default_pl, ["users", creator_id], 100)
        :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

        %Core.CreateRoomResponse{room_id: room_id}

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "Failed to start room: #{inspect(reason)}"
    end
  end

  defp generate_room_id do
    server_name = Application.get_env(:event_dispatcher, :server_name, "nebu.local")
    opaque = :crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower)
    "!#{opaque}:#{server_name}"
  end

  def join_room(request, _stream) do
    room_id = request.room_id_or_alias
    user_id = request.user_id

    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        case Nebu.Room.Server.join(room_id, user_id) do
          :ok ->
            # Mark any pending invitation as accepted so it disappears from
            # rooms.invite in subsequent sync responses (no-op for public joins).
            db_module_invite().accept_invitation(room_id, user_id)
            %Core.JoinRoomResponse{room_id: room_id}

          {:error, :already_member} ->
            # Matrix spec: idempotent — joining an already-joined room is success.
            %Core.JoinRoomResponse{room_id: room_id}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "join failed: #{inspect(reason)}"
        end
    end
  end

  def leave_room(request, _stream) do
    room_id = request.room_id
    user_id = request.user_id

    # If the room GenServer is not running (e.g. after stack restart or the user
    # was only invited and the room was never started for them), we still honour
    # the leave / decline-invite semantics by operating on the DB directly.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        # Room GenServer not running — try to reject any pending invitation so
        # the room disappears from rooms.invite. This covers the common case where
        # a user wants to decline an invite after a stack restart.
        db_module_invite().reject_invitation(room_id, user_id)
        %Core.LeaveRoomResponse{room_id: room_id}

      {:ok, _pid} ->
        case Nebu.Room.Server.leave(room_id, user_id) do
          :ok ->
            %Core.LeaveRoomResponse{room_id: room_id}

          {:error, :not_member} ->
            # User has a pending invite but hasn't joined — treat as "decline invite".
            db_module_invite().reject_invitation(room_id, user_id)
            %Core.LeaveRoomResponse{room_id: room_id}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "leave failed: #{inspect(reason)}"
        end
    end
  end

  def invite_user(request, _stream) do
    room_id = request.room_id
    inviter = request.inviter_id
    invitee = request.invitee_id

    # Verify the room exists and inviter is a member.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = Nebu.Room.Server.get_state(room_id)

        unless MapSet.member?(state.members, inviter) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "you are not a member of this room"
        end

        unless Nebu.Room.PowerLevels.can?(state.power_levels, inviter, :invite) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "insufficient power level for invite"
        end

        case db_module_invite().insert_invitation(room_id, inviter, invitee) do
          :ok ->
            %Core.InviteUserResponse{}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "invite failed: #{inspect(reason)}"
        end
    end
  end

  def get_messages(request, stream) do
    room_id = request.room_id
    from_token = request.from_token
    limit = max(1, min(request.limit, 100))
    direction = if request.direction in ["f", "b"], do: request.direction, else: "b"

    # Extract caller identity from gRPC metadata (set by Go JWTMiddleware).
    {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    # Room existence check — Room GenServer must be running.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        # Membership check — caller must be a room member.
        state = room_registry_module().get_state(room_id)

        unless MapSet.member?(state.members, user_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "#{user_id} is not a member of #{room_id}"
        end

        # Fetch events from PostgreSQL via the configurable messages_db_module.
        # fetch_events/4 signature: (room_id, direction, limit, from_token)
        {:ok, events, next_batch, prev_batch} =
          messages_db_module().fetch_events(room_id, direction, limit, from_token)

        proto_events = Enum.map(events, &event_map_to_proto/1)

        %Core.GetMessagesResponse{
          events: proto_events,
          next_batch: next_batch,
          prev_batch: prev_batch
        }
    end
  end

  # Convert a DB event map (string keys) to a %Core.Event{} protobuf struct.
  # The `content` column is JSONB (returned as Elixir map by Postgrex) —
  # re-encode to JSON bytes for the proto bytes field.
  defp event_map_to_proto(event) do
    # content from the DB may arrive as either:
    #   - Elixir map (Postgrex decoded JSONB object directly)
    #   - Elixir binary/string (Postgrex decoded JSONB string — happens when
    #     the content was stored as a JSON-encoded string rather than a raw object).
    # Normalise to a map before re-encoding so the proto bytes field always
    # contains a JSON object, never a doubly-encoded JSON string.
    raw = Map.get(event, "content", %{})
    content_map =
      cond do
        is_map(raw) -> raw
        is_binary(raw) ->
          case Jason.decode(raw) do
            {:ok, decoded} when is_map(decoded) -> decoded
            _ -> %{}
          end
        true -> %{}
      end
    content_json = Jason.encode!(content_map)

    %Core.Event{
      event_id: Map.get(event, "event_id", ""),
      room_id: Map.get(event, "room_id", ""),
      sender_id: Map.get(event, "sender", ""),
      event_type: Map.get(event, "event_type", ""),
      content: content_json,
      origin_ts: Map.get(event, "origin_server_ts", 0),
      server_ts: System.system_time(:millisecond)
    }
  end

  def set_power_levels(request, stream) do
    room_id = request.room_id
    power_levels_json = request.power_levels_json

    # Extract caller identity from gRPC metadata (set by Go JWTMiddleware).
    {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    # Validate caller identity.
    if is_nil(user_id) or user_id == "" do
      raise GRPC.RPCError,
        status: GRPC.Status.unauthenticated(),
        message: "missing x-user-id metadata"
    end

    # Room existence check — Room GenServer must be running.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        new_levels =
          case Jason.decode(power_levels_json) do
            {:ok, map} -> map
            {:error, _} ->
              raise GRPC.RPCError,
                status: GRPC.Status.invalid_argument(),
                message: "invalid power_levels_json"
          end

        case Nebu.Room.Server.set_power_levels(room_id, user_id, new_levels) do
          :ok ->
            %Core.SetPowerLevelsResponse{}

          {:error, :forbidden} ->
            raise GRPC.RPCError,
              status: GRPC.Status.permission_denied(),
              message: "#{user_id} lacks state_default power level"

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "set_power_levels failed: #{inspect(reason)}"
        end
    end
  end

  def set_presence(_request, _stream) do
    %Core.SetPresenceResponse{}
  end

  def get_presence(request, _stream) do
    user_id = request.user_id

    {:ok, %{status: status, last_active_at: last_active_at}} =
      presence_module().get_presence(user_id)

    now_ms = System.system_time(:millisecond)

    last_active_ago =
      if is_nil(last_active_at) do
        0
      else
        max(0, now_ms - last_active_at)
      end

    %Core.GetPresenceResponse{
      presence: Atom.to_string(status),
      last_active_ago: last_active_ago
    }
  end

  def update_profile(request, stream) do
    {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    if is_nil(user_id) or user_id == "" do
      raise GRPC.RPCError,
        status: GRPC.Status.unauthenticated(),
        message: "missing x-user-id metadata"
    end

    # Guard: caller can only update their own profile (Go already enforces this,
    # but defense-in-depth at Core level).
    if request.user_id != user_id do
      raise GRPC.RPCError,
        status: GRPC.Status.permission_denied(),
        message: "cannot update another user's profile"
    end

    displayname = if request.displayname == "", do: nil, else: request.displayname
    avatar_url = if request.avatar_url == "", do: nil, else: request.avatar_url

    case profile_db_module().upsert_profile(user_id, displayname, avatar_url) do
      :ok ->
        %Core.UpdateProfileResponse{}

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "upsert_profile failed: #{inspect(reason)}"
    end
  end

  def set_typing(request, _stream) do
    room_id = request.room_id
    user_id = request.user_id
    typing = request.typing
    timeout_ms = request.timeout_ms |> max(0) |> min(30_000)

    # Room existence check.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = room_registry_module().get_state(room_id)

        unless MapSet.member?(state.members, user_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "#{user_id} is not a member of #{room_id}"
        end

        # Delegate typing state management to Room GenServer.
        :ok = room_registry_module().set_typing(room_id, user_id, typing, timeout_ms)

        %Core.SetTypingResponse{}
    end
  end

  def send_receipt(request, _stream) do
    room_id = request.room_id
    receipt_type = request.receipt_type
    event_id = request.event_id

    # user_id is sent in the request body by the Go gateway (same as join_room pattern).
    user_id = request.user_id

    if is_nil(user_id) or user_id == "" do
      raise GRPC.RPCError,
        status: GRPC.Status.unauthenticated(),
        message: "missing user_id in request"
    end

    # Room existence check.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = room_registry_module().get_state(room_id)

        unless MapSet.member?(state.members, user_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "#{user_id} is not a member of #{room_id}"
        end

        case receipt_db_module().upsert_receipt(room_id, user_id, receipt_type, event_id) do
          :ok ->
            %Core.SendReceiptResponse{}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "upsert_receipt failed: #{inspect(reason)}"
        end
    end
  end

  def validate_token(request, stream) do
    {user_id, system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    if is_nil(user_id) or user_id == "" do
      raise GRPC.RPCError,
        status: GRPC.Status.unauthenticated(),
        message: "missing x-user-id metadata"
    end

    case Nebu.Session.TokenValidator.validate(
           user_id,
           system_role,
           request.display_name,
           request.email
         ) do
      {:ok, user} ->
        %Core.ValidateTokenResponse{
          user_id: user.user_id,
          system_role: user.system_role,
          display_name: user.display_name,
          is_active: user.is_active
        }

      {:error, :deactivated} ->
        raise GRPC.RPCError,
          status: GRPC.Status.permission_denied(),
          message: "user account is deactivated"

      {:error, reason} ->
        Logger.error("validate_token failed", user_id: user_id, error: inspect(reason))

        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "internal error"
    end
  end

  def get_pending_events(_request, _stream) do
    %Core.GetPendingEventsResponse{}
  end

  def get_metrics(_request, _stream) do
    %Core.GetMetricsResponse{}
  end

  # ─── AC #2: EventBus server-streaming handler ───────────────────────────────
  #
  # Blocking receive loop that keeps the gRPC stream open and forwards events
  # from :pg process groups to the Go gateway.
  #
  # Lifecycle:
  #   1. Trap exits so we get {:EXIT, _, _} on abnormal termination
  #   2. Join the "event_bus:gateways" :pg group to track active connections
  #   3. Subscribe to all currently-active room :pg groups
  #   4. Block in receive loop, forwarding {:new_event, event_map} to stream
  #   5. On {:EXIT, ...}: leave all :pg groups and terminate cleanly
  #
  # :pg automatically removes dead processes from groups — the explicit leave
  # on trap is belt-and-suspenders cleanup and enables monitoring via the group.

  def event_bus(request, stream) do
    Logger.info("EventBus stream opened", node_id: request.node_id)

    # Trap exits so the receive loop gets {:EXIT, _, _} on process kill/stream close.
    # This enables clean :pg membership removal even on abnormal termination.
    Process.flag(:trap_exit, true)

    # Join the "event_bus:gateways" group to mark this stream as active.
    :pg.join("event_bus:gateways", self())

    # Subscribe to all currently-active rooms.
    subscribe_to_all_rooms()

    # Block until stream closes or process exits.
    event_bus_loop(stream)
  end

  defp event_bus_loop(stream) do
    receive do
      {:new_event, event_map} ->
        event = map_to_proto_event(event_map)
        do_send_reply(stream, event)
        event_bus_loop(stream)

      {:EXIT, _pid, _reason} ->
        # Stream closed by client or process was killed — clean up and exit.
        :pg.leave("event_bus:gateways", self())
        leave_all_room_groups()
        {:ok, stream}
    end
  end

  # ─── AC #3: GetRoomState unary handler ──────────────────────────────────────
  #
  # Looks up the Room GenServer via the configurable room_registry_module.
  # In production, room_registry_module == Nebu.Room.Server, whose get_state/1
  # calls GenServer.call(via(room_id), :get_state).
  # In tests, room_registry_module is overridden with FakeRoomRegistry.

  def get_room_state(request, _stream) do
    room_id = request.room_id
    mod = room_registry_module()

    state =
      try do
        mod.get_state(room_id)
      catch
        :exit, {:noproc, _} ->
          raise GRPC.RPCError,
            status: GRPC.Status.not_found(),
            message: "room not found: #{room_id}"
      end

    %Core.GetRoomStateResponse{
      members: MapSet.to_list(state.members),
      power_levels_json: Jason.encode!(state.power_levels),
      room_name: ""
    }
  end

  # ─── AC: GetInitialSync — full state snapshot for all joined rooms ───────────
  #
  # Flow:
  #   1. Extract user_id from request (trusted — Go Gateway set it).
  #   2. Query rooms_db_module for all room IDs where user is an active member.
  #   3. For each room_id:
  #      a. Get room state via room_registry_module().get_state/1.
  #      b. Build state events (m.room.member for each member + m.room.power_levels).
  #      c. Fetch last ≤20 timeline events from messages_db_module().
  #      d. Reverse events (DB returns newest-first, Matrix expects oldest-first).
  #   4. Generate opaque since_token (same format as Story 4-6).
  #   5. Persist token via pg_store_module().persist_since_token/3.
  #   6. Return %Core.GetInitialSyncResponse{}.

  def get_initial_sync(request, _stream) do
    user_id = request.user_id

    room_ids =
      case rooms_db_module().get_rooms_for_user(user_id) do
        {:ok, ids} ->
          ids

        {:error, reason} ->
          raise GRPC.RPCError,
            status: GRPC.Status.unavailable(),
            message: "database unavailable: #{inspect(reason)}"
      end

    sync_rooms =
      Enum.flat_map(room_ids, fn room_id ->
        try do
          state = room_registry_module().get_state(room_id)
          state_events = build_state_events(state)

          {:ok, events, _next_batch, prev_batch} =
            messages_db_module().fetch_events(room_id, "b", 20, "")

          timeline_events =
            events
            |> Enum.reverse()
            |> Enum.map(&event_map_to_proto/1)

          limited = length(events) >= 20

          [
            %Core.SyncRoom{
              room_id: room_id,
              state_events: state_events,
              timeline_events: timeline_events,
              limited: limited,
              prev_batch: prev_batch
            }
          ]
        catch
          # Room GenServer temporarily unavailable (e.g. crashed between DB query
          # and get_state call) — skip the room gracefully rather than crashing
          # the entire sync response.
          :exit, {:noproc, _} -> []
        end
      end)

    # Find the most recent event_id across all rooms for the since_token anchor.
    last_event_id =
      Enum.flat_map(sync_rooms, fn room -> room.timeline_events end)
      |> Enum.max_by(fn ev -> ev.origin_ts end, fn -> nil end)
      |> case do
        nil -> nil
        ev -> ev.event_id
      end

    since_token =
      Base.encode64(
        "#{user_id}:#{last_event_id || ""}:#{System.monotonic_time()}",
        padding: false
      )

    :ok = pg_store_module().persist_since_token(user_id, since_token, last_event_id)

    %Core.GetInitialSyncResponse{
      since_token: since_token,
      rooms: sync_rooms
    }
  end

  # ─── AC: GetSyncDelta — incremental sync with long-polling ─────────────────
  #
  # Flow (revised to prevent missed-event race):
  #   1. Extract user_id, since_token, timeout_ms from request.
  #   2. Clamp timeout_ms to max 30 000 ms.
  #   3. Look up last_event_id via pg_store_module().get_since_token(user_id).
  #      - {:error, :not_found} → fallback to get_initial_sync, set fallback_to_initial: true.
  #   4. Get room IDs: rooms_db_module().get_rooms_for_user(user_id).
  #   5. Subscribe to :pg groups for all user rooms BEFORE DB check (race prevention).
  #   6. Check DB for pending events via messages_db_module().fetch_events_since/3.
  #   7. If pending events → unsubscribe, return delta immediately.
  #   8. If no pending events AND timeout_ms > 0 → wait in receive loop.
  #   9. Generate and persist new since_token.
  #  10. Return %Core.GetSyncDeltaResponse{}.

  def get_sync_delta(request, _stream) do
    user_id = request.user_id
    _since_token = request.since_token
    timeout_ms = request.timeout_ms |> max(0) |> min(30_000)

    # Step 3: Resolve last_event_id from the since_token
    case pg_store_module().get_since_token(user_id) do
      {:error, :not_found} ->
        # Fallback to full initial sync
        initial_req = %Core.GetInitialSyncRequest{user_id: user_id}
        initial_resp = get_initial_sync(initial_req, %{http_request_headers: %{}})

        %Core.GetSyncDeltaResponse{
          since_token: initial_resp.since_token,
          rooms: initial_resp.rooms,
          fallback_to_initial: true
        }

      {:ok, %{last_event_id: last_event_id}} ->
        # Run the incremental sync in a short-lived Task so the Task's process
        # joins and leaves :pg groups. When the Task exits, :pg auto-cleans its
        # membership. This ensures the handler process itself never appears in any
        # room :pg group after get_sync_delta/2 returns.
        #
        # Capture all module-level injectable deps before spawning (they are looked
        # up at call time via Application.get_env, so the Task re-evaluates them in
        # its own context automatically — no capture needed).
        task_timeout = timeout_ms + 10_000

        task = Task.async(fn ->
          do_incremental_sync(user_id, last_event_id, timeout_ms)
        end)

        Task.await(task, task_timeout)
    end
  end

  # Runs the incremental sync logic in a separate (Task) process so that
  # :pg group subscriptions are owned by a process that exits when done.
  # :pg auto-removes dead processes from groups.
  defp do_incremental_sync(user_id, last_event_id, timeout_ms) do
    # Step 4: Get user's rooms
    room_ids =
      case rooms_db_module().get_rooms_for_user(user_id) do
        {:ok, ids} -> ids
        {:error, _} -> []
      end

    # Step 5: Subscribe to :pg groups BEFORE DB check (race prevention)
    Enum.each(room_ids, fn room_id ->
      :pg.join("room:#{room_id}", self())
    end)

    # Step 6: Check DB for pending events per room.
    # Pass last_event_id (string) directly — the DB module resolves the ts internally.
    delta_rooms = fetch_delta_rooms(room_ids, last_event_id)

    result_rooms =
      if delta_rooms != [] do
        # Step 7: Events found — leave :pg groups and return immediately
        Enum.each(room_ids, fn room_id ->
          :pg.leave("room:#{room_id}", self())
        end)

        delta_rooms
      else
        # Step 8: No events — enter long-poll if timeout_ms > 0
        wait_result =
          if timeout_ms > 0 do
            timer_ref = Process.send_after(self(), :sync_long_poll_timeout, timeout_ms)

            receive do
              {:new_event, _event_map} ->
                # Cancel the timer and re-query DB for all pending events
                Process.cancel_timer(timer_ref)
                flush_long_poll_timeout()
                fetch_delta_rooms(room_ids, last_event_id)

              :sync_long_poll_timeout ->
                []
            end
          else
            []
          end

        # Unsubscribe from :pg groups before returning (belt-and-suspenders;
        # :pg auto-removes dead processes, but explicit leave is cleaner)
        Enum.each(room_ids, fn room_id ->
          :pg.leave("room:#{room_id}", self())
        end)

        wait_result
      end

    {new_since_token, newest_event_id} = generate_delta_token(user_id, result_rooms, last_event_id)
    :ok = pg_store_module().persist_since_token(user_id, new_since_token, newest_event_id)

    %Core.GetSyncDeltaResponse{
      since_token: new_since_token,
      rooms: result_rooms,
      fallback_to_initial: false
    }
  end

  # Fetch events since last_event_id for each room, build SyncRoom protos.
  # last_event_id is the string event_id — the DB module resolves it to a timestamp.
  defp fetch_delta_rooms(room_ids, last_event_id) do
    Enum.flat_map(room_ids, fn room_id ->
      case messages_db_module().fetch_events_since(room_id, last_event_id, 20) do
        {:ok, []} ->
          []

        {:ok, events} ->
          try do
            state = room_registry_module().get_state(room_id)
            state_events = build_state_events(state)
            timeline_events = Enum.map(events, &event_map_to_proto/1)

            [
              %Core.SyncRoom{
                room_id: room_id,
                state_events: state_events,
                timeline_events: timeline_events,
                limited: length(events) >= 20,
                prev_batch: ""
              }
            ]
          catch
            :exit, {:noproc, _} -> []
          end

        {:error, _} ->
          []
      end
    end)
  end

  # Flush any stale :sync_long_poll_timeout message left in the mailbox
  # after canceling the timer. This prevents leaking messages into the next
  # receive block.
  defp flush_long_poll_timeout do
    receive do
      :sync_long_poll_timeout -> :ok
    after
      0 -> :ok
    end
  end

  # Generate a new since_token and find the newest event_id from the delta rooms.
  defp generate_delta_token(user_id, rooms, fallback_event_id) do
    newest_event_id =
      Enum.flat_map(rooms, fn room -> room.timeline_events end)
      |> Enum.max_by(fn ev -> ev.origin_ts end, fn -> nil end)
      |> case do
        nil -> fallback_event_id
        ev -> ev.event_id
      end

    new_token =
      Base.encode64(
        "#{user_id}:#{newest_event_id || ""}:#{System.monotonic_time()}",
        padding: false
      )

    {new_token, newest_event_id}
  end

  # Build state events for a room: one m.room.member per active member + one m.room.power_levels.
  defp build_state_events(state) do
    member_events =
      state.members
      |> MapSet.to_list()
      |> Enum.map(fn uid ->
        %Core.SyncRoomStateEvent{
          type: "m.room.member",
          state_key: uid,
          content: Jason.encode!(%{"membership" => "join", "displayname" => ""}),
          sender: uid
        }
      end)

    pl_event = %Core.SyncRoomStateEvent{
      type: "m.room.power_levels",
      state_key: "",
      content: Jason.encode!(state.power_levels),
      sender: ""
    }

    member_events ++ [pl_event]
  end

  # ─── Private helpers ─────────────────────────────────────────────────────────

  # Subscribe this process to all active room :pg groups.
  # Rooms that are started after this call will not be subscribed automatically.
  # For MVP, subscribing at stream-open is sufficient.
  defp subscribe_to_all_rooms do
    # Horde.Registry.select/2 returns [{key, pid, value}] triples.
    # We select only the keys (room_id strings).
    room_ids =
      try do
        Horde.Registry.select(Nebu.Room.Registry, [{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
      rescue
        _ -> []
      catch
        _, _ -> []
      end

    Enum.each(room_ids, fn room_id ->
      :pg.join("room:#{room_id}", self())
    end)
  end

  # Explicitly leave all room:* groups this process has joined.
  # :pg cleans up dead processes automatically, but this provides immediate
  # cleanup when the process exits cleanly via trap_exit.
  defp leave_all_room_groups do
    all_groups =
      try do
        :pg.which_groups()
      rescue
        _ -> []
      catch
        _, _ -> []
      end

    for group <- all_groups,
        is_binary(group),
        String.starts_with?(group, "room:") do
      :pg.leave(group, self())
    end
  end

  # Convert a signed event map (string keys) to a %Core.Event{} protobuf struct.
  defp map_to_proto_event(event_map) do
    content_json = Jason.encode!(Map.get(event_map, "content", %{}))

    %Core.Event{
      event_id: Map.get(event_map, "event_id", ""),
      room_id: Map.get(event_map, "room_id", ""),
      sender_id: Map.get(event_map, "sender", ""),
      event_type: Map.get(event_map, "type", ""),
      content: content_json,
      origin_ts: Map.get(event_map, "origin_server_ts", 0),
      server_ts: System.system_time(:millisecond)
    }
  end

  # Send a reply on the stream.
  # In test mode, the stream may have a :grpc_reply_interceptor key set to a
  # test PID. When present, we forward the reply directly to that PID instead
  # of calling the real GRPC.Server.send_reply/2 (which requires a live gRPC
  # connection not available in unit tests).
  defp do_send_reply(%{grpc_reply_interceptor: interceptor_pid} = _stream, event)
       when is_pid(interceptor_pid) do
    send(interceptor_pid, {:grpc_reply, event})
  end

  defp do_send_reply(stream, event) do
    GRPC.Server.send_reply(stream, event)
  end
end
