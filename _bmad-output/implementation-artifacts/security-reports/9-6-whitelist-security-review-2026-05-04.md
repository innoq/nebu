---
story_id: "9-6"
review_type: security (Kassandra Gate 1)
date: 2026-05-04
classification: CLEAN
severity_counts:
  critical: 0
  high: 0
  medium: 0
  low: 0
  info: 1
security_review: optional
---

# Story 9-6 — Security Review (Kassandra)

**Story:** State Event Type Whitelist — Gateway Middleware
**Scope:** `gateway/internal/matrix/state_event_types.go` and the new whitelist guard in
`gateway/internal/matrix/rooms.go::PutSetRoomState`.
**Classification:** **CLEAN** (no CRITICAL / HIGH / MEDIUM / LOW findings).

---

## 1. Threat model focus areas

The story introduces a string-based allow-list applied to a user-controlled URL path
segment (`{eventType}`). The interesting attack surface is therefore narrow: bypass
of the whitelist via encoding tricks, reflection of attacker-controlled input in
the error response, and resource amplification via overlong path values.

The four explicit checks requested by the reviewer:

1. URL-encoding bypass (`%2e%72%6f%6f%6d → .room`).
2. `r.PathValue("eventType")` decoded vs raw.
3. Safe reflection of `eventType` in error response (XSS).
4. DoS risk from very long `eventType` strings.

All four are addressed below.

---

## 2. Findings

### 2.1 URL-encoding bypass — N/A (no vulnerability)

`http.ServeMux` (Go 1.22+) URL-decodes path segments **before** invoking the handler
and before populating `r.PathValue`. Documented in `net/http`:
*"PathValue returns the value for the named path wildcard in the [ServeMux] pattern
that matched the request. It returns the empty string if the request was not matched
against a pattern or there is no such wildcard in the pattern."* — and per
`(*Request).pathValue` the value is the **decoded** segment.

**Consequence:** A request path `…/state/%6d.room.name` (where `%6d` = `m`) yields
`eventType == "m.room.name"`, which IS in the whitelist — that is the *intended*
behaviour (encoded representation of a legitimate type). There is no asymmetric
case where a non-whitelisted type can be encoded to look like a whitelisted one,
because the whitelist is exact-string-match on the decoded value.

The reverse — encoding a non-whitelisted type to *bypass* the check — is impossible
for the same reason: the lookup is performed on the canonical decoded form.

Path-traversal-style payloads (`m.room.name/../evil`) cannot reach the handler
either: the mux pattern `…/state/{eventType}/{stateKey}` bounds `{eventType}` to a
single path segment delimited by `/`. Embedded slashes are interpreted as segment
separators by the mux, not as part of the `eventType` value.

**Verdict:** No bypass.

### 2.2 `r.PathValue` decoded vs raw — confirmed decoded

`r.PathValue("eventType")` returns the decoded segment. The whitelist comparison
operates on the canonical form. No additional normalisation (Unicode NFC, case
folding, etc.) is applied — but the whitelist consists of ASCII-only Matrix-spec
strings, so a Unicode-confusable attack (e.g. Cyrillic `m`) would simply not match
the whitelist and would be rejected with 400 M_BAD_JSON. This is the safe failure
mode.

**Verdict:** Correct.

### 2.3 XSS via echoed `eventType` in error response — N/A

The error response is produced by `writeMatrixError` (in `login.go:55`):

```go
func writeMatrixError(w http.ResponseWriter, status int, errcode, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}
```

`json.Encoder.Encode` HTML-escapes `<`, `>`, `&` to `<`, `>`, `&` by
default (Go stdlib `encoding/json` SetEscapeHTML is true by default). The
`Content-Type` is `application/json`; browsers do not render JSON as HTML even when
sniffing, and modern Matrix clients consume this through `fetch`/`XHR` and never
inject the response body into the DOM unescaped.

Even if a client did naively inject the message, the JSON encoder escaping prevents
`<script>` from surviving the trip. There is no template, no `text/html`
rendering, and no log sink that interprets the value as code.

**Verdict:** No XSS vector.

### 2.4 DoS via overlong `eventType` — bounded

The HTTP server is configured with `MaxHeaderBytes: 16 * 1024` (16 KiB,
`gateway/cmd/gateway/main.go:1197`). The full HTTP request line — including the
URL path — is part of the header budget, so `eventType` cannot exceed roughly
16 KiB minus the rest of the request line and headers (~15 KiB worst case).

The whitelist lookup is `map[string]bool[eventType]` — Go's hash-map lookup is
O(len(key)) and well under a microsecond at 15 KiB. The error message
construction uses string concatenation `"unknown state event type: " + eventType`,
allocating at most ~15 KiB per rejected request.

For comparison, the per-IP rate limiter (Story 5-21) caps requests well below the
threshold needed to amplify this allocation into a meaningful DoS, and the body
size limit (`bodyLimit1MiB`) caps the much larger body component. There is no
pathological algorithmic complexity (e.g., quadratic regex) along this path.

**Verdict:** Bounded; no DoS amplification.

---

## 3. Defence-in-depth observations (info, no action required)

### INFO-1 — Whitelist breadth is appropriately conservative

`allowedStateEventTypes` covers exactly the Matrix Client-Server API v1.18 §11
state event types plus `m.space.child` / `m.space.parent`. Custom/vendor types
(`com.*`, `io.*`, …) are deliberately excluded. This is the correct posture for
an enterprise gateway — clients that need vendor types can be allowlisted by
extending the single map. The implementation also sits **before** body decoding,
so the gateway absorbs neither parser cost nor allocation cost for rejected
requests, which is the right ordering.

The handler still returns 501 for whitelisted-but-not-implemented types (covered
by the existing `m.room.power_levels`-only switch). A future story (9-7) will
implement the remaining types; until then, the 501 is correct semantics — the
event type IS valid Matrix; the gateway simply hasn't wired the gRPC path yet.

---

## 4. Cross-cutting checks (negative findings — confirmed not applicable)

| Concern | Result |
|---|---|
| SQL injection | N/A — no DB query touches `eventType`. |
| CSRF on state-changing endpoint | Existing JWT bearer auth + Matrix spec design (no cookies). |
| Auth bypass | Whitelist runs *after* `JWTMiddleware`; unauthenticated requests get 401 first. |
| Timing attack | Map lookup leaks length, not membership — irrelevant for an allow-list of public Matrix type names. |
| Open redirect | No redirect emitted on this path. |
| Body-size limit | `bodyLimit1MiB` wraps the route. |
| Rate limit | Per-IP limiter applies to the gateway as a whole. |
| Weak crypto | None invoked. |
| Plaintext secrets in logs | Not logged. |
| Path traversal | `{eventType}` is a single mux segment. |
| JWT validation flaws | Inherited from `JWTMiddleware`; not modified by this story. |
| Both route variants (with/without `{stateKey}`) | Same handler is registered for both — confirmed in `main.go:821-824`. |

---

## 5. Inline fixes applied during this review

- **MINOR (gofmt):** `state_event_types.go` was committed with mis-aligned
  trailing-value spacing (gofmt diff). Re-formatted in place; no behavioural
  change. (Not a security issue — included here for traceability.)

---

## 6. Conclusion

The whitelist is correctly placed (before body decoding, after auth), the
comparison is performed on the URL-decoded canonical form, error reflection is
JSON-encoded and HTML-escaped by default, and the surface is bounded by existing
header/body/rate limits. **No CRITICAL, HIGH, MEDIUM, or LOW findings.**

**Classification: CLEAN.**
