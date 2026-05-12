---
security_review: required
---

# Story 5.19: Admin Error Message Sanitization

Status: ready-for-dev

## Story

As a security-conscious operator,
I want all admin error responses to return generic messages (no Go `err.Error()` strings),
so that DB/DNS/TLS internals are not leaked to unauthenticated clients.

---

## Background / Motivation

Security audit (2026-04-20): `admin/auth.go:240` returns `"Failed to load OIDC configuration: "+err.Error()` and `:247` returns `"OIDC provider discovery failed: "+err.Error()` to the browser. These errors include Go driver messages (`pq: connection refused`, DNS lookup strings, TLS cert subjects) — valuable reconnaissance for an attacker.

---

## Acceptance Criteria

1. Every `http.Error(w, "...: "+err.Error(), ...)` call site in `gateway/internal/admin/` is replaced with two lines:
   - `slog.Error("admin: <context>", "err", err)` — full error into logs
   - `renderError(w, r, http.Status500, "Internal error", "Please contact your administrator.")` — generic message to client

2. The standard error page (existing `errors/500.html`) is used, including a request ID (generate with `crypto/rand`, log alongside the error).

3. Request ID is displayed to the user so they can reference it when contacting support.

4. Unit tests: verify no response body contains `err.Error()` output for stubbed DB-connection-refused errors.

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestAdminError_DoesNotLeakErrorString` — Go httptest, stub `oidc.NewProvider` to return `errors.New("pq: connection refused")`; expect response body to NOT contain `connection refused`

2. `TestAdminError_IncludesRequestID` — response body contains a request ID, logs contain the same ID with the real error

---

## Implementation Notes

- Helper `renderErrorWithID(w, r, status, title, detail)` in `admin/errors.go`
- Request ID via `crypto/rand.Read([16]byte)` → base32, also put on response header `X-Request-ID` for log correlation
- Audit call sites: `grep "err.Error()" gateway/internal/admin/ | grep http.Error`
