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

  def get_messages(_request, _stream) do
    %Core.GetMessagesResponse{}
  end

  def set_presence(_request, _stream) do
    %Core.SetPresenceResponse{}
  end

  def set_typing(_request, _stream) do
    %Core.SetTypingResponse{}
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
      power_levels_json: "{}",
      room_name: ""
    }
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
