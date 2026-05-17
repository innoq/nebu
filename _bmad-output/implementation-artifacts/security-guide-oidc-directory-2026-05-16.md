# Security Implementation Guide: OIDC Directory Endpoint Integration

**Story scope:** 14.2b — `gateway/internal/admin/oidc_directory.go`
**Prepared by:** Kassandra (Security Review Agent)
**Date:** 2026-05-16
**Status:** Pre-implementation — read before writing a single line of code

---

## What This Guide Covers

The OIDC Directory feature makes outbound HTTP calls from the Gateway to an admin-configured external URL. This creates a trust boundary inversion: the Gateway — a trusted system — executes HTTP requests to a URL controlled by whoever has admin access. Every security requirement below follows from that single fact.

---

## CRITICAL Requirements (block the commit if violated)

### CR-1: No plaintext HTTP

```go
// WRONG
if !strings.HasPrefix(cfg.OIDCDirectoryEndpoint, "https://") {
    log.Warn("endpoint is not HTTPS")  // warning is not enforcement
}

// CORRECT
func validateEndpoint(endpoint string) error {
    u, err := url.Parse(endpoint)
    if err != nil || u.Scheme != "https" {
        return fmt.Errorf("oidc_directory_endpoint must use HTTPS: %q", endpoint)
    }
    return nil
}
```

Validation MUST happen at:
1. `PATCH /api/v1/admin/config` — reject storage of non-HTTPS endpoint
2. Service initialization — refuse to start if stored endpoint is non-HTTPS
3. Each call to the directory — defensive check before dialing

Do not log the warning and proceed. Fail hard.

### CR-2: No redirect following

Go's default `http.Client` follows redirects. An admin could store an HTTP URL that the OIDC provider redirects to HTTPS, bypassing CR-1. Alternatively, a compromised OIDC provider could redirect to an internal metadata endpoint.

```go
client := &http.Client{
    Timeout: 10 * time.Second,
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        return http.ErrUseLastResponse  // never follow redirects
    },
}
```

### CR-3: Bearer token never appears in logs

The token used to authenticate against the OIDC directory MUST NOT appear in:
- `slog.Debug` / `slog.Info` / `slog.Warn` / `slog.Error` output
- HTTP trace logs (Go's `httptrace` package)
- Error messages returned to callers
- Panic stack traces (do not store in a struct field that gets `%+v`-printed)

```go
// WRONG — token in request dump
dump, _ := httputil.DumpRequestOut(req, false)
slog.Debug("oidc directory request", "dump", string(dump))

// CORRECT — log only method and redacted URL
slog.Debug("oidc directory call", "method", "GET", "host", req.URL.Host)
```

Pattern from Epic 12 MinIO incident: secrets in function arguments get written to `/proc/<pid>/cmdline`. Secrets in struct fields get printed by `%+v`. The token belongs in a `type secret string` wrapper or behind an accessor that returns `"[REDACTED]"` from `String()`.

### CR-4: Response size limit

An OIDC provider returning millions of users (or a malicious redirect returning an unbounded stream) can OOM the Gateway.

```go
const maxDirectoryResponseBytes = 10 * 1024 * 1024  // 10 MB hard cap

resp, err := client.Do(req)
if err != nil {
    return nil, err
}
defer resp.Body.Close()
body, err := io.ReadAll(io.LimitReader(resp.Body, maxDirectoryResponseBytes))
if err != nil {
    return nil, fmt.Errorf("reading oidc directory response: %w", err)
}
if len(body) == int(maxDirectoryResponseBytes) {
    slog.Warn("oidc directory response truncated at limit", "limit_bytes", maxDirectoryResponseBytes)
    // Return what was parsed, not an error — graceful degradation
}
```

### CR-5: Rate limit tied to authenticated session identity

The rate limit (5 req/s per admin session, per ADR-015) MUST be keyed on the **verified session ID** from the JWT, not on IP address or a request header. IP-keyed limits are bypassable via XFF spoofing (see Epic 12 pattern `XFF rate-limit spoofing`).

```go
// WRONG
sessionID := r.Header.Get("X-Session-Id")

// CORRECT
claims, _ := middleware.SessionFromContext(r.Context())
sessionID := claims.SessionID  // from validated JWT, not from request
```

---

## HIGH Requirements (block the commit if violated)

### HR-1: Admin-only access gate

`oidc_directory.go` functions MUST only be called from handler paths that have already passed the admin auth middleware. Verify that:

1. The search endpoint has the admin middleware in the router chain (not just in the handler)
2. There is no `// TODO: add auth` placeholder
3. Integration test: unauthenticated request to the search endpoint returns 401, not search results

OIDC directory enumeration leaks your entire user base. An unauthenticated endpoint is a full user enumeration vulnerability.

### HR-2: SSRF scope limitation

`oidc_directory_endpoint` is admin-configured. Document and enforce the trust boundary:

Option A (strict): Reject endpoints resolving to private IP ranges:

```go
func isPrivateIP(ip net.IP) bool {
    private := []net.IPNet{
        {IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
        {IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
        {IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)},
        {IP: net.ParseIP("127.0.0.0"), Mask: net.CIDRMask(8, 32)},
        {IP: net.ParseIP("169.254.0.0"), Mask: net.CIDRMask(16, 32)},  // link-local / cloud metadata
    }
    // ...
}
```

Option B (documented trust boundary): If blocking private IPs is out of scope for Epic 14, document explicitly in `oidc_directory.go`:

```go
// SECURITY: oidc_directory_endpoint is admin-configured and trusted.
// Private IP ranges are not blocked. This means an admin with
// malicious intent can use this endpoint to probe internal services.
// Mitigated by: admin access requires valid OIDC + admin group claim.
// Not mitigated against: compromised admin credentials.
```

Option A is preferred. Option B is acceptable for Epic 14 if Option A is tracked as a follow-up.

### HR-3: Claim values from OIDC response are untrusted strings

The JSON response from the OIDC directory contains claim values (display names, email addresses, usernames) from external parties. These must be treated as untrusted input:

1. **No eval, no exec, no template injection** — obvious, but: if claim values are inserted into Go templates using `{{.ClaimValue}}` (not `{{.ClaimValue | html}}`), that is a stored XSS in the admin UI.
2. **Matrix User ID computation from claim value** — the claim value must pass through the same `FormatUserIDFromClaims` sanitization as first-login. Do not use the raw claim value as a Matrix localpart.
3. **Length limits** — enforce a max length on claim values before storing in cache or passing to search results. A 1 MB display name is not valid.

---

## MEDIUM Requirements (must fix before epic-end review)

### MR-1: Cache keying

The 30-second cache MUST be keyed on:
- The endpoint URL (different endpoints → different cache entries)
- The bearer token (different tokens → different cache entries; or at least the token hash)

If the cache is a singleton `sync.Map` with a fixed key, rotating the bearer token will serve stale results from the previous token's fetch.

### MR-2: Timeout on outbound call

10 seconds (shown in CR-2 example) is a reasonable timeout. Make it configurable via `oidc_directory_timeout_seconds` if admin wants tighter control.

**What happens on timeout:** return empty list + `slog.Warn`. Do not propagate the error to the search endpoint — the behavior is "OIDC directory temporarily unavailable, returning DB-only results."

### MR-3: HTTP response status code handling

Non-200 responses from the OIDC provider should be handled explicitly:

| Status | Action |
|--------|--------|
| 200 | Parse response, cache result |
| 401 / 403 | Log warning "oidc directory auth failed — check token"; return empty list |
| 404 | Log warning "oidc directory endpoint not found"; return empty list |
| 429 | Log warning "oidc directory rate limited by provider"; return empty list (do not retry in this request) |
| 5xx | Log warning "oidc directory provider error"; return empty list |

Never panic on unexpected status. Never return the raw status code to the non-admin caller.

### MR-4: Concurrent cache refresh

If 10 admin sessions hit the cache simultaneously and the cache is expired, 10 outbound HTTP calls will fire at once. Use `golang.org/x/sync/singleflight` to collapse concurrent refreshes into one:

```go
var group singleflight.Group

func (s *OIDCDirectoryService) fetch(ctx context.Context) ([]ClaimMap, error) {
    v, err, _ := group.Do("oidc_directory", func() (interface{}, error) {
        return s.doFetch(ctx)
    })
    // ...
}
```

---

## Testing Checkpoints

These test cases MUST exist before the story is marked done:

| # | Test | Type | Must verify |
|---|------|------|-------------|
| T1 | Cache hit — no second HTTP call | Unit (httptest) | singleflight + cache TTL |
| T2 | Cache miss — HTTP call made | Unit (httptest) | actual outbound call on cold cache |
| T3 | Unreachable endpoint — empty list returned, no panic | Unit | graceful degradation |
| T4 | Non-HTTPS endpoint — validation error at config time | Unit | CR-1 enforcement |
| T5 | Rate limit enforcement — 6th request in 1s returns 429 | Unit | per-session rate limit |
| T6 | Unauthenticated search request — 401 | Integration | HR-1 |
| T7 | Response exceeds 10 MB — truncated gracefully | Unit | CR-4 |
| T8 | Bearer token absent from any log output | Unit + inspection | CR-3 |

---

## Anti-Patterns from Previous Epics (do not repeat)

| Pattern | Where it appeared | What to do instead |
|---------|-------------------|--------------------|
| Fail-open when OIDC verifier is nil | Epic 12 Media Gateway | Refuse to start if configuration is invalid |
| XFF-based rate limit key | Epic 12 Story 12.10 | Key on validated session ID from JWT |
| `%+v` printing structs containing tokens | Epic 12 MinIO | Use `type secret string` with masked `String()` |
| Unbounded `io.ReadAll` | Epic 12 Media thumbnail | `io.LimitReader` always |
| Redirect following to bypass HTTPS check | (pre-emptive) | `CheckRedirect: ErrUseLastResponse` |
