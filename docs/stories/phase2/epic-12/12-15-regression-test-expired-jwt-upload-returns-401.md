---
id: "12-15"
epic: 12
title: "Regression Test: Expired JWT Upload Returns 401"
status: done
security_review: not-needed
ui: false
matrix: false
created: 2026-05-13
---

# Story 12.15 — Regression Test: Expired JWT Upload Returns 401

## User Story

As a developer,
I want a dedicated test that verifies an expired JWT is rejected on media upload with 401 M_UNKNOWN_TOKEN,
so that future refactorings of the OIDC verification path cannot accidentally introduce a regression where expired tokens are accepted.

## Background

Traceability Matrix GAP-01 (P0): Story 12.7 AC2-2 states "Upload with expired token → 401". `go-oidc/v3` checks `exp` automatically, but there is no test that proves this. A refactor swapping verifier libraries or adding a nil-check shortcut could silently break this invariant.

This is a **pure test-addition story** — no production code changes are expected. The implementation is already correct (Story 12.7); the gap is test coverage.

## Acceptance Criteria

**[AC-1] Expired JWT returns 401 M_UNKNOWN_TOKEN**
Given a valid JWT that has `exp` set 60 seconds in the past,
When `POST /_matrix/media/v3/upload` is called with that token as Bearer,
Then the response is `401` with body `{"errcode":"M_UNKNOWN_TOKEN","error":"..."}` and the file is NOT written to storage.

**[AC-2] Valid JWT is still accepted (non-regression guard)**
Given a valid JWT that has `exp` set 60 seconds in the future,
When `POST /_matrix/media/v3/upload` is called with that token,
Then the response is `200` (valid token still accepted — proves the error mapping does not over-reject).

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. `TestUpload_ExpiredJWT_Returns401` — Go unit test (`media/internal/upload/upload_test.go`)
   - Given: a `mockTokenVerifier` configured with `forcedErr = fmt.Errorf("token expired: %w", oidc.ErrTokenExpired)`
   - When: `POST /_matrix/media/v3/upload` with `Authorization: Bearer expired-token`
   - Then: response is 401, `errcode` is `"M_UNKNOWN_TOKEN"`, `fakeStorer.putCalled` is empty (no storage write)

2. `TestUpload_ValidJWT_Returns200` — Go unit test (`media/internal/upload/upload_test.go`)
   - Given: a `mockTokenVerifier` configured to succeed (returns `"alice"` subject, no error)
   - When: `POST /_matrix/media/v3/upload` with `Authorization: Bearer valid-token`
   - Then: response is 200, `content_uri` starts with `mxc://test.local/`

## Tasks / Subtasks

- [ ] Add `TestUpload_ExpiredJWT_Returns401` to `media/internal/upload/upload_test.go` (AC-1)
  - [ ] Use `mockTokenVerifier{forcedErr: fmt.Errorf("token expired: %w", oidc.ErrTokenExpired)}` 
  - [ ] Assert HTTP 401, errcode `M_UNKNOWN_TOKEN`
  - [ ] Assert `fakeStorer.putCalled` is empty (no storage write)
- [ ] Add `TestUpload_ValidJWT_Returns200` to `media/internal/upload/upload_test.go` (AC-2)
  - [ ] Use `mockTokenVerifier{subject: "alice"}` (success case)
  - [ ] Assert HTTP 200, `content_uri` starts with `mxc://test.local/`
- [ ] Run `make test-unit-go` to confirm both tests GREEN

## Dev Notes

### Test File Location

All new tests go into `media/internal/upload/upload_test.go` — the existing test file for the upload handler.

**No new test file is needed.** The existing test file already has:
- `mockTokenVerifier` struct with `forcedErr error` and `subject string` fields (lines 54–70)
- `mockMediaStore` struct with `inserted []MediaFileRow`
- `fakeStorer` struct with `putCalled []string`
- `buildHandlerWithStorer` helper
- All required imports: `bytes`, `context`, `encoding/json`, `errors`, `net/http`, `net/http/httptest`, `strings`, `testing`, `github.com/nebu/nebu/media/internal/storage`

### Key Implementation Detail

The `mockTokenVerifier.VerifyToken` method (lines 62–69 of `upload_test.go`) already supports the `forcedErr` field:

```go
func (m *mockTokenVerifier) VerifyToken(_ context.Context, _ string) (string, error) {
    if m.forcedErr != nil {
        return "", m.forcedErr
    }
    // ...
}
```

For `TestUpload_ExpiredJWT_Returns401`: set `forcedErr` to any non-nil error — the handler's `VerifyToken` error path (upload.go lines 237–242) returns 401 M_UNKNOWN_TOKEN on ANY error from `VerifyToken`. The test does NOT need to simulate a real JWT; the mock's `forcedErr` is sufficient.

Using `fmt.Errorf("token expired: %w", oidc.ErrTokenExpired)` (wrapping the canonical sentinel) makes the test intent explicit and documents the exact error type being simulated.

### Import Required

The new test will need `"github.com/coreos/go-oidc/v3/oidc"` for `oidc.ErrTokenExpired`. Add to the import block in `upload_test.go` if not already present.

**Check first:** `upload_test.go` currently imports `storage` but NOT `oidc`. The import needs to be added.

Alternatively, a plain `errors.New("token is expired")` error works just as well because the handler does not inspect the error type — it returns 401 for any non-nil error. Using `oidc.ErrTokenExpired` is preferred for readability but not strictly required.

### Handler Behavior (production code — DO NOT CHANGE)

From `upload.go` lines 234–242:
```go
if h.oidcVerifier != nil {
    subject, err := h.oidcVerifier.VerifyToken(r.Context(), rawToken)
    if err != nil {
        slog.Error("media upload: JWT verification failed", "err", err)
        writeError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid or expired access token")
        return
    }
    // ...
}
```

This is already correct. The test just proves this path is exercised.

### Test Pattern to Follow

`TestUpload_NilVerifier_Returns503` (lines 879–922 of `upload_test.go`) is the closest structural analog — it builds a handler with a specific verifier configuration and asserts a specific error response + no storage/DB side effects.

For `TestUpload_ExpiredJWT_Returns401`, use `buildHandlerWithStorer` (not `buildHandler`) so the `fakeStorer.putCalled` field can be checked:
```go
db := &mockMediaStore{}
storer := &fakeStorer{}
expiredVerifier := &mockTokenVerifier{forcedErr: fmt.Errorf("token expired: %w", oidc.ErrTokenExpired)}

h := NewHandler(HandlerConfig{
    DB:           db,
    Storage:      storer,
    ServerName:   testServerName,
    MaxBytes:     defaultMaxBytes,
    OIDCVerifier: expiredVerifier,
})
```

### Security Note

No production code changes = no new attack surface. `security_review: not-needed` is correct.

### Project Structure Notes

- Test file: `media/internal/upload/upload_test.go` (package `upload`)
- No new files needed
- No production code changes

### References

- Story 12.7 AC2-2: expired token → 401 requirement
- `media/internal/upload/upload.go` lines 234–242: VerifyToken error → 401 M_UNKNOWN_TOKEN
- `media/internal/upload/upload_test.go` lines 54–70: `mockTokenVerifier` definition
- Traceability Matrix GAP-01 (P0)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List
