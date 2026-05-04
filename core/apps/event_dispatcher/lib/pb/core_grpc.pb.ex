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
  rpc :GetPresence, Core.GetPresenceRequest, Core.GetPresenceResponse
  rpc :UpdateProfile, Core.UpdateProfileRequest, Core.UpdateProfileResponse
  rpc :WriteAuditLog, Core.WriteAuditLogRequest, Core.WriteAuditLogResponse
  rpc :DeleteUserKeys, Core.DeleteUserKeysRequest, Core.DeleteUserKeysResponse
  rpc :KickUser, Core.KickUserRequest, Core.KickUserResponse
  rpc :BanUser, Core.BanUserRequest, Core.BanUserResponse
  rpc :UnbanUser, Core.UnbanUserRequest, Core.UnbanUserResponse
  rpc :ForgetRoom, Core.ForgetRoomRequest, Core.ForgetRoomResponse
  rpc :ListPublicRooms, Core.ListPublicRoomsRequest, Core.ListPublicRoomsResponse
  rpc :GetEventContext, Core.GetEventContextRequest, Core.GetEventContextResponse
  rpc :InvalidateUserSessions, Core.InvalidateUserSessionsRequest, Core.InvalidateUserSessionsResponse
  rpc :UpdateRoomSettings, Core.UpdateRoomSettingsRequest, Core.UpdateRoomSettingsResponse
  rpc :ArchiveRoom, Core.ArchiveRoomRequest, Core.ArchiveRoomResponse
  rpc :UnarchiveRoom, Core.UnarchiveRoomRequest, Core.UnarchiveRoomResponse
  rpc :InvalidateAllAdminSessions, Core.InvalidateAllAdminSessionsRequest, Core.InvalidateAllAdminSessionsResponse
  rpc :ListAdminUsers, Core.ListAdminUsersRequest, Core.ListAdminUsersResponse
  rpc :GetAdminUser, Core.GetAdminUserRequest, Core.GetAdminUserResponse
  rpc :DeactivateUser, Core.DeactivateUserRequest, Core.DeactivateUserResponse
  rpc :ReactivateUser, Core.ReactivateUserRequest, Core.ReactivateUserResponse
  rpc :UpdateUserRole, Core.UpdateUserRoleRequest, Core.UpdateUserRoleResponse
  rpc :ListAdminRooms, Core.ListAdminRoomsRequest, Core.ListAdminRoomsResponse
  rpc :GetAdminRoom, Core.GetAdminRoomRequest, Core.GetAdminRoomResponse
  rpc :GetServerConfig, Core.GetServerConfigRequest, Core.GetServerConfigResponse
  rpc :UpdateServerConfig, Core.UpdateServerConfigRequest, Core.UpdateServerConfigResponse
end

defmodule Core.CoreService.Stub do
  use GRPC.Stub, service: Core.CoreService.Service
end
