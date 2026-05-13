## Security Review — Story 12-16

**Diff scope:** New `media/internal/auth` package (JWT middleware), new `media/internal/config` package (config handler), CSP/CORP headers on download+thumbnail handlers, 5 new authenticated `/_matrix/client/v1/media/*` routes + 1 unauthenticated `/_matrix/media/v3/config` route in `media/cmd/media/main.go`.
**Date:** 2026-05-13

---

### Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `media/internal/download/handler.go:158-160` | Unvalidated `{fileName}` path segment used in `Content-Disposition` | Strip or validate `fileName` to safe characters before insertion into the header |
| 2 | LOW | `media/internal/download/handler.go:22-35` | `text/plain` in `safeInlineContentTypes` is served inline — enables stored text-based attacks | Move `text/plain` to attachment-only list or keep inline only under `X-Content-Type-Options: nosniff` (already present) |
| 3 | LOW | `media/internal/config/handler.go:28-32` | Config endpoint returns `{"m.upload.size": N}` with no `X-Content-Type-Options: nosniff` header | Add `X-Content-Type-Options: nosniff` for consistency; not exploitable on a JSON-only endpoint but preserves defence-in-depth |
| 4 | LOW | CSP directive `plugin-types application/pdf` (download+thumbnail handlers) | `plugin-types` is a deprecated CSP1 directive, ignored by modern browsers (including Firefox 84+, Chrome 70+); actual PDF plugin embedding is controlled by `object-src 'self'` instead | Remove `plugin-types application/pdf;` from both CSP headers — it provides no protection and may mislead auditors; `object-src 'self'` already in the policy accomplishes the intended constraint |

---

### Detail

**Finding #1 — Unvalidated `{fileName}` in Content-Disposition header** [LOW]

In `download/handler.go` lines 158–173, the `{fileName}` segment from the URL path is extracted via `r.PathValue("fileName")` and inserted verbatim (inside `%q` Go formatting) into the `Content-Disposition` header:

```go
cdName := r.PathValue("fileName")
if cdName == "" {
    cdName = mediaID
}
w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", cdName))
```

The `%q` verb from `fmt` double-quotes and Go-escapes the string (e.g. converts `\n` to `\\n`, quotes inner double-quotes), which prevents HTTP response splitting by preventing literal CRLF injection. **However**, Go's `%q` escaping does not produce RFC 5987-compliant `filename*=UTF-8''...` encoding. More importantly, an attacker-controlled `{fileName}` value containing semicolons (`;`) or backslash sequences could produce a `Content-Disposition` value that downstream parsers (e.g. older browser Content-Disposition parsers or log processors) interpret differently than intended — though exploitation is constrained by Go's own escaping.

This is classified **LOW** rather than MEDIUM because: (a) the route requires a valid Bearer token (auth middleware must pass first), (b) `%q` prevents raw CRLF injection, (c) no known exploit chain exists that bypasses both the JWT gate and the `%q` encoding. The recommendation is to apply the same `mediaIDPattern` validation (`^[A-Za-z0-9_\-]+$`) from `thumbnail/handler.go` to the `fileName` path value, or use `filepath.Base` + same allowlist to produce a clean filename.

**Finding #2 — `text/plain` served as inline Content-Type** [LOW]

`safeInlineContentTypes` in `download/handler.go` includes `"text/plain": true`. When a browser receives `Content-Type: text/plain` with `Content-Disposition: inline`, it renders the content in the browser tab. An attacker who uploaded a carefully crafted `text/plain` file containing HTML-like strings (e.g. `<script>`) might attempt to exploit browser quirks — but `X-Content-Type-Options: nosniff` (already set on line 147) prevents MIME sniffing so the browser will treat it as plain text and not HTML. The CSP `sandbox; default-src 'none'` headers added in this story further reduce risk.

The real concern is future: if `X-Content-Type-Options` is ever removed or this handler is extended to new outputs, the inline `text/plain` could become an XSS vector. The conservative fix is to move `text/plain` to the attachment-only path. This is advisory; no immediate exploit path exists given the defence-in-depth.

**Finding #3 — Missing `X-Content-Type-Options: nosniff` on config endpoint** [LOW]

`config/handler.go` returns JSON but does not set `X-Content-Type-Options: nosniff`. For a pure JSON API endpoint this is not exploitable — no browser will interpret JSON as a runnable script. The finding is logged purely for consistency: all other handlers in the media gateway set this header, and omitting it from the config endpoint creates an inconsistency that may be flagged in future automated scans.

**Finding #4 — Deprecated CSP `plugin-types` directive** [LOW]

Both `download/handler.go` (line 153) and `thumbnail/handler.go` (line 233) emit:

```
Content-Security-Policy: sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';
```

The `plugin-types` directive was removed from the CSP3 specification and is not implemented in any browser since approximately 2018–2020. Firefox 84 removed it; Chrome silently ignores it. The `object-src 'self'` directive already restricts what types of embedded objects (`<object>`, `<embed>`) are allowed. The `plugin-types` directive is therefore dead code in the security policy, provides no protection, and could mislead future security reviewers into believing PDF plugin loading is restricted by `plugin-types` when in fact it is only restricted by `object-src`.

Remediation: replace both CSP values with:
```
sandbox; default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; object-src 'self';
```

---

### Positive Observations (Not Findings)

The following security properties were specifically verified and found correct:

1. **Fail-closed nil verifier**: `auth.Middleware.Wrap` checks `m.verifier == nil` at the top of every request and returns 503 `M_UNAVAILABLE`. This is correct fail-closed behaviour. Matches the known OIDC fail-open pattern from MEMORY.md (Story 12.8) — this story does not regress it.

2. **Empty Bearer token → M_MISSING_TOKEN (not M_UNKNOWN_TOKEN)**: The `strings.TrimSpace(rawToken) == ""` check correctly distinguishes `Authorization: Bearer ` (empty token) from a well-formed token that fails verification. This prevents the empty-string token from reaching `VerifyToken` where a nil/empty string might cause unexpected verifier behaviour.

3. **Same OIDC verifier instance for upload and download**: `authMW := auth.New(uploadVerifier)` reuses the same `*upload.OIDCTokenVerifier` returned by `initOIDCVerifier`, which performs full go-oidc/v3 validation (signature, `exp`, `aud`, issuer). No separate/weaker verifier is wired for the new endpoints.

4. **OIDC audience / claim scope**: The auth middleware (`auth.TokenVerifier.VerifyToken`) does not extract or store the caller's identity — it is used solely for authentication, not audit. Config and download endpoints require a valid token but do not need an audit trail, which is consistent with the story's security note. The uploader identity is only stored on POST upload.

5. **Rate limiting on new endpoints**: All 5 new routes (including unauthenticated v3/config) are wrapped in `downloadRL`, which applies the download-tier rate limiter (100 req/s burst 20). No new route is unprotected.

6. **v3/config unauthenticated by design**: `GET /_matrix/media/v3/config` exposes only the max upload size (`m.upload.size`). This is a non-sensitive, read-only configuration value. Serving it without auth is correct per Matrix spec backward-compatibility requirements and per AC-1.

7. **No SQL injection surface**: No new DB queries are introduced in this story. The auth middleware performs no DB operations. The config handler performs no DB operations.

8. **No new path traversal surface**: The storage key for download/thumbnail continues to use `serverName + "/" + mediaID` from DB lookups, not from URL path values. The `{fileName}` value is only used in `Content-Disposition`, not as a filesystem or storage path.

9. **CSP `sandbox` directive**: The `sandbox` directive (no flags) is the strongest CSP sandbox value — it disables scripts, forms, plugins, popups, and cross-origin access within the loaded resource. This correctly addresses the Inline Content-Type XSS surface identified in MEMORY.md for Story 12.16.

10. **`Cross-Origin-Resource-Policy: cross-origin`**: Allows media to be loaded cross-origin by Element Web clients. This is the correct value per Matrix spec §Media Repository v1.4+.

---

### Summary

CRITICAL: 0
HIGH: 0
MEDIUM: 0
LOW: 4

**Verdict:** APPROVED

All four LOW findings are advisory hardening items with no immediate exploit path. The two most actionable are F-1 (add `mediaIDPattern` validation to `fileName`) and F-4 (remove dead `plugin-types` directive). These may be addressed in this story or tracked as follow-ups; neither blocks the commit.

The core security model of this story — fail-closed JWT middleware, rate-limited v1 endpoints, consistent OIDC verifier reuse, CSP sandbox on media downloads — is correctly implemented.
