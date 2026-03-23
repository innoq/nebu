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

  def validate_token(_request, _stream) do
    {:ok, %Core.ValidateTokenResponse{}}
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
