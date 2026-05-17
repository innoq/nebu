# Code Review Findings — Story 14.2b — Cycle 0

## Verdict: 3 MINOR — cycling back to dev-story

## Acceptance Auditor: PASS
All 4 ACs covered by implementation and tests.

## MINOR-1: FetchUsers docstring misleading — validation errors ARE propagated
**Location:** `oidc_directory.go:199-212` (FetchUsers docstring)
**Issue:** Docstring says "no error is propagated to caller (AC2)" but the CR-1 non-HTTPS validation path returns `nil, err` to the caller. The behavior is correct (AC3 requires returning an error for non-HTTPS), but the docstring creates a false contract.
**Fix:** Update docstring to distinguish: "non-HTTPS validation errors ARE returned; runtime fetch errors (unreachable, non-200) are swallowed."

## MINOR-2: FetchUsers accepts `sessionID` parameter but never uses it
**Location:** `oidc_directory.go:208` — `func (s *OIDCDirectoryService) FetchUsers(sessionID string)`
**Issue:** `sessionID` is accepted but not used inside the function — rate limiting is a separate `Allow()` call. This creates a misleading API contract where callers might assume rate limiting happens automatically in FetchUsers. Either: (a) remove the `sessionID` parameter from FetchUsers (make rate limiting explicitly the caller's responsibility), or (b) call `s.Allow(sessionID)` inside FetchUsers and return an error if rate-limited.
**Fix:** Option (a) — remove the unused `sessionID` parameter from `FetchUsers`. Update tests to match.

## MINOR-3: context.Background() instead of caller context in doFetch
**Location:** `oidc_directory.go:228` — `s.sfGroup.Do(s.cacheKey, func() (interface{}, error) { return s.doFetch(context.Background()) })`
**Issue:** The request context from the admin HTTP handler is not passed to doFetch. If the admin cancels the request, the outbound OIDC directory call continues for up to 10 seconds. Singleflight doesn't help here since the caller context is lost.
**Fix:** Pass context through: `FetchUsers(ctx context.Context, sessionID string)` → propagate to doFetch. Update test signatures. Note: singleflight.Do doesn't accept context, so the internal fetch will still run to completion even if one caller's context is cancelled — this is acceptable (the result populates the cache for other callers).

## INFO: maxDirectoryResponseBytes comparison type
Comparing `int64(len(body)) == maxDirectoryResponseBytes` — works correctly on all 64-bit platforms. Informational only.
