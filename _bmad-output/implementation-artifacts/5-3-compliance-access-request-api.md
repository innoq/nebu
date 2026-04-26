---
security_review: required
---

# Story 5.3: Compliance Access Request API

Status: review

## Story

As a compliance officer,
I want to submit a formal access request for specific room message data within a defined time range,
so that I can initiate the Four-Eyes review process with a documented justification.

**Size:** S

---

## Acceptance Criteria

### AC1 — Auth Gate: `compliance_officer` Role Required

- `POST /api/v1/compliance/access-requests` is protected by JWT middleware (same `jwtMiddleware` instance used for Matrix routes in `main.go`).
- The handler extracts `system_role` from `middleware.ContextKeySystemRole` in the request context.
- If `system_role != "compliance_officer"` → respond `403` with JSON body `{"errcode":"M_FORBIDDEN","error":"Compliance officer role required"}`.
- **Role detection does NOT require a new `server_config` key.** The role is already fully supported by `auth.MapSystemRole` / `auth.ExtractRoleClaim` (see `gateway/internal/auth/roles.go`). The OIDC claim name is read from `cfg.OIDCClaimRole` (the same env var used for all other JWT role checks). No new bootstrap config is needed.

### AC2 — Request Body Validation

Request body (JSON, `Content-Type: application/json` required):

```json
{
  "room_id": "!abc:server.example",
  "time_range_start": "2026-01-01T00:00:00Z",
  "time_range_end": "2026-03-31T23:59:59Z",
  "justification": "Investigating potential policy violation under reference ABC-2026-001"
}
```

Validation rules (applied in order):

1. `requireJSON(w, r)` must pass (415 on wrong Content-Type) — reuse the existing helper from `gateway/internal/matrix/validate.go`.
2. `json.NewDecoder(r.Body)` with `.DisallowUnknownFields()` — unknown fields → `400 M_BAD_JSON`.
3. `room_id` missing or empty → `400 M_BAD_JSON "room_id is required"`.
4. `room_id` must be a valid Matrix room ID — use `matrix.ValidateMatrixRoomID(roomID)` from `gateway/internal/matrix/validate.go`.
5. `time_range_start` missing or empty → `400 M_BAD_JSON "time_range_start is required"`.
6. `time_range_end` missing or empty → `400 M_BAD_JSON "time_range_end is required"`.
7. Both timestamps must parse as RFC 3339 (a strict subset of ISO 8601 accepted by `time.Parse(time.RFC3339, ...)`).
8. `time_range_end` must be strictly after `time_range_start` → `400 M_BAD_JSON "time_range_end must be after time_range_start"`.
9. `justification` missing or empty → `400 M_BAD_JSON "justification is required"`.
10. `len(justification) < 20` → `400 M_BAD_JSON "justification must be at least 20 characters"`.

### AC3 — Room Existence Check

After body validation, query the `rooms` table:

```sql
SELECT 1 FROM rooms WHERE room_id = $1
```

If no row found → `404` with JSON `{"errcode":"M_NOT_FOUND","error":"Room not found"}`.

Use the existing `*sql.DB` handle passed into the handler. The `rooms` table schema is defined in `gateway/migrations/000009_rooms.up.sql` (`room_id TEXT PRIMARY KEY`).

### AC4 — DB Insert into `compliance_requests`

On all validations passing, insert one row:

```sql
INSERT INTO compliance_requests
  (requester_user_id, room_id, time_range_start, time_range_end, justification)
VALUES ($1, $2, $3, $4, $5)
RETURNING id
```

The `id` (UUID), `status` (`'pending'` default), `created_at` (server default) are all DB-generated.

`requester_user_id` is taken from `middleware.ContextKeySub` (the OIDC `sub` claim — the canonical user identifier, **not** the pre-computed Matrix `user_id`).

### AC5 — Audit Log Emission

After successful insert, call:

```go
audit.LogEvent(
    r.Context(),
    coreClient,
    requesterSub,
    "compliance_access_requested",
    "room",
    roomID,
    map[string]any{"justification_length": len(justification)},
    "success",
    "",
)
```

Use `gateway/internal/audit.LogEvent` — imported as `auditpkg "github.com/nebu/nebu/internal/audit"`. Audit failure must never block the response (the function already returns nil on gRPC error).

### AC6 — Success Response

HTTP `201 Created` with:

```json
{"request_id": "<uuid>", "status": "pending"}
```

### AC7 — Migration `000019_compliance_requests.up.sql`

Create file `gateway/migrations/000019_compliance_requests.up.sql`:

```sql
CREATE TABLE compliance_requests (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_user_id TEXT       NOT NULL,
    room_id          TEXT        NOT NULL,
    time_range_start TIMESTAMPTZ NOT NULL,
    time_range_end   TIMESTAMPTZ NOT NULL,
    justification    TEXT        NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'pending',
    approver_user_id TEXT,
    approved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT compliance_requests_status_check
        CHECK (status IN ('pending', 'approved', 'rejected'))
);

-- RLS: application role may insert and select; UPDATE is restricted to approved/rejected
-- transitions (managed by Story 5.4). Direct DELETE is forbidden — data must be retained
-- for audit trail (Story 5.7 handles GDPR deletion).
ALTER TABLE compliance_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance_requests FORCE ROW LEVEL SECURITY;

CREATE POLICY compliance_requests_insert ON compliance_requests FOR INSERT WITH CHECK (true);
CREATE POLICY compliance_requests_select ON compliance_requests FOR SELECT USING (true);
-- UPDATE allowed (needed for approve/reject in Story 5.4 — policy restricts columns, not rows).
CREATE POLICY compliance_requests_update ON compliance_requests FOR UPDATE USING (true);
-- DELETE denied — retention enforced (GDPR deletion deferred to Story 5.7).
CREATE POLICY compliance_requests_no_delete ON compliance_requests FOR DELETE USING (false);
```

Also create `000019_compliance_requests.down.sql`:

```sql
DROP TABLE IF EXISTS compliance_requests;
```

**Note on migration numbering:** `000018_audit_log.up.sql` is the most recent migration (Story 5-1). `000019` is the correct next number. The `epics.md` lists `20240006_compliance_requests.sql` — that is incorrect/legacy. Always use the `000019_*` sequential format matching all other migrations in `gateway/migrations/`.

### AC8 — Unit Tests

File: `gateway/internal/compliance/handler_test.go`
Build tag: `//go:build !integration` (unit) for handler tests; `//go:build integration` for any DB-level tests.

Minimum required test cases (written FIRST before handler implementation):

| Test | Expected result |
|---|---|
| `TestPostAccessRequest_Valid` | 201 + JSON with UUID `request_id` and `"status":"pending"` |
| `TestPostAccessRequest_MissingJustification` | 400 M_BAD_JSON |
| `TestPostAccessRequest_JustificationTooShort` | 400 M_BAD_JSON (< 20 chars) |
| `TestPostAccessRequest_NonComplianceOfficer` | 403 M_FORBIDDEN |
| `TestPostAccessRequest_UnknownRoomID` | 404 M_NOT_FOUND |
| `TestPostAccessRequest_TimeRangeEndBeforeStart` | 400 M_BAD_JSON |
| `TestPostAccessRequest_MissingRoomID` | 400 M_BAD_JSON |
| `TestPostAccessRequest_AuditEmittedOnSuccess` | audit.LogEvent called with correct args |

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestPostAccessRequest_Valid` — Go httptest (unit)
   - Given: valid JWT with `system_role=compliance_officer`, body with valid room_id (seeded), valid timestamps, justification >= 20 chars, mock DB returning UUID
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 201, JSON body contains `"request_id"` (valid UUID format) and `"status":"pending"`

2. `TestPostAccessRequest_NonComplianceOfficer` — Go httptest (unit)
   - Given: valid JWT with `system_role=user` (or `instance_admin`)
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 403, `{"errcode":"M_FORBIDDEN",...}`

3. `TestPostAccessRequest_MissingJustification` — Go httptest (unit)
   - Given: valid JWT, body missing `justification` field
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 400, `{"errcode":"M_BAD_JSON",...}`

4. `TestPostAccessRequest_JustificationTooShort` — Go httptest (unit)
   - Given: valid JWT, body with `justification: "short"` (< 20 chars)
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 400, `{"errcode":"M_BAD_JSON","error":"justification must be at least 20 characters"}`

5. `TestPostAccessRequest_UnknownRoomID` — Go httptest (unit)
   - Given: valid JWT with compliance_officer role, room_id not in mock DB
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 404, `{"errcode":"M_NOT_FOUND","error":"Room not found"}`

6. `TestPostAccessRequest_TimeRangeEndBeforeStart` — Go httptest (unit)
   - Given: `time_range_end` is before `time_range_start`
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 400, `{"errcode":"M_BAD_JSON","error":"time_range_end must be after time_range_start"}`

7. `TestPostAccessRequest_AuditEmittedOnSuccess` — Go httptest (unit)
   - Given: valid request, mock DB and mock gRPC client
   - When: `POST /api/v1/compliance/access-requests` succeeds
   - Then: mock `pb.CoreServiceClient.WriteAuditLog` was called with `action="compliance_access_requested"`, `target_type="room"`, `target_id=room_id`, `outcome="success"`

8. `TestPostAccessRequest_InvalidMatrixRoomID` — Go httptest (unit)
   - Given: valid JWT, body with `room_id: "not-a-matrix-room-id"`
   - When: `POST /api/v1/compliance/access-requests`
   - Then: HTTP 400, `{"errcode":"M_BAD_JSON",...}`

---

## Tasks / Subtasks

- [x] Migration (AC7)
  - [x] Create `gateway/migrations/000019_compliance_requests.up.sql` with schema + RLS
  - [x] Create `gateway/migrations/000019_compliance_requests.down.sql`

- [x] Handler package (AC1–AC6)
  - [x] Write failing tests first in `gateway/internal/compliance/handler_test.go`
  - [x] Create `gateway/internal/compliance/handler.go` — `AccessRequestHandler` struct
  - [x] Implement role check (AC1), body validation (AC2), room existence (AC3), DB insert (AC4), audit (AC5), response (AC6)

- [x] Route registration
  - [x] Register `POST /api/v1/compliance/access-requests` in `gateway/cmd/gateway/main.go`
  - [x] Apply middleware chain: `bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(handler.PostAccessRequest)))`

- [x] Tests
  - [x] All 8 unit tests green before marking story done
  - [x] `make test-unit-go` passes

---

## Dev Notes

### Package Location

Create a new package: `gateway/internal/compliance/`

- `handler.go` — `AccessRequestHandler` struct + `PostAccessRequest(w, r)` method
- `handler_test.go` — unit tests (no build tag needed unless integration)

This mirrors the pattern of `gateway/internal/matrix/` and `gateway/internal/admin/`.

### Role Check Pattern

**Do NOT create a new middleware.** Extract `system_role` inline from context, exactly like existing handlers do:

```go
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
if systemRole != "compliance_officer" {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
    return
}
```

The `compliance_officer` role is already mapped by `auth.MapSystemRole` via `JWTMiddleware`. No `server_config` key for `compliance_officer_group_claim` is needed — the claim name is the same OIDC claim configured via `NEBU_OIDC_CLAIM_ROLE` (`cfg.OIDCClaimRole`), shared with all other role checks.

See `gateway/internal/auth/roles.go:8` — `compliance_officer` is already a first-class role in `MapSystemRole`.

### Error Response Helper

The `writeMatrixError` function is package-private in `gateway/internal/matrix/`. Create a local equivalent in the `compliance` package:

```go
func writeComplianceError(w http.ResponseWriter, status int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]string{"errcode": errcode, "error": message})
}
```

### JSON Decoding Pattern

Use `DisallowUnknownFields()` — match the pattern from `gateway/internal/matrix/rooms.go:74-76`:

```go
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields()
if err := dec.Decode(&req); err != nil {
    writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
    return
}
```

### Room Existence Check

No existing Go function checks room existence in the gateway — implement inline SQL query using the `*sql.DB` handle:

```go
var exists int
err := h.db.QueryRowContext(r.Context(), `SELECT 1 FROM rooms WHERE room_id = $1`, roomID).Scan(&exists)
if errors.Is(err, sql.ErrNoRows) {
    writeComplianceError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
    return
}
if err != nil {
    slog.Error("compliance: room existence check failed", "room_id", roomID, "err", err)
    writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
    return
}
```

### DB Insert + RETURNING

Use `RETURNING id` to get the generated UUID in one round-trip:

```go
var requestID string
err = h.db.QueryRowContext(r.Context(),
    `INSERT INTO compliance_requests
       (requester_user_id, room_id, time_range_start, time_range_end, justification)
     VALUES ($1, $2, $3, $4, $5)
     RETURNING id`,
    requesterSub, roomID, req.TimeRangeStart, req.TimeRangeEnd, req.Justification,
).Scan(&requestID)
```

Note: `time_range_start` and `time_range_end` are stored as the original parsed `time.Time` values, which `database/sql` serializes correctly to `TIMESTAMPTZ`.

### Route Registration in main.go

Add near the Matrix route section (after `bodyLimit64KiB` is already declared):

```go
// Story 5.3 — Compliance Access Request API
complianceDB, err := sql.Open("pgx", cfg.DBURL)
if err != nil { /* handle */ }
defer complianceDB.Close()
accessRequestHandler := &compliance.AccessRequestHandler{
    DB:         complianceDB,
    CoreClient: coreClient.CoreServiceClient(),
}
mux.Handle("POST /api/v1/compliance/access-requests",
    bodyLimit64KiB(jwtMiddleware(http.HandlerFunc(accessRequestHandler.PostAccessRequest))))
```

**Route namespace:** `/api/v1/compliance/*` — this is a separate compliance API namespace, NOT under `/_matrix/client/v3/` (Matrix CS API). It is NOT under `/admin/` (admin web UI). It sits on the same HTTP port (`:8008`) but with its own path prefix.

### Handler Struct

```go
type AccessRequestHandler struct {
    DB         *sql.DB
    CoreClient pb.CoreServiceClient
}
```

### Requester User ID

Use `middleware.ContextKeySub` (the raw OIDC `sub` claim) — NOT `middleware.ContextKeyUserID` (the pre-computed Matrix user ID). The `sub` is the canonical stable identifier for compliance records.

```go
requesterSub, _ := r.Context().Value(middleware.ContextKeySub).(string)
```

### Body Size Limit

Apply `bodyLimit64KiB` (64 KiB) — same as admin POST endpoints. The compliance request body is administrative data, not Matrix message payload, so 64 KiB is the appropriate tier (not 1 MiB).

### requireJSON Reuse

`requireJSON` is in package `matrix` and is unexported. Define an equivalent inline in the compliance handler or add a new exported helper. The simplest approach: define a local `requireJSON` in `gateway/internal/compliance/handler.go` with the same logic (checking `Content-Type: application/json` prefix).

### Timestamp Parsing

```go
start, err := time.Parse(time.RFC3339, req.TimeRangeStart)
if err != nil {
    writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_start is not a valid ISO 8601 timestamp")
    return
}
end, err := time.Parse(time.RFC3339, req.TimeRangeEnd)
if err != nil {
    writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_end is not a valid ISO 8601 timestamp")
    return
}
if !end.After(start) {
    writeComplianceError(w, http.StatusBadRequest, "M_BAD_JSON", "time_range_end must be after time_range_start")
    return
}
```

### Audit Import

```go
import auditpkg "github.com/nebu/nebu/internal/audit"
// ...
auditpkg.LogEvent(r.Context(), h.CoreClient, requesterSub,
    "compliance_access_requested", "room", roomID,
    map[string]any{"justification_length": len(req.Justification)},
    "success", "")
```

### Scope — What Is In This Story

- `POST /api/v1/compliance/access-requests` only
- `compliance_requests` table creation (migration 000019)
- Role check (officer-only gate)
- Full body validation (room_id, timestamps, justification)
- Room existence check against `rooms` table
- DB insert
- Audit log emission
- Unit tests

### Scope — What Is NOT In This Story (deferred to later stories)

- `GET /api/v1/compliance/access-requests` (Story 5.4)
- `POST /api/v1/compliance/access-requests/{id}/approve` (Story 5.4)
- `POST /api/v1/compliance/access-requests/{id}/reject` (Story 5.4)
- Compliance sessions / JWT (Story 5.5)
- Data export (Story 5.6)
- `approved_at` column usage (Story 5.4 will populate it)
- RLS role separation (deferred per FB-51-01 to Story 5.29 — the `nebu` superuser+BYPASSRLS is the sole DB role for now; RLS policies defined in the migration will be enforced once 5.29 adds the restricted role)

### Project Structure Notes

New files to create:
- `gateway/migrations/000019_compliance_requests.up.sql`
- `gateway/migrations/000019_compliance_requests.down.sql`
- `gateway/internal/compliance/handler.go`
- `gateway/internal/compliance/handler_test.go`

Modified files:
- `gateway/cmd/gateway/main.go` — add route registration for `POST /api/v1/compliance/access-requests`

### TEA Learnings from 5-1 / 5-2 (apply here)

- **`//go:build integration` tag is mandatory** for any test that touches a real DB (`*sql.DB` against actual PostgreSQL). Unit tests that use a mock DB or `database/sql` with a mock driver do NOT need this tag.
- The `TestPostAccessRequest_Valid` test should mock the DB (not use a real PostgreSQL) so it runs in `make test-unit-go` without a running database.
- Mock the `pb.CoreServiceClient` interface for `TestPostAccessRequest_AuditEmittedOnSuccess` — do not use a real gRPC connection in unit tests.
- Pattern for DB mock in Go unit tests: implement a minimal interface wrapping `*sql.DB` operations, or use `github.com/DATA-DOG/go-sqlmock` if already a dependency.

### References

- Role mapping: `gateway/internal/auth/roles.go` — `MapSystemRole`, `ExtractRoleClaim`
- JWT middleware + context keys: `gateway/internal/middleware/auth.go`
- Body limit middleware: `gateway/internal/middleware/body_limit.go`
- `requireJSON` pattern: `gateway/internal/matrix/validate.go:60-69`
- `DisallowUnknownFields` pattern: `gateway/internal/matrix/rooms.go:74-77`
- Audit LogEvent signature: `gateway/internal/audit/writer.go:28-65`
- rooms table: `gateway/migrations/000009_rooms.up.sql`
- audit_log RLS pattern: `gateway/migrations/000018_audit_log.up.sql`
- Route registration: `gateway/cmd/gateway/main.go:180-196` (bodyLimit, jwtMiddleware wiring)
- Story 5-2 story file: `_bmad-output/implementation-artifacts/5-2-audit-log-writer-generisch-alle-admin-aktionen-atomare-garantie.md` (AC4 — Go `audit.LogEvent` signature)

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

- fakedb driver in handler_test.go returned `fmt.Errorf("EOF")` instead of `io.EOF` for empty result sets. `database/sql` distinguishes these: `io.EOF` → `ErrNoRows`, any other error → propagated as-is. Fixed by adding `io` import and using `io.EOF` in `fakeRows.Next()`. This is a bug fix in the test scaffolding, not a logic change.

### Completion Notes List

- AC1 (Role gate): Inline `systemRole != "compliance_officer"` check, no new middleware. Role already mapped by `JWTMiddleware` via `auth.MapSystemRole` / OIDC claim.
- AC2 (Validation): `requireJSON` (local copy), `DisallowUnknownFields`, field presence + `matrix.ValidateMatrixRoomID`, RFC 3339 timestamp parsing, `end.After(start)`, `len(justification) >= 20`. All 10 validation rules satisfied in order.
- AC3 (Room existence): Raw SQL `SELECT 1 FROM rooms WHERE room_id = $1` via `*sql.DB`, checks `sql.ErrNoRows` → 404. No repository abstraction — simple and consistent with story guidance.
- AC4 (DB insert): `INSERT INTO compliance_requests (...) VALUES (...) RETURNING id`, scan result as `string`. `time.Time` values passed directly; pgx driver serialises to TIMESTAMPTZ correctly.
- AC5 (Audit): `auditpkg.LogEvent` with 500ms context timeout. Never-raise (returns nil on gRPC error). `requesterSub` from `middleware.ContextKeySub` (canonical OIDC `sub`, not Matrix user_id).
- AC6 (Response): HTTP 201, `{"request_id":"<uuid>","status":"pending"}`.
- AC7 (Migration): `000019_compliance_requests.up.sql` + down. `pgcrypto` already enabled in 000001. `approved_at TIMESTAMPTZ` column added (required by integration test). RLS: INSERT/SELECT/UPDATE open, DELETE USING (false). Index on `(status, created_at DESC)`. `migrations_test.go` updated to expect both files.
- AC8 (Tests): 9 unit tests (the 8 required + the content-type test AC2.1 counted separately), all green. `make test-unit-go` exit 0.
- Route: `POST /api/v1/compliance/access-requests` in `main.go` under compliance API namespace (not Matrix, not admin). `bodyLimit64KiB(jwtMiddleware(...))`. Dedicated `complianceDB` connection opened.
- `matrix.ValidateMatrixRoomID` reused (yes) — compliance room_id uses the same Matrix `!local:server` format.
- RLS UPDATE/DELETE decision: UPDATE left open for Story 5.4 approve/reject; DELETE denied (`USING (false)`) with GDPR deletion deferred to Story 5.7 SECURITY DEFINER approach.

### File List

- `gateway/migrations/000019_compliance_requests.up.sql` (new)
- `gateway/migrations/000019_compliance_requests.down.sql` (new)
- `gateway/migrations/migrations_test.go` (modified — added 000019 entries)
- `gateway/internal/compliance/handler.go` (implemented — was stub returning 501)
- `gateway/internal/compliance/handler_test.go` (bug fix — `io.EOF` in fakeRows.Next)
- `gateway/cmd/gateway/main.go` (modified — compliance import + route registration)

### Change Log

- 2026-04-23: Story 5.3 implemented. Migration 000019 (compliance_requests table + RLS). Handler PostAccessRequest (AC1–AC6). Route POST /api/v1/compliance/access-requests wired in main.go. All 9 unit tests green. make test-unit-go exit 0.
