# Deferred Work

## Deferred from: code review of story-5-11 (2026-04-20)

- Draft reads outside TX boundary (TOCTOU): `ClaimSelectionHandler` reads draft data (instanceName, oidcIssuer, etc.) outside the transaction, then writes them inside the TX. A concurrent request could theoretically read stale draft data. Pre-existing; bootstrap runs exactly once by a single admin and Story 5.10 guards the entry points.

## Deferred from: code review of story-4-4 (2026-04-03)

- Private key stored in `:persistent_term` without access control — acknowledged MVP limitation; Phase 2 must persist key to DB/disk to survive restarts.
- `:pg.start_link/0` uses default scope (global atom) — could collide with other umbrella apps; should use named scope when pg is used system-wide.
- `:pg.get_local_members/1` is node-local only — remote cluster subscribers silently skipped; Story 4-8 will address with full gRPC EventBus.
- ETS `:NebuTxnDedup` grows unbounded — acknowledged TODO; TTL pruning strategy needed in Story 4-X.
- `events` table missing index on `sender` and `event_type` — add when query patterns require it.
- `Jason.OrderedObject` is internal Jason struct — acceptable for now; monitor Jason major version upgrades.
- `CanonicalJson.normalize/1` treats Keyword lists as plain lists — document constraint or add clause when Keyword-list content is possible.
- Self-send in `:pg` broadcast (GenServer joins own group) — intentional no-op pattern; Story 4-8 replaces with real subscriber.
- `insert_room/1` ON CONFLICT returns node-clock timestamp not DB row timestamp — pre-existing from Story 4-2; fix with RETURNING clause.
- Determinism test verifies EventId in isolation, not two end-to-end calls — valid approach given server-side timestamp; acceptable as-is.

## Deferred from: code review of story-4-3 (2026-04-03)

- Architecture expects separate `canonical_json.ex` module alongside `event_id.ex` — currently integrated as private function `canonical_json/1` in `Nebu.EventId`. Acceptable until Story 4-4+ needs direct access; extract then.
- Tests do not cover maps with >32 keys — would have caught the MAJOR `Map.new` sort-order bug. Add a large-map test when verifying the `Jason.OrderedObject` fix.

## Deferred from: code review of story-4-2 (2026-04-03)

- Kein `@behaviour`-Modul fuer DB-Interface — `Nebu.Room.DB`, `FakeDB` und `FailingWriteDB` implementieren dasselbe Interface (`load_members/1`, `insert_room/1`, `insert_member/2`, `delete_member/2`) ohne expliziten `@callback`-Vertrag. Bei API-Aenderungen (wie in diesem Review: `load_members` Return-Signatur erweitert) muessen alle Implementierungen manuell synchron gehalten werden. Empfehlung: `@behaviour Nebu.Room.DBBehaviour` einfuehren, wenn weitere DB-Module oder Mox hinzukommen.

## Deferred from: code review of story-3-9 (2026-04-02) — RESOLVED in Epic 3 Retrospective (2026-04-02)

- ~~`extractFirstRoleClaim` takes only the first array element~~ → **FIXED**: Replaced with `auth.MatchesAdminGroupClaim` which checks ALL array elements across ALL claims. `admin_group_claim` is now configurable via Bootstrap Wizard.
- ~~Catch-all `GET /admin/` handler silently swallows DB errors~~ → **FIXED**: DB error in catch-all now returns 500 via `admin.Error500`.
- ~~`bootstrap-done.html` hardcodes `instance_admin` role name~~ → **FIXED**: `bootstrap-done.html` and `DoneHandler` removed. Post-bootstrap flow redirects directly to dashboard.

## Deferred from: code review of bugfix-logout-oidc-dex-session (2026-04-20)

- **loginToken TTL mismatch**: Kommentar in `sso.go:274` gibt 30s an, Implementation nutzt `5*time.Minute` (300s). Potentiell längeres Exposure-Fenster als beabsichtigt. Pre-existing.
- **Global SSO State Race**: `globalSSOState` und `globalLoginTokens` sind Package-Singletons ohne Reset zwischen E2E-Testiterationen. Cleanup-Loop wird nur bei Writes getriggert. Pre-existing Architektur-Eigenschaft.
