---
security_review: required
---

# Story 5.20: Request Body Size Limits + HTTP Server Timeouts

Status: ready-for-dev

## Story

As an operator running Nebu in production,
I want every JSON handler to cap its request body and every listener to have explicit read/write timeouts,
so that a single authenticated client cannot OOM the gateway or exhaust FDs via Slowloris.

---

## Background / Motivation

Security audit (2026-04-20):
- `grep MaxBytesReader` across `gateway/` returns zero matches
- `http.ListenAndServe(":8008", mux)` uses the default `http.Server{}` — no `ReadTimeout`, `WriteTimeout`, `ReadHeaderTimeout`, `IdleTimeout`, or `MaxHeaderBytes`
- `PutSetRoomState` decodes user JSON into `map[string]any` with no cap — a 10GB POST here is trivial OOM

Applies to every POST/PUT handler in `gateway/internal/matrix/` and `gateway/internal/admin/`.

---

## Acceptance Criteria

1. `cmd/gateway/main.go` constructs the public `http.Server` with:
   - `ReadHeaderTimeout: 10 * time.Second`
   - `ReadTimeout: 30 * time.Second`
   - `WriteTimeout: 60 * time.Second` (sync uses long-polling — document why WriteTimeout is 60s and not lower)
   - `IdleTimeout: 120 * time.Second`
   - `MaxHeaderBytes: 16 * 1024`

2. Matrix long-poll endpoint (`/sync`) is served via a dedicated `http.Server` (or the handler explicitly sets `http.ResponseController.SetWriteDeadline`) so its timeout is compatible with up to 60s of polling.

3. A shared middleware `BodyLimitMiddleware(max int64)` wraps `r.Body` with `http.MaxBytesReader(w, r.Body, max)`. It is applied with sensible defaults:
   - `1 MiB` for all Matrix JSON endpoints (send, createRoom, typing, receipt, read_markers, filter, user_directory/search, login, profile)
   - `10 MiB` for media-upload-metadata endpoints (the media gateway handles the actual blob separately — scope confirmed with gateway team)
   - `64 KiB` for admin-bootstrap and admin-POST endpoints

4. Exceeding the limit returns 413 `M_TOO_LARGE` (Matrix spec errcode) with a clear error.

5. `http.MaxBytesReader` is applied BEFORE `json.NewDecoder` reads, so oversized bodies never touch `map[string]any`.

6. Internal gRPC listener and health-check listener also set explicit timeouts.

7. Unit tests (table-driven):
   - `TestBodyLimit_RejectsOversizedJSON` — 2 MiB POST → 413
   - `TestBodyLimit_AcceptsWithinLimit` — 100 KiB POST → 200
   - `TestServerTimeouts_HeaderTimeout` — slow headers → connection closed within ReadHeaderTimeout

---

## Acceptance Tests

### Tests written FIRST (ATDD gate):

1. `TestBodyLimit_CreateRoom_Rejects2MiB` — Go httptest: 2 MiB body to `POST /createRoom` → 413

2. `TestBodyLimit_Send_Rejects2MiB` — 2 MiB body to `PUT /rooms/../send/..` → 413

3. `TestServerTimeouts_SlowLoris` — connect, write partial headers, never complete; assert connection closed within 10s (ReadHeaderTimeout)

4. Regression: `e2e/features/room-flow.feature` still passes (normal-sized events not affected)

---

## Implementation Notes

- `gateway/internal/middleware/body_limit.go` — new file
- Apply via the existing `http.Handler` composition chain in `main.go`
- For `http.MaxBytesReader`: inspect the error via `errors.As(err, &httpMaxBytesError{})` to distinguish "too large" from other read errors
- Document the WriteTimeout choice in a code comment (cross-reference Matrix sync long-poll spec)
