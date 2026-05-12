# Story 3.14: Vue.js Self-hosted + SSE Live Metrics Widget

Status: done

## Story

As an operator,
I want live-updating metrics (message throughput, active sessions) on the Dashboard,
so that I can monitor Nebu in real time without page reloads during chat tests.

## Acceptance Criteria

1. `vue.esm-browser.prod.js` (Vue 3, ESM build) is placed in `gateway/internal/admin/static/vendor/` and embedded via `//go:embed`; served at `/admin/static/vendor/vue.esm-browser.prod.js` — no CDN
2. `GET /admin/sse/metrics` is an SSE endpoint (`Content-Type: text/event-stream`):
   - Sends an initial `event: metrics` with JSON payload `{"msg_per_sec": <float>, "active_sessions": <int>, "room_count": <int>}` immediately on connect
   - Sends updated payload every 5 seconds
   - Metrics are fetched from `gRPC CoreService.GetMetrics`; if gRPC fails (or method not yet implemented — see Dev Notes), sends `event: error` with `{"error": "core unavailable"}` and keeps connection open for retry
   - Sends `event: ping` every 30 seconds to keep the connection alive
3. The Dashboard template's `{{ define "scripts" }}` block loads the self-hosted Vue.js and a `metrics-widget.js` inline script
4. `metrics-widget.js` mounts a Vue 3 app on `#live-metrics`, connects to `/admin/sse/metrics` via `EventSource`, and renders:
   - `msg/s`: formatted number (e.g., `12.4 msg/s`)
   - `Active Sessions`: integer
   - `Rooms`: integer
   - On SSE error: show "Core unreachable" badge in amber
5. The `TopbarStatusIndicator` (C3) in the topbar (`#topbar-status`) is updated by the Vue app to reflect current Core health (GREEN / AMBER / RED) via reactive class replacement
6. An integration test (in `gateway/internal/admin/`) verifies `GET /admin/sse/metrics` returns `Content-Type: text/event-stream` and the first `event: metrics` line within 2 seconds

## Tasks / Subtasks

- [x] Task 1: Download and embed Vue 3 ESM browser build (AC: 1)
  - [x] 1.1 Download `vue.esm-browser.prod.js` from the Vue 3 release (see Dev Notes for exact URL and verification)
  - [x] 1.2 Place at `gateway/internal/admin/static/vendor/vue.esm-browser.prod.js`
  - [x] 1.3 Extend `static.go` to embed `static/vendor/` via `//go:embed` and add `ServeVendorFile` handler
  - [x] 1.4 Register `GET /admin/static/vendor/{filename}` in `main.go` alongside existing static asset routes

- [x] Task 2: Add `GetMetrics` stub to proto and gRPC client (AC: 2)
  - [x] 2.1 Add `GetMetrics` RPC to `proto/core.proto` with `GetMetricsRequest` (empty) and `GetMetricsResponse` (`msg_per_sec float`, `active_sessions int32`, `room_count int32`) — see Dev Notes for exact field types
  - [x] 2.2 Run `make proto` to regenerate `gateway/internal/grpc/pb/` stubs
  - [x] 2.3 Add `GetMetrics` stub method to `gateway/internal/grpc/client.go` (returns `nil, nil` like other Epic 4 stubs)

- [x] Task 3: Create `gateway/internal/admin/sse.go` — SSE metrics handler (AC: 2)
  - [x] 3.1 Define `MetricsReader` interface (consumer-defined): `GetMetrics(ctx context.Context) (msgPerSec float64, activeSessions int, roomCount int, err error)`
  - [x] 3.2 Implement `coreMetricsReader` that calls `grpcClient.GetMetrics(ctx, &pb.GetMetricsRequest{})` and maps fields — implemented as `coreMetricsAdapter` in `main.go` to avoid pb import in admin package (follows existing `CoreStateReader` pattern)
  - [x] 3.3 Implement `SSEMetricsHandler` struct: `{ core MetricsReader }`
  - [x] 3.4 Implement `NewSSEMetricsHandler(core MetricsReader) *SSEMetricsHandler`
  - [x] 3.5 Implement `Handler(w http.ResponseWriter, r *http.Request)`:
    - Set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`
    - Flush initial metrics immediately on connect
    - Loop: send metrics every 5 seconds, send `event: ping` every 30 seconds
    - On gRPC error: send `event: error\ndata: {"error":"core unavailable"}\n\n` and continue (do not close)
    - Use `r.Context().Done()` channel to detect client disconnect and exit loop cleanly

- [x] Task 4: Create `gateway/internal/admin/templates/metrics-widget.js` (inline script) (AC: 3, 4, 5)
  - [x] 4.1 Use Vue 3 Composition API with `createApp` + `ref` (no `<script setup>` — this is a plain JS file)
  - [x] 4.2 Root component template (inline string) renders: three metric stat blocks (`msg_per_sec`, `active_sessions`, `room_count`) and an error badge (`"Core unreachable"`) that appears when `hasError` is true
  - [x] 4.3 On mount: open `new EventSource('/admin/sse/metrics')`, register `message` + `metrics` event handlers, register `error` handler
  - [x] 4.4 On `event: metrics`: parse JSON, update reactive refs, clear error state
  - [x] 4.5 On `event: error`: set `hasError = true`, show amber error badge
  - [x] 4.6 On `EventSource` `onerror` (network-level): set `hasError = true`
  - [x] 4.7 Unmount cleanup: call `eventSource.close()` in `onUnmounted`
  - [x] 4.8 Update topbar `#topbar-status` DOM element: when `hasError` → apply error classes; when connected → apply success classes (use the same DaisyUI classes as `base.html`: `text-error`/`text-success`/`text-warning`, `bg-error`/`bg-success`)

- [x] Task 5: Update `gateway/internal/admin/templates/dashboard.html` `{{ define "scripts" }}` block (AC: 3)
  - [x] 5.1 Replace `{{ define "scripts" }}{{ end }}` with a block that:
    - Loads `/admin/static/vendor/vue.esm-browser.prod.js` via `<script type="module" src="...">`
    - Loads `/admin/static/metrics-widget.js` via `<script type="module" src="...">`

- [x] Task 6: Add `ServeVendorFile` and `ServeMetricsWidgetJS` static handlers in `static.go` (AC: 1, 3)
  - [x] 6.1 Extend the `//go:embed` directive in `static.go` to include `static/vendor`
  - [x] 6.2 Add `ServeVendorFile(w http.ResponseWriter, r *http.Request)` following the same pattern as `ServeFontFile`: path-safe, `Content-Type: application/javascript`, `Cache-Control: public, max-age=31536000, immutable`
  - [x] 6.3 Store `metrics-widget.js` inside `gateway/internal/admin/templates/` (already covered by `adminFS //go:embed templates`); add `ServeMetricsWidgetJS` handler that reads from `adminFS`

- [x] Task 7: Register new routes in `main.go` (AC: 1, 2)
  - [x] 7.1 Register `GET /admin/static/vendor/{filename}` → `admin.ServeVendorFile`
  - [x] 7.2 Register `GET /admin/static/metrics-widget.js` → `admin.ServeMetricsWidgetJS`
  - [x] 7.3 Create `sseMetricsHandler := admin.NewSSEMetricsHandler(coreMetricsAdapter)` where `coreMetricsAdapter` wraps `coreClient`
  - [x] 7.4 Register `GET /admin/sse/metrics` → `sessionGuard(http.HandlerFunc(sseMetricsHandler.Handler))`

- [x] Task 8: Write unit and integration tests (AC: 6)
  - [x] 8.1 Unit test `TestSSEMetricsHandler_ContentType`: mock `MetricsReader` returns (0, 0, 0, nil) → assert `Content-Type: text/event-stream`
  - [x] 8.2 Unit test `TestSSEMetricsHandler_InitialEvent`: mock returns valid metrics → assert response body starts with `event: metrics` within first 512 bytes
  - [x] 8.3 Unit test `TestSSEMetricsHandler_GRPCError`: mock returns error → assert response body contains `event: error` and `"core unavailable"`
  - [x] 8.4 Unit test `TestSSEMetricsHandler_NoCacheHeaders`: assert `Cache-Control: no-cache` header present

- [x] Task 9: Run `make test-unit-go` and confirm zero regressions

## Dev Notes

### CRITICAL: `GetMetrics` Does NOT Exist in `core.proto` Yet

The current `proto/core.proto` has no `GetMetrics` RPC. This story adds it as a new RPC. Because the Elixir Core does not implement it yet (Epic 4), the Go gRPC client stub returns `nil, nil` — this means `GetMetrics` will always fail (nil response). The `coreMetricsReader` must handle `nil` response gracefully:

```go
resp, err := c.grpcClient.GetMetrics(ctx, &pb.GetMetricsRequest{})
if err != nil || resp == nil {
    return 0, 0, 0, fmt.Errorf("core unavailable")
}
return float64(resp.MsgPerSec), int(resp.ActiveSessions), int(resp.RoomCount), nil
```

This means the SSE endpoint will always emit `event: error` with `{"error":"core unavailable"}` until Epic 4 implements it. This is the EXPECTED behavior for Story 3.14.

### CRITICAL: Proto Field Types for `GetMetricsResponse`

Add to `proto/core.proto`:

```protobuf
// GetMetrics — Admin Dashboard live metrics (Epic 3 skeleton; full implementation Epic 4)
rpc GetMetrics(GetMetricsRequest) returns (GetMetricsResponse);

message GetMetricsRequest {}

message GetMetricsResponse {
  float  msg_per_sec      = 1;  // messages per second (rolling 60s window)
  int32  active_sessions  = 2;  // currently active user sessions
  int32  room_count       = 3;  // total number of active rooms
}
```

After editing `core.proto`, run `make proto` to regenerate Go and Elixir stubs. The generated Go struct will be `pb.GetMetricsResponse` with fields `MsgPerSec float32`, `ActiveSessions int32`, `RoomCount int32`.

### CRITICAL: Vue 3 ESM Browser Build — Self-Hosting

Download URL for Vue 3.x ESM browser production build:
```
https://unpkg.com/vue@3/dist/vue.esm-browser.prod.js
```
Or use the specific release:
```
https://cdn.jsdelivr.net/npm/vue@3.4.21/dist/vue.esm-browser.prod.js
```

The correct filename is `vue.esm-browser.prod.js`. This is the **ESM** build (not UMD), suitable for use with `<script type="module">`. It exports `createApp`, `ref`, `reactive`, `computed`, `onMounted`, `onUnmounted`, etc. directly.

Download this file manually before implementing and place at:
```
gateway/internal/admin/static/vendor/vue.esm-browser.prod.js
```

The file is approximately 140KB (minified). Do NOT use `vue.global.prod.js` (that's the UMD build) or `vue.esm-bundler.js` (requires bundler).

### CRITICAL: `metrics-widget.js` — Vue 3 ESM Import Pattern

Because this is an ESM module (served as `<script type="module">`), use ES module import syntax:

```javascript
import { createApp, ref, onMounted, onUnmounted } from '/admin/static/vendor/vue.esm-browser.prod.js';

const MetricsWidget = {
  setup() {
    const msgPerSec = ref(0);
    const activeSessions = ref(0);
    const roomCount = ref(0);
    const hasError = ref(false);
    let es = null;

    onMounted(() => {
      es = new EventSource('/admin/sse/metrics');

      es.addEventListener('metrics', (e) => {
        const data = JSON.parse(e.data);
        msgPerSec.value = data.msg_per_sec;
        activeSessions.value = data.active_sessions;
        roomCount.value = data.room_count;
        hasError.value = false;
        updateTopbar('ok');
      });

      es.addEventListener('error', (e) => {
        // SSE application-level error event from server
        hasError.value = true;
        updateTopbar('error');
      });

      es.onerror = () => {
        // Network-level EventSource error
        hasError.value = true;
        updateTopbar('error');
      };
    });

    onUnmounted(() => {
      if (es) es.close();
    });

    return { msgPerSec, activeSessions, roomCount, hasError };
  },
  template: `
    <div class="card-body py-6">
      <div v-if="hasError" role="alert" aria-live="assertive"
           class="badge badge-warning gap-2">
        Core unreachable
      </div>
      <div v-else class="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div>
          <dt class="text-sm font-medium text-base-content/60">msg/s</dt>
          <dd class="mt-1 text-2xl font-mono">{{ msgPerSec.toFixed(1) }} msg/s</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-base-content/60">Active Sessions</dt>
          <dd class="mt-1 text-2xl font-mono">{{ activeSessions }}</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-base-content/60">Rooms</dt>
          <dd class="mt-1 text-2xl font-mono">{{ roomCount }}</dd>
        </div>
      </div>
    </div>
  `
};

createApp(MetricsWidget).mount('#live-metrics');
```

**TopbarStatus Update function** (separate from Vue reactivity, directly manipulates DOM):
```javascript
function updateTopbar(status) {
  const el = document.getElementById('topbar-status');
  if (!el) return;
  const dot = el.querySelector('[aria-hidden="true"]');

  // Remove all state classes
  el.classList.remove('text-success', 'text-warning', 'text-error');
  if (dot) dot.classList.remove('bg-success', 'bg-warning', 'bg-error');

  if (status === 'ok') {
    el.classList.add('text-success');
    if (dot) dot.classList.add('bg-success');
    el.setAttribute('aria-label', 'System status: OK');
    // Remove text node (ok = dot only per UX-DR6)
  } else if (status === 'error') {
    el.classList.add('text-error');
    if (dot) dot.classList.add('bg-error');
    el.setAttribute('aria-label', 'System status: Core unreachable');
  }
}
```

### CRITICAL: Tailwind CSS Purge-Safe Classes in `metrics-widget.js`

The `admin.css` is built by Tailwind CSS Standalone CLI which scans for class strings. Because `metrics-widget.js` is in `gateway/internal/admin/templates/`, it IS scanned by Tailwind (configured via `tailwind.config.js` — check the `content` array). Verify that all classes used in the template string are either:
1. Already present in the CSS from other templates (most DaisyUI/Tailwind utility classes are safe), OR
2. Added as safelist entries in `tailwind.config.js`

Key classes that might be missing if not used elsewhere:
- `badge`, `badge-warning` — DaisyUI component, likely safe
- `text-2xl` — Tailwind utility, likely safe

**IMPORTANT**: After adding `metrics-widget.js`, run `make build-gateway` to confirm Tailwind picks up new classes and the CSS is regenerated correctly before testing.

### CRITICAL: SSE Handler Implementation — Avoid Goroutine Leaks

The SSE handler MUST use `r.Context()` for lifecycle management:

```go
func (h *SSEMetricsHandler) Handler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("X-Accel-Buffering", "no")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    // Send initial metrics immediately
    h.sendMetrics(w, flusher, r.Context())

    ticker := time.NewTicker(5 * time.Second)
    pingTicker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    defer pingTicker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            h.sendMetrics(w, flusher, r.Context())
        case <-pingTicker.C:
            fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
            flusher.Flush()
        }
    }
}

func (h *SSEMetricsHandler) sendMetrics(w http.ResponseWriter, flusher http.Flusher, ctx context.Context) {
    msgPS, sessions, rooms, err := h.core.GetMetrics(ctx)
    if err != nil {
        fmt.Fprintf(w, "event: error\ndata: {\"error\":\"core unavailable\"}\n\n")
        flusher.Flush()
        return
    }
    payload := fmt.Sprintf(`{"msg_per_sec":%.1f,"active_sessions":%d,"room_count":%d}`, msgPS, sessions, rooms)
    fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", payload)
    flusher.Flush()
}
```

### CRITICAL: `embed.FS` Extension — Do NOT Break Existing Pattern

The current `static.go` uses:
```go
//go:embed static/admin.css static/fonts
var staticFS embed.FS
```

To add vendor files, change to:
```go
//go:embed static/admin.css static/fonts static/vendor
var staticFS embed.FS
```

The `//go:embed` directive in `handler.go` already covers `templates/` (including `metrics-widget.js`):
```go
//go:embed templates
var adminFS embed.FS
```

So `metrics-widget.js` should be served from `adminFS` (already embedded) — add `ServeMetricsWidgetJS` to `static.go` reading from `adminFS` but exposing it as a public function. OR alternatively serve it directly from the `templates/` embed. See file structure table below.

### CRITICAL: `#live-metrics` Div Already Exists in `dashboard.html`

Story 3.13 already created `<div id="live-metrics">` with a skeleton placeholder in `dashboard.html`. Story 3.14 must:
1. Keep the `id="live-metrics"` attribute exactly as is
2. Vue's `createApp(...).mount('#live-metrics')` will REPLACE the inner content of that div with the Vue component — the skeleton/spinner text will be replaced once Vue loads and mounts
3. Do NOT remove the skeleton content from `dashboard.html` — it serves as the SSR no-JS fallback and appears before Vue hydrates

### CRITICAL: Dashboard `{{ define "scripts" }}` Update

In `dashboard.html`, replace:
```html
{{ define "scripts" }}{{ end }}
```
with:
```html
{{ define "scripts" }}
<script type="module" src="/admin/static/vendor/vue.esm-browser.prod.js"></script>
<script type="module" src="/admin/static/metrics-widget.js"></script>
{{ end }}
```

**IMPORTANT**: Use `type="module"` for both scripts. The `metrics-widget.js` imports from the vendor file using a bare path — this only works in module context. Without `type="module"`, the `import` statement will throw a `SyntaxError`.

**ALSO IMPORTANT**: The `<script type="module">` for `metrics-widget.js` must come AFTER the vendor script tag (or rely on the ESM import inside the file — the explicit import line inside `metrics-widget.js` makes the ordering irrelevant for browsers that support modules, but being explicit is safer).

### File Structure

| File | Action |
|------|--------|
| `gateway/internal/admin/static/vendor/vue.esm-browser.prod.js` | CREATE — downloaded Vue 3 ESM build (binary asset) |
| `gateway/internal/admin/static.go` | MODIFY — add `static/vendor` to embed, add `ServeVendorFile` handler |
| `proto/core.proto` | MODIFY — add `GetMetrics` RPC + messages |
| `gateway/internal/grpc/pb/` | REGENERATE — run `make proto` |
| `gateway/internal/grpc/client.go` | MODIFY — add `GetMetrics` stub |
| `gateway/internal/admin/sse.go` | CREATE — `SSEMetricsHandler`, `MetricsReader` interface |
| `gateway/internal/admin/sse_test.go` | CREATE — 4 unit tests |
| `gateway/internal/admin/templates/metrics-widget.js` | CREATE — Vue 3 component, SSE client, topbar updater |
| `gateway/internal/admin/templates/dashboard.html` | MODIFY — update `{{ define "scripts" }}` block |
| `gateway/cmd/gateway/main.go` | MODIFY — register 3 new routes + SSE handler |

**Do NOT modify:**
- `gateway/internal/admin/handler.go` — no changes needed; `adminFS //go:embed templates` already picks up `metrics-widget.js`
- `gateway/internal/admin/dashboard.go` — no changes needed; SSE is a separate handler
- `gateway/internal/admin/page_data.go` — no changes needed; Vue updates topbar via DOM
- `gateway/internal/admin/templates/layouts/base.html` — no changes needed; Vue manipulates `#topbar-status` via DOM directly

### SSE Wire Format Reference

The SSE protocol requires each event to be separated by a blank line. The Go handler must produce EXACT wire format:
```
event: metrics\n
data: {"msg_per_sec":0.0,"active_sessions":0,"room_count":0}\n
\n
```
In Go: `fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", payload)`

For ping: `fmt.Fprintf(w, "event: ping\ndata: {}\n\n")`

For error: `fmt.Fprintf(w, "event: error\ndata: {\"error\":\"core unavailable\"}\n\n")`

The browser's `EventSource` fires the named event listener based on the `event:` field. Default unnamed events fire the `message` listener. Always use named events (`event: metrics`, `event: error`, `event: ping`) for this handler.

### Test Pattern for SSE Handler

The SSE handler is long-running (infinite loop). For unit tests, use a context with cancellation to terminate the handler after the initial event:

```go
func TestSSEMetricsHandler_InitialEvent(t *testing.T) {
    core := &fakeMetricsReader{msgPerSec: 1.5, activeSessions: 3, roomCount: 7}
    h := NewSSEMetricsHandler(core)

    // Use a context that cancels after a short read
    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    r := httptest.NewRequest("GET", "/admin/sse/metrics", nil).WithContext(ctx)
    w := httptest.NewRecorder()
    h.Handler(w, r)

    body := w.Body.String()
    if !strings.Contains(body, "event: metrics") {
        t.Errorf("expected 'event: metrics' in body, got:\n%s", body)
    }
    if !strings.Contains(body, `"msg_per_sec"`) {
        t.Errorf("expected msg_per_sec in body, got:\n%s", body)
    }
}
```

**Note**: `httptest.ResponseRecorder` does NOT implement `http.Flusher`. The `SSEMetricsHandler.Handler` must check for Flusher and gracefully handle the missing interface. In tests, either:
1. Perform the Flusher check and fall back to not flushing (still writes to `w`), OR
2. Use a custom `flusherRecorder` in tests:
```go
type flusherRecorder struct {
    *httptest.ResponseRecorder
}
func (f *flusherRecorder) Flush() {} // no-op for tests
```

Use option 2 for test correctness. The handler's Flusher check should be a warning, not a hard error for the SSE flow (or make Flusher required and use `flusherRecorder` in all tests).

### UX Requirements (UX-DR4, UX-DR6, UX-DR23)

From the UX spec:
- **UX-DR4 (C1 StatusCard)**: `role="status"`, `aria-live="polite"` — the Vue component renders inside `#live-metrics` which already has these ARIA attributes from `dashboard.html`. Do NOT add duplicate ARIA attributes in the Vue template.
- **UX-DR6 (C3 TopbarStatusIndicator)**: SSE-driven; Vue updates the topbar when status changes. For `ok` state: only green dot (no text per UX spec). For `error`: dot + text "Core unreachable".
- **UX-DR23 (Sentinel Dashboard)**: Live metrics are SECONDARY (below the fold); the Vue widget hydrates the existing `#live-metrics` placeholder that already has the skeleton/spinner as fallback.
- **UX-DR25 (Reduced-Motion)**: The `animate-pulse` skeleton in `dashboard.html` already respects `prefers-reduced-motion: reduce`. Vue component does not add any animations.

### Relationship to Adjacent Stories

- **Story 3.13 (done)**: Provides `<div id="live-metrics">` placeholder, `DashboardPageData` with `TopbarStatus`/`TopbarLabel` (used server-side on initial render), `base.html` with `#topbar-status` span structure. Story 3.14 EXTENDS this via client-side Vue.
- **Story 3.15 (next)**: Gherkin test scenario "Dashboard accessible after authentication" asserts body contains "Dashboard" and a StatusCard for "Gateway" with class `"green"`. This test runs at the HTTP level (Godog/net/http), not browser level — the SSE/Vue behavior is out of scope for that Gherkin test.
- **Epic 4 (future)**: When `GetMetrics` is implemented in Elixir Core, the stub in `client.go` will be replaced with a real gRPC call. No changes to the SSE handler or Vue widget needed.

### Proto Regeneration

After editing `proto/core.proto`, run:
```bash
make proto
```
This runs `buf generate` inside the build container. The generated files appear in `gateway/internal/grpc/pb/core.pb.go` and `gateway/internal/grpc/pb/core_grpc.pb.go`. The Elixir stub generation is also triggered but the Elixir core will not implement `GetMetrics` yet — this is fine.

### Anti-Patterns to Avoid

- **DO NOT** use a CDN for Vue.js — the epic explicitly requires self-hosting (`no CDN` per AC 1). The file must be in `static/vendor/` and served from the embedded FS.
- **DO NOT** use `vue.global.prod.js` — that's the UMD/IIFE build which adds `Vue` to `window`. Use `vue.esm-browser.prod.js` with `<script type="module">`.
- **DO NOT** use `vue.esm-bundler.js` — that requires a bundler (Vite/webpack) to resolve `@vue/runtime-core` etc.
- **DO NOT** create a Vue SFC (`.vue` file) — no bundler in this project. Use inline template string in `metrics-widget.js`.
- **DO NOT** forget `type="module"` on the `<script>` tags — ES module `import` syntax requires it, otherwise browser throws SyntaxError.
- **DO NOT** call `w.WriteHeader()` manually before writing SSE data — setting headers before the first `Fprintf` is sufficient.
- **DO NOT** close the SSE connection on gRPC error — keep it open and send `event: error` so the client can display the error state and retry on reconnect.
- **DO NOT** use Go's `http.Flusher` type assertion as a hard fatal — return 500 if no flusher (as shown in sample), but ensure tests use `flusherRecorder`.
- **DO NOT** duplicate the `//go:embed templates` directive in `handler.go` — it already covers `templates/metrics-widget.js`.
- **DO NOT** add `metrics-widget.js` to `staticFS` — it's a template asset (served from `adminFS`), not a static asset. It does NOT need `Cache-Control: immutable` since it may change between releases.
- **DO NOT** interpolate dynamic class names in Vue template strings that Tailwind can't statically scan (e.g., `:class="'text-' + status"`) — always use full class names in conditionals.

### Previous Story Learnings (from Story 3.13)

- `w.Header().Set(...)` MUST be called BEFORE any `Fprintf` or `Write` to the response. In SSE handler, set all headers at the top.
- `path.Base(filename)` prevents directory traversal — use this same pattern in `ServeVendorFile`.
- The `pageTmpls` key is `path.Base(file)` without extension — irrelevant for `metrics-widget.js` (served directly, not via template engine), but ensure the `.js` file doesn't accidentally get parsed as a template.
- Tailwind CSS class purging: dynamic class interpolation (e.g., `class="text-{{ .Status }}"`) BREAKS purging. Story 3.13's code review caught this. Apply same discipline to Vue template class bindings — always use full static class strings with `v-if`/`v-else` conditionals.
- All existing `admin` package tests use `package admin` (NOT `package admin_test`) — follow same convention for `sse_test.go`.

### Architecture Constraints

Per `architecture.md`:
- "Admin UI ausschließlich via `embed.FS` ausliefern — kein Filesystem-Zugriff zur Laufzeit" — both `vue.esm-browser.prod.js` and `metrics-widget.js` MUST be served from embedded FS, never from the filesystem at runtime.
- ADR-009 (OpenAPI Spec-First): The SSE endpoint `/admin/sse/metrics` is NOT a REST/Matrix API endpoint — do NOT add it to `openapi.yaml`.
- The SSE endpoint is an admin endpoint — it MUST be behind `sessionGuard` (same as `/admin/dashboard`).

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

None.

### Completion Notes List

- Task 1: Downloaded Vue 3.4.21 ESM browser build (~150KB) from jsDelivr CDN. Placed at `gateway/internal/admin/static/vendor/vue.esm-browser.prod.js`. Extended `//go:embed` in `static.go` to include `static/vendor`.
- Task 2: Added `GetMetrics` RPC + `GetMetricsRequest`/`GetMetricsResponse` messages to `proto/core.proto`. Ran `make proto` — Elixir and Go stubs regenerated successfully. Added `GetMetrics` stub to `gateway/internal/grpc/client.go` (returns `nil, nil` — Epic 4 placeholder).
- Task 3: Created `gateway/internal/admin/sse.go` with `MetricsReader` interface, `SSEMetricsHandler` struct, `NewSSEMetricsHandler` constructor, and `Handler` method with ticker-based 5s/30s intervals. The `coreMetricsReader` adapter was implemented in `main.go` as `coreMetricsAdapter` (following the existing `CoreStateReader` pattern in `dashboard.go` to avoid importing `pb` types in the `admin` package). Handler returns HTTP 500 if `http.Flusher` unavailable; uses `r.Context().Done()` for clean shutdown.
- Task 4: Created `gateway/internal/admin/templates/metrics-widget.js` with Vue 3 Composition API (`createApp`, `ref`, `onMounted`, `onUnmounted`). ESM import from self-hosted `/admin/static/vendor/vue.esm-browser.prod.js`. Handles `event: metrics`, `event: error`, and network-level `onerror`. `updateTopbar()` function directly manipulates `#topbar-status` DOM element. All template class names are full static strings (Tailwind purge-safe).
- Task 5: Updated `dashboard.html` `{{ define "scripts" }}` block to load both `vue.esm-browser.prod.js` and `metrics-widget.js` via `<script type="module">`.
- Task 6: Added `ServeVendorFile` and `ServeMetricsWidgetJS` handlers to `static.go`. Vendor files get `Cache-Control: public, max-age=31536000, immutable`. `metrics-widget.js` gets `Cache-Control: no-cache` (served from `adminFS` templates embed, may change between releases).
- Task 7: Registered all three new routes in `main.go`. SSE endpoint behind `sessionGuard`. `coreMetricsAdapter` struct defined in `main.go` as the bridge between `*coregrpc.Client` and `admin.MetricsReader`.
- Task 8: Created `gateway/internal/admin/sse_test.go` with `flusherRecorder` test helper (wraps `httptest.ResponseRecorder` to implement `http.Flusher`), `fakeMetricsReader`, and 4 unit tests: ContentType, InitialEvent, GRPCError, NoCacheHeaders. Context with 200ms timeout terminates the infinite loop cleanly.
- Task 9: `make test-unit-go` — all 11 packages pass, zero regressions. `make build-gateway` — Docker build succeeds.

### File List

- `gateway/internal/admin/static/vendor/vue.esm-browser.prod.js` — CREATED (Vue 3.4.21 ESM browser build, ~150KB)
- `gateway/internal/admin/static.go` — MODIFIED (added `static/vendor` to embed, added `ServeVendorFile` and `ServeMetricsWidgetJS` handlers)
- `proto/core.proto` — MODIFIED (added `GetMetrics` RPC, `GetMetricsRequest`, `GetMetricsResponse`)
- `gateway/internal/grpc/pb/core.pb.go` — REGENERATED
- `gateway/internal/grpc/pb/core_grpc.pb.go` — REGENERATED
- `core/apps/event_dispatcher/lib/pb/core.pb.ex` — REGENERATED (Elixir stub)
- `gateway/internal/grpc/client.go` — MODIFIED (added `GetMetrics` stub)
- `gateway/internal/admin/sse.go` — CREATED (`MetricsReader` interface, `SSEMetricsHandler`)
- `gateway/internal/admin/sse_test.go` — CREATED (4 unit tests)
- `gateway/internal/admin/templates/metrics-widget.js` — CREATED (Vue 3 SSE widget)
- `gateway/internal/admin/templates/dashboard.html` — MODIFIED (`{{ define "scripts" }}` block)
- `gateway/cmd/gateway/main.go` — MODIFIED (added `coreMetricsAdapter`, new routes)

### Review Findings

- [x] [Review][Patch][MINOR] Go convention: `ctx context.Context` must be first parameter in `sendMetrics` — reordered from `(w, flusher, ctx)` to `(ctx, w, flusher)` [sse.go:63] — FIXED
- [x] [Review][Patch][MINOR] HTML5 validity: `<dt>`/`<dd>` elements wrapped in `<div>` instead of required `<dl>` parent — changed outer `<div v-else>` to `<dl v-else>` [metrics-widget.js:68] — FIXED
- [x] [Review][Dismiss] Redundant `<script type="module">` tag for Vue ESM in dashboard.html — ESM loader deduplicates automatically, no functional impact
- [x] [Review][Dismiss] Proto `float` (32-bit) vs Go `float64` in MetricsReader interface — upcast is lossless, no precision loss
- [x] [Review][Dismiss] Vendor/widget JS routes unauthenticated — consistent with existing static asset pattern (CSS/fonts), no secrets exposed

## Change Log

- 2026-04-01: Story created — ready-for-dev
- 2026-04-01: Story implemented — all tasks complete, tests pass, status set to review
- 2026-04-01: Code review passed — 2 MINOR fixes applied (ctx parameter order, HTML5 dl wrapper), 3 dismissed
