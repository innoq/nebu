defmodule Nebu.EventDispatcher.ServerDedupTest do
  use ExUnit.Case, async: false

  # ─── dedup_member_state_events/2 — Regression & Correctness Tests ────────────
  #
  # MAJOR-2 fix: tests for the dedup_member_state_events/2 private function,
  # exercised through the public get_initial_sync/2 handler.
  #
  # Key invariant: dedup must key on state_key (the subject of the membership
  # event, i.e. the person whose membership changed), NOT on sender_id (the
  # person who triggered the change). These differ for Invite and Kick:
  #
  #   Self-join: sender=Alice, state_key=Alice → same (dedup works either way)
  #   Invite:    sender=Alice, state_key=Bob   → ONLY Bob's state event removed
  #              Bug: old code keyed on sender_id (Alice) → Alice's state event
  #              was incorrectly removed instead of Bob's.
  #
  # async: false — shares Horde.Registry, Horde.DynamicSupervisor, ETS tables,
  # and Application.put_env with other test modules. Must run sequentially.
  #
  # Test strategy:
  #   - All tests call Server.get_initial_sync/2 directly.
  #   - Fake modules for Room DB, messages DB, rooms DB, invite DB, pg store.
  #   - Member state events come from Room.Server state (via build_state_events).
  #   - Timeline events are seeded in ETS with "event_type"/"state_key" keys.
  #   - Tests assert which m.room.member state events survive deduplication.

  alias Nebu.EventDispatcher.Server

  # ─── DedupTestFakeDB ─────────────────────────────────────────────────────────
  #
  # ETS-backed fake satisfying the DB behaviours required by get_initial_sync/2.
  # Named table: :dedup_test_db

  defmodule DedupTestFakeDB do
    # ── Room.DB behaviour (Room.Server.init/1) ────────────────────────────────

    def load_members(room_id) do
      case :ets.lookup(:dedup_test_db, {:room, room_id}) do
        [] ->
          {:error, :not_found}

        [{_, created_at_ms}] ->
          members =
            :ets.match(:dedup_test_db, {{:member, room_id, :"$1"}, :active})

          pl_json =
            case :ets.lookup(:dedup_test_db, {:power_levels, room_id}) do
              [{_, json}] -> json
              [] -> "{}"
            end

          {:ok, Enum.map(members, fn [uid] -> uid end), created_at_ms, pl_json}
      end
    end

    def insert_room(room_id) do
      now_ms = System.system_time(:millisecond)
      :ets.insert(:dedup_test_db, {{:room, room_id}, now_ms})
      {:ok, now_ms}
    end

    def insert_member(room_id, user_id) do
      :ets.insert(:dedup_test_db, {{:member, room_id, user_id}, :active})
      :ok
    end

    def delete_member(room_id, user_id) do
      :ets.delete(:dedup_test_db, {:member, room_id, user_id})
      :ok
    end

    def insert_event(event) do
      :ets.insert(:dedup_test_db, {{:event, event["event_id"]}, event})
      :ok
    end

    def set_power_levels(room_id, power_levels_json) do
      :ets.insert(:dedup_test_db, {{:power_levels, room_id}, power_levels_json})
      :ok
    end

    # ── Rooms DB (get_rooms_for_user/1) ───────────────────────────────────────

    def get_rooms_for_user(user_id) do
      rooms =
        :ets.match(:dedup_test_db, {{:member_for_user, user_id, :"$1"}, :active})

      {:ok, Enum.map(rooms, fn [room_id] -> room_id end)}
    end

    def get_recently_left_rooms_for_user(_user_id), do: {:ok, []}

    # ── Messages DB (fetch_events/4) ──────────────────────────────────────────

    def fetch_events(room_id, _direction, limit, _from_token) do
      all_events =
        :ets.match(:dedup_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev -> ev["room_id"] == room_id and Map.has_key?(ev, "event_type") end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :desc)
        |> Enum.take(limit)

      {:ok, all_events, "", ""}
    end

    def fetch_events_since(room_id, last_event_id, limit) do
      cutoff_ts =
        case last_event_id do
          nil -> 0
          "" -> 0
          id ->
            case get_event_timestamp(id) do
              {:ok, ts} -> ts
              {:error, _} -> 0
            end
        end

      events =
        :ets.match(:dedup_test_db, {{:event, :"$1"}, :"$2"})
        |> Enum.map(fn [_id, ev] -> ev end)
        |> Enum.filter(fn ev ->
          ev["room_id"] == room_id and
            Map.has_key?(ev, "event_type") and
            ev["origin_server_ts"] > cutoff_ts
        end)
        |> Enum.sort_by(fn ev -> ev["origin_server_ts"] end, :asc)
        |> Enum.take(limit)

      {:ok, events}
    end

    def get_event_timestamp(event_id) do
      case :ets.lookup(:dedup_test_db, {:event, event_id}) do
        [{_, event_map}] -> {:ok, event_map["origin_server_ts"]}
        [] -> {:error, :not_found}
      end
    end

    def get_room_name(_room_id), do: {:error, :not_found}
    def load_room_settings(_room_id), do: {:ok, 0}
    def get_room_status(_room_id), do: {:ok, "active"}
    def check_room_status_for_update(_room_id), do: {:ok, "active"}
    def get_room_creator(_room_id), do: {:error, :not_found}
    def get_generic_state_events(_room_id), do: {:ok, []}
    def get_room_create_event(_room_id), do: {:error, :not_found}
    # Story 9-28: no thread relations in dedup unit tests — return empty list / 0.
    def fetch_events_by_relation(_room_id, _event_id, _rel_type, _limit), do: {:ok, []}
    def count_thread_children(_room_id, _event_id), do: {:ok, 0}
    def event_in_room?(_event_id, _room_id), do: true
  end

  # ─── DedupTestFakeInviteDB ────────────────────────────────────────────────────

  defmodule DedupTestFakeInviteDB do
    def get_pending_invite_rooms_for_user(_user_id), do: {:ok, []}
    def get_declined_invite_rooms_for_user(_user_id), do: {:ok, []}
    def insert_invitation(_room_id, _inviter, _invitee), do: :ok
    def accept_invitation(_room_id, _invitee_id), do: :ok
    def reject_invitation(_room_id, _invitee_id), do: :ok
  end

  # ─── DedupTestFakePgStore ─────────────────────────────────────────────────────

  defmodule DedupTestFakePgStore do
    def persist_since_token(_user_id, _since_token, _last_event_id), do: :ok
    def get_since_token(_user_id), do: {:error, :not_found}
    def invalidate_session(_user_id), do: :ok
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    if :ets.info(:dedup_test_db) != :undefined, do: :ets.delete(:dedup_test_db)
    :ets.new(:dedup_test_db, [:named_table, :public, :set])

    Application.put_env(:room_manager, :db_module, DedupTestFakeDB)
    Application.put_env(:event_dispatcher, :messages_db_module, DedupTestFakeDB)
    Application.put_env(:event_dispatcher, :rooms_db_module, DedupTestFakeDB)
    Application.put_env(:event_dispatcher, :invite_db_module, DedupTestFakeInviteDB)
    Application.put_env(:event_dispatcher, :pg_store_module, DedupTestFakePgStore)
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    if :ets.whereis(:NebuTxnDedup) != :undefined do
      :ets.delete_all_objects(:NebuTxnDedup)
    end

    on_exit(fn ->
      Application.delete_env(:room_manager, :db_module)
      Application.delete_env(:event_dispatcher, :messages_db_module)
      Application.delete_env(:event_dispatcher, :rooms_db_module)
      Application.delete_env(:event_dispatcher, :invite_db_module)
      Application.delete_env(:event_dispatcher, :pg_store_module)
      Application.delete_env(:event_dispatcher, :server_name)

      if :ets.info(:dedup_test_db) != :undefined, do: :ets.delete(:dedup_test_db)

      if :ets.whereis(:NebuTxnDedup) != :undefined do
        :ets.delete_all_objects(:NebuTxnDedup)
      end
    end)

    :ok
  end

  # ─── Helper: start a room and join a member ───────────────────────────────────

  defp setup_room_with_member(room_id, user_id) do
    {:ok, _pid} = Nebu.Room.RoomSupervisor.start_room(room_id)

    on_exit(fn ->
      if Process.whereis(Nebu.Room.HordeSupervisor) != nil do
        case Nebu.Room.RoomSupervisor.lookup_room(room_id) do
          {:ok, pid} ->
            Horde.DynamicSupervisor.terminate_child(Nebu.Room.HordeSupervisor, pid)
          _ -> :ok
        end
      end
    end)

    :ok = Nebu.Room.Server.join(room_id, user_id)
    :ets.insert(:dedup_test_db, {{:member_for_user, user_id, room_id}, :active})
    :ok
  end

  defp build_stream, do: %{http_request_headers: %{}}

  # ─────────────────────────────────────────────────────────────────────────────
  # Test 1 — Self-join in timeline: member state event is deduplicated
  #
  # Given: @alice:test.local is a member of !dedup-self:test.local
  #        (so build_state_events emits an m.room.member state event for Alice)
  #        AND a timeline event of type m.room.member with state_key=Alice exists
  # When:  get_initial_sync is called for Alice
  # Then:  response room has NO m.room.member state event for Alice
  #        (because Alice's join is in the timeline — dedup removes the duplicate)
  # ─────────────────────────────────────────────────────────────────────────────

  describe "dedup_member_state_events — self-join" do
    test "member state event for self-joiner is removed when join appears in timeline" do
      alice = "@alice:test.local"
      room_id = "!dedup-self:test.local"

      :ok = setup_room_with_member(room_id, alice)

      # Seed a timeline m.room.member event where sender == state_key (self-join).
      DedupTestFakeDB.insert_event(%{
        "event_id" => "$dedup_self_join_1",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.member",
        "state_key" => alice,
        "content" => %{"membership" => "join"},
        "origin_server_ts" => 1_700_000_001_000
      })

      request = %Core.GetInitialSyncRequest{user_id: alice}
      response = Server.get_initial_sync(request, build_stream())

      assert length(response.rooms) == 1,
             "expected 1 room in response, got #{length(response.rooms)}"

      [room] = response.rooms

      member_state_events =
        Enum.filter(room.state_events, fn ev -> ev.type == "m.room.member" end)

      alice_state_events =
        Enum.filter(member_state_events, fn ev -> ev.state_key == alice end)

      assert alice_state_events == [],
             "expected Alice's m.room.member state event to be deduplicated " <>
               "(it exists in timeline), but found: #{inspect(alice_state_events)}"

      # The join IS in the timeline — verify it's there.
      alice_timeline_events =
        Enum.filter(room.timeline_events, fn ev ->
          ev.event_type == "m.room.member" and ev.state_key == alice
        end)

      assert length(alice_timeline_events) == 1,
             "expected Alice's m.room.member to be in timeline, got: #{inspect(room.timeline_events)}"
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # Test 2 — Invite (sender ≠ state_key): only Bob's state event is removed,
  #           NOT the inviter Alice's state event (regression test for MAJOR-1 bug)
  #
  # Given: @alice:test.local and @bob:test.local are both members of !dedup-invite:test.local
  #        (build_state_events emits m.room.member for BOTH)
  #        AND a timeline event of type m.room.member where
  #            sender=@alice:test.local (inviter) and state_key=@bob:test.local (invitee)
  # When:  get_initial_sync is called for Alice
  # Then:  Bob's m.room.member state event IS removed (his join is in the timeline)
  #        Alice's m.room.member state event is KEPT (she is NOT the state_key)
  #
  # OLD BUG: The code keyed on sender_id (Alice), so Alice's state event was
  # wrongly removed while Bob's remained. This regression test catches that.
  # ─────────────────────────────────────────────────────────────────────────────

  describe "dedup_member_state_events — invite (sender ≠ state_key)" do
    test "only the invitee's (state_key) state event is deduplicated, NOT the inviter's" do
      alice = "@alice:test.local"
      bob = "@bob:test.local"
      room_id = "!dedup-invite:test.local"

      :ok = setup_room_with_member(room_id, alice)
      :ok = Nebu.Room.Server.join(room_id, bob)
      :ets.insert(:dedup_test_db, {{:member_for_user, alice, room_id}, :active})

      # Seed a timeline m.room.member event where sender=Alice (inviter) but
      # state_key=Bob (the person whose membership changed — the invitee).
      DedupTestFakeDB.insert_event(%{
        "event_id" => "$dedup_invite_bob_1",
        "room_id" => room_id,
        "sender" => alice,
        "event_type" => "m.room.member",
        "state_key" => bob,
        "content" => %{"membership" => "invite"},
        "origin_server_ts" => 1_700_000_002_000
      })

      request = %Core.GetInitialSyncRequest{user_id: alice}
      response = Server.get_initial_sync(request, build_stream())

      assert length(response.rooms) == 1,
             "expected 1 room in response, got #{length(response.rooms)}"

      [room] = response.rooms

      member_state_events =
        Enum.filter(room.state_events, fn ev -> ev.type == "m.room.member" end)

      bob_state_events =
        Enum.filter(member_state_events, fn ev -> ev.state_key == bob end)

      alice_state_events =
        Enum.filter(member_state_events, fn ev -> ev.state_key == alice end)

      # Bob's state event must be removed — his membership change is in the timeline.
      assert bob_state_events == [],
             "expected Bob's m.room.member state event to be deduplicated " <>
               "(state_key=Bob in timeline), but found: #{inspect(bob_state_events)}"

      # REGRESSION: Alice's state event must be KEPT — she is the sender, not the subject.
      # The old code keyed on sender_id, so Alice's state event was wrongly removed.
      assert length(alice_state_events) == 1,
             "expected Alice's m.room.member state event to be KEPT " <>
               "(she is the sender/inviter, not the state_key), " <>
               "but found: #{inspect(alice_state_events)}. " <>
               "This is the MAJOR-1 regression: dedup keyed on sender_id instead of state_key."
    end
  end

  # ─────────────────────────────────────────────────────────────────────────────
  # Test 3 — Empty timeline: state events returned unchanged
  #
  # Given: @alice:test.local is a member of !dedup-empty:test.local
  #        AND the timeline is empty (no events at all)
  # When:  get_initial_sync is called for Alice
  # Then:  Alice's m.room.member state event is KEPT (nothing to deduplicate against)
  # ─────────────────────────────────────────────────────────────────────────────

  describe "dedup_member_state_events — empty timeline" do
    test "state events are returned unchanged when timeline is empty" do
      alice = "@alice:test.local"
      room_id = "!dedup-empty:test.local"

      :ok = setup_room_with_member(room_id, alice)

      # No timeline events seeded — dedup must be a no-op.

      request = %Core.GetInitialSyncRequest{user_id: alice}
      response = Server.get_initial_sync(request, build_stream())

      assert length(response.rooms) == 1,
             "expected 1 room in response, got #{length(response.rooms)}"

      [room] = response.rooms

      assert room.timeline_events == [],
             "expected empty timeline, got: #{inspect(room.timeline_events)}"

      alice_state_events =
        Enum.filter(room.state_events, fn ev ->
          ev.type == "m.room.member" and ev.state_key == alice
        end)

      assert length(alice_state_events) == 1,
             "expected Alice's m.room.member state event to be KEPT when timeline is empty, " <>
               "got: #{inspect(alice_state_events)}"
    end
  end
end
