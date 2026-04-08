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
