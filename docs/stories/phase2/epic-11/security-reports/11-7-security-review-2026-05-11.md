# Security Review — Story 11.7 (Safari SSO re-login fix)

**Diff scope:** Staged changes to security-sensitive code in the Gateway:
- `gateway/internal/matrix/sso.go` — per-request OIDC nonce (16 random bytes → 32 hex), nonce stored in `ssoStateEntry`, nonce verified against `id_token` claim in `GetSSOCallback`, `Cache-Control: no-store` set on the 302 redirect.
- `gateway/internal/matrix/login.go` — `PostLogin` consults `h.store.IsInvalidated(rawJWT)` after JWT verification, rejects denylisted tokens with `403 M_FORBIDDEN`, logs structured event `denylist_hit_login`; `NewLoginHandler` warns when `cfg.Store == nil`.
- `gateway/cmd/gateway/main.go` — `tokenStore` construction hoisted above `NewLoginHandler` so it can be passed via `LoginConfig.Store`; the same `PostgresTokenStore` instance is reused by `JWTMiddleware`, the logout handler, and the user-status checker.

**Date:** 2026-05-11
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Story:** docs/stories/phase2/epic-11/11-7-safari-relogin-fix.md
**Branch:** feature/phase-2-after-mvp

## Methodology

Adversarial review against the full SEC Gate 2 scope:
SQL injection, XSS, CSRF on state-changing endpoints, auth bypass (missing middleware, IDOR), timing attacks on secret comparison, open redirects, missing body-size limits, missing rate limits, weak crypto primitives, plaintext secrets in logs, missing security headers, path traversal, JWT validation flaws (alg confusion, missing `exp`/`aud`/`nonce`).

MEMORY.md (`_bmad/memory/nebu-agent-kassandra/MEMORY.md`) consulted — no accepted risks applicable here. Recurring patterns checked: this story adds no SQL migrations, no new tables, no `state_key` filters, and no new DB modules taking `user_id` for authorization scoping, so none of the recurring patterns apply.

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `gateway/internal/matrix/sso.go:411` | Non-constant-time comparison of nonce (`nonceClaims.Nonce != entry.nonce`). Not exploitable in practice (single-use, 128-bit, server-side state lifecycle is 10 min and the entry is popped before comparison), but a best-practice deviation. | Optional hardening: use `crypto/subtle.ConstantTimeCompare([]byte(nonceClaims.Nonce), []byte(entry.nonce)) == 1`. Not blocking. |
| 2 | LOW | `gateway/internal/matrix/sso.go:412-413` | The mismatching nonce returned by the IdP is logged in full (`"got", nonceClaims.Nonce`) while the expected nonce is logged only as an 8-char prefix. The "got" value originates from an `id_token` whose signature has already been validated against the OIDC JWKS, so it is not an attacker-supplied arbitrary string, but in principle the field could be logged at the same prefix granularity as `want_prefix` to keep operator log surface symmetric. | Consider trimming `nonceClaims.Nonce` to a short prefix for symmetry. Not blocking — this is on the error path only, and the value is not a server secret. |

## Detail

### Finding #1 — Nonce equality uses plain `!=` [LOW]

**Code:**
```go
if entry.nonce == "" || nonceClaims.Nonce != entry.nonce {
    slog.Error(...)
    writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "SSO nonce mismatch — please try logging in again")
    return
}
```

**Why this is not exploitable:**
- `entry.nonce` is generated per request via `crypto/rand` (16 bytes → 128 bits of entropy).
- The state key under which the nonce is stored is also 128-bit random and single-use (`globalSSOState.pop()` is called *before* this comparison and the entry is removed unconditionally — see the explanatory comment at lines 340-343).
- Each guess therefore consumes the only valid attempt; an attacker cannot iteratively probe nonce bytes by timing a response, because every probe destroys the state.
- An attacker who can already produce a JWT with a valid Dex signature (otherwise `callbackVerifier.Verify` fails at line 397) does not gain anything additional from a successful nonce equality leak.

**Best-practice fix (optional):** use `crypto/subtle.ConstantTimeCompare`. This is the idiomatic Go pattern for "compare two opaque high-entropy values that are scoped to security-relevant decisions" and matches how the token denylist hashes are derived (`sha256` then compare). Not a blocker.

### Finding #2 — Mismatched nonce logged in full [LOW]

**Code:**
```go
slog.Error("matrix SSO: nonce mismatch — Dex returned a stale or cached id_token",
    "want_prefix", entry.nonce[:min(8, len(entry.nonce))], "got", nonceClaims.Nonce)
```

The "got" nonce is a value that an attacker who can produce a signed JWT could control. In Nebu's deployment model that is Dex itself (or any token issuer reaching the JWKS). The "want" value is masked to 8 chars, "got" is logged in full. In an audit log, the asymmetry is mildly confusing and a future story could decide to log only `len("got")` or its prefix for log-volume reasons; from a confidentiality standpoint, there is no leak because:

- The value did not match the live server-side nonce (else this log branch is unreachable).
- The value is not the access token, refresh token, or any persistent secret.
- The slot of the SSO state has already been popped.

**Best-practice fix (optional):** trim `nonceClaims.Nonce` to a short prefix for symmetry. Not a blocker.

## Scope Coverage Confirmation

| Dimension | Result | Notes |
|-----------|--------|-------|
| **SQL injection** | N/A | No new SQL. `PostgresTokenStore.IsInvalidated` uses parameterised query `WHERE token_hash = $1` with sha256 hash; existing code, not changed by this story. |
| **XSS** | N/A | No HTML rendered. All error responses go through `writeMatrixError` → JSON. No user input echoed back. |
| **CSRF** | N/A | `GetSSORedirect` is GET with no state mutation beyond a transient capped (10k) in-memory map. `POST /login` is the standard Matrix login endpoint, unauthenticated by design; CSRF is not the applicable threat model — credential validation is. |
| **Auth bypass / missing middleware** | Clean | No new routes added. `POST /login` continues to be wrapped with `strictRL(bodyLimit1MiB(...))`. SSO routes wrapped with `mediumRL`. |
| **IDOR** | N/A | No user-scoped resources accessed. |
| **Timing attack on secret comparison** | LOW — Finding #1 | See above. Not exploitable due to single-use state. |
| **Open redirect** | Clean | `entry.redirectURL` is the value stored at `GetSSORedirect` time, where `isRedirectURLAllowedWithSchemes` enforces the scheme allowlist + denylist. This story does not widen that surface. |
| **Missing body-size limits** | Clean | `POST /login` continues to use `bodyLimit1MiB`. The SSO callback is GET. |
| **Missing rate limits** | Clean | All affected endpoints continue to use existing rate-limit middlewares (`strictRL`, `mediumRL`). |
| **Weak crypto primitives** | Clean | `crypto/rand` for nonce + state + loginToken. `sha256` for the denylist hash. RS256 (default) enforced for OIDC verification via `validate.SupportedAlgs()`. No MD5/SHA1/DES/RC4. |
| **Plaintext secrets in logs** | Clean | The denylist-hit log uses only `"event","denylist_hit_login"` — no JWT in the message. The slog.Warn in `NewLoginHandler` for a nil store contains no secrets. |
| **Missing security headers** | Clean | `Cache-Control: no-store` *added*, not removed — improves caching posture for the 302 redirect. |
| **Path traversal** | N/A | No filesystem paths constructed from user input. |
| **JWT validation flaws** | Clean | The new callback-side verifier configures `ClientID` (audience check) and `SupportedSigningAlgs` (alg whitelist from `NEBU_OIDC_SUPPORTED_ALGS`, default RS256 only — `alg: none` is rejected, RS256↔HS256 confusion is blocked). `oidc.Verifier.Verify` enforces `exp`, `iss`, and signature. The nonce claim is then extracted and explicitly compared by the application code, as documented in the inline comment at lines 391-392 (the OIDC library does not auto-validate nonce). `PostLogin` performs the authoritative second verification with the same configuration. |

## Defense-in-depth observations (non-findings)

- The new `IsInvalidated` check in `PostLogin` is layered: it runs **after** the cryptographic `verifier.Verify` succeeds. A token that is expired or has a bad signature is already rejected before the denylist is consulted, so the denylist never sees, and never persists, an unrelated string.
- `PostgresTokenStore` already hashes the rawToken (sha256) before persisting and before lookup, so a compromised DB does not expose JWTs.
- `tokenStore` is initialised once and shared across `JWTMiddleware`, `LogoutHandlerWithCore`, `WithUserStatusCheck`, and now `LoginHandler`. Single source of truth — no risk of one component "forgetting" a token that another invalidated.
- The `pop()-before-verify` ordering for SSO state is intentional and **correct**: state replay protection must not depend on later verification succeeding. The code comment makes this explicit (lines 340-343). A retry attempt with the same `state` correctly returns `400 M_UNKNOWN`, not a leaked status from the verification step.
- `entry.nonce[:min(8, len(entry.nonce))]` is bounds-safe: when `entry.nonce` is empty `min(8, 0) == 0` and `[:0]` is `""`. (Go 1.21+ `min` builtin.)

## Summary

CRITICAL: 0 — block commit
HIGH: 0 — block commit
MEDIUM: 0 — advisory, address before epic end
LOW: 2 — advisory (both optional best-practice hardenings)

**Verdict:** APPROVED — CLEAN

No CRITICAL or HIGH findings. The two LOW items are optional hardening suggestions that do not block the commit. Story 11.7 may proceed to the epic-end SEC Gate 2 review without remediation.
