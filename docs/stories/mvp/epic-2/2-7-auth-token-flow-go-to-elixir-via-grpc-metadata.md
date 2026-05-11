# Story 2.7: Auth Token Flow — Go to Elixir via gRPC Metadata

Status: done

## Change Log

- 2026-03-27: Implemented auth token metadata transport for Go→Elixir gRPC. Created `metadata.go` (Go) and `Nebu.Grpc.Metadata` (Elixir) with full unit test coverage.

## Story

As a developer,
I want validated user identity passed from Go to Elixir as gRPC metadata,
So that Elixir handlers have reliable user context without re-validating tokens.

## Acceptance Criteria

1. **Given** a validated Matrix API request with a known `user_id` and `system_role`,
   **When** the Go gateway makes any gRPC call to Elixir,
   **Then** it sets `"x-user-id": "@<sub>:<server_name>"` and `"x-system-role": "<role>"` in the gRPC outgoing metadata

2. **Given** an Elixir gRPC handler receiving a request,
   **When** it reads the gRPC metadata,
   **Then** `x-user-id` and `x-system-role` are accessible via the metadata map

3. **Given** Elixir's trust model,
   **When** Elixir handlers are reviewed,
   **Then** no handler re-validates the OIDC token — metadata values are trusted as authoritative

## Tasks / Subtasks

- [x] Create `gateway/internal/grpc/metadata.go` (AC: #1)
  - [x] Define exported constants: `MetadataKeyUserID = "x-user-id"`, `MetadataKeySystemRole = "x-system-role"`
  - [x] Implement `WithUserMetadata(ctx context.Context, userID, systemRole string) context.Context`
    - Uses `metadata.Pairs(MetadataKeyUserID, userID, MetadataKeySystemRole, systemRole)`
    - Returns `metadata.NewOutgoingContext(ctx, md)`
  - [x] Implement `FormatUserID(sub, serverName string) string`
    - Returns `"@" + sub + ":" + serverName`; returns `""` if sub is empty

- [x] Create `gateway/internal/grpc/metadata_test.go` (AC: #1)
  - [x] `TestWithUserMetadata`: set metadata → read back via `metadata.FromOutgoingContext` → assert keys/values
  - [x] Table-driven variant: cases for `("user", "user")`, `("instance_admin", "instance_admin")`, `("", "user")`
  - [x] `TestFormatUserID`: table-driven — `("abc123", "example.com")` → `"@abc123:example.com"`, `("", "example.com")` → `""`

- [x] Create `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex` (AC: #2, #3)
  - [x] Module `Nebu.Grpc.Metadata`
  - [x] `user_id(stream)` — extracts `"x-user-id"` from `stream.adapter.payload.headers`, returns `String.t() | nil`
  - [x] `system_role(stream)` — extracts `"x-system-role"`, defaults to `"user"` if absent
  - [x] `trusted_identity(stream)` — returns `{user_id(stream), system_role(stream)}`
  - [x] Private `get_header(stream, key)` — uses `List.keyfind/3` on the headers proplist

- [x] Create `core/apps/event_dispatcher/test/nebu/grpc/metadata_test.exs` (AC: #2, #3)
  - [x] `test "user_id/1 returns value when header present"`
  - [x] `test "user_id/1 returns nil when header absent"`
  - [x] `test "system_role/1 returns value when present"`
  - [x] `test "system_role/1 defaults to user when absent"`
  - [x] `test "trusted_identity/1 returns both values"`

## Dev Notes

### What This Story Does

Creates the metadata transport infrastructure for the auth token flow — pure helper code, no wiring yet.

- **Go:** `gateway/internal/grpc/metadata.go` — `WithUserMetadata` injects `x-user-id`/`x-system-role` into outgoing gRPC context; `FormatUserID` formats `@{sub}:{serverName}`
- **Elixir:** `Nebu.Grpc.Metadata` — helper module to extract identity from incoming gRPC stream headers

**Scope boundary:** This story creates the helpers. Epic 4 stories wire them into actual Matrix API handlers when stubs are replaced. Do NOT modify `client.go` or `server.ex` stubs.

**Architecture rule (CRITICAL):** The OIDC token is NEVER passed to Elixir. Only `user_id` + `system_role` are forwarded as gRPC metadata. [Source: architecture.md#Enforcement rule 2]

### Go: metadata.go

**Package:** `package grpc` (in `gateway/internal/grpc/`)

**Import:** `"google.golang.org/grpc/metadata"` — already available as sub-package of `google.golang.org/grpc v1.79.3` (no new dep needed)

**No new import of `middleware` needed** — `FormatUserID` takes plain strings; caller extracts `sub` from HTTP context using `middleware.ContextKeySub`.

```go
package grpc

import (
    "context"

    "google.golang.org/grpc/metadata"
)

const (
    MetadataKeyUserID     = "x-user-id"
    MetadataKeySystemRole = "x-system-role"
)

// WithUserMetadata returns ctx with x-user-id and x-system-role set as outgoing gRPC metadata.
// userID must already be formatted as "@{sub}:{serverName}" via FormatUserID.
func WithUserMetadata(ctx context.Context, userID, systemRole string) context.Context {
    md := metadata.Pairs(
        MetadataKeyUserID, userID,
        MetadataKeySystemRole, systemRole,
    )
    return metadata.NewOutgoingContext(ctx, md)
}

// FormatUserID builds a Matrix user ID from an OIDC sub claim and the server name.
// Returns "" if sub is empty (unauthenticated context).
func FormatUserID(sub, serverName string) string {
    if sub == "" {
        return ""
    }
    return "@" + sub + ":" + serverName
}
```

**Import alias note:** The `grpc` package already imports `google.golang.org/grpc` as `grpclib` (see `client.go`). The new `google.golang.org/grpc/metadata` sub-package imports as just `"metadata"` — no alias conflict.

**Usage pattern for Epic 4 handlers (do NOT implement now — for reference only):**
```go
// In a Matrix API handler, before any gRPC call:
sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
userID := grpcpkg.FormatUserID(sub, cfg.ServerName)
grpcCtx := grpcpkg.WithUserMetadata(r.Context(), userID, systemRole)
resp, err := client.SendEvent(grpcCtx, req)
```

**Server name source:** `cfg.ServerName` from `Config.ServerName` (env var `NEBU_SERVER_NAME`).

### Go: metadata_test.go

**Package:** `package grpc` (white-box, consistent with `client_test.go`)

```go
package grpc

import (
    "context"
    "testing"

    "google.golang.org/grpc/metadata"
)

func TestWithUserMetadata_SetsOutgoingMetadata(t *testing.T) {
    tests := []struct {
        name       string
        userID     string
        systemRole string
    }{
        {"regular user", "@alice:example.com", "user"},
        {"instance admin", "@kai:example.com", "instance_admin"},
        {"empty values", "", "user"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := WithUserMetadata(context.Background(), tt.userID, tt.systemRole)
            md, ok := metadata.FromOutgoingContext(ctx)
            if !ok {
                t.Fatal("no outgoing metadata found in context")
            }
            if got := md.Get(MetadataKeyUserID); len(got) == 0 || got[0] != tt.userID {
                t.Errorf("x-user-id = %v, want %q", got, tt.userID)
            }
            if got := md.Get(MetadataKeySystemRole); len(got) == 0 || got[0] != tt.systemRole {
                t.Errorf("x-system-role = %v, want %q", got, tt.systemRole)
            }
        })
    }
}

func TestFormatUserID(t *testing.T) {
    tests := []struct {
        sub        string
        serverName string
        want       string
    }{
        {"abc-uuid-123", "example.com", "@abc-uuid-123:example.com"},
        {"", "example.com", ""},
        {"user1", "nebu.internal", "@user1:nebu.internal"},
    }
    for _, tt := range tests {
        t.Run(tt.sub+"/"+tt.serverName, func(t *testing.T) {
            got := FormatUserID(tt.sub, tt.serverName)
            if got != tt.want {
                t.Errorf("FormatUserID(%q, %q) = %q, want %q", tt.sub, tt.serverName, got, tt.want)
            }
        })
    }
}
```

### Elixir: Nebu.Grpc.Metadata

**grpc-elixir version:** `~> 0.8` (from `core/apps/event_dispatcher/mix.exs`)

In grpc-elixir 0.8, gRPC client metadata arrives as HTTP/2 headers stored as a proplist in `stream.adapter.payload.headers`. Each entry is a `{binary_key, binary_value}` tuple.

```elixir
defmodule Nebu.Grpc.Metadata do
  @moduledoc """
  Extracts validated user identity from incoming gRPC stream metadata.

  Elixir trusts these values fully — the Go gateway has already validated the OIDC token.
  No re-validation in Elixir (Architecture Rule: auth token never forwarded to Elixir).
  """

  @user_id_key "x-user-id"
  @system_role_key "x-system-role"
  @default_role "user"

  @doc "Returns x-user-id from gRPC stream metadata, or nil if absent."
  @spec user_id(GRPC.Server.Stream.t()) :: String.t() | nil
  def user_id(stream), do: get_header(stream, @user_id_key)

  @doc "Returns x-system-role from gRPC stream metadata. Defaults to \"user\" if absent."
  @spec system_role(GRPC.Server.Stream.t()) :: String.t()
  def system_role(stream), do: get_header(stream, @system_role_key) || @default_role

  @doc "Returns {user_id, system_role} tuple. system_role defaults to \"user\" if absent."
  @spec trusted_identity(GRPC.Server.Stream.t()) :: {String.t() | nil, String.t()}
  def trusted_identity(stream), do: {user_id(stream), system_role(stream)}

  defp get_header(stream, key) do
    headers = stream.adapter.payload.headers
    case List.keyfind(headers, key, 0) do
      {^key, value} -> value
      nil -> nil
    end
  end
end
```

### Elixir: metadata_test.exs

Tests use a bare map as a fake stream — this works because the module only accesses `stream.adapter.payload.headers` via map key access.

```elixir
defmodule Nebu.Grpc.MetadataTest do
  use ExUnit.Case, async: true
  alias Nebu.Grpc.Metadata

  defp build_stream(headers) do
    %{adapter: %{payload: %{headers: headers}}}
  end

  test "user_id/1 returns value when header present" do
    stream = build_stream([{"x-user-id", "@alice:example.com"}])
    assert Metadata.user_id(stream) == "@alice:example.com"
  end

  test "user_id/1 returns nil when header absent" do
    stream = build_stream([])
    assert Metadata.user_id(stream) == nil
  end

  test "system_role/1 returns value when present" do
    stream = build_stream([{"x-system-role", "instance_admin"}])
    assert Metadata.system_role(stream) == "instance_admin"
  end

  test "system_role/1 defaults to user when absent" do
    stream = build_stream([])
    assert Metadata.system_role(stream) == "user"
  end

  test "trusted_identity/1 returns both values" do
    stream = build_stream([{"x-user-id", "@kai:example.com"}, {"x-system-role", "instance_admin"}])
    assert Metadata.trusted_identity(stream) == {"@kai:example.com", "instance_admin"}
  end
end
```

### Project Structure Notes

Files to create:
```
gateway/
  internal/
    grpc/
      client.go            ← EXISTS — do NOT touch
      client_test.go       ← EXISTS — do NOT touch
      metadata.go          ← CREATE
      metadata_test.go     ← CREATE

core/
  apps/
    event_dispatcher/
      lib/
        nebu/
          grpc/
            metadata.ex    ← CREATE (new directory nebu/grpc/)
          event_dispatcher/
            server.ex      ← EXISTS — do NOT touch
      test/
        nebu/
          grpc/
            metadata_test.exs ← CREATE (new test directory nebu/grpc/)
```

**New directories needed:**
- `core/apps/event_dispatcher/lib/nebu/grpc/` (new)
- `core/apps/event_dispatcher/test/nebu/grpc/` (new)

Elixir auto-discovers modules and tests by directory — no registration needed.

### Cross-Story Context

| Story | Relationship |
|-------|-------------|
| 2.4   | `JWTMiddleware` sets `ContextKeySub` + `ContextKeySystemRole` in HTTP context — Story 2.7 reads these via `FormatUserID` call sites |
| 2.14  | `ValidateToken` gRPC handler in Elixir uses `Nebu.Grpc.Metadata` to read `x-user-id`/`x-system-role` |
| 4.x   | All Matrix API gRPC calls (SendEvent, CreateRoom, etc.) will use `WithUserMetadata` before calling Core |

### Build & Test

```bash
make test-unit-go      # go test -race ./... — must pass; includes metadata_test.go
make build-gateway     # verify compilation
make test-unit-elixir  # mix test — must pass; includes metadata_test.exs
```

### References

- [Source: architecture.md#Auth-Token-Flow] Canonical: Go → Elixir via `x-user-id` + `x-system-role` only
- [Source: architecture.md#Enforcement rule 2] "Auth-Token nie an Elixir weitergeben"
- [Source: epics.md#Story-2.7] Acceptance criteria (authoritative)
- [Source: gateway/internal/middleware/auth.go] `ContextKeySub`, `ContextKeySystemRole` — exported context keys
- [Source: gateway/internal/config/config.go] `ServerName string // NEBU_SERVER_NAME`
- [Source: gateway/internal/grpc/client.go] Package pattern, `grpclib` alias convention
- [Source: gateway/internal/grpc/client_test.go] Test package convention: `package grpc`
- [Source: core/apps/event_dispatcher/mix.exs] `grpc ~> 0.8`
- [Source: core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex] Existing gRPC server pattern
- [Source: gateway/go.mod] `google.golang.org/grpc v1.79.3` — metadata sub-package included

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

None — implementation was straightforward following story spec exactly.

### Completion Notes List

- Created `gateway/internal/grpc/metadata.go` with `WithUserMetadata`, `FormatUserID`, and constants.
  Uses `google.golang.org/grpc/metadata` (already in go.mod as sub-package of grpc v1.79.3, no new deps).
- Created `gateway/internal/grpc/metadata_test.go` with table-driven tests for both functions; all pass with `-race`.
- Created `core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex` module `Nebu.Grpc.Metadata` with `user_id/1`,
  `system_role/1`, `trusted_identity/1`, and private `get_header/2` using `List.keyfind/3`.
- Created `core/apps/event_dispatcher/test/nebu/grpc/metadata_test.exs` with 5 unit tests using bare map as fake stream.
- All tests pass: `go test -race ./...` (ok internal/grpc), `mix test --warnings-as-errors` (21 tests, 0 failures).
- Scope boundary respected: `client.go` and `server.ex` were NOT modified. No new dependencies introduced.

### File List

gateway/internal/grpc/metadata.go
gateway/internal/grpc/metadata_test.go
core/apps/event_dispatcher/lib/nebu/grpc/metadata.ex
core/apps/event_dispatcher/test/nebu/grpc/metadata_test.exs
