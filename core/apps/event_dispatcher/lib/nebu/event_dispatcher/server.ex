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

  # ─── Configurable AuditWriter module for testability ─────────────────────────
  # Override via Application.put_env(:compliance, :audit_writer, FakeAuditWriter) in tests.
  defp audit_writer_module do
    Application.get_env(:compliance, :audit_writer, Compliance.AuditWriter)
  end

  # ─── Configurable SessionSupervisor module for testability ─────────────────
  # Override via Application.put_env(:event_dispatcher, :session_supervisor_module, FakeModule) in tests.
  defp session_supervisor_module do
    Application.get_env(:event_dispatcher, :session_supervisor_module, Nebu.Session.SessionSupervisor)
  end

  # ─── Configurable AdminDB module for testability ─────────────────────────────
  # Override via Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB) in tests.
  defp admin_db_module do
    Application.get_env(:event_dispatcher, :admin_db_module, Nebu.Admin.DB)
  end

  def send_event(request, _stream) do
    room_id = request.room_id
    sender_id = request.sender_id
    event_type = request.event_type
    txn_id = request.txn_id
    # Story 9-7: extract state_key (field 7 in SendEventRequest).
    # SEC Gate 1 fix: use is_state_event (field 8) to decide the power level check.
    # When is_state_event=true, Room.Server uses :change_state (state_default=50).
    # When is_state_event=false (default), Room.Server uses :send_event (events_default=0).
    # state_key is passed as the Matrix state_key string (may be "" for m.room.name).
    # Pass nil when NOT a state event so Room.Server applies the correct power check.
    is_state_event = Map.get(request, :is_state_event, false)
    raw_state_key = Map.get(request, :state_key, "")
    state_key = if is_state_event, do: raw_state_key, else: nil

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
        # Story 9-7: pass state_key so state events are persisted with their key.
        # SEC Gate 1: state_key=nil triggers :send_event check; state_key=string triggers :change_state.
        case Nebu.Room.Server.send_event(room_id, sender_id, event_type, content, txn_id, state_key) do
          {:ok, event_id} ->
            %Core.SendEventResponse{event_id: event_id}

          {:error, :forbidden} ->
            raise GRPC.RPCError,
              status: GRPC.Status.permission_denied(),
              message: "#{sender_id} lacks power level to send events in #{room_id}"

          {:error, :room_archived} ->
            raise GRPC.RPCError,
              status: GRPC.Status.failed_precondition(),
              message: "M_ROOM_ARCHIVED: room is archived"

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
        # Persist m.room.create FIRST — Matrix spec §8.5.1 requires it to be the
        # first event in the room timeline (before m.room.member and m.room.power_levels).
        # Emitting after Server.join would put m.room.member before m.room.create in
        # the events table, causing Element to log "No membership changes detected".
        create_event_map = %{
          "room_id"          => room_id,
          "type"             => "m.room.create",
          "state_key"        => "",
          "sender"           => creator_id,
          "content"          => %{"creator" => creator_id, "room_version" => "10"},
          "origin_server_ts" => Nebu.DB.Helpers.now_ms()
        }
        create_event_id = Nebu.EventId.generate(create_event_map)
        create_event_with_id = Map.put(create_event_map, "event_id", create_event_id)
        {_pub, priv_create} = :persistent_term.get(:nebu_signing_key)
        create_event_json = Nebu.CanonicalJson.encode!(create_event_map)
        create_sig = :crypto.sign(:eddsa, :none, create_event_json, [priv_create, :ed25519])
        create_signed = Map.put(create_event_with_id, "signatures", %{"nebu" => Base.encode64(create_sig)})
        case messages_db_module().insert_event(create_signed) do
          :ok ->
            Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, create_signed}))
          {:error, reason} ->
            Logger.warning("create_room: failed to write m.room.create for #{room_id}: #{inspect(reason)}")
        end

        # Join creator AFTER m.room.create — emit_membership_event writes m.room.member
        # as the second event in the timeline (spec §8.5.1 order: create → member → pl).
        :ok = Nebu.Room.Server.join(room_id, creator_id)

        # Wake the creator's long-polling sync task immediately (mirrors join_room/2 pattern).
        # Without this broadcast, a sync task that started BEFORE createRoom returns will not
        # be subscribed to room:#{room_id}. Any leave/send event before the sync timeout
        # (30 s) would be missed, causing GAP-FORGET and similar issues.
        :pg.get_local_members("user:#{creator_id}")
        |> Enum.each(&send(&1, {:new_join, room_id}))

        default_pl = Nebu.Room.Server.default_power_levels()
        creator_pl = put_in(default_pl, ["users", creator_id], 100)
        :ok = Nebu.Room.Server.set_power_levels(room_id, creator_id, creator_pl)

        # Emit m.room.name state event if a name was provided
        name = request.name
        if name != nil and name != "" do
          event_map = %{
            "room_id"          => room_id,
            "type"             => "m.room.name",
            "state_key"        => "",
            "sender"           => creator_id,
            "content"          => %{"name" => name},
            "origin_server_ts" => Nebu.DB.Helpers.now_ms()
          }
          event_id = Nebu.EventId.generate(event_map)
          event_with_id = Map.put(event_map, "event_id", event_id)
          {_pub, priv} = :persistent_term.get(:nebu_signing_key)
          event_json = Nebu.CanonicalJson.encode!(event_map)
          signature = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
          sig_b64 = Base.encode64(signature)
          signed = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})
          case messages_db_module().insert_event(signed) do
            :ok ->
              Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, signed}))
            {:error, reason} ->
              Logger.warning("create_room: failed to write m.room.name for #{room_id}: #{inspect(reason)}")
          end
        end

        audit_writer_module().log(
          creator_id,
          "room_created",
          "room",
          room_id,
          %{"is_direct" => request.is_direct},
          "success"
        )

        %Core.CreateRoomResponse{room_id: room_id}

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "Failed to start room: #{inspect(reason)}"
    end
  end

  # ─── WriteAuditLog ─────────────────────────────────────────────────────────────
  # Called by the Go gateway for admin-layer events (login, logout, bootstrap).
  # Delegates to the configurable audit_writer_module (Compliance.AuditWriter in prod).
  # Returns ok: false when AuditWriter returns {:error, _} — Go decides whether to warn.
  def write_audit_log(request, _stream) do
    metadata =
      case Jason.decode(request.metadata_json) do
        {:ok, m} when is_map(m) -> m
        _ -> %{}
      end

    error_detail =
      if request.error_detail == "", do: nil, else: request.error_detail

    case audit_writer_module().log(
           request.actor_user_id,
           request.action,
           request.target_type,
           request.target_id,
           metadata,
           request.outcome,
           error_detail
         ) do
      :ok ->
        %Core.WriteAuditLogResponse{ok: true}

      {:error, _} ->
        %Core.WriteAuditLogResponse{ok: false}
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
            # Wake user's long-polling sync task immediately (mirrors invite_user/2 pattern).
            # Without this, the sync task sleeps 30 s after a public-room join (GAP-JOIN-PUBLIC).
            :pg.get_local_members("user:#{user_id}")
            |> Enum.each(&send(&1, {:new_join, room_id}))
            audit_writer_module().log(user_id, "room_joined", "room", room_id, %{}, "success")
            %Core.JoinRoomResponse{room_id: room_id}

          {:error, :already_member} ->
            # Matrix spec: idempotent — joining an already-joined room is success.
            # No audit log for idempotent joins (AC9 scope decision).
            %Core.JoinRoomResponse{room_id: room_id}

          {:error, :room_full} ->
            # Story 6.8: room has reached max_members capacity.
            # Go gateway maps codes.ResourceExhausted → 403 M_ROOM_FULL.
            raise GRPC.RPCError,
              status: GRPC.Status.resource_exhausted(),
              message: "room is full"

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "join failed: #{inspect(reason)}"
        end
    end
  end

  # ─── UpdateRoomSettings — Story 6.8: Admin PATCH /admin/rooms/{roomId} ─────────
  #
  # Called by the Go gateway after a successful DB update of room settings.
  # Delegates to Nebu.Room.Server.update_settings/2 (best-effort fire-and-forget cast).
  # Returns UpdateRoomSettingsResponse{ok: true} always — failure to find the running
  # GenServer is non-fatal (room may not be started, or settings will be loaded on next init).

  def update_room_settings(%Core.UpdateRoomSettingsRequest{} = req, _stream) do
    room_id = req.room_id
    max_members = req.max_members

    # Best-effort: call update_settings only if the room GenServer is running.
    # If the room is not started, the new max_members will be loaded from DB on next init.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, _pid} ->
        Nebu.Room.Server.update_settings(room_id, %{max_members: max_members})

      {:error, :not_found} ->
        # Room GenServer not running — settings will be applied on next init/1.
        :ok
    end

    %Core.UpdateRoomSettingsResponse{ok: true}
  end

  def leave_room(request, _stream) do
    room_id = request.room_id
    user_id = request.user_id

    # If the room GenServer is not running (e.g. after stack restart or the user
    # was only invited and the room was never started for them), we still honour
    # the leave / decline-invite semantics by operating on the DB directly.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        # Room GenServer not running — reject the pending invitation directly in DB.
        db_module_invite().reject_invitation(room_id, user_id)
        # Emit a proper m.room.member leave event so fetch_delta_rooms can find it
        # even if the :pg broadcast arrives between two sync cycles (race-proof).
        emit_decline_event(room_id, user_id)
        %Core.LeaveRoomResponse{room_id: room_id}

      {:ok, _pid} ->
        case Nebu.Room.Server.leave(room_id, user_id) do
          :ok ->
            # Wake the user's long-polling sync task immediately so rooms.leave
            # arrives within the long-poll window instead of waiting up to 30 s.
            # The sync task may not be subscribed to room:#{room_id} if it started
            # before the user was in this room (e.g. a room the user created and
            # immediately left). Broadcasting to user:#{user_id} guarantees wakeup
            # regardless of room subscriptions. The Go side's buildLeaveRooms does
            # the actual DB query — the sync task just needs to wake up and return [].
            members = :pg.get_local_members("user:#{user_id}")
            require Logger
            Logger.debug("[leave_room] broadcasting {:new_leave, #{room_id}} to #{length(members)} sync tasks for #{user_id}")
            Enum.each(members, &send(&1, {:new_leave, room_id}))
            %Core.LeaveRoomResponse{room_id: room_id}

          {:error, :not_member} ->
            # User has a pending invite but hasn't joined — treat as "decline invite".
            db_module_invite().reject_invitation(room_id, user_id)
            emit_decline_event(room_id, user_id)
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
            # Wake the invitee's long-polling sync task immediately (Bug 4-29f fix).
            # The invitee subscribed to "user:#{invitee}" in do_incremental_sync before
            # entering the receive loop. Without this broadcast the long-poll sleeps 30 s.
            :pg.get_local_members("user:#{invitee}")
            |> Enum.each(&send(&1, {:new_invite, room_id}))

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
      server_ts: System.system_time(:millisecond),
      state_key: Map.get(event, "state_key") || ""
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
        # Upsert profile row on every successful login so GET /profile/{userId} returns
        # 200 for OIDC-provisioned users (Bug 2a fix). Non-fatal: profile write failure
        # must not block login — the provisioned user session is already valid.
        display_name_for_profile =
          if request.display_name == "", do: nil, else: request.display_name

        case profile_db_module().upsert_profile(user_id, display_name_for_profile, nil) do
          :ok -> :ok
          {:error, reason} ->
            Logger.warning("validate_token: profile upsert failed for #{user_id}: #{inspect(reason)}")
        end

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

  # ─── DeleteUserKeys — Story 5.7: DSGVO Key-Deletion ──────────────────────────
  #
  # Delegates to Compliance.UserDeletion.delete_user_keys/3.
  # Maps Elixir result tuples to gRPC status codes (consumed by Go gateway for HTTP mapping):
  #   {:ok, %{keys_deleted_at: ms}} → DeleteUserKeysResponse{status: "keys_deleted", keys_deleted_at: ms}
  #   {:error, :user_not_found}     → GRPC.Status.not_found()
  #   {:error, :conflict}           → GRPC.Status.already_exists()
  #   {:error, reason}              → GRPC.Status.internal()
  #
  # The failure-invariant audit ("user_keys_deletion_attempted") is handled inside
  # Compliance.UserDeletion — the gRPC handler does NOT need to emit it.

  def delete_user_keys(%Core.DeleteUserKeysRequest{} = req, _stream) do
    case Compliance.UserDeletion.delete_user_keys(req.admin_user_id, req.target_user_id, req.reason) do
      {:ok, %{keys_deleted_at: keys_deleted_at_ms}} ->
        %Core.DeleteUserKeysResponse{status: "keys_deleted", keys_deleted_at: keys_deleted_at_ms}

      {:error, :user_not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "user not found"

      {:error, :conflict} ->
        raise GRPC.RPCError,
          status: GRPC.Status.already_exists(),
          message: "deletion already in progress or completed"

      {:error, reason} ->
        Logger.error("delete_user_keys failed",
          target_user_id: req.target_user_id,
          reason: inspect(reason)
        )

        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "deletion failed: #{inspect(reason)}"
    end
  end

  # ─── InvalidateUserSessions — Story 6.5: Admin deactivation revokes all sessions ─
  # Story 9-22 (AC4): Per-device logout when device_id is set.
  #
  # Called by the Go gateway:
  #   - Admin deactivation: device_id="" → destroy_session/1 deletes all sync_tokens +
  #     sessions rows for the user and evicts from ETS.
  #   - Matrix POST /logout: device_id="<id>" → destroy_session/2 deletes only the
  #     (user_id, device_id) sync_tokens + sessions rows in a single DB transaction.
  #     ETS is NOT evicted (other devices may still be active).
  #
  # Returns %Core.InvalidateUserSessionsResponse{ok: true} on success.
  # Raises GRPC.RPCError with status=internal on failure (Go logs as warning, does not block).

  def invalidate_user_sessions(%Core.InvalidateUserSessionsRequest{} = req, _stream) do
    # AC4 (Story 9-22): per-device cleanup when device_id is present.
    result =
      if req.device_id != "" do
        session_supervisor_module().destroy_session(req.user_id, req.device_id)
      else
        session_supervisor_module().destroy_session(req.user_id)
      end

    case result do
      :ok ->
        %Core.InvalidateUserSessionsResponse{ok: true}

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "session invalidation failed: #{inspect(reason)}"
    end
  end

  # ─── InvalidateAllAdminSessions — Story 6.10: Admin config OIDC change ────────
  #
  # Called by the Go gateway when OIDC issuer/client_id/client_secret changes in
  # PATCH /admin/config. Forces all active sessions to re-authenticate.
  #
  # Flow:
  #   1. List all active user_ids from ETS via Nebu.Session.EtsStore.list_user_ids/0.
  #   2. Call session_supervisor_module().destroy_session/1 for each user (best-effort).
  #   3. Always return %Core.InvalidateAllAdminSessionsResponse{ok: true} — no-op if ETS empty.
  #
  # Never raises — individual session failures are silently ignored (best-effort).
  # Returns ok: true even if ETS is empty (idempotent no-op).

  def invalidate_all_admin_sessions(%Core.InvalidateAllAdminSessionsRequest{} = _req, _stream) do
    user_ids = Nebu.Session.EtsStore.list_user_ids()

    Enum.each(user_ids, fn user_id ->
      case session_supervisor_module().destroy_session(user_id) do
        :ok ->
          :ok

        {:error, reason} ->
          Logger.warning("InvalidateAllAdminSessions: failed to destroy session for #{user_id}: #{inspect(reason)}")
      end
    end)

    %Core.InvalidateAllAdminSessionsResponse{ok: true}
  end

  def get_pending_events(_request, _stream) do
    %Core.GetPendingEventsResponse{}
  end

  # ─── GetMetrics — Real implementation (Story 9.1, Task 8) ───────────────────
  #
  # Replaces the empty stub with real counts from ETS and Horde.
  #   - active_sessions: counted from Nebu.Session.EtsStore.list_user_ids/0
  #     (already used in invalidate_all_admin_sessions/2).
  #   - room_count: counted from Horde.Registry.select/2
  #     (already used in subscribe_to_all_rooms/0).
  #   - msg_per_sec: kept at 0.0 for MVP (rolling window not yet implemented).

  def get_metrics(_request, _stream) do
    active_sessions = Nebu.Session.EtsStore.list_user_ids() |> length()

    room_count =
      try do
        Horde.Registry.select(Nebu.Room.Registry, [{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
        |> length()
      rescue
        _ -> 0
      catch
        _, _ -> 0
      end

    %Core.GetMetricsResponse{
      active_sessions: active_sessions,
      room_count: room_count,
      msg_per_sec: 0.0
    }
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

  def get_room_state(request, stream) do
    room_id = request.room_id
    event_type = Map.get(request, :event_type, "")
    state_key = Map.get(request, :state_key, "")
    mod = room_registry_module()

    {caller_id, system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    state =
      try do
        mod.get_state(room_id)
      catch
        :exit, {:noproc, _} ->
          raise GRPC.RPCError,
            status: GRPC.Status.not_found(),
            message: "room not found: #{room_id}"
      end

    # System-role callers (internal gateway fanout) skip the membership check.
    # User-role callers must be room members (Story 7-19 IDOR fix preserved).
    unless system_role == "system" or MapSet.member?(state.members, caller_id) do
      raise GRPC.RPCError,
        status: GRPC.Status.permission_denied(),
        message: "#{caller_id} is not a member of #{room_id}"
    end

    # Build the full state_events list for this room.
    all_state_events = build_state_events(state, room_id)

    # Apply optional event_type / state_key filter (Story 7-19, AC2 + AC3 + AC6).
    # When event_type is empty, return all events (backward compat: /members caller).
    # When event_type is set, filter to matching events; raise not_found if none match.
    filtered_events =
      if event_type == "" do
        all_state_events
      else
        matched =
          Enum.filter(all_state_events, fn ev ->
            ev.type == event_type && ev.state_key == state_key
          end)

        if matched == [] do
          raise GRPC.RPCError,
            status: GRPC.Status.not_found(),
            message: "no state event for type=#{event_type} state_key=#{state_key} in room #{room_id}"
        end

        matched
      end

    %Core.GetRoomStateResponse{
      members: MapSet.to_list(state.members),
      power_levels_json: Jason.encode!(state.power_levels),
      room_name: "",
      state_events: filtered_events
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
          state_events = build_state_events(state, room_id)

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
              state_events: dedup_member_state_events(state_events, timeline_events),
              timeline_events: timeline_events,
              limited: limited,
              prev_batch: prev_batch
            }
          ]
        catch
          # Room GenServer not running (e.g. after core restart). Start it on demand —
          # Room.init/1 reloads all state from DB, so this is always safe.
          :exit, {:noproc, _} ->
            _ = Nebu.Room.RoomSupervisor.start_room(room_id)
            try do
              state      = room_registry_module().get_state(room_id)
              state_evs  = build_state_events(state, room_id)
              {:ok, evs, _next_batch, prev_b} =
                messages_db_module().fetch_events(room_id, "b", 20, "")
              tl_evs = evs |> Enum.reverse() |> Enum.map(&event_map_to_proto/1)
              [%Core.SyncRoom{
                room_id:        room_id,
                state_events:   dedup_member_state_events(state_evs, tl_evs),
                timeline_events: tl_evs,
                limited:        length(evs) >= 20,
                prev_batch:     prev_b
              }]
            catch
              :exit, _ -> []
            end
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
    # proto3 string fields are never nil — no need for `|| ""`
    device_id = request.device_id
    # client_since_token is validated against the stored token after lookup (AC2, Story 9-22).
    client_since_token = request.since_token
    timeout_ms = request.timeout_ms |> max(0) |> min(30_000)

    # Step 3: Resolve last_event_id from the since_token.
    # Use per-device lookup when device_id is present (Story 9-22).
    lookup_result =
      if device_id != "" do
        pg_store_module().get_since_token(user_id, device_id)
      else
        pg_store_module().get_since_token(user_id)
      end

    case lookup_result do
      {:error, :not_found} ->
        # AC3: No stored token for this (user_id, device_id) → full initial sync.
        # This handles first sync after re-login, unknown device_id, or missing row.
        initial_req = %Core.GetInitialSyncRequest{user_id: user_id}
        initial_resp = get_initial_sync(initial_req, %{http_request_headers: %{}})

        # MAJOR-A fix: persist the freshly-minted token to the per-device row so
        # the next request from this device resolves the correct checkpoint and
        # does not trigger a perpetual full-sync (AC1/AC2 recovery).
        if device_id != "" do
          :ok = pg_store_module().persist_since_token(
            user_id, device_id, initial_resp.since_token, nil
          )
        end

        %Core.GetSyncDeltaResponse{
          since_token: initial_resp.since_token,
          rooms: initial_resp.rooms,
          fallback_to_initial: true
        }

      {:ok, %{since_token: stored_token, last_event_id: last_event_id}} ->
        # AC2: Validate that the client echoes back the token we issued.
        # A mismatch means a stale or replayed token → fall back to full initial sync.
        if client_since_token != stored_token do
          initial_req = %Core.GetInitialSyncRequest{user_id: user_id}
          initial_resp = get_initial_sync(initial_req, %{http_request_headers: %{}})

          # MAJOR-A fix: persist the freshly-minted token to the per-device row so
          # the next request from this device resolves the correct checkpoint and
          # does not trigger a perpetual full-sync (AC2 recovery path).
          if device_id != "" do
            :ok = pg_store_module().persist_since_token(
              user_id, device_id, initial_resp.since_token, nil
            )
          end

          %Core.GetSyncDeltaResponse{
            since_token: initial_resp.since_token,
            rooms: initial_resp.rooms,
            fallback_to_initial: true
          }
        else
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
            do_incremental_sync(user_id, device_id, last_event_id, timeout_ms)
          end)

          Task.await(task, task_timeout)
        end
    end
  end

  # Runs the incremental sync logic in a separate (Task) process so that
  # :pg group subscriptions are owned by a process that exits when done.
  # :pg auto-removes dead processes from groups.
  defp do_incremental_sync(user_id, device_id, last_event_id, timeout_ms) do
    # Step 4: Get user's rooms
    room_ids =
      case rooms_db_module().get_rooms_for_user(user_id) do
        {:ok, ids} -> ids
        {:error, _} -> []
      end

    # Also subscribe to rooms where user has a pending invite — so that
    # invite-decline broadcasts wake up this sync task immediately.
    invited_room_ids =
      case db_module_invite().get_pending_invite_rooms_for_user(user_id) do
        {:ok, ids} -> ids
        {:error, _} -> []
      end

    # Also include rooms where the user has ALREADY declined an invite.
    # Race condition fix: if the decline happens just before this sync task starts,
    # get_pending_invite_rooms_for_user returns [] (rejected_at IS NOT NULL).
    # Without declined_room_ids, fetch_delta_rooms never checks the declined room
    # and the :pg broadcast is missed → 30 s long-poll → sync returns too late.
    # fetch_events_since returns [] for old declines (events predate since_ts),
    # so this only triggers an immediate return for genuinely new decline events.
    declined_room_ids =
      case db_module_invite().get_declined_invite_rooms_for_user(user_id) do
        {:ok, ids} -> ids
        {:error, _} -> []
      end

    # GAP-LEAVE-UI: include recently-left rooms so fetch_delta_rooms finds the leave event
    # in the initial DB check. Closes the race window where {new_leave} fires before the
    # sync task subscribes to :pg groups, causing a 30 s long-poll delay.
    left_room_ids =
      case rooms_db_module().get_recently_left_rooms_for_user(user_id) do
        {:ok, ids} -> ids
        {:error, _} -> []
      end

    room_ids = room_ids ++ invited_room_ids ++ declined_room_ids ++ left_room_ids

    # Step 5a: Subscribe to user-level :pg group BEFORE DB check.
    # Receives {:new_invite, room_id} when invite_user/2 sends an invitation — this
    # wakes the long-poll immediately instead of sleeping the full 30 s (Bug 4-29f fix).
    :pg.join("user:#{user_id}", self())

    # Step 5b: Subscribe to room-level :pg groups BEFORE DB check (race prevention).
    Enum.each(room_ids, fn room_id ->
      :pg.join("room:#{room_id}", self())
    end)

    # Step 6: Check DB for pending events per room.
    # Pass last_event_id (string) directly — the DB module resolves the ts internally.
    delta_rooms = fetch_delta_rooms(room_ids, last_event_id)

    leave_all_groups = fn ->
      :pg.leave("user:#{user_id}", self())
      Enum.each(room_ids, fn room_id -> :pg.leave("room:#{room_id}", self()) end)
    end

    result_rooms =
      if delta_rooms != [] do
        # Step 7: Events found — leave :pg groups and return immediately
        leave_all_groups.()
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

              {:new_invite, _room_id} ->
                # New invitation for this user — cancel timer and return empty delta.
                # The Go gateway's buildInviteRooms queries the DB for invite data,
                # so returning [] here is sufficient to unblock the client.
                Process.cancel_timer(timer_ref)
                flush_long_poll_timeout()
                []

              {:new_join, new_room_id} ->
                # User joined a public room — subscribe to it and re-query (mirrors {:new_invite} pattern).
                # GAP-JOIN-PUBLIC: without this, join_room broadcasts are missed and the long-poll sleeps 30 s.
                Process.cancel_timer(timer_ref)
                flush_long_poll_timeout()
                :pg.join("room:#{new_room_id}", self())
                fetch_delta_rooms(Enum.uniq([new_room_id | room_ids]), last_event_id)

              {:new_leave, recv_room_id} ->
                # User left a room — wake up immediately so Go's buildLeaveRooms can find it via DB query.
                # GAP-LEAVE-UI / GAP-FORGET: without this, leave_room broadcasts to room:#{room_id} only;
                # if the sync task missed that subscription (race), the leave is never delivered within
                # the long-poll window. Returning [] is sufficient — Go queries DB for rooms.leave.
                require Logger
                Logger.debug("[do_incremental_sync] {:new_leave} received for room #{recv_room_id}, waking sync task for #{user_id}")
                Process.cancel_timer(timer_ref)
                flush_long_poll_timeout()
                []

              :sync_long_poll_timeout ->
                []
            end
          else
            []
          end

        # Unsubscribe from all :pg groups before returning (belt-and-suspenders;
        # :pg auto-removes dead processes, but explicit leave is cleaner)
        leave_all_groups.()

        wait_result
      end

    {new_since_token, newest_event_id} = generate_delta_token(user_id, result_rooms, last_event_id)

    # Persist per-device when device_id is set, fallback to legacy /1 key otherwise (Story 9-22).
    if device_id != "" do
      :ok = pg_store_module().persist_since_token(user_id, device_id, new_since_token, newest_event_id)
    else
      :ok = pg_store_module().persist_since_token(user_id, new_since_token, newest_event_id)
    end

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
            timeline_events = Enum.map(events, &event_map_to_proto/1)
            limited = length(events) >= 20
            # When limited, the oldest event's event_id is the backward-pagination
            # cursor (Spec §6.3.3). Client passes this to GET /messages?from=<token>&dir=b.
            prev_batch = if limited, do: List.first(events)["event_id"] || "", else: ""

            # §6.3.3: state.events = room state BEFORE the timeline window. Any
            # {type, state_key} already delivered in the timeline must be excluded
            # from state so the client computes the change (not a no-op delta).
            # We detect state events in the raw batch via Map.has_key?("state_key"),
            # which correctly identifies state events with empty state_key (power_levels,
            # name, join_rules) — proto conversion cannot distinguish them from messages.
            timeline_state_keys =
              events
              |> Enum.filter(fn ev -> Map.has_key?(ev, "state_key") end)
              |> MapSet.new(fn ev -> {ev["event_type"] || "", ev["state_key"] || ""} end)

            state_events =
              build_state_events(state, room_id)
              |> Enum.reject(fn ev ->
                MapSet.member?(timeline_state_keys, {ev.type, ev.state_key})
              end)

            [
              %Core.SyncRoom{
                room_id: room_id,
                state_events: dedup_member_state_events(state_events, timeline_events),
                timeline_events: timeline_events,
                limited: limited,
                prev_batch: prev_batch
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

  # Removes m.room.member entries from state_events when the same user already has
  # a m.room.member event in timeline_events. This prevents Element Web from logging
  # "No membership changes detected" — without deduplication, matrix-js-sdk sees the
  # join in state (prev=join) and again in the timeline (new=join), computes no change,
  # and silently skips processing the membership event. With deduplication, the member
  # event is ONLY in the timeline, so the sdk correctly detects: none→join.
  #
  # State events for members NOT in the timeline are kept intact (e.g. for rooms with
  # limited=true where historic joins are outside the timeline window).
  defp dedup_member_state_events(state_events, timeline_events) do
    member_state_keys =
      timeline_events
      |> Enum.filter(fn ev -> ev.event_type == "m.room.member" end)
      |> MapSet.new(fn ev -> ev.state_key end)

    if MapSet.size(member_state_keys) == 0 do
      state_events
    else
      Enum.reject(state_events, fn ev ->
        ev.type == "m.room.member" && MapSet.member?(member_state_keys, ev.state_key)
      end)
    end
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

  # Build state events for a room: m.room.create + one m.room.member per active member
  # + one m.room.power_levels + one m.room.name (if a name has been stored).
  defp build_state_events(state, room_id) do
    creator_id =
      case messages_db_module().get_room_creator(room_id) do
        {:ok, id} ->
          id
        _ ->
          # Fallback for rooms created before m.room.create was persisted.
          (state.power_levels || %{})
          |> Map.get("users", %{})
          |> Enum.max_by(fn {_uid, lvl} -> lvl end, fn -> {"", 0} end)
          |> elem(0)
      end

    # MAJOR-2 fix: use the persisted m.room.create content when available so that
    # upgraded rooms return the `predecessor` field rather than a synthesized fallback.
    # Fall back to synthesized content for rooms created before create-event persistence,
    # or when the DB query fails (e.g. in unit tests without a real DB).
    create_content =
      case messages_db_module().get_room_create_event(room_id) do
        {:ok, persisted_content} when is_map(persisted_content) ->
          persisted_content
        _ ->
          %{"creator" => creator_id, "room_version" => "10"}
      end

    create_event = %Core.SyncRoomStateEvent{
      type: "m.room.create",
      state_key: "",
      content: Jason.encode!(create_content),
      sender: creator_id
    }

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

    name_events =
      case messages_db_module().get_room_name(room_id) do
        {:ok, name} ->
          [%Core.SyncRoomStateEvent{
            type: "m.room.name",
            state_key: "",
            content: Jason.encode!(%{"name" => name}),
            sender: ""
          }]
        _ -> []
      end

    # Story 9-7: load generic state events persisted via PUT /rooms/{roomId}/state/{eventType}.
    # These cover m.room.topic, m.room.join_rules, m.room.encryption, m.room.avatar, etc.
    # Excludes types already assembled above (member, power_levels, create, name).
    generic_state_events =
      case messages_db_module().get_generic_state_events(room_id) do
        {:ok, evs} ->
          Enum.map(evs, fn ev ->
            %Core.SyncRoomStateEvent{
              type: ev.type,
              state_key: ev.state_key,
              content: ev.content_json,
              sender: ev.sender
            }
          end)
        _ -> []
      end

    [create_event] ++ member_events ++ [pl_event] ++ name_events ++ generic_state_events
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
      server_ts: System.system_time(:millisecond),
      state_key: Map.get(event_map, "state_key", "")
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

  # ─── Story 7-22: Room Moderation ─────────────────────────────────────────────

  @doc """
  Handles POST /_matrix/client/v3/rooms/{roomId}/kick.

  Kicks `target_id` from the room:
  1. Verifies room exists.
  2. Verifies `caller_id` is a room member.
  3. Checks caller power level ≥ kick threshold (50 by default).
  4. Calls Room GenServer leave/2 to remove target from members.
  5. Returns empty KickUserResponse on success.
  """
  def kick_user(request, stream) do
    room_id   = request.room_id
    {caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
    target_id = request.target_id

    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = Nebu.Room.Server.get_state(room_id)

        unless MapSet.member?(state.members, caller_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "you are not a member of this room"
        end

        unless Nebu.Room.PowerLevels.can?(state.power_levels, caller_id, :kick) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "insufficient power level for kick"
        end

        case Nebu.Room.Server.remove_member(room_id, target_id) do
          :ok ->
            # Emit kick as m.room.member leave with sender = caller_id (not target_id).
            reason = if request.reason != "", do: request.reason, else: nil
            content = %{"membership" => "leave"} |> then(fn c ->
              if reason, do: Map.put(c, "reason", reason), else: c
            end)
            kick_event = %{
              "room_id"          => room_id,
              "type"             => "m.room.member",
              "state_key"        => target_id,
              "sender"           => caller_id,
              "content"          => content,
              "origin_server_ts" => Nebu.DB.Helpers.now_ms()
            }
            event_id      = Nebu.EventId.generate(kick_event)
            event_with_id = Map.put(kick_event, "event_id", event_id)
            {_pub, priv}  = :persistent_term.get(:nebu_signing_key)
            event_json    = Nebu.CanonicalJson.encode!(kick_event)
            signature     = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
            sig_b64       = Base.encode64(signature)
            signed        = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})
            case messages_db_module().insert_event(signed) do
              :ok ->
                Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, signed}))
                %Core.KickUserResponse{}
              {:error, reason} ->
                raise GRPC.RPCError,
                  status: GRPC.Status.internal(),
                  message: "kick failed: #{inspect(reason)}"
            end

          {:error, :not_member} ->
            raise GRPC.RPCError,
              status: GRPC.Status.not_found(),
              message: "target user is not a member of this room"

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "kick failed: #{inspect(reason)}"
        end
    end
  end

  @doc """
  Handles POST /_matrix/client/v3/rooms/{roomId}/ban.

  Bans `target_id` from the room:
  1. Verifies room exists.
  2. Verifies `caller_id` is a room member.
  3. Checks caller power level ≥ ban threshold (50 by default).
  4. If target is currently joined, removes them from members first.
  5. Emits a m.room.member ban state event.
  6. Returns empty BanUserResponse on success.
  """
  def ban_user(request, stream) do
    room_id   = request.room_id
    {caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
    target_id = request.target_id

    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = Nebu.Room.Server.get_state(room_id)

        unless MapSet.member?(state.members, caller_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "you are not a member of this room"
        end

        unless Nebu.Room.PowerLevels.can?(state.power_levels, caller_id, :ban) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "insufficient power level for ban"
        end

        # If the target is currently a member, remove them without emitting a leave event
        # (the ban event below is the authoritative membership change).
        if MapSet.member?(state.members, target_id) do
          Nebu.Room.Server.remove_member(room_id, target_id)
        end

        reason = if request.reason != "", do: request.reason, else: nil
        ban_content = %{"membership" => "ban"} |> then(fn c ->
          if reason, do: Map.put(c, "reason", reason), else: c
        end)

        # Emit a m.room.member ban state event to signal the ban.
        ban_event = %{
          "room_id"          => room_id,
          "type"             => "m.room.member",
          "state_key"        => target_id,
          "sender"           => caller_id,
          "content"          => ban_content,
          "origin_server_ts" => Nebu.DB.Helpers.now_ms()
        }
        event_id       = Nebu.EventId.generate(ban_event)
        event_with_id  = Map.put(ban_event, "event_id", event_id)
        {_pub, priv}   = :persistent_term.get(:nebu_signing_key)
        event_json     = Nebu.CanonicalJson.encode!(ban_event)
        signature      = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
        sig_b64        = Base.encode64(signature)
        signed         = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})

        case messages_db_module().insert_event(signed) do
          :ok ->
            Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, signed}))
            %Core.BanUserResponse{}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "ban failed: #{inspect(reason)}"
        end
    end
  end

  @doc """
  Handles POST /_matrix/client/v3/rooms/{roomId}/unban.

  Unbans `target_id` from the room by setting their membership to leave:
  1. Verifies room exists.
  2. Verifies `caller_id` is a room member.
  3. Checks caller power level ≥ ban threshold (50 by default).
  4. Emits a m.room.member leave state event (unban = set to leave).
  5. Returns empty UnbanUserResponse on success.
  """
  def unban_user(request, stream) do
    room_id   = request.room_id
    {caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
    target_id = request.target_id

    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = Nebu.Room.Server.get_state(room_id)

        unless MapSet.member?(state.members, caller_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "you are not a member of this room"
        end

        unless Nebu.Room.PowerLevels.can?(state.power_levels, caller_id, :ban) do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "insufficient power level for unban"
        end

        # Emit m.room.member leave state event — unban = set membership to leave.
        unban_event = %{
          "room_id"          => room_id,
          "type"             => "m.room.member",
          "state_key"        => target_id,
          "sender"           => caller_id,
          "content"          => %{"membership" => "leave"},
          "origin_server_ts" => Nebu.DB.Helpers.now_ms()
        }
        event_id       = Nebu.EventId.generate(unban_event)
        event_with_id  = Map.put(unban_event, "event_id", event_id)
        {_pub, priv}   = :persistent_term.get(:nebu_signing_key)
        event_json     = Nebu.CanonicalJson.encode!(unban_event)
        signature      = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
        sig_b64        = Base.encode64(signature)
        signed         = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})

        case messages_db_module().insert_event(signed) do
          :ok ->
            Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, signed}))
            %Core.UnbanUserResponse{}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "unban failed: #{inspect(reason)}"
        end
    end
  end

  @doc """
  Handles POST /_matrix/client/v3/rooms/{roomId}/forget.

  Marks a room as excluded from future /sync for the calling user:
  1. Verifies room exists.
  2. Verifies caller is NOT currently joined (must leave first).
  3. Returns empty ForgetRoomResponse on success.

  Note: In MVP, forget is a no-op beyond the FailedPrecondition check because
  GetSyncDelta does not yet filter forgotten rooms. A follow-up story will add
  the `forgotten_rooms` column to session state and filter in GetSyncDelta.
  """
  def forget_room(request, _stream) do
    room_id = request.room_id
    user_id = request.user_id

    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:ok, _pid} ->
        state = Nebu.Room.Server.get_state(room_id)

        # Cannot forget a room while still joined.
        if MapSet.member?(state.members, user_id) do
          raise GRPC.RPCError,
            status: GRPC.Status.failed_precondition(),
            message: "user must leave the room before forgetting"
        end

        # Room is not currently joined — forget acknowledged.
        %Core.ForgetRoomResponse{}
    end
  end

  # ─── Story 6.9: ArchiveRoom + UnarchiveRoom gRPC handlers ─────────────────────
  #
  # archive_room/2:
  #   Called by the Go gateway after successfully setting rooms.status = 'archived' in DB.
  #   Best-effort: terminates the running Room GenServer via Horde so in-memory state is
  #   cleared. The DB is authoritative — even if the GenServer is not running, ok=true.
  #   On next start, Room.Server.init/1 sees status="archived" → {:stop, :normal} → no loop.
  #
  # unarchive_room/2:
  #   Called by the Go gateway after successfully setting rooms.status = 'active' in DB.
  #   Best-effort: starts the Room GenServer so it is immediately available for messages.
  #   Returns ok=true even if start_room fails (DB is authoritative; Gateway returns 200).

  # ─── Story 9.1 AC:4 — ArchiveRoom with atomic SELECT FOR UPDATE ─────────────
  #
  # IMPORTANT CONTRACT CHANGE (Story 9.1):
  # Pre-9.1: Go Gateway set rooms.status='archived' in DB, then called this RPC
  #          which only terminated the GenServer.
  # Post-9.1: Core now owns the DB write atomically (SELECT FOR UPDATE) before
  #           terminating the GenServer. The Gateway call sequence must adapt in
  #           Story 9.2 — the Gateway should no longer update DB before calling
  #           this RPC; Core is now the authoritative writer for archive operations.
  #
  # The admin_db_module().archive_room_atomic/1 call performs:
  #   1. BEGIN TRANSACTION
  #   2. SELECT status FROM rooms WHERE room_id = ? FOR UPDATE
  #   3. UPDATE rooms SET status = 'archived' WHERE room_id = ?
  #   4. COMMIT
  # Then the GenServer is terminated (best-effort).

  def archive_room(%Core.ArchiveRoomRequest{} = req, stream) do
    room_id = req.room_id
    {actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    # Step 1: Atomically update rooms.status='archived' in DB (SELECT FOR UPDATE).
    # This replaces the old pre-9.1 contract where the Go Gateway did the DB write.
    case admin_db_module().archive_room_atomic(room_id) do
      :ok ->
        :ok

      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{room_id}"

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "archive_room DB update failed: #{inspect(reason)}"
    end

    # Step 2: Terminate the running GenServer (best-effort — idempotent if not running).
    # Room.Server.child_spec uses :transient restart, so Horde will NOT restart it
    # after a terminate_child call. DB status is the authoritative archived flag.
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, pid} ->
        case Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid) do
          :ok ->
            :ok

          {:error, reason} ->
            # Race: process likely crashed between lookup and terminate. The DB
            # is already archived, so this is a soft failure — log and continue.
            Logger.warning("ArchiveRoom: terminate_child failed (likely already stopped) — #{inspect(reason)}",
              room_id: room_id
            )

            :ok
        end

      {:error, :not_found} ->
        # Already stopped — no-op (idempotent).
        :ok
    end

    # Story 9.3: Audit log for room archival (mirrors deactivate_user pattern from Story 9.2).
    audit_writer_module().log(
      actor_id,
      "room_archived",
      "room",
      room_id,
      %{},
      "success"
    )

    %Core.ArchiveRoomResponse{ok: true}
  end

  def unarchive_room(%Core.UnarchiveRoomRequest{} = req, stream) do
    room_id = req.room_id
    {actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    # Start the Room GenServer so it is immediately available.
    # Room.Server.init/1 now calls get_room_status/1 on start:
    #   → {:ok, "active"} means the room is unarchived — init proceeds normally.
    #   → {:ok, "archived"} would be a race condition (DB not yet updated) — init stops.
    # Best-effort: if start_room fails, the DB is already updated to 'active';
    # the GenServer will start on the next client request.
    case Nebu.Room.RoomSupervisor.start_room(room_id) do
      {:ok, _pid} -> :ok
      {:error, _reason} -> :ok
    end

    # Story 9.3: Audit log for room unarchival (mirrors reactivate_user pattern from Story 9.2).
    audit_writer_module().log(
      actor_id,
      "room_unarchived",
      "room",
      room_id,
      %{},
      "success"
    )

    %Core.UnarchiveRoomResponse{ok: true}
  end

  # ─── Story 9.1: Admin gRPC RPCs — User + Room Management ─────────────────────

  # ─── ListAdminUsers ──────────────────────────────────────────────────────────
  #
  # Returns paginated users from PostgreSQL for the Admin UI.
  # Email masking: returns "u***@domain" pattern (first char + *** + @domain).
  # PII: display_name_encrypted (Tier 1) is decrypted here via admin_db_module.
  # Email (Tier 2) is returned masked — never in plaintext.
  # Security: admin RPCs verified via RequireRole middleware in Go Gateway (HTTP layer).

  def list_admin_users(%Core.ListAdminUsersRequest{} = req, _stream) do
    limit = if req.limit > 0, do: min(req.limit, 100), else: 20
    cursor = req.cursor || ""
    search = req.search || ""

    {users, next_cursor} = admin_db_module().list_users(limit, cursor, search)

    proto_users =
      Enum.map(users, fn user ->
        %Core.AdminUserProto{
          user_id: user.user_id,
          display_name: decrypt_display_name(user),
          email_masked: mask_email(user),
          is_active: user.is_active,
          system_role: user.system_role || "user",
          created_at: user.created_at || 0
        }
      end)

    %Core.ListAdminUsersResponse{
      users: proto_users,
      total: length(proto_users),
      next_cursor: next_cursor
    }
  end

  # ─── GetAdminUser ────────────────────────────────────────────────────────────

  def get_admin_user(%Core.GetAdminUserRequest{} = req, _stream) do
    case admin_db_module().get_user(req.user_id) do
      {:ok, user} ->
        %Core.GetAdminUserResponse{
          user: %Core.AdminUserProto{
            user_id: user.user_id,
            display_name: decrypt_display_name(user),
            email_masked: mask_email(user),
            is_active: user.is_active,
            system_role: user.system_role || "user",
            created_at: user.created_at || 0
          }
        }

      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "user not found: #{req.user_id}"

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "get_admin_user failed: #{inspect(reason)}"
    end
  end

  # ─── DeactivateUser ──────────────────────────────────────────────────────────
  #
  # Sets is_active=false in DB, then calls destroy_session/1 AFTER the DB commit.
  # Security invariant: DB update must complete before session invalidation.

  def deactivate_user(%Core.DeactivateUserRequest{} = req, stream) do
    user_id = req.user_id
    {actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    case admin_db_module().set_is_active(user_id, false) do
      :ok ->
        # Session invalidation happens AFTER DB commit (sequencing invariant).
        case session_supervisor_module().destroy_session(user_id) do
          :ok ->
            :ok

          {:error, reason} ->
            Logger.warning("DeactivateUser: destroy_session failed for #{user_id}: #{inspect(reason)}")
        end

        audit_writer_module().log(
          actor_id,
          "user_deactivated",
          "user",
          user_id,
          %{},
          "success"
        )

        %Core.DeactivateUserResponse{ok: true}

      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "user not found: #{user_id}"

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "deactivate_user DB update failed: #{inspect(reason)}"
    end
  end

  # ─── ReactivateUser ──────────────────────────────────────────────────────────
  #
  # Sets is_active=true in DB. Does NOT call destroy_session (reactivation must
  # not invalidate existing sessions).

  def reactivate_user(%Core.ReactivateUserRequest{} = req, stream) do
    user_id = req.user_id
    {actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    case admin_db_module().set_is_active(user_id, true) do
      :ok ->
        audit_writer_module().log(
          actor_id,
          "user_reactivated",
          "user",
          user_id,
          %{},
          "success"
        )

        %Core.ReactivateUserResponse{ok: true}

      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "user not found: #{user_id}"

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "reactivate_user DB update failed: #{inspect(reason)}"
    end
  end

  # ─── UpdateUserRole ──────────────────────────────────────────────────────────
  #
  # Updates users.system_role directly (NOT role_overrides — those are Gateway-side
  # overrides with TTL cache). Valid values: "user", "instance_admin", "compliance_officer".

  @valid_roles ~w(user instance_admin compliance_officer)

  def update_user_role(%Core.UpdateUserRoleRequest{} = req, stream) do
    user_id = req.user_id
    role = req.role
    {actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    unless role in @valid_roles do
      raise GRPC.RPCError,
        status: GRPC.Status.invalid_argument(),
        message: "invalid role '#{role}'; must be one of: #{Enum.join(@valid_roles, ", ")}"
    end

    case admin_db_module().set_system_role(user_id, role) do
      :ok ->
        audit_writer_module().log(
          actor_id,
          "update_user_role",
          "user",
          user_id,
          %{"role" => role},
          "success"
        )

        %Core.UpdateUserRoleResponse{ok: true}

      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "user not found: #{user_id}"

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "update_user_role DB update failed: #{inspect(reason)}"
    end
  end

  # ─── ListAdminRooms ──────────────────────────────────────────────────────────

  def list_admin_rooms(%Core.ListAdminRoomsRequest{} = req, _stream) do
    limit = if req.limit > 0, do: min(req.limit, 100), else: 20
    cursor = req.cursor || ""
    status_filter = req.status_filter || ""
    search = req.search || ""

    {rooms, next_cursor} = admin_db_module().list_rooms(limit, cursor, status_filter, search)

    proto_rooms =
      Enum.map(rooms, fn room ->
        %Core.AdminRoomProto{
          room_id: room.room_id,
          name: room.name || "",
          status: room.status || "active",
          member_count: room.member_count || 0,
          created_at: room.created_at || 0
        }
      end)

    %Core.ListAdminRoomsResponse{
      rooms: proto_rooms,
      total: length(proto_rooms),
      next_cursor: next_cursor
    }
  end

  # ─── GetAdminRoom ────────────────────────────────────────────────────────────

  def get_admin_room(%Core.GetAdminRoomRequest{} = req, _stream) do
    case admin_db_module().get_room(req.room_id) do
      {:ok, room} ->
        %Core.GetAdminRoomResponse{
          room: %Core.AdminRoomDetailProto{
            room_id: room.room_id,
            name: room.name || "",
            status: room.status || "active",
            member_count: room.member_count || 0,
            max_members: room.max_members || 0,
            visibility: room.visibility || "private",
            created_at: room.created_at || 0
          }
        }

      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{req.room_id}"

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "get_admin_room failed: #{inspect(reason)}"
    end
  end

  # ─── ListAdminRoomMembers (Story 9.18) ───────────────────────────────────────
  #
  # Returns all current members (left_at IS NULL) for a room, ordered by joined_at ASC.
  # display_name is decrypted via the same decrypt_display_name/1 helper used by
  # list_admin_users/get_admin_user. On decryption failure, display_name = "" (non-fatal).
  # Empty room returns empty members list (not an error).

  def list_admin_room_members(%Core.ListAdminRoomMembersRequest{} = req, _stream) do
    case admin_db_module().list_room_members(req.room_id) do
      {:ok, members} ->
        proto_members =
          Enum.map(members, fn member ->
            %Core.AdminRoomMemberProto{
              user_id: member.user_id,
              display_name: decrypt_display_name(member),
              joined_at: member.joined_at || 0
            }
          end)

        %Core.ListAdminRoomMembersResponse{members: proto_members}

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "list_admin_room_members failed: #{inspect(reason)}"
    end
  end

  # ─── GetServerConfig ─────────────────────────────────────────────────────────
  #
  # Security invariant: oidc_client_secret MUST NOT be returned in the response.
  # It is AES-256-GCM encrypted in the DB and only the Go Gateway has the key.
  # The admin_db_module().get_server_config/0 already filters it out at DB level.

  def get_server_config(%Core.GetServerConfigRequest{} = _req, _stream) do
    case admin_db_module().get_server_config() do
      {:ok, config} ->
        %Core.GetServerConfigResponse{
          config: %Core.ServerConfigProto{
            instance_name: Map.get(config, "instance_name", ""),
            oidc_issuer: Map.get(config, "oidc_issuer", ""),
            oidc_client_id: Map.get(config, "oidc_client_id", ""),
            room_default_max_members: parse_int_config(config, "room_default_max_members"),
            room_default_visibility: Map.get(config, "room_default_visibility", ""),
            audit_log_retention_days: parse_int_config(config, "audit_log_retention_days")
          }
        }

      {:error, reason} ->
        raise GRPC.RPCError,
          status: GRPC.Status.internal(),
          message: "get_server_config failed: #{inspect(reason)}"
    end
  end

  # ─── UpdateServerConfig ──────────────────────────────────────────────────────
  #
  # Upserts server_config table rows for provided fields.
  # Empty string / zero fields are not updated (callers omit fields they don't change).
  # NOTE: The Gateway calls CoreClient.InvalidateAllAdminSessions after OIDC config changes
  # (Story 6.10) — Core only persists the data here.

  def update_server_config(%Core.UpdateServerConfigRequest{} = req, stream) do
    {actor_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

    changes =
      []
      |> maybe_add_change("instance_name", req.instance_name)
      |> maybe_add_change("oidc_issuer", req.oidc_issuer)
      |> maybe_add_change("oidc_client_id", req.oidc_client_id)
      |> maybe_add_int_change("room_default_max_members", req.room_default_max_members)
      |> maybe_add_change("room_default_visibility", req.room_default_visibility)
      |> maybe_add_int_change("audit_log_retention_days", req.audit_log_retention_days)

    if changes == [] do
      %Core.UpdateServerConfigResponse{ok: true}
    else
      case admin_db_module().upsert_server_config(Map.new(changes)) do
        :ok ->
          # Audit log for server config update (Story 9.4 — mirrors deactivate_user pattern from Story 9.2).
          audit_writer_module().log(
            actor_id,
            "server_config_updated",
            "server_config",
            "config",
            %{changed_keys: Map.keys(Map.new(changes))},
            "success"
          )

          %Core.UpdateServerConfigResponse{ok: true}

        {:error, reason} ->
          raise GRPC.RPCError,
            status: GRPC.Status.internal(),
            message: "update_server_config failed: #{inspect(reason)}"
      end
    end
  end

  # ─── Private helpers for admin handlers ──────────────────────────────────────

  # Decrypts display_name from the user map returned by admin_db_module.
  #
  # Two supported layouts:
  #   - Test fakes (FakeAdminDB): return %{display_name: "Alice"} (already plaintext).
  #   - Real DB (Nebu.Admin.DB): returns %{display_name_encrypted: <<...>>,
  #     display_name_nonce: <<...>>}; decrypted via Nebu.Signature.decrypt_operational_pii/3.
  #
  # Falls back to empty string on decryption failure or missing data.
  defp decrypt_display_name(%{display_name: dn}) when is_binary(dn) do
    dn
  end

  defp decrypt_display_name(%{display_name_encrypted: enc, display_name_nonce: nonce})
       when is_binary(enc) and is_binary(nonce) do
    server_key = Application.get_env(:signature, :pii_encryption_key)

    case Nebu.Signature.decrypt_operational_pii(enc, nonce, server_key) do
      {:ok, plaintext} -> plaintext
      {:error, _} -> ""
    end
  end

  defp decrypt_display_name(_), do: ""

  # Masks email to "u***@domain" format.
  #
  # Two supported layouts:
  #   - Test fakes (FakeAdminDB): return %{email_masked: "a***@example.com"} (pre-masked).
  #   - Real DB (Nebu.Admin.DB): returns %{email_encrypted: <<...>>, email_nonce: <<...>>,
  #     email_ephemeral_pub: <<...>>}; decrypted via Nebu.Signature.decrypt_sensitive_pii/4.
  #
  # Email masking pattern: first char of local part + "***" + "@" + domain.
  defp mask_email(%{email_masked: masked}) when is_binary(masked) and byte_size(masked) > 0 do
    masked
  end

  defp mask_email(%{email_encrypted: enc, email_nonce: nonce, email_ephemeral_pub: ephemeral_pub})
       when is_binary(enc) and is_binary(nonce) and is_binary(ephemeral_pub) do
    {_pub, priv} = :persistent_term.get(:nebu_encryption_key, {nil, nil})

    case Nebu.Signature.decrypt_sensitive_pii(enc, ephemeral_pub, nonce, priv) do
      {:ok, email} -> do_mask_email(email)
      {:error, _} -> "***@unknown"
    end
  end

  defp mask_email(_), do: "***@unknown"

  defp do_mask_email(email) do
    case String.split(email, "@", parts: 2) do
      [local, domain] when byte_size(local) > 0 ->
        first = String.first(local)
        "#{first}***@#{domain}"

      _ ->
        "***@unknown"
    end
  end

  defp parse_int_config(config, key) do
    case Map.get(config, key) do
      nil -> 0
      val when is_integer(val) -> val
      val when is_binary(val) -> String.to_integer(val)
      _ -> 0
    end
  end

  defp maybe_add_change(acc, _key, ""), do: acc
  defp maybe_add_change(acc, key, value) when is_binary(value), do: [{key, value} | acc]
  defp maybe_add_change(acc, _key, _), do: acc

  defp maybe_add_int_change(acc, _key, 0), do: acc
  defp maybe_add_int_change(acc, key, value) when is_integer(value) and value > 0, do: [{key, value} | acc]
  defp maybe_add_int_change(acc, _key, _), do: acc

  # ─── Private helpers ──────────────────────────────────────────────────────────

  # Emits a signed m.room.member leave event into the events table and broadcasts
  # it to any :pg subscribers of the room. Called when an invite is declined via
  # POST /rooms/{roomId}/leave so that:
  #   1. fetch_delta_rooms can detect the decline even across sync-cycle boundaries
  #      (race-proof — event persisted in DB, not just a volatile :pg message).
  #   2. Any sync task subscribed to the room's :pg group wakes up immediately.
  defp emit_decline_event(room_id, user_id) do
    event_map = %{
      "room_id"          => room_id,
      "type"             => "m.room.member",
      "state_key"        => user_id,
      "sender"           => user_id,
      "content"          => %{"membership" => "leave"},
      "origin_server_ts" => Nebu.DB.Helpers.now_ms()
    }
    event_id      = Nebu.EventId.generate(event_map)
    event_with_id = Map.put(event_map, "event_id", event_id)
    {_pub, priv}  = :persistent_term.get(:nebu_signing_key)
    event_json    = Nebu.CanonicalJson.encode!(event_map)
    signature     = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
    sig_b64       = Base.encode64(signature)
    signed        = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})

    case messages_db_module().insert_event(signed) do
      :ok ->
        Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, signed}))

      {:error, reason} ->
        require Logger
        Logger.warning("emit_decline_event: failed for #{user_id} in #{room_id}: #{inspect(reason)}")
    end
  end

  # ─── UpgradeRoom — Story 9.8: atomic room version upgrade ─────────────────────
  #
  # Sequence (all in one call, no extra GenServer abstraction needed):
  #   1. Verify old room exists and requester has power_level >= 100 (owner).
  #   2. Emit m.room.tombstone in the old room.
  #   3. Create new room, join requester as creator, set creator power levels.
  #   4. Emit m.room.create in new room WITH predecessor (old_room_id + tombstone_event_id).
  #   5. Copy state events from old room (excluding create, tombstone, aliases, member).
  #      Emit m.room.join_rules LAST per Matrix spec.
  #   6. Invite all old members (except requester, who is already joined).
  #   7. Write audit log: room_upgraded.
  #
  # Security: power level check (step 1) is performed BEFORE any state mutation.
  # All events are emitted via the private emit_state_event/5 helper (same signing
  # pattern as create_room/2) to avoid triggering the Room.Server power check a
  # second time after we've already verified power level >= 100.

  def upgrade_room(request, stream) do
    {requester_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)
    old_room_id  = request.old_room_id
    new_version  = if request.new_version == "" or is_nil(request.new_version),
                     do: "10",
                     else: request.new_version

    # 1. Verify old room exists and requester is owner (power_level >= 100).
    case Nebu.Room.RoomSupervisor.lookup_room(old_room_id) do
      {:error, :not_found} ->
        raise GRPC.RPCError,
          status: GRPC.Status.not_found(),
          message: "room not found: #{old_room_id}"

      {:ok, _pid} ->
        old_state =
          try do
            room_registry_module().get_state(old_room_id)
          catch
            :exit, {:noproc, _} ->
              raise GRPC.RPCError,
                status: GRPC.Status.not_found(),
                message: "room not found: #{old_room_id}"
          end

        requester_level =
          get_in(old_state.power_levels, ["users", requester_id]) || 0

        if requester_level < 100 do
          raise GRPC.RPCError,
            status: GRPC.Status.permission_denied(),
            message: "insufficient power level for room upgrade"
        end

        # 2. Create new room and set up creator membership + power levels.
        # MAJOR-3 fix: m.room.create MUST be the first event in any room per Matrix spec.
        # We therefore emit m.room.create BEFORE joining the creator so that the
        # m.room.create event has a lower origin_server_ts than the m.room.member event.
        new_room_id = generate_room_id()

        case Nebu.Room.RoomSupervisor.start_room(new_room_id) do
          {:ok, _new_pid} ->
            # 3. Emit m.room.tombstone in the OLD room (via private helper, bypasses Room.Server
            #    power check — we already confirmed power_level >= 100 above).
            tombstone_content = %{
              "body"             => "This room has been replaced",
              "replacement_room" => new_room_id
            }

            tombstone_event_id =
              case emit_state_event(old_room_id, requester_id, "m.room.tombstone", "", tombstone_content) do
                {:ok, event_id} -> event_id
                {:error, reason} ->
                  raise GRPC.RPCError,
                    status: GRPC.Status.internal(),
                    message: "Failed to emit tombstone event: #{inspect(reason)}"
              end

            # 4. Emit m.room.create in new room WITH predecessor FIRST (before join).
            # MAJOR-3 fix: create event must be the first event written to the new room.
            create_content = %{
              "creator"      => requester_id,
              "room_version" => new_version,
              "predecessor"  => %{
                "room_id"  => old_room_id,
                "event_id" => tombstone_event_id
              }
            }
            emit_state_event(new_room_id, requester_id, "m.room.create", "", create_content)

            # Now join the creator and set power levels (m.room.member comes AFTER m.room.create).
            :ok = Nebu.Room.Server.join(new_room_id, requester_id)

            default_pl  = Nebu.Room.Server.default_power_levels()
            creator_pl  = put_in(default_pl, ["users", requester_id], 100)
            :ok = Nebu.Room.Server.set_power_levels(new_room_id, requester_id, creator_pl)

            # 5. Copy state events from old room (spec-mandated order).
            copy_state_events(old_room_id, new_room_id, requester_id)

            # 6. Invite all old members (except requester — already joined).
            old_members = MapSet.delete(old_state.members, requester_id)

            Enum.each(old_members, fn member_id ->
              case db_module_invite().insert_invitation(new_room_id, requester_id, member_id) do
                :ok ->
                  :pg.get_local_members("user:#{member_id}")
                  |> Enum.each(&send(&1, {:new_invite, new_room_id}))

                {:error, reason} ->
                  Logger.warning(
                    "upgrade_room: invite failed for #{member_id} in #{new_room_id}: #{inspect(reason)}"
                  )
              end
            end)

            # 7. Audit log.
            audit_writer_module().log(
              requester_id,
              "room_upgraded",
              "room",
              old_room_id,
              %{"new_room_id" => new_room_id, "new_version" => new_version},
              "success"
            )

            %Core.UpgradeRoomResponse{new_room_id: new_room_id}

          {:error, reason} ->
            raise GRPC.RPCError,
              status: GRPC.Status.internal(),
              message: "Failed to start new room: #{inspect(reason)}"
        end
    end
  end

  # Emits a signed state event into the events table and broadcasts to :pg subscribers.
  # Used by upgrade_room/2 and any other path that needs to write state events directly
  # without going through Room.Server (which would re-check power levels).
  # Returns {:ok, event_id} on success, {:error, reason} on DB failure.
  defp emit_state_event(room_id, sender_id, event_type, state_key, content) do
    event_map = %{
      "room_id"          => room_id,
      "type"             => event_type,
      "state_key"        => state_key,
      "sender"           => sender_id,
      "content"          => content,
      "origin_server_ts" => Nebu.DB.Helpers.now_ms()
    }

    event_id      = Nebu.EventId.generate(event_map)
    event_with_id = Map.put(event_map, "event_id", event_id)
    {_pub, priv}  = :persistent_term.get(:nebu_signing_key)
    event_json    = Nebu.CanonicalJson.encode!(event_map)
    signature     = :crypto.sign(:eddsa, :none, event_json, [priv, :ed25519])
    sig_b64       = Base.encode64(signature)
    signed        = Map.put(event_with_id, "signatures", %{"nebu" => sig_b64})

    case messages_db_module().insert_event(signed) do
      :ok ->
        Enum.each(:pg.get_local_members("room:#{room_id}"), &send(&1, {:new_event, signed}))
        {:ok, event_id}

      {:error, reason} ->
        {:error, reason}
    end
  end

  # Copies state events from old_room to new_room per Matrix spec Section 11.35.1.
  #
  # Filter rules:
  #   - Exclude m.room.create (new room gets its own with predecessor).
  #   - Exclude m.room.tombstone (the old room is dead; we don't copy its tombstone).
  #   - Exclude m.room.aliases (room-alias events are scoped to the old room's alias).
  #   - Exclude m.space.child / m.space.parent (space relationships are not migrated).
  #   - Exclude m.room.member (membership is re-established via invite/join).
  #
  # Required state events to copy per Matrix spec: m.room.name, m.room.topic,
  # m.room.join_rules, m.room.power_levels, m.room.history_visibility,
  # m.room.guest_access, m.room.avatar, m.room.canonical_alias, m.room.encryption.
  #
  # MAJOR-1 fix: m.room.name is excluded by get_generic_state_events/1 (it has a
  # dedicated DB helper in Story 9-7). We fetch it here explicitly and include it in
  # the "other" batch so the upgraded room preserves its name.
  #
  # Order: emit all state events first, then m.room.join_rules last (per spec).
  # Uses get_generic_state_events/1 from Story 9-7 which excludes member,
  # power_levels, create, and name.
  defp copy_state_events(old_room_id, new_room_id, requester_id) do
    # Fetch generic state events (excludes member, power_levels, create, name per 9-7).
    state_events =
      case messages_db_module().get_generic_state_events(old_room_id) do
        {:ok, events} -> events
        {:error, _}   -> []
      end

    # Exclude tombstone, aliases, and space events (get_generic_state_events already
    # excludes member/power_levels/create/name, but tombstone/aliases are included).
    excluded_types = [
      "m.room.tombstone",
      "m.room.aliases",
      "m.space.child",
      "m.space.parent"
    ]

    # MAJOR-1 fix: get_generic_state_events excludes m.room.name, so fetch it
    # separately and prepend it as a synthetic event entry for the copy.
    name_state_events =
      case messages_db_module().get_room_name(old_room_id) do
        {:ok, name} ->
          [%{type: "m.room.name", state_key: "", content_json: Jason.encode!(%{"name" => name})}]
        {:error, _} ->
          []
      end

    {join_rules_events, other_events} =
      (name_state_events ++ state_events)
      |> Enum.reject(fn e -> e.type in excluded_types end)
      |> Enum.split_with(fn e -> e.type == "m.room.join_rules" end)

    # Emit non-join_rules state events first.
    Enum.each(other_events, fn e ->
      content =
        case Jason.decode(e.content_json) do
          {:ok, map} -> map
          _          -> %{}
        end
      emit_state_event(new_room_id, requester_id, e.type, e.state_key || "", content)
    end)

    # Emit join_rules last (Matrix spec requirement).
    Enum.each(join_rules_events, fn e ->
      content =
        case Jason.decode(e.content_json) do
          {:ok, map} -> map
          _          -> %{}
        end
      emit_state_event(new_room_id, requester_id, e.type, e.state_key || "", content)
    end)
  end
end
