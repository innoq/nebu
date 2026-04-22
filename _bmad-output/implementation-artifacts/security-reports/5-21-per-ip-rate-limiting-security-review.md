# Security Review: Story 5.21 -- Per-IP Rate Limiting for Public Endpoints (Re-Review)

**Date:** 2026-04-20
**Reviewer:** Kassandra (Security Agent)
**Story:** 5-21-per-ip-rate-limiting
**Diff base:** HEAD (staged changes)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-6`
**Review round:** 2 (re-review after fixes from round 1)

## Executive Summary

Round 2 re-review of Story 5.21. All four findings from round 1 have been addressed: HIGH-1 (XFF spoofing) is fixed via rightmost extraction with thorough documentation; MAJOR-1 (missing loose tier) is implemented and wired to all remaining unauthenticated endpoints; MAJOR-2 (Prometheus metrics) is implemented with `nebu_rate_limit_total{tier,decision}` counter and tested; MINOR-1 (TOCTOU race) is fixed with `sync.Mutex` around the Get+Add sequence. The implementation is sound. Two new observations at LOW and INFO level.

## Findings

### RESOLVED from Round 1

#### [RESOLVED] HIGH-1: X-Forwarded-For IP Spoofing (was CWE-346)

- **Original:** `extractClientIP` used leftmost XFF entry, allowing trivial rate-limit bypass via header spoofing.
- **Fix verified:** `extractClientIP` (ratelimit.go:85-98) now takes `ips[len(ips)-1]` (the entry appended by the trusted proxy) when `len(ips) >= 2`. Falls back to `RemoteAddr` when XFF has zero or one entry. Extensive documentation in code comments (lines 7-20, 66-84) explains the trust model and requires the reverse proxy to strip client-supplied XFF headers.
- **Test:** `TestRateLimit_TrustedProxy_RightmostMinusOne` (ratelimit_test.go:212-255) verifies that spoofed leftmost entries are ignored and the rightmost (proxy-appended) IP is used as rate-limit key.
- **Status:** FIXED. No residual risk for the documented single-proxy architecture.

#### [RESOLVED] MAJOR-1: Missing `loose` Rate-Limit Tier (was AC 2 Violation)

- **Original:** Only `strictRL` and `mediumRL` were defined; unauthenticated discovery endpoints had no rate limiting.
- **Fix verified:** `looseRL` defined in main.go (line 136) at 300 req/min, burst 100. Applied to: `GET /_matrix/client/versions`, `GET /.well-known/matrix/client`, `GET /_matrix/client/v3/login` (GET), `GET /_matrix/client/v3/capabilities`, `GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata`, `GET /_matrix/media/v3/config`, `GET /_matrix/client/v3/directory/room/{roomAlias}`, `GET /_matrix/client/v3/thirdparty/protocols`, `GET /_matrix/client/v3/voip/turnServer`, `GET /_matrix/client/unstable/org.matrix.msc3814.v1/dehydrated_device`, `GET /_matrix/client/unstable/org.matrix.msc4143/rtc/transports`, `GET /_matrix/client/v3/keys/changes`.
- **Status:** FIXED. All unauthenticated public endpoints now have rate limiting.

#### [RESOLVED] MAJOR-2: Missing Prometheus Metrics (was AC 6 Violation)

- **Original:** No Prometheus instrumentation in the rate-limit middleware.
- **Fix verified:** `nebu_rate_limit_total` CounterVec registered via `prometheus.MustRegister` in `init()` (ratelimit.go:50-60) with labels `tier` and `decision`. Incremented on every allow (line 171) and deny (lines 152, 166) decision.
- **Test:** `TestRateLimit_PrometheusCounters` (ratelimit_test.go:268-342) verifies counter values via `prometheus.DefaultGatherer.Gather()`.
- **Status:** FIXED.

#### [RESOLVED] MINOR-1: TOCTOU Race in getLimiter

- **Original:** `cache.Get()` followed by `cache.Add()` without atomicity -- two concurrent requests for the same new IP could each create separate limiters.
- **Fix verified:** `sync.Mutex` (ratelimit.go:131) with `mu.Lock()` / `defer mu.Unlock()` around the Get+Add sequence (lines 133-142).
- **Status:** FIXED.

#### [RESOLVED] MINOR-2: GET /profile/{userId} Missing mediumRL

- **Original:** Profile read route was registered without rate-limit middleware.
- **Fix verified:** main.go line 700: `mux.Handle("GET /_matrix/client/v3/profile/{userId}", mediumRL(http.HandlerFunc(profileHandler.GetProfile)))`.
- **Status:** FIXED.

---

### New Findings (Round 2)

### [LOW] Misleading "rightmost-minus-1" terminology in extractClientIP

- **CWE / OWASP:** N/A (documentation accuracy, no runtime vulnerability)
- **File:** gateway/internal/middleware/ratelimit.go:17, 75, 89
- **Description:** The code comments consistently use the term "rightmost-minus-1" to describe the XFF extraction strategy, but the implementation takes `ips[len(ips)-1]` which is the **rightmost** (last) entry -- not rightmost-minus-1 (`ips[len(ips)-2]`). In the single-proxy architecture these are equivalent (the rightmost entry is the one the trusted proxy appended for the client), but the term "rightmost-minus-1" has a specific meaning in the security literature (RFC 7239 / Adam Langley's 2014 analysis) referring to the second-to-last entry. If a second proxy is ever added to the deployment topology, a developer reading "rightmost-minus-1" would incorrectly assume the implementation already handles multi-hop chains.
- **Impact:** No runtime vulnerability in the current single-proxy deployment. Maintenance risk if deployment topology changes.
- **Recommendation:** Rename the strategy to "rightmost" in all comments, or add an explicit note: "In Nebu's single-proxy architecture, rightmost == rightmost-minus-1 because the trusted proxy appends exactly one entry." Alternatively, if multi-proxy support is a future requirement, implement configurable `trusted_proxy_count` and extract `ips[len(ips)-trusted_proxy_count]`.
- **Reference:** RFC 7239 Section 5, OWASP Cheat Sheet: HTTP Headers

### [INFO] Static asset routes not rate-limited

- **CWE / OWASP:** A04:2021 (Insecure Design) -- observation only
- **File:** gateway/cmd/gateway/main.go:270-274
- **Description:** Five `/admin/static/*` routes (CSS, fonts, vendor libs, JS) are unauthenticated and not wrapped with `looseRL`. These are static file-serving endpoints whose response bodies are typically small and cacheable. Applying rate limiting here would increase the risk of blocking legitimate page loads (a single admin page load triggers ~5 asset requests) without meaningful security benefit, since static asset serving is inherently cheap.
- **Impact:** Minimal. Static assets do not trigger database queries, OIDC flows, or state mutations. A volumetric DoS targeting static assets would be more effectively mitigated at the reverse-proxy or CDN layer.
- **Recommendation:** No action required. If future static assets become computationally expensive (e.g., server-side rendering), revisit.

### [INFO] Legacy admin auth routes not rate-limited (pre-existing)

- **CWE / OWASP:** CWE-307 (Improper Restriction of Excessive Authentication Attempts)
- **File:** gateway/cmd/gateway/main.go:237-239
- **Description:** Legacy routes `GET /admin/auth/login` and `GET /admin/auth/callback` (marked as "backward compatibility -- Story 3.10 will supersede") are unauthenticated OIDC-triggering endpoints without rate limiting. This is a pre-existing condition, not introduced by Story 5.21. The new canonical equivalents (`/admin/login`, `/admin/login/start`, `/admin/callback`) are correctly wrapped with `strictRL`.
- **Impact:** Same attack surface as HIGH-1 from round 1, but on legacy routes. Low priority given the supersede plan.
- **Recommendation:** Either apply `strictRL` to the legacy routes or remove them if Story 3.10 is complete. Track as tech-debt, not as a 5.21 blocker.

### [INFO] loginTokenStore still has no capacity cap

- **File:** gateway/internal/matrix/sso.go:89-124
- **Description:** Carried over from round 1 INFO-1. `loginTokenStore` performs stale-purge on every `save()` but has no hard capacity limit like `ssoStateStore` (10,000). The attack surface is limited because `save()` is only called after a successful OIDC token exchange (requires valid authorization code). TTL is 5 minutes with stale purge -- organic cleanup prevents unbounded growth under normal load.
- **Impact:** Theoretical only. Would require an attacker to complete thousands of valid OIDC flows within 5 minutes.
- **Recommendation:** Optional defense-in-depth: add a capacity cap consistent with `ssoStateStoreMaxEntries`. Not a blocker.

### [INFO] IPv6/IPv4 dual-stack rate-limit bypass potential

- **File:** gateway/internal/middleware/ratelimit.go:85-98
- **Description:** Carried over from round 1. Rate limiter keys on exact IP string. An attacker with dual-stack connectivity gets two independent buckets. IPv6-mapped IPv4 normalization and /64 prefix keying are potential future improvements.
- **Impact:** Doubles effective rate limit per physical client in dual-stack environments.
- **Recommendation:** Future improvement. Not a blocker for MVP.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A -- no compliance data access in diff |
| `reason` field on compliance access         | ✅ N/A -- no compliance data access in diff |
| Audit-log immutability                      | ✅ N/A -- no audit table changes |
| `instance_admin` notification (if in-scope) | ✅ N/A -- no scope escalation paths |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ N/A -- rate limiter does not handle tokens; existing OIDC validation unchanged |
| Matrix Power Level checks                   | ✅ N/A -- no room mutation endpoints modified |
| No hardcoded secrets                        | ✅ No secrets in diff |
| TLS 1.3 enforcement                         | ✅ N/A -- no TLS config changes |
| AES-256-GCM correctness                     | ✅ N/A -- no encryption in diff |
| Ed25519 verify-before-accept                | ✅ N/A -- no signature handling in diff |
| No secrets in logs / error messages          | ✅ Rate-limit 429 responses contain only `M_LIMIT_EXCEEDED` and retry duration; `slog.Warn` for store-full logs error string only; no tokens, IPs, or credentials in log output |

## Dependency Scan

- **New dependency: `golang.org/x/time v0.15.0`** -- Official Go extended library. No known vulnerabilities. Provides `rate.Limiter` (token bucket). Standard choice for HTTP rate limiting in Go.
- **Promoted from indirect to direct: `github.com/hashicorp/golang-lru v0.5.4`** -- Already present as indirect dependency. Thread-safe LRU cache. No known vulnerabilities in v0.5.4.
- **Promoted from indirect to direct: `github.com/jackc/pgx/v5 v5.8.0`** -- Already present as indirect dependency. PostgreSQL driver. Not related to this story's changes (likely go.mod cleanup).
- **New direct dependency: `github.com/prometheus/client_golang v1.23.2`** -- Already present, used for metrics counter.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0     |
| HIGH      | 0     |
| MEDIUM    | 0     |
| LOW       | 1     |
| INFO      | 4     |

## Pipeline Decision

**CLEAN** -- no CRITICAL / HIGH findings. All four findings from round 1 (1x HIGH, 2x MAJOR, 1x MINOR) have been verified as fixed. One new LOW (misleading comment terminology) and four INFO observations. Pipeline may proceed.

---

*Generated by Kassandra -- BMAD Security Review Agent. This report is an immutable audit artifact -- do not edit retrospectively; create a new review if re-analysis is required.*
