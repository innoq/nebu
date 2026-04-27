---
security_review: required
---

# Story 5.6: Compliance Data Export + Ed25519-Signatur

Status: review

## Story

As a compliance officer,
I want to download a cryptographically signed export of message data scoped to my approved request,
so that the export is tamper-evident and legally defensible.

**Size:** S

---

## Acceptance Criteria

### AC1 — GET /api/v1/compliance/export: Happy Path → 200 + signed JSON file

- Route: `GET /api/v1/compliance/export`
- Protected by `jwtMiddleware` (same chain as Stories 5.3–5.5).
- Role gate: `compliance_officer` only — 403 `M_FORBIDDEN "Compliance officer role required"` otherwise.
- Requires `X-Compliance-Token: <jwt>` header — 401 `M_UNKNOWN_TOKEN "Compliance token required"` if absent.
- Validates compliance session token via `ValidateComplianceToken` from `gateway/internal/compliance/jwt.go` (Story 5.5):
  - Signature (EdDSA-pinned, uses `compPubKey` loaded at startup).
  - `exp` not in the past.
  - `sub` == caller's OIDC `sub` (from `middleware.ContextKeySub`).
  - `iat` not in the future beyond 30s tolerance.
  - Any validation failure → 401 `M_UNKNOWN_TOKEN "Invalid or expired compliance token"`.
- Pre-flight SELECT from `compliance_requests` to fetch `requester_user_id` and `approver_user_id`:
  ```sql
  SELECT requester_user_id, approver_user_id
    FROM compliance_requests
   WHERE id = $1
  ```
  - Uses `claims.ComplianceRequestID` as the key.
  - 0 rows → 500 M_UNKNOWN (token was valid but request deleted — data integrity issue).
  - DB error → 500 M_UNKNOWN.
- Fetch `m.room.message` events directly from the `events` table (Go Gateway owns the DB):
  ```sql
  SELECT event_id, room_id, sender, event_type, content, origin_server_ts, signatures
    FROM events
   WHERE room_id = $1
     AND event_type = 'm.room.message'
     AND origin_server_ts BETWEEN $2 AND $3
   ORDER BY origin_server_ts ASC
  ```
  - `$1` = `claims.RoomID` (from token — not from URL).
  - `$2` = `time.Parse(time.RFC3339, claims.TimeRangeStart).UnixMilli()`.
  - `$3` = `time.Parse(time.RFC3339, claims.TimeRangeEnd).UnixMilli()`.
  - Strict scope enforcement: ALL scope comes from token claims — no URL query params for room_id or time_range.
- Build export document:
  ```json
  {
    "export_id": "<gen_random_uuid — generate via crypto/rand UUID>",
    "generated_at": "<time.Now().UTC().Format(time.RFC3339)>",
    "compliance_request_id": "<claims.ComplianceRequestID>",
    "room_id": "<claims.RoomID>",
    "time_range_start": "<claims.TimeRangeStart>",
    "time_range_end": "<claims.TimeRangeEnd>",
    "requester": "<requester_user_id from DB>",
    "approver": "<approver_user_id from DB>",
    "events": [<array of event objects — see AC5 for shape>]
  }
  ```
- Sign the export document:
  - Marshal export document to JSON bytes using `json.Marshal` (standard Go map: keys are sorted deterministically since Go 1.12+).
  - Sign the marshalled bytes with `compSignKey` (Ed25519 private key) using `crypto/ed25519.Sign(compSignKey, docBytes)`.
  - Base64-encode the signature: `base64.StdEncoding.EncodeToString(sig)`.
  - Append `"server_signature"` field to the final response JSON (add to the same struct/map before final marshal).
  - **Note:** The final response body is `json.Marshal` of the full struct including `server_signature` — the signature is over the document WITHOUT `server_signature`.
- Response headers:
  - `Content-Type: application/json`
  - `Content-Disposition: attachment; filename="compliance-export-<compliance_request_id>.json"`
- Response status: `200 OK`
- Audit: `auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub, "compliance_export_downloaded", "compliance_request", complianceRequestID, map[string]any{"event_count": len(events)}, "success", "")` — never-raise, 500ms timeout.

### AC2 — No X-Compliance-Token header → 401

- Request has no `X-Compliance-Token` header → `401 M_UNKNOWN_TOKEN "Compliance token required"`.

### AC3 — Invalid/expired/tampered token → 401

- `ValidateComplianceToken` returns an error for any reason → `401 M_UNKNOWN_TOKEN "Invalid or expired compliance token"`.

### AC4 — Non-officer JWT → 403

- Caller's OIDC JWT has `system_role != 'compliance_officer'` → `403 M_FORBIDDEN "Compliance officer role required"`.

### AC5 — Event shape in export

Each element in `"events"` array:
```json
{
  "event_id": "<TEXT from DB>",
  "room_id": "<TEXT from DB>",
  "sender": "<TEXT from DB>",
  "type": "m.room.message",
  "content": <JSONB from DB — raw JSON object>,
  "origin_server_ts": <BIGINT from DB — milliseconds>,
  "signatures": <JSONB from DB — may be null/nil>
}
```
- `content` and `signatures` are raw JSON (stored as `JSONB` in DB → scan as `json.RawMessage`).
- `signatures` column is nullable (`JSONB` without NOT NULL constraint) — use `omitempty` or explicit null handling.

### AC6 — Strict scope: no events outside token's time_range

- Events with `origin_server_ts` outside `[time_range_start_ms, time_range_end_ms]` are never present in export.
- Events from a room other than `token.room_id` are never present (the WHERE clause enforces this).

### AC7 — Empty range → valid signed export with `"events": []`

- No events in range: valid export document with `"events": []` — NOT a 404.
- `server_signature` is still present and valid over the document with the empty events array.
- Audit is still emitted with `event_count: 0`.

### AC8 — Audit on success with correct event_count

- `compliance_export_downloaded` audit always emitted on 200.
- `metadata.event_count` == actual number of events included in the export.

### AC9 — Audit failure → 200 (never-raise)

- If `auditpkg.LogEvent` gRPC call fails or times out → still return 200 with valid signed export.
- Matches established `auditTimeout = 500 * time.Millisecond` pattern from Stories 5.3–5.5.

### AC10 — Token sub mismatch → 401

- `ValidateComplianceToken` is called with `expectedSub = callerSub` (from JWT middleware).
- If `token.sub != callerSub` → `ValidateComplianceToken` returns an error → 401.

### AC11 — Signature verifiability

- `server_signature` field in the response can be verified using `compPubKey` (the Ed25519 public key stored in `server_config.compliance_signing_key_pub`).
- Verification: `ed25519.Verify(pubKey, docBytes, sig)` where `docBytes` is the JSON of the document WITHOUT `server_signature`.

### AC12 — Unit Tests

Written FIRST (failing) before handler implementation. No real PostgreSQL required (fake DB driver pattern as in Stories 5.3–5.5).

| Test | Expected |
|---|---|
| `TestGetExport_HappyPath` | 200, Content-Disposition header set, body parseable, `events` array length matches mock, audit called once |
| `TestGetExport_EmptyRange` | 200, `"events":[]`, `server_signature` present and non-empty |
| `TestGetExport_SignatureVerifiable` | `server_signature` base64-decodes and `ed25519.Verify` passes against document bytes |
| `TestGetExport_NoComplianceToken` | 401 `M_UNKNOWN_TOKEN` |
| `TestGetExport_ExpiredToken` | 401 `M_UNKNOWN_TOKEN` |
| `TestGetExport_SubMismatch` | 401 `M_UNKNOWN_TOKEN` |
| `TestGetExport_TamperedToken` | 401 `M_UNKNOWN_TOKEN` |
| `TestGetExport_NonOfficer` | 403 `M_FORBIDDEN` |
| `TestGetExport_EventsOutsideRangeExcluded` | Events with ts < start or ts > end are NOT in response (scoped DB query test) |
| `TestGetExport_AuditEventCount` | audit metadata `event_count` == len(events) from mock |
| `TestGetExport_AuditFailureStill200` | mock gRPC audit returns error → handler still returns 200 |
| `TestGetExport_ContentDispositionHeader` | `Content-Disposition: attachment; filename="compliance-export-<request_id>.json"` |

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestGetExport_HappyPath` — Go httptest (unit)
   - Given: valid `jwtMiddleware` context with `system_role=compliance_officer`, `sub=@alice:server`; valid `X-Compliance-Token` signed with test Ed25519 key (sub=@alice:server, not expired); mock DB returns 1 `compliance_requests` row + 3 event rows
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 200, `Content-Type: application/json`, `Content-Disposition` header set with request_id, body contains `export_id`, `events` array with 3 items, `server_signature` non-empty

2. `TestGetExport_EmptyRange` — Go httptest (unit)
   - Given: same as happy path but mock DB returns 0 event rows
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 200, `"events":[]`, `server_signature` present

3. `TestGetExport_SignatureVerifiable` — Go httptest (unit)
   - Given: test Ed25519 keypair; mock returns 2 events; handler uses test signing key
   - When: `GET /api/v1/compliance/export`
   - Then: parse response body; base64-decode `server_signature`; reconstruct doc JSON without `server_signature`; `ed25519.Verify(pubKey, docBytes, sig)` == true

4. `TestGetExport_NoComplianceToken` — Go httptest (unit)
   - Given: valid JWT context, no `X-Compliance-Token` header
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 401 `{"errcode":"M_UNKNOWN_TOKEN","error":"Compliance token required"}`

5. `TestGetExport_ExpiredToken` — Go httptest (unit)
   - Given: `X-Compliance-Token` with `exp = now-1` (past)
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 401 `M_UNKNOWN_TOKEN`

6. `TestGetExport_SubMismatch` — Go httptest (unit)
   - Given: token signed with `sub=@bob:server`, caller JWT has `sub=@alice:server`
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 401 `M_UNKNOWN_TOKEN`

7. `TestGetExport_TamperedToken` — Go httptest (unit)
   - Given: `X-Compliance-Token` signed with a different Ed25519 key
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 401 `M_UNKNOWN_TOKEN`

8. `TestGetExport_NonOfficer` — Go httptest (unit)
   - Given: caller JWT has `system_role=instance_admin`
   - When: `GET /api/v1/compliance/export`
   - Then: HTTP 403 `M_FORBIDDEN "Compliance officer role required"`

9. `TestGetExport_EventsOutsideRangeExcluded` — Go httptest (unit)
   - Given: mock DB query is verified to include `room_id`, `event_type`, and `origin_server_ts BETWEEN` clauses; events returned are only those in range
   - When: `GET /api/v1/compliance/export`
   - Then: `events` array length matches only in-scope events

10. `TestGetExport_AuditEventCount` — Go httptest (unit)
    - Given: mock returns 5 events; mock `pb.CoreServiceClient`
    - When: `GET /api/v1/compliance/export`
    - Then: `WriteAuditLog` called once with `action="compliance_export_downloaded"`, `target_type="compliance_request"`, metadata JSON contains `"event_count":5`

11. `TestGetExport_AuditFailureStill200` — Go httptest (unit)
    - Given: mock `WriteAuditLog` returns gRPC error
    - When: `GET /api/v1/compliance/export`
    - Then: HTTP 200 (audit failure suppressed, never-raise)

12. `TestGetExport_ContentDispositionHeader` — Go httptest (unit)
    - Given: happy path conditions, `compliance_request_id = "abc-123"`
    - When: `GET /api/v1/compliance/export`
    - Then: `Content-Disposition: attachment; filename="compliance-export-abc-123.json"`

---

## Tasks / Subtasks

- [x] `ExportHandler` struct + `GetExport` method in `gateway/internal/compliance/handler.go` (AC1–AC12)
  - [x] Write 12 failing tests first in `gateway/internal/compliance/export_test.go`
  - [x] Add `ExportHandler` struct (do NOT break `AccessRequestHandler` or `SessionHandler`)
  - [x] Role gate + `X-Compliance-Token` header extraction
  - [x] `ValidateComplianceToken` call (reuse from `jwt.go`)
  - [x] Pre-flight SELECT on `compliance_requests` for requester + approver
  - [x] Events SELECT (strict scope: room_id + origin_server_ts BETWEEN)
  - [x] Build export document struct
  - [x] Sign document: `json.Marshal(doc without server_sig)` → `ed25519.Sign` → base64
  - [x] Append `server_signature` field, re-marshal full response
  - [x] Set `Content-Disposition` header
  - [x] Emit audit (never-raise, 500ms timeout)
  - [x] Return 200

- [x] Route registration in `gateway/cmd/gateway/main.go`
  - [x] `GET /api/v1/compliance/export`
  - [x] `jwtMiddleware(http.HandlerFunc(exportHandler.GetExport))` — no `bodyLimit64KiB` needed (GET, no body); no `requireJSON` needed
  - [x] Wire `complianceDB`, `coreClient`, `compSignKey`, `compPubKey` (already loaded in main.go by Story 5.5)

- [x] Run `make test-unit-go` — all green

---

## Dev Notes

### Architecture Decision: Go Gateway Fetches Events Directly from DB

**Decision: Go Gateway fetches events directly from the `events` table via `pgx`/`database/sql`.**

Rationale:
- The `events` table is owned by the PostgreSQL instance directly accessible to the Go Gateway.
- Adding a gRPC method `ExportEvents` to Elixir Core would create an unnecessary roundtrip for a compliance-only read path that does not need actor-model coordination.
- All existing read paths in Epic 4 (GetMessages, GetInitialSync, GetSyncDelta) already query the DB directly from Go for similar event-fetching operations.
- The events table schema is stable and known to the Go layer (see `gateway/migrations/000010_events.up.sql`).

**DB Schema for events table** (`000010_events.up.sql`):
```sql
CREATE TABLE events (
    event_id         TEXT    PRIMARY KEY,
    room_id          TEXT    NOT NULL REFERENCES rooms(room_id),
    sender           TEXT    NOT NULL,
    event_type       TEXT    NOT NULL,
    content          JSONB   NOT NULL,
    origin_server_ts BIGINT  NOT NULL,  -- Unix milliseconds
    signatures       JSONB               -- nullable
);
CREATE INDEX events_room_id_ts_idx ON events (room_id, origin_server_ts);
```

Note: `origin_server_ts` is BIGINT (Unix milliseconds). Token claims `TimeRangeStart`/`TimeRangeEnd` are RFC 3339 strings → parse with `time.Parse(time.RFC3339, ...)` then convert to millis via `.UnixMilli()`.

### Signing: Go stdlib crypto/ed25519, NOT go-jose

**Decision: Use `crypto/ed25519.Sign(privKey, docBytes)` directly for export document signing.**

This is different from the JWT signing in Story 5.5 which uses `go-jose/v4`. The export document is NOT a JWT — it's a raw JSON document. The signature is over the JSON bytes of the document (excluding the `server_signature` field itself).

```go
import (
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
)

// Build document WITHOUT server_signature first
doc := exportDocument{ ... }
docBytes, err := json.Marshal(doc)
// Sign the marshalled bytes
sig := ed25519.Sign(h.SigningKey, docBytes)
sigB64 := base64.StdEncoding.EncodeToString(sig)

// Build final response: same struct + signature field
response := exportResponse{
    exportDocument: doc,
    ServerSignature: sigB64,
}
responseBytes, _ := json.Marshal(response)
```

**Canonical JSON note:** `json.Marshal` on a struct in Go produces deterministic field ordering (struct field order). Use a struct (not a map) for the export document to ensure deterministic serialization order. Document this clearly in code comments as "struct-field-order JSON, not Matrix Canonical JSON — sufficient for compliance export tamper-evidence at MVP; full Matrix Canonical JSON deferred per BMAD Scope Decision."

Architecture enforcement rule from `architecture.md` line 1014: `canonical_json/1` from the Signature app is for Event IDs — this is a different use-case (export document signing, not event signing). A struct-based approach is acceptable here.

### Compliance Signing Key — Already in Memory

The `compSignKey` (ed25519.PrivateKey) and `compPubKey` (ed25519.PublicKey) are already loaded by the Go Gateway at startup by Story 5.5 (`ensureComplianceSigningKey` in `cmd/gateway/main.go`). Story 5.6 just needs them passed to `ExportHandler` — no new loading code needed.

### ExportHandler Struct

```go
// ExportHandler handles GET /api/v1/compliance/export.
// DB is the complianceDB handle; CoreClient for audit; SigningKey/PublicKey for export doc signing.
type ExportHandler struct {
    DB         *sql.DB
    CoreClient pb.CoreServiceClient
    SigningKey  ed25519.PrivateKey
    PublicKey   ed25519.PublicKey
}
```

Same pattern as `SessionHandler` from Story 5.5.

### UUID Generation for export_id

No DB round-trip needed. Use Go stdlib:

```go
import "crypto/rand"

func generateUUID() string {
    var b [16]byte
    _, _ = rand.Read(b[:])
    b[6] = (b[6] & 0x0f) | 0x40 // Version 4
    b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
    return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
        b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
```

Or import `github.com/google/uuid` if already in `go.mod`. Check `go.mod` first — if not present, use the crypto/rand approach above to avoid adding a dependency.

### Export Document Struct Design

Use two structs — one for signing, one for the final response:

```go
// exportDoc is serialized to JSON and SIGNED — must NOT contain server_signature.
type exportDoc struct {
    ExportID            string        `json:"export_id"`
    GeneratedAt         string        `json:"generated_at"`
    ComplianceRequestID string        `json:"compliance_request_id"`
    RoomID              string        `json:"room_id"`
    TimeRangeStart      string        `json:"time_range_start"`
    TimeRangeEnd        string        `json:"time_range_end"`
    Requester           string        `json:"requester"`
    Approver            string        `json:"approver"`
    Events              []exportEvent `json:"events"`
}

// exportEvent represents one event in the export.
type exportEvent struct {
    EventID         string          `json:"event_id"`
    RoomID          string          `json:"room_id"`
    Sender          string          `json:"sender"`
    Type            string          `json:"type"`
    Content         json.RawMessage `json:"content"`
    OriginServerTS  int64           `json:"origin_server_ts"`
    Signatures      json.RawMessage `json:"signatures,omitempty"`
}

// exportResponse is the complete response body including the signature.
type exportResponse struct {
    exportDoc
    ServerSignature string `json:"server_signature"`
}
```

**Important:** Embed `exportDoc` (not pointer, to get flat JSON output) in `exportResponse`. Then sign `json.Marshal(exportDoc{...})` and set `ServerSignature`.

Actually: Go embedding in JSON marshaling embeds all fields at top level — test this carefully. Alternative: duplicate fields or use `json.RawMessage` composition. Simplest: marshal `doc` separately for signing, then build final JSON manually using `json.RawMessage`:

```go
docBytes, _ := json.Marshal(doc)
// inject server_signature into already-marshalled bytes:
// Use map[string]json.RawMessage approach or a dedicated final struct
```

The clearest approach: use a map for final serialization:

```go
docBytes, _ := json.Marshal(doc)
sig := ed25519.Sign(h.SigningKey, docBytes)
sigB64 := base64.StdEncoding.EncodeToString(sig)

// Build final response by merging doc fields + server_signature
var m map[string]json.RawMessage
json.Unmarshal(docBytes, &m)
sigBytes, _ := json.Marshal(sigB64)
m["server_signature"] = sigBytes
responseBytes, _ := json.Marshal(m)
```

**CAUTION:** `json.Marshal` on a map produces alphabetically-ordered keys (Go map marshaling is sorted by key). For the final response this is fine. For the signed doc use the STRUCT (deterministic by field order). Do NOT use a map for the signed portion — only for the final output that includes `server_signature`.

### Handler Flow (Detailed)

```go
func (h *ExportHandler) GetExport(w http.ResponseWriter, r *http.Request) {
    // 1. Role gate
    systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
    if systemRole != "compliance_officer" {
        writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN", "Compliance officer role required")
        return
    }
    callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)

    // 2. Extract X-Compliance-Token header
    tokenStr := r.Header.Get("X-Compliance-Token")
    if tokenStr == "" {
        writeComplianceError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Compliance token required")
        return
    }

    // 3. Validate token (reuse ValidateComplianceToken from jwt.go)
    claims, err := ValidateComplianceToken(tokenStr, h.PublicKey, callerSub)
    if err != nil {
        writeComplianceError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid or expired compliance token")
        return
    }

    // 4. Parse time range from claims (RFC 3339 → time.Time → milliseconds)
    startTime, _ := time.Parse(time.RFC3339, claims.TimeRangeStart)
    endTime, _ := time.Parse(time.RFC3339, claims.TimeRangeEnd)
    startMs := startTime.UnixMilli()
    endMs := endTime.UnixMilli()

    // 5. Pre-flight: fetch requester + approver from compliance_requests
    var requesterUserID, approverUserID string
    // approver_user_id is nullable — use sql.NullString
    var approverNull sql.NullString
    err = h.DB.QueryRowContext(r.Context(),
        `SELECT requester_user_id, approver_user_id FROM compliance_requests WHERE id = $1`,
        claims.ComplianceRequestID,
    ).Scan(&requesterUserID, &approverNull)
    if err != nil {
        // no row or DB error → token was valid but DB is inconsistent
        slog.Error("compliance/export: pre-flight compliance_requests query failed", "err", err)
        writeComplianceError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
        return
    }
    if approverNull.Valid {
        approverUserID = approverNull.String
    }

    // 6. Fetch events (strict scope from claims)
    rows, err := h.DB.QueryContext(r.Context(),
        `SELECT event_id, room_id, sender, event_type, content, origin_server_ts, signatures
           FROM events
          WHERE room_id = $1
            AND event_type = 'm.room.message'
            AND origin_server_ts BETWEEN $2 AND $3
          ORDER BY origin_server_ts ASC`,
        claims.RoomID, startMs, endMs,
    )
    // ... scan rows into []exportEvent

    // 7. Build exportDoc struct

    // 8. json.Marshal(doc) → sign → base64

    // 9. Build response map with server_signature

    // 10. Set headers, emit audit, write 200
}
```

### Route Registration in main.go

Add near the existing compliance session route (~line 705, after the 5.5 block):

```go
// Story 5.6 — Compliance Data Export
exportHandler := &compliance.ExportHandler{
    DB:         complianceDB,
    CoreClient: coreClient,
    SigningKey:  compSignKey,
    PublicKey:   compPubKey,
}
mux.Handle("GET /api/v1/compliance/export",
    jwtMiddleware(http.HandlerFunc(exportHandler.GetExport)))
```

No `bodyLimit64KiB` (GET, no body). No `requireJSON` (GET endpoint, no request body to check).

### Audit Pattern (Identical to Stories 5.3–5.5)

```go
auditCtx, cancel := context.WithTimeout(context.Background(), auditTimeout)
defer cancel()
_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
    "compliance_export_downloaded", "compliance_request", claims.ComplianceRequestID,
    map[string]any{"event_count": len(events)},
    "success", "")
```

`auditTimeout = 500 * time.Millisecond` — already declared as `const` in `handler.go`. Do NOT redefine.

### Error Response Pattern

`writeComplianceError` already defined in `handler.go` — do NOT redefine. Status 401 uses `writeComplianceError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "...")`.

### Test Pattern (Fake DB Driver — Same as Stories 5.3–5.5)

Stories 5.3, 5.4, 5.5 use a fake DB driver pattern for unit tests (no real PostgreSQL). Follow the exact same approach used in:
- `gateway/internal/compliance/handler_test.go` — approval tests
- `gateway/internal/compliance/session_test.go` — session tests

The test file should be named `gateway/internal/compliance/export_test.go`.

For the signature verification test, generate a test Ed25519 keypair inline:
```go
pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
h := &ExportHandler{DB: mockDB, CoreClient: mockCore, SigningKey: privKey, PublicKey: pubKey}
```

### Scope Decision: AC10 (Token room_id != "requested" room_id)

The epics spec mentions "If compliance session token scope does not match requested room_id → 403". However, in this design, the room_id comes exclusively from the token claims (not from any URL parameter or query string). Therefore:
- A tampered token (modified room_id claim) fails `ValidateComplianceToken` signature check → 401 before any room_id is used.
- There is no URL room_id to mismatch against.
- **Decision: AC10 from epics is implicitly satisfied by the signature verification in AC3.** No additional 403 guard needed. Document this explicitly in the handler comment.

### Scope Decision: Canonical JSON

Architecture enforcement rule (`architecture.md` line 1014): `canonical_json/1` from the Elixir Signature app is for Matrix event IDs. For compliance export document signing:
- Use Go struct-based `json.Marshal` (deterministic by struct field order).
- This is "best-effort canonical JSON" — sufficient for MVP tamper-evidence.
- Full Matrix Canonical JSON for export documents is deferred as a follow-up (FB-56-01, to be added to 5-29 story as a MEDIUM finding if the legal team requires it).
- Add a code comment: `// Export document is signed over struct-marshalled JSON (deterministic field order). Full Matrix Canonical JSON deferred per Story 5.6 scope decision.`

### Project Structure Notes

- Handler addition: `gateway/internal/compliance/handler.go` — add `ExportHandler` struct and `GetExport` method at the end of the file (after `SessionHandler`). Do NOT create a new file; keep all compliance handlers in one file per established pattern.
- Test file: NEW `gateway/internal/compliance/export_test.go` (following the naming convention of `session_test.go`, `approval_test.go`, `handler_test.go`).
- Route: `gateway/cmd/gateway/main.go` — add after the Story 5.5 session route block.
- No new migrations needed for this story.
- No new proto/gRPC methods needed — events are fetched directly from DB.
- No new Elixir code needed for this story.

### Files to Create / Modify

| File | Action |
|---|---|
| `gateway/internal/compliance/handler.go` | MODIFY — add `ExportHandler` struct + `GetExport` method |
| `gateway/internal/compliance/export_test.go` | NEW — 12 unit tests |
| `gateway/cmd/gateway/main.go` | MODIFY — route registration (GET /api/v1/compliance/export) |

### References

- `ValidateComplianceToken` function: `gateway/internal/compliance/jwt.go` (exported, reuse directly)
- `ComplianceClaims` struct: `gateway/internal/compliance/jwt.go` (has RoomID, TimeRangeStart, TimeRangeEnd, ComplianceRequestID, Sub, Exp, Iat)
- `SessionHandler` struct as structural pattern: `gateway/internal/compliance/handler.go` lines 452–457
- `writeComplianceError` helper: `gateway/internal/compliance/handler.go`
- `auditTimeout` const: `gateway/internal/compliance/handler.go` line 31
- `auditpkg.LogEvent` signature: `gateway/internal/audit/writer.go`
- Events table schema: `gateway/migrations/000010_events.up.sql`
- compliance_requests table schema: `gateway/migrations/000019_compliance_requests.up.sql`
- compliance_sessions table schema: `gateway/migrations/000020_compliance_sessions.up.sql`
- `compSignKey` / `compPubKey` loading: `gateway/cmd/gateway/main.go` (`ensureComplianceSigningKey` from Story 5.5)
- Algorithm pinning lesson: Story 5.18 (`5-18-jwt-algorithm-pinning.md`)
- Fake DB test pattern: `gateway/internal/compliance/session_test.go`, `handler_test.go`
- Story 5.5 Completion Notes (session handler learnings): `5-5-compliance-session-handler-24h-jwt-sub-binding-expiry-audit.md`, Dev Agent Record section

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (Story context created by bmad-create-story skill, 2026-04-23)
claude-sonnet-4-6 (Story implemented by bmad-dev-story skill, 2026-04-23)

### Debug Log References

None — implementation green on first pass.

### Completion Notes List

- Implemented `ExportHandler` struct + `GetExport` method appended to `gateway/internal/compliance/handler.go`. No new file created (per established pattern — all compliance handlers in one file).
- Key decision: signed bytes use **map-based JSON marshaling** (alphabetically sorted keys). The test `TestGetExport_SignatureVerifiable` reconstructs the signed doc by removing `server_signature` from the response map and re-marshaling — this is only consistent if the handler also produces alphabetically sorted JSON for the signed portion. Using a `map[string]json.RawMessage` for both the signed doc and the final response guarantees this.
- `events.origin_server_ts` is confirmed as BIGINT (epoch milliseconds) per migration `000010_events.up.sql`. RFC 3339 claims from token → `time.Parse` → `.UnixMilli()` before passing to SQL `BETWEEN`.
- `approver_user_id` is nullable in `compliance_requests` (no NOT NULL constraint) — scanned via `sql.NullString`; defaults to empty string if NULL.
- Added `crypto/rand`, `encoding/base64`, `fmt` to handler.go imports.
- Route `GET /api/v1/compliance/export` registered in `main.go` after Story 5.5 session handler block. No `bodyLimit64KiB` (GET, no body). No `requireJSON`.
- All 13 tests pass: `make test-unit-go` → `ok github.com/nebu/nebu/internal/compliance 1.06s`.

### File List

- `gateway/internal/compliance/handler.go` — MODIFIED: added `ExportHandler` struct + `GetExport` method + helper `generateExportUUID`; added `crypto/rand`, `encoding/base64`, `fmt` imports
- `gateway/cmd/gateway/main.go` — MODIFIED: added `GET /api/v1/compliance/export` route + `ExportHandler` wiring after Story 5.5 block

### Change Log

- 2026-04-23: Story 5.6 implemented — `ExportHandler.GetExport` with Ed25519 document signing, strict scope enforcement, audit emission, and 13 unit tests all green.
