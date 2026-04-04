defmodule Core.CoreService.Service do
  use GRPC.Service, name: "core.CoreService", protoc_gen_elixir_version: "0.16.0"

  rpc :SendEvent, Core.SendEventRequest, Core.SendEventResponse
  rpc :CreateRoom, Core.CreateRoomRequest, Core.CreateRoomResponse
  rpc :JoinRoom, Core.JoinRoomRequest, Core.JoinRoomResponse
  rpc :GetMessages, Core.GetMessagesRequest, Core.GetMessagesResponse
  rpc :SetPresence, Core.SetPresenceRequest, Core.SetPresenceResponse
  rpc :SetTyping, Core.SetTypingRequest, Core.SetTypingResponse
  rpc :ValidateToken, Core.ValidateTokenRequest, Core.ValidateTokenResponse
  rpc :GetPendingEvents, Core.GetPendingEventsRequest, Core.GetPendingEventsResponse
  rpc :EventBus, Core.EventBusRequest, stream(Core.Event)
  rpc :GetMetrics, Core.GetMetricsRequest, Core.GetMetricsResponse
  rpc :GetRoomState, Core.GetRoomStateRequest, Core.GetRoomStateResponse
end

defmodule Core.CoreService.Stub do
  use GRPC.Stub, service: Core.CoreService.Service
end
