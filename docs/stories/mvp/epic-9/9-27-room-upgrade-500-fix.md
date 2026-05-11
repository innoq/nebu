---
title: "9-27: Room Upgrade 500 Error — Fix Unhandled Exceptions in upgrade_room"
epic: "9"
story_id: "9-27"
status: "ready-for-dev"
security_review: "required"
matrix: true
ui: true
---

# Story 9-27: Room Upgrade 500 Error Fix

## Problem

**Marie kann den Raum `!5b593f6600388d83:localhost` nicht upgraden — sie erhält einen 500-Fehler statt eines 200.**

Element Web zeigt den "Upgrade to recommended chat version"-Button (Marie ist Room-Owner mit Power Level 100). Beim Klick gibt der Server HTTP 500 `M_UNKNOWN` zurück.

## Root Cause Analysis

Die `upgrade_room/2`-Funktion in `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` verwendet bare Pattern-Matching auf fehlschlagbare Operationen:

```elixir
:ok = Nebu.Room.Server.join(new_room_id, requester_id)       # MatchError wenn {:error, ...}
:ok = Nebu.Room.Server.set_power_levels(...)                  # MatchError wenn {:error, ...}
```

Ein `MatchError` ist keine `GRPC.RPCError` — der gRPC-Framework gibt `codes.Unknown` zurück, den Go-Gateway mappt auf HTTP 500. Zusätzlich:

1. Der Return-Value von `emit_state_event(new_room_id, ..., "m.room.create", ...)` wird ignoriert (stille Daten-Korruption möglich).
2. Der alte Raum wird nach dem Tombstone **nicht archiviert** — Matrix-Spec §11.35.1 verlangt, dass nach dem Tombstone keine weiteren State-Events oder Messages im alten Raum möglich sein sollen.

## Acceptance Criteria

### AC1 — Room Owner erfolgreich upgraden
**Given** Marie ist Room-Owner (Power Level 100) von Raum `!room:localhost`  
**When** sie `POST /_matrix/client/v3/rooms/!room:localhost/upgrade` mit `{"new_version":"10"}` aufruft  
**Then** erhält sie HTTP 200 mit `{"replacement_room": "!new_room_id:localhost"}`

### AC2 — MatchErrors werden zu 500 mit `M_UNKNOWN` (statt Crash/Unknown)
**Given** `Nebu.Room.Server.join` gibt `{:error, :db_error}` zurück  
**When** `upgrade_room` aufgerufen wird  
**Then** wird `GRPC.Status.internal()` geraised (nicht eine MatchError) → HTTP 500 `M_UNKNOWN`

### AC3 — `emit_state_event` Fehler für m.room.create werden korrekt behandelt
**Given** die DB-Insertion für m.room.create schlägt fehl  
**When** `upgrade_room` läuft  
**Then** wird ein `GRPC.Status.internal()` Error geraised mit Fehlermeldung

### AC4 — Non-Owner erhält 403
**Given** Kai ist NICHT Room-Owner (Power Level 0)  
**When** er `POST /_matrix/client/v3/rooms/{roomId}/upgrade` aufruft  
**Then** erhält er HTTP 403 `M_FORBIDDEN`

### AC5 — Alter Raum wird nach dem Upgrade archiviert
**Given** Marie hat einen Raum erfolgreich upgraded  
**When** jemand versucht, eine Nachricht in den alten Raum zu senden  
**Then** erhält er HTTP 403 `M_ROOM_ARCHIVED` (Raum ist archiviert/tombstoned)

### AC6 — Element Web zeigt Room Upgrade erfolgreich an
**Given** Alex ist mit Element Web eingeloggt und hat einen Raum erstellt  
**When** er in den Room-Settings "Upgrade to recommended chat version" klickt  
**Then** wird er in den neuen Raum weitergeleitet (kein Fehler-Dialog)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AC1+AC2+AC3: Unit tests — upgrade_room error paths** — ExUnit + GRPC mock  
   - Given: FakeDB that returns `{:error, :db_error}` from `insert_member`  
   - When: `upgrade_room` is called with a valid owner  
   - Then: returns `GRPC.RPCError` with `codes.Internal`, NOT a MatchError crash

2. **AC4: Integration test — Godog non-owner upgrade** — `gateway/features/upgrade_room.feature`  
   - Already exists: `NonMember_Upgrade_Returns403` scenario  
   - Given: Kai creates room, Marie is NOT owner  
   - When: Marie POSTs upgrade  
   - Then: 403 M_FORBIDDEN

3. **AC5: Integration test — old room archived after upgrade** — `gateway/features/upgrade_room.feature`  
   - Given: Kai upgrades his room  
   - When: Marie (member) tries to send event to OLD room  
   - Then: 403 M_ROOM_ARCHIVED

4. **AC6: E2E Browser test — Element Web upgrade flow** — Playwright+Cucumber  
   - Feature: `e2e/features/element/room/upgrade.feature`  
   - Steps: `e2e/step-definitions/element/room.steps.ts` (upgrade steps)  
   - Given: Marie logged in as room owner in Element Web  
   - When: she clicks "Upgrade to recommended chat version"  
   - Then: she is redirected to the new room (no error dialog visible)

5. **Crash/restart test — Room.Server upgrade-path resilience** — ExUnit  
   - Given: upgrade_room completes successfully  
   - When: Room.Server for new_room_id is killed and restarted  
   - Then: power_levels and members are recovered from DB correctly

## Technical Notes

### Fixes required in `upgrade_room/2`:

1. **Replace `:ok = Nebu.Room.Server.join(...)` with proper error handling**:
   ```elixir
   case Nebu.Room.Server.join(new_room_id, requester_id) do
     :ok -> :ok
     {:error, reason} ->
       raise GRPC.RPCError,
         status: GRPC.Status.internal(),
         message: "Failed to join requester to new room: #{inspect(reason)}"
   end
   ```

2. **Replace `:ok = Nebu.Room.Server.set_power_levels(...)` with proper error handling**:
   ```elixir
   case Nebu.Room.Server.set_power_levels(new_room_id, requester_id, creator_pl) do
     :ok -> :ok
     {:error, reason} ->
       raise GRPC.RPCError,
         status: GRPC.Status.internal(),
         message: "Failed to set power levels on new room: #{inspect(reason)}"
   end
   ```

3. **Handle `emit_state_event` return for m.room.create**:
   ```elixir
   case emit_state_event(new_room_id, requester_id, "m.room.create", "", create_content) do
     {:ok, _} -> :ok
     {:error, reason} ->
       raise GRPC.RPCError,
         status: GRPC.Status.internal(),
         message: "Failed to emit m.room.create in new room: #{inspect(reason)}"
   end
   ```

4. **Archive old room after tombstone** (Matrix spec §11.35.1):
   After emitting tombstone, call `admin_db_module().archive_room_atomic(old_room_id)`.  
   Then stop the old room's GenServer via `Horde.DynamicSupervisor.terminate_child`.

### E2E Test Setup:

The Playwright test for AC6 needs:
- Alex logged in via Dex (using existing `dex-auth.ts` fixture)
- Alex creates a room, clicks upgrade in Room Settings
- Assert: Element Web navigates to replacement room (no error toast)

Use existing step infrastructure from `e2e/step-definitions/element/room.steps.ts`.
