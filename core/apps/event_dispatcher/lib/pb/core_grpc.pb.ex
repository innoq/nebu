defmodule Core.CoreService.Service do
  use GRPC.Service, name: "core.CoreService", protoc_gen_elixir_version: "0.16.0"

  rpc :SendEvent, Core.SendEventRequest, Core.SendEventResponse
  rpc :CreateRoom, Core.CreateRoomRequest, Core.CreateRoomResponse
  rpc :JoinRoom, Core.JoinRoomRequest, Core.JoinRoomResponse
  rpc :LeaveRoom, Core.LeaveRoomRequest, Core.LeaveRoomResponse
  rpc :GetMessages, Core.GetMessagesRequest, Core.GetMessagesResponse
  rpc :SetPresence, Core.SetPresenceRequest, Core.SetPresenceResponse
  rpc :SetTyping, Core.SetTypingRequest, Core.SetTypingResponse
  rpc :ValidateToken, Core.ValidateTokenRequest, Core.ValidateTokenResponse
  rpc :GetPendingEvents, Core.GetPendingEventsRequest, Core.GetPendingEventsResponse
  rpc :EventBus, Core.EventBusRequest, stream(Core.Event)
  rpc :GetMetrics, Core.GetMetricsRequest, Core.GetMetricsResponse
  rpc :GetRoomState, Core.GetRoomStateRequest, Core.GetRoomStateResponse
  rpc :InviteUser, Core.InviteUserRequest, Core.InviteUserResponse
  rpc :SetPowerLevels, Core.SetPowerLevelsRequest, Core.SetPowerLevelsResponse
  rpc :SendReceipt, Core.SendReceiptRequest, Core.SendReceiptResponse
  rpc :GetInitialSync, Core.GetInitialSyncRequest, Core.GetInitialSyncResponse
  rpc :GetSyncDelta, Core.GetSyncDeltaRequest, Core.GetSyncDeltaResponse
end

defmodule Core.CoreService.Stub do
  use GRPC.Stub, service: Core.CoreService.Service
end
