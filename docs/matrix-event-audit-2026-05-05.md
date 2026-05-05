# Matrix Event Correctness Audit — 2026-05-05

**Story:** 9-10a — Matrix Event Correctness Spike (DM-Loop Root Cause)
**Status:** COMPLETE — findings documented 2026-05-05
**Date:** 2026-05-05

---

## Executive Summary

4 areas audited against Matrix Client-Server API v1.18: 1 HIGH DEVIATION (unsigned.age missing from all timeline events in `/sync` responses), 3 PASS (keys/query response format, m.room.encryption state event handling, device_lists / device_one_time_keys_count in sync). The historical DM creation loop root causes (Story 5-29e) were the missing `device_one_time_keys_count` in sync responses and the empty top-level `device_keys` map in `keys/query`; both were fixed. The remaining gap is `unsigned.age` missing from timeline events, which may cause matrix-js-sdk to treat events as stale and trigger re-polling. Story 9-10b must add an `Unsigned` struct field to `syncTimelineEvent` in `sync.go` to close this gap.

---

## Audit Findings

### Finding 1: keys/query Response Format — PASS

**Spec reference:** §11.12.1 — Key Distribution
https://spec.matrix.org/v1.18/client-server-api/#post_matrixclientv3keysquery

**Current behavior:**
For known users: `{"device_keys":{"@user:server":{}},"failures":{}}` — the inner map is an empty object `{}` (no device key objects). Source: `gateway/internal/matrix/keys_query.go` with comment "User known, no devices registered yet (E2EE stub)". For unknown users: the user is omitted from `device_keys` entirely and is NOT listed in `failures`.

**Spec requirement:**
§11.12.1 specifies the response shape as `{ "device_keys": { "@alice:example.com": { "JLAFKJWSCS": { device_key_object } } }, "failures": {} }` where the inner map keys are device IDs. For a homeserver with no E2EE devices registered, returning an empty inner map `{}` per user is spec-compliant — the spec does not mandate that devices must exist. The `failures` field is reserved for remote federation errors; omitting a local unknown user from `device_keys` (rather than listing them in `failures`) is the correct behaviour for a non-federated server.

**Impact on DM creation loop:**
The original DM loop trigger (Story 5-29e) was that `keys/query` returned `{"device_keys":{}}` — an empty top-level object — so Element Web could not determine whether the queried user existed. Story 5-29e fixed this by including `{"@user:server":{}}`. Element Web correctly interprets the empty inner map as "user has no E2EE devices" and proceeds. No loop trigger from this area.

**Classification:** PASS — current behaviour is spec-compliant for an MVP non-E2EE server.
**Godog stub:** `gateway/features/matrix_event_correctness.feature`
  Scenario: KeysQuery_KnownUser_DeviceKeysEntryPresent
  Scenario: KeysQuery_UnknownUser_NotInFailures

---

### Finding 2: m.room.encryption State Event Handling — PASS

**Spec reference:** §11.10 — Room Encryption
https://spec.matrix.org/v1.18/client-server-api/#mroomencryption

**Current behavior:**
Story 9-6 added `m.room.encryption` to the state event type whitelist middleware in `gateway/internal/middleware/`. Story 9-7 wired `Core.SendEvent` for all whitelisted state event types. The integration test `set_room_state_full_test.go` line 189 confirms that `PUT /rooms/{roomId}/state/m.room.encryption` returns HTTP 200 with an `event_id` in the response body.

**Spec requirement:**
§11.10 requires that homeservers accept `m.room.encryption` state events from room members with sufficient power level. Rejecting this event (403 M_FORBIDDEN or 400 M_BAD_JSON) breaks Element Web DM creation because Element Web sends this event to enable encryption immediately after creating a DM room.

**Impact on DM creation loop:**
If `m.room.encryption` were rejected, Element Web would retry the DM creation flow continuously, producing an infinite loop. Stories 9-6 and 9-7 prevent this. The end-to-end path is fully wired.

**Classification:** PASS — `m.room.encryption` is whitelisted and accepted end-to-end.
**Godog stub:** `gateway/features/matrix_event_correctness.feature`
  Scenario: StateEvent_mRoomEncryption_Accepted

---

### Finding 3: unsigned.age in Sync Timeline Events — HIGH DEVIATION

**Spec reference:** §8.4.3 — Unsigned Data
https://spec.matrix.org/v1.18/client-server-api/#unsigned-data

**Current behavior:**
`syncTimelineEvent` struct in `gateway/internal/matrix/sync.go` (approx. lines 295–303):

```go
type syncTimelineEvent struct {
    EventID  string          `json:"event_id"`
    Type     string          `json:"type"`
    Sender   string          `json:"sender"`
    RoomID   string          `json:"room_id"`
    Content  json.RawMessage `json:"content"`
    OriginTS int64           `json:"origin_server_ts"`
}
```

There is no `Unsigned` field. Consequently, `unsigned` is never included in serialized timeline events returned from `/sync`. All events lack the `unsigned.age` field.

**Spec requirement:**
§8.4.3 states that every event sent from a homeserver to a client SHOULD include an `unsigned` object containing `age` (the time in milliseconds since the event was created, from the homeserver's perspective at the time of sending). The matrix-js-sdk (used by Element Web) reads `event.unsigned?.age` for event freshness detection and deduplication. While the spec uses SHOULD (not MUST), the matrix-js-sdk treats missing `unsigned.age` as an edge case that can cause:

1. Incorrect event deduplication — events without `unsigned.age` may be treated as "instant" (age=0), and the SDK may re-fetch them on the next sync cycle.
2. Stale event re-polling — the SDK's lag detection uses `unsigned.age` to determine whether an event has been received on time; without it, the SDK cannot make this determination and may conservatively re-poll.

**Impact on DM creation loop:**
Missing `unsigned.age` is a contributing factor to the observed DM loop behaviour, particularly for the `m.room.member` invite events sent during DM creation. When the SDK does not see `unsigned.age` on these events, it may not advance its internal "received up to" cursor correctly, causing it to re-request events it has already seen. This manifests as the repeating `GET /sync` calls observed in the DM creation network trace.

**Fix scope:** Story 9-10b — add `Unsigned struct { Age int64 \`json:"age"\` }` field to `syncTimelineEvent` and populate it as `time.Now().UnixMilli() - event.OriginTS`.

**Classification:** HIGH DEVIATION — omitting `unsigned.age` violates SHOULD-level spec guidance in §8.4.3 and is a known source of re-polling behaviour in matrix-js-sdk clients.
**Godog stub:** `gateway/features/matrix_event_correctness.feature`
  Scenario: Sync_TimelineEvents_HaveUnsignedAge

---

### Finding 4: device_lists and device_one_time_keys_count in /sync — PASS

**Spec reference:** §8.4 — Sync
https://spec.matrix.org/v1.18/client-server-api/#get_matrixclientv3sync

**Current behavior:**
Story 5-29e introduced `emptySyncDeviceFields()` in `gateway/internal/matrix/sync.go`. Code inspection confirms:
- `DeviceOneTimeKeysCount`: `map[string]int{}` → serializes as `{}` (empty JSON object, NOT null)
- `DeviceUnusedFallbackKeys`: `[]string{}` → serializes as `[]` (empty JSON array, NOT null)
- `DeviceLists.Changed`: `[]string{}` → serializes as `[]` (NOT null)
- `DeviceLists.Left`: `[]string{}` → serializes as `[]` (NOT null)

All sync response construction sites call `emptySyncDeviceFields()`.

**Spec requirement:**
§8.4 requires `device_one_time_keys_count` to be present as a JSON object (not null). If this field is null or absent, the matrix-js-sdk triggers an OTK-upload polling loop: the client reads `device_one_time_keys_count["signed_curve25519"] = 0` (or undefined), uploads new OTKs, re-checks on the next sync, still sees 0, and loops indefinitely. Similarly, `device_lists.changed` and `device_lists.left` must be present as arrays (not null) to prevent device-tracking loops.

**Impact on DM creation loop:**
This was the primary OTK-upload loop trigger identified in Story 5-29e. The fix (`emptySyncDeviceFields()`) correctly prevents the null-triggered loop. The current implementation is sufficient.

**Classification:** PASS — Story 5-29e fix is correct and complete. `emptySyncDeviceFields()` ensures all required fields serialize as non-null.
**Godog stub:** `gateway/features/matrix_event_correctness.feature`
  Scenario: Sync_DeviceFields_NonNull

---

## DM Loop Root Cause

**Historical root causes (Story 5-29e, fixed):**

Two root causes were identified and fixed in Story 5-29e (2026-04-23):

1. **OTK replenishment loop (Bug 4):** `device_one_time_keys_count` was absent from `/sync` responses. Element Web read it as null/undefined, interpreted this as "0 OTKs on server", uploaded new OTKs via `POST /keys/upload`, then re-checked on the next sync — still null — and looped. Fix: `emptySyncDeviceFields()` ensures the field is always `{}`.

2. **Key query loop (Bug 2b):** `POST /keys/query` returned `{"device_keys":{}}` for ALL queries, including queries for known users. Element Web could not determine whether the queried DM partner existed, so it re-queried repeatedly. Fix: return `{"device_keys":{"@user:server":{}}}` (user present with empty inner device map) for known users.

**Remaining gap (HIGH — not yet fixed):**

`unsigned.age` is absent from all timeline events in `/sync` responses (`syncTimelineEvent` struct has no `Unsigned` field). The matrix-js-sdk uses `unsigned.age` for event deduplication and lag tracking. Without it, the SDK may not advance its internal sync cursor correctly for events received during DM creation (particularly `m.room.member` invite events), causing it to re-request the same events on subsequent syncs. This manifests as the repeating `GET /sync` + `POST /keys/query` calls still observable in the DM creation flow.

**DM creation flow with current state:**

```
Client → POST /createRoom (is_direct:true, invite:["@bob"])    → 200 [roomId]
Client → GET  /sync                                             → 200 [m.room.member events, NO unsigned.age]
Client → POST /keys/query {"device_keys":{"@bob:...":[]}}      → 200 {"device_keys":{"@bob:...":{}}}
Client → PUT  /rooms/{roomId}/state/m.room.encryption           → 200 [event_id]
Client → GET  /sync                                             → 200 [m.room.encryption event, NO unsigned.age]
                                                    ↑ May re-request events due to missing unsigned.age
```

The loop is no longer the hard infinite loop observed before Story 5-29e (OTK loop is fixed, keys/query map is fixed), but the missing `unsigned.age` causes sporadic re-polling of already-seen events, keeping the DM creation spinner alive longer than expected. Story 9-10b must add `unsigned.age` to close this gap.

---

## Findings Summary Table

| Finding | Area | Classification | Rating | Godog Stub Scenario | Story 9-10b AC |
|---------|------|----------------|--------|---------------------|----------------|
| 1 | keys/query response format | PASS | — | KeysQuery_KnownUser_DeviceKeysEntryPresent | AC4 |
| 2 | m.room.encryption state event | PASS | — | StateEvent_mRoomEncryption_Accepted | AC1 |
| 3 | unsigned.age in timeline events | DEVIATION | HIGH | Sync_TimelineEvents_HaveUnsignedAge | AC3 |
| 4 | device_lists / OTK count in sync | PASS | — | Sync_DeviceFields_NonNull | AC1/AC2 |

---

## Spec Citations Reference

All spec references point to Matrix Client-Server API v1.18:

| Section | Title | URL |
|---------|-------|-----|
| §8.4 | Sync | https://spec.matrix.org/v1.18/client-server-api/#get_matrixclientv3sync |
| §8.4.3 | Unsigned Data | https://spec.matrix.org/v1.18/client-server-api/#unsigned-data |
| §11.10 | Room Encryption | https://spec.matrix.org/v1.18/client-server-api/#mroomencryption |
| §11.12.1 | Key Distribution | https://spec.matrix.org/v1.18/client-server-api/#post_matrixclientv3keysquery |
