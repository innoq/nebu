defmodule Nebu.EventDispatcher.ServerReceiptsTest do
  use ExUnit.Case, async: false

  # ─── Story 4-17: Elixir send_receipt/2 gRPC handler ──────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-17 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS tables are all process-global resources.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.send_receipt/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map with http_request_headers that includes
  #     x-user-id and x-system-role metadata (matches Nebu.Grpc.Metadata.trusted_identity/1
  #     contract used by the implementation — see send_event, join_room, etc.).
  #   - DB injection (Room): Room.Server.init/1 uses
  #     Application.get_env(:room_manager, :db_module). Inject FakeRoomDB.
  #   - DB injection (receipts): send_receipt/2 uses
  #     Application.get_env(:event_dispatcher, :receipt_db_module, Nebu.Receipt.DB).
  #     Inject FakeReceiptDB, which captures upsert_receipt/4 call args in an
  #     ETS table for assertion.
  #   - Rooms are started via Nebu.Room.RoomSupervisor.start_room/1 and tracked
  #     with on_exit cleanup (Horde.DynamicSupervisor.terminate_child/2).
  #   - The non-member test: starts a room but does NOT add @bob to members.

  alias Nebu.EventDispatcher.Server

  # ─── FakeRoomDB ──────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeRoomDB).

  defmodule FakeRoomDB do
    def load_members(room_id) do
      case :ets.lookup(:receipts_test_room_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:receipts_test_room_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:receipts_test_room_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:receipts_test_room_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:receipts_test_room_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:receipts_test_room_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:receipts_test_room_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:receipts_test_room_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 — returns {:ok, "active"} so normal rooms start correctly.
    def get_room_status(_room_id), do: {:ok, "active"}
  end

  # ─── FakeReceiptDB ────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Receipt.DB interface.
  # Injected via Application.put_env(:event_dispatcher, :receipt_db_module, FakeReceiptDB).
  # Captures all upsert_receipt/4 calls for assertion.
  # Returns :ok unconditionally (no FK validation — keeps tests focused on handler logic).

  defmodule FakeReceiptDB do
    def upsert_receipt(room_id, user_id, receipt_type, event_id) do
      :ets.insert(
        :receipts_test_receipt_db,
        {:receipt, room_id, user_id, receipt_type, event_id}
      )

      :ok
    end
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create ETS tables for FakeRoomDB and FakeReceiptDB.
    # Guard against stale tables from --watch reruns.
    Enum.each([:receipts_test_room_db, :receipts_test_receipt_db], fn table ->
      if :ets.info(table) != :undefined do
        :ets.delete(table)
      end
    end)

    :ets.new(:receipts_test_room_db, [:named_table, :public, :set])
    :ets.new(:receipts_test_receipt_db, [:named_table, :public, :bag])

    # Inject fake DB modules.
    Application.put_env(:room_manager, :db_module, FakeRoomDB)
    Application.put_env(:event_dispatcher, :receipt_db_module, FakeReceiptDB)

    # Override server_name for deterministic assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # Clear :NebuTxnDedup between tests.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :receipt_db_module)
      Application.delete_env(:event_dispatcher, :server_name)

      Enum.each([:receipts_test_room_db, :receipts_test_receipt_db], fn table ->
        if :ets.info(table) != :undefined do
          :ets.delete(table)
        end
      end)

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ─────────────────────────────────────────────────────────
  #
  # Builds a minimal fake stream that satisfies Nebu.Grpc.Metadata.trusted_identity/1.
  # The send_receipt/2 handler uses trusted_identity/1 to extract user_id from
  # stream metadata (x-user-id header). The user_id in the stream must match
  # the request.user_id for membership checks.

  defp build_stream(user_id, system_role \\ "user") do
    %{
      http_request_headers: %{
        "x-user-id" => user_id,
        "x-system-role" => system_role
      }
    }
  end

  # ─── Room GenServer tracking ──────────────────────────────────────────────────

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

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ok
  end

  # ─── AT #9: send_receipt persists to DB via FakeReceiptDB ────────────────────
  #
  # AC #2 — send_receipt/2 calls receipt_db_module().upsert_receipt/4 with the
  # correct args and returns %Core.SendReceiptResponse{}.
  #
  # Given: Room GenServer running for "!receipttest:test.local" with @alice as member;
  #        FakeReceiptDB injected via receipt_db_module
  # When: Server.send_receipt/2 called with room_id, user_id "@alice:test.local",
  #       receipt_type "m.read", event_id "$event1"
  # Then: FakeReceiptDB.upsert_receipt/4 was called with correct args;
  #       %Core.SendReceiptResponse{} returned

  describe "Server.send_receipt/2 — happy path" do
    test "persists receipt via FakeReceiptDB and returns SendReceiptResponse" do
      room_id = "!receipttest:test.local"
      alice = "@alice:test.local"
      event_id = "$event1"
      receipt_type = "m.read"

      :ok = setup_room_with_member(room_id, alice)

      request = %Core.SendReceiptRequest{
        room_id: room_id,
        user_id: alice,
        receipt_type: receipt_type,
        event_id: event_id
      }

      response = Server.send_receipt(request, build_stream(alice))

      assert %Core.SendReceiptResponse{} = response,
             "expected %Core.SendReceiptResponse{}, got: #{inspect(response)}"

      # Verify FakeReceiptDB captured the upsert call with correct args.
      receipts = :ets.match(:receipts_test_receipt_db, {:receipt, room_id, alice, receipt_type, event_id})

      assert length(receipts) == 1,
             "expected exactly 1 receipt row in FakeReceiptDB for (#{room_id}, #{alice}, #{receipt_type}, #{event_id})"
    end

    # Guards against: upsert_receipt(user_id, room_id, ...) argument swap
    test "upsert_receipt argument ORDER guard - room_id is first, not user_id" do
      room_id = "!receiptargs:test.local"
      alice = "@alice:test.local"
      event_id = "$specificEvent42"
      receipt_type = "m.read"

      :ok = setup_room_with_member(room_id, alice)

      request = %Core.SendReceiptRequest{
        room_id: room_id,
        user_id: alice,
        receipt_type: receipt_type,
        event_id: event_id
      }

      _response = Server.send_receipt(request, build_stream(alice))

      # Exact arg match to guard against argument reordering bugs.
      all_receipts = :ets.tab2list(:receipts_test_receipt_db)

      assert Enum.any?(all_receipts, fn row ->
               row == {:receipt, room_id, alice, receipt_type, event_id}
             end),
             "expected upsert_receipt/4 called with " <>
               "(#{room_id}, #{alice}, #{receipt_type}, #{event_id}), " <>
               "got: #{inspect(all_receipts)}"
    end
  end

  # ─── AT #10: send_receipt non-member → GRPC.RPCError PERMISSION_DENIED ────────
  #
  # AC #2 — when the user is not a member of the room, send_receipt/2 must raise
  # GRPC.RPCError with status permission_denied. FakeReceiptDB must NOT be called.
  #
  # Given: Room GenServer running for "!receiptperm:test.local";
  #        @bob:test.local is NOT a member
  # When: Server.send_receipt/2 called for @bob
  # Then: raises %GRPC.RPCError{status: GRPC.Status.permission_denied()};
  #       FakeReceiptDB.upsert_receipt/4 was NOT called

  describe "Server.send_receipt/2 — not a room member" do
    test "raises GRPC.RPCError permission_denied when user is not a member" do
      room_id = "!receiptperm:test.local"
      alice = "@alice:test.local"
      bob = "@bob:test.local"

      # Start the room and join only alice — bob is deliberately excluded.
      :ok = setup_room_with_member(room_id, alice)

      request = %Core.SendReceiptRequest{
        room_id: room_id,
        user_id: bob,
        receipt_type: "m.read",
        event_id: "$event1"
      }

      error =
        try do
          Server.send_receipt(request, build_stream(bob))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected status permission_denied (#{GRPC.Status.permission_denied()}), got: #{error.status}"

      # Verify FakeReceiptDB was NOT called (no row inserted).
      receipts = :ets.tab2list(:receipts_test_receipt_db)

      assert receipts == [],
             "expected FakeReceiptDB to be empty (not called), got: #{inspect(receipts)}"
    end
  end

  # ─── Room not found → GRPC.RPCError NOT_FOUND ────────────────────────────────
  #
  # AC #2 — when no Room GenServer is running for the given room_id,
  # send_receipt/2 must raise GRPC.RPCError with status not_found.
  #
  # Given: no Room GenServer running for "!ghostreceipt:test.local"
  # When: Server.send_receipt/2 called
  # Then: raises %GRPC.RPCError{status: GRPC.Status.not_found()}

  describe "Server.send_receipt/2 — room not found" do
    test "raises GRPC.RPCError not_found when room does not exist" do
      request = %Core.SendReceiptRequest{
        room_id: "!ghostreceipt:test.local",
        user_id: "@alice:test.local",
        receipt_type: "m.read",
        event_id: "$event1"
      }

      error =
        try do
          Server.send_receipt(request, build_stream("@alice:test.local"))
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.not_found(),
             "expected status not_found (#{GRPC.Status.not_found()}), got: #{error.status}"
    end
  end
end
