# Security Review: Story 5.18 — OIDC JWT Algorithm Pinning

**Reviewer:** Kassandra (Security Agent)
**Date:** 2026-04-20
**Scope:** Staged diff for Story 5.18
**Classification:** CLEAN

---

## Threat Model

| Threat | STRIDE | CWE | Status |
|---|---|---|---|
| Algorithm confusion (HS256 accepted as RS256) | Spoofing | CWE-327 | **MITIGATED** |
| Empty algorithm list bypasses pinning | Spoofing | CWE-327 | **MITIGATED** |
| Operator misconfiguration allows weak alg | Spoofing | CWE-327 | **ACCEPTABLE** (operator responsibility) |

---

## Files Reviewed

| File | Change Summary | Security Impact |
|---|---|---|
| `gateway/internal/validate/algs.go` | New: env-var parser for `NEBU_OIDC_SUPPORTED_ALGS` | Core defense |
| `gateway/internal/middleware/alg.go` | New: thin wrapper delegating to `validate.SupportedAlgs()` | Convenience layer |
| `gateway/internal/middleware/auth.go` | Verifier now uses `SupportedSigningAlgs` | AC1 fix |
| `gateway/internal/admin/auth.go` | Verifier now uses `SupportedSigningAlgs` | AC2 fix |
| `gateway/internal/matrix/login.go` | Verifier now uses `SupportedSigningAlgs` | AC3 fix |
| `gateway/internal/middleware/alg_test.go` | 6 tests: env parsing + HS256 rejection + RS256 green path | Validation |

---

## Findings

### 0 CRITICAL / 0 HIGH findings

### Minor Issues (fixed during review)

1. **Per-request `os.Getenv` in JWTMiddleware** -- `ParseSupportedAlgs()` was called inside the per-request handler closure, causing an unnecessary `os.Getenv` syscall on every authenticated request. **Fixed:** moved the call to middleware construction time (outside the closure), stored in `algs` local variable.

2. **Inconsistent indentation in middleware/auth.go** -- Extra tab level on `oidc.Config` struct literal. **Fixed:** aligned with surrounding code style.

3. **Stale RED-phase comments in alg_test.go** -- Header comments still referenced "intentionally FAILING" and "compile error" despite implementation being complete. **Fixed:** updated to reflect green-phase reality.

---

## Algorithm Pinning Verification

### All 3 verifier sites confirmed:

| Site | File:Line | `SupportedSigningAlgs` set? |
|---|---|---|
| JWT Middleware | `middleware/auth.go:76` | Yes, via `ParseSupportedAlgs()` (cached at construction) |
| Admin Callback | `admin/auth.go:552` | Yes, via `validate.SupportedAlgs()` |
| Matrix Login | `matrix/login.go:129` | Yes, via `validate.SupportedAlgs()` |

### Empty-string defense:

`validate.SupportedAlgs()` handles all edge cases:
- Unset env var -> `["RS256"]`
- Empty string `""` -> `["RS256"]`
- Whitespace-only `"   "` -> `["RS256"]`
- Trailing/leading commas `",RS256,"` -> `["RS256"]` (empty parts filtered)
- `","` -> `["RS256"]` (all parts empty after trim)

No path produces `[""]` -- the `t != ""` guard in the loop prevents it.

### HS256 rejection test:

`TestJWTMiddleware_HS256Rejected` constructs a valid HS256 JWT with correct issuer/audience claims and passes it through the real `JWTMiddleware`. Verifies:
- HTTP 401 response
- `M_UNKNOWN_TOKEN` errcode in JSON body
- Inner handler is NOT called (algorithm confusion would reach it)

### RS256 green path:

`TestJWTMiddleware_RS256StillAcceptedAfterPinning` confirms that a valid RS256-signed JWT still passes through the middleware after pinning is applied.

---

## Residual Risk

- `admin/auth.go` and `matrix/login.go` still call `validate.SupportedAlgs()` per invocation (not cached). This is acceptable because both are low-frequency paths (SSO callback / login endpoint), not the hot path like JWT middleware.
- Operators can set `NEBU_OIDC_SUPPORTED_ALGS=HS256` which would weaken security. This is by design (operator responsibility) and documented in the env var.

---

## Verdict

**CLEAN** -- No CRITICAL or HIGH findings. All 3 minor issues were fixed during review. Algorithm pinning is correctly applied at all verifier construction sites with robust empty-input handling.
