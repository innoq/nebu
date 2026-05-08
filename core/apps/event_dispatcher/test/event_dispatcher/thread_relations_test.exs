defmodule Nebu.EventDispatcher.ThreadRelationsTest do
  use ExUnit.Case, async: false

  # ─── Story 9-28: Thread Relations — get_relations/2 unit tests ───────────────
  #
  # Tests written FIRST (red phase) — get_relations/2 does not exist yet.
  # All tests must fail until:
  #   1. get_relations/2 is added to Nebu.EventDispatcher.Server
  #   2. fetch_events_by_relation/4 is added to Nebu.Room.DB
  #
  # Injection pattern (same as upgrade_room tests):
  #   Application.put_env(:event_dispatcher, :messages_db_module, FakeRelationsDB)
  #   Application.put_env(:event_dispatcher, :rooms_db_module, FakeRoomsDB)
  #   Application.put_env(:event_dispatcher, :room_registry_module, FakeRegistry)
  #
  # async: false — Application.put_env is process-global.

  alias Nebu.EventDispatcher.Server

  @thread_root_id "$root_event_9_28:nebu.local"
  @reply_event_id "$reply_event_9_28:nebu.local"
  @room_id "!thread_room_9_28:nebu.local"
  @user_id "@alex:nebu.local"

  # ─── FakeRoomsDB ─────────────────────────────────────────────────────────────
  # Returns a single joined room for the test user.

  defmodule FakeRoomsDB do
    def get_rooms_for_user(_user_id), do: {:ok, ["!thread_room_9_28:nebu.local"]}
  end

  # ─── FakeRegistry ─────────────────────────────────────────────────────────────
  # Minimal registry: the user is a member of the test room.

  defmodule FakeRegistry do
    def get_state(room_id) do
      %{
        members: MapSet.new(["@alex:nebu.local", "@kai:nebu.local"]),
        power_levels: %{},
        room_id: room_id
      }
    end
  end

  # ─── FakeRelationsDB (happy-path) ────────────────────────────────────────────

  defmodule FakeRelationsDB do
    def fetch_events_by_relation(room_id, event_id, "m.thread", _limit) do
      {:ok, [
        %{
          "event_id"         => "$reply_event_9_28:nebu.local",
          "room_id"          => room_id,
          "sender"           => "@alex:nebu.local",
          "event_type"       => "m.room.message",
          "content"          => %{
            "msgtype" => "m.text",
            "body"    => "First thread reply",
            "m.relates_to" => %{
              "rel_type" => "m.thread",
              "event_id" => event_id
            }
          },
          "origin_server_ts" => 1_000_001,
          "state_key"        => ""
        }
      ]}
    end

    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?("$root_event_9_28:nebu.local", _room_id), do: true
    def event_in_room?(_event_id, _room_id), do: false
    def insert_event(_event), do: :ok
    def fetch_events_since(_room_id, _last_id, _limit), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _token), do: {:ok, [], "", ""}
  end

  # ─── FakeRelationsDBEmpty ────────────────────────────────────────────────────

  defmodule FakeRelationsDBEmpty do
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?("$root_event_9_28:nebu.local", _room_id), do: true
    def event_in_room?(_event_id, _room_id), do: false
    def insert_event(_event), do: :ok
    def fetch_events_since(_room_id, _last_id, _limit), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _token), do: {:ok, [], "", ""}
  end

  # ─── helpers ─────────────────────────────────────────────────────────────────

  defp make_request(room_id, event_id, rel_type, user_id, limit \\ 20) do
    %Core.GetRelationsRequest{
      room_id:  room_id,
      event_id: event_id,
      rel_type: rel_type,
      user_id:  user_id,
      limit:    limit
    }
  end

  defp fake_stream(user_id) do
    %{http_request_headers: %{"x-user-id" => user_id}}
  end

  setup do
    Application.put_env(:event_dispatcher, :messages_db_module, FakeRelationsDB)
    Application.put_env(:event_dispatcher, :rooms_db_module, FakeRoomsDB)
    Application.put_env(:event_dispatcher, :room_registry_module, FakeRegistry)

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :rooms_db_module)
      Application.delete_env(:event_dispatcher, :room_registry_module)
    end)

    :ok
  end

  # ─── AC1: returns thread reply events ─────────────────────────────────────────

  test "get_relations returns m.thread child events for thread root" do
    req = make_request(@room_id, @thread_root_id, "m.thread", @user_id)
    resp = Server.get_relations(req, fake_stream(@user_id))

    assert %Core.GetRelationsResponse{} = resp
    assert length(resp.events) == 1
    [event] = resp.events
    assert event.event_id == @reply_event_id
    assert event.event_type == "m.room.message"
    assert event.room_id == @room_id
  end

  # ─── AC2: empty chunk when no thread replies ──────────────────────────────────

  test "get_relations returns empty list when no thread replies exist" do
    Application.put_env(:event_dispatcher, :messages_db_module, FakeRelationsDBEmpty)

    req = make_request(@room_id, @thread_root_id, "m.thread", @user_id)
    resp = Server.get_relations(req, fake_stream(@user_id))

    assert %Core.GetRelationsResponse{} = resp
    assert resp.events == []
  end

  # ─── AC4: non-member gets PERMISSION_DENIED ────────────────────────────────────

  test "get_relations raises PERMISSION_DENIED for non-member" do
    non_member = "@stranger:nebu.local"
    req = make_request(@room_id, @thread_root_id, "m.thread", non_member)

    assert_raise GRPC.RPCError, fn ->
      Server.get_relations(req, fake_stream(non_member))
    end
  end
end
