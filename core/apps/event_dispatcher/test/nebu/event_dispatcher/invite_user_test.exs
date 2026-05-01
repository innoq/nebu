defmodule Nebu.EventDispatcher.InviteUserTest do
  use ExUnit.Case, async: false

  # ─── Story 8-10a: invite_user :pg broadcast to invitee ───────────────────────
  #
  # Root cause: invite_user/2 inserts the DB row but never sends
  # {:new_invite, room_id} to the invitee's sync task via :pg group
  # "user:#{invitee}". The invitee's long-polling sync task therefore
  # sleeps for the full 30-second timeout instead of waking immediately.
  #
  # Fix: after db_module_invite().insert_invitation returns :ok, broadcast
  # {:new_invite, room_id} to :pg.get_local_members("user:#{invitee}").
  #
  # async: false — Horde.Registry, Application.put_env, and :pg are process-global.

  alias Nebu.EventDispatcher.Server

  # ─── FakeDB (room_manager) ───────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying Nebu.Room.DB behaviour.
  # Injected via Application.put_env(:room_manager, :db_module, FakeDB).

  defmodule FakeDB do
    def load_members(room_id) do
      case :ets.lookup(:invite_user_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:invite_user_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:invite_user_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:invite_user_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:invite_user_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:invite_user_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:invite_user_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    # Story 6.8: load_room_settings/1 returns {:ok, 0} (no limit) for unit tests.
    def load_room_settings(_room_id), do: {:ok, 0}
  end

  # ─── FakeInviteDB ────────────────────────────────────────────────────────────

  defmodule FakeInviteDB do
    def insert_invitation(_room_id, _inviter, _invitee), do: :ok
    def accept_invitation(_room_id, _invitee_id), do: :ok
    def reject_invitation(_room_id, _invitee_id), do: :ok
    def get_pending_invite_rooms_for_user(_user_id), do: {:ok, []}
    def get_declined_invite_rooms_for_user(_user_id), do: {:ok, []}
  end

  defmodule FailingInviteDB do
    def insert_invitation(_room_id, _inviter, _invitee), do: {:error, :db_error}
    def accept_invitation(_room_id, _invitee_id), do: :ok
    def reject_invitation(_room_id, _invitee_id), do: :ok
    def get_pending_invite_rooms_for_user(_user_id), do: {:ok, []}
    def get_declined_invite_rooms_for_user(_user_id), do: {:ok, []}
  end

  defmodule NoOpAuditWriter do
    def log(_, _, _, _, _, _, _ \\ nil), do: :ok
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # ETS table for FakeDB (public so Room GenServer processes can access it).
    if :ets.info(:invite_user_test_db) != :undefined do
      :ets.delete(:invite_user_test_db)
    end

    :ets.new(:invite_user_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, FakeDB)
    Application.put_env(:event_dispatcher, :invite_db_module, FakeInviteDB)
    Application.put_env(:event_dispatcher, :messages_db_module, FakeDB)
    Application.put_env(:compliance, :audit_writer, NoOpAuditWriter)

    case :pg.start_link() do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :invite_db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:compliance, :audit_writer)

      if :ets.info(:invite_user_test_db) != :undefined do
        :ets.delete(:invite_user_test_db)
      end
    end)

    :ok
  end

  defp build_stream do
    %{http_request_headers: %{"x-user-id" => "@marie:test.local", "x-system-role" => "user"}}
  end

  # Starts a room via Horde and registers on_exit cleanup.
  defp start_and_track_room(room_id) do
    case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
      {:ok, _pid} -> :ok
      _ -> {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)
    end

    on_exit(fn ->
      if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:ok, pid} ->
            Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
          _ -> :ok
        end
      end
    end)

    :ok
  end

  # ─── AC1: invite_user broadcasts {:new_invite, room_id} to "user:#{invitee}" ──
  #
  # Given: test process subscribes to :pg group "user:@alex:test.local"
  # When: invite_user called with room_id + inviter=marie + invitee=alex
  # Then: test process receives {:new_invite, room_id} within 200 ms

  describe "invite_user/2 — :pg notification to invitee" do
    test "broadcasts {:new_invite, room_id} to user-level :pg group for invitee" do
      room_id = "!inv-test-#{System.unique_integer([:positive])}:test.local"
      invitee = "@alex:test.local"

      # Start room and make marie a member (she must be a member to invite).
      :ok = start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, "@marie:test.local")

      # Subscribe to invitee's user-level :pg group (simulates sync task).
      :ok = :pg.join("user:#{invitee}", self())
      on_exit(fn -> :pg.leave("user:#{invitee}", self()) end)

      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: "@marie:test.local",
        invitee_id: invitee
      }

      Server.invite_user(request, build_stream())

      assert_receive {:new_invite, ^room_id}, 200,
        "Expected {:new_invite, #{room_id}} from invite_user broadcast within 200 ms."
    end

    test "does NOT broadcast when insert_invitation fails" do
      Application.put_env(:event_dispatcher, :invite_db_module, FailingInviteDB)

      room_id = "!inv-fail-#{System.unique_integer([:positive])}:test.local"
      invitee = "@bob:test.local"

      :ok = start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, "@marie:test.local")

      :ok = :pg.join("user:#{invitee}", self())
      on_exit(fn -> :pg.leave("user:#{invitee}", self()) end)

      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: "@marie:test.local",
        invitee_id: invitee
      }

      assert_raise GRPC.RPCError, fn ->
        Server.invite_user(request, build_stream())
      end

      refute_receive {:new_invite, _}, 100,
        "Must NOT broadcast {:new_invite, _} when insert_invitation fails"
    end

    test "does nothing when no process is subscribed to invitee user group" do
      room_id = "!inv-nosub-#{System.unique_integer([:positive])}:test.local"
      invitee = "@charlie:test.local"

      :ok = start_and_track_room(room_id)
      :ok = Nebu.Room.Server.join(room_id, "@marie:test.local")

      # No subscription to "user:#{invitee}" — broadcast should be a no-op (no crash).
      request = %Core.InviteUserRequest{
        room_id: room_id,
        inviter_id: "@marie:test.local",
        invitee_id: invitee
      }

      assert %Core.InviteUserResponse{} = Server.invite_user(request, build_stream())
    end
  end
end
