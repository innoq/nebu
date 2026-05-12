defmodule Nebu.EventDispatcher.GetEventTest do
  use ExUnit.Case, async: false

  # ─── Story 11-8: Server.get_event/2 unit tests ───────────────────────────────
  #
  # Bug fix: GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} → 404
  # Root cause: endpoint never registered in main.go; GetEvent gRPC RPC missing.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env
  # are process-global resources.
  #
  # Test strategy:
  #   - Call Server.get_event/2 directly (synchronous unary handler).
  #   - Fake stream carries x-user-id so trusted_identity/1 resolves the caller.
  #   - FakeDB injected at :event_dispatcher, :messages_db_module — no PostgreSQL.
  #   - Room started via RoomSupervisor.start_room/1 + FakeRoomDB for Room.Server.
  #   - Membership enforced through real Room.Server state (users joined via join/2).

  alias Nebu.EventDispatcher.Server

  @room_id    "!get_event_test_11_8:nebu.local"
  @event_id   "$test_evt_11_8:nebu.local"
  @user_id    "@kai:nebu.local"
  @non_member "@stranger:nebu.local"
  @unknown_id "$unknown_evt_11_8:nebu.local"

  # ─── FakeRoomDB ──────────────────────────────────────────────────────────────
  # Minimal in-memory DB for Room.Server.init — no PostgreSQL required.

  defmodule FakeRoomDB do
    def load_members(_room_id),              do: {:error, :not_found}
    def insert_room(_room_id),               do: {:ok, System.system_time(:millisecond)}
    def insert_member(_room_id, _user_id),   do: :ok
    def delete_member(_room_id, _user_id),   do: :ok
    def insert_event(_event),                do: :ok
    def set_power_levels(_room_id, _levels), do: :ok
    def get_rooms_for_user(_user_id),        do: {:ok, []}
    def get_recently_left_rooms_for_user(_), do: {:ok, []}
    def fetch_events(_r, _d, _l, _t),        do: {:ok, [], "", ""}
    def fetch_events_since(_r, _l_id, _lim), do: {:ok, []}
    def get_event_timestamp(_event_id),      do: {:error, :not_found}
    def get_room_name(_room_id),             do: {:ok, nil}
    def load_room_settings(_room_id),        do: {:ok, 0}
    def get_room_status(_room_id),           do: {:ok, "active"}
    def check_room_status_for_update(_),     do: {:ok, "active"}
    def fetch_event(_event_id, _room_id),    do: {:error, :not_found}
  end

  # ─── FakeEventDB (happy path) ─────────────────────────────────────────────────
  # Returns the test event on fetch_event; all other calls are stubs.

  defmodule FakeEventDB do
    def load_members(_room_id),              do: {:error, :not_found}
    def insert_room(_room_id),               do: {:ok, System.system_time(:millisecond)}
    def insert_member(_room_id, _user_id),   do: :ok
    def delete_member(_room_id, _user_id),   do: :ok
    def insert_event(_event),                do: :ok
    def set_power_levels(_room_id, _levels), do: :ok
    def get_rooms_for_user(_user_id),        do: {:ok, []}
    def get_recently_left_rooms_for_user(_), do: {:ok, []}
    def fetch_events(_r, _d, _l, _t),        do: {:ok, [], "", ""}
    def fetch_events_since(_r, _l_id, _lim), do: {:ok, []}
    def get_event_timestamp(_event_id),      do: {:error, :not_found}
    def get_room_name(_room_id),             do: {:ok, nil}
    def load_room_settings(_room_id),        do: {:ok, 0}
    def get_room_status(_room_id),           do: {:ok, "active"}
    def check_room_status_for_update(_),     do: {:ok, "active"}

    def fetch_event("$test_evt_11_8:nebu.local", _room_id) do
      {:ok, %{
        "event_id"         => "$test_evt_11_8:nebu.local",
        "room_id"          => "!get_event_test_11_8:nebu.local",
        "sender"           => "@kai:nebu.local",
        "event_type"       => "m.room.message",
        "content"          => %{"msgtype" => "m.text", "body" => "hello from 11-8"},
        "origin_server_ts" => 1_700_000_000_000,
        "state_key"        => ""
      }}
    end

    def fetch_event(_event_id, _room_id), do: {:error, :not_found}
  end

  # ─── FakeEventDBError ─────────────────────────────────────────────────────────
  # Returns a DB error for the event fetch to exercise the internal-error branch.

  defmodule FakeEventDBError do
    def load_members(_room_id),              do: {:error, :not_found}
    def insert_room(_room_id),               do: {:ok, System.system_time(:millisecond)}
    def insert_member(_room_id, _user_id),   do: :ok
    def delete_member(_room_id, _user_id),   do: :ok
    def insert_event(_event),                do: :ok
    def set_power_levels(_room_id, _levels), do: :ok
    def get_rooms_for_user(_user_id),        do: {:ok, []}
    def get_recently_left_rooms_for_user(_), do: {:ok, []}
    def fetch_events(_r, _d, _l, _t),        do: {:ok, [], "", ""}
    def fetch_events_since(_r, _l_id, _lim), do: {:ok, []}
    def get_event_timestamp(_event_id),      do: {:error, :not_found}
    def get_room_name(_room_id),             do: {:ok, nil}
    def load_room_settings(_room_id),        do: {:ok, 0}
    def get_room_status(_room_id),           do: {:ok, "active"}
    def check_room_status_for_update(_),     do: {:ok, "active"}
    def fetch_event(_event_id, _room_id),    do: {:error, :db_connection_lost}
  end

  # ─── Helpers ─────────────────────────────────────────────────────────────────

  defp fake_stream(user_id) do
    %{http_request_headers: %{"x-user-id" => user_id}}
  end

  defp make_request(room_id, event_id) do
    %Core.GetEventRequest{room_id: room_id, event_id: event_id}
  end

  defp start_and_track_room(room_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:ok, pid} -> Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
          _          -> :ok
        end
      end
    end)

    :ok
  end

  # ─── Setup ───────────────────────────────────────────────────────────────────

  setup do
    Application.put_env(:room_manager, :db_module, FakeRoomDB)
    Application.put_env(:event_dispatcher, :messages_db_module, FakeEventDB)

    case :pg.start_link() do
      {:ok, _pid}                -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok = start_and_track_room(@room_id)
    :ok = Nebu.Room.Server.join(@room_id, @user_id)

    :ok
  end

  # ─── AC1: happy path returns the event ───────────────────────────────────────

  test "get_event returns GetEventResponse with the target event" do
    req  = make_request(@room_id, @event_id)
    resp = Server.get_event(req, fake_stream(@user_id))

    assert %Core.GetEventResponse{} = resp
    refute is_nil(resp.event)

    event = resp.event
    assert event.event_id   == @event_id
    assert event.room_id    == @room_id
    assert event.sender_id  == @user_id
    assert event.event_type == "m.room.message"
    assert event.origin_ts  == 1_700_000_000_000

    {:ok, content} = Jason.decode(event.content)
    assert content["body"] == "hello from 11-8"
  end

  # ─── AC2: unknown event_id returns NOT_FOUND ─────────────────────────────────

  test "get_event raises NOT_FOUND for unknown event_id" do
    req = make_request(@room_id, @unknown_id)

    assert_raise GRPC.RPCError, fn ->
      Server.get_event(req, fake_stream(@user_id))
    end
  end

  # ─── AC3: non-member returns PERMISSION_DENIED ───────────────────────────────

  test "get_event raises PERMISSION_DENIED for non-member" do
    req = make_request(@room_id, @event_id)

    error = assert_raise GRPC.RPCError, fn ->
      Server.get_event(req, fake_stream(@non_member))
    end

    assert error.status == GRPC.Status.permission_denied()
  end

  # ─── Room not found returns NOT_FOUND ────────────────────────────────────────

  test "get_event raises NOT_FOUND for unknown room" do
    req = make_request("!nonexistent_room:nebu.local", @event_id)

    error = assert_raise GRPC.RPCError, fn ->
      Server.get_event(req, fake_stream(@user_id))
    end

    assert error.status == GRPC.Status.not_found()
  end

  # ─── DB error returns INTERNAL ───────────────────────────────────────────────

  test "get_event raises INTERNAL when fetch_event returns DB error" do
    Application.put_env(:event_dispatcher, :messages_db_module, FakeEventDBError)

    req = make_request(@room_id, "$any_event_id")

    error = assert_raise GRPC.RPCError, fn ->
      Server.get_event(req, fake_stream(@user_id))
    end

    assert error.status == GRPC.Status.internal()
  end
end
