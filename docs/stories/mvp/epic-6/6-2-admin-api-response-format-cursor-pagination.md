---
security_review: not-needed
---

# Story 6.2: Admin API Response Format + Cursor-Pagination

Status: review

## Story

As a developer integrating the Admin API,
I want a consistent `{"data": ..., "meta": {...}, "error": null}` envelope and a standardised cursor-based pagination scheme,
so that all list endpoints behave predictably and pagination tokens are safe to use across restarts.

## Acceptance Criteria

1. `gateway/internal/api/response.go` defines Go types (exact field names and JSON tags must match):
   ```go
   type APIResponse[T any] struct {
       Data  T          `json:"data"`
       Meta  *Meta      `json:"meta,omitempty"`
       Error *APIError  `json:"error,omitempty"`
   }
   type Meta struct {
       Total      int    `json:"total,omitempty"`
       NextCursor string `json:"next_cursor,omitempty"`
       PrevCursor string `json:"prev_cursor,omitempty"`
   }
   type APIError struct {
       Code    string `json:"code"`
       Message string `json:"message"`
   }
   ```

2. `gateway/internal/api/pagination.go` implements:
   - `EncodeCursor(afterID, afterCreatedAt string) string` — returns `Base64URLNoPad(json({"after_id":"<uuid>","after_created_at":"<ISO8601>"}))`
   - `DecodeCursor(cursor string) (afterID, afterCreatedAt string, err error)` — inverse; returns sentinel `ErrInvalidCursor` (a package-level `var ErrInvalidCursor = errors.New("invalid cursor")`) on malformed input

3. Invalid cursor → `400 M_BAD_JSON` on all list endpoints (consumed by Stories 6.4, 6.7 — this story only defines the helpers)

4. On error responses: `data` is `null`, `error` is populated; HTTP status reflects the error type

5. Unit tests in `gateway/internal/api/` cover:
   - `EncodeCursor` + `DecodeCursor` round-trip (encode then decode returns original values)
   - Malformed cursor (truncated, invalid base64, invalid JSON) returns `ErrInvalidCursor`
   - Error response JSON has `data: null` (not omitted, explicitly null)

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **EncodeCursor/DecodeCursor round-trip** — Go unit test (`testing`)
   - Given: `afterID = "550e8400-e29b-41d4-a716-446655440000"`, `afterCreatedAt = "2026-01-15T10:30:00Z"`
   - When: `cursor := EncodeCursor(afterID, afterCreatedAt)` then `DecodeCursor(cursor)`
   - Then: returned `afterID` and `afterCreatedAt` equal the originals; no error

2. **Malformed cursor returns ErrInvalidCursor** — Go unit test
   - Given: `cursor = "not-valid-base64!!"`
   - When: `DecodeCursor(cursor)` is called
   - Then: error is (or wraps) `ErrInvalidCursor`; afterID and afterCreatedAt are empty strings

3. **Error response serializes data as null** — Go unit test
   - Given: `APIResponse[any]{Data: nil, Error: &APIError{Code: "M_NOT_FOUND", Message: "not found"}}`
   - When: marshalled to JSON
   - Then: JSON contains `"data":null` and `"error":{"code":"M_NOT_FOUND","message":"not found"}`; `"meta"` key is absent

## Tasks / Subtasks

- [x] Create `gateway/internal/api/response.go` (AC: #1, #4)
  - [x] Define `APIResponse[T any]` generic struct with `json:"data"`, `json:"meta,omitempty"`, `json:"error,omitempty"` tags
  - [x] Define `Meta` struct with `Total int`, `NextCursor string`, `PrevCursor string`
  - [x] Define `APIError` struct with `Code string`, `Message string`
  - [x] Add package-level helper `WriteJSON(w http.ResponseWriter, status int, v any)` using `encoding/json` (reused by all handlers)

- [x] Create `gateway/internal/api/pagination.go` (AC: #2, #3)
  - [x] Define `var ErrInvalidCursor = errors.New("invalid cursor")`
  - [x] Implement `EncodeCursor(afterID, afterCreatedAt string) string`
    - Encode `{"after_id": afterID, "after_created_at": afterCreatedAt}` as JSON
    - Encode JSON bytes as `base64.RawURLEncoding` (no padding — standard for opaque tokens)
  - [x] Implement `DecodeCursor(cursor string) (afterID, afterCreatedAt string, err error)`
    - Decode `base64.RawURLEncoding`; on error → return `"", "", ErrInvalidCursor`
    - Unmarshal JSON into struct; on error → return `"", "", ErrInvalidCursor`
    - Validate non-empty `after_id` and `after_created_at` fields; if missing → return `ErrInvalidCursor`

- [x] Write unit tests `gateway/internal/api/pagination_test.go` (AC: #5)
  - [x] Round-trip test: encode then decode returns original values
  - [x] Malformed base64 → `ErrInvalidCursor`
  - [x] Valid base64 but invalid JSON → `ErrInvalidCursor`
  - [x] Valid JSON but missing fields → `ErrInvalidCursor`
  - [x] Empty string input → `ErrInvalidCursor`

- [x] Write unit tests `gateway/internal/api/response_test.go` (AC: #5)
  - [x] Error response: `data` field serializes as `null` (not omitted) when `Data` is `nil`
  - [x] Success response: `error` key absent when Error is nil (omitempty)
  - [x] Meta included when non-nil; absent when nil (omitempty)

- [x] Run `make test-unit-go` — all tests must pass green (AC: #5)

## Dev Notes

### Context: What Exists vs What This Story Creates

| Item | Current State | This Story's Action |
|---|---|---|
| `gateway/internal/api/response.go` | MISSING | **CREATE** |
| `gateway/internal/api/pagination.go` | MISSING | **CREATE** |
| `gateway/internal/api/api_gen.go` | EXISTS — generated by Story 6.1, do not modify | **READ ONLY** |
| `gateway/internal/api/server.go` | EXISTS — AdminServer stub, do not modify | **READ ONLY** |
| `gateway/internal/api/openapi_handler.go` | EXISTS — serves spec, do not modify | **READ ONLY** |

**This story creates only two new files and their tests.** It does NOT wire these types into endpoints yet — that happens in Stories 6.4 and 6.7.

### Critical: `data: null` vs `data` absent

The AC requires that error responses have `"data": null` (explicitly present, null value), not omitting the key. This conflicts with `omitempty` behaviour for pointer types.

**Solution:** Use `json.RawMessage` for the `Data` field, or keep `T any` but rely on how `nil` interfaces serialize. For the generic `APIResponse[T any]`:
- When `T` is `any` (interface type) and the value is `nil`, Go's `encoding/json` will encode `null` — **but only if `omitempty` is NOT set on `Data`**.
- The AC specifies `json:"data"` (no `omitempty`) for `Data` — this is intentional. Do not add `omitempty` to `Data`.

**Correct JSON tags:**
```go
type APIResponse[T any] struct {
    Data  T          `json:"data"`           // no omitempty — must be null on errors
    Meta  *Meta      `json:"meta,omitempty"` // omitempty — absent when nil
    Error *APIError  `json:"error,omitempty"` // omitempty — absent on success
}
```

For error responses, construct as:
```go
// In a helper function
func ErrorResponse(code, message string) APIResponse[any] {
    return APIResponse[any]{
        Data:  nil,
        Error: &APIError{Code: code, Message: message},
    }
}
```

### Architecture: Admin API Response Contract

From `architecture.md#API-Response-Formate`:
- Admin API always uses: `{ "data": {...}, "meta": {...} }` on success, `{ "error": {...} }` on failure
- `data` field is ALWAYS present (null on errors, never absent) — architectural invariant
- **Anti-pattern (forbidden):** Matrix error format `{"errcode": "M_X", "error": "..."}` on Admin routes
- **Anti-pattern (forbidden):** Offset pagination `?page=2&per_page=50`
- **Required:** Cursor pagination `?cursor=<opaque>&limit=50`

### Cursor Encoding Spec

The cursor is an opaque base64url-encoded JSON blob. This is the internal format (consumers treat it as opaque):
```json
{"after_id": "<uuid>", "after_created_at": "<ISO8601>"}
```

Encoded as `base64.RawURLEncoding` (URL-safe, no `=` padding) per Go stdlib. This format:
- Survives server restarts (no server-side state required)
- Is safe in URL query params without additional encoding
- Is versioned implicitly — future migrations can detect old cursor format by decode failure

**Use `base64.RawURLEncoding`, NOT `base64.StdEncoding`** — standard encoding uses `+` and `/` which are URL-unsafe.

### WriteJSON Helper

Add a package-level helper in `response.go` to standardize JSON responses across all Admin handlers:

```go
// WriteJSON writes v as JSON to w with the given HTTP status code.
// Sets Content-Type: application/json.
func WriteJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(v)
}
```

Stories 6.3+ will use `WriteJSON` — defining it here ensures consistency and prevents each handler from rolling its own JSON encoding.

### Build Constraints

All files in `gateway/internal/api/` use the build constraint `//go:build go1.22` (established in Story 6.1 via the generated `api_gen.go`). New files in this package should include the same constraint for consistency, though it is not strictly required since Go generics (needed for `APIResponse[T any]`) require Go 1.18+.

### Package: `gateway/internal/api`

Module path: `github.com/nebu/nebu/internal/api`. Do NOT create a new package — add all new files to the existing `api` package.

### No openapi.yaml Changes Required

This story creates Go helper types used by handler implementations. It does NOT add new API operations to `gateway/api/openapi.yaml` and does NOT require re-running `make gen-api`.

### Go Generics (Go 1.18+)

`APIResponse[T any]` uses Go generics. The project is on Go 1.26 (per `gateway/go.mod`), so generics are fully supported. No compatibility concerns.

### Testing Pattern (matches Story 6.1 test style)

```go
// gateway/internal/api/pagination_test.go
package api_test

import (
    "errors"
    "testing"

    "github.com/nebu/nebu/internal/api"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
    afterID := "550e8400-e29b-41d4-a716-446655440000"
    afterCreatedAt := "2026-01-15T10:30:00Z"

    cursor := api.EncodeCursor(afterID, afterCreatedAt)

    gotID, gotCreatedAt, err := api.DecodeCursor(cursor)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if gotID != afterID {
        t.Errorf("afterID: got %q, want %q", gotID, afterID)
    }
    if gotCreatedAt != afterCreatedAt {
        t.Errorf("afterCreatedAt: got %q, want %q", gotCreatedAt, afterCreatedAt)
    }
}

func TestDecodeCursor_MalformedBase64(t *testing.T) {
    _, _, err := api.DecodeCursor("not-valid-base64!!")
    if !errors.Is(err, api.ErrInvalidCursor) {
        t.Errorf("expected ErrInvalidCursor, got %v", err)
    }
}
```

### Project Structure Notes

**Files to CREATE:**
- `gateway/internal/api/response.go` — `APIResponse[T]`, `Meta`, `APIError`, `WriteJSON`
- `gateway/internal/api/pagination.go` — `ErrInvalidCursor`, `EncodeCursor`, `DecodeCursor`
- `gateway/internal/api/pagination_test.go` — round-trip, malformed cursor tests
- `gateway/internal/api/response_test.go` — JSON serialization tests

**Files NOT to touch:**
- `gateway/internal/api/api_gen.go` — generated code; never edit manually
- `gateway/internal/api/server.go` — AdminServer stub; Stories 6.3+ will extend this
- `gateway/internal/api/openapi_handler.go` — unchanged
- `gateway/api/openapi.yaml` — no new paths needed
- `Makefile` — no changes needed

### Security Review Rationale

`security_review: not-needed` — this story only creates in-memory Go types and encoding helpers. No new HTTP routes, no DB access, no auth logic, no user input handling at the HTTP layer. Cursor encoding uses standard library `base64` and `encoding/json` with no crypto. No new attack surface is introduced.

### References

- [Source: epics.md#Story-6.2] Full Acceptance Criteria (lines 2662–2694)
- [Source: epics.md#Story-6.4] Cursor usage in user list endpoint — consumer of `DecodeCursor`/`EncodeCursor`
- [Source: epics.md#Story-6.7] Cursor usage in room list endpoint — consumer of `DecodeCursor`/`EncodeCursor`
- [Source: architecture.md#API-Response-Formate] Admin API response envelope contract, pagination anti-patterns
- [Source: architecture.md#Enforced-Implementation-Rules rule 11] Admin API must use `StrictServerInterface` pattern
- [Source: 6-1-openapi-spec-first-setup-codegen-pipeline-strictserverinterface-live-endpoint.md] Package structure, build constraints, module path `github.com/nebu/nebu`
- [Source: gateway/go.mod] Go 1.26, `encoding/json` + `encoding/base64` are stdlib (no new deps needed)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

_No issues encountered._

### Completion Notes List

- Created `gateway/internal/api/response.go` with `APIResponse[T any]`, `Meta`, `APIError` types and `WriteJSON` + `ErrorResponse` helpers. `Data` field intentionally has no `omitempty` tag to ensure `"data":null` is always present on error responses (architectural invariant).
- Created `gateway/internal/api/pagination.go` with `ErrInvalidCursor` sentinel, `EncodeCursor` (base64.RawURLEncoding, URL-safe no-pad), and `DecodeCursor` with full validation: empty string, invalid base64, invalid JSON, and missing required fields all return `ErrInvalidCursor`.
- Activated all 13 acceptance tests (removed `t.Skip` calls) in the pre-existing test files. All 13 new tests + 6 existing openapi tests pass green.
- `make test-unit-go` (full race-detector suite, 18 packages) — all PASS, 0 regressions.

### File List

- `gateway/internal/api/response.go` (CREATED)
- `gateway/internal/api/pagination.go` (CREATED)
- `gateway/internal/api/pagination_test.go` (MODIFIED — removed t.Skip calls)
- `gateway/internal/api/response_test.go` (MODIFIED — removed t.Skip calls)
- `_bmad-output/implementation-artifacts/6-2-admin-api-response-format-cursor-pagination.md` (MODIFIED — tasks checked, status updated)
