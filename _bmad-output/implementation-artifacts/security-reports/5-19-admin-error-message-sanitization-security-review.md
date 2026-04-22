# Security Review: Story 5.19 — Admin Error Message Sanitization

**Reviewer:** Kassandra (Security Agent)
**Date:** 2026-04-20
**Scope:** Staged diff for Story 5.19
**Classification:** CLEAN (with 1 MINOR)

---

## Threat Model

| Threat | STRIDE | CWE | Status |
|---|---|---|---|
| Internal error string leakage (DB driver, DNS, TLS) | Information Disclosure | CWE-209 | **MITIGATED** |
| Request ID prediction / enumeration | Information Disclosure | CWE-330 | **NOT APPLICABLE** |
| Request ID as side-channel for sensitive data | Information Disclosure | CWE-200 | **NOT APPLICABLE** |

---

## Files Reviewed

| File | Change Summary | Security Impact |
|---|---|---|
| `gateway/internal/admin/errors.go` | New: `renderErrorWithID()` with crypto/rand request ID | Core defense |
| `gateway/internal/admin/auth.go` | 2 call sites migrated to `renderErrorWithID()` | AC1 fix |
| `gateway/internal/admin/page_data.go` | New: `ErrorPageData` struct with `RequestID` | Data carrier |
| `gateway/internal/admin/templates/errors/500.html` | Conditionally displays Reference ID | AC3 fix |
| `gateway/internal/admin/error_sanitization_test.go` | 2 tests: no-leak + request-ID presence | Validation |

---

## Findings

### 0 CRITICAL / 0 HIGH findings

### 0 MINOR findings (after fix)

1. **MINOR (fixed during review): Stale ATDD phase comments in test file** -- `error_sanitization_test.go` lines 25-26 and 72-73 still reference "Phase 1 (ATDD): this test is written BEFORE renderErrorWithID is implemented. It will fail until the implementation is complete." Implementation is now complete; comments are misleading. **Fixed:** updated to reflect green-phase reality.

---

## AC Verification

### AC1: No `err.Error()` in response bodies

**Story scope:** "Every `http.Error(w, '...: '+err.Error(), ...)` call site in `gateway/internal/admin/`"

Full `grep "err.Error()" gateway/internal/admin/*.go` result:

| File:Line | Expression | Status |
|---|---|---|
| `api_gen.go:158` | `http.Error(w, err.Error(), http.StatusBadRequest)` | **NOT ACTIVE** -- generated code (DO NOT EDIT), `ServerInterface` is not wired in `main.go`. Dead code path. |

All other `http.Error()` calls in `admin/*.go` use static string literals ("internal error", "Forbidden", "bad request", etc.) -- none concatenate `err.Error()`.

**Conclusion:** The two call sites that previously leaked `err.Error()` in `auth.go` (OIDC provider discovery + type assertion) are now routed through `renderErrorWithID()`. No active `err.Error()` leakage remains.

### AC2: Request ID generation (crypto/rand)

`renderErrorWithID()` uses `crypto/rand.Read([10]byte)` -- 80 bits of entropy, base32-encoded to 16 characters. This is cryptographically random, not `math/rand`.

Fallback on `rand.Read` failure: fixed sentinel `"XXXXXXXX00"` (base32-encoded). This is acceptable -- `crypto/rand.Read` failure is catastrophic (OS entropy pool exhausted) and the sentinel still produces a valid, non-empty ID.

### AC3: Request ID visible to user

`500.html` template conditionally renders `Reference ID: <code>{{ .RequestID }}</code>` when `ErrorPageData.RequestID` is non-empty. The `X-Request-ID` header is also set for programmatic correlation.

### AC4: Tests verify no leak

- `TestAdminError_DoesNotLeakErrorString`: stubs provider cache to return `"pq: connection refused"`, asserts body does NOT contain `"connection refused"` or `"pq:"`.
- `TestAdminError_IncludesRequestID`: asserts `X-Request-ID` header is non-empty, >= 8 chars, and appears in response body.

Both tests exercise the real `LoginStartHandler` code path via `httptest`.

---

## Security Assessment: Request ID

| Property | Assessment |
|---|---|
| Entropy source | `crypto/rand` -- CSPRNG |
| Bit length | 80 bits (10 bytes) |
| Encoding | Base32, no padding, 16 characters |
| Sensitive data in ID? | No -- pure random bytes, no user/session/error data encoded |
| Predictability | Not predictable; no sequential component |
| Side-channel risk | None -- ID is opaque random, reveals nothing about the error |

---

## Coverage Gap Analysis

The story correctly targets only call sites that previously concatenated `err.Error()` into the HTTP response. The remaining ~45 `http.Error()` calls in `admin/*.go` use static strings and do not leak internal details. These are out of scope for Story 5.19 but are worth noting:

- `auth.go:785` leaks a mild operational hint: `"internal error: bootstrap draft incomplete -- please restart the wizard"` -- not a security risk (no stack trace/driver info), but slightly more verbose than the "internal error" pattern used elsewhere.
- `bootstrap.go:169` returns a JSON error body with an instructive message about HTTPS -- acceptable for a validation error (not an internal error).

Neither constitutes a finding.

---

## Residual Risk

- `api_gen.go:158` contains `http.Error(w, err.Error(), http.StatusBadRequest)` in generated code. Currently dead (ServerInterface not wired), but if it is wired in the future without a custom `ErrorHandlerFunc`, request-parsing errors would leak to clients. **Recommendation:** when wiring the OpenAPI handler, always pass a custom `ErrorHandlerFunc` that sanitizes errors.

---

## Verdict

**CLEAN** -- No CRITICAL or HIGH findings. 1 MINOR (stale comments) fixed during review. Error sanitization is correctly applied at all previously-leaking call sites. Request ID uses `crypto/rand` with 80 bits of entropy. No sensitive data in the ID. Tests verify both non-leakage and ID presence.
