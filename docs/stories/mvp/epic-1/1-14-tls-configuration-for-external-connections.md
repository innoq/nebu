# Story 1.14: TLS Configuration for External Connections

Status: done

## Story

As an operator,
I want TLS configurable for external client-to-gateway connections,
so that production deployments are encrypted while local development remains frictionless.

## Acceptance Criteria

1. **TLS enabled path:** Given `NEBU_TLS_CERT_FILE` and `NEBU_TLS_KEY_FILE` env vars are both set, when the gateway starts, then it listens on HTTPS on `:8443` with TLS 1.2 as minimum protocol version and TLS 1.3 preferred.

2. **TLS disabled path:** Given TLS env vars are NOT set, when the gateway starts, then it listens on plain HTTP on `:8080` and logs a warning: `"TLS disabled — not suitable for production"`.

3. **TLS 1.1 rejection:** Given a TLS connection from a client using TLS 1.1 or lower, when processed by the gateway, then the connection is rejected with a TLS handshake error.

4. **docker-compose local dev:** Given the `docker-compose.yml` for local development, when TLS env vars are absent from the gateway service, then the gateway runs on plain HTTP — no certificate setup required for local development.

5. **mTLS placeholder:** Given `NEBU_TLS_CLIENT_CA_FILE` is NOT set (mTLS Phase 2), when the gateway starts, then mTLS is disabled — client certificates are not required (forward-compatible placeholder).

## Tasks / Subtasks

- [x] Add TLS fields to `gateway/internal/config/config.go` (AC: #1, #2, #5)
  - [x] Add `TLSCertFile string // NEBU_TLS_CERT_FILE`
  - [x] Add `TLSKeyFile string // NEBU_TLS_KEY_FILE`
  - [x] Add `TLSClientCAFile string // NEBU_TLS_CLIENT_CA_FILE (mTLS Phase 2 placeholder)`
  - [x] Load all three via `os.Getenv(...)` in `Load()`
- [x] Extend `gateway/internal/config/config_test.go` (AC: #1, #2, #5)
  - [x] Test: TLS fields empty by default when env vars unset (extend `TestLoad_Defaults`)
  - [x] Test: `NEBU_TLS_CERT_FILE` and `NEBU_TLS_KEY_FILE` are loaded from env vars
  - [x] Test: `NEBU_TLS_CLIENT_CA_FILE` is loaded from env var
- [x] Update `gateway/cmd/gateway/main.go` to conditionally start TLS or plain HTTP (AC: #1, #2, #3, #4)
  - [x] Import `crypto/tls`
  - [x] After `metrics := admin.NewMetrics(...)`, replace the `go func() { http.ListenAndServe(":8080", ...) }()` block:
    - [x] If `cfg.TLSCertFile != "" && cfg.TLSKeyFile != ""`: build `http.Server{Addr: ":8443", TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, Handler: metrics.Middleware(pubMux)}`, start with `ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)` in goroutine
    - [x] Else: log warning `slog.Warn("TLS disabled — not suitable for production")`, keep `http.ListenAndServe(":8080", metrics.Middleware(pubMux))` in goroutine
  - [x] Log start message: `"Public HTTPS server starting"` with `addr: ":8443"` for TLS path; `"Public HTTP server starting"` with `addr: ":8080"` for plain path
- [x] Verify `docker-compose.yml` has no `NEBU_TLS_CERT_FILE` / `NEBU_TLS_KEY_FILE` for gateway service (AC: #4) — no code change needed, just verify the gateway service block
- [x] Run `make test-unit-go` and confirm all tests pass, no regressions (AC: #1–#5)

## Dev Notes

### File Placement

Files to modify (no new files needed):
```
gateway/internal/config/config.go        ← add 3 TLS fields + env reads
gateway/internal/config/config_test.go   ← extend existing tests
gateway/cmd/gateway/main.go              ← replace pubMux goroutine with conditional TLS
```

### Config Changes — Pattern to Follow

`config.go` currently follows the pattern of `os.Getenv(key)` for optional fields (no defaults). The three TLS fields all follow this same pattern — they are optional and default to `""`:

```go
// Add to Config struct:
TLSCertFile    string // NEBU_TLS_CERT_FILE
TLSKeyFile     string // NEBU_TLS_KEY_FILE
TLSClientCAFile string // NEBU_TLS_CLIENT_CA_FILE (mTLS Phase 2 — not wired up in MVP)

// Add to Load() return:
TLSCertFile:    os.Getenv("NEBU_TLS_CERT_FILE"),
TLSKeyFile:     os.Getenv("NEBU_TLS_KEY_FILE"),
TLSClientCAFile: os.Getenv("NEBU_TLS_CLIENT_CA_FILE"),
```

`TLSClientCAFile` is a forward-compatible placeholder only — add the config field but do **not** wire it into the TLS setup in this story. It is logged as unused/future work.

### main.go — TLS Branch Pattern

Replace the current pubMux goroutine block in `gateway/cmd/gateway/main.go`:

```go
// CURRENT (lines 62–68 in main.go):
go func() {
    slog.Info("Public HTTP server starting", "addr", ":8080")
    if err := http.ListenAndServe(":8080", metrics.Middleware(pubMux)); err != nil {
        slog.Error("Public HTTP server failed", "err", err)
        os.Exit(1)
    }
}()

// REPLACE WITH:
go func() {
    handler := metrics.Middleware(pubMux)
    if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
        srv := &http.Server{
            Addr:    ":8443",
            Handler: handler,
            TLSConfig: &tls.Config{
                MinVersion: tls.VersionTLS12,
            },
        }
        slog.Info("Public HTTPS server starting", "addr", ":8443")
        if err := srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
            slog.Error("Public HTTPS server failed", "err", err)
            os.Exit(1)
        }
    } else {
        slog.Warn("TLS disabled — not suitable for production")
        slog.Info("Public HTTP server starting", "addr", ":8080")
        if err := http.ListenAndServe(":8080", handler); err != nil {
            slog.Error("Public HTTP server failed", "err", err)
            os.Exit(1)
        }
    }
}()
```

Add `"crypto/tls"` to the imports in `main.go`.

### TLS 1.1 Rejection — How It Works

AC3 (TLS 1.1 rejection) is guaranteed automatically by `tls.Config{MinVersion: tls.VersionTLS12}`. Go's crypto/tls will reject the handshake for any client offering TLS 1.1 or lower with a handshake error. No additional code is required — setting MinVersion is sufficient.

### TLS 1.3 as Preferred (AC1)

Go's crypto/tls negotiates TLS 1.3 automatically when the client supports it, whenever `MinVersion` is set to `tls.VersionTLS12` or lower. There is no need to explicitly set `MaxVersion` or configure cipher suites — the stdlib handles TLS 1.3 preference by default since Go 1.18+. `tls.Config{MinVersion: tls.VersionTLS12}` is the complete and correct config for "1.2 minimum, 1.3 preferred."

### mTLS Placeholder (AC5)

`NEBU_TLS_CLIENT_CA_FILE` is loaded into `cfg.TLSClientCAFile` but **not used** in this story. Phase 2 (Ephemeral mTLS) is documented in ADR-008 (`docs/architecture/adr/008-node-registration-psk-mtls.md`). Do not wire up client CA verification in this story — just add the config field.

### config_test.go — Test Pattern

Tests use `package config_test` (external test package — note this differs from other packages like `package admin` used in `metrics_test.go`). Use `t.Setenv()` for env vars (auto-cleanup) and `os.Unsetenv()` in defaults test.

Extend `TestLoad_Defaults` to also check new TLS fields:
```go
if cfg.TLSCertFile != "" {
    t.Errorf("TLSCertFile: got %q, want empty", cfg.TLSCertFile)
}
if cfg.TLSKeyFile != "" {
    t.Errorf("TLSKeyFile: got %q, want empty", cfg.TLSKeyFile)
}
if cfg.TLSClientCAFile != "" {
    t.Errorf("TLSClientCAFile: got %q, want empty", cfg.TLSClientCAFile)
}
```

Add a dedicated TLS test:
```go
func TestLoad_TLSFields(t *testing.T) {
    t.Setenv("NEBU_TLS_CERT_FILE", "/certs/server.crt")
    t.Setenv("NEBU_TLS_KEY_FILE", "/certs/server.key")
    t.Setenv("NEBU_TLS_CLIENT_CA_FILE", "/certs/ca.crt")

    cfg := config.Load()

    if cfg.TLSCertFile != "/certs/server.crt" {
        t.Errorf("TLSCertFile: got %q, want /certs/server.crt", cfg.TLSCertFile)
    }
    if cfg.TLSKeyFile != "/certs/server.key" {
        t.Errorf("TLSKeyFile: got %q, want /certs/server.key", cfg.TLSKeyFile)
    }
    if cfg.TLSClientCAFile != "/certs/ca.crt" {
        t.Errorf("TLSClientCAFile: got %q, want /certs/ca.crt", cfg.TLSClientCAFile)
    }
}
```

### Port Strategy

| Mode | Port | Env vars |
|---|---|---|
| TLS enabled (production) | `:8443` | Both `NEBU_TLS_CERT_FILE` and `NEBU_TLS_KEY_FILE` set |
| Plain HTTP (local dev) | `:8080` | Neither TLS var set |

Port 8443 matches the architecture diagram ("Port 443/8443" for external HTTPS). The internal mux port `:8008` (node registry) is unchanged.

docker-compose.yml currently exposes `:8008` and `:8080` for the gateway service. No changes to docker-compose are needed for this story — the absence of TLS env vars in the gateway service block satisfies AC4.

### Architecture Compliance

- [Source: architecture.md#Architektur-Grenzen] `Internet → [TLS 1.3] → Go Gateway (Port 443/8443)` — 8443 is the HTTPS port
- [Source: epics.md FR47] `System unterstützt TLS-Terminierung (mTLS optional konfigurierbar)` — this story delivers FR47 MVP
- [Source: epics.md NFR-S1] `Alle externen Verbindungen via TLS 1.2 minimum (TLS 1.3 bevorzugt)` — enforced by `MinVersion: tls.VersionTLS12`
- [Source: architecture.md#Requirements-Mapping] `FR43–47 (Ops + TLS)` → `gateway/internal/admin/metrics.go + docker-compose.yml` — TLS config lives in config.go + main.go (not admin package)
- [Source: architecture.md#Technical-Constraints] `NEBU_* env vars` pattern: `NEBU_{COMPONENT}_{KEY}` — new fields follow `NEBU_TLS_*` pattern

### Previous Story Intelligence (from 1-13 Prometheus metrics)

- **Config pattern**: always use `os.Getenv()` for optional fields, never `os.ReadFile()` for TLS paths — store the **path**, callers read the file (same as `NEBU_INTERNAL_SECRET_FILE`)
- **main.go structure**: the `go func() { http.ListenAndServe }()` goroutine is replaced in-place — do not change the `mux` / `:8008` internal server below it
- **Test package style**: `config_test.go` uses `package config_test` (external). This differs from `metrics_test.go` which uses `package admin` (internal). Follow whichever matches the file being edited
- **`make test-unit-go`** runs `go test ./...` inside `golang:1.26-alpine` via Docker — no local Go installation needed
- **All 1-13 gateway tests must pass** (0 regressions) — particularly `admin.TestMetrics_*` and `health.TestHealth_*` tests which depend on `main.go` integration patterns

### Project Structure Notes

- Modification only — no new files required
- `gateway/go.mod` module: `github.com/nebu/nebu`, Go 1.26 — `crypto/tls` is stdlib, no new dependencies needed
- The internal mux (`:8008`, node registry + PSK middleware) is **unchanged** by this story
- docker-compose.yml gateway service already lacks TLS env vars — this satisfies AC4 without modification

### References

- [Source: epics.md#Story-1.14] Full AC and user story (lines 662–690)
- [Source: epics.md FR47, NFR-S1] TLS functional and non-functional requirements (lines 65, 78)
- [Source: architecture.md#Architektur-Grenzen] Port 443/8443 for external TLS (line 1190)
- [Source: architecture.md#Core-Decisions] mTLS Phase 2 pattern with ADR-008 (lines 258–263)
- [Source: gateway/internal/config/config.go] Current Config struct + Load() — add TLS fields here
- [Source: gateway/internal/config/config_test.go] Test pattern: `package config_test`, `t.Setenv()`, `os.Unsetenv()`
- [Source: gateway/cmd/gateway/main.go:62–68] pubMux goroutine — the block to replace with TLS branch
- [Source: docker-compose.yml#gateway] Gateway service env vars — no TLS vars present (AC4 satisfied)

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- Added 3 TLS fields (TLSCertFile, TLSKeyFile, TLSClientCAFile) to Config struct and Load() in config.go
- Extended TestLoad_Defaults to unset and verify all 3 TLS env vars are empty by default
- Added TestLoad_TLSFields to verify all 3 TLS fields load correctly from env vars
- Replaced pubMux goroutine in main.go with TLS branch: HTTPS on :8443 when both cert+key set, plain HTTP on :8080 with warning otherwise
- Added crypto/tls import to main.go; TLSConfig sets MinVersion: tls.VersionTLS12 (TLS 1.1 rejection + TLS 1.3 preference automatic)
- TLSClientCAFile is loaded but not wired (mTLS Phase 2 placeholder per ADR-008)
- Verified docker-compose.yml has no NEBU_TLS_* vars for gateway service (AC4 satisfied without code changes)
- All 9 test packages pass, 0 regressions
- Code review fix: Added partial TLS config warning (only cert OR only key set) to prevent silent HTTP fallback

### File List

- gateway/internal/config/config.go
- gateway/internal/config/config_test.go
- gateway/cmd/gateway/main.go

## Change Log

- 2026-03-24: Implemented TLS configuration — conditional HTTPS (:8443) / HTTP (:8080) start, TLS 1.2 minimum, TLS 1.3 preferred, mTLS placeholder field
- 2026-03-25: Code review fix — added partial TLS config warning when only one of cert/key is set
