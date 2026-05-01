defmodule Nebu.EventDispatcher.ServerModerationMetadataTest do
  use ExUnit.Case, async: false

  # ─── Story 7-32: Fix moderation gRPC handlers — caller_id from trusted metadata ─
  #
  # These tests are written FIRST (ATDD red-phase gate), before the fix is applied.
  # ALL "metadata identity wins" tests in this module are expected to FAIL until
  # Story 7-32 is implemented, because the current code reads caller_id from
  # request.caller_id (body) instead of Nebu.Grpc.Metadata.trusted_identity(stream).
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS tables are all process-global resources.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.kick_user/2 and ban_user/2 directly
  #     (synchronous unary handlers).
  #   - Fake stream: minimal map with http_request_headers containing x-user-id and
  #     x-system-role (matches Nebu.Grpc.Metadata.trusted_identity/1 contract).
  #   - The critical scenario: request.caller_id = "@victim:test.local" (body, power 100)
  #     but stream metadata x-user-id = "@attacker:test.local" (power 0, below threshold).
  #   - If the handler uses body caller_id → permission granted (wrong: @victim has power 100).
  #   - If the handler uses metadata identity → permission denied (correct: @attacker has power 0).
  #   - The tests assert permission_denied, so they FAIL on the current (body) implementation
  #     and PASS after the fix (metadata).
  #   - Power levels are inserted into ETS *before* start_room so the GenServer reads
  #     them on init — no reload function needed.
  #   - FakeRoomDB is also used as the messages_db_module to capture insert_event calls.

  alias Nebu.EventDispatcher.Server

  # ─── NoOpAuditWriter ─────────────────────────────────────────────────────────
  #
  # Prevents dependency on Nebu.Repo when moderation handlers emit audit events.
  # Signature matches Compliance.AuditWriter.log/6-7.

  defmodule NoOpAuditWriter do
    def log(_, _, _, _, _, _, _ \\ nil), do: :ok
  end

  # ─── FakeRoomDB ──────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying Nebu.Room.DB behaviour.
  # Also used as the messages_db_module to avoid PostgreSQL dependency.
  # Pattern follows server_set_typing_test.exs exactly.

  defmodule FakeRoomDB do
    def load_members(room_id) do
      case :ets.lookup(:moderation_metadata_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:moderation_metadata_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:moderation_metadata_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:moderation_metadata_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:moderation_metadata_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:moderation_metadata_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:moderation_metadata_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:moderation_metadata_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create ETS table for FakeRoomDB.
    # Guard against stale tables from --watch reruns.
    if :ets.info(:moderation_metadata_test_db) != :undefined do
      :ets.delete(:moderation_metadata_test_db)
    end

    :ets.new(:moderation_metadata_test_db, [:named_table, :public, :set])

    # Inject fake DB module for Room GenServer init.
    Application.put_env(:room_manager, :db_module, FakeRoomDB)

    # Inject fake messages DB to avoid PostgreSQL for insert_event calls.
    Application.put_env(:event_dispatcher, :messages_db_module, FakeRoomDB)

    # Override server_name for deterministic event IDs.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Inject no-op audit writer so audit calls don't require Nebu.Repo.
    Application.put_env(:compliance, :audit_writer, NoOpAuditWriter)

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
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :server_name)
      Application.delete_env(:compliance, :audit_writer)

      if :ets.info(:moderation_metadata_test_db) != :undefined do
        :ets.delete(:moderation_metadata_test_db)
      end

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Fake gRPC stream ────────────────────────────────────────────────────────
  #
  # Builds a minimal fake stream satisfying Nebu.Grpc.Metadata.trusted_identity/1.
  # The http_request_headers map is read by get_header/2 in metadata.ex.

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

  # ─── Power-level room setup ───────────────────────────────────────────────────
  #
  # Inserts power levels into ETS BEFORE starting the Room GenServer so that
  # Room.Server.init/1 reads the correct power levels immediately.
  # Members are added after start_room via Room.Server.join/2.
  #
  # members_and_levels: [{user_id, power_level}, ...]
  # kick_threshold and ban_threshold both default to 50 (Matrix default).

  defp setup_room_with_power_levels(room_id, members_and_levels) do
    power_levels_json =
      Jason.encode!(%{
        "users" =>
          Map.new(members_and_levels, fn {uid, level} -> {uid, level} end),
        "kick" => 50,
        "ban" => 50
      })

    # Insert the room row first so load_members finds it (not {:error, :not_found}).
    {:ok, _created_at_ms} = FakeRoomDB.insert_room(room_id)

    # Write power levels to ETS before starting the Room GenServer so that
    # Room.Server.init/1 picks them up via load_members.
    :ok = FakeRoomDB.set_power_levels(room_id, power_levels_json)

    # Now start the Room GenServer (it will pick up the power levels from ETS).
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = start_and_track_room(room_id)

    # Add members via Room.Server.join so state.members is populated.
    Enum.each(members_and_levels, fn {uid, _level} ->
      :ok = Nebu.Room.Server.join(room_id, uid)
    end)

    :ok
  end

  # ─── AC #4: kick_user — metadata identity wins over body caller_id ────────────
  #
  # Security test: the handler MUST use the identity from gRPC stream metadata,
  # not the identity from the request body.
  #
  # Setup:
  #   - @victim:test.local   → power level 100 (admin; would be allowed to kick)
  #   - @attacker:test.local → power level 0   (regular user; NOT allowed to kick)
  #   - @target:test.local   → power level 0   (the kick target)
  #
  # Call:
  #   - request.caller_id = "@victim:test.local"  ← body says victim (power 100)
  #   - stream x-user-id  = "@attacker:test.local" ← metadata says attacker (power 0)
  #
  # Expected:
  #   - GRPC.RPCError with status permission_denied  (attacker's power 0 < kick threshold 50)
  #
  # This test FAILS on the current implementation (which uses request.caller_id = victim,
  # power 100 ≥ 50 → kick succeeds) and PASSES after the fix (metadata = attacker,
  # power 0 < 50 → permission denied).

  describe "Server.kick_user/2 — metadata identity wins over body caller_id" do
    test "uses stream metadata identity (attacker, power 0) not body caller_id (victim, power 100) → permission_denied" do
      room_id  = "!sec-kick-test:test.local"
      victim   = "@victim:test.local"
      attacker = "@attacker:test.local"
      target   = "@target:test.local"

      :ok =
        setup_room_with_power_levels(room_id, [
          {victim, 100},
          {attacker, 0},
          {target, 0}
        ])

      # Build a request where the body caller_id identifies the high-power victim.
      # The stream metadata, however, identifies the low-power attacker.
      # If the handler reads from body → victim has power 100 → kick would succeed.
      # If the handler reads from metadata → attacker has power 0 → permission_denied.
      request = %Core.KickUserRequest{
        room_id:   room_id,
        caller_id: victim,    # body: high-privilege user (should NOT be trusted)
        target_id: target,
        reason:    ""
      }

      stream = build_stream(attacker)  # metadata: low-privilege attacker

      error =
        try do
          Server.kick_user(request, stream)
          flunk(
            "expected GRPC.RPCError to be raised (attacker has power 0 < kick threshold 50), " <>
            "but no exception was raised — this means the handler used request.caller_id " <>
            "(victim, power 100) instead of stream metadata (attacker, power 0)"
          )
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected permission_denied (#{GRPC.Status.permission_denied()}) " <>
             "because metadata identity @attacker has power 0 < kick threshold 50, " <>
             "but got status #{error.status} (message: #{error.message}). " <>
             "This indicates the handler is using the body caller_id (victim, power 100) " <>
             "instead of the trusted stream metadata (attacker, power 0)."
    end
  end

  # ─── AC #5: ban_user — metadata identity wins over body caller_id ─────────────
  #
  # Same security scenario as above, applied to ban_user/2.
  #
  # This test FAILS on the current implementation and PASSES after the fix.

  describe "Server.ban_user/2 — metadata identity wins over body caller_id" do
    test "uses stream metadata identity (attacker, power 0) not body caller_id (victim, power 100) → permission_denied" do
      room_id  = "!sec-ban-test:test.local"
      victim   = "@victim:test.local"
      attacker = "@attacker:test.local"
      target   = "@target:test.local"

      :ok =
        setup_room_with_power_levels(room_id, [
          {victim, 100},
          {attacker, 0},
          {target, 0}
        ])

      # Body caller_id = victim (power 100 → would allow ban).
      # Stream metadata = attacker (power 0 → must be denied).
      request = %Core.BanUserRequest{
        room_id:   room_id,
        caller_id: victim,    # body: high-privilege user (should NOT be trusted)
        target_id: target,
        reason:    ""
      }

      stream = build_stream(attacker)  # metadata: low-privilege attacker

      error =
        try do
          Server.ban_user(request, stream)
          flunk(
            "expected GRPC.RPCError to be raised (attacker has power 0 < ban threshold 50), " <>
            "but no exception was raised — this means the handler used request.caller_id " <>
            "(victim, power 100) instead of stream metadata (attacker, power 0)"
          )
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected permission_denied (#{GRPC.Status.permission_denied()}) " <>
             "because metadata identity @attacker has power 0 < ban threshold 50, " <>
             "but got status #{error.status} (message: #{error.message}). " <>
             "This indicates the handler is using the body caller_id (victim, power 100) " <>
             "instead of the trusted stream metadata (attacker, power 0)."
    end
  end

  # ─── AC #6 (regression): kick_user — happy path with matching identity ─────────
  #
  # Verifies that the fix does not regress the happy path: when both request.caller_id
  # and stream metadata agree on the same moderator identity (power 50), the kick
  # succeeds and returns %Core.KickUserResponse{}.
  #
  # This test should PASS both before and after the fix.

  describe "Server.kick_user/2 — happy path regression (matching identity)" do
    test "moderator with matching body and metadata identity (power 50) can kick target" do
      room_id   = "!kick-happy:test.local"
      moderator = "@moderator:test.local"
      target    = "@target:test.local"

      :ok =
        setup_room_with_power_levels(room_id, [
          {moderator, 50},
          {target, 0}
        ])

      request = %Core.KickUserRequest{
        room_id:   room_id,
        caller_id: moderator,  # body: moderator (power 50 ≥ kick threshold)
        target_id: target,
        reason:    ""
      }

      # Stream metadata also identifies the moderator — no identity mismatch.
      stream = build_stream(moderator)

      response = Server.kick_user(request, stream)

      assert %Core.KickUserResponse{} = response,
             "expected %Core.KickUserResponse{}, got: #{inspect(response)}"
    end
  end
end
