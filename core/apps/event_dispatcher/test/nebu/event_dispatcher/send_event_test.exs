defmodule Nebu.EventDispatcher.SendEventTest do
  use ExUnit.Case, async: false

  # ─── Story 4-11: Elixir send_event/2 gRPC handler ────────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-11 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS :NebuTxnDedup table are all process-global resources.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.send_event/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map with http_request_headers (matches real gRPC
  #     stream contract used by other handlers in this module).
  #   - DB injection: Room.Server.init/1 uses Application.get_env(:room_manager, :db_module).
  #     We inject FakeDB (same pattern as create_room_test.exs) to avoid PostgreSQL.
  #   - Rooms used in tests are started via Nebu.Room.RoomSupervisor.start_room/1
  #     and tracked with on_exit cleanup via Horde.DynamicSupervisor.terminate_child/2
  #     to prevent cross-test Horde pollution.
  #   - Creators are joined to their rooms before send_event is tested — the
  #     Elixir handler checks room membership before delegating to Room.Server.
  #   - Idempotency test: calls send_event/2 twice with the same txn_id and
  #     asserts both return the same event_id using the REAL :NebuTxnDedup ETS
  #     table (the table that Room.Server.send_event/5 uses for dedup — Story 4-4).
  #   - :NebuTxnDedup is cleared before each test to prevent cross-test leakage.

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeDB).
  # No PostgreSQL connection is required.

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:send_event_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:send_event_test_db, {{:member, room_id, :"$1"}, :active})

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:send_event_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:send_event_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:send_event_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:send_event_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 — returns {:ok, "active"} so normal rooms start correctly.
    def get_room_status(_room_id), do: {:ok, "active"}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create the ETS table for FakeDB (public so Room GenServer processes can
    # access it). Guard against stale tables from --watch reruns.
    if :ets.info(:send_event_test_db) != :undefined do
      :ets.delete(:send_event_test_db)
    end

    :ets.new(:send_event_test_db, [:named_table, :public, :set])

    # Inject fake DB so Room GenServers don't need PostgreSQL.
    Application.put_env(:room_manager, :db_module, FakeDB)

    # Override server_name for deterministic room_id assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # :NebuTxnDedup is a named ETS table created at Application boot
    # (Nebu.Room.Application). It CANNOT be deleted and recreated between tests.
    # Clear all entries before each test to prevent idempotency state leakage.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      # Remove Application overrides.
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :server_name)

      # Drop the ETS table created for this test (guard against double-delete).
      if :ets.info(:send_event_test_db) != :undefined do
        :ets.delete(:send_event_test_db)
      end

      # Clear :NebuTxnDedup entries left by this test.
      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ─────────────────────────────────────────────────────────

  defp build_stream do
    %{http_request_headers: %{}}
  end

  # ─── Room GenServer tracking ──────────────────────────────────────────────────
  #
  # Registers an on_exit callback to stop the Room GenServer for room_id via
  # Horde.DynamicSupervisor.terminate_child/2 — the correct Horde-level
  # termination (matches join_room_test.exs pattern from Story 4-10).

  defp start_and_track_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, _pid} ->
        on_exit(fn ->
          if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
            case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
              {:ok, pid} ->
                Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)

              _ ->
                :ok
            end
          end
        end)

        :ok

      error ->
        error
    end
  end

  # ─── Helper: start a room and join the creator ────────────────────────────────
  #
  # Sets up the pre-condition for send_event tests: a running room with the
  # given user already joined as a member.

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ok
  end

  # ─── AT #7: send_event happy path → SendEventResponse with $ event_id ─────────
  #
  # AC #11 — send_event/2 calls Nebu.Room.Server.send_event/5 and returns
  # %Core.SendEventResponse{event_id: event_id} where event_id starts with "$".
  #
  # Given: Room GenServer running for "!sendtest:test.local", @alice is a member
  # When: Server.send_event/2 is called with a valid SendEventRequest
  # Then: returns %Core.SendEventResponse{event_id: event_id}
  #       where event_id starts with "$" (content-hash format, Story 4-4)

  describe "Server.send_event/2 — happy path" do
    test "returns SendEventResponse with a $-prefixed event_id" do
      room_id = "!sendtest:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)

      content_bytes = Jason.encode!(%{"msgtype" => "m.text", "body" => "hello"})

      request = %Core.SendEventRequest{
        room_id: room_id,
        sender_id: alice,
        event_type: "m.room.message",
        txn_id: "txn-happy-1",
        content: content_bytes,
        origin_ts: System.system_time(:millisecond)
      }

      response = Server.send_event(request, build_stream())

      assert %Core.SendEventResponse{} = response
      assert is_binary(response.event_id)
      assert response.event_id != "",
             "expected a non-empty event_id in SendEventResponse"
      assert String.starts_with?(response.event_id, "$"),
             "expected event_id to start with '$', got: #{response.event_id}"
    end
  end

  # ─── AT #8: room not found → GRPC.RPCError NOT_FOUND ─────────────────────────
  #
  # AC #12 — when no Room GenServer is running for the given room_id,
  # send_event/2 must raise GRPC.RPCError with status not_found.
  #
  # Given: no Room GenServer running for "!ghost:test.local"
  # When: Server.send_event/2 is called
  # Then: raises %GRPC.RPCError{status: GRPC.Status.not_found()}

  describe "Server.send_event/2 — room not found" do
    test "raises GRPC.RPCError with not_found status when room does not exist" do
      content_bytes = Jason.encode!(%{"msgtype" => "m.text", "body" => "hello"})

      request = %Core.SendEventRequest{
        room_id: "!ghost:test.local",
        sender_id: "@alice:test.local",
        event_type: "m.room.message",
        txn_id: "txn-notfound-1",
        content: content_bytes,
        origin_ts: System.system_time(:millisecond)
      }

      error =
        try do
          Server.send_event(request, build_stream())
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.not_found(),
             "expected status not_found (#{GRPC.Status.not_found()}), got: #{error.status}"
    end
  end

  # ─── AT #9: sender is not a room member → GRPC.RPCError PERMISSION_DENIED ─────
  #
  # AC #12 — when the sender is not a member of the room, send_event/2 must
  # raise GRPC.RPCError with status permission_denied.
  #
  # Given: Room GenServer running for "!membtest:test.local",
  #        @bob:test.local is NOT a member
  # When: Server.send_event/2 is called with sender_id "@bob:test.local"
  # Then: raises %GRPC.RPCError{status: GRPC.Status.permission_denied()}

  describe "Server.send_event/2 — sender not a member" do
    test "raises GRPC.RPCError with permission_denied when sender is not a member" do
      room_id = "!membtest:test.local"
      alice = "@alice:test.local"
      bob = "@bob:test.local"

      # Start the room and join only alice — bob is deliberately excluded.
      :ok = setup_room_with_member(room_id, alice)

      content_bytes = Jason.encode!(%{"msgtype" => "m.text", "body" => "unauthorized"})

      request = %Core.SendEventRequest{
        room_id: room_id,
        sender_id: bob,
        event_type: "m.room.message",
        txn_id: "txn-perm-1",
        content: content_bytes,
        origin_ts: System.system_time(:millisecond)
      }

      error =
        try do
          Server.send_event(request, build_stream())
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected status permission_denied (#{GRPC.Status.permission_denied()}), got: #{error.status}"
    end
  end

  # ─── MINOR-3: {:error, reason} internal-error path ──────────────────────────
  #
  # AC #12 — when Nebu.Room.Server.send_event/5 returns {:error, reason},
  # send_event/2 must raise GRPC.RPCError with status internal.
  #
  # INFO — IMPLEMENTATION GAP (MINOR):
  # The current implementation in Nebu.EventDispatcher.Server.send_event/2
  # (line ~49 of server.ex) calls `Nebu.Room.Server.send_event/5` directly
  # (hardcoded — not via the injectable `room_registry_module/0` helper).
  # This means the {:error, reason} branch CANNOT be reached in a unit test
  # without either:
  #   (a) refactoring send_event to call room_registry_module().send_event/5
  #       (consistent with how get_state/1 is already injected), or
  #   (b) using a full integration test with a real room that simulates a DB
  #       failure mid-send (complex, slow).
  #
  # Recommended fix (Phase 2): extend room_registry_module/0 to also wrap
  # send_event calls so the error path is testable without PostgreSQL.
  # Until that refactor lands, this describe block is intentionally skipped.
  #
  # @tag :skip prevents ExUnit from running the placeholder test below and
  # keeps the test suite green without hiding the coverage gap.

  describe "Server.send_event/2 — internal error from Room.Server" do
    @tag :skip
    test "returns GRPC internal error when Room.Server.send_event returns {:error, reason}" do
      # Skipped — see INFO note above.
      # To enable: refactor server.ex to call room_registry_module().send_event/5
      # and inject a fake module that returns {:error, :db_failure}.
      flunk("not yet testable without injecting send_event via room_registry_module")
    end
  end

  # ─── AT #10: duplicate txn_id → same event_id (idempotent) ───────────────────
  #
  # AC #9 — calling send_event/2 twice with the same {room_id, sender_id, txn_id}
  # must return the same event_id both times (no new event persisted).
  # This test uses the REAL :NebuTxnDedup ETS table — the same table that
  # Nebu.Room.Server.send_event/5 uses for idempotency (Story 4-4, ADR-002).
  #
  # Given: Room GenServer running, @alice is a member; first send_event succeeded
  # When: send_event/2 is called again with the same txn_id
  # Then: second response.event_id == first response.event_id (no error, no new event)

  describe "Server.send_event/2 — txn_id idempotency" do
    test "returns the same event_id on duplicate txn_id (real :NebuTxnDedup ETS)" do
      room_id = "!idempotent:test.local"
      alice = "@alice:test.local"

      :ok = setup_room_with_member(room_id, alice)

      content_bytes = Jason.encode!(%{"msgtype" => "m.text", "body" => "idempotent message"})

      request = %Core.SendEventRequest{
        room_id: room_id,
        sender_id: alice,
        event_type: "m.room.message",
        txn_id: "txn-dedup-1",
        content: content_bytes,
        origin_ts: System.system_time(:millisecond)
      }

      # First call — creates the event.
      response1 = Server.send_event(request, build_stream())

      assert %Core.SendEventResponse{} = response1
      assert String.starts_with?(response1.event_id, "$"),
             "first response event_id must start with '$'"

      # Second call — same txn_id must return the same event_id (ETS dedup hit).
      response2 = Server.send_event(request, build_stream())

      assert %Core.SendEventResponse{} = response2

      assert response2.event_id == response1.event_id,
             "idempotency violation: first call returned #{response1.event_id}, " <>
               "second call returned #{response2.event_id} — expected same event_id"

      # Verify only ONE message event was persisted to FakeDB (not two).
      # Exclude m.room.member state events emitted by setup (emit_membership_event).
      all_entries = :ets.match_object(:send_event_test_db, {{:event, :_}, :_})
      message_events = Enum.reject(all_entries, fn {{:event, _}, ev} -> ev["type"] == "m.room.member" end)

      assert length(message_events) == 1,
             "expected exactly 1 message event in FakeDB after duplicate send, got #{length(message_events)}"
    end
  end
end
