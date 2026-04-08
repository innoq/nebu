# Story 4.18: Profile + Presence API

**Status:** review
**Epic:** 4 — End-Users Can Chat in Rooms Using Any Standard Matrix Client
**Story Key:** 4-18-profile-presence-api
**Created:** 2026-04-03

---

## Story

As an end-user,
I want to view and update my profile and check the presence status of other users,
so that my display name and avatar are correct and I can see who is online.

---

## Acceptance Criteria

1. `GET /_matrix/client/v3/profile/{userId}` — **unauthenticated** (no JWT required); reads from the `profiles` PostgreSQL table directly in the Go Gateway; returns `200 {"displayname": "...", "avatar_url": "mxc://..."}` or `404 M_NOT_FOUND` if user has no profile row.

2. `PUT /_matrix/client/v3/profile/{userId}/displayname` — **authenticated** (JWT required); body `{"displayname": "..."}` (1–128 chars, non-empty); calls `gRPC CoreService.UpdateProfile`; returns `200 {}` or `403 M_FORBIDDEN` if path `userId` ≠ authenticated user; returns `400 M_INVALID_PARAM` if displayname is empty or longer than 128 chars.

3. `PUT /_matrix/client/v3/profile/{userId}/avatar_url` — **authenticated** (JWT required); body `{"avatar_url": "mxc://..."}` (must start with `mxc://`); calls `gRPC CoreService.UpdateProfile`; returns `200 {}` or `403 M_FORBIDDEN` if path `userId` ≠ authenticated user; returns `400 M_INVALID_PARAM` if URL does not start with `mxc://`.

4. `GET /_matrix/client/v3/presence/{userId}/status` — **authenticated** (JWT required); calls `gRPC CoreService.GetPresence`; returns `200 {"presence": "online"|"offline"|"unavailable", "last_active_ago": <ms>}` where `last_active_ago` is `now_ms - last_active_at`; returns `404 M_NOT_FOUND` if user is not found.

5. `gRPC CoreService` proto adds:
   - `rpc GetPresence(GetPresenceRequest) returns (GetPresenceResponse)`
   - `rpc UpdateProfile(UpdateProfileRequest) returns (UpdateProfileResponse)`

6. `SetPresence` stub in `gateway/internal/grpc/client.go` (currently `return nil, nil`) is implemented as a real gRPC call.

7. New PostgreSQL migration `000015_profiles.up.sql` creates the `profiles` table with columns `user_id TEXT PRIMARY KEY REFERENCES users(user_id)`, `displayname TEXT`, `avatar_url TEXT`, `updated_at BIGINT NOT NULL`.

8. New Go handler files: `gateway/internal/matrix/profile.go` and `gateway/internal/matrix/presence.go`.

9. Unit tests: `gateway/internal/matrix/profile_test.go` and `gateway/internal/matrix/presence_test.go`; Elixir ExUnit tests for `get_presence/2` and `update_profile/2` handlers in EventDispatcher.Server.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. GET /profile/{userId} — existing user → 200 — Go httptest**
- Given: `profiles` row exists for `@alice:test.local` with `displayname = "Alice"`, `avatar_url = "mxc://test.local/abc123"`; no Authorization header needed
- When: `GET /_matrix/client/v3/profile/@alice:test.local`
- Then: `200 {"displayname": "Alice", "avatar_url": "mxc://test.local/abc123"}`

**2. GET /profile/{userId} — no profile row → 404 — Go httptest**
- Given: no `profiles` row for `@unknown:test.local`
- When: `GET /_matrix/client/v3/profile/@unknown:test.local`
- Then: `404 {"errcode": "M_NOT_FOUND", "error": "..."}`

**3. PUT /profile/{userId}/displayname — happy path → 200 — Go httptest**
- Given: authenticated user `@alice:test.local`; path userId = `@alice:test.local`; body `{"displayname": "Alice Nebu"}`; mock UpdateProfile returns success
- When: `PUT /_matrix/client/v3/profile/@alice:test.local/displayname`
- Then: `200 {}`; mock received `UpdateProfileRequest{user_id: "@alice:test.local", displayname: "Alice Nebu"}`

**4. PUT /profile/{userId}/displayname — userId mismatch → 403 — Go httptest**
- Given: authenticated user `@alice:test.local`; path userId = `@bob:test.local`
- When: `PUT /_matrix/client/v3/profile/@bob:test.local/displayname`
- Then: `403 {"errcode": "M_FORBIDDEN", ...}`; Core is NOT called

**5. PUT /profile/{userId}/displayname — empty displayname → 400 — Go httptest**
- Given: authenticated user `@alice:test.local`; body `{"displayname": ""}`
- When: `PUT /_matrix/client/v3/profile/@alice:test.local/displayname`
- Then: `400 {"errcode": "M_INVALID_PARAM", ...}`; Core is NOT called

**6. PUT /profile/{userId}/displayname — displayname > 128 chars → 400 — Go httptest**
- Given: authenticated user `@alice:test.local`; body with `displayname` of 129 chars
- When: `PUT /_matrix/client/v3/profile/@alice:test.local/displayname`
- Then: `400 {"errcode": "M_INVALID_PARAM", ...}`; Core is NOT called

**7. PUT /profile/{userId}/avatar_url — valid mxc URI → 200 — Go httptest**
- Given: authenticated user `@alice:test.local`; body `{"avatar_url": "mxc://test.local/img1"}`; mock UpdateProfile returns success
- When: `PUT /_matrix/client/v3/profile/@alice:test.local/avatar_url`
- Then: `200 {}`; mock received `UpdateProfileRequest{user_id: "@alice:test.local", avatar_url: "mxc://test.local/img1"}`

**8. PUT /profile/{userId}/avatar_url — non-mxc URL → 400 — Go httptest**
- Given: authenticated user `@alice:test.local`; body `{"avatar_url": "https://cdn.example.com/img.jpg"}`
- When: `PUT /_matrix/client/v3/profile/@alice:test.local/avatar_url`
- Then: `400 {"errcode": "M_INVALID_PARAM", "error": "avatar_url must be an mxc:// URI"}`; Core is NOT called

**9. GET /presence/{userId}/status — online user → 200 — Go httptest**
- Given: authenticated user; mock GetPresence returns `{presence: "online", last_active_ago: 5000}`
- When: `GET /_matrix/client/v3/presence/@alice:test.local/status`
- Then: `200 {"presence": "online", "last_active_ago": 5000}`

**10. GET /presence/{userId}/status — user not found → 404 — Go httptest**
- Given: authenticated user; mock GetPresence returns `codes.NotFound`
- When: `GET /_matrix/client/v3/presence/@unknown:test.local/status`
- Then: `404 {"errcode": "M_NOT_FOUND", ...}`

**11. GET /presence/{userId}/status — unauthenticated → 401 — Go httptest**
- Given: no Authorization header
- When: `GET /_matrix/client/v3/presence/@alice:test.local/status`
- Then: 401 (JWTMiddleware rejects before handler runs)

**12. Elixir: get_presence → returns status — ExUnit**
- Given: `Nebu.Presence.Manager` ETS has `@alice:test.local` with status `:online`, last_active_at = `T`; current time = `T + 5000ms`
- When: `Nebu.EventDispatcher.Server.get_presence(request, stream)` called with `{user_id: "@alice:test.local"}`
- Then: returns `%Core.GetPresenceResponse{presence: "online", last_active_ago: 5000}`

**13. Elixir: get_presence — offline default — ExUnit**
- Given: `@unknown:test.local` has no ETS entry
- When: `get_presence` called
- Then: returns `%Core.GetPresenceResponse{presence: "offline", last_active_ago: 0}`

**14. Elixir: update_profile — upserts profiles table — ExUnit**
- Given: fake profile DB module injected; `user_id = "@alice:test.local"`, `displayname = "Alice Nebu"`, `avatar_url = nil`
- When: `Nebu.EventDispatcher.Server.update_profile(request, stream)` called
- Then: fake `upsert_profile/3` was called with `("@alice:test.local", "Alice Nebu", nil)`; `UpdateProfileResponse{}` returned

**15. Elixir: update_profile — avatar_url update — ExUnit**
- Given: fake profile DB module; `user_id = "@alice:test.local"`, `displayname = nil`, `avatar_url = "mxc://test.local/abc"`
- When: `update_profile` called with avatar_url only (displayname is empty string)
- Then: fake `upsert_profile/3` called; response returned successfully

---

## Technical Requirements

### New Proto RPCs: GetPresence + UpdateProfile

Add to `proto/core.proto` inside `service CoreService`:

```protobuf
// GetPresence — returns presence status for a user
rpc GetPresence(GetPresenceRequest) returns (GetPresenceResponse);

// UpdateProfile — upserts displayname and/or avatar_url for a user
rpc UpdateProfile(UpdateProfileRequest) returns (UpdateProfileResponse);
```

Add message definitions (AFTER existing messages, before the closing brace of the file):

```protobuf
// GetPresence — reads from Presence.Manager ETS, never raises not_found (offline default)
message GetPresenceRequest {
  string user_id = 1;
}
message GetPresenceResponse {
  string presence       = 1;  // "online" | "offline" | "unavailable"
  int64  last_active_ago = 2;  // milliseconds since last active (0 if never seen)
}

// UpdateProfile — upserts displayname and/or avatar_url in the profiles table
// Fields are optional strings: empty string = do not update that field
message UpdateProfileRequest {
  string user_id      = 1;
  string displayname  = 2;  // empty = not updating displayname
  string avatar_url   = 3;  // empty = not updating avatar_url
}
message UpdateProfileResponse {}
```

**IMPORTANT:** Run `make proto` to regenerate both Go and Elixir stubs after editing `proto/core.proto`. Generated files:
- `gateway/internal/grpc/pb/core.pb.go` and `core_grpc.pb.go`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` and `core_grpc.pb.ex`

### PostgreSQL Migration: profiles table

**File:** `gateway/migrations/000015_profiles.up.sql`
**File:** `gateway/migrations/000015_profiles.down.sql`

Up migration:
```sql
-- profiles: public-facing Matrix user profile (separate from encrypted PII in users table)
CREATE TABLE profiles (
    user_id      TEXT   PRIMARY KEY REFERENCES users(user_id),
    displayname  TEXT,
    avatar_url   TEXT,
    updated_at   BIGINT NOT NULL
);
```

Down migration:
```sql
DROP TABLE IF EXISTS profiles;
```

**Why a separate table (not `users`):** The `users` table stores PII as encrypted BLOBs (`display_name_encrypted`, `display_name_nonce`). The `profiles` table stores the plain-text public-facing Matrix profile (`displayname`, `avatar_url`) — these are explicitly public per Matrix spec. Previous migration: `000014_read_receipts` — next is `000015_profiles`.

### Go Handler: profile.go

**File:** `gateway/internal/matrix/profile.go`
**Package:** `package matrix`

This file handles THREE endpoints sharing a single core interface:

```go
// ProfileCoreClient is the consumer-defined interface for UpdateProfile gRPC calls.
type ProfileCoreClient interface {
    UpdateProfile(ctx context.Context, req *pb.UpdateProfileRequest) (*pb.UpdateProfileResponse, error)
}

// ProfileHandler handles GET/PUT /_matrix/client/v3/profile/{userId}[/displayname|/avatar_url].
type ProfileHandler struct {
    coreClient ProfileCoreClient
    serverName string
    db         ProfileDB  // interface for direct DB reads (GET profile — no gRPC needed)
}

// ProfileDB is the interface for reading profile data directly from PostgreSQL.
// Defined by the consumer (this handler) per Go interface convention (ADR-009).
type ProfileDB interface {
    GetProfile(ctx context.Context, userID string) (*ProfileData, error)
}

type ProfileData struct {
    DisplayName string  // may be empty
    AvatarURL   string  // may be empty
}

type ProfileConfig struct {
    CoreClient ProfileCoreClient
    ServerName string
    DB         ProfileDB
}

func NewProfileHandler(cfg ProfileConfig) *ProfileHandler
```

**GET /profile/{userId} — no auth, direct DB:**
```go
// GetProfile handles GET /_matrix/client/v3/profile/{userId}.
// No JWT required — public endpoint per Matrix spec.
// Reads directly from the profiles PostgreSQL table (no gRPC round-trip).
func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request)
// Flow:
//  1. Extract userId from r.PathValue("userId").
//  2. Call h.db.GetProfile(r.Context(), userId).
//  3. If not found → 404 M_NOT_FOUND.
//  4. Return 200 {"displayname": "...", "avatar_url": "..."} (omit nil/empty fields? — include both always for Matrix client compat).
```

**PUT /profile/{userId}/displayname:**
```go
// PutDisplayname handles PUT /_matrix/client/v3/profile/{userId}/displayname.
// Requires JWT auth. Checks path userId == authenticated user. Validates 1–128 chars.
func (h *ProfileHandler) PutDisplayname(w http.ResponseWriter, r *http.Request)
// Flow:
//  1. Extract userId from path.
//  2. Extract sub from JWT context → build authedUserID.
//  3. If userId != authedUserID → 403 M_FORBIDDEN (before any Core call).
//  4. Decode body: {"displayname": "..."}.
//  5. Validate: len(displayname) in [1,128] → 400 M_INVALID_PARAM if not.
//  6. Call gRPC UpdateProfile with displayname set; avatar_url = "".
//  7. Map gRPC errors: PermissionDenied → 403; default → 500.
//  8. Return 200 {}.
```

**PUT /profile/{userId}/avatar_url:**
```go
// PutAvatarURL handles PUT /_matrix/client/v3/profile/{userId}/avatar_url.
// Requires JWT auth. Checks path userId == authenticated user. Validates mxc:// prefix.
func (h *ProfileHandler) PutAvatarURL(w http.ResponseWriter, r *http.Request)
// Flow:
//  1. Extract userId from path.
//  2. Extract sub from JWT context → build authedUserID.
//  3. If userId != authedUserID → 403 M_FORBIDDEN.
//  4. Decode body: {"avatar_url": "..."}.
//  5. Validate: strings.HasPrefix(avatarURL, "mxc://") → 400 M_INVALID_PARAM if not.
//  6. Call gRPC UpdateProfile with avatar_url set; displayname = "".
//  7. Map gRPC errors: PermissionDenied → 403; default → 500.
//  8. Return 200 {}.
```

**ProfileDB implementation (for production use):**

Create `gateway/internal/db/profile_store.go` (or `gateway/internal/matrix/profile_db.go` — check where `db.NewPostgresTokenStore` lives; follow that pattern):

```go
type PostgresProfileDB struct {
    db *sql.DB
}

func NewPostgresProfileDB(db *sql.DB) *PostgresProfileDB

func (p *PostgresProfileDB) GetProfile(ctx context.Context, userID string) (*ProfileData, error) {
    // SELECT displayname, avatar_url FROM profiles WHERE user_id = $1
    // Return &ProfileData{} on success; nil, errNotFound (custom sentinel) if no row.
}
```

In tests: use a mock `ProfileDB` struct implementing the interface.

### Go Handler: presence.go

**File:** `gateway/internal/matrix/presence.go`
**Package:** `package matrix`

```go
// PresenceCoreClient is the consumer-defined interface for GetPresence.
type PresenceCoreClient interface {
    GetPresence(ctx context.Context, req *pb.GetPresenceRequest) (*pb.GetPresenceResponse, error)
}

// PresenceHandler handles GET /_matrix/client/v3/presence/{userId}/status.
type PresenceHandler struct {
    coreClient PresenceCoreClient
    serverName string
}

type PresenceConfig struct {
    CoreClient PresenceCoreClient
    ServerName string
}

func NewPresenceHandler(cfg PresenceConfig) *PresenceHandler

// GetPresenceStatus handles GET /_matrix/client/v3/presence/{userId}/status.
// Flow:
//  1. Extract userId from r.PathValue("userId").
//  2. Extract sub + systemRole from JWT context.
//  3. Build gRPC context with user metadata.
//  4. Call CoreService.GetPresence.
//  5. Map gRPC errors: NotFound → 404 M_NOT_FOUND; default → 500 M_UNKNOWN.
//  6. Return 200 {"presence": resp.Presence, "last_active_ago": resp.LastActiveAgo}.
func (h *PresenceHandler) GetPresenceStatus(w http.ResponseWriter, r *http.Request)
```

JSON response struct:
```go
type presenceStatusResponse struct {
    Presence      string `json:"presence"`
    LastActiveAgo int64  `json:"last_active_ago"`
}
```

### client.go: Implement SetPresence stub + add GetPresence and UpdateProfile

In `gateway/internal/grpc/client.go`:

1. Replace the existing `SetPresence` stub (currently `return nil, nil`):
```go
// SetPresence calls the Elixir core to set the presence status for a user.
func (c *Client) SetPresence(ctx context.Context, req *pb.SetPresenceRequest) (*pb.SetPresenceResponse, error) {
    return c.core.SetPresence(ctx, req)
}
```

2. Add `GetPresence` (after proto regeneration):
```go
// GetPresence calls the Elixir core to retrieve presence status for a user.
func (c *Client) GetPresence(ctx context.Context, req *pb.GetPresenceRequest) (*pb.GetPresenceResponse, error) {
    return c.core.GetPresence(ctx, req)
}
```

3. Add `UpdateProfile`:
```go
// UpdateProfile calls the Elixir core to upsert a user's displayname and/or avatar_url.
func (c *Client) UpdateProfile(ctx context.Context, req *pb.UpdateProfileRequest) (*pb.UpdateProfileResponse, error) {
    return c.core.UpdateProfile(ctx, req)
}
```

### main.go: Register new routes

Add after the receipts handler registration (before `slog.Info("HTTP server starting")`):

```go
// Profile DB for direct reads (GET /profile — no gRPC)
profileDB, err := sql.Open("pgx", cfg.DBURL)
if err != nil {
    slog.Error("failed to open DB for profile store", "err", err)
    os.Exit(1)
}
defer profileDB.Close()

profileHandler := matrix.NewProfileHandler(matrix.ProfileConfig{
    CoreClient: coreClient,
    ServerName: serverName,
    DB:         db.NewPostgresProfileDB(profileDB),
})
// GET is unauthenticated — no jwtMiddleware wrapper
mux.HandleFunc("GET /_matrix/client/v3/profile/{userId}", profileHandler.GetProfile)
// PUT endpoints require auth
mux.Handle("PUT /_matrix/client/v3/profile/{userId}/displayname",
    jwtMiddleware(http.HandlerFunc(profileHandler.PutDisplayname)))
mux.Handle("PUT /_matrix/client/v3/profile/{userId}/avatar_url",
    jwtMiddleware(http.HandlerFunc(profileHandler.PutAvatarURL)))

presenceHandler := matrix.NewPresenceHandler(matrix.PresenceConfig{
    CoreClient: coreClient,
    ServerName: serverName,
})
mux.Handle("GET /_matrix/client/v3/presence/{userId}/status",
    jwtMiddleware(http.HandlerFunc(presenceHandler.GetPresenceStatus)))
```

**Note on DB connection:** Check if the existing `bootstrapDB` or `tokenDB` connection can be reused (prefer reuse over opening another connection). If main.go already has a `*sql.DB` suitable for profile reads, inject it directly instead of opening a new one.

### Elixir: get_presence — implementation in EventDispatcher.Server

Add `get_presence/2` handler (NO configurable module needed — reads directly from `Nebu.Presence.Manager.get_presence/1`):

```elixir
def get_presence(request, _stream) do
  user_id = request.user_id

  {:ok, %{status: status, last_active_at: last_active_at}} =
    Nebu.Presence.Manager.get_presence(user_id)

  now_ms = System.system_time(:millisecond)

  last_active_ago =
    if is_nil(last_active_at) do
      0
    else
      max(0, now_ms - last_active_at)
    end

  %Core.GetPresenceResponse{
    presence: Atom.to_string(status),
    last_active_ago: last_active_ago
  }
end
```

**Key facts about `Nebu.Presence.Manager.get_presence/1`:**
- NEVER returns `{:error, :not_found}` — always returns `{:ok, %{status: :offline, last_active_at: nil}}` for unknown users (from manager.ex line 62-64)
- Returns `status` as an atom (`:online`, `:offline`, `:unavailable`) — convert with `Atom.to_string/1`
- `last_active_at` is `nil` for users never seen — handle nil case (→ 0 ms ago)

**No 404 path:** Because `get_presence` never errors, this handler always succeeds. The Go handler receives a response with `presence: "offline"` for unknown users — NOT a 404. Update AC4 note: `404 M_NOT_FOUND` only applies if you implement user existence validation. For MVP: always return 200 with status "offline" for unknown users.

> **IMPORTANT MVP DECISION:** Do NOT return 404 for unknown users from `get_presence`. `Nebu.Presence.Manager.get_presence/1` defaults unknown users to offline — this is intentional (Matrix clients handle `{"presence": "offline"}` fine). Remove the `NotFound → 404` gRPC error mapping from `presence.go` Go handler (Core never sends it). This avoids an unnecessary DB user-existence check.

### Elixir: update_profile — implementation in EventDispatcher.Server

Add `update_profile/2` handler with configurable DB module:

```elixir
# ─── Configurable profile DB module for testability ─────────────────────────
defp profile_db_module do
  Application.get_env(:event_dispatcher, :profile_db_module, Nebu.Profile.DB)
end

def update_profile(request, stream) do
  {user_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)

  if is_nil(user_id) or user_id == "" do
    raise GRPC.RPCError,
      status: GRPC.Status.unauthenticated(),
      message: "missing x-user-id metadata"
  end

  # Guard: caller can only update their own profile (Go already enforces this,
  # but defense-in-depth at Core level).
  if request.user_id != user_id do
    raise GRPC.RPCError,
      status: GRPC.Status.permission_denied(),
      message: "cannot update another user's profile"
  end

  displayname = if request.displayname == "", do: nil, else: request.displayname
  avatar_url = if request.avatar_url == "", do: nil, else: request.avatar_url

  case profile_db_module().upsert_profile(user_id, displayname, avatar_url) do
    :ok ->
      %Core.UpdateProfileResponse{}
    {:error, reason} ->
      raise GRPC.RPCError,
        status: GRPC.Status.internal(),
        message: "upsert_profile failed: #{inspect(reason)}"
  end
end
```

### Elixir: Nebu.Profile.DB module

Create new module (follow `Nebu.Receipt.DB` pattern — place in `core/apps/event_dispatcher/lib/nebu/profile/db.ex` or check where `Nebu.Receipt.DB` is placed and follow same app):

```elixir
defmodule Nebu.Profile.DB do
  @moduledoc "PostgreSQL persistence for user profiles."

  @sql_upsert """
  INSERT INTO profiles (user_id, displayname, avatar_url, updated_at)
  VALUES ($1, $2, $3, $4)
  ON CONFLICT (user_id)
  DO UPDATE SET
    displayname = COALESCE(EXCLUDED.displayname, profiles.displayname),
    avatar_url  = COALESCE(EXCLUDED.avatar_url, profiles.avatar_url),
    updated_at  = EXCLUDED.updated_at
  """

  @doc """
  Upserts a profile row.

  Pass `nil` for a field to leave the existing value unchanged (COALESCE logic).
  """
  @spec upsert_profile(String.t(), String.t() | nil, String.t() | nil) :: :ok | {:error, term()}
  def upsert_profile(user_id, displayname, avatar_url) do
    now_ms = System.system_time(:millisecond)
    case Ecto.Adapters.SQL.query(Nebu.Repo, @sql_upsert, [user_id, displayname, avatar_url, now_ms]) do
      {:ok, _} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end
end
```

**COALESCE strategy:** If `displayname` is `nil`, the existing `displayname` is preserved. This allows partial updates (update only displayname or only avatar_url) without losing the other field.

### Go: PostgresProfileDB for direct profile reads

Create `gateway/internal/db/profile_store.go` (alongside `postgres_token_store.go` — follow that file's package and pattern):

```go
package db

import (
    "context"
    "database/sql"
    "errors"
    "github.com/nebu/nebu/internal/matrix"
)

// ErrProfileNotFound is returned when a profile row does not exist.
var ErrProfileNotFound = errors.New("profile not found")

type PostgresProfileDB struct {
    db *sql.DB
}

func NewPostgresProfileDB(db *sql.DB) *PostgresProfileDB {
    return &PostgresProfileDB{db: db}
}

func (p *PostgresProfileDB) GetProfile(ctx context.Context, userID string) (*matrix.ProfileData, error) {
    row := p.db.QueryRowContext(ctx,
        "SELECT displayname, avatar_url FROM profiles WHERE user_id = $1", userID)
    var displayname, avatarURL sql.NullString
    if err := row.Scan(&displayname, &avatarURL); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, ErrProfileNotFound
        }
        return nil, err
    }
    return &matrix.ProfileData{
        DisplayName: displayname.String,
        AvatarURL:   avatarURL.String,
    }, nil
}
```

**In profile.go GetProfile handler:** Check `errors.Is(err, db.ErrProfileNotFound)` → 404; other errors → 500.

**Circular import guard:** If `db` package imports from `matrix` package (for `ProfileData`), this creates a circular import since `matrix` would import `db`. Resolve by either:
- Option A: Define `ProfileData` in the `db` package and have `matrix` import from `db` — then `ProfileDB` interface in `matrix` uses `*db.ProfileData`.
- Option B: Define a separate `ProfileRecord` struct in the `db` package, and keep `ProfileData` in `matrix`; `PostgresProfileDB` implements the interface with a conversion.
- **Preferred:** Move `PostgresProfileDB` into `gateway/internal/matrix/` as `profile_db.go` (an unexported package-level implementation) — keep everything in the `matrix` package to avoid cross-package imports. Or use `gateway/internal/db/` with a standalone `ProfileData` type that the matrix handler converts.

Check how `db.NewPostgresTokenStore` is structured to decide. The cleanest approach for MVP: define a minimal `db.ProfileRow` struct in `gateway/internal/db/profile_store.go` that the `matrix.ProfileDB` interface returns — OR define `ProfileDB` interface to return `(string, string, error)` tuple to avoid struct cross-dependency.

---

## Files to Create

| File | Action |
|------|--------|
| `gateway/internal/matrix/profile.go` | CREATE — ProfileHandler (GET + PUT displayname + PUT avatar_url) |
| `gateway/internal/matrix/profile_test.go` | CREATE — unit tests (write FIRST) |
| `gateway/internal/matrix/presence.go` | CREATE — PresenceHandler (GET status) |
| `gateway/internal/matrix/presence_test.go` | CREATE — unit tests (write FIRST) |
| `gateway/migrations/000015_profiles.up.sql` | CREATE — profiles table |
| `gateway/migrations/000015_profiles.down.sql` | CREATE — down migration |
| `core/apps/event_dispatcher/lib/nebu/profile/db.ex` | CREATE — profile DB module |

## Files to Modify

| File | Change |
|------|--------|
| `proto/core.proto` | Add `rpc GetPresence` + `rpc UpdateProfile` + 4 message types |
| `gateway/internal/grpc/pb/core.pb.go` | REGENERATE via `make proto` |
| `gateway/internal/grpc/pb/core_grpc.pb.go` | REGENERATE via `make proto` |
| `core/apps/event_dispatcher/lib/pb/core.pb.ex` | REGENERATE via `make proto` |
| `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` | REGENERATE via `make proto` |
| `gateway/internal/grpc/client.go` | Implement `SetPresence` stub; add `GetPresence` + `UpdateProfile` methods |
| `gateway/internal/grpc/client_test.go` | Update `SetPresence` test case (was stub → now wired); add `GetPresence` + `UpdateProfile` test cases |
| `gateway/internal/grpc/stream_test.go` | Add `GetPresence` + `UpdateProfile` stubs to `mockCoreClient` |
| `gateway/cmd/gateway/main.go` | Register 4 new routes; open profile DB connection |
| `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` | Add `get_presence/2`, `update_profile/2` handlers; add `profile_db_module/0` private fn |
| `gateway/internal/db/` (or `matrix/`) | Add `PostgresProfileDB` implementing `matrix.ProfileDB` |

---

## Architecture Guardrails

### Go conventions
- `profile.go` and `presence.go` are separate files per architecture spec (see `gateway/internal/matrix/` tree in architecture.md: `profile.go` and `presence.go` explicitly listed)
- Consumer-defined interfaces: `ProfileCoreClient` in `profile.go`; `PresenceCoreClient` in `presence.go`; `ProfileDB` in `profile.go` — NOT in `client.go`
- Package: `package matrix` for both handler files
- Error handling: use `writeMatrixError(w, statusCode, errcode, message)` — already exists in `matrix` package
- Path values: `r.PathValue("userId")` (Go 1.22+ mux, consistent with typing.go)
- Auth context: `r.Context().Value(middleware.ContextKeySub).(string)` + `coregrpc.FormatUserID(sub, serverName)`
- gRPC context: `coregrpc.WithUserMetadata(r.Context(), userID, systemRole)` — see rooms.go

### GET /profile is unauthenticated — do NOT wrap with jwtMiddleware
- Matrix spec: profile is public; any client can read
- `mux.HandleFunc("GET /_matrix/client/v3/profile/{userId}", profileHandler.GetProfile)` — NO jwtMiddleware
- Go 1.22+ mux: method+path specificity takes precedence — separate registrations for GET, PUT/displayname, PUT/avatar_url

### Presence — offline default, never 404 from Core
- `Nebu.Presence.Manager.get_presence/1` ALWAYS returns `{:ok, %{status: ..., last_active_at: ...}}` — unknown users default to `:offline`
- Core's `get_presence/2` handler NEVER raises `GRPC.RPCError` with `not_found`
- Go presence handler: remove `codes.NotFound → 404` error mapping; only map `default → 500`
- `last_active_ago`: always `0` for offline users with `nil` last_active_at

### Elixir conventions
- No configurable module needed for `get_presence` (calls `Nebu.Presence.Manager` directly — it's already testable via its ETS)
- `update_profile` uses configurable `profile_db_module()` via `Application.get_env` pattern (mandatory for ExUnit testability)
- No try/rescue — let it crash + Supervisor
- `Nebu.Grpc.Metadata.trusted_identity/1` is the canonical way to extract `user_id` from stream metadata — check how it's used in `send_receipt/2` of EventDispatcher.Server

### profiles table — UPSERT semantics
- `PRIMARY KEY (user_id)` — one row per user
- `COALESCE` on conflict: passing `nil` for a field preserves existing value
- `updated_at BIGINT NOT NULL` — Unix milliseconds (consistent with all other BIGINT timestamps in schema)
- FK: `REFERENCES users(user_id)` — user must exist before profile can be created (Core enforces user existence implicitly since authenticated users are provisioned on first login)

### Migration numbering
- Previous: `000014_read_receipts` — next MUST be `000015_profiles`
- Do NOT skip numbers; do NOT reuse

### Proto field conventions
- `GetPresence`: `presence` field returns string atom name (not enum) — consistent with `SetPresence` request using string `"online"|"offline"|"unavailable"`
- `UpdateProfile`: empty string `""` means "do not update this field" — Core converts `""` to `nil` before upsert
- Proto field numbers: start at 1, never reuse

---

## Previous Story Intelligence (Story 4-17)

**Key learnings from Story 4-17 implementation notes:**

1. **`client.go` pattern for wired methods:** `return c.core.MethodName(ctx, req)` — single line; add to `client_test.go` expecting connection error (not nil,nil like stubs).

2. **`stream_test.go` mockCoreClient:** After adding new gRPC methods to proto, add the stub implementation to `mockCoreClient` in `stream_test.go` — otherwise compilation fails. Pattern: `func (m *mockCoreClient) GetPresence(ctx context.Context, req *pb.GetPresenceRequest, ...) (*pb.GetPresenceResponse, error) { return nil, nil }`.

3. **`client_test.go` SetTyping update:** When a stub becomes wired, update its test from "expect nil,nil" to "expect connection error" — consistent with all other wired methods. Do the same for `SetPresence` in this story.

4. **Receipt DB location:** `Nebu.Receipt.DB` was placed in `core/apps/event_dispatcher/lib/nebu/receipt/db.ex` (not in `room_manager`). Follow same pattern for `Nebu.Profile.DB` → `core/apps/event_dispatcher/lib/nebu/profile/db.ex`.

5. **Route registration order:** Add profile + presence routes AFTER receipts handler, BEFORE `slog.Info("HTTP server starting")`.

6. **`m.read.private`:** Dev added it as valid receipt type (implementation decision). This story has no similar edge — follow the spec strictly.

7. **FK constraints:** Story 4-17 dev omitted FK on `read_receipts.event_id` for test isolation. For `profiles`: `REFERENCES users(user_id)` is safe to keep since authenticated users are always provisioned before they can call PUT /profile.

**Established patterns to follow (unchanged from 4-17):**
- Handler constructor: `NewXxxHandler(cfg XxxConfig) *XxxHandler`
- Consumer-defined interface in handler file (not client.go)
- Test mock: `mockXxxCoreClient` struct in `_test.go`
- `buildAuthedHandler` + `setupOIDCServer` + `signJWT` test helpers in handler test files (see `rooms_test.go`)
- `writeMatrixError(w, code, errcode, message)` shared helper
- `json.NewEncoder(w).Encode(map[string]any{})` for empty 200 response
- `coregrpc.FormatUserID(sub, serverName)` for user ID construction

---

## Elixir Test Structure

### Test file locations
- Elixir tests: `core/apps/event_dispatcher/test/nebu/event_dispatcher/server_profile_test.exs` and `server_presence_test.exs`
  - Follow existing pattern: check `core/apps/event_dispatcher/test/` for existing test files to confirm exact location convention

### Elixir test setup for get_presence
- `Nebu.Presence.Manager.get_presence/1` reads directly from ETS (`:NebuPresence` table) — no configurable module
- For tests: ensure the ETS table `:NebuPresence` is accessible (Presence.Application starts it in supervised app; for unit tests, start Presence.Application or create the ETS table manually)
- Simpler test approach: use `Application.put_env` to swap a fake presence module: add `defp presence_module` to EventDispatcher.Server with `Application.get_env(:event_dispatcher, :presence_module, Nebu.Presence.Manager)` and call `presence_module().get_presence(user_id)` — makes tests fast and deterministic without real ETS

### Elixir test setup for update_profile
- `Application.put_env(:event_dispatcher, :profile_db_module, FakeProfileDB)` — same pattern as `receipt_db_module`
- `FakeProfileDB` implements `upsert_profile/3` — capture args via Agent for assertion

---

## Dependencies

- Story 4-7 (done): `Nebu.Presence.Manager` with `get_presence/1` — returns `{:ok, %{status, last_active_at}}`; ETS table `:NebuPresence` owned by Presence.Application
- Story 2-1 (done): `users` table exists with `user_id PRIMARY KEY` — required for `profiles` FK constraint
- Story 4-17 (review): `read_receipts` migration `000014` done — next is `000015`; `Nebu.Receipt.DB` placement pattern established
- Story 2-12 (done): User record DB write on first login — authenticated users always have a `users` row before PUT /profile is called

---

## Story Completion Status

Ultimate context engine analysis completed — comprehensive developer guide created.

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

No blocking issues encountered.

### Completion Notes List

- Implemented all proto additions: `GetPresence` and `UpdateProfile` RPCs with message definitions added to `proto/core.proto`; regenerated Go + Elixir stubs via `make proto`.
- Created `gateway/migrations/000015_profiles.up.sql` and `.down.sql` with `profiles` table (`user_id TEXT PK REFERENCES users(user_id)`, `displayname TEXT`, `avatar_url TEXT`, `updated_at BIGINT NOT NULL`).
- Created `gateway/internal/matrix/profile.go`: `ProfileHandler` with `GetProfile` (public, no JWT), `PutDisplayname` (JWT + userId==sub + 1-128 chars validation), `PutAvatarURL` (JWT + userId==sub + mxc:// validation). `ErrProfileNotFound` sentinel defined in package.
- Created `gateway/internal/matrix/presence.go`: `PresenceHandler` with `GetPresenceStatus` (JWT required). Returns 503 M_UNAVAILABLE on Core error; NO 404 for unknown users (Core always returns offline default).
- Created `gateway/internal/db/profile_store.go`: `PostgresProfileDB` implementing `matrix.ProfileDB`; returns `matrix.ErrProfileNotFound` on `sql.ErrNoRows`.
- Updated `gateway/internal/grpc/client.go`: Implemented `SetPresence` stub (was `nil,nil`); added `GetPresence` and `UpdateProfile` wired methods.
- Updated `gateway/internal/grpc/client_test.go`: Updated `SetPresence` test from nil,nil stub to connection-error; added `GetPresence` and `UpdateProfile` test cases.
- Updated `gateway/internal/grpc/stream_test.go`: Added `GetPresence` and `UpdateProfile` stubs to `mockCoreClient`.
- Updated `gateway/cmd/gateway/main.go`: Registered 4 new routes (GET /profile unauthenticated; PUT /profile/displayname, PUT /profile/avatar_url, GET /presence authenticated); reused `bootstrapDB` connection for profile store.
- Created `core/apps/event_dispatcher/lib/nebu/profile/db.ex`: `Nebu.Profile.DB` with `upsert_profile/3` using COALESCE strategy (nil preserves existing value).
- Updated `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`: Added `get_presence/2` handler (reads from configurable `presence_module()`), `update_profile/2` handler (uses `profile_db_module()`), `profile_db_module/0` and `presence_module/0` configurable helpers.
- Updated `core/apps/event_dispatcher/mix.exs`: Added `presence` as umbrella dependency (required for `Nebu.Presence.Manager`).
- Updated test mock `profile_test.go` to use exported `ErrProfileNotFound` instead of local unexported sentinel; removed unused `errors` import.
- All 7 Go profile tests pass, all 4 Go presence tests pass; all 86 Elixir event_dispatcher tests pass (2 skipped as expected).

### File List

- `proto/core.proto` — Added `GetPresence` + `UpdateProfile` RPCs and 4 message types
- `gateway/internal/grpc/pb/core.pb.go` — REGENERATED via `make proto`
- `gateway/internal/grpc/pb/core_grpc.pb.go` — REGENERATED via `make proto`
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — REGENERATED via `make proto`
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` — REGENERATED via `make proto`
- `gateway/migrations/000015_profiles.up.sql` — NEW: profiles table
- `gateway/migrations/000015_profiles.down.sql` — NEW: down migration
- `gateway/internal/matrix/profile.go` — NEW: ProfileHandler
- `gateway/internal/matrix/profile_test.go` — MODIFIED: use ErrProfileNotFound sentinel; remove unused errors import
- `gateway/internal/matrix/presence.go` — NEW: PresenceHandler
- `gateway/internal/matrix/presence_test.go` — unchanged (already passing)
- `gateway/internal/db/profile_store.go` — NEW: PostgresProfileDB
- `gateway/internal/grpc/client.go` — Implemented SetPresence; added GetPresence + UpdateProfile
- `gateway/internal/grpc/client_test.go` — Updated SetPresence test; added GetPresence + UpdateProfile tests
- `gateway/internal/grpc/stream_test.go` — Added GetPresence + UpdateProfile stubs to mockCoreClient
- `gateway/cmd/gateway/main.go` — Registered 4 new routes; profile DB wired
- `core/apps/event_dispatcher/lib/nebu/profile/db.ex` — NEW: Nebu.Profile.DB
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — Added get_presence/2 + update_profile/2 + helpers
- `core/apps/event_dispatcher/mix.exs` — Added presence umbrella dependency
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — Updated 4-18 → review
