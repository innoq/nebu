# Security Review — Story 9.14: Admin UI — OIDC Token Refresh

| Field | Value |
| --- | --- |
| Reviewer | Kassandra (bmad-security-review) |
| Date | 2026-05-05 |
| Gate | SEC Gate 1 (per-story) |
| Diff scope | `git diff --staged` (12 files, +1193 / -58) |
| Story | `_bmad-output/implementation-artifacts/9-14-admin-ui-oidc-token-refresh.md` |
| Classification | **CLEAN** |
| Blocking | No |

## Executive Summary

Silent OIDC token refresh introduces three new attack surfaces — refresh-token storage in PostgreSQL, decrypt/use/discard at runtime in middleware, and silent session-sliding via the OIDC token endpoint. The implementation handles each one defensibly: refresh tokens are AES-256-GCM encrypted with the existing `internalSecret` before persistence, decrypted into a single local variable per request, never logged, never returned in error messages, and rotated whenever Dex returns a new refresh token. Cookie security flags on the refreshed cookie match the original. Audit metadata is restricted to `expires_at`. No CRITICAL or HIGH findings. Two MEDIUM defense-in-depth gaps and two LOW observations are recorded.

## Severity Counts

| CRITICAL | HIGH | MEDIUM | LOW | INFO |
| --- | --- | --- | --- | --- |
| 0 | 0 | 2 | 2 | 1 |

## Component Classification

| Bucket | Files |
| --- | --- |
| Auth / crypto | `gateway/internal/admin/middleware.go`, `gateway/internal/admin/auth.go` |
| API / middleware | `gateway/cmd/gateway/main.go` |
| DB layer | `gateway/internal/db/admin_session_store.go`, `gateway/migrations/000039_admin_sessions_refresh_token.{up,down}.sql` |
| Tests | `gateway/internal/admin/{token_refresh_9_14_test.go,session_revocation_test.go}`, `gateway/migrations/migrations_039_test.go` |
| Infrastructure | `dev/dex/config.yaml` |
| Story doc | `_bmad-output/implementation-artifacts/9-14-admin-ui-oidc-token-refresh.md` |

## Nebu Invariants Check

| Invariant | Status | Evidence |
| --- | --- | --- |
| AES-256-GCM correctness — same `internalSecret`, no secondary key | ✅ | `crypto.go:16` derives 32-byte key via `sha256.Sum256(secret)`; `middleware.go:298,347` and `auth.go:769` all pass `cfg.Secret` / `a.secret`. |
| AES-GCM nonce randomness, no reuse | ✅ | `crypto.go:25-27` reads fresh `gcm.NonceSize()` bytes from `crypto/rand` per encryption. |
| OIDC `iss`, `aud`, `exp` validation on initial login | ✅ | Existing pattern unchanged in `auth.go CallbackHandler`. Refresh path delegates response validation to `golang.org/x/oauth2`. |
| No refresh tokens in logs / error messages | ✅ | Reviewed every `slog.` call in middleware/auth; none emit `plainRT`, `EncryptedRefreshToken`, `token.RefreshToken`, or `newToken`. Audit metadata at `middleware.go:264` only contains `expires_at`. |
| No hardcoded secrets | ✅ | All keys flow through `cfg.Secret` (set from `internalSecret` env in `main.go:250`). |
| TLS 1.3 enforcement (gateway → Dex) | ⚠️ | `oidc.ClientContext(ctx, nil)` falls back to `http.DefaultClient`, which has no explicit `tls.Config{MinVersion: tls.VersionTLS13}`. Pre-existing project-wide pattern; flagged in LOW-2 below. |
| Decrypt → use → discard for refresh token | ✅ | `plainRT` (`middleware.go:298`) is a function-local variable in `attemptTokenRefresh`; goes out of scope on return. Returned upward only as ciphertext. |
| Token rotation — old token replaced after exchange | ✅ | `middleware.go:342-345` always uses `newToken.RefreshToken` if non-empty; otherwise re-stores old plaintext (Dex non-rotation mode). |
| Cookie security flags on refresh match original | ✅ | `middleware.go:250-258` (`HttpOnly: true`, `Secure: isRequestSecure(r)`, `SameSite: Lax`, `Path: /admin`) — identical to `auth.go:793-801`. |
| Audit-log immutability (migration touches only `admin_sessions`) | ✅ | Migration 000039 adds nullable `refresh_token TEXT` to `admin_sessions`; no audit-table changes. |
| SQL injection — parameterized queries only | ✅ | `db/admin_session_store.go` uses `$1..$5` placeholders for INSERT/UPDATE/SELECT/DELETE on the new column. |

## Findings

### MEDIUM-1 — `validateIssuerURL` not enforced in refresh path

**File / Line:** `gateway/internal/admin/middleware.go:292` (`attemptTokenRefresh`)
**CWE:** CWE-295 (Improper Certificate Validation, defense-in-depth) / CWE-918 (SSRF, defense-in-depth)
**OWASP:** A07:2021 — Identification & Authentication Failures

**Description.** `LoginStartHandler` (`auth.go:398`) and `CallbackHandler` (`auth.go:598`) call `validateIssuerURL(issuer)` to enforce HTTPS-only issuers (with a localhost exception for development) before constructing the OIDC provider. The refresh middleware does not. The issuer is loaded directly from `server_config` (or its in-memory cache) and passed to `globalProviderCache.load(...)` without policy validation.

**Impact.** Defense-in-depth gap. Direct exploit path requires write access to `server_config` (which already implies full system compromise). However, if a misconfiguration or future bug ever allowed a non-HTTPS issuer to land in `server_config` (e.g. a wizard regression), refresh-token traffic would silently flow over plaintext to that issuer — including the decrypted refresh token in the POST body. The login path would refuse such a config with a redirect to `/admin/bootstrap`; the refresh path would accept it.

**Recommendation.** Add `if err := validateIssuerURL(issuer); err != nil { return time.Time{}, "", fmt.Errorf("session refresh: %w", err) }` after the `LoadOIDCConfig` call in `attemptTokenRefresh`. Align with the existing login/callback pattern.

---

### MEDIUM-2 — Concurrent refresh race may revoke a valid session

**File / Line:** `gateway/internal/admin/middleware.go:219-238` (`SessionGuardWithRefresh`)
**CWE:** CWE-362 (Concurrent Execution Using Shared Resource — Race Condition)
**OWASP:** A04:2021 — Insecure Design

**Description.** Two near-simultaneous authenticated requests on the same session can both pass the expiry check, both decrypt the same refresh token, and both POST to Dex's token endpoint. With single-use rotation enabled (Dex's default for `refresh_token` grants when `disable_terminating_token_rotation` is unset), the first request consumes the token and the second receives a 4xx — at which point the second request executes `cfg.Store.Revoke(...)` and clears the cookie, forcibly logging out a user whose first request just succeeded.

**Impact.** Availability / UX. An admin user with multiple concurrent tabs (or any client that pipelines requests, e.g. a refreshing dashboard) experiences spurious logouts during the refresh window. No confidentiality or integrity breach — the user simply re-authenticates. Bounded blast radius (admin UI only).

**Recommendation.** Three options, in increasing complexity:
1. (Lowest effort) Document the limitation; rely on the 5-minute pre-expiry window making collisions rare in practice.
2. Treat token-endpoint failures during refresh as non-fatal **only if the session is still within `ExpiresAt`** (i.e. the pre-expiry window): keep the session, log a warning, retry on the next request. Revoke only when the session has actually expired and the refresh token cannot be exchanged.
3. Serialize concurrent refreshes per SID via `SELECT ... FOR UPDATE` on the session row inside a transaction, or via an in-process `singleflight.Group` keyed by SID. The second caller waits for the first and reuses its result.

---

### LOW-1 — ID token from refresh response is not re-verified

**File / Line:** `gateway/internal/admin/middleware.go:328` (`ts.Token()` result)
**CWE:** CWE-345 (Insufficient Verification of Data Authenticity)

**Description.** `attemptTokenRefresh` accepts the refresh response solely on its expiry timestamp and rotated refresh token. The new `id_token` (if returned by Dex) is neither parsed nor verified. By contrast, the initial `CallbackHandler` performs `provider.Verifier(...).Verify(ctx, rawIDToken)` plus nonce check.

**Impact.** Marginal. The `sub` field on the session row was set at initial login and is not updated by the refresh — so even if Dex were compromised and started returning ID tokens for a different `sub`, the gateway would not change the bound user. The risk is theoretical: a future code change that begins reading the ID token during refresh would inherit the missing verification.

**Recommendation.** Add a comment at `middleware.go:328` documenting that the ID token is intentionally ignored in the refresh path (and that the `sub` is sourced from the existing session row). If at some point claim re-verification is desired (e.g. group-membership re-check on long-lived sessions), wire `provider.Verifier(...).Verify(...)` analogously to `CallbackHandler`.

---

### LOW-2 — `http.DefaultClient` on the refresh hot path

**File / Line:** `gateway/internal/admin/middleware.go:309` (`oidc.ClientContext(ctx, nil)`)
**CWE:** CWE-400 (Uncontrolled Resource Consumption — partial)

**Description.** `oidc.ClientContext(ctx, nil)` falls back to `http.DefaultClient`, which has no `Timeout` and no explicit `tls.Config{MinVersion: tls.VersionTLS13}`. The same pattern exists in the login/callback handlers (introduced before Story 9.14). Story 9.14 promotes this pattern from a once-per-session login event to a once-per-five-minutes-per-active-session middleware hook on every authenticated admin request.

**Impact.** A slow or hanging Dex endpoint can stall the request goroutine until the request `ctx` deadline trips. With default Go HTTP-server timeouts, this leaves admin requests hanging long enough to multiply under load. Defense-in-depth gap rather than acute risk.

**Recommendation.** Configure a single shared `*http.Client` with `Timeout: 5 * time.Second` and `Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}`. Pass it via `oidc.ClientContext(ctx, sharedHTTPClient)` for both discovery and the oauth2 token-source call. This also addresses the same concern in `CallbackHandler` and `LoginStartHandler` — out of scope for Story 9.14 but worth a follow-up.

---

### INFO-1 — New attack surface: Dex `refresh_token` grant + persistent refresh-token storage

**File / Line:** `dev/dex/config.yaml`, `gateway/internal/db/admin_session_store.go`, migration 000039
**Note:** Recorded for the audit trail.

The `refresh_token` grant is now enabled in dev Dex config. Operators must mirror this in production Dex configs. The PostgreSQL `admin_sessions.refresh_token` column stores AES-256-GCM ciphertext keyed by the same `internalSecret` already used to encrypt `oidc_client_secret` in `server_config`. Compromise of `internalSecret` already enabled cookie forgery and OIDC client-secret recovery; refresh-token recovery is now in the same blast radius — no escalation.

## Triage Notes

- **Why no CRITICAL.** All Nebu invariants pass: AES-256-GCM keyed by `internalSecret`, no plaintext refresh tokens in logs / errors / audit metadata, parameterized SQL, cookie flags identical between login and refresh, decrypt-use-discard pattern observed.
- **Why no HIGH.** The two MEDIUM findings (issuer-URL validation gap, concurrent-refresh race) require either DB compromise or concurrent-tab UX edge case — neither is reachable from a normal authenticated user without an additional precondition. Per the Rufschädigungs-Test, neither would land in a CVE about Nebu without a chained vulnerability.
- **Grace window.** The 30-second post-expiry grace cannot extend the absolute session lifetime: each refresh re-caps `ExpiresAt` at `min(token.Expiry, now+8h)` (`middleware.go:334-338`), so an attacker who steals a cookie inside the grace window gains at most 8 hours — the same bound as a fresh login.
- **Token rotation.** Dex's `refresh_token` grant returns a new refresh token; the new token replaces the old in DB on the next `UpdateExpiry`. If `UpdateExpiry` itself fails, the next request will try the stored (old, now-invalidated) token and fail-closed by revoking — self-healing.

## Recommended Actions

1. (MEDIUM-1) Add `validateIssuerURL` in `attemptTokenRefresh` — single-line change, aligns with existing login/callback policy.
2. (MEDIUM-2) Choose one of the three race-condition mitigations — option (2) is the smallest behavioral change and removes spurious logouts.
3. (LOW) Defer LOW-1 / LOW-2 to a follow-up hardening pass; LOW-2 is best fixed project-wide rather than per-story.

No follow-up story is required to gate this story `done`. The MEDIUMs may be tracked as quality debt in the Epic 9 retrospective.
