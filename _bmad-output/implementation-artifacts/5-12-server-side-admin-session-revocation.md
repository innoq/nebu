---
security_review: required
---

# Story 5.12: Server-side Admin Session Store + Revocation

Status: ready-for-dev

## Story

As an instance admin,
I want admin sessions to be backed by a server-side store with a revocable SID,
so that `LogoutHandler` invalidates the session on the server and a stolen session cookie cannot be used after logout.

---

## Background / Motivation

Today the admin session is a stateless HMAC-signed cookie (`admin_session`, `admin/auth.go:514–540`) with 8h fixed expiry. `LogoutHandler` only deletes the browser cookie — the signed value remains valid server-side for the full 8h window. A copy of the cookie (XSS, malware, shared machine, proxy log) cannot be revoked.

This is the root cause the user is currently tracking in `bugfix-logout-oidc-dex-session.md` — logout does not invalidate the session server-side.

---

## Acceptance Criteria

1. Migration `20240006_admin_sessions.up.sql` creates table `admin_sessions`:
   - `sid TEXT PRIMARY KEY` — 256-bit random, base64url
   - `user_id TEXT NOT NULL`
   - `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
   - `expires_at TIMESTAMPTZ NOT NULL`
   - `revoked_at TIMESTAMPTZ` (nullable)
   - Index on `expires_at` for cleanup

2. `CallbackHandler` inserts one row into `admin_sessions` on success and stores only the `sid` in the HMAC-signed cookie (not the user_id or roles).

3. `SessionGuard` looks up the `sid` in `admin_sessions`:
   - Row not found → 401
   - `revoked_at IS NOT NULL` → 401
   - `expires_at < NOW()` → 401
   - Otherwise: set `user_id` into request context (read from the DB row, not the cookie)

4. `LogoutHandler` sets `revoked_at = NOW()` for the current `sid` before clearing the browser cookie. Still returns 302 to `/admin/logout-complete` on success.

5. A cleanup goroutine (or `pg_cron`) deletes rows where `expires_at < NOW() - INTERVAL '7 days'` once per hour.

6. Session expiry in `admin_sessions.expires_at` is bound to `min(idToken.Exp, now + 8h)` — never longer than the OIDC token lifetime.

7. Unit tests:
   - `TestLogout_RevokesSessionInDB`
   - `TestSessionGuard_RejectsRevokedSID`
   - `TestSessionGuard_RejectsExpiredSID`
   - `TestCallback_ExpiryCappedByIDTokenExp`

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestLogout_RevokesSessionInDB` — Go httptest + Postgres
   - Given: authenticated admin session with `sid=abc`
   - When: `POST /admin/logout`
   - Then: `SELECT revoked_at FROM admin_sessions WHERE sid='abc'` returns non-null

2. `TestSessionGuard_RejectsRevokedSID` — Go httptest
   - Given: session row with `revoked_at=NOW()`, cookie contains that `sid`
   - When: `GET /admin/dashboard`
   - Then: 302 to `/admin/login` (or 401)

3. Crash/restart test (Elixir parity): after `docker restart gateway`, the cookie with the revoked `sid` is still rejected — revocation survives restart (DB-backed).

---

## Implementation Notes

- `gateway/internal/db/admin_session_store.go` — new file with `Create`, `Get`, `Revoke`, `CleanupExpired`
- `admin/auth.go:CallbackHandler` — generate SID via `crypto/rand` (32 bytes), insert row, store `sid` in cookie
- `admin/middleware.go:SessionGuard` — DB lookup on every request
- Performance: add a small in-process LRU (e.g., `hashicorp/golang-lru/v2`) with 30s TTL to avoid a DB roundtrip on every admin page load. Cache invalidation: `LogoutHandler` purges the entry before revoking in DB.
- Coordinate with `bugfix-logout-oidc-dex-session.md` — this story supersedes that bugfix
