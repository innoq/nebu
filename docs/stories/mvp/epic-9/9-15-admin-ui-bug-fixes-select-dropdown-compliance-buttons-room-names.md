---
security_review: not-needed
---

# Story 9.15: Admin UI Bug-Fixes — Select-Dropdown-Sichtbarkeit, Compliance-Button-Kontrast, Room-Fallback-Name

Status: review

## Story

**As an** admin operator,
**I want** the Admin UI dropdowns, compliance action buttons and room list entries to display correctly,
**so that** filter selections are visible, approval/rejection buttons are distinguishable on every table row, and rooms without a name show a meaningful fallback instead of a blank entry.

**Size:** S

---

## Background

Three visual bugs were reported by Philipp via `tmp/bugs.md` (2026-05-05) after hands-on testing of the Admin UI on branch `feature/phase-2-epic-9`:

1. **Select dropdowns appear white** — selected value invisible in Obsidian Dark theme.
2. **Compliance Approve/Reject buttons invisible on alternating rows** — zebra-stripe background (#1f2937 / base-200) matches `btn-success` green (#22c55e) on every second row.
3. **Rooms without names render as blank list entries** — Direct Chats have no `name` in the DB; `AdminRoomProto` has only `member_count` (no members array), so a "user1-user2" composite name requires a Proto change that is out of scope for this story.

All three bugs are **pure HTML-template fixes** — no Go code changes, no gRPC proto changes, no DB migrations.

---

## Acceptance Criteria

**AC1 — Select-Dropdown-Wert sichtbar (filter_bar.html + compliance.html):**
Alle `<select>`-Elemente in `filter_bar.html` und `compliance.html` erhalten die Klassen `bg-base-200 text-base-content`. Der ausgewählte Wert wird im Obsidian-Dark-Theme klar lesbar angezeigt. Betrifft:
- Users-Seite: Role-Filter
- Rooms-Seite: Visibility-Filter
- Compliance-Seite: Status-Filter

**AC2 — Compliance Approve/Reject-Buttons auf alternierenden Zeilen sichtbar (compliance.html):**
Die Action-Buttons in der Compliance-Tabelle verwenden `btn btn-xs btn-outline btn-success` (Approve) und `btn btn-xs btn-outline btn-error` (Reject). Die Outline-Variante hat ausreichend Kontrast auf jeder Zeile (base-100 und base-200) der `table-zebra`-Tabelle.

**AC3 — Room-Fallback-Name für Rooms ohne Namen (rooms.html):**
Rooms mit leerem `name`-Feld (`name == ""`) zeigen in der Master-List und im Detail-Panel den Fallback-Text `(Direct Chat · N members)` an (wobei `N` der Wert von `MemberCount` ist). Rooms mit vorhandenem Namen sind davon unberührt.

---

## Tasks / Subtasks

- [x] **T1 — Select-Dropdown-Fix in filter_bar.html (AC1)**
  - [x] In `gateway/internal/admin/templates/components/filter_bar.html`: `select select-bordered select-sm` → `select select-bordered select-sm bg-base-200 text-base-content`

- [x] **T2 — Select-Dropdown-Fix in compliance.html (AC1)**
  - [x] In `gateway/internal/admin/templates/compliance.html` (Zeile 20): `select select-bordered select-sm` → `select select-bordered select-sm bg-base-200 text-base-content`

- [x] **T3 — Compliance-Buttons auf btn-outline umstellen (AC2)**
  - [x] In `gateway/internal/admin/templates/compliance.html` (Zeile 66): `btn btn-xs btn-success` → `btn btn-xs btn-outline btn-success`
  - [x] In `gateway/internal/admin/templates/compliance.html` (Zeile 70): `btn btn-xs btn-error` → `btn btn-xs btn-outline btn-error`

- [x] **T4 — Room-Fallback-Name in rooms.html (AC3)**
  - [x] In `gateway/internal/admin/templates/rooms.html` Master-List (Zeile 29): Ersetze `{{ .Name }}` durch ein Template-Conditional:
    ```html
    {{ if .Name }}{{ .Name }}{{ else }}(Direct Chat · {{ .MemberCount }} members){{ end }}
    ```
  - [x] In `gateway/internal/admin/templates/rooms.html` Detail-Panel `{{ define "detail_title" }}` (Zeile 54): Analog ersetze `{{ .ActiveRoom.Name }}` durch:
    ```html
    {{ if .ActiveRoom.Name }}{{ .ActiveRoom.Name }}{{ else }}(Direct Chat · {{ .ActiveRoom.MemberCount }} members){{ end }}
    ```

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. `TestSelectDropdownHasBackgroundClasses` — Playwright**
- Given: Admin UI running at `http://localhost:8008`, admin logged in
- When: Navigate to `/admin/users`
- Then: The Role-Filter `<select>` element has CSS classes `bg-base-200` AND `text-base-content`

**2. `TestComplianceSelectDropdownHasBackgroundClasses` — Playwright**
- Given: Admin logged in
- When: Navigate to `/admin/compliance`
- Then: The Status-Filter `<select>` element (id: `status-filter`) has CSS classes `bg-base-200` AND `text-base-content`

**3. `TestComplianceApproveButtonIsOutline` — Playwright**
- Given: Admin logged in, at least one pending compliance request exists
- When: Navigate to `/admin/compliance`
- Then: The "Approve" button in the actions column has class `btn-outline` AND class `btn-success`

**4. `TestComplianceRejectButtonIsOutline` — Playwright**
- Given: Admin logged in, at least one pending compliance request exists
- When: Navigate to `/admin/compliance`
- Then: The "Reject" button in the actions column has class `btn-outline` AND class `btn-error`

**5. `TestRoomWithoutNameShowsFallback` — Playwright**
- Given: Admin logged in, at least one room with empty name exists in the list
- When: Navigate to `/admin/rooms`
- Then: The rooms list does NOT contain any blank/empty `<span class="font-medium flex-1 truncate">` elements; rooms with empty names display the pattern `(Direct Chat · N members)`

**6. `TestRoomWithNameDisplaysName` — Playwright**
- Given: Admin logged in, at least one room with a non-empty name exists
- When: Navigate to `/admin/rooms`
- Then: The named room shows its real name in the list (regression check)

---

## Dev Notes

### Files to modify

| File | Änderung |
|------|----------|
| `gateway/internal/admin/templates/components/filter_bar.html` | `<select>`: `bg-base-200 text-base-content` ergänzen |
| `gateway/internal/admin/templates/compliance.html` | `<select>`: `bg-base-200 text-base-content` ergänzen; Approve/Reject-Buttons `btn-outline` ergänzen |
| `gateway/internal/admin/templates/rooms.html` | `{{ .Name }}` → Fallback-Conditional; `{{ .ActiveRoom.Name }}` in `detail_title` → Fallback-Conditional |

### Kein Go-Code, kein Proto, keine Migration

Diese Story ist ausschließlich Template-Arbeit. Es ist explizit **nicht** erlaubt:
- Neue Go-Handler oder Struct-Felder hinzuzufügen
- `AdminRoomProto` / `.proto`-Dateien zu ändern
- Datenbank-Migrationen zu erstellen
- Neue HTTP-Routen hinzuzufügen

### Warum kein "user1-user2"-Name für Direkte Chats (AC3-Einschränkung)

Der vorgeschlagene Composite-Name aus Teilnehmer-Usernames ist aktuell nicht ohne Proto-Änderung umsetzbar: `AdminRoomProto` hat kein `members`-Array, nur `member_count` (int). Eine Proto-Änderung würde den Scope dieser Bug-Fix-Story erheblich erweitern. Der Fallback `(Direct Chat · N members)` ist daher die korrekte MVP-Lösung.

### DaisyUI `select` im Dark-Theme

DaisyUI's `select` erbt im Obsidian-Dark-Theme keine `background-color` aus `base-200` automatisch. Die explizite Klasse `bg-base-200 text-base-content` ist der korrekte Fix — ohne sie erscheint das Select-Element weiß mit unsichtbarem Text.

### DaisyUI `btn-outline` Kontrast-Fix

`btn-outline btn-success` zeigt einen grünen Rahmen mit grünem Text auf transparentem Hintergrund — sichtbar auf base-100 und base-200 gleichermaßen. Das ist der idiomatische DaisyUI-Weg für Buttons, die auf wechselnden Hintergründen lesbar sein müssen.

### Nach Template-Änderungen: Gateway neu bauen

```bash
make build-gateway
docker compose up -d --force-recreate gateway
```

### Relevante Template-Zeilen (Stand 2026-05-05)

**filter_bar.html (Zeile 14):**
```html
<!-- vorher -->
class="select select-bordered select-sm"
<!-- nachher -->
class="select select-bordered select-sm bg-base-200 text-base-content"
```

**compliance.html (Zeile 20, Status-Filter):**
```html
<!-- vorher -->
class="select select-bordered select-sm"
<!-- nachher -->
class="select select-bordered select-sm bg-base-200 text-base-content"
```

**compliance.html (Zeile 66, Approve-Button):**
```html
<!-- vorher -->
class="btn btn-xs btn-success"
<!-- nachher -->
class="btn btn-xs btn-outline btn-success"
```

**compliance.html (Zeile 70, Reject-Button):**
```html
<!-- vorher -->
class="btn btn-xs btn-error"
<!-- nachher -->
class="btn btn-xs btn-outline btn-error"
```

**rooms.html Master-List (Zeile 29, Name-Anzeige):**
```html
<!-- vorher -->
<span class="font-medium flex-1 truncate">{{ .Name }}</span>
<!-- nachher -->
<span class="font-medium flex-1 truncate">{{ if .Name }}{{ .Name }}{{ else }}(Direct Chat &middot; {{ .MemberCount }} members){{ end }}</span>
```

**rooms.html Detail-Title (Zeile 54):**
```html
<!-- vorher -->
{{ define "detail_title" }}
  {{ if .ActiveRoom }}{{ .ActiveRoom.Name }}{{ else }}Room not found{{ end }}
{{ end }}
<!-- nachher -->
{{ define "detail_title" }}
  {{ if .ActiveRoom }}{{ if .ActiveRoom.Name }}{{ .ActiveRoom.Name }}{{ else }}(Direct Chat &middot; {{ .ActiveRoom.MemberCount }} members){{ end }}{{ else }}Room not found{{ end }}
{{ end }}
```

### Bezug zu Story 9-13

Story 9-13 hat verwandte Template-Fixes durchgeführt (btn-error, border-l-4, Badge-Visibility auf selektierter Zeile). Die vorliegenden Bugs stammen aus einer separaten Testphase nach dem Merge von 9-13 und betreffen Selektoren und Kontraste, die in 9-13 nicht adressiert wurden.

### Source

- Bug-Report: `tmp/bugs.md`
- Template-Dateien: `gateway/internal/admin/templates/`
- Story-Vorlage: `_bmad-output/implementation-artifacts/9-13-admin-ui-ux-bug-fixes-visual-polish.md`

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

N/A — pure template changes, no debugging required.

### Completion Notes List

- T1: Added `bg-base-200 text-base-content` to `<select>` in `filter_bar.html` component. Covers Users Role-Filter and Rooms Visibility-Filter (both use this shared component).
- T2: Added `bg-base-200 text-base-content` to `<select id="status-filter">` in `compliance.html`. Covers Compliance Status-Filter.
- T3: Changed Approve button from `btn btn-xs btn-success` → `btn btn-xs btn-outline btn-success` and Reject button from `btn btn-xs btn-error` → `btn btn-xs btn-outline btn-error` in `compliance.html`.
- T4: Replaced `{{ .Name }}` in rooms.html master-list (line 29) with conditional `{{ if .Name }}{{ .Name }}{{ else }}(Direct Chat &middot; {{ .MemberCount }} members){{ end }}`. Also updated `detail_title` block (line 54) to apply the same fallback for `{{ .ActiveRoom.Name }}`. `RoomRowData` embeds `StubRoom` so `.MemberCount` is accessible without any Go changes.
- CSS verification: `bg-base-200` and `text-base-content` were already present in the compiled `gateway/internal/admin/static/admin.css` — no CSS rebuild needed.
- Gateway: `make build-gateway` ran successfully (binary with updated embedded templates). Container restarted via `docker compose up -d --force-recreate gateway` and confirmed healthy.

### File List

- gateway/internal/admin/templates/components/filter_bar.html
- gateway/internal/admin/templates/compliance.html
- gateway/internal/admin/templates/rooms.html
- gateway/internal/admin/stubs.go (added room-006 with empty Name to exercise AC3 fallback in ATDD spec)
- gateway/internal/admin/rooms.go (code-review follow-up: extended fallback to confirm-dialog message and avatar initial — same `(Direct Chat · N members)` / `·` semantic as the template fallback)
- gateway/internal/admin/rooms_page_test.go (code-review follow-up: TestRoomsPagePagination updated for new pagination boundary — 6 stubs > pageSize=5 → HasMore=true)
- e2e/tests/features/admin/admin-ui-bug-fixes-9-15.spec.ts (Playwright ATDD scaffold)

## Change Log

| Date | Change |
|------|--------|
| 2026-05-05 | Implemented Story 9-15: 3 template bug-fixes — Select-Dropdown bg-base-200, Compliance btn-outline, Room Fallback Name |
