# Deferred Work

## Deferred from: code review of story-4-3 (2026-04-03)

- Architecture expects separate `canonical_json.ex` module alongside `event_id.ex` — currently integrated as private function `canonical_json/1` in `Nebu.EventId`. Acceptable until Story 4-4+ needs direct access; extract then.
- Tests do not cover maps with >32 keys — would have caught the MAJOR `Map.new` sort-order bug. Add a large-map test when verifying the `Jason.OrderedObject` fix.

## Deferred from: code review of story-4-2 (2026-04-03)

- Kein `@behaviour`-Modul fuer DB-Interface — `Nebu.Room.DB`, `FakeDB` und `FailingWriteDB` implementieren dasselbe Interface (`load_members/1`, `insert_room/1`, `insert_member/2`, `delete_member/2`) ohne expliziten `@callback`-Vertrag. Bei API-Aenderungen (wie in diesem Review: `load_members` Return-Signatur erweitert) muessen alle Implementierungen manuell synchron gehalten werden. Empfehlung: `@behaviour Nebu.Room.DBBehaviour` einfuehren, wenn weitere DB-Module oder Mox hinzukommen.

## Deferred from: code review of story-3-9 (2026-04-02) — RESOLVED in Epic 3 Retrospective (2026-04-02)

- ~~`extractFirstRoleClaim` takes only the first array element~~ → **FIXED**: Replaced with `auth.MatchesAdminGroupClaim` which checks ALL array elements across ALL claims. `admin_group_claim` is now configurable via Bootstrap Wizard.
- ~~Catch-all `GET /admin/` handler silently swallows DB errors~~ → **FIXED**: DB error in catch-all now returns 500 via `admin.Error500`.
- ~~`bootstrap-done.html` hardcodes `instance_admin` role name~~ → **FIXED**: `bootstrap-done.html` and `DoneHandler` removed. Post-bootstrap flow redirects directly to dashboard.
