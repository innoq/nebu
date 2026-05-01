defmodule Nebu.Room.DBBehaviourTest do
  use ExUnit.Case, async: true

  # ─── Story 5.29d AC1+AC2 (FB-E5-03): @behaviour Nebu.Room.DBBehaviour ────────
  #
  # Purpose: compile-time conformance guard — ensures every module that implements
  # the Room DB interface declares `@behaviour Nebu.Room.DBBehaviour`. When a new
  # callback is added to the behaviour or a fake drifts, the compiler will warn
  # (or error with strict settings) before tests run.
  #
  # RED-PHASE CONTRACT:
  #   These tests FAIL until:
  #     1. `Nebu.Room.DBBehaviour` is defined with all required @callbacks.
  #     2. `Nebu.Room.DB` declares `@behaviour Nebu.Room.DBBehaviour`.
  #     3. `Nebu.Room.InviteDB` — note: separate behaviour not required; InviteDB
  #        callbacks are in Nebu.Room.InviteDBBehaviour (future story).
  #        For this story, only Nebu.Room.DB is checked.
  #
  # The test-process check (module_behaviours assertions) is compile-time information
  # surfaced at runtime. Incorrect @behaviour declarations cause warnings; missing
  # callbacks cause warnings. We assert them explicitly here so CI counts this as
  # a failing test, not just a compiler warning.
  #
  # AC coverage:
  #   AC1 (FB-E5-03) — Nebu.Room.DBBehaviour module exists
  #   AC2            — Nebu.Room.DB implements Nebu.Room.DBBehaviour

  # ─── AC1: Nebu.Room.DBBehaviour module exists and has the required callbacks ───

  describe "Nebu.Room.DBBehaviour — module and callback contract" do
    test "Nebu.Room.DBBehaviour is defined as a module" do
      # FAILS until db_behaviour.ex is created.
      assert Code.ensure_loaded?(Nebu.Room.DBBehaviour),
             "Nebu.Room.DBBehaviour module not found — create core/apps/room_manager/lib/nebu/room/db_behaviour.ex"
    end

    test "Nebu.Room.DBBehaviour defines the required callbacks" do
      # FAILS until db_behaviour.ex declares all @callback entries.
      callbacks = Nebu.Room.DBBehaviour.behaviour_info(:callbacks)

      required = [
        {:load_members, 1},
        {:insert_room, 1},
        {:insert_member, 2},
        {:delete_member, 2},
        {:insert_event, 1},
        {:set_power_levels, 2},
        {:get_rooms_for_user, 1},
        {:fetch_events, 4},
        {:fetch_events_since, 3},
        {:get_event_timestamp, 1},
        {:get_room_name, 1},
        # Story 6.8: load_room_settings/1 callback for max_members recovery on restart.
        {:load_room_settings, 1}
      ]

      for {fun, arity} <- required do
        assert {fun, arity} in callbacks,
               "Expected Nebu.Room.DBBehaviour to declare @callback #{fun}/#{arity} but it was not found in #{inspect(callbacks)}"
      end
    end
  end

  # ─── AC2: Nebu.Room.DB declares @behaviour Nebu.Room.DBBehaviour ──────────────

  describe "Nebu.Room.DB — behaviour declaration" do
    test "Nebu.Room.DB declares @behaviour Nebu.Room.DBBehaviour" do
      # FAILS until `@behaviour Nebu.Room.DBBehaviour` is added to db.ex.
      behaviours = Nebu.Room.DB.module_info(:attributes)[:behaviour] || []

      assert Nebu.Room.DBBehaviour in behaviours,
             "Nebu.Room.DB must declare `@behaviour Nebu.Room.DBBehaviour`. " <>
               "Current behaviours: #{inspect(behaviours)}"
    end

    test "Nebu.Room.DB exports all callbacks declared by Nebu.Room.DBBehaviour" do
      # Belt-and-suspenders: verify the production module exports all required callbacks.
      # FAILS if Nebu.Room.DB is missing any function listed in the behaviour.
      callbacks = Nebu.Room.DBBehaviour.behaviour_info(:callbacks)
      db_exports = Nebu.Room.DB.module_info(:exports)

      for {fun, arity} <- callbacks do
        assert {fun, arity} in db_exports,
               "Nebu.Room.DB is missing function #{fun}/#{arity} required by Nebu.Room.DBBehaviour"
      end
    end
  end
end
