defmodule Core.Event do
  @moduledoc false

  use Protobuf, full_name: "core.Event", protoc_gen_elixir_version: "0.16.0", syntax: :proto3

  field :event_id, 1, type: :string, json_name: "eventId"
  field :room_id, 2, type: :string, json_name: "roomId"
  field :sender_id, 3, type: :string, json_name: "senderId"
  field :event_type, 4, type: :string, json_name: "eventType"
  field :content, 5, type: :bytes
  field :origin_ts, 6, type: :int64, json_name: "originTs"
  field :server_ts, 7, type: :int64, json_name: "serverTs"
  field :state_key, 8, type: :string, json_name: "stateKey"
end

defmodule Core.SendEventRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.SendEventRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :sender_id, 2, type: :string, json_name: "senderId"
  field :event_type, 3, type: :string, json_name: "eventType"
  field :txn_id, 4, type: :string, json_name: "txnId"
  field :content, 5, type: :bytes
  field :origin_ts, 6, type: :int64, json_name: "originTs"
  field :state_key, 7, type: :string, json_name: "stateKey"
  field :is_state_event, 8, type: :bool, json_name: "isStateEvent"
end

defmodule Core.SendEventResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.SendEventResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :event_id, 1, type: :string, json_name: "eventId"
end

defmodule Core.CreateRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.CreateRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :creator_id, 1, type: :string, json_name: "creatorId"
  field :name, 2, proto3_optional: true, type: :string
  field :topic, 3, proto3_optional: true, type: :string
  field :is_direct, 4, type: :bool, json_name: "isDirect"
end

defmodule Core.CreateRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.CreateRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.JoinRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.JoinRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :room_id_or_alias, 2, type: :string, json_name: "roomIdOrAlias"
end

defmodule Core.JoinRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.JoinRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.LeaveRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.LeaveRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :room_id, 2, type: :string, json_name: "roomId"
end

defmodule Core.LeaveRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.LeaveRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.GetMessagesRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetMessagesRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :from_token, 2, type: :string, json_name: "fromToken"
  field :to_token, 3, proto3_optional: true, type: :string, json_name: "toToken"
  field :limit, 4, type: :int32
  field :direction, 5, type: :string
end

defmodule Core.GetMessagesResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetMessagesResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :events, 1, repeated: true, type: Core.Event
  field :next_batch, 2, type: :string, json_name: "nextBatch"
  field :prev_batch, 3, type: :string, json_name: "prevBatch"
end

defmodule Core.SetPresenceRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.SetPresenceRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :presence, 2, type: :string
  field :status_msg, 3, proto3_optional: true, type: :string, json_name: "statusMsg"
end

defmodule Core.SetPresenceResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.SetPresenceResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.SetTypingRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.SetTypingRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :user_id, 2, type: :string, json_name: "userId"
  field :typing, 3, type: :bool
  field :timeout_ms, 4, type: :int32, json_name: "timeoutMs"
end

defmodule Core.SetTypingResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.SetTypingResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.ValidateTokenRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ValidateTokenRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :display_name, 2, type: :string, json_name: "displayName"
  field :email, 3, type: :string
end

defmodule Core.ValidateTokenResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ValidateTokenResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 2, type: :string, json_name: "userId"
  field :system_role, 3, type: :string, json_name: "systemRole"
  field :display_name, 4, type: :string, json_name: "displayName"
  field :is_active, 5, type: :bool, json_name: "isActive"
end

defmodule Core.GetPendingEventsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetPendingEventsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :node_id, 1, type: :string, json_name: "nodeId"
  field :since_token, 2, type: :string, json_name: "sinceToken"
end

defmodule Core.GetPendingEventsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetPendingEventsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :events, 1, repeated: true, type: Core.Event
  field :next_token, 2, type: :string, json_name: "nextToken"
end

defmodule Core.EventBusRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.EventBusRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :node_id, 1, type: :string, json_name: "nodeId"
  field :since_token, 2, proto3_optional: true, type: :string, json_name: "sinceToken"
end

defmodule Core.GetMetricsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetMetricsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.GetMetricsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetMetricsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :msg_per_sec, 1, type: :float, json_name: "msgPerSec"
  field :active_sessions, 2, type: :int32, json_name: "activeSessions"
  field :room_count, 3, type: :int32, json_name: "roomCount"
end

defmodule Core.InviteUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.InviteUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :inviter_id, 2, type: :string, json_name: "inviterId"
  field :invitee_id, 3, type: :string, json_name: "inviteeId"
end

defmodule Core.InviteUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.InviteUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.GetRoomStateRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetRoomStateRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :event_type, 2, type: :string, json_name: "eventType"
  field :state_key, 3, type: :string, json_name: "stateKey"
end

defmodule Core.GetRoomStateResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetRoomStateResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :members, 1, repeated: true, type: :string
  field :power_levels_json, 2, type: :string, json_name: "powerLevelsJson"
  field :room_name, 3, type: :string, json_name: "roomName"
  field :state_events, 4, repeated: true, type: Core.SyncRoomStateEvent, json_name: "stateEvents"
end

defmodule Core.SetPowerLevelsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.SetPowerLevelsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :power_levels_json, 2, type: :string, json_name: "powerLevelsJson"
end

defmodule Core.SetPowerLevelsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.SetPowerLevelsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.SendReceiptRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.SendReceiptRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :user_id, 2, type: :string, json_name: "userId"
  field :receipt_type, 3, type: :string, json_name: "receiptType"
  field :event_id, 4, type: :string, json_name: "eventId"
end

defmodule Core.SendReceiptResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.SendReceiptResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.GetInitialSyncRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetInitialSyncRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
end

defmodule Core.GetInitialSyncResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetInitialSyncResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :since_token, 1, type: :string, json_name: "sinceToken"
  field :rooms, 2, repeated: true, type: Core.SyncRoom
end

defmodule Core.SyncRoomStateEvent do
  @moduledoc false

  use Protobuf,
    full_name: "core.SyncRoomStateEvent",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :type, 1, type: :string
  field :state_key, 2, type: :string, json_name: "stateKey"
  field :content, 3, type: :bytes
  field :sender, 4, type: :string
end

defmodule Core.GetSyncDeltaRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetSyncDeltaRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :since_token, 2, type: :string, json_name: "sinceToken"
  field :timeout_ms, 3, type: :int64, json_name: "timeoutMs"
end

defmodule Core.GetSyncDeltaResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetSyncDeltaResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :since_token, 1, type: :string, json_name: "sinceToken"
  field :rooms, 2, repeated: true, type: Core.SyncRoom
  field :fallback_to_initial, 3, type: :bool, json_name: "fallbackToInitial"
end

defmodule Core.SyncRoom do
  @moduledoc false

  use Protobuf, full_name: "core.SyncRoom", protoc_gen_elixir_version: "0.16.0", syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :state_events, 2, repeated: true, type: Core.SyncRoomStateEvent, json_name: "stateEvents"
  field :timeline_events, 3, repeated: true, type: Core.Event, json_name: "timelineEvents"
  field :limited, 4, type: :bool
  field :prev_batch, 5, type: :string, json_name: "prevBatch"
end

defmodule Core.GetPresenceRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetPresenceRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
end

defmodule Core.GetPresenceResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetPresenceResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :presence, 1, type: :string
  field :last_active_ago, 2, type: :int64, json_name: "lastActiveAgo"
end

defmodule Core.UpdateProfileRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateProfileRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :displayname, 2, type: :string
  field :avatar_url, 3, type: :string, json_name: "avatarUrl"
end

defmodule Core.UpdateProfileResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateProfileResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.WriteAuditLogRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.WriteAuditLogRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :actor_user_id, 1, type: :string, json_name: "actorUserId"
  field :action, 2, type: :string
  field :target_type, 3, type: :string, json_name: "targetType"
  field :target_id, 4, type: :string, json_name: "targetId"
  field :metadata_json, 5, type: :bytes, json_name: "metadataJson"
  field :outcome, 6, type: :string
  field :error_detail, 7, type: :string, json_name: "errorDetail"
end

defmodule Core.WriteAuditLogResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.WriteAuditLogResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.DeleteUserKeysRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.DeleteUserKeysRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :admin_user_id, 1, type: :string, json_name: "adminUserId"
  field :target_user_id, 2, type: :string, json_name: "targetUserId"
  field :reason, 3, type: :string
end

defmodule Core.DeleteUserKeysResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.DeleteUserKeysResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :status, 1, type: :string
  field :keys_deleted_at, 2, type: :int64, json_name: "keysDeletedAt"
end

defmodule Core.KickUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.KickUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :caller_id, 2, type: :string, json_name: "callerId"
  field :target_id, 3, type: :string, json_name: "targetId"
  field :reason, 4, type: :string
end

defmodule Core.KickUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.KickUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.BanUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.BanUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :caller_id, 2, type: :string, json_name: "callerId"
  field :target_id, 3, type: :string, json_name: "targetId"
  field :reason, 4, type: :string
end

defmodule Core.BanUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.BanUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.UnbanUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UnbanUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :caller_id, 2, type: :string, json_name: "callerId"
  field :target_id, 3, type: :string, json_name: "targetId"
end

defmodule Core.UnbanUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UnbanUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.ForgetRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ForgetRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :user_id, 2, type: :string, json_name: "userId"
end

defmodule Core.ForgetRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ForgetRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.ListPublicRoomsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListPublicRoomsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :limit, 1, type: :int32
  field :since, 2, type: :string
  field :filter_term, 3, type: :string, json_name: "filterTerm"
end

defmodule Core.RoomSummary do
  @moduledoc false

  use Protobuf,
    full_name: "core.RoomSummary",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :name, 2, type: :string
  field :topic, 3, type: :string
  field :num_joined_members, 4, type: :int32, json_name: "numJoinedMembers"
  field :world_readable, 5, type: :bool, json_name: "worldReadable"
  field :guest_can_join, 6, type: :bool, json_name: "guestCanJoin"
end

defmodule Core.ListPublicRoomsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListPublicRoomsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :rooms, 1, repeated: true, type: Core.RoomSummary
  field :next_cursor, 2, type: :string, json_name: "nextCursor"
  field :total_estimate, 3, type: :int32, json_name: "totalEstimate"
end

defmodule Core.GetEventContextRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetEventContextRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :event_id, 2, type: :string, json_name: "eventId"
  field :limit, 3, type: :int32
end

defmodule Core.ContextStateEvent do
  @moduledoc false

  use Protobuf,
    full_name: "core.ContextStateEvent",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :event_type, 1, type: :string, json_name: "eventType"
  field :state_key, 2, type: :string, json_name: "stateKey"
  field :content, 3, type: :bytes
  field :sender, 4, type: :string
end

defmodule Core.GetEventContextResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetEventContextResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :event, 1, type: Core.Event
  field :events_before, 2, repeated: true, type: Core.Event, json_name: "eventsBefore"
  field :events_after, 3, repeated: true, type: Core.Event, json_name: "eventsAfter"
  field :state, 4, repeated: true, type: Core.ContextStateEvent
  field :start_token, 5, type: :string, json_name: "startToken"
  field :end_token, 6, type: :string, json_name: "endToken"
end

defmodule Core.InvalidateUserSessionsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.InvalidateUserSessionsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
end

defmodule Core.InvalidateUserSessionsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.InvalidateUserSessionsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.UpdateRoomSettingsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateRoomSettingsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :max_members, 2, type: :int32, json_name: "maxMembers"
end

defmodule Core.UpdateRoomSettingsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateRoomSettingsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.ArchiveRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ArchiveRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.ArchiveRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ArchiveRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.UnarchiveRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UnarchiveRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.UnarchiveRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UnarchiveRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.InvalidateAllAdminSessionsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.InvalidateAllAdminSessionsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.InvalidateAllAdminSessionsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.InvalidateAllAdminSessionsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.AdminUserProto do
  @moduledoc false

  use Protobuf,
    full_name: "core.AdminUserProto",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :display_name, 2, type: :string, json_name: "displayName"
  field :email_masked, 3, type: :string, json_name: "emailMasked"
  field :is_active, 4, type: :bool, json_name: "isActive"
  field :system_role, 5, type: :string, json_name: "systemRole"
  field :created_at, 6, type: :int64, json_name: "createdAt"
end

defmodule Core.ListAdminUsersRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListAdminUsersRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :limit, 1, type: :int32
  field :cursor, 2, type: :string
  field :search, 3, type: :string
end

defmodule Core.ListAdminUsersResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListAdminUsersResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :users, 1, repeated: true, type: Core.AdminUserProto
  field :total, 2, type: :int32
  field :next_cursor, 3, type: :string, json_name: "nextCursor"
end

defmodule Core.GetAdminUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetAdminUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
end

defmodule Core.GetAdminUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetAdminUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user, 1, type: Core.AdminUserProto
end

defmodule Core.DeactivateUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.DeactivateUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
end

defmodule Core.DeactivateUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.DeactivateUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.ReactivateUserRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ReactivateUserRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
end

defmodule Core.ReactivateUserResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ReactivateUserResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.UpdateUserRoleRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateUserRoleRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :role, 2, type: :string
end

defmodule Core.UpdateUserRoleResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateUserRoleResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.AdminRoomProto do
  @moduledoc false

  use Protobuf,
    full_name: "core.AdminRoomProto",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :name, 2, type: :string
  field :status, 3, type: :string
  field :member_count, 4, type: :int32, json_name: "memberCount"
  field :created_at, 5, type: :int64, json_name: "createdAt"
end

defmodule Core.AdminRoomDetailProto do
  @moduledoc false

  use Protobuf,
    full_name: "core.AdminRoomDetailProto",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
  field :name, 2, type: :string
  field :status, 3, type: :string
  field :member_count, 4, type: :int32, json_name: "memberCount"
  field :max_members, 5, type: :int32, json_name: "maxMembers"
  field :visibility, 6, type: :string
  field :created_at, 7, type: :int64, json_name: "createdAt"
end

defmodule Core.ListAdminRoomsRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListAdminRoomsRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :limit, 1, type: :int32
  field :cursor, 2, type: :string
  field :status_filter, 3, type: :string, json_name: "statusFilter"
  field :search, 4, type: :string
end

defmodule Core.ListAdminRoomsResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListAdminRoomsResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :rooms, 1, repeated: true, type: Core.AdminRoomProto
  field :total, 2, type: :int32
  field :next_cursor, 3, type: :string, json_name: "nextCursor"
end

defmodule Core.GetAdminRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetAdminRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.GetAdminRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetAdminRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room, 1, type: Core.AdminRoomDetailProto
end

defmodule Core.ServerConfigProto do
  @moduledoc false

  use Protobuf,
    full_name: "core.ServerConfigProto",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :instance_name, 1, type: :string, json_name: "instanceName"
  field :oidc_issuer, 2, type: :string, json_name: "oidcIssuer"
  field :oidc_client_id, 3, type: :string, json_name: "oidcClientId"
  field :room_default_max_members, 4, type: :int32, json_name: "roomDefaultMaxMembers"
  field :room_default_visibility, 5, type: :string, json_name: "roomDefaultVisibility"
  field :audit_log_retention_days, 6, type: :int32, json_name: "auditLogRetentionDays"
end

defmodule Core.GetServerConfigRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetServerConfigRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3
end

defmodule Core.GetServerConfigResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.GetServerConfigResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :config, 1, type: Core.ServerConfigProto
end

defmodule Core.UpdateServerConfigRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateServerConfigRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :instance_name, 1, type: :string, json_name: "instanceName"
  field :oidc_issuer, 2, type: :string, json_name: "oidcIssuer"
  field :oidc_client_id, 3, type: :string, json_name: "oidcClientId"
  field :room_default_max_members, 4, type: :int32, json_name: "roomDefaultMaxMembers"
  field :room_default_visibility, 5, type: :string, json_name: "roomDefaultVisibility"
  field :audit_log_retention_days, 6, type: :int32, json_name: "auditLogRetentionDays"
end

defmodule Core.UpdateServerConfigResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpdateServerConfigResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :ok, 1, type: :bool
end

defmodule Core.UpgradeRoomRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpgradeRoomRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :old_room_id, 1, type: :string, json_name: "oldRoomId"
  field :requester_id, 2, type: :string, json_name: "requesterId"
  field :new_version, 3, type: :string, json_name: "newVersion"
end

defmodule Core.UpgradeRoomResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.UpgradeRoomResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :new_room_id, 1, type: :string, json_name: "newRoomId"
end

defmodule Core.AdminRoomMemberProto do
  @moduledoc false

  use Protobuf,
    full_name: "core.AdminRoomMemberProto",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :user_id, 1, type: :string, json_name: "userId"
  field :display_name, 2, type: :string, json_name: "displayName"
  field :joined_at, 3, type: :int64, json_name: "joinedAt"
end

defmodule Core.ListAdminRoomMembersRequest do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListAdminRoomMembersRequest",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :room_id, 1, type: :string, json_name: "roomId"
end

defmodule Core.ListAdminRoomMembersResponse do
  @moduledoc false

  use Protobuf,
    full_name: "core.ListAdminRoomMembersResponse",
    protoc_gen_elixir_version: "0.16.0",
    syntax: :proto3

  field :members, 1, repeated: true, type: Core.AdminRoomMemberProto
end
