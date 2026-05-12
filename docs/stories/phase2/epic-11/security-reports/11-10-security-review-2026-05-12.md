# Security Review — Story 11-10 (OIDC Claim Mapping)

**Reviewer:** Kassandra (nebu-agent-kassandra)
**Date:** 2026-05-12
**Diff scope:** staged diff against feature/phase-2-after-mvp
**Story:** docs/stories/phase2/epic-11/11-10-oidc-claim-mapping.md
**Status:** APPROVED — no CRITICAL or HIGH findings

---

## Scope Reviewed

Changed surfaces analysed end-to-end against the security scope provided by the caller:

- `gateway/migrations/000044_oidc_claim_mapping.up.sql` / `.down.sql` (seed defaults)
- `gateway/internal/admin/claim_mapping.go` (new handler — GET + POST `/admin/config/claim-mapping`)
- `gateway/internal/admin/auth.go` (`postgresServerConfigReader` extensions: `LoadClaimMapping`, `SaveClaimMapping`, `LoadServerConfigKey`; `ClaimSelectionHandler` extended to persist claim mapping inside bootstrap TX)
- `gateway/internal/admin/bootstrap.go` (Step 3: Claim Mapping handler in wizard)
- `gateway/internal/admin/templates/claim-mapping.html` (new template)
- `gateway/internal/admin/templates/bootstrap.html` (Step 3 form fragment)
- `gateway/internal/admin/templates/layouts/base.html` (Claim Mapping nav link)
- `gateway/internal/admin/page_data.go` / `flash.go` (new data types + allowed flash)
- `gateway/internal/grpc/metadata.go` (`FormatUserIDFromClaims` refactor — new signature `(claimName, claims, serverName)`)
- `gateway/internal/matrix/login.go` (`PostLogin` uses cached claim loaders for user-id, displayname, email)
- `gateway/internal/middleware/auth.go` (`JWTMiddleware` accepts new `userIDClaimLoader` param)
- `gateway/cmd/gateway/main.go` (TTL-cached `claimLoader` + route registration)

Cross-checked against MEMORY.md known patterns:
- "DB-module user_id trust-boundary docstring" — N/A here, the new DB helpers don't take user_id from request payload.
- "Missing RLS on new tables" — no new tables added; migration only seeds existing `server_config`.
- "Nullable state_key + equality filter" — N/A.

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `gateway/internal/admin/claim_mapping.go:88` | POST `/admin/config/claim-mapping` is not covered by an admin-side rate limiter (`adminRL`). Consistent with the existing `/admin/config/role-mapping` route, but means a compromised admin session could replay updates at high throughput. | Wrap the `POST` route in `adminRL(...)` alongside the existing `bodyLimit64KiB(csrf(sessionGuard(...)))` chain, mirroring the bootstrap route. Defense-in-depth only — exploitation requires already-valid admin session + CSRF token. |
| 2 | LOW | `gateway/internal/admin/claim_mapping.go:122-130` | Prior values for the audit log are read **outside** the persistence path (separate call to `LoadClaimMapping`). Under concurrent updates the recorded `previous_*` values can disagree with what was actually overwritten — audit fidelity issue, not a security bypass. | Either read prior values inside a transaction with `SaveClaimMapping` and use `INSERT … RETURNING (old) … FROM …`, or document that the audit's "previous" fields are a best-effort snapshot. |
| 3 | LOW | `gateway/cmd/gateway/main.go:97-115` (`claimLoader.get`) | Refresh path holds the mutex during the DB call, so concurrent requests serialize on a single global lock once the 60s TTL expires. No security impact (correctness is preserved); pure latency concern. | Use a singleflight-style refresh or release the lock around the DB call with a "refresh in progress" sentinel. Optional. |
| 4 | LOW | `gateway/cmd/gateway/main.go:118-131` (`newServerConfigClaimLoader`) | `os.Getenv("NEBU_OIDC_USER_ID_CLAIM")` is passed straight through without the validation regex applied to admin-UI input. An operator who sets a garbage value (or one containing odd characters) bypasses the `^[a-zA-Z0-9:_\-.]+$` check applied via the admin UI. Trust-boundary parity issue only — env vars are operator-controlled, same trust level as DB. | At startup, validate the env value with `oidcClaimNameRe`; refuse to start (or log + ignore) when malformed. Hardening only. |
| 5 | LOW | `gateway/internal/admin/auth.go:993-1004` (bootstrap claim-mapping draft load) | Claim mapping values pulled from `bootstrap_draft` during `ClaimSelectionHandler` are NOT re-validated against `oidcClaimNameRe` before being persisted to `server_config`. The wizard validates them on the way in (Step 3 in `bootstrap.go:234-236`), but a direct DB write (or a future code path that bypasses the wizard) could insert unvalidated values via this hand-off. | Re-run `validateClaimField` on `oidcUserIDClaim/oidcDisplaynameClaim/oidcEmailClaim` inside `ClaimSelectionHandler` before calling `saveClaimMappingTx`. Defense-in-depth — the only current caller already validates upstream. |

No MEDIUM, HIGH, or CRITICAL findings.

---

## Detail

### Finding #1 — Missing rate limit on `/admin/config/claim-mapping` POST [LOW]

**What the code does:** `mux.Handle("POST /admin/config/claim-mapping", bodyLimit64KiB(csrf(sessionGuard(...))))` — no `adminRL` wrapper. Compare `/admin/bootstrap` which uses `adminRL(bodyLimit64KiB(csrf(guard(...))))`.

**Why low-severity:** The endpoint is behind `sessionGuard`, so an attacker needs a valid admin session AND a valid CSRF token. Even unlimited writes don't escalate privileges — they just churn the row. Consistent with the rest of `/admin/config/*` (role-mapping, audit-log-config), so this is a project-wide pattern, not a 11-10-specific regression.

**Remediation:** Add `adminRL` (60/min, burst 20 — same as bootstrap) to the route declaration. Or accept as a pre-existing pattern and address project-wide in a separate hardening story.

### Finding #2 — Audit log "previous values" read outside the write [LOW]

**What the code does:** `UpdateHandler` calls `LoadClaimMapping` (line 123) to capture prior values, then calls `SaveClaimMapping` (line 127). The two are not joined in a transaction. Under a concurrent racing admin POST, the captured `previous_*` values may not be the values that were actually overwritten.

**Why low-severity:** Audit log records the change, the actor, and the new values correctly. Only the "previous" field is potentially stale by one update.

**Remediation:** Either fold the read into a `SELECT FOR UPDATE` + `INSERT … ON CONFLICT DO UPDATE` inside `saveClaimMappingTx`, or document the best-effort semantics in the function header.

### Finding #3 — `claimLoader` refresh serializes under the lock [LOW]

**What the code does:** `claimLoader.get(ctx)` holds `c.mu` for the duration of `loadFn(ctx)` when the TTL expires. Under high concurrency, the first request hits the DB and every other request blocks behind it.

**Why low-severity:** No correctness issue; fail-open behavior preserved. Pure throughput concern. The TTL is 60s, so the cold-cache window is brief.

**Remediation:** Use `golang.org/x/sync/singleflight` or split into "fast-path read" + "background refresh".

### Finding #4 — Env-var override bypasses the claim-name regex [LOW]

**What the code does:** `newServerConfigClaimLoader(..., "NEBU_OIDC_USER_ID_CLAIM", os.Getenv(...))` injects the env value unfiltered. The admin UI enforces `^[a-zA-Z0-9:_\-.]+$` + ≤50 chars; the env-var path does not.

**Why low-severity:** Env vars are operator-controlled. A malicious operator can already write to `server_config` directly. Worth flagging as a trust-boundary parity issue.

**Remediation:** At startup, run `oidcClaimNameRe.MatchString(envValue)` and reject with a clear log line if invalid. Adds one screen of defense at boot.

### Finding #5 — `ClaimSelectionHandler` doesn't re-validate draft claim names [LOW]

**What the code does:** Bootstrap Step 3 (`bootstrap.go:234-236`) validates via `validateClaimField`. Once persisted to `bootstrap_draft`, the values flow through `ClaimSelectionHandler` (`auth.go:993-1004`) into `saveClaimMappingTx` without re-validation.

**Why low-severity:** All current code paths into `bootstrap_draft` go through the validated wizard step. There is no current path that bypasses validation. This is purely a defense-in-depth concern in case a future contributor adds a different draft writer (e.g. for testing or an alternate flow).

**Remediation:** Call `validateClaimField` on each of the three values inside `ClaimSelectionHandler` before invoking `saveClaimMappingTx`. Optional — adds 6 lines and removes a latent footgun.

---

## Threat-Model Walkthrough (caller's checklist)

The caller enumerated specific risk categories. Findings for each:

| Risk | Result |
|------|--------|
| SQL injection in claim name storage/retrieval | **Clean.** All queries are parameterized (`$1`/`$2`); migration 000044 uses static SQL literals. `LoadServerConfigKey` is called only with hardcoded keys; even if called with attacker-controlled key, the value is bound as a parameter. |
| XSS via claim name in templates | **Clean.** `html/template` auto-escapes `{{ .UserIDClaim }}` etc. (`gateway/internal/admin/handler.go:5` imports `html/template`). Input is also constrained by `oidcClaimNameRe` to `[a-zA-Z0-9:_\-.]+`, ≤50 chars — no `<>'"` can ever reach a template. |
| CSRF on `POST /admin/config/claim-mapping` | **Clean.** Route is wrapped in `csrf(...)`; template embeds `<input type="hidden" name="_csrf" value="{{ .CSRFToken }}">`. Bootstrap Step 3 form does the same. |
| Auth bypass — unauthenticated claim mapping change | **Clean.** Route wrapped in `sessionGuard(...)`. Bootstrap routes wrapped in `guard(...)` and gated by `bootstrapChecker.IsBootstrapActive` (replay protection in `LoginStartHandler` and `completeBootstrapTx`'s `ON CONFLICT DO NOTHING` → `ErrAlreadyCompleted`). |
| OIDC claim injection → privilege escalation | **Clean.** Claim values are extracted from a JWT verified against the OIDC issuer. Trust boundary is the OIDC IdP. Admin-controlled claim *name* (not value) is constrained by regex and used only as a Go map key — no SQL or template injection vector. |
| Timing attacks on claim name comparison | **Clean.** Claim names are not secrets; constant-time comparison is not required. Comparisons (`==` on map keys, `.MatchString`) are appropriate. No HMAC/secret comparison was changed. |
| Body-size limits on claim mapping POST | **Clean.** `bodyLimit64KiB` wrapper applied to `POST /admin/config/claim-mapping` and `POST /admin/bootstrap`. |
| Rate limits on claim mapping changes | **LOW finding #1** — missing `adminRL` on `/admin/config/claim-mapping` POST. Consistent with other `/admin/config/*` routes. |
| Path traversal via claim names | **Clean.** Claim names are never used as file paths; only as map keys and audit-log fields. Regex prevents `/` and `\\` anyway. |
| JWT validation gaps from new claim-name lookup | **Clean.** `FormatUserIDFromClaims(claimName, claims, serverName)` operates only after `verifier.Verify(...)` confirms the JWT is valid (login.go:159, middleware/auth.go above the changed block). Claim *name* is admin-config; value is JWT-asserted. No new validation skip. |
| Identity confusion via attacker-controlled claim values | **Documented risk, not a finding.** A user cannot change which OIDC claim is used (that's admin-only). They can only control claim *values* as far as their IdP allows. The story's admin UI carries a prominent warning that changing the user-ID claim post-bootstrap remaps every existing user; this is acknowledged operational risk, not a code vulnerability. `user_id` is `PRIMARY KEY` in the `users` table (migration 000004), so a value collision causes an INSERT failure, not silent account takeover. |
| Bootstrap transaction race | **Clean.** `completeBootstrapTx` uses `INSERT … ON CONFLICT DO NOTHING` and returns `ErrAlreadyCompleted` when `RowsAffected == 0`. Two concurrent bootstrap completion attempts result in exactly one commit; the loser rolls back. The claim mapping written corresponds to the winner's `bootstrap_draft` snapshot — last-writer-wins on the draft is acceptable because only the committing TX establishes the admin session. |

---

## Notes for Future Reviews

Two recurring observations worth keeping in mind:

1. **Cache TTL window for security-relevant config.** The 60s `claimLoader` TTL means a claim-mapping change is not effective immediately across the gateway. Combined with audit logging this is acceptable, but any future config that *increases* privileges (e.g. an "admin override" claim) must reload synchronously, not via TTL cache, or use an event-driven invalidation.

2. **Env-var escape hatches vs admin-UI validation.** `NEBU_OIDC_USER_ID_CLAIM` (Finding #4) is one example; the pattern recurs across the gateway (role claim, displayname, etc.). A single startup-time validation utility for "operator-supplied claim names" would close this category for good.

---

## Summary

- CRITICAL: 0
- HIGH: 0
- MEDIUM: 0
- LOW: 5

**Verdict: APPROVED.** No CRITICAL or HIGH findings — commit is not blocked. All five LOW findings are defense-in-depth or hygiene; address as time permits before epic close.
