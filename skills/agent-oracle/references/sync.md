---
name: sync
description: Complete Matrix Client-Server API v1.18 reference for GET /sync — request parameters, full response structure, all event placement rules, timeline semantics, incremental vs full sync, long-polling, filters, lazy loading, bundled aggregations, and stripped state.
---

# Sync — Full Reference (Matrix CS API v1.18)

**Endpoint:** `GET /_matrix/client/v3/sync`  
**Auth:** Required (Bearer token)

---

## Request Parameters

| Parameter      | Type    | Required | Description |
|----------------|---------|----------|-------------|
| `filter`       | string  | No       | Filter ID (from `POST /filter`) or inline filter JSON. Controls which rooms, event types, senders, and how many events are returned. |
| `since`        | string  | No       | Opaque token from previous `next_batch`. Absent = full sync. |
| `full_state`   | boolean | No       | If `true`: return full room state even in incremental sync. Default `false`. |
| `set_presence` | string  | No       | `online` (default), `offline` (no presence update), `unavailable`. |
| `timeout`      | integer | No       | Long-poll timeout in **milliseconds**. `0` = return immediately. Server MUST NOT exceed this. |

**`since` semantics:** Opaque — clients MUST NOT parse or construct it. If the server no longer has the token (e.g. pruned history), it MUST return a full sync. The server MUST return the most up-to-date `next_batch` even when nothing changed.

---

## Response Structure

```json
{
  "next_batch": "s72595_4483_1934",
  "rooms": {
    "join":   { "!roomId:server": { ...JoinedRoom } },
    "invite": { "!roomId:server": { ...InvitedRoom } },
    "knock":  { "!roomId:server": { ...KnockedRoom } },
    "leave":  { "!roomId:server": { ...LeftRoom } }
  },
  "presence":    { "events": [ ...PresenceEvent ] },
  "account_data": { "events": [ ...AccountDataEvent ] },
  "to_device":   { "events": [ ...ToDeviceEvent ] },
  "device_lists": {
    "changed": [ "@user:server" ],
    "left":    [ "@user:server" ]
  },
  "device_one_time_keys_count": { "signed_curve25519": 20 },
  "device_unused_fallback_key_types": [ "signed_curve25519" ]
}
```

`next_batch` is the ONLY required field. All other top-level keys MAY be omitted if empty.

---

## JoinedRoom Object

```json
{
  "summary": {
    "m.heroes": [ "@alice:server", "@bob:server" ],
    "m.joined_member_count": 5,
    "m.invited_member_count": 1
  },
  "state": {
    "events": [ ...ClientEvent ]
  },
  "timeline": {
    "events": [ ...ClientEvent ],
    "limited": true,
    "prev_batch": "t47409-4357353_219380_26003_2265"
  },
  "ephemeral": {
    "events": [ ...EphemeralEvent ]
  },
  "account_data": {
    "events": [ ...AccountDataEvent ]
  },
  "unread_notifications": {
    "notification_count": 5,
    "highlight_count": 1
  },
  "unread_thread_notifications": {
    "$threadRootEventId": {
      "notification_count": 3,
      "highlight_count": 0
    }
  }
}
```

### `state.events`

- **Full sync (no `since`):** All current state events — a complete snapshot of the room state at the start of the returned timeline.
- **Incremental sync (with `since`):** Only state events that changed since `since`. If `full_state: true`, returns full state regardless.
- State events in `state` represent state at the **start** of the `timeline`, not after it.
- Format: full `ClientEvent` (includes `event_id`, `sender`, `origin_server_ts`, `room_id`).

### `timeline`

- `events`: Chronologically ordered room events (timeline events + state changes that are also in the timeline).
- `limited`: `true` if the server omitted events between the `since` token and the start of this batch. When `limited: true`, `prev_batch` MUST be present.
- `prev_batch`: Pagination token for `GET /rooms/{roomId}/messages?dir=b&from=<prev_batch>` to retrieve the gap. MUST be present when `limited: true`.

### `ephemeral.events`

Ephemeral events are **NOT** in the timeline. They have no `event_id` and are not persisted.

| Event Type  | Where          | Content |
|-------------|----------------|---------|
| `m.typing`  | `ephemeral`    | `{"user_ids": ["@user:server"]}` — complete list of currently typing users in the room |
| `m.receipt` | `ephemeral`    | `{"$eventId": {"m.read": {"@user:server": {"ts": 12345}}, "m.read.private": {"@user:server": {"ts": 12345}}}}` |

**`m.typing` format:**
```json
{
  "type": "m.typing",
  "room_id": "!room:server",
  "content": {
    "user_ids": ["@alice:server", "@bob:server"]
  }
}
```
The `user_ids` list is the **complete** set of currently typing users — not a delta. An empty list means nobody is typing.

**`m.receipt` format:**
```json
{
  "type": "m.receipt",
  "room_id": "!room:server",
  "content": {
    "$eventId": {
      "m.read": {
        "@user:server": { "ts": 1661384124153, "thread_id": "main" }
      },
      "m.read.private": {
        "@user:server": { "ts": 1661384124153 }
      }
    }
  }
}
```
Multiple event IDs and multiple users can appear in one receipt event. `m.read.private` receipts are only visible to the reading user's own devices.

### `account_data.events` (per-room)

Room-scoped account data. Common types:
- `m.tag` — user's tags for the room (`{"tags": {"m.favourite": {"order": 0.5}}}`)
- `m.fully_read` — the event up to which the user has read (`{"event_id": "$..."}`)
- Custom namespaced types

---

## InvitedRoom Object

```json
{
  "invite_state": {
    "events": [ ...StrippedStateEvent ]
  }
}
```

- `invite_state.events` is a **stripped state** — minimal events to identify the room. No timeline events.
- Spec-defined stripped event types to include: `m.room.join_rules`, `m.room.create`, `m.room.name`, `m.room.avatar`, `m.room.canonical_alias`, `m.room.encryption`, `m.room.member` (the invite event for the invited user).

**Stripped state event format — MISSING fields vs full event:**
```json
{
  "type": "m.room.name",
  "state_key": "",
  "content": { "name": "My Room" },
  "sender": "@alice:server"
}
```
No `event_id`, no `origin_server_ts`, no `room_id`, no `unsigned`. These are intentionally stripped.

---

## KnockedRoom Object (v1.1+)

```json
{
  "knock_state": {
    "events": [ ...StrippedStateEvent ]
  }
}
```

Same stripped-state format as invite. Appears when the user has knocked on a room.

---

## LeftRoom Object

```json
{
  "state": {
    "events": [ ...ClientEvent ]
  },
  "timeline": {
    "events": [ ...ClientEvent ],
    "limited": false
  }
}
```

- No `ephemeral` — user is no longer in the room.
- `state` reflects the room state at the time the user left (or was kicked/banned).
- Appears in incremental sync after the leave event; subsequent syncs MAY omit the room.

---

## Top-Level `presence.events`

```json
{
  "type": "m.presence",
  "sender": "@user:server",
  "content": {
    "presence": "online",
    "last_active_ago": 420845,
    "currently_active": true,
    "status_msg": "In a meeting"
  }
}
```

`presence` values: `online`, `offline`, `unavailable`. `last_active_ago` is milliseconds. `currently_active: true` means the user is actively using the client right now (not just recently active).

---

## Top-Level `account_data.events`

Global (not per-room) account data. Common types:
- `m.push_rules` — the user's push rule configuration
- `m.direct` — map of `userId → [roomId]` for DM rooms
- `m.ignored_user_list` — `{"ignored_users": {"@user:server": {}}}`
- `m.identity_server` — configured identity server
- Custom namespaced types

---

## Top-Level `to_device.events`

Messages sent directly to this device (not room events). Used for E2E key exchange, device verification.

```json
{
  "type": "m.room_key",
  "sender": "@alice:server",
  "content": { ... }
}
```

Common types: `m.room_key`, `m.forwarded_room_key`, `m.room_key_request`, `m.key.verification.request`, `m.key.verification.ready`, `m.key.verification.start`, `m.key.verification.accept`, `m.key.verification.key`, `m.key.verification.mac`, `m.key.verification.done`, `m.key.verification.cancel`, `m.secret.request`, `m.secret.send`, `m.dummy`.

---

## `device_lists`

- `changed`: User IDs whose device list changed (added/updated/removed a device). Clients should re-query these users' keys via `/keys/query`.
- `left`: User IDs who left all rooms shared with the syncing user. Clients can forget their keys.

Only present in incremental sync. In full sync, clients must query all known users.

---

## Full Sync vs Incremental Sync

| Aspect | Full sync (no `since`) | Incremental sync (with `since`) |
|--------|------------------------|----------------------------------|
| `rooms.join[].state` | Full current state of all joined rooms | Only changed state events since `since` |
| `rooms.join[].timeline` | Recent events (limited by filter) | New events since `since` |
| `rooms.invite` | All current invites | New invites since `since` |
| `rooms.leave` | Rooms left recently (server-dependent) | Rooms left since `since` |
| `presence` | Presence of known users | Changed presence since `since` |
| `account_data` | All current account data | Changed account data since `since` |
| `device_lists` | Not present; client must query | Users whose device list changed |

**`full_state: true`** in incremental sync: forces `state` to return full current state (as in full sync), but `timeline` still returns only new events since `since`.

---

## Long-Polling Behavior

- `timeout=0` (or absent): Server MUST return immediately, even if nothing changed.
- `timeout=N` (N > 0): Server MUST block up to N milliseconds. Return as soon as any event is available, or after N ms with only `next_batch`.
- Server MUST NOT hold the connection past `timeout`.
- Empty sync (nothing changed): Response contains only `next_batch`. All other keys MAY be omitted.

---

## Filters in Sync

The `filter` parameter can be a filter ID or inline JSON. Key filter fields for sync:

```json
{
  "room": {
    "rooms": ["!include:server"],
    "not_rooms": ["!exclude:server"],
    "include_leave": false,
    "state": {
      "types": ["m.room.member"],
      "not_types": [],
      "senders": [],
      "not_senders": [],
      "lazy_load_members": true,
      "include_redundant_members": false,
      "limit": 10
    },
    "timeline": {
      "types": ["m.room.message"],
      "not_types": ["m.reaction"],
      "limit": 50,
      "lazy_load_members": true
    },
    "ephemeral": {
      "types": ["m.typing"],
      "not_types": []
    },
    "account_data": { "types": [] }
  },
  "presence": {
    "not_types": ["m.presence"],
    "senders": []
  },
  "account_data": {
    "types": ["m.push_rules"]
  },
  "event_fields": ["type", "content", "sender"],
  "event_format": "client"
}
```

**Lazy loading members** (`lazy_load_members: true` in `state` or `timeline`):
- Server MUST only return `m.room.member` state events for users who sent events in the timeline batch.
- Server MUST always include the syncing user's own membership event.
- Significantly reduces payload for large rooms.
- `include_redundant_members: true` overrides: return all previously-sent member events again even if already received.

---

## Bundled Aggregations (`unsigned.m.relations`)

Since v1.3, timeline events MAY include aggregated relation info in `unsigned.m.relations`:

```json
{
  "unsigned": {
    "m.relations": {
      "m.annotation": {
        "chunk": [
          { "type": "m.reaction", "key": "👍", "count": 3 }
        ]
      },
      "m.replace": {
        "event_id": "$latestEditEventId",
        "origin_server_ts": 12345,
        "sender": "@user:server"
      },
      "m.thread": {
        "latest_event": { ...ClientEvent },
        "count": 7,
        "current_user_participated": true
      },
      "m.reference": {
        "chunk": [{ "event_id": "$refEventId" }]
      }
    }
  }
}
```

Bundled aggregations are **informational** — clients MUST NOT rely on them being present. Redacted events have their aggregations removed.

---

## Room Summary (`summary` in JoinedRoom)

Used to calculate a display name for rooms without an explicit name:

- `m.heroes`: Up to 5 user IDs of room members (excluding the syncing user) for constructing the display name. Ordered by last activity time.
- `m.joined_member_count`: Total joined members.
- `m.invited_member_count`: Total invited members.

The spec defines the display name calculation algorithm using heroes — server provides the data, client computes the name.

---

## Event Placement Summary

| Event type         | Where in sync response                          |
|--------------------|--------------------------------------------------|
| `m.room.message`   | `rooms.join[].timeline.events`                  |
| `m.room.encrypted` | `rooms.join[].timeline.events`                  |
| `m.room.redaction` | `rooms.join[].timeline.events`                  |
| `m.sticker`        | `rooms.join[].timeline.events`                  |
| `m.reaction`       | `rooms.join[].timeline.events`                  |
| `m.room.member`    | `rooms.join[].state.events` or timeline if change happens within the timeline window |
| `m.room.create`    | `rooms.join[].state.events` (initial state)     |
| `m.room.power_levels` | `rooms.join[].state.events`                  |
| `m.room.name`      | `rooms.join[].state.events`                     |
| `m.room.topic`     | `rooms.join[].state.events`                     |
| `m.room.avatar`    | `rooms.join[].state.events`                     |
| `m.room.join_rules` | `rooms.join[].state.events`                    |
| `m.room.canonical_alias` | `rooms.join[].state.events`               |
| `m.room.history_visibility` | `rooms.join[].state.events`            |
| `m.room.encryption` | `rooms.join[].state.events`                   |
| `m.room.tombstone` | `rooms.join[].state.events`                     |
| `m.typing`         | `rooms.join[].ephemeral.events` — NOT timeline  |
| `m.receipt`        | `rooms.join[].ephemeral.events` — NOT timeline  |
| `m.presence`       | top-level `presence.events`                     |
| `m.push_rules`     | top-level `account_data.events`                 |
| `m.direct`         | top-level `account_data.events`                 |
| `m.ignored_user_list` | top-level `account_data.events`              |
| `m.tag`            | `rooms.join[].account_data.events`              |
| `m.fully_read`     | `rooms.join[].account_data.events`              |
| `m.room_key`       | top-level `to_device.events`                    |
| `m.key.verification.*` | top-level `to_device.events` (or room timeline for in-room verification) |
| `m.secret.*`       | top-level `to_device.events`                    |

---

## Common Spec Violations to Watch For

1. **`limited: true` without `prev_batch`** — MUST violation. Clients need `prev_batch` to fill the gap.
2. **Ephemeral events in timeline** — `m.typing` and `m.receipt` are never timeline events.
3. **State reflects end of timeline, not start** — state MUST represent state at the **start** of the returned timeline batch.
4. **`next_batch` absent** — MUST always be present, even in empty sync.
5. **Stripped state with `event_id`** — invite/knock state MUST use stripped format (no `event_id`).
6. **`m.typing` as delta** — `user_ids` is the complete current list, not a diff.
7. **`full_state: true` ignored in incremental sync** — MUST be respected.
8. **`timeout` exceeded** — server MUST return within `timeout` ms.
9. **`set_presence: offline` updating presence** — MUST NOT update last-active-ago when set to `offline`.
10. **Room-level `account_data` placed at top level** — `m.tag` and `m.fully_read` are per-room, not global.
