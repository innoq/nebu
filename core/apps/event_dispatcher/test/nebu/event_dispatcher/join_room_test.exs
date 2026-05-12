defmodule Nebu.EventDispatcher.JoinRoomTest do
  use ExUnit.Case, async: false

  # ─── Story 4-10: Elixir join_room/2 gRPC handler ─────────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-10 is implemented.
  #
  # async: false — Horde.Registry, Horde.DynamicSupervisor, Application.put_env,
  # and the ETS :NebuTxnDedup table are all process-global resources.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.join_room/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map matching the real gRPC stream contract.
  #   - DB injection: Room.Server.init/1 uses Application.get_env(:room_manager, :db_module).
  #     We inject FakeDB (same pattern as create_room_test.exs) to avoid PostgreSQL.
  #   - Rooms used in tests are started via Nebu.Room.RoomSupervisor.start_room/1
  #     and tracked with on_exit cleanup to prevent cross-test Horde pollution.
  #   - The :NebuTxnDedup ETS table is cleared before each test.
  #   - join_room idempotency: calling join/2 on an already-member user must
  #     return {:ok, ...} (not an error) — the handler must propagate this as
  #     JoinRoomResponse (Matrix spec requirement, AC #7).

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB ──────────────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeDB).
  # No PostgreSQL connection is required.

  # No-op audit writer — Story 5-2 wired Server.join_room/2 to emit an
  # audit log entry; this test does not verify that emission (covered in
  # audit_room_ops_test.exs) and must not depend on Nebu.Repo.
  defmodule NoOpAuditWriter do
    def log(_, _, _, _, _, _, _ \\ nil), do: :ok
  end

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:join_room_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:join_room_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:join_room_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:join_room_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:join_room_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:join_room_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:join_room_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:join_room_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}

    # Story 6.9: get_room_status/1 — returns {:ok, "active"} so normal rooms start correctly.
    def get_room_status(_room_id), do: {:ok, "active"}

    # Story 9-9: TOCTOU fix — returns {:ok, "active"} for normal rooms.
    def check_room_status_for_update(_room_id), do: {:ok, "active"}

    # GAP-LEAVE-UI: required by @behaviour Nebu.Room.DBBehaviour.
    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}
  end

  # ─── FakeInviteDB ─────────────────────────────────────────────────────────────
  #
  # ETS-backed fake for invitation persistence. Injected via
  # Application.put_env(:event_dispatcher, :invite_db_module, FakeInviteDB).
  # No PostgreSQL FK constraint enforcement in tests.

  defmodule FakeInviteDB do
    def insert_invitation(room_id, inviter_id, invitee_id) do
      :ets.insert(:join_room_test_invitations, {room_id, inviter_id, invitee_id})
      :ok
    end

    # Story 5.29d AC1 (FB-E5-03): accept_invitation/2 was missing — added to
    # satisfy the Nebu.Room.InviteDB interface used by Server.join_room/2 when
    # joining an invited room. In join_room_test, tests focus on direct join
    # (not accept-invite path), so we return :ok unconditionally.
    def accept_invitation(_room_id, _invitee_id), do: :ok
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Create the ETS table for FakeDB (public so Room GenServer processes can
    # access it). Guard against stale tables from --watch reruns.
    if :ets.info(:join_room_test_db) != :undefined do
      :ets.delete(:join_room_test_db)
    end

    :ets.new(:join_room_test_db, [:named_table, :public, :set])

    # Create the ETS table for FakeInviteDB.
    if :ets.info(:join_room_test_invitations) != :undefined do
      :ets.delete(:join_room_test_invitations)
    end

    :ets.new(:join_room_test_invitations, [:named_table, :public, :bag])

    # Inject fake DB modules so Room GenServers and invite handler don't need PostgreSQL.
    Application.put_env(:room_manager, :db_module, FakeDB)
    Application.put_env(:event_dispatcher, :invite_db_module, FakeInviteDB)

    # Story 5.29d AC1 (FB-E5-03): also inject FakeDB as the messages_db_module so
    # Server.join_room/2's insert_event call does not hit Nebu.Repo.
    Application.put_env(:event_dispatcher, :messages_db_module, FakeDB)

    # Story 5-2: Server.join_room/2 now calls Compliance.AuditWriter.log/6
    # on the :ok branch. Inject NoOpAuditWriter here so this test does not
    # depend on Nebu.Repo being started in the :test env.
    Application.put_env(:compliance, :audit_writer, NoOpAuditWriter)

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    # Clear :NebuTxnDedup between tests to prevent idempotency state leakage.
    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :invite_db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:compliance, :audit_writer)

      if :ets.info(:join_room_test_db) != :undefined do
        :ets.delete(:join_room_test_db)
      end

      if :ets.info(:join_room_test_invitations) != :undefined do
        :ets.delete(:join_room_test_invitations)
      end

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
  # Registers an on_exit callback to stop the Room GenServer for room_id.
  # Prevents Horde-managed GenServers from accumulating across tests.

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

  # Horde.Registry uses DeltaCrdt which can propagate asynchronously even on a
  # single node. Retry the lookup until the entry appears or we time out.
  defp wait_for_registry(room_id, retries \\ 20) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, pid} when is_pid(pid) ->
        :ok

      _ when retries > 0 ->
        Process.sleep(10)
        wait_for_registry(room_id, retries - 1)

      _ ->
        raise "Horde.Registry did not register #{room_id} within timeout"
    end
  end

  # ─── Helper: start a room and join a user into it directly ───────────────────
  #
  # Uses RoomSupervisor.start_room/1 + Room.Server.join/2 to put the system
  # in the desired pre-condition state without going through the gRPC handler.

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    :ok = wait_for_registry(room_id)
    start_and_track_room(room_id)
    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ok
  end

  # ─── AT #7: join_room happy path → JoinRoomResponse, member list updated ─────
  #
  # AC #10 — join_room/2 looks up the Room GenServer, calls Room.Server.join/2,
  # and returns %Core.JoinRoomResponse{room_id: room_id}.
  # Post-condition: state.members contains the joining user.
  #
  # Given: Room GenServer running for "!jointest:test.local", @alice not yet a member
  # When: Server.join_room/2 is called for @alice
  # Then: returns %Core.JoinRoomResponse{room_id: "!jointest:test.local"} AND
  #       Nebu.Room.Server.get_state/1 shows @alice in members

  describe "Server.join_room/2 — happy path" do
    test "returns JoinRoomResponse and adds user to room members" do
      room_id = "!jointest:test.local"

      # Start the room first (no members yet).
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      start_and_track_room(room_id)

      request = %Core.JoinRoomRequest{
        user_id: "@alice:test.local",
        room_id_or_alias: room_id
      }

      response = Server.join_room(request, build_stream())

      assert %Core.JoinRoomResponse{} = response
      assert response.room_id == room_id

      # Post-condition: @alice is now a member.
      state = Nebu.Room.Server.get_state(room_id)

      assert MapSet.member?(state.members, "@alice:test.local"),
             "expected @alice:test.local in members after join, got: #{inspect(state.members)}"
    end
  end

  # ─── AT #8: join_room room not found → GRPC.RPCError NOT_FOUND ───────────────
  #
  # AC #3 (Elixir side) — when no Room GenServer is running for the given
  # room_id, join_room/2 must raise GRPC.RPCError with status not_found.
  #
  # Given: no Room GenServer running for "!missing:test.local"
  # When: Server.join_room/2 is called
  # Then: raises %GRPC.RPCError{status: GRPC.Status.not_found()}

  describe "Server.join_room/2 — room not found" do
    test "raises GRPC.RPCError with not_found status when room does not exist" do
      request = %Core.JoinRoomRequest{
        user_id: "@alice:test.local",
        room_id_or_alias: "!missing:test.local"
      }

      assert_raise GRPC.RPCError, fn ->
        Server.join_room(request, build_stream())
      end
    end

    test "raised error has not_found status code" do
      request = %Core.JoinRoomRequest{
        user_id: "@alice:test.local",
        room_id_or_alias: "!nonexistent:test.local"
      }

      error =
        try do
          Server.join_room(request, build_stream())
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.not_found(),
             "expected status not_found (#{GRPC.Status.not_found()}), got: #{error.status}"
    end
  end

  # ─── AT #9: join_room already member → idempotent success ────────────────────
  #
  # AC #7 — if the user is already a member of the room, join_room/2 must NOT
  # raise an error. It returns %Core.JoinRoomResponse{room_id: room_id} as per
  # the Matrix spec idempotency requirement.
  #
  # Given: @alice:test.local is already a member of "!idempotent:test.local"
  # When: Server.join_room/2 is called again for @alice
  # Then: returns %Core.JoinRoomResponse{room_id: "!idempotent:test.local"} (no error)

  describe "Server.join_room/2 — idempotent join" do
    test "returns JoinRoomResponse when user is already a member (no error)" do
      room_id = "!idempotent:test.local"
      user_id = "@alice:test.local"

      # Pre-condition: Alice is already a member.
      :ok = setup_room_with_member(room_id, user_id)

      request = %Core.JoinRoomRequest{
        user_id: user_id,
        room_id_or_alias: room_id
      }

      # Must not raise — idempotent join returns success.
      response = Server.join_room(request, build_stream())

      assert %Core.JoinRoomResponse{} = response
      assert response.room_id == room_id
    end

    test "member list still contains user after duplicate join" do
      room_id = "!idempotent2:test.local"
      user_id = "@bob:test.local"

      :ok = setup_room_with_member(room_id, user_id)

      request = %Core.JoinRoomRequest{
        user_id: user_id,
        room_id_or_alias: room_id
      }

      _response = Server.join_room(request, build_stream())

      state = Nebu.Room.Server.get_state(room_id)

      assert MapSet.member?(state.members, user_id),
             "expected #{user_id} to remain in members after duplicate join"

      # Idempotent — member count must not increase.
      assert MapSet.size(state.members) == 1,
             "expected exactly 1 member after duplicate join, got: #{MapSet.size(state.members)}"
    end
  end

  # ─── AT #10: invite_user stores invitation ────────────────────────────────────
  #
  # AC #11 — invite_user/2 validates caller is a member, inserts invitation record,
  # and returns %Core.InviteUserResponse{}.
  #
  # Given: @alice:test.local is a member of "!invite:test.local", @bob is not
  # When: Server.invite_user/2 is called with caller=alice, invitee=bob
  # Then: returns %Core.InviteUserResponse{} and invitation exists in FakeInviteDB

  describe "Server.invite_user/2 — happy path" do
    test "returns InviteUserResponse and stores invitation when caller is a member" do
      room_id = "!invite:test.local"
      alice = "@alice:test.local"
      bob = "@bob:test.local"

      # Pre-condition: alice is already a member.
      :ok = setup_room_with_member(room_id, alice)

      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: alice,
        invitee_id: bob
      }

      response = Server.invite_user(request, build_stream())

      assert %Core.InviteUserResponse{} = response

      # Verify invitation was stored in FakeInviteDB.
      invitations = :ets.lookup(:join_room_test_invitations, room_id)

      assert Enum.any?(invitations, fn {_rid, inviter, invitee} ->
               inviter == alice and invitee == bob
             end),
             "expected invitation for #{bob} from #{alice} in #{room_id}, got: #{inspect(invitations)}"
    end

    test "raises GRPC.RPCError permission_denied if caller is not a member" do
      room_id = "!invite_no_perm:test.local"
      non_member = "@stranger:test.local"
      bob = "@bob:test.local"

      # Start room but do NOT add non_member.
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      start_and_track_room(room_id)

      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: non_member,
        invitee_id: bob
      }

      error =
        try do
          Server.invite_user(request, build_stream())
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected status permission_denied (#{GRPC.Status.permission_denied()}), got: #{error.status}"
    end

    test "raises GRPC.RPCError not_found when room does not exist" do
      request = %Core.InviteUserRequest{
        room_id: "!nonexistent_invite:test.local",
        inviter_id: "@alice:test.local",
        invitee_id: "@bob:test.local"
      }

      error =
        try do
          Server.invite_user(request, build_stream())
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.not_found(),
             "expected status not_found (#{GRPC.Status.not_found()}), got: #{error.status}"
    end
  end

  # ─── MAJOR-1: invite_user — insufficient power level for invite ───────────────
  #
  # AC #6 (Story 4-13) — invite_user/2 checks power levels after membership check.
  # When the room has `invite` threshold set to 50 but the inviter has power level 0,
  # the handler must raise GRPC.RPCError with permission_denied status.
  #
  # Given: Room GenServer with power_levels where "invite" threshold is 50;
  #        @alice:test.local is a member at default power 0
  # When: Server.invite_user/2 called with alice as inviter
  # Then: raises GRPC.RPCError with status: GRPC.Status.permission_denied()

  describe "Server.invite_user/2 — power level check for invite" do
    test "raises GRPC.RPCError permission_denied when inviter lacks invite power level" do
      room_id = "!invite_pl_check:test.local"
      alice = "@alice:test.local"
      bob = "@bob:test.local"

      # Start room and add alice as member.
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, alice)

      # Set power levels so invite threshold is 50 (alice has default power 0).
      # The bootstrapping path allows set_power_levels when power_levels is %{}.
      elevated_pl = %{
        "ban" => 50,
        "kick" => 50,
        "invite" => 50,
        "redact" => 50,
        "state_default" => 50,
        "events_default" => 0,
        "users_default" => 0,
        "users" => %{"@admin:test.local" => 100},
        "events" => %{}
      }

      :ok = Nebu.Room.Server.set_power_levels(room_id, alice, elevated_pl)

      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: alice,
        invitee_id: bob
      }

      error =
        try do
          Server.invite_user(request, build_stream())
          flunk("expected GRPC.RPCError to be raised, but no exception was raised")
        rescue
          e in GRPC.RPCError -> e
        end

      assert error.status == GRPC.Status.permission_denied(),
             "expected status permission_denied (#{GRPC.Status.permission_denied()}), got: #{error.status}"
    end

    test "invite_user succeeds when inviter has sufficient invite power level" do
      room_id = "!invite_pl_allow:test.local"
      admin = "@admin:test.local"
      bob = "@bob:test.local"

      # Start room and add admin as member.
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, admin)

      # Set power levels so invite threshold is 50 and admin has power 100.
      elevated_pl = %{
        "ban" => 50,
        "kick" => 50,
        "invite" => 50,
        "redact" => 50,
        "state_default" => 50,
        "events_default" => 0,
        "users_default" => 0,
        "users" => %{admin => 100},
        "events" => %{}
      }

      :ok = Nebu.Room.Server.set_power_levels(room_id, admin, elevated_pl)

      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: admin,
        invitee_id: bob
      }

      # Admin (power 100) can invite even when threshold is 50.
      response = Server.invite_user(request, build_stream())
      assert %Core.InviteUserResponse{} = response
    end
  end

  # ─── Story 9-19 Fix 1 (GAP-JOIN-PUBLIC): join_room broadcasts {:new_join} ─────
  #
  # AC: When a user joins a public room, join_room/2 must broadcast {:new_join, room_id}
  # to the "user:#{user_id}" :pg group so the long-polling sync task wakes immediately.
  #
  # Given: test process subscribed to "user:@newjoin-user:test.local" :pg group
  # When:  Server.join_room/2 called for @newjoin-user
  # Then:  {:new_join, room_id} received within 200 ms
  #
  # Note: only the actual new join triggers the broadcast — the :already_member branch does NOT.

  describe "Server.join_room/2 — :pg broadcast to joining user (GAP-JOIN-PUBLIC)" do
    test "broadcasts {:new_join, room_id} to user :pg group on successful join" do
      room_id = "!pgbroadcast-#{System.unique_integer([:positive])}:test.local"
      user_id = "@newjoin-user:test.local"

      # Start room.
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      :ok = wait_for_registry(room_id)
      start_and_track_room(room_id)

      # Subscribe to user's :pg group (simulates sync task entering long-poll).
      :ok = :pg.join("user:#{user_id}", self())
      on_exit(fn -> :pg.leave("user:#{user_id}", self()) end)

      request = %Core.JoinRoomRequest{
        user_id: user_id,
        room_id_or_alias: room_id
      }

      Server.join_room(request, build_stream())

      assert_receive {:new_join, ^room_id}, 200,
        "Expected {:new_join, #{room_id}} broadcast from join_room within 200 ms (GAP-JOIN-PUBLIC fix)."
    end

    test "does NOT broadcast {:new_join} when user is already a member (:already_member path)" do
      room_id = "!pgbroadcast-already-#{System.unique_integer([:positive])}:test.local"
      user_id = "@already-joined-user:test.local"

      # Start room and add user as member first.
      {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
      :ok = wait_for_registry(room_id)
      start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, user_id)

      # Subscribe to user's :pg group.
      :ok = :pg.join("user:#{user_id}", self())
      on_exit(fn -> :pg.leave("user:#{user_id}", self()) end)

      request = %Core.JoinRoomRequest{
        user_id: user_id,
        room_id_or_alias: room_id
      }

      # Second join — idempotent, must return success but NOT broadcast.
      assert %Core.JoinRoomResponse{} = Server.join_room(request, build_stream())

      refute_receive {:new_join, _}, 100,
        "Must NOT broadcast {:new_join, _} when user is already a member (idempotent path)"
    end
  end
end
