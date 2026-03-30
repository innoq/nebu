defmodule Nebu.EventDispatcher.Server do
  use GRPC.Server, service: Core.CoreService.Service

  require Logger

  def send_event(_request, _stream) do
    {:ok, %Core.SendEventResponse{}}
  end

  def create_room(_request, _stream) do
    {:ok, %Core.CreateRoomResponse{}}
  end

  def join_room(_request, _stream) do
    {:ok, %Core.JoinRoomResponse{}}
  end

  def get_messages(_request, _stream) do
    {:ok, %Core.GetMessagesResponse{}}
  end

  def set_presence(_request, _stream) do
    {:ok, %Core.SetPresenceResponse{}}
  end

  def set_typing(_request, _stream) do
    {:ok, %Core.SetTypingResponse{}}
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
        {:ok,
         %Core.ValidateTokenResponse{
           user_id: user.user_id,
           system_role: user.system_role,
           display_name: user.display_name,
           is_active: user.is_active
         }}

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
    {:ok, %Core.GetPendingEventsResponse{}}
  end

  def event_bus(_request, stream) do
    # Placeholder — Epic 4 Story 4.8 implements full streaming EventBus logic
    Logger.warning("event_bus stub called — not yet implemented")
    {:ok, stream}
  end
end
