---
security_review: optional
---

# Story 5.29e: Production Bugs from Manual Testing — Room Upgrade, Direct Messages, Admin UI

Status: ready-for-dev

## Story

As an end-user (Alex / Marie) and as an instance admin,
I want room version upgrades to actually upgrade the room, direct-message creation between two real users to complete without spinning forever, and the admin UI to load after login,
so that the headline workflows the product promises don't break in front of users at the most ordinary moments.

---

## Background / Motivation

These three findings come from manual exploratory testing (`tmp/test-findings.md`, captured 2026-04-23 by Philipp). They are NOT security findings — they are user-facing functional gaps the per-story unit/integration tests did not catch because:
- The room-upgrade endpoint has never been wired into the gateway.
- The DM flow exercises `keys/query` + profile lookup paths that the matrix-API surface in Epic 4 left as 501/404 stubs.
- The admin UI's gRPC connectivity check fails when the gateway can't reach the core in the user's actual setup.

Together, this is the difference between "feature complete on paper" and "feature complete for a real person".

---

## Findings rolled into this story

### Bug 1 — Room version upgrade returns 404

**Reported:** "Dieser Chat läuft mit der Chat-Version 1, welche dieser Homeserver als instabil markiert hat. ... Aktualisiere auf die empfohlene Chat-Version" → click "Chat auf Version 6 aktualisieren" → `Server returned 404 error: 404 page not found`.

**Likely root cause:** Matrix endpoint `POST /_matrix/client/v3/rooms/{roomId}/upgrade` is not registered in `gateway/cmd/gateway/main.go`. Matrix-spec defines this as the way to upgrade a room to a newer version — clients (FluffyChat / Element) call it when they detect the room is on an old version.

**Related observation:** "Diese Chats kann ich auch nicht Löschen. Kann es sein dass dort auch kein Event über `/keys/query` gefunden wird?" — possible secondary bug: encrypted rooms with no key bundle for the requesting user's device. Investigate alongside Bug 2.

**Fix:**
1. Implement `POST /_matrix/client/v3/rooms/{roomId}/upgrade` per Matrix Client-Server API §10.2.7. Body: `{"new_version": "<version>"}`. Returns `{"replacement_room": "<new_room_id>"}`.
2. Server creates a new room with the requested version, links the old room (`m.room.tombstone` event in old room pointing at new), copies state events that should carry over (per spec).
3. Return 404 / 400 / 403 only for the spec-defined cases, never as the default-mux fallback.
4. Smoke-test with FluffyChat and/or Element on a v1 room.

### Bug 2 — Direct message creation hangs (`keys/query` returns nothing)

**Reported:** Marie wants to start a DM with Alex. `@alex:localhost` is found. On clicking "Start DM":
```
Eventuell existieren folgende Benutzer nicht
Konnte keine Profile für die folgenden Matrix-IDs finden – möchtest du dennoch eine Direktnachricht beginnen?

@alex:localhost: Profile not found
```
After "Dennoch DM beginnen": empty room appears in the sidebar, the spinner "Chat mit @alex:localhost wird erstellt" never resolves.

**Likely root cause:** Two endpoints are involved:
1. `GET /_matrix/client/v3/profile/{userId}` returns 404 even though Alex's user exists. Either the Profile-DB lookup fails for users who registered via Bootstrap, or the displayname row was never created on first login.
2. `POST /_matrix/client/v3/keys/query` should return Alex's device keys. If it returns an empty `device_keys` map (or 501), the client cannot create the encrypted DM and stays stuck.

**Quote from Philipp:** "Schaue nochmal in der Matrix-Spezifikation. Nach, welche Methoden alle etwas in keys/query antworten sollten."

**Fix:**
1. Audit `GET /_matrix/client/v3/profile/{userId}` against `users` table — confirm a profile row exists for every user that completes OIDC login (provisioning step in 2-13).
2. Audit `POST /_matrix/client/v3/keys/query`. Per Matrix spec, it must return:
   ```
   {"device_keys": {"<userId>": {"<deviceId>": {<DeviceKeys structure>}}}, "failures": {}, "master_keys": {...}, "self_signing_keys": {...}, "user_signing_keys": {...}}
   ```
   Confirm every key-type returns a populated map (or an explicit empty map) — not 501 / null.
3. End-to-end Playwright test: Marie creates DM with Alex → DM room created with both members joined → no spinner remaining after 5s.

### Bug 3 — Admin UI: "Core unreachable" after login

**Reported:** "Nach login kommt 'Core unreachable'".

**Likely root cause:** The dashboard's metrics card calls `coreClient.GetMetrics(ctx, ...)` (`gateway/internal/admin/metrics.go` / `dashboard.go`). The handler renders "Core unreachable" when the gRPC call returns an error. Common causes:
- Core service not running in the user's compose stack.
- gRPC port not reachable (post-FB-52-01 fix would tighten this further).
- gRPC client created with a stale connection.

**Fix:**
1. Reproduce locally with `make dev` — confirm scenario.
2. If Core is up but the call fails: debug the gRPC client (logs at slog.Debug level).
3. Distinguish "Core down" from "Core misconfigured" in the UI message — give the admin a useful next step (link to logs / dependency status page).
4. Add health-check probe: render the Core dashboard card only when `/ready` reports core gRPC reachable. Otherwise show a less alarming "Core metrics temporarily unavailable" card.

---

## Acceptance Criteria

1. `POST /_matrix/client/v3/rooms/{roomId}/upgrade` is registered and returns `{"replacement_room": "..."}` for valid requests; 400/403/404 only per spec.
2. After login (Bootstrap or regular), `GET /_matrix/client/v3/profile/{userId}` returns 200 with `displayname` for the just-logged-in user.
3. `POST /_matrix/client/v3/keys/query` returns a non-empty, spec-compliant response for any user that exists in `users`.
4. Marie+Alex DM creation flow (Playwright): DM room appears in the sidebar with both members joined, no infinite spinner.
5. Admin UI dashboard does not show "Core unreachable" when Core is in fact running. When Core is genuinely down, the message is informative and non-alarming.

---

## Acceptance Tests

- `TestRoomUpgrade_HappyPath` (Go integration, real Matrix client / Godog)
- `TestRoomUpgrade_UnknownVersion_Returns400`
- `TestProfile_AfterBootstrapLogin_Returns200`
- `TestKeysQuery_ReturnsDeviceKeys` (smoke against a freshly-provisioned user)
- Playwright `e2e/features/dm-create-marie-alex.spec.ts` (full flow, no spinner after 5s)
- Playwright `e2e/features/admin-dashboard-after-login.spec.ts` (no "Core unreachable" alarm in normal state)

---

## Implementation Notes

- Source for these bugs: `tmp/test-findings.md` (Philipp, 2026-04-23). Reference exact wording in the test names so the regression intent is traceable.
- Bugs 1 and 2 are likely related (encrypted rooms with no member keys cannot be decrypted, can't be deleted, can't be upgraded — same `keys/query` gap). Investigate together; split if scope grows.
- Bug 3 is independent of Bugs 1/2.
- Pair with Story 4 retrospective FluffyChat-smoke-test learnings (CLAUDE.md `MEMORY.md`): real Matrix-client smoke is the only reliable signal for these flows.

---

## Dependencies

- Independent of 5-29a/b/c/d (these are functional bugs, not security follow-ups).
- Depends on Stories 4-2..4-15 (room operations, keys/query infrastructure) — but those are all marked done; this story's job is to find what fell through.

---

## Change Log

- 2026-04-23: Story split out from 5-29 master collector. Captures three production bugs from manual exploratory testing recorded in `tmp/test-findings.md`.
