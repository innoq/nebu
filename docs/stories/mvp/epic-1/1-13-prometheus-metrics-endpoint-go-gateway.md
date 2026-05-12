# Story 1.13: Prometheus Metrics Endpoint — Go Gateway

Status: done

## Story

As an operator,
I want a Prometheus-compatible metrics endpoint on the gateway,
so that standard monitoring tools can scrape operational data without custom instrumentation.

## Acceptance Criteria

1. **GET :8080/metrics — 200 OK:** Response is `200 OK` with `Content-Type: text/plain; version=0.0.4` (Prometheus exposition format).

2. **Standard Go runtime metrics present:** The response includes `go_goroutines`, `go_memstats_alloc_bytes`, and `go_gc_duration_seconds` (from Go runtime collector registered by default).

3. **`nebu_http_requests_total` counter:** Counter is present, increments on each completed HTTP request, and carries `method` and `status_code` labels (e.g., `method="GET"`, `status_code="200"`).

4. **`nebu_grpc_status` gauge:** Gauge is present and reflects current gateway status: `2` = GRÜN (Ready), `1` = GELB (Idle/Connecting), `0` = ROT (TransientFailure/Shutdown).

5. **Dependency added:** `github.com/prometheus/client_golang` is a direct dep in `gateway/go.mod`; `go build ./...` succeeds.

6. **No authentication:** `GET /metrics` returns `200 OK` without any auth header — ops/internal use only.

## Tasks / Subtasks

- [x] Add `prometheus/client_golang` to `gateway/go.mod` (AC: #5)
  - [x] Run: `docker run --rm -v $(PWD):/workspace -w /workspace/gateway golang:1.26-alpine sh -c "go get github.com/prometheus/client_golang && go mod tidy"`
  - [x] Verify `go.mod` has `github.com/prometheus/client_golang v1.x.x` as a direct `require`
- [x] Create `gateway/internal/admin/metrics.go` (AC: #1–#4)
  - [x] Define `coreStateProvider` interface with `State() connectivity.State`
  - [x] Define `Metrics` struct holding `httpRequestsTotal *prometheus.CounterVec`
  - [x] Implement `NewMetrics(reg prometheus.Registerer, core coreStateProvider) *Metrics`
    - [x] Register `nebu_http_requests_total` counter vec with labels `["method", "status_code"]`
    - [x] Register `nebu_grpc_status` GaugeFunc mapping `connectivity.State` → 2/1/0
  - [x] Implement `Middleware(h http.Handler) http.Handler` with `statusRecorder` to capture response code
- [x] Update `gateway/cmd/gateway/main.go` (AC: #1, #3, #6)
  - [x] Import `admin` and `prometheus` packages
  - [x] Call `admin.NewMetrics(prometheus.DefaultRegisterer, coreClient)` before building pubMux
  - [x] Add `pubMux.Handle("GET /metrics", promhttp.Handler())` route
  - [x] Wrap the pubMux server with `metrics.Middleware(pubMux)` when calling `http.ListenAndServe`
- [x] Create `gateway/internal/admin/metrics_test.go` (AC: #1–#4)
  - [x] Test: `MetricsHandler` returns 200 with `text/plain` Content-Type
  - [x] Test: `nebu_grpc_status` = 2 when core state is `connectivity.Ready`
  - [x] Test: `nebu_grpc_status` = 1 when core state is `connectivity.Idle`
  - [x] Test: `nebu_grpc_status` = 1 when core state is `connectivity.Connecting`
  - [x] Test: `nebu_grpc_status` = 0 when core state is `connectivity.TransientFailure`
  - [x] Test: `nebu_grpc_status` = 0 when core state is `connectivity.Shutdown`
  - [x] Test: `Middleware` increments `nebu_http_requests_total{method="GET",status_code="200"}` on successful request
  - [x] Test: `Middleware` captures non-200 status codes (e.g., 503)
- [x] Run `make test-unit-go` and confirm all tests pass, no regressions (AC: #5)

## Dev Notes

### File Placement

Per architecture (FR44-45), observability lives in `gateway/internal/admin/metrics.go`:
```
gateway/internal/admin/metrics.go       ← new file
gateway/internal/admin/metrics_test.go  ← new file
```
The `gateway/internal/admin/.gitkeep` placeholder already exists — delete it or let it coexist.

### Dependency — prometheus/client_golang

Add as direct dependency. Use the container to keep everything Docker-only (CLAUDE.md requirement):

```bash
docker run --rm -v $(PWD):/workspace -w /workspace/gateway golang:1.26-alpine \
  sh -c "go get github.com/prometheus/client_golang && go mod tidy"
```

This updates `gateway/go.mod` and `gateway/go.sum`. Packages used:
- `github.com/prometheus/client_golang/prometheus` — CounterVec, GaugeFunc, Registerer
- `github.com/prometheus/client_golang/prometheus/promhttp` — HTTP exposition handler

### metrics.go — Full Implementation Pattern

```go
package admin

import (
    "fmt"
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "google.golang.org/grpc/connectivity"
)

// coreStateProvider is the minimal interface needed to read gRPC state.
// *coregrpc.Client satisfies this interface without importing that package.
type coreStateProvider interface {
    State() connectivity.State
}

// Metrics holds all custom Prometheus metrics for the gateway.
type Metrics struct {
    httpRequestsTotal *prometheus.CounterVec
}

// NewMetrics registers nebu_http_requests_total and nebu_grpc_status with reg.
// In production: call with prometheus.DefaultRegisterer.
// In tests: call with prometheus.NewRegistry() to avoid global state pollution.
func NewMetrics(reg prometheus.Registerer, core coreStateProvider) *Metrics {
    m := &Metrics{
        httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "nebu_http_requests_total",
            Help: "Total number of completed HTTP requests.",
        }, []string{"method", "status_code"}),
    }
    reg.MustRegister(m.httpRequestsTotal)

    reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
        Name: "nebu_grpc_status",
        Help: "Current gRPC connection status: 2=GRÜN (Ready), 1=GELB (Idle/Connecting), 0=ROT (down).",
    }, func() float64 {
        switch core.State() {
        case connectivity.Ready:
            return 2
        case connectivity.Idle, connectivity.Connecting:
            return 1
        default: // TransientFailure, Shutdown
            return 0
        }
    }))

    return m
}

// Middleware wraps h, recording nebu_http_requests_total after each completed request.
func (m *Metrics) Middleware(h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
        h.ServeHTTP(rec, r)
        m.httpRequestsTotal.WithLabelValues(r.Method, fmt.Sprintf("%d", rec.code)).Inc()
    })
}

// statusRecorder captures the HTTP status code written by a handler.
type statusRecorder struct {
    http.ResponseWriter
    code int
}

func (sr *statusRecorder) WriteHeader(code int) {
    sr.code = code
    sr.ResponseWriter.WriteHeader(code)
}
```

### main.go — Integration

Add to `gateway/cmd/gateway/main.go` (after gRPC client init, before pubMux routes):

```go
import (
    // existing imports...
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/nebu/nebu/internal/admin"
)

// ... (after coreClient init)

metrics := admin.NewMetrics(prometheus.DefaultRegisterer, coreClient)

pubMux := http.NewServeMux()
healthHandler := health.NewHandler(cfg.DBURL, coreClient)
pubMux.HandleFunc("GET /health", healthHandler.Health)
pubMux.HandleFunc("GET /ready", healthHandler.Ready)
pubMux.Handle("GET /metrics", promhttp.Handler())  // ← Add this line

go func() {
    slog.Info("Public HTTP server starting", "addr", ":8080")
    if err := http.ListenAndServe(":8080", metrics.Middleware(pubMux)); err != nil {  // ← Wrap with Middleware
        slog.Error("Public HTTP server failed", "err", err)
        os.Exit(1)
    }
}()
```

**Critical**: `promhttp.Handler()` uses `prometheus.DefaultGatherer` which includes Go runtime metrics automatically (go_goroutines, go_memstats_alloc_bytes, go_gc_duration_seconds). Do NOT replace it with a custom registry handler unless you also register GoCollector and ProcessCollector.

### metrics_test.go — Full Test Pattern

Use `prometheus.NewRegistry()` per test to avoid global state contamination. Use `promhttp.HandlerFor(reg, ...)` as handler:

```go
package admin

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "google.golang.org/grpc/connectivity"
)

type fakeCore struct{ state connectivity.State }
func (f fakeCore) State() connectivity.State { return f.state }

func newTestMetrics(t *testing.T, state connectivity.State) (*Metrics, prometheus.Gatherer) {
    t.Helper()
    reg := prometheus.NewRegistry()
    m := NewMetrics(reg, fakeCore{state: state})
    return m, reg
}

func TestMetrics_handler_returns_prometheus_format(t *testing.T) {
    _, gatherer := newTestMetrics(t, connectivity.Ready)
    handler := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
    req := httptest.NewRequest("GET", "/metrics", nil)
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Errorf("got %d, want 200", rr.Code)
    }
    if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
        t.Errorf("Content-Type got %q, want text/plain prefix", ct)
    }
}

func TestNebuGrpcStatus_values(t *testing.T) {
    cases := []struct {
        state connectivity.State
        want  float64
    }{
        {connectivity.Ready, 2},
        {connectivity.Idle, 1},
        {connectivity.Connecting, 1},
        {connectivity.TransientFailure, 0},
        {connectivity.Shutdown, 0},
    }
    for _, tc := range cases {
        t.Run(tc.state.String(), func(t *testing.T) {
            _, gatherer := newTestMetrics(t, tc.state)
            mfs, err := gatherer.Gather()
            if err != nil {
                t.Fatalf("Gather: %v", err)
            }
            for _, mf := range mfs {
                if mf.GetName() == "nebu_grpc_status" {
                    got := mf.GetMetric()[0].GetGauge().GetValue()
                    if got != tc.want {
                        t.Errorf("nebu_grpc_status got %v, want %v", got, tc.want)
                    }
                    return
                }
            }
            t.Error("nebu_grpc_status metric not found")
        })
    }
}

func TestMiddleware_increments_counter_on_200(t *testing.T) {
    m, gatherer := newTestMetrics(t, connectivity.Ready)
    inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })
    req := httptest.NewRequest("GET", "/health", nil)
    rr := httptest.NewRecorder()
    m.Middleware(inner).ServeHTTP(rr, req)

    mfs, _ := gatherer.Gather()
    for _, mf := range mfs {
        if mf.GetName() == "nebu_http_requests_total" {
            labels := mf.GetMetric()[0].GetLabel()
            var method, code string
            for _, l := range labels {
                if l.GetName() == "method" {
                    method = l.GetValue()
                }
                if l.GetName() == "status_code" {
                    code = l.GetValue()
                }
            }
            if method != "GET" || code != "200" {
                t.Errorf("labels: method=%q status_code=%q; want GET/200", method, code)
            }
            if v := mf.GetMetric()[0].GetCounter().GetValue(); v != 1 {
                t.Errorf("counter value got %v, want 1", v)
            }
            return
        }
    }
    t.Error("nebu_http_requests_total metric not found")
}

func TestMiddleware_captures_503(t *testing.T) {
    m, gatherer := newTestMetrics(t, connectivity.Ready)
    inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusServiceUnavailable)
    })
    req := httptest.NewRequest("GET", "/ready", nil)
    rr := httptest.NewRecorder()
    m.Middleware(inner).ServeHTTP(rr, req)

    mfs, _ := gatherer.Gather()
    for _, mf := range mfs {
        if mf.GetName() == "nebu_http_requests_total" {
            labels := mf.GetMetric()[0].GetLabel()
            for _, l := range labels {
                if l.GetName() == "status_code" && l.GetValue() != "503" {
                    t.Errorf("status_code got %q, want 503", l.GetValue())
                }
            }
            return
        }
    }
    t.Error("nebu_http_requests_total metric not found")
}
```

### statusRecorder — WriteHeader Call Behaviour

The `statusRecorder.WriteHeader` is called explicitly by handlers that set non-200 codes. Handlers that call only `w.Write(...)` without `WriteHeader` never trigger `WriteHeader`, so the default `200` in `statusRecorder.code` is used. This matches `net/http/httptest.ResponseRecorder` behaviour — same pattern, compatible with existing tests.

### Architecture Compliance

- [Source: architecture.md#Health-Readiness-Endpoints] `GET :8080/metrics` → Prometheus — co-located on pubMux with `/health` and `/ready`
- [Source: architecture.md#Gateway-Status-Modell] GRÜN=2, GELB=1, ROT=0 for `nebu_grpc_status` gauge
- [Source: architecture.md#Planned-Directory-Structure] `gateway/internal/admin/metrics.go` ← FR44-45
- [Source: epics.md#Story-1.13] No auth required for `/metrics`; ops-only endpoint

### Project Structure Notes

- File: `gateway/internal/admin/metrics.go` — package `admin` (dir already exists with `.gitkeep`)
- Test: `gateway/internal/admin/metrics_test.go` — package `admin` (consistent with `health_test.go` which uses same package)
- `coreStateProvider` interface defined locally in `admin` package — `*coregrpc.Client` satisfies it via duck-typing, no cross-package import needed
- Do NOT put metrics in `gateway/internal/health/` — health is a separate concern. Metrics is in `admin` per architecture
- `promhttp.Handler()` (default gatherer) is used in main.go for production — includes GoCollector/ProcessCollector automatically

### Previous Story Intelligence (from 1-12 Elixir health)

- The Go pattern for injectable deps is a struct with function fields (see `health.Handler.checkDB`, `health.Handler.getMigVersion`) — follow the same injectable-interface approach for `coreStateProvider`
- Tests use `package health` (same package, not `health_test`) — follow same pattern: `package admin` in test file
- Inline test doubles (fakeCore with `State()` method) — same pattern used in `health_test.go:fakeCore`
- The `statusRecorder` pattern (embed `http.ResponseWriter`, override `WriteHeader`) is standard; `health_test.go` uses `httptest.NewRecorder()` for the same purpose
- The existing `pubMux` goroutine in `main.go` already wraps with `http.ListenAndServe(":8080", pubMux)` — change this to `metrics.Middleware(pubMux)` without touching the goroutine structure
- All 1-11 gateway tests must remain green (0 regressions) after this change

### References

- [Source: epics.md#Story-1.13] Full AC and BDD scenarios
- [Source: architecture.md#Health-Readiness-Endpoints] `GET :8080/metrics` Prometheus endpoint definition
- [Source: gateway/cmd/gateway/main.go:51-63] pubMux setup — where `/metrics` route and middleware wrap goes
- [Source: gateway/internal/health/health.go:15-17] `coreState` interface — mirrors the `coreStateProvider` to define in admin package
- [Source: gateway/internal/health/health_test.go:13-18] `fakeCore` inline test double pattern — reuse same approach
- [Source: gateway/go.mod] Current Go module: `github.com/nebu/nebu`, Go 1.26 — no existing prometheus dep

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

### Completion Notes List

- Created `gateway/internal/admin/metrics.go` with `coreStateProvider` interface, `Metrics` struct, `NewMetrics()` registering `nebu_http_requests_total` CounterVec and `nebu_grpc_status` GaugeFunc, and `Middleware()` with `statusRecorder`
- Created `gateway/internal/admin/metrics_test.go` with 7 tests covering all AC (#1–#4): handler format, all 5 gRPC states, middleware 200, middleware 503
- Updated `gateway/cmd/gateway/main.go`: added admin/prometheus imports, metrics init before pubMux, `/metrics` route with `promhttp.Handler()`, wrapped server with `metrics.Middleware()`
- Added `github.com/prometheus/client_golang v1.23.2` as direct dep in `gateway/go.mod`
- All tests pass: `make test-unit-go` → `ok github.com/nebu/nebu/internal/admin 0.003s`, 0 regressions

### File List

- `gateway/internal/admin/metrics.go` (new)
- `gateway/internal/admin/metrics_test.go` (new)
- `gateway/cmd/gateway/main.go` (modified)
- `gateway/go.mod` (modified — prometheus/client_golang v1.23.2 added as direct dep)
- `gateway/go.sum` (modified)

## Senior Developer Review (AI)

**Reviewer:** Phil (via claude-opus-4-6) on 2026-03-24
**Outcome:** Approved (1 LOW fixed)

### Findings
- **LOW (fixed):** Weak assertion in `TestMiddleware_captures_503` — negative check replaced with positive assertion verifying both `method` and `status_code` labels exist with expected values

### Verification
- All 6 Acceptance Criteria: IMPLEMENTED
- All 17 subtasks marked [x]: VERIFIED as actually done
- Git vs Story File List: 0 discrepancies
- Architecture compliance: confirmed (G12 Status-Modell, FR44-45, Health & Readiness Endpoints)
- Code quality: clean, idiomatic Go, proper test isolation with per-test registries

## Change Log

- 2026-03-24: Implemented Prometheus metrics endpoint for Go gateway — added nebu_http_requests_total counter, nebu_grpc_status gauge, HTTP middleware, and /metrics route (Story 1-13)
- 2026-03-24: Code review passed — fixed weak test assertion in TestMiddleware_captures_503 (1 LOW)
