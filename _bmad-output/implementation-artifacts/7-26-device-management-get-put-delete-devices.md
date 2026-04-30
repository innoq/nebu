---
id: 7-26
type: feature
security_review: required
created: 2026-04-30
---

# Story 7.26: Device Management — GET/PUT/DELETE /devices + POST /delete_devices

Status: ready-for-dev

## Story

As an end-user,
I want to list, rename, and delete my active login sessions (devices) from within any Matrix client,
so that I can maintain control over which devices have access to my account.

## Context / Background

The `GET /devices` stub currently returns `{"devices":[]}`. This story replaces the stub with real data from the sessions table and adds device rename, single-device delete, and bulk delete endpoints.

DELETE operations require User Interactive Authentication (UIA) — a Matrix challenge-response mechanism. Nebu is OIDC-only, so the only supported UIA stage is `m.login.sso` (re-authenticate via OIDC). UIA state is tracked server-side via a short-lived session keyed by a `session` UUID returned in the initial 401 challenge.

A user cannot delete their own current device (the device whose access token is being used in the request).

## Acceptance Criteria

1. `GET /_matrix/client/v3/devices` returns the real list of active sessions for the authenticated user from the sessions table. Each entry includes `device_id`, `display_name` (from `device_display_name` column, may be null), `last_seen_ip`, `last_seen_ts` (Unix ms).

2. `GET /_matrix/client/v3/devices/{deviceId}` returns the device object for one device; M_NOT_FOUND (404) if `deviceId` does not belong to the authenticated user.

3. `PUT /_matrix/client/v3/devices/{deviceId}` accepts body `{"display_name":"My Laptop"}` and updates `device_display_name` in the sessions table; M_NOT_FOUND if device does not belong to authenticated user.

4. `DELETE /_matrix/client/v3/devices/{deviceId}` requires UIA. Without auth object → 401 with `{"flows":[{"stages":["m.login.sso"]}],"session":"<uuid>","params":{}}`. After successful SSO UIA completion → invalidates that device's access token and returns `{}`. Returns M_FORBIDDEN if `deviceId` is the caller's own current device.

5. `POST /_matrix/client/v3/delete_devices` accepts `{"devices":["d1","d2"],"auth":{...}}`. Requires UIA (same flow). On success, invalidates all listed devices atomically (all-or-none). Silently ignores device IDs that do not belong to the user (no error).

6. Migration `000030_device_display_name.up.sql` adds nullable column `device_display_name TEXT` to the sessions table.

7. UIA implementation is reusable: extracted into `gateway/internal/matrix/uia.go` for future use (e.g. account deactivation).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [GET /devices returns real session list] — Godog (`gateway/features/devices.feature`)
   - Given: authenticated user `@alice:nebu.test` with two active sessions (device A, device B)
   - When: GET `/_matrix/client/v3/devices` using device A's token
   - Then: HTTP 200, `devices` array contains both device A and device B entries

2. [GET /devices/{deviceId} returns single device] — Godog
   - Given: authenticated user with device `DEVICE_A`
   - When: GET `/_matrix/client/v3/devices/DEVICE_A`
   - Then: HTTP 200, `device_id` = `DEVICE_A`; separate request for unknown ID → 404 M_NOT_FOUND

3. [PUT /devices/{deviceId} updates display name] — Godog
   - Given: authenticated user with device `DEVICE_A`
   - When: PUT `/_matrix/client/v3/devices/DEVICE_A` with body `{"display_name":"Work Laptop"}`
   - Then: HTTP 200 `{}`; subsequent GET returns `display_name` = `"Work Laptop"`

4. [DELETE /devices/{deviceId} — UIA challenge on first attempt] — Godog
   - Given: authenticated user with device `DEVICE_B` (not the current device)
   - When: DELETE `/_matrix/client/v3/devices/DEVICE_B` with no auth body
   - Then: HTTP 401, body contains `flows`, `session`, `params`

5. [DELETE own current device is forbidden] — Godog
   - Given: authenticated user using device `DEVICE_A`
   - When: DELETE `/_matrix/client/v3/devices/DEVICE_A` (with completed UIA)
   - Then: HTTP 403, `errcode` = `M_FORBIDDEN`

6. [POST /delete_devices bulk-invalidates atomically] — Go httptest (`gateway/internal/matrix/devices_test.go`)
   - Given: three sessions A, B, C for a user; completed UIA session
   - When: POST `/delete_devices` with `{"devices":["DEVICE_B","DEVICE_C"],"auth":{...}}`
   - Then: sessions B and C are invalidated; session A remains valid; GET /devices returns only A

7. [UIA: completed SSO session proceeds] — Go httptest (mock OIDC callback)
   - Given: UIA session UUID issued in 401 response
   - When: DELETE retried with `{"auth":{"type":"m.login.sso","session":"<uuid>"}}`; OIDC callback completed for that session
   - Then: HTTP 200 `{}`

## Implementation Notes

**Migration:** `migrations/000030_device_display_name.up.sql`
```sql
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS device_display_name TEXT;
```

**New handler file:** `gateway/internal/matrix/devices.go`
- `ListDevicesHandler`, `GetDeviceHandler`, `PutDeviceHandler`, `DeleteDeviceHandler`, `DeleteDevicesHandler`
- All read from sessions table via new gRPC calls or direct DB query via Session Manager

**UIA module:** `gateway/internal/matrix/uia.go`
- `StartUIA(w, r) (sessionID string)` — writes 401 JSON challenge, stores UIA state (ETS or in-memory map with TTL)
- `CheckUIA(r) (completed bool, err error)` — validates `auth.session` and `auth.type`, checks OIDC callback flag
- UIA session TTL: 5 minutes

**gRPC proto additions** (`proto/core.proto`):
```proto
rpc ListDevices(ListDevicesRequest) returns (ListDevicesResponse);
rpc UpdateDeviceDisplayName(UpdateDeviceDisplayNameRequest) returns (UpdateDeviceDisplayNameResponse);
rpc InvalidateSessions(InvalidateSessionsRequest) returns (InvalidateSessionsResponse);
```

**Route registration** in `gateway/cmd/gateway/main.go`:
```
GET    /_matrix/client/v3/devices                      → jwtMiddleware(ListDevicesHandler)
GET    /_matrix/client/v3/devices/{deviceId}           → jwtMiddleware(GetDeviceHandler)
PUT    /_matrix/client/v3/devices/{deviceId}           → jwtMiddleware(bodyLimit1MiB(PutDeviceHandler))
DELETE /_matrix/client/v3/devices/{deviceId}           → jwtMiddleware(DeleteDeviceHandler)
POST   /_matrix/client/v3/delete_devices               → jwtMiddleware(bodyLimit1MiB(DeleteDevicesHandler))
```

**Security considerations:**
- Token invalidation must be synchronous — the session record must be deleted (or marked invalid) before the 200 response is returned.
- UIA state must not be transferable between users — tie `session` UUID to the requesting `user_id`.
- Current device detection: compare `deviceId` in path against the `device_id` claim embedded in the JWT or session record.

## Tasks

- [ ] Write failing Godog scenarios in `gateway/features/devices.feature`
- [ ] Write failing Go httptest in `gateway/internal/matrix/devices_test.go`
- [ ] Add migration `migrations/000030_device_display_name.up.sql`
- [ ] Extend `proto/core.proto`; run `make proto`
- [ ] Implement Elixir gRPC handlers for ListDevices, UpdateDeviceDisplayName, InvalidateSessions
- [ ] Implement `gateway/internal/matrix/uia.go`
- [ ] Implement `gateway/internal/matrix/devices.go`
- [ ] Register routes in `main.go`; remove the existing `GET /devices` stub
- [ ] Run `make test-unit-go` + `make test-unit-elixir` — all pass
- [ ] Run `make test-integration` — Godog scenarios green
- [ ] Security Gate 1: run `/bmad-security-review` on staged diff before merge
