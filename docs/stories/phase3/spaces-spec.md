# Matrix Spaces — Vollständige Spezifikation für Nebu Phase 3

**Erstellt:** 2026-05-11  
**Status:** Draft  
**Autor:** Philipp Beyerlein  
**Zielversion:** Matrix Client-Server API v1.12+

---

## 1. Überblick

Matrix Spaces (ehemals MSC1772) ist ein stabiles Feature der Matrix-Spezifikation seit v1.2. Spaces sind spezielle Rooms vom Typ `m.space`, die andere Räume und Sub-Spaces als Kinder referenzieren. Sie bilden eine Hierarchie, über die Clients (Element Web, FluffyChat) eine strukturierte Navigation ähnlich wie Slack-Workspaces oder Discord-Server darstellen können.

### 1.1 Relevante MSCs und Spec-Referenzen

| MSC / Spec | Titel | Status in Matrix-Spec | Für Nebu relevant |
|---|---|---|---|
| [MSC1772](https://github.com/matrix-org/matrix-spec-proposals/pull/1772) | Spaces (Room Type `m.space`) | **Stable** seit v1.2 | Core-Feature |
| [MSC2946](https://github.com/matrix-org/matrix-spec-proposals/pull/2946) | Spaces Summary / Hierarchy API | **Stable** seit v1.2 | Core-Feature (`/hierarchy`) |
| [MSC3083](https://github.com/matrix-org/matrix-spec-proposals/pull/3083) | Restricted Join Rules | **Stable** seit v1.2 | Space-geschützte Räume |
| [MSC3289](https://github.com/matrix-org/matrix-spec-proposals/pull/3289) | Room Type field in `m.room.create` | **Stable** seit v1.2 | Teil von MSC1772 |
| [MSC3664](https://github.com/matrix-org/matrix-spec-proposals/pull/3664) | Pushrules für Space-Rooms | **Stable** seit v1.7 | Notifications in Spaces |
| [MSC2753](https://github.com/matrix-org/matrix-spec-proposals/pull/2753) | Peeking via `/sync` | Deprecated/Draft | **Nicht implementieren** |
| [MSC3006](https://github.com/matrix-org/matrix-spec-proposals/pull/3006) | Space-Knocking | Merged in v1.2 | Phase 3 MVP optional |

**Spec-Referenzen:**
- https://spec.matrix.org/v1.12/client-server-api/#spaces
- https://spec.matrix.org/v1.12/client-server-api/#get_matrixclientv1roomsroomidgethierarchy
- https://spec.matrix.org/v1.12/client-server-api/#restricted-rooms

---

## 2. Konzeptmodell

```
Space "Engineering"  (!space1:nebu.example)
├── #general:nebu.example         (m.space.child)
├── #backend:nebu.example         (m.space.child, suggested: true)
├── Sub-Space "Frontend"          (m.space.child, room_type: m.space)
│   ├── #frontend-general:nebu.example
│   └── #frontend-design:nebu.example
└── #incidents:nebu.example       (m.space.child, suggested: false)
```

Ein Space ist ein normaler Room mit folgenden Besonderheiten:

1. `m.room.create` enthält `content.type: "m.space"`
2. Kinder werden über `m.space.child` State Events referenziert (State-Key = Child-Room-ID)
3. Kinder können optional über `m.space.parent` den Eltern-Space zurückverlinken
4. Räume in einem Space können mit `join_rule: "restricted"` nur für Space-Mitglieder geöffnet werden

---

## 3. State Events

### 3.1 `m.room.create` — Room-Typ-Feld

Um einen Space zu erstellen, muss `createRoom` das Feld `type` in `creation_content` erhalten:

```json
{
  "creation_content": {
    "type": "m.space"
  },
  "name": "Engineering",
  "topic": "Alle Engineering-Themen",
  "preset": "private_chat"
}
```

**Regeln:**
- `type` ist optional. Fehlt er, ist es ein normaler Room.
- Gültige Werte: `"m.space"` (weitere Typen über MSC3289 möglich, aber nicht für Nebu MVP relevant).
- Das `type`-Feld wird in das `m.room.create` Event als `content.type` übernommen.
- Clients und Server nutzen `content.type == "m.space"` um Spaces von normalen Räumen zu unterscheiden.
- **Rückwärtskompatibilität:** Normaler `createRoom` ohne `type` bleibt unverändert.

**Gespeichertes `m.room.create` Event:**
```json
{
  "type": "m.room.create",
  "state_key": "",
  "content": {
    "creator": "@alice:nebu.example",
    "room_version": "10",
    "type": "m.space"
  }
}
```

---

### 3.2 `m.space.child` — Kind-Referenz

Ein Space referenziert seine Kinder über State Events vom Typ `m.space.child`. Der State-Key ist die Room-ID des Kind-Raums.

**Inhalt (gültige Verlinkung):**
```json
{
  "via": ["nebu.example"],
  "order": "a",
  "suggested": false
}
```

**Inhalt (Verlinkung entfernen):**
```json
{}
```

| Feld | Typ | Pflicht | Beschreibung |
|---|---|---|---|
| `via` | `[string]` | Ja (für gültige Verlinkung) | Liste von Homeservern, über die der Raum erreichbar ist. Da Nebu kein Federation unterstützt, wird hier immer `[server_name]` des lokalen Servers eingetragen. Ein leeres Array `[]` ist semantisch äquivalent zu einem leeren Content (keine gültige Verlinkung). |
| `order` | `string` | Nein | Optionaler Sortierschlüssel (lexikographisch). Räume ohne `order` kommen nach Räumen mit `order`. Erlaubt: US-ASCII `\x20–\x7E`, max. 50 Zeichen. |
| `suggested` | `boolean` | Nein | `true` = der Space schlägt diesen Raum Nicht-Mitgliedern vor. Default: `false`. |

**Wer darf `m.space.child` setzen?**
- Nur Mitglieder des **Space-Rooms** mit ausreichendem Power Level (State-Default, typisch 50 oder 100).
- Der Sender muss Mitglied des Kind-Raums **nicht** zwingend sein (Space-Admin verwaltet die Hierarchie).
- Empfehlung für Default Power Levels in Spaces: `{ "state_default": 50, "events_default": 0 }` — so können Moderatoren Kinder hinzufügen, aber nur Admins Räume erstellen.

**Wann gilt eine Verlinkung als gültig?**
Ein `m.space.child` Event verweist auf einen gültigen Kind-Raum, wenn:
1. `via` nicht leer ist, **und**
2. Der referenzierte Raum existiert (für `/hierarchy` wird er nur angezeigt, wenn der anfragende User Lesezugriff hat oder der Raum `world_readable` / `guest_can_join` ist oder der User Mitglied ist).

---

### 3.3 `m.space.parent` — Eltern-Rückverlinkung

Ein Raum kann optional auf seinen Eltern-Space zurückverlinken. Dieser State Event ist optional, erhöht aber die Navigierbarkeit in Clients.

**State-Key:** Room-ID des Eltern-Space.

**Inhalt:**
```json
{
  "via": ["nebu.example"],
  "canonical": true
}
```

| Feld | Typ | Pflicht | Beschreibung |
|---|---|---|---|
| `via` | `[string]` | Ja | Wie bei `m.space.child` — lokaler Server. |
| `canonical` | `boolean` | Nein | `true` = dieser Space ist der primäre/kanonische Eltern-Space. Nur ein Eltern-Space sollte `canonical: true` tragen. Default: `false`. |

**Wer darf `m.space.parent` setzen?**
Entweder:
- Ein User, der ausreichendes Power Level im **Kind-Raum** hat, **oder**
- Ein User, der ausreichendes Power Level im **Eltern-Space** hat.

**Verifikation durch Server:** Der Server SOLLTE prüfen, ob der Sender tatsächlich Zugriff auf den referenzierten Eltern-Space hat, um Spam-Verlinkungen zu verhindern. Konkret: Der Sender muss Mitglied des Eltern-Space mit ausreichendem PL sein. Schlägt die Verifikation fehl, wird der Event abgelehnt mit `M_FORBIDDEN`.

> **Nebu-Entscheidung:** `m.space.parent` Validierung implementieren — ohne diese würde jeder beliebige User jeden Space als seinen "Eltern-Space" deklarieren können.

---

### 3.4 `m.room.join_rules` — Restricted Join Rule (MSC3083)

Räume in einem Space können so konfiguriert werden, dass nur Mitglieder bestimmter Spaces beitreten können:

```json
{
  "join_rule": "restricted",
  "allow": [
    {
      "type": "m.room_membership",
      "room_id": "!spaceId:nebu.example"
    },
    {
      "type": "m.room_membership",
      "room_id": "!anotherSpaceId:nebu.example"
    }
  ]
}
```

**Join-Entscheidungslogik:**
1. Ist der anfragende User bereits Mitglied des Raums → **ablehnen** (schon beigetreten).
2. Hat der User eine Einladung → **beitreten** (Einladungen überschreiben Restricted).
3. Ist der User Mitglied **mindestens eines** der in `allow` aufgelisteten Räume → **beitreten erlaubt**.
4. Sonst → `M_FORBIDDEN`.

**Gültige `allow`-Einträge:**

| Typ | Bedeutung |
|---|---|
| `m.room_membership` | User muss in dem angegebenen Raum/Space Mitglied (joined) sein |

Andere `type`-Werte: Server ignoriert unbekannte Typen (keine Fehler, aber kein Erlaubnis-Grant).

**Room Version Requirement:** `restricted` Join Rules erfordern Room Version 8 oder höher (oder Version 9 für korrekte Restricted + Knocking-Kombination). Da Nebu kein Federation betreibt, ist die Anforderung an die Room Version weniger kritisch — aber für Element Web-Kompatibilität sollten Spaces in Room Version 10 (aktuelle empfohlene Standardversion) erstellt werden.

**Capabilities-Update:**
`GET /_matrix/client/v3/capabilities` muss `m.room_versions` mit `restricted`-Support anzeigen:
```json
{
  "capabilities": {
    "m.room_versions": {
      "default": "10",
      "available": {
        "6": "stable",
        "7": "stable",
        "8": "stable",
        "9": "stable",
        "10": "stable"
      }
    },
    "m.change_password": { "enabled": false }
  }
}
```

---

## 4. HTTP-Endpunkte

### 4.1 Neue Endpunkte

#### `GET /_matrix/client/v1/rooms/{roomId}/hierarchy`

**MSC:** MSC2946 — Stable seit Matrix v1.2  
**Zweck:** Liefert die Raum-Hierarchie eines Space (BFS-Traversal über `m.space.child` State Events).  
**Auth:** Bearer Token erforderlich.

**Query-Parameter:**

| Parameter | Typ | Default | Beschreibung |
|---|---|---|---|
| `from` | `string` | — | Paginierungstoken aus vorherigem Response (opaque, server-seitig). |
| `limit` | `integer` | 50 | Maximale Anzahl Räume pro Seite. Server-seitiges Maximum: 1000. |
| `max_depth` | `integer` | ∞ (server-limit) | Maximale Traversierungstiefe. `0` = nur Root-Space ohne Kinder. Server-seitiges Maximum: 10 (konfigurierbar). |
| `suggested_only` | `boolean` | `false` | Nur Räume mit `suggested: true` in ihrem `m.space.child` Event zurückgeben. |

**Response 200:**
```json
{
  "rooms": [
    {
      "room_id": "!spaceId:nebu.example",
      "room_type": "m.space",
      "name": "Engineering",
      "topic": "Alle Engineering-Themen",
      "canonical_alias": "#engineering:nebu.example",
      "num_joined_members": 42,
      "avatar_url": "mxc://nebu.example/abc123",
      "world_readable": false,
      "guest_can_join": false,
      "join_rule": "invite",
      "children_state": [
        {
          "type": "m.space.child",
          "state_key": "!generalRoomId:nebu.example",
          "content": {
            "via": ["nebu.example"],
            "suggested": true
          },
          "sender": "@alice:nebu.example",
          "origin_server_ts": 1715000000000
        }
      ]
    },
    {
      "room_id": "!generalRoomId:nebu.example",
      "name": "general",
      "num_joined_members": 38,
      "world_readable": false,
      "guest_can_join": false,
      "join_rule": "restricted",
      "children_state": []
    }
  ],
  "next_batch": "t47_opaque_token"
}
```

**Response-Felder pro Raum (SpaceSummary):**

| Feld | Typ | Pflicht | Beschreibung |
|---|---|---|---|
| `room_id` | `string` | Ja | Die Room ID. |
| `room_type` | `string` | Nein | `"m.space"` wenn es ein Space ist, sonst fehlt das Feld. |
| `name` | `string` | Nein | Aus `m.room.name` State Event (content.name). |
| `topic` | `string` | Nein | Aus `m.room.topic` State Event (content.topic). |
| `canonical_alias` | `string` | Nein | Aus `m.room.canonical_alias`. |
| `num_joined_members` | `integer` | Ja | Anzahl aktuell beigetretener Mitglieder. |
| `avatar_url` | `string` | Nein | mxc:// URI aus `m.room.avatar`. |
| `world_readable` | `boolean` | Ja | `true` wenn `m.room.history_visibility` = `world_readable`. |
| `guest_can_join` | `boolean` | Ja | `true` wenn `m.room.guest_access` = `can_join`. |
| `join_rule` | `string` | Nein | Wert aus `m.room.join_rules` (z.B. `"invite"`, `"restricted"`, `"public"`). |
| `children_state` | `[StrippedStateEvent]` | Ja | Alle `m.space.child` State Events des Raums als Stripped State (ohne `event_id`, aber mit `sender`, `origin_server_ts`). Nur für Räume, auf die der User Lesezugriff hat. |

**Sichtbarkeitsregeln (wer sieht welche Räume in der Hierarchy):**

Ein Raum wird in `/hierarchy` zurückgegeben, wenn **mindestens eine** der folgenden Bedingungen gilt:
1. Der anfragende User ist Mitglied des Raums, **oder**
2. `world_readable: true` (history_visibility = world_readable), **oder**
3. `guest_can_join: true` (guest_access = can_join), **oder**
4. `join_rule = "public"`, **oder**
5. `join_rule = "restricted"` — Space-Mitglied kann sehen, aber nicht zwingend beitreten.

Räume, auf die der User keinerlei Zugriff hätte und die nicht öffentlich sichtbar sind, werden **übersprungen** (kein Error, nur not included). Die Traversierung geht trotzdem durch den Raum hindurch (seine Kinder werden geprüft).

**Traversierungsalgorithmus (BFS):**
```
Queue = [rootSpaceId]
Visited = {}
Result = []

while Queue not empty and |Result| < limit:
  roomId = Queue.dequeue()
  if roomId in Visited: continue
  Visited.add(roomId)
  
  if user can see roomId:
    summary = buildSpaceSummary(roomId)
    Result.append(summary)
  
  if depth < max_depth:
    children = getSpaceChildren(roomId)  // m.space.child state events mit via != []
    for child in children (sorted by order, then room_id):
      Queue.enqueue(child.state_key)
```

**Paginierung:** Der Server teilt die BFS-Traversierung bei `limit` ab und gibt einen opaken `next_batch`-Token zurück. Der Token kodiert den BFS-Zustand (Queue, Visited-Set, aktuelle Tiefe). Beim nächsten Aufruf mit `from=<token>` wird die BFS fortgesetzt.

**Fehler:**

| HTTP Status | Fehlercode | Bedingung |
|---|---|---|
| 403 | `M_FORBIDDEN` | Anfragender User ist kein Mitglied des Root-Space und hat auch kein öffentliches Sichtrecht. |
| 404 | `M_NOT_FOUND` | Room existiert nicht. |
| 400 | `M_BAD_PARAM` | Ungültiger `from`-Token, negativer `limit`-Wert. |
| 400 | `M_BAD_PARAM` | `limit` überschreitet Server-Maximum. |

---

### 4.2 Modifizierte bestehende Endpunkte

#### `POST /_matrix/client/v3/createRoom`

**Änderung:** `creation_content.type` wird durchgereicht und in `m.room.create` gespeichert.

**Neues Verhalten:**
- Wenn `creation_content.type == "m.space"`, wird der Room als Space angelegt.
- Die Gateway-Schicht reicht `type` via gRPC an Core weiter.
- Core speichert `type` als Teil des `m.room.create` Event-Contents.
- Keine anderen Änderungen am Create-Flow.

**Validierung:**
- `type` muss ein String sein. Unbekannte Typen werden akzeptiert (forward-compatibility), aber nur `"m.space"` hat in Nebu MVP spezielle Semantik.
- `type` darf nicht `null` sein (aber darf fehlen).

**Beispiel-Request:**
```json
{
  "name": "Engineering",
  "preset": "private_chat",
  "creation_content": {
    "type": "m.space"
  },
  "initial_state": [
    {
      "type": "m.room.history_visibility",
      "content": { "history_visibility": "shared" }
    }
  ],
  "power_level_content_override": {
    "events_default": 0,
    "state_default": 50,
    "users_default": 0,
    "events": {
      "m.space.child": 50,
      "m.room.name": 100,
      "m.room.topic": 100
    }
  }
}
```

---

#### `POST /_matrix/client/v3/join/{roomIdOrAlias}` und `POST /_matrix/client/v3/rooms/{roomId}/join`

**Änderung:** Restricted Join Rule muss geprüft werden.

**Prüfsequenz für `join_rule: "restricted"`:**
1. Hat der User eine Einladung? → **Erlaubt** (weiter zu Schritt 5).
2. `allow`-Array aus `m.room.join_rules` lesen.
3. Für jeden Eintrag mit `type: "m.room_membership"`: Prüfe, ob User Mitglied (`join`) in `room_id` ist.
4. Ist der User in **mindestens einem** der `allow`-Räume Mitglied? → **Erlaubt**.
5. Sonst → `403 M_FORBIDDEN` mit `error: "You are not allowed to join this room"`.

**Elixir-Core-Implementierung:**
- `RoomGenServer.join/2` erhält den anfragenden User.
- Core liest `m.room.join_rules` aus dem eigenen State.
- Membership-Check gegen die `allow`-Räume über Session Manager (ETS-Lookup).
- Kein gRPC-Roundtrip nötig, da Core alle Session- und Membership-Daten lokal hält.

**Neuer gRPC-Request-Parameter:**
```protobuf
message JoinRoomRequest {
  string room_id = 1;
  string user_id = 2;
  string via_server = 3;  // existing
  // no new fields needed — Core resolves membership internally
}
```

---

#### `PUT /_matrix/client/v3/rooms/{roomId}/state/m.space.child/{childRoomId}`

**Änderung:** Neuer State Event Type — muss im Gateway-Whitelist und Core-Handler unterstützt werden.

**Gateway-Whitelist:** `m.space.child` muss in `gateway/internal/middleware/` zur erlaubten State-Event-Liste hinzugefügt werden.

**Core-Validierung:**
1. Sender muss Mitglied des Space (roomId) sein mit ausreichendem Power Level für State Events.
2. `childRoomId` muss eine valide Matrix Room ID sein (`!<opaque>:<server>`).
3. Wenn Content nicht leer: `via` muss vorhanden und nicht-leer sein.
4. `order`-Feld: falls vorhanden, muss es US-ASCII `\x20–\x7E` sein, max. 50 Zeichen.
5. `suggested`-Feld: falls vorhanden, muss boolean sein.

**Fehler:**

| HTTP Status | Fehlercode | Bedingung |
|---|---|---|
| 403 | `M_FORBIDDEN` | Insufficient power level. |
| 400 | `M_BAD_JSON` | `via` fehlt bei nicht-leerem Content. |
| 400 | `M_BAD_JSON` | `order` enthält ungültige Zeichen oder > 50 Zeichen. |
| 404 | `M_NOT_FOUND` | Space-Room existiert nicht. |

---

#### `PUT /_matrix/client/v3/rooms/{roomId}/state/m.space.parent/{parentSpaceId}`

**Änderung:** Neuer State Event Type — analog zu `m.space.child`.

**Core-Validierung:**
1. Sender muss Mitglied des Kind-Raums (roomId) **oder** des Eltern-Space (parentSpaceId) mit ausreichendem Power Level sein.
2. `parentSpaceId` muss valide Matrix Room ID sein.
3. Der Eltern-Space muss existieren.
4. Wenn Content nicht leer: `via` muss vorhanden und nicht-leer sein.
5. **Sicherheitscheck:** Falls der Sender kein Mitglied des Eltern-Space ist und nicht ausreichendes PL dort hat, muss das `m.space.parent` Event abgelehnt werden (`M_FORBIDDEN`). Begründung: Verhinderung von Spam-Verlinkungen.

---

#### `GET /_matrix/client/v3/sync`

**Änderung:** Keine strukturelle Änderung am Sync-Response. Spaces sind normale Rooms in `rooms.join`.

**Neu in Sync:**
- `m.space.child` und `m.space.parent` State Events erscheinen in `rooms.join[roomId].state.events` wie alle anderen State Events.
- `rooms.join[spaceId].timeline.events` enthält `m.space.child`-Änderungen als Timeline-Events, wenn ein Kind hinzugefügt/entfernt wird.
- `account_data` für Spaces: Clients speichern Space-spezifische Daten über `m.direct`, `m.push_rules` etc. — keine Nebu-seitige Änderung nötig.

**Space-Type in Sync-Response:**
Im `rooms.join`-Objekt liefert Nebu für jeden Raum bei Initial-Sync seinen `m.room.create` State Event — dieser enthält `content.type: "m.space"`. Clients leiten daraus die Space-Unterscheidung ab.

---

#### `GET /_matrix/client/v3/capabilities`

**Änderung:** Room Version Support für Restricted Rooms anzeigen.

**Vorher (aktuell):**
```json
{
  "capabilities": {
    "m.room_versions": { "default": "6", "available": { "6": "stable" } },
    "m.change_password": { "enabled": false }
  }
}
```

**Nachher (Phase 3):**
```json
{
  "capabilities": {
    "m.room_versions": {
      "default": "10",
      "available": {
        "6": "stable",
        "7": "stable",
        "8": "stable",
        "9": "stable",
        "10": "stable"
      }
    },
    "m.change_password": { "enabled": false }
  }
}
```

> **Wichtig:** Die Angabe von Room Version 8+ ist notwendig, damit Element Web `join_rule: "restricted"` als valide erkennt und die Option in der Raumkonfiguration anzeigt.

---

## 5. Events in der Sync-Response — vollständige Felder

### 5.1 Stripped State Events (in `/hierarchy` `children_state`)

```json
{
  "type": "m.space.child",
  "state_key": "!childRoomId:nebu.example",
  "sender": "@alice:nebu.example",
  "origin_server_ts": 1715000000000,
  "content": {
    "via": ["nebu.example"],
    "order": "a",
    "suggested": true
  }
}
```

Stripped State Events haben **kein** `event_id`-Feld (spec-konform für Hierarchy-Response).

### 5.2 Volle State Events (in `/sync` und `/rooms/{roomId}/state`)

```json
{
  "type": "m.space.child",
  "state_key": "!childRoomId:nebu.example",
  "event_id": "$contentHashEventId",
  "sender": "@alice:nebu.example",
  "room_id": "!spaceId:nebu.example",
  "origin_server_ts": 1715000000000,
  "unsigned": {
    "age": 42
  },
  "content": {
    "via": ["nebu.example"],
    "order": "a",
    "suggested": true
  }
}
```

---

## 6. Power Levels — Empfehlungen für Spaces

### 6.1 Default Power Levels bei Space-Erstellung

Wenn kein `power_level_content_override` angegeben wird, gelten die Matrix-Defaults. Für Spaces empfehlen wir folgende Defaults, die Nebu beim `createRoom` mit `type: "m.space"` automatisch setzt (sofern kein Override angegeben):

```json
{
  "events_default": 0,
  "state_default": 50,
  "ban": 50,
  "kick": 50,
  "redact": 50,
  "invite": 0,
  "users_default": 0,
  "events": {
    "m.space.child": 50,
    "m.space.parent": 50,
    "m.room.name": 100,
    "m.room.topic": 50,
    "m.room.avatar": 50,
    "m.room.power_levels": 100,
    "m.room.history_visibility": 100
  }
}
```

**Begründung:** Space-Moderatoren (PL 50) können Kinder verwalten und Topics ändern, aber der Space-Name und die Power Levels bleiben Admin-only (PL 100).

### 6.2 Power Level-Rollen im Space-Kontext

| Power Level | Rolle | Rechte |
|---|---|---|
| 0 | Mitglied | Nachrichten lesen (wenn Raum es erlaubt), Sub-Spaces sehen |
| 50 | Moderator | Kinder hinzufügen/entfernen (`m.space.child`), Topic ändern |
| 100 | Admin | Space-Name, Power Levels, Raum löschen/archivieren |

---

## 7. Fehlercodes — vollständige Liste für Spaces

| HTTP-Status | Matrix-Fehlercode | Beschreibung | Trigger |
|---|---|---|---|
| 400 | `M_BAD_JSON` | Ungültiger Event-Content | `m.space.child` ohne `via` bei nicht-leerem Content; `order` mit ungültigen Zeichen |
| 400 | `M_BAD_PARAM` | Ungültiger Query-Parameter | `limit` < 0, ungültiger `from`-Token in `/hierarchy` |
| 400 | `M_UNKNOWN_TOKEN` / `M_MISSING_TOKEN` | Kein gültiger Auth-Token | Alle Space-Endpunkte |
| 403 | `M_FORBIDDEN` | Kein Zugriff | `m.space.parent` ohne PL im Eltern-Space; Join bei `restricted` ohne Space-Mitgliedschaft; `/hierarchy` auf privaten Space ohne Mitgliedschaft |
| 404 | `M_NOT_FOUND` | Raum existiert nicht | `/hierarchy` für unbekannte Room ID; `m.space.parent` mit unbekanntem Parent |
| 429 | `M_LIMIT_EXCEEDED` | Rate Limit | `/hierarchy` (rechenintensiv — separates Rate-Limit empfohlen) |

---

## 8. Architektur-Implikationen für Nebu

### 8.1 Go Gateway

**Neue Routen:**
```go
// In gateway/cmd/gateway/main.go
router.GET("/_matrix/client/v1/rooms/:roomId/hierarchy", auth(matrix.GetSpaceHierarchyHandler))
```

**Whitelist-Erweiterung** (`gateway/internal/middleware/state_event_whitelist.go` o.ä.):
```go
var allowedStateEventTypes = map[string]bool{
  // ... existing ...
  "m.space.child":  true,
  "m.space.parent": true,
}
```

**Handler:** `gateway/internal/matrix/spaces.go` — neues File für Space-spezifische Handler.

**Capabilities-Update** (`gateway/internal/matrix/capabilities.go`):
- Room Versions auf 6–10 erweitern.
- Default auf `"10"` setzen.

### 8.2 gRPC — Neuer Handler

```protobuf
// proto/core_service.proto

message GetSpaceHierarchyRequest {
  string space_id = 1;       // Root Space Room ID
  string user_id  = 2;       // Anfragender User (für Sichtbarkeitsprüfung)
  int32  max_depth = 3;       // 0 = unbegrenzt (server-limit)
  int32  limit = 4;           // max Räume pro Page
  bool   suggested_only = 5;
  string from_token = 6;     // Paginierung, leer = Anfang
}

message SpaceSummaryRoom {
  string room_id = 1;
  string room_type = 2;       // "m.space" oder leer
  string name = 3;
  string topic = 4;
  string canonical_alias = 5;
  int32  num_joined_members = 6;
  string avatar_url = 7;
  bool   world_readable = 8;
  bool   guest_can_join = 9;
  string join_rule = 10;
  repeated bytes children_state_json = 11;  // JSON-encoded stripped state events
}

message GetSpaceHierarchyResponse {
  repeated SpaceSummaryRoom rooms = 1;
  string next_batch_token = 2;  // leer = kein weiterer Page
}

service CoreService {
  // ... existing RPCs ...
  rpc GetSpaceHierarchy(GetSpaceHierarchyRequest) returns (GetSpaceHierarchyResponse);
}
```

**Alternative (kein neuer gRPC-Handler):** Gateway macht mehrere `GetRoomState`-Aufrufe für BFS. Das ist funktional korrekt aber **ineffizient** bei tiefen Hierarchien (N+1-Problem). Empfehlung: **Dedizierter gRPC-Handler**.

### 8.3 Elixir/OTP Core

#### Room Manager — Space Hierarchy

**Neues Modul:** `core/apps/room_manager/lib/nebu/room_manager/space_hierarchy.ex`

```elixir
defmodule Nebu.RoomManager.SpaceHierarchy do
  @moduledoc """
  BFS-Traversal über m.space.child State Events für /hierarchy.
  """
  
  @max_depth_limit 10
  @max_rooms_limit 1000
  
  def get_hierarchy(space_id, user_id, opts \\ []) do
    max_depth     = min(opts[:max_depth] || @max_depth_limit, @max_depth_limit)
    limit         = min(opts[:limit] || 50, @max_rooms_limit)
    suggested_only = opts[:suggested_only] || false
    from_token    = opts[:from_token]
    
    # BFS mit Paginierungs-Resume via from_token
    bfs_traverse(space_id, user_id, max_depth, limit, suggested_only, from_token)
  end
  
  defp bfs_traverse(root_id, user_id, max_depth, limit, suggested_only, from_token) do
    # ... BFS-Implementierung
  end
  
  defp can_user_see_room?(room_id, user_id) do
    # Membership check + world_readable + guest_can_join + join_rule = public
  end
  
  defp get_space_children(room_id) do
    # Alle m.space.child State Events des Raums mit via != []
    RoomGenServer.get_state_events_by_type(room_id, "m.space.child")
    |> Enum.filter(fn e -> e.content["via"] not in [nil, []] end)
  end
end
```

#### Room GenServer — Restricted Join Rule

**Bestehender Join-Handler** (`handle_call({:join, user_id}, ...)`):

```elixir
defp check_join_allowed(state, user_id) do
  join_rules_event = get_state(state, "m.room.join_rules", "")
  join_rule = get_in(join_rules_event, [:content, "join_rule"]) || "invite"
  
  case join_rule do
    "public" ->
      :ok
    "invite" ->
      check_invitation(state, user_id)
    "restricted" ->
      check_restricted_join(state, user_id, join_rules_event)
    "knock" ->
      check_knock(state, user_id)
    _ ->
      {:error, :m_forbidden}
  end
end

defp check_restricted_join(state, user_id, join_rules_event) do
  # Einladung überschreibt restricted
  case check_invitation(state, user_id) do
    :ok -> :ok
    _ ->
      allow_list = get_in(join_rules_event, [:content, "allow"]) || []
      room_ids = for %{"type" => "m.room_membership", "room_id" => rid} <- allow_list, do: rid
      
      if Enum.any?(room_ids, &SessionManager.is_member?(user_id, &1)) do
        :ok
      else
        {:error, :m_forbidden}
      end
  end
end
```

### 8.4 PostgreSQL

**Kein neues Schema erforderlich.** `m.space.child` und `m.space.parent` sind normale State Events und werden in der bestehenden `events`-Tabelle gespeichert.

**Empfohlene Index-Optimierung für Hierarchy-Queries:**

```sql
-- Migration: 000046_space_hierarchy_index.up.sql
-- Composite index für schnellen BFS-Lookup: alle m.space.child Events eines Raums
CREATE INDEX CONCURRENTLY idx_events_space_child
  ON events (room_id, state_key)
  WHERE event_type = 'm.space.child';

-- Index für m.space.parent lookup (Kind → Eltern-Space)
CREATE INDEX CONCURRENTLY idx_events_space_parent
  ON events (state_key)  -- state_key = parent space ID
  WHERE event_type = 'm.space.parent';
```

---

## 9. Sync-Response-Erweiterungen — Details

### 9.1 Initial Sync (kein `since`-Token)

Für einen Space-Room enthält `rooms.join[spaceId].state.events`:
- `m.room.create` mit `content.type: "m.space"` — **hier erkennt der Client den Space-Typ**
- Alle `m.space.child` State Events (ein Event pro Kind-Raum)
- Alle `m.space.parent` State Events (ein Event pro Eltern-Space)
- Alle anderen Standard-State-Events (`m.room.name`, `m.room.join_rules` etc.)

### 9.2 Incremental Sync

Wenn ein Kind hinzugefügt/entfernt wird:
- `rooms.join[spaceId].timeline.events` enthält das neue/geänderte `m.space.child` Event.
- `rooms.join[spaceId].state.events` enthält das Event **nicht** (State-Updates kommen nur bei Initial-Sync im `state`-Array, danach nur noch in `timeline`).

### 9.3 `m.direct` Account Data und Spaces

Spaces sind keine Direct-Message-Räume. `m.direct` Account Data wird durch Spaces nicht verändert. DMs im Kontext eines Space bleiben normale DM-Räume.

---

## 10. Element Web — Kompatibilitätsanforderungen

Element Web nutzt folgende Endpunkte und Mechanismen für Spaces:

| Feature | Anforderung | Kritisch für EW |
|---|---|---|
| Space-Sidebar anzeigen | `m.room.create.content.type == "m.space"` in Sync | **Ja** |
| Space-Hierarchie laden | `GET /_matrix/client/v1/rooms/{roomId}/hierarchy` | **Ja** |
| Kind-Raum hinzufügen | `PUT /state/m.space.child/{childId}` | **Ja** |
| Space erstellen | `POST /createRoom` mit `creation_content.type: "m.space"` | **Ja** |
| Restricted Rooms | `join_rule: "restricted"` in `m.room.join_rules` | **Ja** |
| Room Version >= 8 in Capabilities | `GET /capabilities` mit `"8": "stable"` | **Ja** |
| Space-Level Push Rules | `m.push_rules` mit Space-Scope | Nein (Phase 3+) |
| Space-Member-List | `GET /rooms/{spaceId}/members` (bestehend) | **Ja** |
| Space-Room-Settings | `GET/PUT /rooms/{spaceId}/state/{type}` (bestehend) | **Ja** |

---

## 11. FluffyChat — Kompatibilitätsanforderungen

FluffyChat (Flutter) unterstützt Spaces ab Version 1.8+. Anforderungen:

| Feature | Anforderung |
|---|---|
| Space-Erkennung | `m.room.create.content.type == "m.space"` |
| Hierarchie-Navigation | `GET /_matrix/client/v1/rooms/{roomId}/hierarchy` |
| Join aus Space | `join_rule: "restricted"` + Session-Check |
| Space-Avatar | `m.room.avatar` State Event im Space-Room |

---

## 12. Admin UI — Erweiterungen (Phase 3)

### 12.1 Space-Management-Seite

Neue Admin-UI-Seite: `/admin/spaces`

**Funktionen:**
- Liste aller Spaces auf dem Server (Room-List gefiltert nach `type == "m.space"`)
- Space-Detail: Name, Mitgliederzahl, Hierarchie-Übersicht (flach)
- Kind-Räume verwalten: hinzufügen, entfernen, `suggested`-Flag setzen
- Space archivieren (= alle User kicken, Space-Room sperren)

### 12.2 Admin API-Erweiterung

```
GET  /api/admin/v1/spaces               — Liste aller Spaces
GET  /api/admin/v1/spaces/{spaceId}     — Space-Details + Children
POST /api/admin/v1/spaces/{spaceId}/children  — Kind hinzufügen
DELETE /api/admin/v1/spaces/{spaceId}/children/{childId}  — Kind entfernen
```

Diese Endpunkte setzen serverseitig `m.space.child` State Events im Namen eines Admin-Users.

---

## 13. Roadmap-Abgrenzung

### Phase 3 MVP (dieser Epic)

| Feature | Enthalten |
|---|---|
| Space erstellen (`type: "m.space"`) | ✅ |
| `m.space.child` setzen/entfernen | ✅ |
| `m.space.parent` setzen/entfernen + Sicherheitscheck | ✅ |
| `GET /hierarchy` mit BFS + Paginierung | ✅ |
| Restricted Join Rules (MSC3083) | ✅ |
| Capabilities Update (Room Version 8–10) | ✅ |
| Sync: Space-Type in `m.room.create` | ✅ |
| Element Web Kompatibilität | ✅ |
| Admin UI Space-Management | ✅ |
| PostgreSQL Index für BFS | ✅ |

### Phase 3+ (spätere Epics)

| Feature | Begründung für Verschiebung |
|---|---|
| Space-Level Push Rules (MSC3664 vollständig) | Erfordert Erweiterung des Push-Rule-Engines |
| Space Knocking (join_rule: knock_restricted) | Kleiner Mehrwert, hohe Komplexität |
| Matrix Federation für Spaces | Federation ist generelles Phase-3-Thema |
| Space-spezifische Notification-Silence | UI-Feature, kein Server-Feature |

---

## 14. Offene Fragen / ADR-Kandidaten

| # | Frage | Empfehlung |
|---|---|---|
| F1 | **BFS-Tiefenlimit:** Was ist das server-seitige Maximum für `max_depth`? | Default 10, konfigurierbar via `NEBU_SPACE_MAX_DEPTH` |
| F2 | **Paginierungstoken-Format:** Opaker Token (Base64-encoded State) oder Cursor (last_room_id + depth)? | Opaker Token (BFS-State serialisiert) — einfacher für Resume |
| F3 | **`m.space.parent`-Validierung:** Hard-Reject oder Soft-Accept mit Warning-Log? | Hard-Reject (`M_FORBIDDEN`) — verhindert Spam |
| F4 | **Capabilities Default Room Version:** `"6"` (aktuell) oder `"10"` (empfohlen für Spaces)? | `"10"` in Phase 3 — Restricted Rooms benötigen v8+ |
| F5 | **Rate Limiting für `/hierarchy`:** Separates, strengeres Limit wegen BFS-Kosten? | Ja — empfohlen: 10 req/min pro User (vs. 300/min Standard) |
| F6 | **Space-Power-Level-Defaults:** Automatisch setzen oder nur wenn kein Override? | Automatisch setzen wenn `type: "m.space"` und kein Override |

---

## 15. Story-Vorschläge für Epic 12 (Phase 3: Spaces MVP)

| Story | Titel | Größe | Security Review |
|---|---|---|---|
| 12-1 | `createRoom`-Erweiterung: `creation_content.type` durchreichen | S | not-needed |
| 12-2 | PostgreSQL-Index für Space-BFS (`idx_events_space_child`, `_parent`) | S | not-needed |
| 12-3 | `m.space.child` State Event Whitelist + Core-Validierung | M | optional |
| 12-4 | `m.space.parent` State Event Whitelist + Sicherheitscheck | M | **required** |
| 12-5 | Restricted Join Rules (MSC3083) — Core Enforcement | L | **required** |
| 12-6 | `GET /hierarchy` — gRPC Handler + BFS-Traversal in Core | L | optional |
| 12-7 | `GET /hierarchy` — Go Gateway Handler + Paginierung | M | optional |
| 12-8 | Capabilities Update (Room Versions 6–10, Default 10) | S | not-needed |
| 12-9 | Element Web E2E: Space erstellen, Raum hinzufügen, beitreten | M | not-needed |
| 12-10 | Admin UI: Space-Liste + Space-Detail + Kinder verwalten | L | optional |
| 12-11 | Admin API: `/api/admin/v1/spaces` CRUD | M | **required** |

**Empfohlene Reihenfolge:** 12-1 → 12-2 → 12-3 → 12-4 → 12-5 → 12-6 → 12-7 → 12-8 → 12-9 → 12-10 → 12-11

---

_Quellen:_  
_Matrix Spec v1.12 — https://spec.matrix.org/v1.12/client-server-api/#spaces_  
_MSC1772 — https://github.com/matrix-org/matrix-spec-proposals/pull/1772_  
_MSC2946 — https://github.com/matrix-org/matrix-spec-proposals/pull/2946_  
_MSC3083 — https://github.com/matrix-org/matrix-spec-proposals/pull/3083_  
_Nebu Matrix API Scope — docs/matrix-api-scope.md_  
_Nebu Architecture — docs/architecture/_
