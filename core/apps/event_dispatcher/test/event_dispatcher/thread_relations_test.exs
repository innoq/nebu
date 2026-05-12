defmodule Nebu.EventDispatcher.ThreadRelationsTest do
  use ExUnit.Case, async: false

  # ─── Story 9-28: Thread Relations — get_relations/2 unit tests ───────────────
  # ─── Story 9-30: Postgrex.JSONB regression guard ─────────────────────────────
  #
  # These tests are GREEN after their respective stories were implemented.
  # To verify a regression: revert event_map_to_proto/1 in server.ex and the
  # Story 9-30 tests will fail with Protocol.UndefinedError (via Jason.encode!).
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

  # Stand-in for %Postgrex.JSONB{decoded: map} — Postgrex is not a direct dep of
  # event_dispatcher, so we cannot use the real struct at compile time in tests.
  # Production code matches by shape (is_struct + :decoded field), not module name,
  # so this stand-in correctly exercises the fix path in event_map_to_proto/1.
  defmodule FakePostgrexJSONB do
    defstruct [:decoded]
  end

  defmodule FakeRelationsDB do
    def fetch_events_by_relation(room_id, event_id, "m.thread", _limit, _opts) do
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

    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?("$root_event_9_28:nebu.local", _room_id), do: true
    def event_in_room?(_event_id, _room_id), do: false
    def insert_event(_event), do: :ok
    def fetch_events_since(_room_id, _last_id, _limit), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _token), do: {:ok, [], "", ""}
  end

  # ─── FakeRelationsDBEmpty ────────────────────────────────────────────────────

  defmodule FakeRelationsDBEmpty do
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?("$root_event_9_28:nebu.local", _room_id), do: true
    def event_in_room?(_event_id, _room_id), do: false
    def insert_event(_event), do: :ok
    def fetch_events_since(_room_id, _last_id, _limit), do: {:ok, []}
    def fetch_events(_room_id, _dir, _limit, _token), do: {:ok, [], "", ""}
  end

  # ─── FakeRelationsDBWithJSONB ─────────────────────────────────────────────────
  #
  # Story 9-30 regression guard: uses FakePostgrexJSONB (same shape as Postgrex.JSONB)
  # to simulate Postgrex returning a JSONB column as a struct instead of a plain map.
  # Production code matches by shape (is_struct + :decoded field) so the stand-in
  # exercises the identical fix path without needing Postgrex as a direct dep.
  #
  # Without the fix in event_map_to_proto/1, Jason.encode!(%Postgrex.JSONB{...})
  # raises Protocol.UndefinedError → gRPC INTERNAL → HTTP 500.

  defmodule FakeRelationsDBWithJSONB do
    def fetch_events_by_relation(room_id, event_id, "m.thread", _limit, _opts) do
      jsonb_content = %Nebu.EventDispatcher.ThreadRelationsTest.FakePostgrexJSONB{
        decoded: %{
          "msgtype" => "m.text",
          "body"    => "Thread reply with JSONB content",
          "m.relates_to" => %{
            "rel_type" => "m.thread",
            "event_id" => event_id
          }
        }
      }

      {:ok, [
        %{
          "event_id"         => "$reply_jsonb_9_30:nebu.local",
          "room_id"          => room_id,
          "sender"           => "@alex:nebu.local",
          "event_type"       => "m.room.message",
          "content"          => jsonb_content,
          "origin_server_ts" => 1_000_002,
          "state_key"        => ""
        }
      ]}
    end

    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit, _opts), do: {:ok, []}
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

  # ─── Story 9-30: Postgrex.JSONB regression guard ─────────────────────────────
  #
  # Root cause: Postgrex returns JSONB columns as %Postgrex.JSONB{decoded: map}.
  # event_map_to_proto/1 passed the struct to Jason.encode! which has no encoder
  # → Protocol.UndefinedError → gRPC INTERNAL → HTTP 500 on GET /relations.
  #
  # Fix (applied in server.ex): shape-based guard before the is_map branch:
  #   is_struct(raw) and Map.has_key?(raw, :decoded) and is_map(raw.decoded) -> raw.decoded
  # Shape-based (not module-name-based) because Postgrex is not a direct dep of
  # event_dispatcher, so is_struct(raw, Postgrex.JSONB) fails at compile time here.
  #
  # FakePostgrexJSONB is a local stand-in with identical shape (%{decoded: map})
  # used here to keep the unit test isolated from Postgrex internals.
  # Regression test: revert the is_struct branch in server.ex and this test
  # will fail with Protocol.UndefinedError.

  describe "Story 9-30 — Postgrex.JSONB regression: event_map_to_proto handles JSONB struct" do
    test "get_relations does not crash when content is a Postgrex.JSONB-style struct" do
      Application.put_env(:event_dispatcher, :messages_db_module, FakeRelationsDBWithJSONB)

      req = make_request(@room_id, @thread_root_id, "m.thread", @user_id)

      # Before the fix: raises Protocol.UndefinedError (via Jason.encode!) which is
      # caught by GRPC.Server and re-raised as GRPC.RPCError with status INTERNAL.
      # After the fix: returns a valid GetRelationsResponse with encoded JSON content.
      resp = Server.get_relations(req, fake_stream(@user_id))

      assert %Core.GetRelationsResponse{} = resp
      assert length(resp.events) == 1
      [event] = resp.events
      assert event.event_id == "$reply_jsonb_9_30:nebu.local"
      assert event.event_type == "m.room.message"

      # The content field must be valid JSON (the decoded map re-encoded by Jason).
      # Before the fix: content is nil or raises before reaching this point.
      assert is_binary(event.content), "event.content must be a JSON string, got: #{inspect(event.content)}"

      {:ok, decoded} = Jason.decode(event.content)
      assert decoded["msgtype"] == "m.text",
             "expected content.msgtype == 'm.text', got: #{inspect(decoded)}"
      assert decoded["body"] == "Thread reply with JSONB content",
             "expected content.body to match, got: #{inspect(decoded)}"
    end

  end
end
