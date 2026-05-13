---
status: ready-for-dev
epic: 12
story: 8
security_review: required
matrix: false
ui: false
---

# Story 12.8: OIDC Fail-Open Hardening on Media Gateway

Status: ready-for-dev

## Story

As a system operator,
I want the media gateway to fail-closed when OIDC configuration is missing or Dex is unreachable at startup,
so that the JWT-validation shortcut from Story 12.7 cannot accidentally slip into production with fail-open behavior.

**Size:** S

---

## Context

Story 12.7 introduced OIDC JWT verification for the media gateway upload endpoint via `go-oidc/v3`. However, the implementation in `media/cmd/media/main.go` has a LOW-security finding from Kassandra's review (Story 12-7 security report, advisory):

> "OIDC fail-open on startup: same pattern as gateway — if `oidc.NewProvider` fails, uploadVerifier is `nil`, and the `else { uploaderUserID = rawToken }` fallback in the upload handler becomes reachable in production."

The current code has two fail-open paths:
1. `NEBU_OIDC_ISSUER` is empty → `uploadVerifier` stays `nil`, slog.Warn logged, MVP fallback active
2. `NEBU_OIDC_ISSUER` is set but Dex unreachable → `oidc.NewProvider` fails, slog.Warn logged, `uploadVerifier` stays `nil`, MVP fallback active

In both cases the upload handler's `else { uploaderUserID = rawToken }` branch accepts any Bearer string as an uploader identity — a genuine security risk if deployed without OIDC.

This story closes those paths:
- Empty `NEBU_OIDC_ISSUER` → `os.Exit(1)` with `FATAL:` log
- `NEBU_OIDC_ISSUER` set but Dex unreachable → retry 5 times with 2s backoff, then `os.Exit(1)`
- nil-verifier fallback in upload handler → 503 M_UNAVAILABLE (no more `uploaderUserID = rawToken`)

---

## Acceptance Criteria

### AC-1: Missing NEBU_OIDC_ISSUER causes fatal exit

**Given** `NEBU_OIDC_ISSUER` env var is empty or unset,
**When** the media gateway starts,
**Then** it exits with a non-zero exit code and logs `FATAL: NEBU_OIDC_ISSUER is required`.

### AC-2: Unreachable Dex causes retry then fatal exit

**Given** `NEBU_OIDC_ISSUER` is set but Dex is unreachable (connection refused),
**When** the media gateway starts,
**Then** it retries OIDC discovery up to 5 times with 2s backoff, then exits with a non-zero exit code and logs the connection error.

### AC-3: Successful OIDC init allows normal startup

**Given** `NEBU_OIDC_ISSUER` is set and Dex is reachable,
**When** the media gateway starts,
**Then** the OIDC verifier is initialised successfully and the service starts normally.

### AC-4: nil-verifier upload request returns 503

**Given** the OIDC verifier is nil (startup failed or not yet wired),
**When** an upload request arrives,
**Then** it is rejected with 503 `M_UNAVAILABLE` (no fail-open to any-bearer acceptance).

---

## Tasks / Subtasks

- [ ] T1 — Fail-fast on missing `NEBU_OIDC_ISSUER` in `main()` (AC-1)
  - [ ] Add early check in `main()`: if `os.Getenv("NEBU_OIDC_ISSUER") == ""` → `slog.Error("FATAL: NEBU_OIDC_ISSUER is required")` + `os.Exit(1)`
  - [ ] Remove the `slog.Warn("media: NEBU_OIDC_ISSUER not set")` + nil-verifier MVP path

- [ ] T2 — Retry loop for OIDC provider discovery (AC-2)
  - [ ] Wrap `oidc.NewProvider` in a retry loop: 5 attempts, 2s sleep between attempts
  - [ ] On all retries exhausted: `slog.Error("FATAL: OIDC provider unreachable", ...)` + `os.Exit(1)`
  - [ ] Remove the `slog.Warn("media: OIDC provider unavailable")` + nil-verifier MVP path

- [ ] T3 — 503 M_UNAVAILABLE for nil-verifier in upload handler (AC-4)
  - [ ] In `upload.Handler.ServeHTTP()`: replace the `else { uploaderUserID = rawToken }` branch with `writeError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "OIDC verifier not available")` + `return`
  - [ ] Update `HandlerConfig` doc comment to reflect that `nil` is now an error condition, not a fallback

- [ ] T4 — Write ATDD tests before any implementation (tests first)
  - [ ] `media/cmd/media/main_test.go`: add tests for AC-1, AC-2, AC-3 via `initOIDCVerifier` helper
  - [ ] `media/internal/upload/upload_test.go`: add test for AC-4 (nil verifier → 503)

- [ ] T5 — Update sprint-status.yaml (pipeline step)

---

## Dev Notes

### Architecture of the Current Fail-Open Path

**File:** `media/cmd/media/main.go`

Current code in `main()` (lines ~160-181):
```go
var uploadVerifier upload.TokenVerifier
if oidcIssuer := getenv("NEBU_OIDC_ISSUER", ""); oidcIssuer != "" {
    oidcClientID := getenv("NEBU_OIDC_CLIENT_ID", "nebu")
    oidcProvider, err := oidc.NewProvider(ctx, oidcIssuer)
    if err != nil {
        slog.Warn("media: OIDC provider unavailable — upload JWT validation disabled until resolved",
            "issuer", oidcIssuer, "err", err)
        // Fail-open: allow startup without OIDC
    } else {
        uploadVerifier = oidcProvider.Verifier(&oidc.Config{ClientID: oidcClientID})
        slog.Info("media: OIDC JWT validation enabled for uploads", "issuer", oidcIssuer)
    }
} else {
    slog.Warn("media: NEBU_OIDC_ISSUER not set — upload JWT validation disabled (MVP mode)")
}
```

**File:** `media/internal/upload/upload.go`

Current nil-verifier fallback in `ServeHTTP()` (lines ~138-142):
```go
} else {
    // MVP fallback: OIDC verifier not configured — accept any Bearer token.
    uploaderUserID = rawToken
}
```

### Required Changes

**`media/cmd/media/main.go`:**

1. Replace the entire OIDC block with a new `initOIDCVerifier(ctx, issuer, clientID)` function that:
   - Returns a non-nil `upload.TokenVerifier` on success
   - Calls `os.Exit(1)` via `log.Fatal` / `slog.Error + os.Exit` after 5 retries with 2s sleep
   - Is called unconditionally — but first check `oidcIssuer == ""` and fail fast

2. Early check before calling `initOIDCVerifier`:
   ```go
   oidcIssuer := os.Getenv("NEBU_OIDC_ISSUER")
   if oidcIssuer == "" {
       slog.Error("FATAL: NEBU_OIDC_ISSUER is required")
       os.Exit(1)
   }
   ```

3. `initOIDCVerifier` structure:
   ```go
   func initOIDCVerifier(ctx context.Context, issuer, clientID string) upload.TokenVerifier {
       const maxAttempts = 5
       const retryDelay = 2 * time.Second
       var lastErr error
       for attempt := 1; attempt <= maxAttempts; attempt++ {
           provider, err := oidc.NewProvider(ctx, issuer)
           if err == nil {
               slog.Info("media: OIDC verifier initialised", "issuer", issuer)
               return provider.Verifier(&oidc.Config{ClientID: clientID})
           }
           lastErr = err
           slog.Warn("media: OIDC provider unavailable, retrying",
               "attempt", attempt, "max", maxAttempts, "err", err)
           if attempt < maxAttempts {
               time.Sleep(retryDelay)
           }
       }
       slog.Error("FATAL: media: OIDC provider unreachable after retries",
           "issuer", issuer, "err", lastErr)
       os.Exit(1)
       return nil // unreachable
   }
   ```

**`media/internal/upload/upload.go`:**

Replace the `else` MVP fallback branch:
```go
// Before (REMOVE):
} else {
    uploaderUserID = rawToken
}

// After (ADD):
} else {
    writeError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "OIDC verifier not available")
    return
}
```

### Testing Strategy

**`media/cmd/media/main_test.go` — test `initOIDCVerifier` directly:**
- AC-1 is tested via the early `os.Exit` check in `main()` — this requires running a subprocess (exec.Command) to capture exit code. Use `TestMain_OIDC_EmptyIssuer_FatalExit` with `os/exec`.
- AC-2 can be tested by calling `initOIDCVerifier` with a localhost URL where nothing is listening. Need to intercept `os.Exit(1)` — see pattern below.
- AC-3 requires a real OIDC endpoint; skip with `testing.Short()` if no mock available. Alternatively, test the retry logic with a mock that succeeds on attempt N.

**Pattern for testing `os.Exit(1)` in Go:**
```go
// Re-run the test binary as a subprocess with a special env flag.
// See https://pkg.go.dev/os/exec for details.
if os.Getenv("CRASH_EXPECTED") == "1" {
    // call the function that should os.Exit
    return
}
cmd := exec.Command(os.Args[0], "-test.run=TestMyFatal")
cmd.Env = append(os.Environ(), "CRASH_EXPECTED=1")
err := cmd.Run()
if e, ok := err.(*exec.ExitError); ok && !e.Success() {
    return // expected non-zero exit
}
t.Fatalf("expected non-zero exit, got: %v", err)
```

**`media/internal/upload/upload_test.go` — test AC-4:**
- Create a `Handler` with `oidcVerifier: nil` (use `NewHandler` with `OIDCVerifier: nil`).
- POST with a valid `Authorization: Bearer sometoken`.
- Assert: status 503, body contains `M_UNAVAILABLE`.

### Key Constraint: `os.Exit` Is Not Mockable

`initOIDCVerifier` calls `os.Exit(1)`. In unit tests, use the subprocess pattern above. Do NOT suppress the exit or use a global `osExit` variable — keep the production path clean.

For AC-2 retry count, extract the retry logic into a testable helper:
```go
// tryOIDCProvider tries oidc.NewProvider once.
// Returns provider on success, error on failure.
// Tests can inject a mock via this signature.
type oidcProviderFunc func(ctx context.Context, issuer string) (*oidc.Provider, error)
```

Or simply test that the subprocess exits non-zero with any `NEBU_OIDC_ISSUER` pointing to a dead URL, since the retry delay makes full retry testing slow. Use a 0ms delay test variant with an injected `retryDelay` parameter, or mock the time.Sleep.

### Simpler Approach: Extract `initOIDCVerifier` for Testability

To avoid the subprocess-test complexity for AC-2, extract as:

```go
func initOIDCVerifier(ctx context.Context, issuer, clientID string, maxAttempts int, retryDelay time.Duration) upload.TokenVerifier
```

In tests: call with `maxAttempts=2, retryDelay=0` and a dead URL → verify it calls `os.Exit(1)` via subprocess.

For the retry-success path: create a mock `providerFn` that fails N times then succeeds.

Preferred test approach (keep it simple):
1. Test AC-1 in main_test.go via subprocess (`CRASH_EXPECTED` pattern).
2. Test AC-4 directly in upload_test.go (no exit needed).
3. Skip the full retry loop test or use a very short delay.

### Previous Story Context (12.7)

Story 12.7 added the `TokenVerifier` interface and `OIDCTokenVerifier` usage. The interface is in `media/internal/upload/upload.go`. The `OIDCVerifier` field on `HandlerConfig` is already present. No changes to the interface needed.

The Kassandra LOW advisory from story 12.7:
> "The MVP nil-verifier fallback is reachable if NEBU_OIDC_ISSUER is missing at runtime — same fail-open pattern as gateway"

This story resolves that advisory by enforcing fail-closed.

### Files To Modify

- `media/cmd/media/main.go` — replace OIDC init block, add early issuer check, extract `initOIDCVerifier`
- `media/internal/upload/upload.go` — replace nil-verifier else branch with 503 M_UNAVAILABLE
- `media/cmd/media/main_test.go` — add ATDD tests for AC-1, AC-2, AC-3
- `media/internal/upload/upload_test.go` — add AC-4 test (nil verifier → 503)

### Do NOT Touch

- `media/internal/upload/upload.go` — only the `else` branch after the OIDC verify block; do not change the `TokenVerifier` interface or `HandlerConfig` struct shape
- `media/internal/download/handler.go` — not affected
- `media/internal/thumbnail/handler.go` — not affected
- All gRPC/core code — not affected
- All gateway code — not affected

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestMain_OIDC_EmptyIssuer_FatalExit** — `media/cmd/media/main_test.go` (Go subprocess test)
   - Given: `NEBU_OIDC_ISSUER=""` (unset)
   - When: subprocess runs the binary entrypoint (or calls `initOIDCVerifier` helper)
   - Then: process exits with non-zero code

2. **TestInitOIDCVerifier_AllRetriesFail** — `media/cmd/media/main_test.go` (Go subprocess test)
   - Given: `NEBU_OIDC_ISSUER=http://localhost:1` (connection refused), maxAttempts=2, retryDelay=0
   - When: subprocess calls `initOIDCVerifier`
   - Then: process exits with non-zero code

3. **TestUploadHandler_NilVerifier_Returns503** — `media/internal/upload/upload_test.go` (Go httptest)
   - Given: upload Handler created with `OIDCVerifier: nil`
   - When: POST /_matrix/media/v3/upload with `Authorization: Bearer anytoken`
   - Then: response is 503 with `errcode: M_UNAVAILABLE`

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (nebu-pipeline story creation)

### Debug Log References

N/A

### Completion Notes List

- Story created by nebu-pipeline Step 1 on 2026-05-13
- Security context: addresses Kassandra LOW advisory from story 12.7
- Testing: subprocess pattern required for os.Exit coverage; AC-4 is straightforward httptest

### File List

- `docs/stories/phase2/epic-12/12-8-oidc-fail-open-hardening-media-gateway.md` (this file)
