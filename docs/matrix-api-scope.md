# Matrix API Scope

This document provides a full inventory of Matrix Client-Server API endpoints and their
implementation status in Nebu. Derived from `gateway/cmd/gateway/main.go` route registrations,
cross-referenced with the Matrix Client-Server API specification.

**Legend:**
- ✅ Implemented — real handler with database-backed logic
- 🔶 Stub — returns a hardcoded or minimal valid response; no persistent storage
- ❌ Intentionally excluded — will not be implemented (by design)
- ⏳ Planned — requires ADR decision before implementation

---

## Discovery and Client Configuration

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/versions` | ✅ | Returns v1.1–v1.11 |
| GET | `/.well-known/matrix/client` | ✅ | Dynamic base_url from request Host |
| GET | `/_matrix/client/v3/capabilities` | 🔶 | Returns change_password: false, room_versions: {6: stable} |
| GET | `/_matrix/client/unstable/org.matrix.msc2965/auth_metadata` | 🔶 | Returns 404 (MSC2965 not supported; forces fallback to m.login.sso) |

## Authentication

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/login` | ✅ | Returns m.login.sso flow |
| POST | `/_matrix/client/v3/login` | ✅ | OIDC token exchange → Nebu session |
| GET | `/_matrix/client/v3/login/sso/redirect` | ✅ | Initiates PKCE flow to OIDC provider |
| GET | `/_matrix/client/v3/login/sso/redirect/oidc` | ✅ | OIDC callback, issues Nebu token |
| POST | `/_matrix/client/v3/logout` | ✅ | Revokes session token |
| GET | `/_matrix/client/v3/account/whoami` | ✅ | Returns user_id from JWT |

## Sync

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/sync` | ✅ | Long-poll, ETS + gRPC, account_data included |

## Room Management

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| POST | `/_matrix/client/v3/createRoom` | ✅ | Creates room via gRPC CreateRoom |
| POST | `/_matrix/client/v3/join/{roomIdOrAlias}` | ✅ | Join by room ID or alias |
| POST | `/_matrix/client/v3/rooms/{roomId}/join` | ✅ | Accept invitation |
| POST | `/_matrix/client/v3/rooms/{roomId}/leave` | ✅ | Leave room via gRPC LeaveRoom |
| POST | `/_matrix/client/v3/rooms/{roomId}/invite` | ✅ | Invite user |
| POST | `/_matrix/client/v3/rooms/{roomId}/kick` | ✅ | Kick user (power level check) |
| POST | `/_matrix/client/v3/rooms/{roomId}/ban` | ✅ | Ban user |
| POST | `/_matrix/client/v3/rooms/{roomId}/unban` | ✅ | Unban user |
| POST | `/_matrix/client/v3/rooms/{roomId}/forget` | ✅ | Forget room |
| POST | `/_matrix/client/v3/rooms/{roomId}/upgrade` | 🔶 | Returns 501 Not Implemented |
| GET | `/_matrix/client/v3/joined_rooms` | 🔶 | Returns empty list (use /sync instead) |

## Messaging and Events

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| PUT | `/_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}` | ✅ | Sends event via gRPC SendEvent |
| GET | `/_matrix/client/v3/rooms/{roomId}/messages` | ✅ | Paginated message history |
| PUT | `/_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}` | ✅ | Set room state event |
| PUT | `/_matrix/client/v3/rooms/{roomId}/state/{eventType}` | ✅ | Set state (no stateKey) |
| GET | `/_matrix/client/v3/rooms/{roomId}/state` | ✅ | Get all state events |
| GET | `/_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}` | ✅ | Get single state event |
| GET | `/_matrix/client/v3/rooms/{roomId}/context/{eventId}` | ✅ | Event context (before/after) |
| POST | `/_matrix/client/v3/rooms/{roomId}/read_markers` | 🔶 | Acknowledged, not persisted |

## Event Relations and Threads

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}` | ✅ | Thread reply list via gRPC GetRelations; m.relations bundled aggregations in /sync unsigned field |

## Room Members

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/rooms/{roomId}/members` | ✅ | Returns membership events |
| GET | `/_matrix/client/v3/rooms/{roomId}/joined_members` | ✅ | Compact map of joined members |
| GET | `/_matrix/client/v3/rooms/{roomId}/aliases` | ✅ | Returns [] (no alias storage yet) |

## Presence and Typing

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/presence/{userId}/status` | ✅ | Reads from Elixir presence state |
| PUT | `/_matrix/client/v3/presence/{userId}/status` | ✅ | Updates presence status |
| PUT | `/_matrix/client/v3/rooms/{roomId}/typing/{userId}` | ✅ | Sends typing indicator |
| POST | `/_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}` | ✅ | Read receipts |

## Profile

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/profile/{userId}` | ✅ | Public (unauthenticated) |
| GET | `/_matrix/client/v3/profile/{userId}/displayname` | ✅ | Public sub-field |
| GET | `/_matrix/client/v3/profile/{userId}/avatar_url` | ✅ | Public sub-field |
| PUT | `/_matrix/client/v3/profile/{userId}/displayname` | ✅ | Requires JWT auth |
| PUT | `/_matrix/client/v3/profile/{userId}/avatar_url` | ✅ | Requires JWT auth |

## Account Data and Tags

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/user/{userId}/account_data/{type}` | ✅ | Global account data |
| PUT | `/_matrix/client/v3/user/{userId}/account_data/{type}` | ✅ | Global account data |
| GET | `/_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}` | ✅ | Per-room account data |
| PUT | `/_matrix/client/v3/user/{userId}/rooms/{roomId}/account_data/{type}` | ✅ | Per-room account data |
| GET | `/_matrix/client/v3/user/{userId}/rooms/{roomId}/tags` | ✅ | Room tags |
| PUT | `/_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}` | ✅ | Add/update room tag |
| DELETE | `/_matrix/client/v3/user/{userId}/rooms/{roomId}/tags/{tag}` | ✅ | Remove room tag |
| POST | `/_matrix/client/v3/user/{userId}/filter` | 🔶 | Returns filter_id "0" (unfiltered sync) |
| GET | `/_matrix/client/v3/user/{userId}/filter/{filterId}` | 🔶 | Returns stored filter (or empty) |

## Devices

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/devices` | ✅ | Lists devices (backed by sessions table) |
| GET | `/_matrix/client/v3/devices/{deviceId}` | ✅ | Get single device |
| PUT | `/_matrix/client/v3/devices/{deviceId}` | ✅ | Update device display name |
| DELETE | `/_matrix/client/v3/devices/{deviceId}` | ✅ | Delete device (UIA: m.login.sso) |
| POST | `/_matrix/client/v3/delete_devices` | ✅ | Delete multiple devices |

## Push Notifications

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/pushrules/` | ✅ | All push rules (DB-backed, default rules seeded) |
| GET | `/_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}` | ✅ | Single push rule |
| PUT | `/_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}` | ✅ | Create/update push rule |
| DELETE | `/_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}` | ✅ | Delete push rule |
| PUT | `/_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/enabled` | ✅ | Enable/disable rule |
| PUT | `/_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/actions` | ✅ | Update rule actions |
| GET | `/_matrix/client/v3/pushers` | ✅ | List pushers |
| POST | `/_matrix/client/v3/pushers/set` | ✅ | Add/remove pusher |
| GET | `/_matrix/client/v3/notifications` | ✅ | Notification list (cursor-paginated) |

## Room Directory and Public Rooms

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/publicRooms` | ✅ | Public room directory |
| POST | `/_matrix/client/v3/publicRooms` | ✅ | Filtered public rooms |
| PUT | `/_matrix/client/v3/directory/room/{roomAlias}` | 🔶 | Acknowledged, no storage |
| DELETE | `/_matrix/client/v3/directory/room/{roomAlias}` | 🔶 | Acknowledged |
| GET | `/_matrix/client/v3/directory/room/{roomAlias}` | 🔶 | Returns 404 (no alias storage) |

## User Directory

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| POST | `/_matrix/client/v3/user_directory/search` | ✅ | Searches users table |

## Third-Party and Other

| Method | Endpoint | Status | Notes |
|---|---|---|---|
| GET | `/_matrix/client/v3/account/3pid` | 🔶 | Returns empty list (3PIDs not supported) |
| GET | `/_matrix/client/v3/thirdparty/protocols` | 🔶 | Returns {} |
| GET | `/_matrix/client/v3/voip/turnServer` | 🔶 | Returns 404 (TURN not configured) |
| GET | `/_matrix/media/v3/config` | 🔶 | Returns m.upload.size: 10 MiB |

## Current E2EE Stubs

These endpoints are implemented as stubs to prevent Matrix client error dialogs. No actual
key material is stored. See [ADR-011](architecture/adr/ADR-011-managed-e2ee-key-escrow.md) for
the roadmap toward Managed E2EE.

| Method | Endpoint | Status | Stub Behavior |
|---|---|---|---|
| POST | `/_matrix/client/v3/keys/upload` | 🔶 | Returns fake key counts (curve25519: 50) |
| POST | `/_matrix/client/r0/keys/upload` | 🔶 | Same as v3 (legacy compatibility) |
| POST | `/_matrix/client/v3/keys/query` | 🔶 | Returns empty device_keys for known users |
| POST | `/_matrix/client/v3/keys/claim` | 🔶 | Returns empty one_time_keys |
| GET | `/_matrix/client/v3/keys/changes` | 🔶 | Returns changed: [], left: [] |
| POST | `/_matrix/client/v3/keys/device_signing/upload` | 🔶 | UIA dummy flow (m.login.dummy), accepts silently |
| POST | `/_matrix/client/v3/keys/signatures/upload` | 🔶 | Returns failures: {} |
| GET | `/_matrix/client/v3/room_keys/version` | 🔶 | Returns 404 (no backup) |
| POST | `/_matrix/client/v3/room_keys/version` | 🔶 | Returns version: "1" (not stored) |

## Intentionally Excluded

The following endpoint namespaces are deliberately not implemented. Nebu is designed as a
closed-network, non-federated server. These APIs are only relevant for Matrix federation and
identity services.

| Namespace | Reason |
|---|---|
| `/_matrix/federation/*` | No federation — see [README §No Federation](../README.md#no-federation) (closed-network data-sovereignty model; Phase 3 vision) |
| `/_matrix/identity/*` | No identity server — email/phone binding not supported |
| `/_matrix/key/v2/server` | Server key exchange only needed for federation |
| `POST /_matrix/client/v3/search` | Requires ADR-010 (FTS strategy) decision first — see [ADR-010](architecture/adr/ADR-010-fts-strategy.md) |

_Source: `gateway/cmd/gateway/main.go` route registrations; `CLAUDE.md`, §Matrix API Scope; `_bmad-output/planning-artifacts/prd.md`, §Endpoint Specification; Story 9-28 (GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType})_
