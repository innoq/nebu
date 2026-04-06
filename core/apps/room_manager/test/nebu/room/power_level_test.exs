defmodule Nebu.Room.PowerLevelsTest do
  use ExUnit.Case, async: true

  # Pure unit tests for Nebu.Room.PowerLevels.
  # All maps use string keys per architecture rule (no atom keys in DB-sourced maps).
  # These tests MUST FAIL until Nebu.Room.PowerLevels is implemented in
  # core/apps/room_manager/lib/nebu/room/power_level.ex

  # ─── AC#1 + AC#2: default_levels/0 ────────────────────────────────────────

  describe "Nebu.Room.PowerLevels.default_levels/0" do
    test "returns map with all required default keys and correct values" do
      levels = Nebu.Room.PowerLevels.default_levels()

      assert levels["ban"] == 50
      assert levels["kick"] == 50
      assert levels["invite"] == 0
      assert levels["redact"] == 50
      assert levels["state_default"] == 50
      assert levels["events_default"] == 0
      assert levels["users_default"] == 0
      assert levels["users"] == %{}
      assert levels["events"] == %{}
    end

    test "returned map has exactly nine top-level keys" do
      levels = Nebu.Room.PowerLevels.default_levels()
      assert map_size(levels) == 9
    end

    test "uses string keys, not atom keys" do
      levels = Nebu.Room.PowerLevels.default_levels()
      # String keys exist
      assert Map.has_key?(levels, "ban")
      # Atom keys must NOT exist
      refute Map.has_key?(levels, :ban)
    end
  end

  # ─── AC#3: get_user_level/2 ─────────────────────────────────────────────────

  describe "Nebu.Room.PowerLevels.get_user_level/2" do
    test "returns per-user override when present in users map" do
      levels = %{
        "users_default" => 0,
        "users" => %{"@admin:test.local" => 100}
      }

      assert Nebu.Room.PowerLevels.get_user_level(levels, "@admin:test.local") == 100
    end

    test "returns users_default when user has no per-user override" do
      levels = %{
        "users_default" => 0,
        "users" => %{}
      }

      assert Nebu.Room.PowerLevels.get_user_level(levels, "@alice:test.local") == 0
    end

    test "returns 0 when users_default and users are absent" do
      assert Nebu.Room.PowerLevels.get_user_level(%{}, "@alice:test.local") == 0
    end

    test "returns custom users_default when user has no override" do
      levels = %{
        "users_default" => 25,
        "users" => %{}
      }

      assert Nebu.Room.PowerLevels.get_user_level(levels, "@bob:test.local") == 25
    end
  end

  # ─── AC#1 (can?): user with level >= required → true ────────────────────────

  describe "Nebu.Room.PowerLevels.can?/3 — returns true" do
    test "default user can send_event (power 0 >= events_default 0)" do
      levels = Nebu.Room.PowerLevels.default_levels()
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :send_event) == true
    end

    test "default user can invite (power 0 >= invite 0)" do
      levels = Nebu.Room.PowerLevels.default_levels()
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :invite) == true
    end

    test "user with power 50 can kick (power 50 >= kick threshold 50)" do
      levels = Map.put(Nebu.Room.PowerLevels.default_levels(), "users", %{"@mod:test.local" => 50})
      assert Nebu.Room.PowerLevels.can?(levels, "@mod:test.local", :kick) == true
    end

    test "user with power 50 can ban (power 50 >= ban threshold 50)" do
      levels = Map.put(Nebu.Room.PowerLevels.default_levels(), "users", %{"@mod:test.local" => 50})
      assert Nebu.Room.PowerLevels.can?(levels, "@mod:test.local", :ban) == true
    end

    test "user with power 50 can change_state (power 50 >= state_default 50)" do
      levels = Map.put(Nebu.Room.PowerLevels.default_levels(), "users", %{"@mod:test.local" => 50})
      assert Nebu.Room.PowerLevels.can?(levels, "@mod:test.local", :change_state) == true
    end

    test "room creator (power 100) can perform all actions" do
      levels =
        Nebu.Room.PowerLevels.default_levels()
        |> Map.put("users", %{"@creator:test.local" => 100})

      for action <- [:send_event, :invite, :kick, :ban, :change_state] do
        assert Nebu.Room.PowerLevels.can?(levels, "@creator:test.local", action) == true,
               "Expected can?/3 to return true for action #{action}"
      end
    end
  end

  # ─── AC#2 (can?): user with level < required → false ────────────────────────

  describe "Nebu.Room.PowerLevels.can?/3 — returns false" do
    test "default user cannot kick (power 0 < kick threshold 50)" do
      levels = Nebu.Room.PowerLevels.default_levels()
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :kick) == false
    end

    test "default user cannot ban (power 0 < ban threshold 50)" do
      levels = Nebu.Room.PowerLevels.default_levels()
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :ban) == false
    end

    test "default user cannot change_state (power 0 < state_default 50)" do
      levels = Nebu.Room.PowerLevels.default_levels()
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :change_state) == false
    end

    test "user with power 49 cannot kick (power 49 < threshold 50)" do
      levels =
        Nebu.Room.PowerLevels.default_levels()
        |> Map.put("users", %{"@alice:test.local" => 49})

      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :kick) == false
    end

    test "default user cannot send_event when events_default raised to 50" do
      levels =
        Nebu.Room.PowerLevels.default_levels()
        |> Map.put("events_default", 50)

      # Alice has no per-user override → power 0 < 50
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :send_event) == false
    end

    test "user with no per-user override cannot invite when invite threshold raised to 50" do
      levels =
        Nebu.Room.PowerLevels.default_levels()
        |> Map.put("invite", 50)

      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :invite) == false
    end
  end

  # ─── AC#5: room creator assigned power 100 in users map ─────────────────────

  describe "room creator power level 100 in users sub-map" do
    test "power levels with creator entry grants creator power 100" do
      # Simulates the map that create_room flow sets via set_power_levels/3
      levels =
        Nebu.Room.PowerLevels.default_levels()
        |> put_in(["users", "@creator:test.local"], 100)

      assert levels["users"]["@creator:test.local"] == 100
    end

    test "creator at power 100 can change_state while default user cannot" do
      levels =
        Nebu.Room.PowerLevels.default_levels()
        |> put_in(["users", "@creator:test.local"], 100)

      assert Nebu.Room.PowerLevels.can?(levels, "@creator:test.local", :change_state) == true
      assert Nebu.Room.PowerLevels.can?(levels, "@alice:test.local", :change_state) == false
    end
  end
end
