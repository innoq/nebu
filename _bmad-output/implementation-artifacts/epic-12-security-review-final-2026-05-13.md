# Epic 12 Security Review — Final SEC Gate 2 — 2026-05-13

## Scope

- **Epic:** 12 — Media Gateway Phase 2 (MinIO backend, IAM, thumbnails, SEC Gate 2 hardening, OIDC fail-closed, canonical user ID, per-IP rate limiting)
- **Base:** `5020ec8`
- **HEAD:** `c0a77c1` (branch `feature/epic-12-media`)
- **Reviewer:** Kassandra (adversarial security)
- **Trigger:** Mandatory final epic-end SEC Gate 2 after Stories 12.8, 12.9, 12.10 each landed (per-story reviews reported CLEAN).
- **Stories covered (full diff range):**
  - 12.1 — MinIO Docker Compose + Docker Secrets
  - 12.2 — Storage interface refactor
  - 12.3 — MinIO backend wiring + IAM hardening
  - 12.4 — Media download error classification
  - 12.5 — On-demand thumbnail generation
  - 12.6 — Blurhash + animated thumbnail correctness
  - 12.7 — SEC Gate 2 fixes (closing 10 prior findings)
  - **12.8 — OIDC fail-open hardening (closing Residual-1)**
  - **12.9 — Canonical Matrix user ID in upload audit trail (intended to close Residual-2)**
  - **12.10 — Per-IP rate limiting on media gateway (closing Residual-3 recommendation)**
  - Plus Epic-11 carry-over (11.7–11.11) and infra fixes already covered in the prior reviews.

## Method

1. Verified each of the 3 LOW advisories from the 2026-05-13 prior review is actually resolved by 12.8 / 12.9 / 12.10.
2. Adversarial review of the new cross-story attack surface created by 12.8–12.10 combined.
3. Targeted inspection of:
   - OIDC verifier startup retry loop (races, retry amplification).
   - Rate-limiter X-Forwarded-For extraction (IP spoofing).
   - Migrations 000045, 000046, 000047 (RLS regressions, constraint regressions).
4. MEMORY.md scan: all recurring patterns (RLS UPDATE without key-scope, "MVP shortcut" residue, unauthenticated-endpoint resource bounds, `uploader_user_id` ≠ Matrix user ID, OIDC fail-open at startup) re-checked against the final diff.

*Note: there is no migration `000048` in this branch — the user prompt's reference to "000048" appears to be a typo. Only 000047 was added after the 2026-05-13 mid-review.*

## Findings — Prior-Advisory Resolution Verification

| Prior Advisory | Story Intended to Fix | Status | Notes |
|---|---|---|---|
| LOW R-1 — OIDC fail-open at startup | **12.8** | **FIXED** | `main.go:203-207`: empty `NEBU_OIDC_ISSUER` → `slog.Error("FATAL: …")` + `os.Exit(1)`. `main.go:324-337` (`initOIDCVerifier`): on all-attempts-failed → `slog.Error("FATAL: …")` + `os.Exit(1)`. Verifier can no longer be nil when `upload.Handler` accepts traffic. The handler-side nil check (`upload.go:202-227`) is now defensive belt-and-braces returning 503 M_UNAVAILABLE if ever reached. The "any-bearer-accepted" branch is gone. |
| LOW R-2 — `uploader_user_id` not canonical Matrix user ID | **12.9** | **PARTIAL — see Finding F-1 (MEDIUM)** | 12.9 wraps the OIDC subject in `formatMatrixUserID(subject, serverName)` (upload.go:165-189), so the stored value is now `@<sanitised>:server`. **However**, the localpart is still extracted from `claims.Sub` (with `claims.Name` fallback) — **not** from the operator-configured `oidc_user_id_claim` (default `name`). The audit-trail-correlation defect named in the 2026-05-13 review is therefore only partially closed: format is canonical, but the underlying claim choice diverges from the gateway. See F-1. |
| LOW R-3 — Per-IP rate limiting on media gateway | **12.10** | **FIXED with new HIGH — see Finding F-2** | `media/internal/ratelimit/ratelimit.go` adds two tiers (upload 10 r/s burst 5; download/thumbnail 100 r/s burst 20). 429 M_LIMIT_EXCEEDED + `Retry-After` header. sync.Map + 5-minute stale eviction. However the X-Forwarded-For extraction trusts the header unconditionally — see F-2. |

## New / Carried Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|---|---|---|---|---|
| F-1 | **MEDIUM** | 12.9 | `media/internal/upload/upload.go:47-66, 220` | Media upload extracts uploader identity from `claims.Sub` (then `claims.Name` fallback), bypassing the operator-configured `oidc_user_id_claim` (default `name`, see migration 000044) that the gateway login flow honours via `FormatUserIDFromClaims`. The stored `uploader_user_id` is `@<sub>:server` while the same user's `sender` in room events is `@<name>:server` — the values do not correlate. The Story 12.9 goal "enable compliance correlation with room events" is not fully achieved. | New (12.9 fix incomplete) |
| F-2 | **HIGH** | 12.10 | `media/internal/ratelimit/ratelimit.go:173-188`; `docker-compose.yml:230-231` | The rate-limiter unconditionally trusts `X-Forwarded-For` for any request whose header has ≥2 entries — there is **no** `trustedProxy` toggle equivalent to the API gateway's `gateway/internal/middleware/ratelimit.go:85-99`. Combined with the compose `ports: ["8009:8009"]` host binding, an external attacker can rotate the rightmost XFF entry on every request to force a fresh `LoadOrStore` and bypass the rate limit entirely. The MVP deployment has no reverse proxy fronting :8009 to strip XFF, so the documented operator assumption ("the reverse proxy MUST strip any X-Forwarded-For…") is not met by the shipping topology. | New (12.10 introduced) |
| F-3 | LOW | 12.8 | `media/cmd/media/main.go:191, 209` | `initOIDCVerifier` is invoked with `ctx := context.Background()` (line 191). The retry loop calls `oidc.NewProvider(ctx, issuer)` with this never-cancelled context — there is no per-attempt timeout. If Dex accepts TCP but never replies (slowloris-class TLS handshake or hung HTTPS), each retry can stall indefinitely and the 5-attempt fail-closed promise becomes unbounded wait. Not exploitable from outside; affects startup reliability. | New (12.8 hardening gap) |
| F-4 | LOW | 12.10 | `media/internal/ratelimit/ratelimit.go:86-98` | Race between `cleanupOnce`'s staleness read and `rl.limiters.Delete(key)`: another goroutine can `LoadOrStore` and bump `lastSeen` between the unlock and the delete, causing a fresh entry to be evicted. The attacker-observable effect is a new fresh-burst window for the affected IP — not a bypass, but a small jitter at the cleanup boundary. Acceptable but worth recording. | New (12.10) |
| F-5 | LOW | 12.10 | `media/internal/ratelimit/ratelimit.go:126-132` | `NEBU_RATE_LIMIT_DISABLED=true` is checked only at middleware construction. There is no log line that announces "rate limiting disabled" at startup. An operator who accidentally sets this in production has no audit signal. Defence-in-depth: emit a `slog.Warn("media: rate limiting disabled by NEBU_RATE_LIMIT_DISABLED")` when the no-op branch is taken. | New (12.10) |
| F-6 | INFO | 12.10 | `media/internal/ratelimit/ratelimit.go:174-182` | Header comment says "rightmost-minus-1" (matching the gateway) but the implementation uses the **rightmost** entry directly (`ips[len(ips)-1]`). The gateway likewise uses `ips[len-1]`, so the actual behaviour is consistent — but the naming convention "minus-1" refers to "trusted proxy adds the entry, drop the leftmost client-controlled ones" not "subtract one from the index". This is a documentation hygiene issue. | New (informational) |

### F-1 Detail — Canonical user ID claim mismatch (MEDIUM)

The gateway (`gateway/internal/grpc/metadata.go:54-66`) builds Matrix user IDs via:

```go
FormatUserIDFromClaims(uidClaim, allClaims, serverName)
// where uidClaim = server_config.oidc_user_id_claim (default "name" per mig 000044, post-3a5e305 commit)
```

The media upload handler (`media/internal/upload/upload.go:47-66`) does:

```go
if claims.Sub != "" { return claims.Sub, nil }
if claims.Name != "" { return claims.Name, nil }
```

With Dex's OIDC token typically containing both `sub` (UUID) and `name` (login):

| Surface | localpart source |
|---|---|
| Gateway message `sender` | `name` claim (sanitised) — e.g. `@alex:server` |
| Media `uploader_user_id` | `sub` claim (sanitised) — e.g. `@abc123def456:server` |

Audit/compliance queries that try to join `media_files.uploader_user_id` with room events on user identity will fail to correlate.

**Remediation:** load `oidc_user_id_claim` from `server_config` at media startup (DB read once, cached) or expose it via env (`NEBU_OIDC_USER_ID_CLAIM`, default `sub`). Then use that claim consistently. Alternatively, perform the canonicalisation in the gateway and pass a server-side translated ID to media via a service-to-service header — but that re-introduces cross-binary coupling 12.9 explicitly avoided.

**Severity rationale:** MEDIUM, not LOW — Story 12.9's stated AC was "compliance correlation". Promising a compliance feature without delivering its semantics is a defence-in-depth weakness that audit/legal teams will rely on. Not exploitable for unauthorised access; not blocking the epic.

### F-2 Detail — Rate-limit X-Forwarded-For spoofing (HIGH)

`extractClientIP` in the media ratelimit middleware:

```go
func extractClientIP(r *http.Request) string {
    xff := r.Header.Get("X-Forwarded-For")
    if xff != "" {
        ips := strings.Split(xff, ",")
        if len(ips) >= 2 {
            return strings.TrimSpace(ips[len(ips)-1])
        }
    }
    host, _, _ := net.SplitHostPort(r.RemoteAddr)
    return host
}
```

The gateway has the same logic gated by `trustedProxy bool`. Media has **no** such gate.

**Threat model:**
1. Compose binds `8009:8009` to the host (`docker-compose.yml:230-231`).
2. MVP deployments — per CLAUDE.md and ADR-012, this repo is reference implementation only with example-only CI credentials — typically run docker-compose directly on a VM or via the gitlab-ci.yml. There is no reverse proxy required to be in front of :8009.
3. An external attacker can `curl -H "X-Forwarded-For: 1.1.1.1, $(uuidgen)" http://target:8009/_matrix/media/v3/upload …` and the rate limiter buckets each request to a unique IP, defeating the per-IP token bucket entirely.
4. Result: the 10 r/s upload ceiling does not apply. An authenticated attacker (or in the unhappy case where Dex is misconfigured to issue tokens loosely) can saturate upload throughput; an unauthenticated attacker can defeat the download/thumbnail 100 r/s ceiling and DoS via the (still 50 MiB-per-request) decrypt path.

**Why HIGH, not MEDIUM:** the entire reason 12.10 exists is to mitigate the recurring "unauthenticated endpoint + unbounded resource" pattern recorded in MEMORY.md. The fix as shipped does not mitigate this for any topology that the project itself ships. The pattern is identical to the gateway's `trustedProxy=true` decision but was not gated.

**Remediation (one of):**
1. Add `NEBU_TRUSTED_PROXY` (default `false`) gating the XFF path, mirroring gateway. When unset/false, use `RemoteAddr` only.
2. Explicitly document that the operator MUST front :8009 with a proxy that strips XFF, and bind :8009 to `127.0.0.1` only in compose (`ports: ["127.0.0.1:8009:8009"]`). This requires a compose change.
3. Refuse to honour XFF entirely unless `NEBU_TRUSTED_PROXY=true`.

Recommended: option 1 (toggle-gated, matches the gateway). One-line fix, one env var, one test.

### F-3 Detail — OIDC retry context unbounded (LOW)

```go
ctx := context.Background()                                     // main.go:191
// …
uploadVerifier := initOIDCVerifier(ctx, oidcIssuer, oidcClientID, 5, 2*time.Second)
// → initOIDCVerifierWith → newProvider(ctx, issuer) per attempt
```

`oidc.NewProvider` makes a discovery GET to `issuer/.well-known/openid-configuration`. With `context.Background()`, an unresponsive Dex (TCP accepts, never sends headers) means each attempt may hang for the default HTTP client timeout — which is effectively never if not configured. The fail-closed promise of 12.8 ("5 attempts, 2s backoff, then exit") becomes "5 unbounded waits". Not exploitable externally, but the operational SLA of 10 s startup-or-die is not guaranteed.

**Remediation:** `ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)` per attempt; cancel after success or expiry.

### Migration Review (000045 / 000046 / 000047)

| Mig | Verdict | Notes |
|---|---|---|
| 000045 | Resolved in 000046 | Original `USING (true)` policy is the cautionary tale in MEMORY.md. Replaced by key-scoped `config_update_mutable` in 000046. No live regression on HEAD. |
| 000046 | Clean | Key allowlist matches Bootstrap UI mutable keys; server_name, bootstrap_completed remain immutable at the DB layer. Down migration restores the permissive policy with an explicit `WARNING` comment — acceptable. |
| 000047 | Clean | `COMMENT ON COLUMN` only. No schema change, no RLS impact, no constraint regression. Down reverts to NULL comment. |

No new RLS or constraint regressions in this slice. The recurring "Missing RLS on new tables" pattern does not apply (no new tables added).

## Cross-Story Attack-Surface Analysis (12.8 + 12.9 + 12.10 combined)

1. **12.8 closes the front-door fail-open** — the verifier is non-nil by the time the handler accepts traffic. Good. The handler still has a defensive 503 branch — good belt-and-braces.
2. **12.9 records canonical-looking user IDs** — but with the wrong claim (F-1). Anyone relying on `uploader_user_id` for compliance correlation will be silently misled. The compose default (`oidc_user_id_claim=name`) makes this a real-world divergence on every fresh install.
3. **12.10 adds rate limits** — defeated by F-2 in the shipping topology. The ports are exposed; the gate is XFF-trusted. An attacker chaining 12.10's XFF spoof with the still-unauthenticated download/thumbnail endpoints reconstructs the original Finding #11 (concurrent thumbnail DoS) the dimension caps from 12.7 were sized to mitigate at a known request rate.
4. **The three stories independently look fine. Together, they create the illusion of defence-in-depth without delivering it in MVP topology.** This is the recurring pattern: harden one layer, leave the surrounding deployment posture mismatched.

## Accepted Risks (pre-existing, carried)

| Risk | Justification | Accepted by | Date |
|---|---|---|---|
| Download / thumbnail endpoints intentionally unauthenticated per Matrix v3 spec | Matrix CS API v3 download is unauthenticated by design. Authenticated `_matrix/client/v1/media/*` is a separate epic. Mitigated in MVP by dimension caps and (intended) rate limits. | Project policy (CLAUDE.md "Matrix API Scope") | 2026-05-12 |

## Follow-up Stories Required (BLOCKING for epic-done)

- **Story 12-FU-10 (HIGH, blocking)** — Gate XFF trust in `media/internal/ratelimit` behind `NEBU_TRUSTED_PROXY` (default `false`). When `false`, use `RemoteAddr` only. Mirrors the gateway's pattern (`gateway/internal/middleware/ratelimit.go:85-99`). Add unit tests proving spoofed XFF cannot create distinct buckets when the toggle is off. Alternatively/additionally, bind `:8009` to `127.0.0.1` in `docker-compose.yml` (`ports: ["127.0.0.1:8009:8009"]`) — but the in-code gate is the primary fix.

## Recommended Follow-ups (non-blocking)

- **Story 12-FU-11 (MEDIUM)** — Load `oidc_user_id_claim` from `server_config` (or env override) at media startup; use it consistently with `FormatUserIDFromClaims` semantics so `media_files.uploader_user_id` correlates with `events.sender`. Backfill question for the team: should existing rows be migrated, or remain grandfathered (current 000047 comment policy)?
- **Story 12-FU-12 (LOW)** — Add per-attempt context timeout (`context.WithTimeout(10s)`) to `initOIDCVerifierWith` to bound startup retries.
- **Story 12-FU-13 (LOW)** — Emit `slog.Warn` when `NEBU_RATE_LIMIT_DISABLED=true` is honoured, for operational audit.
- **Story 12-FU-14 (INFO)** — Fix the "rightmost-minus-1" naming inconsistency in media ratelimit comments and align with gateway docstring.

## Summary

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH | **1** (F-2) |
| MEDIUM | **1** (F-1) |
| LOW | 3 (F-3, F-4, F-5) |
| INFO | 1 (F-6) |

- Follow-up stories required: **1** (12-FU-10, blocking)
- Recommended follow-ups: 4 (12-FU-11..14, non-blocking)
- Accepted risks: 1 (carried, pre-existing — unauthenticated media v3 endpoints)

**Epic security gate: BLOCKED — requires follow-up story 12-FU-10 (or compose-port + doc remediation) before epic 12 can be marked done.**

## Classification

**HIGH — BLOCKED.**

The three per-story reviews for 12.8 / 12.9 / 12.10 each reported CLEAN in isolation. Looked at together, on the topology this project ships, the combined surface area has a HIGH finding (F-2 — rate-limit XFF spoofing) and a MEDIUM finding (F-1 — canonical user ID claim mismatch defeats the audit-correlation goal of 12.9).

The fix for F-2 is small (one env-toggle gating the XFF code path, matching the gateway). The fix for F-1 is one config read at startup. Neither requires architectural rework. Both must be addressed (or formally accepted in writing) before epic close.

---

*Reviewed by Kassandra, 2026-05-13. The team deserved to know.*
