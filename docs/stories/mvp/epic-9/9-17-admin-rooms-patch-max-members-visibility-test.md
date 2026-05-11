---
status: done
epic: 9
story: 17
security_review: not-needed
---

# Story 9.17: GAP-9-002 — Admin Rooms PATCH max_members and visibility Coverage

Status: done

## Story

As a **quality engineer**,
I want Go unit tests for `POST /admin/rooms/{roomId}/settings` covering the `max_members` and `visibility` fields,
so that AC-9.3-3 has full test coverage and the PATCH handler's validation logic is regression-protected.

**Size:** XS — pure test addition, zero production code changes.

---

## Background

The epic-9 traceability matrix (filed as **GAP-9-002**) identified that AC-9.3-3 ("Room settings update calls PATCH /api/v1/admin/rooms/{roomId}") is only PARTIALLY covered. The existing tests in `gateway/internal/admin/rooms_detail_test.go` cover the room name sub-endpoint (`POST /admin/rooms/{roomId}/name`) but nothing tests the **settings sub-endpoint** (`POST /admin/rooms/{roomId}/settings`) which handles the `max_members` field with its own validation rules.

**Endpoint under test:** `POST /admin/rooms/{roomId}/settings`
**Handler:** `RoomsHandler.UpdateRoomSettingsHandler` in `gateway/internal/admin/rooms.go`

### Handler behaviour (verified by reading source, line 343–384):

1. Reads `max_members` from form value (string).
2. If `max_members` is empty → `maxMembers = 0` (no limit), proceeds to redirect.
3. If `max_members` is non-empty:
   - `strconv.Atoi` fails → 400 `"max_members must be a non-negative integer (0 = no limit)"`
   - Parsed value `< 0` → 400
   - Parsed value `> 1_000_000` → 400
4. When `core == nil` (unit-test stub path): no-op (no stub mutation), always redirects `302 → /admin/rooms/{roomId}?flash=Settings+updated`.
5. The handler does **not** read a `visibility` form field. Visibility is not in `UpdateRoomSettingsRequest` proto. Sending a `visibility` form field is silently ignored; the handler still redirects 302.

### Note on "visibility" test scope:

Because the handler ignores visibility (it's absent from the proto), "testing visibility" means:
- Confirming that submitting a `visibility` field alongside `max_members` does not break the handler.
- The test is a combined happy-path: POST with both `max_members=50` and `visibility=private` → 302 with `flash=Settings+updated`.

This reflects the real-world form POST an admin would send from the UI detail panel.

---

## Acceptance Criteria

**AC1 — TestUpdateRoomMaxMembers (happy path):**
- `POST /admin/rooms/room-001/settings` with `max_members=50` returns HTTP 302.
- Location header contains `/admin/rooms/room-001`.
- Location header contains `flash=Settings+updated`.

**AC2 — TestUpdateRoomMaxMembersZero (zero = no limit):**
- `POST /admin/rooms/room-001/settings` with `max_members=0` returns HTTP 302.
- Handler treats `0` as "no limit" — no 400 error.

**AC3 — TestUpdateRoomMaxMembersNegative (invalid):**
- `POST /admin/rooms/room-001/settings` with `max_members=-1` returns HTTP 400.
- Response body contains the validation message.

**AC4 — TestUpdateRoomMaxMembersInvalid (non-numeric string):**
- `POST /admin/rooms/room-001/settings` with `max_members=abc` returns HTTP 400.

**AC5 — TestUpdateRoomMaxMembersTooLarge (over 1_000_000):**
- `POST /admin/rooms/room-001/settings` with `max_members=1000001` returns HTTP 400.

**AC6 — TestUpdateRoomMaxMembersAtLimit (boundary: exactly 1_000_000):**
- `POST /admin/rooms/room-001/settings` with `max_members=1000000` returns HTTP 302 (valid boundary).

**AC7 — TestUpdateRoomSettingsWithVisibility (visibility field ignored, not breaking):**
- `POST /admin/rooms/room-001/settings` with `max_members=50` AND `visibility=private` returns HTTP 302.
- Location contains `flash=Settings+updated`.
- Confirms visibility field is silently accepted without error.

**AC8 — TestUpdateRoomSettingsEmptyMaxMembers (empty = no-limit, no-op):**
- `POST /admin/rooms/room-001/settings` with `max_members=` (empty string) returns HTTP 302.
- Confirms empty max_members is not a validation error.

**AC9 — No production code changed:**
- `gateway/internal/admin/rooms.go` is NOT modified.
- No other production files changed.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestUpdateRoomMaxMembers** — Go unit test (`httptest`)
   - Given: `RoomsHandler` with nil core (stub path)
   - When: POST `/admin/rooms/room-001/settings` with body `max_members=50`
   - Then: HTTP 302, Location `/admin/rooms/room-001?flash=Settings+updated`

2. **TestUpdateRoomMaxMembersZero** — Go unit test
   - Given: same setup
   - When: POST with `max_members=0`
   - Then: HTTP 302 (zero is valid, means no limit)

3. **TestUpdateRoomMaxMembersNegative** — Go unit test
   - Given: same setup
   - When: POST with `max_members=-1`
   - Then: HTTP 400

4. **TestUpdateRoomMaxMembersInvalid** — Go unit test
   - Given: same setup
   - When: POST with `max_members=abc`
   - Then: HTTP 400

5. **TestUpdateRoomMaxMembersTooLarge** — Go unit test
   - Given: same setup
   - When: POST with `max_members=1000001`
   - Then: HTTP 400

6. **TestUpdateRoomMaxMembersAtLimit** — Go unit test
   - Given: same setup
   - When: POST with `max_members=1000000`
   - Then: HTTP 302 (boundary: exactly at limit is valid)

7. **TestUpdateRoomSettingsWithVisibility** — Go unit test
   - Given: same setup
   - When: POST with `max_members=50` AND `visibility=private`
   - Then: HTTP 302, flash=Settings+updated (visibility silently ignored)

8. **TestUpdateRoomSettingsEmptyMaxMembers** — Go unit test
   - Given: same setup
   - When: POST with empty `max_members` string
   - Then: HTTP 302 (empty = 0 = no limit)

---

## Tasks / Subtasks

- [ ] **Task 1 — Add 8 unit tests to `gateway/internal/admin/rooms_detail_test.go`** (AC: #1–#9)
  - [ ] Add `TestUpdateRoomMaxMembers` (AC1)
  - [ ] Add `TestUpdateRoomMaxMembersZero` (AC2)
  - [ ] Add `TestUpdateRoomMaxMembersNegative` (AC3)
  - [ ] Add `TestUpdateRoomMaxMembersInvalid` (AC4)
  - [ ] Add `TestUpdateRoomMaxMembersTooLarge` (AC5)
  - [ ] Add `TestUpdateRoomMaxMembersAtLimit` (AC6)
  - [ ] Add `TestUpdateRoomSettingsWithVisibility` (AC7)
  - [ ] Add `TestUpdateRoomSettingsEmptyMaxMembers` (AC8)

- [ ] **Task 2 — Verify no production code changes** (AC9)
  - [ ] Confirm `rooms.go` is unmodified after test run
  - [ ] Run `make test-unit-go` to confirm all tests pass (existing + new)

---

## Dev Notes

### File to modify

| File | Change type |
|------|-------------|
| `gateway/internal/admin/rooms_detail_test.go` | MODIFY — append 8 new test functions |

No other files. No new files. No production code.

### Route registration

The route is:
```
POST /admin/rooms/{roomId}/settings
```
Registered in `gateway/cmd/gateway/main.go` line 336:
```go
mux.Handle("POST /admin/rooms/{roomId}/settings", bodyLimit64KiB(csrf(sessionGuard(http.HandlerFunc(roomsHandler.UpdateRoomSettingsHandler)))))
```

In unit tests, the mux is set up directly without CSRF/session middleware (identical pattern to existing tests in `rooms_detail_test.go`).

### Test scaffolding pattern

Follow the exact pattern used by the existing tests in `rooms_detail_test.go`. Each test:
1. Creates `NewTemplateHandler()` (ignores error for brevity but fatals if err != nil)
2. Creates `NewRoomsHandler(tmpl)` — nil core triggers stub/no-op path
3. Registers `mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)`
4. Builds a `url.Values{}` form body
5. POSTs via `httptest.NewRequest` + `httptest.NewRecorder`
6. Asserts status code and, for redirect cases, Location header

Example scaffold (happy path):

```go
func TestUpdateRoomMaxMembers(t *testing.T) {
    tmpl, err := NewTemplateHandler()
    if err != nil {
        t.Fatalf("NewTemplateHandler: %v", err)
    }
    h := NewRoomsHandler(tmpl)

    mux := http.NewServeMux()
    mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

    form := url.Values{}
    form.Set("max_members", "50")
    body := strings.NewReader(form.Encode())

    w := httptest.NewRecorder()
    r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
    r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    mux.ServeHTTP(w, r)

    if w.Code != http.StatusFound {
        t.Fatalf("want 302 got %d", w.Code)
    }
    location := w.Header().Get("Location")
    if !strings.Contains(location, "/admin/rooms/room-001") {
        t.Errorf("expected Location to contain '/admin/rooms/room-001', got: %s", location)
    }
    if !strings.Contains(location, "flash=Settings+updated") {
        t.Errorf("expected Location to contain 'flash=Settings+updated', got: %s", location)
    }
}
```

Example scaffold (400 path):

```go
func TestUpdateRoomMaxMembersNegative(t *testing.T) {
    tmpl, err := NewTemplateHandler()
    if err != nil {
        t.Fatalf("NewTemplateHandler: %v", err)
    }
    h := NewRoomsHandler(tmpl)

    mux := http.NewServeMux()
    mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

    form := url.Values{}
    form.Set("max_members", "-1")
    body := strings.NewReader(form.Encode())

    w := httptest.NewRecorder()
    r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
    r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    mux.ServeHTTP(w, r)

    if w.Code != http.StatusBadRequest {
        t.Fatalf("want 400 for negative max_members got %d", w.Code)
    }
}
```

### Visibility test scaffold

`TestUpdateRoomSettingsWithVisibility` sends both fields in a single POST:

```go
form := url.Values{}
form.Set("max_members", "50")
form.Set("visibility", "private")
```

The handler ignores `visibility` completely (not in the proto; not read from form). The test just confirms the response is still 302 with `flash=Settings+updated` — no 400, no crash.

### Handler validation boundaries (source truth — rooms.go line 356–362)

```go
if maxMembersStr != "" {
    n, err := strconv.Atoi(maxMembersStr)
    if err != nil || n < 0 || n > 1_000_000 {
        http.Error(w, "max_members must be a non-negative integer (0 = no limit)", http.StatusBadRequest)
        return
    }
    maxMembers = int32(n)
}
```

| Input          | Result |
|----------------|--------|
| `""`           | 302 (no-limit, no validation) |
| `"0"`          | 302 (valid: no limit) |
| `"1"`          | 302 |
| `"1000000"`    | 302 (valid: at boundary) |
| `"1000001"`    | 400 (exceeds limit) |
| `"-1"`         | 400 (negative) |
| `"abc"`        | 400 (non-numeric) |
| `"1.5"`        | 400 (non-integer, Atoi fails) |

### No stub mutation

On the nil-core path, `UpdateRoomSettingsHandler` does **not** mutate `stubRooms`. The comment on line 382 confirms:
```go
// stub fallback (nil client, unit-test path): no-op — max_members has no stub field to mutate
```

Therefore, no `t.Cleanup()` is needed — unlike `TestUpdateRoomName` and `TestArchiveRoom` which do require cleanup because they mutate stub data.

### Imports already present in rooms_detail_test.go

The existing test file imports `"net/http"`, `"net/http/httptest"`, `"net/url"`, `"strings"`, `"testing"`. No new imports are needed.

### AC reference comment format

Follow the existing comment convention for AC attribution:
```go
// TestUpdateRoomMaxMembers verifies that POST /admin/rooms/room-001/settings with max_members=50
// returns HTTP 302 with flash=Settings+updated.
// AC: 9.3-3 (TestUpdateRoomMaxMembers — Story 9.17 GAP-9-002)
```

### Running the tests

```bash
make test-unit-go
```

Or targeted:
```bash
cd gateway && go test ./internal/admin/ -run TestUpdateRoom -v
```

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

---

## Review Findings (2026-05-05)

Reviewer: bmad-code-review (Auto-Mode); Test-Review-Findings vom bmad-testarch-test-review reingenommen.

- [x] [Review][Patch] MINOR-001 — `t.Parallel()` in alle 8 neuen Tests ergänzt [`gateway/internal/admin/rooms_detail_test.go`]
- [x] [Review][Patch] MINOR-002 — Magic Number `1_000_000` durch benannte Konstante `maxMembersLimit` ersetzt; Tests `TestUpdateRoomMaxMembersTooLarge` und `TestUpdateRoomMaxMembersAtLimit` nutzen `strconv.Itoa(maxMembersLimit[+1])` statt String-Literalen [`gateway/internal/admin/rooms_detail_test.go`]
- [x] [Review][Dismiss] INFO-001 — `t.Run` Subtest-Grouping nicht eingeführt (konsistent mit existierendem Stil im File, kein AC-Verstoß)
- [x] [Review][Dismiss] INFO-002 — 400-Response-Body in `TestUpdateRoomMaxMembersNegative` nicht zusätzlich verifiziert (optional, nicht im AC)
- [x] [Review][Dismiss] INFO/EDGE-001 — Float `"1.5"`-Test nicht ergänzt (in Handler-Doku-Tabelle erwähnt, aber nicht in AC1–AC8)

### Verifikation

- `go vet ./internal/admin/` — clean
- `go test ./internal/admin/ -run "TestUpdateRoom(MaxMembers|Settings)" -v` — 8/8 neue Tests pass + Regression `TestUpdateRoomName` pass
- `go test ./internal/admin/` — 406/406 pass, keine Regression
- `git diff --staged --name-only` bestätigt AC9: nur `rooms_detail_test.go` + Story- und ATDD-Doku staged, `gateway/internal/admin/rooms.go` unverändert
