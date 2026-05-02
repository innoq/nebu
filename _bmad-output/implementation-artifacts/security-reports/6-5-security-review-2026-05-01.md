# Security Review — Story 6-5 (User Deactivation + Reactivation + Session-Invalidierung)

**Date:** 2026-05-01
**Reviewer:** Kassandra (security-review agent)
**Scope:** `git diff --staged` for Story 6-5
**Files reviewed:**
- `gateway/api/openapi.yaml` (+97)
- `gateway/cmd/gateway/main.go` (+14, -8)
- `gateway/internal/api/api_gen.go` (regenerated)
- `gateway/internal/api/router.go` (+29, autofix +13)
- `gateway/internal/api/server.go` (+206, autofix +5)
- `gateway/internal/api/deactivation_repo.go` (+90)
- `gateway/internal/api/deactivation_handler_test.go` (+724, tests)
- `gateway/internal/middleware/auth.go` (+97)
- `gateway/internal/middleware/auth_deactivated_test.go` (+234, tests)
- `gateway/migrations/000034_users_deactivation.up.sql` (+8)
- `proto/core.proto` (+16)
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` (+28)
- `core/apps/event_dispatcher/test/nebu/event_dispatcher/invalidate_user_sessions_test.exs` (+198, tests)

**Focus areas (per request):**
1. Deactivation-Bypass — kann ein deaktivierter User trotzdem API-Calls machen?
2. State-Machine — können anonymisierte / keys_deleted User reaktiviert werden?
3. JWT-TTL-Cache-Poisoning — kann der Cache manipuliert werden?
4. gRPC-Invalidation-Timing — Race-Conditions zwischen DB-Update, Cache und Session-Eviction.

**Frameworks applied (weighted lenses):**
- OWASP Top 10 (2021): A01 Broken Access Control, A04 Insecure Design, A07 Identification & Auth Failures, A09 Security Logging Failures
- OWASP ASVS L2: V2 Authentication, V3 Session Management, V4 Access Control, V8 Data Protection, V14 Configuration
- CWE Top 25: CWE-285 (Improper Authorization), CWE-613 (Insufficient Session Expiration), CWE-287 (Improper Authentication), CWE-841 (Improper Enforcement of Behavioral Workflow), CWE-672 (Use of Expired Resource)
- STRIDE: **E**levation of Privilege (deactivated → still authorised), **T**ampering (cache state), **D**enial of Service (cache exhaustion), **R**epudiation (audit gaps)
- Nebu invariants: account-status invariant, audit immutability, RSP, OIDC validation chain, session lifecycle

---

## Classification: **HIGH**

| Severity | Count |
|----------|------:|
| CRITICAL |     0 |
| HIGH     |     1 |
| MEDIUM   |     2 |
| LOW      |     2 |
| INFO     |     2 |

One HIGH finding blocks merge. CRITICAL was avoided only because the deactivated user can still acquire a fresh OIDC token (which fails the existing `validate_token` check at Elixir core, returning `:deactivated`) — but with an already-issued JWT they bypass the gateway entirely on the Matrix API surface.

---

## Findings

### HIGH-1 — Deactivation bypass on Matrix API and most admin routes (CWE-285, CWE-613, OWASP A01)

**Files:**
- `gateway/cmd/gateway/main.go:441` — `jwtMiddleware` constructed
- `gateway/cmd/gateway/main.go:1126` — `WithUserStatusCheck` wrapped only into `jwtWithStatusCheck` and passed to `RegisterAdminRoutes`
- `gateway/cmd/gateway/main.go:446..796..` — 52 other route registrations using bare `jwtMiddleware` (Matrix API + non-Story-6.5 admin routes)

**Evidence:**

```go
// main.go:441
jwtMiddleware := middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, tokenStore, serverName)
// ... 52 occurrences below this line use `jwtMiddleware` (NOT wrapped) ...

// main.go:1126 — only the new Admin API routes get the wrapper
jwtWithStatusCheck := middleware.WithUserStatusCheck(jwtMiddleware, &middleware.DBUserStatusChecker{DB: bootstrapDB})
apihandler.RegisterAdminRoutes(mux, adminSrv, jwtWithStatusCheck)
```

A deactivated user holding a still-valid (unexpired, non-denylisted) OIDC JWT can continue to:
- `POST /_matrix/client/v3/rooms/{id}/send/...` — send messages (line 780)
- `POST /_matrix/client/v3/createRoom`, `/join/{id}`, `/invite`, `/kick`, `/ban` (lines 746–767+)
- `GET /_matrix/client/v3/sync`, `/messages` (line 788)
- `GET/PUT /_matrix/client/v3/devices/...`, `/keys/query`, `/pushrules`, `/pushers` (lines 491–725)
- `POST /_matrix/client/v3/logout` (line 739)
- `POST /api/v1/admin/users/{userId}/anonymize` (line 1070) — IF deactivated user is also instance_admin
- `DELETE /api/v1/admin/users/{userId}/keys` (line 1058)
- `POST /api/v1/admin/compliance/sessions/{sessionId}/revoke` (line 1049)

**Acceptance criterion violated:** Story AC#6 — *"After deactivation, any Matrix API or Admin API request using the deactivated user's JWT token returns 401 M_UNKNOWN_TOKEN. This is achieved by adding an `is_active` check in the Go gateway's JWT middleware (`JWTMiddleware` in `gateway/internal/middleware/auth.go`)..."* — is satisfied only for the new `/api/v1/admin/users/{userId}/(de)activate` paths.

**Mitigations that exist (do not close the gap):**
- `Nebu.Session.PgStore.invalidate_session/1` deletes `sync_tokens` rows. `/sync` long-poll fails after the row is gone — but only on next sync cycle, not for stateless API calls.
- The `validate_token` Elixir handler (`core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:604`) returns `{:error, :deactivated}` for is_active=false users — but this is only on **new login**, not on already-issued tokens.
- The Go denylist would catch tokens explicitly logged out — but deactivation does NOT add to the denylist; it relies on the (unwired) `is_active` check.

**Attack scenarios:**
1. Insider threat — admin deactivates Eve at 09:00. Eve has a JWT issued at 08:55 (1h expiry). Until 09:55 Eve continues posting, sending invites, listing devices, calling room directory — undetected.
2. Compromised account — security incident triggers admin deactivation. The attacker (in possession of Eve's JWT) keeps writing for the JWT TTL. Deactivation gives a false sense of containment.
3. Token theft + admin role — if the attacker has stolen an instance_admin JWT, deactivating that admin does NOT prevent further `/users/{id}/keys` deletions or anonymizations until JWT expiry.

**Why HIGH, not CRITICAL:**
- The user cannot **acquire** a new token (Elixir `validate_token` rejects deactivated users at login).
- JWT TTL bounds the window (typically ≤1 hour).
- No Press-Release-on-Nebu scenario by itself; combined with token theft, would escalate.

**Why not MEDIUM:**
- This is the entire point of the story (AC#6 explicit). Shipping it half-implemented contradicts the story's design intent and is a regression of the security posture admin operators expect when they click "deactivate".
- The fix is one line.

**Fix:**

```go
// main.go:441 — replace
jwtMiddleware := middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, tokenStore, serverName)

// with
baseJWT := middleware.JWTMiddleware(oidcProvider, cfg.OIDCClientID, cfg.OIDCClaimRole, tokenStore, serverName)
jwtMiddleware := middleware.WithUserStatusCheck(baseJWT, &middleware.DBUserStatusChecker{DB: bootstrapDB})
// remove the duplicate `jwtWithStatusCheck` variable at line 1126; just pass jwtMiddleware
```

This makes every authenticated route inherit the `is_active` check by default. The 60s TTL cache is shared across all routes — acceptable, the cache key is per-userID and `sync.Map` handles concurrency.

---

### MEDIUM-1 — State-machine race: TOCTOU between GetUserStatus and DeactivateUser (CWE-367, CWE-841)

**File:** `gateway/internal/api/server.go:236-249`, `gateway/internal/api/deactivation_repo.go:67-77`

The deactivation handler reads `is_active` (line 236), then issues a separate UPDATE (line 249). Two concurrent admin requests can both observe `is_active=true` and both proceed to UPDATE; the second UPDATE silently overwrites `deactivated_at` and `deactivation_reason` with the second admin's values, and the second `audit.LogEvent` records a deactivation that semantically did not happen (the user was already deactivated by request #1).

**Reactivation is worse:** In `server.go:282-307`, two concurrent reactivation requests (admin clicks twice, or two admins act simultaneously) can both UPDATE — but the second UPDATE clears `deactivation_reason` for an already-active user, which is a no-op DB-side. The audit log emits two `user_reactivated` events.

A more concerning case for reactivation: **deactivate → reactivate within the same window**. If admin A deactivates at T=0 (sees is_active=true, UPDATE, gRPC InvalidateSessions, audit) and admin B reactivates at T=0+ε (sees is_active=false, UPDATE), both succeed. The user is now active but their sessions were invalidated. Admin B's audit log says "reactivated" but there is no record that A's invalidation happened in between. Forensics / RSP timeline is corrupted.

**Recommendation:**
Use a conditional UPDATE that includes the precondition:
```sql
UPDATE users SET is_active = false, deactivated_at = $2, deactivation_reason = $3
WHERE user_id = $1 AND is_active = true
```
Check `RowsAffected()` — if 0, return 409 to the second caller. This is defense-in-depth at the DB layer; the application-layer check stays for clearer error messages.

**Why MEDIUM, not HIGH:**
- Both concurrent admins must hold `instance_admin` role — small attacker pool.
- Outcome converges (user is deactivated either way).
- Audit duplication, not audit forgery.

**Why not LOW:**
- Reactivation race during incident response is a realistic concurrent-admin scenario (two responders).
- The audit trail loses temporal ordering, which RSP cares about.

---

### MEDIUM-2 — DBUserStatusChecker fails open on `sql.ErrNoRows` (CWE-841)

**File:** `gateway/internal/middleware/auth.go:35-42`

```go
func (c *DBUserStatusChecker) IsUserActive(ctx context.Context, userID string) (bool, error) {
    var isActive bool
    err := c.DB.QueryRowContext(ctx, "SELECT is_active FROM users WHERE user_id = $1", userID).Scan(&isActive)
    if errors.Is(err, sql.ErrNoRows) {
        return true, nil // unknown user: fail open
    }
    return isActive, err
}
```

When the JWT `sub` resolves to a userID that does not exist in the `users` table, the middleware returns `isActive=true` and lets the request through. The justification ("let downstream handle 404") is plausible but creates an attack surface:

1. Many Matrix API endpoints don't 404 on unknown users — they 200/204 (`/sync`, `/keys/query`, `/account/whoami`, `/profile/{userId}` for own profile, `/devices`).
2. An attacker presenting a JWT with a `sub` claim for which no DB row exists (e.g., revoked Keycloak user that Nebu never provisioned, or a typo'd OIDC `sub` claim) is granted authenticated access.
3. The "fail-open" pattern conflates "this is a deactivation check" with "this is the only check that ensures the user exists". Existing code does NOT have a separate "user must exist in users table" gate before reaching protected endpoints.

**Recommendation:**
Return `false` for `sql.ErrNoRows` and let the middleware reject with 401 `M_UNKNOWN_TOKEN`. A user that doesn't exist in our DB has no active account by definition. Add a unit test for this case.

If product wants to keep "fail-open for unknown users" — at minimum, add a structured log: `slog.Warn("user_status_check: user not found in users table — failing open", "user_id", userID)`.

**Why MEDIUM, not HIGH:**
- The race window only exists for users with a valid OIDC token — not all attackers.
- Existing flows (`validate_token`, `JWTMiddleware`'s sub→userID mapping) typically provision the row before any authenticated request is processed.

**Why not LOW:**
- The defensive boundary is being lifted in the wrong direction. This is a "principle of least authority" violation — fail-closed is the default for auth checks.

---

### LOW-1 — Cache TTL allows up to 60s of stale-deactivation reuse (CWE-613, accepted trade-off)

**Files:**
- `gateway/internal/middleware/auth.go:50-77` (cache implementation)
- Story Dev Notes line 654 acknowledges: *"a deactivated user could still make requests for up to 60 seconds after deactivation before the cache expires. This is an explicit design trade-off..."*

This is documented and accepted. Recording it for the audit trail. The 60s window is small enough to be acceptable for non-emergency deactivation; for security-incident-response cases, AC implementations can clear the cache via the proposed `Invalidate(userID)` API (see code review MINOR-2).

**Compounding factor:** The cache is per-process. In a multi-replica deployment, the actual maximum bypass window is `60s × replicas` (in worst case the user hits each replica during its own cache TTL window). Document this in the Phase-2 distributed-cache plan.

**No action required for MVP.**

---

### LOW-2 — Unbounded `deactivation_reason` text persisted to DB and audit (CWE-770, CWE-400)

**Files:**
- `gateway/internal/api/server.go:229,232` — `reason` is trimmed and length-checked for **minimum** (≥10) but no maximum
- `gateway/internal/api/deactivation_repo.go:67-77` — `reason` written to `users.deactivation_reason TEXT` (PostgreSQL TEXT has no length limit by default)
- `gateway/internal/api/server.go:264-266` — `reason` included in audit metadata; `audit.MaxMetadataJSONBytes = 16 KiB` would silently drop if exceeded (`gateway/internal/audit/writer.go:38`)

An admin sending a 10 MB reason would: (a) bloat the `users` row (PostgreSQL TOAST handles it but query latency rises), (b) cause `audit.LogEvent` to drop the metadata (so the audit row says "user_deactivated" but loses the reason — RSP gap).

**Recommendation:** Cap `reason` at e.g. 4 KiB in the handler before any DB write. Also enforce in OpenAPI: `maxLength: 4096`.

**Why LOW:** Requires admin role; effect is internal DoS / quality-of-data, not data exfiltration or auth bypass.

---

### INFO-1 — Reactivation does not clear status cache (UX/security trade-off)

**File:** `gateway/internal/api/server.go:274-316`

After successful reactivation, the per-process status cache may still hold `isActive=false` for up to 60s. A reactivated user with a fresh JWT will be rejected with 401 `M_UNKNOWN_TOKEN "Account deactivated"` despite being active again.

**Security posture:** Fail-closed (good). But UX is broken — admin clicks "reactivate" and tells the user "log in again", and the user's first attempt fails confusingly.

**Recommendation:** Add an `Invalidate(userID)` method to the closure-cached `WithUserStatusCheck` and call it from `ReactivateAdminUser`. Out of scope for this security review — flagged for the dev backlog.

---

### INFO-2 — gRPC `InvalidateUserSessions` failure logs warning but does not retry

**Files:**
- `gateway/internal/api/server.go:254-259` — best-effort: `slog.Warn` and continue
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:667-677` — Elixir handler raises `GRPC.RPCError` with `internal` status on `{:error, _}`

If the gRPC call fails (Core down, network flap, DB transaction conflict in `Nebu.Session.PgStore.invalidate_session`), the user is marked deactivated in `users.is_active=false` but their ETS session entry persists. The 60s cache delays the deactivation effect by another 60s (now the bypass window is until JWT expiry, not 60s).

**Mitigations:**
- The HIGH-1 fix above narrows the bypass to JWT-expiry-time, which is bounded.
- An eventual consistency reconciler (a periodic job that scans `users WHERE is_active=false AND last_seen_at > deactivated_at`) would catch missed invalidations — out of scope.

**No action required for MVP** — but document the residual risk in the runbook.

---

## Nebu Invariants Check

| Invariant | Status | Notes |
|-----------|--------|-------|
| Compliance RSP (Read-Same-Process) | OK | Migration adds columns, no separate table, atomic UPDATE |
| Audit immutability | OK | Audit emits via existing `audit.LogEvent` (never-raise contract preserved) |
| OIDC validation chain | OK | `validate_token` already rejects deactivated at login; this story adds the runtime check |
| Crypto primitives | N/A | No new crypto in this story |
| Secrets hygiene | OK | `reason` is admin-supplied; not a secret |
| RLS boundary | N/A | No new tables; existing `users` table RLS unchanged |
| Matrix power-level enforcement | N/A | Out of scope |

---

## Stack-Specific Checks

**Go:**
- Context propagation: OK — handlers thread `ctx` to gRPC and DB calls.
- Error wrapping: OK — `errors.Is(err, ErrUserNotFound)` used correctly.
- Concurrency: `sync.Map` for cache is safe; closure-scoped per middleware instance avoids the package-global pitfall (Debug Log entry #4).
- SQL injection: parameterised queries throughout (`$1`, `$2`, `$3`).

**Elixir:**
- Pattern matching: OK — `%Core.InvalidateUserSessionsRequest{}` exhaustive.
- `let it crash`: OK — `GRPC.RPCError` raised on `{:error, _}`, supervisor restarts the gRPC handler.
- `Application.put_env` in tests with `async: true` — flagged in test review (test isolation, not a security issue).

**PostgreSQL:**
- Migration is idempotent for new installs; `down.sql` uses `IF EXISTS` correctly.
- New columns are nullable — no backfill required, no row rewrites.

---

## Severity Counts (Summary)

| Severity | Count |
|----------|------:|
| CRITICAL |     0 |
| HIGH     |     1 |
| MEDIUM   |     2 |
| LOW      |     2 |
| INFO     |     2 |

**Classification: HIGH** — HIGH-1 must be fixed before merge. Once fixed, story is cleared.

---

## Recommended Actions (Priority Order)

1. **HIGH-1 (BLOCKER):** Wrap `jwtMiddleware` with `WithUserStatusCheck` at the construction point in `main.go:441` so all 52+ authenticated routes inherit the deactivation check. Add an integration test that asserts a deactivated user is rejected on at least `POST /_matrix/client/v3/createRoom` and `GET /_matrix/client/v3/sync`.
2. **MEDIUM-1:** Add WHERE-clause guards to UPDATE in `dbDeactivationRepo.DeactivateUser` and `ReactivateUser`; check `RowsAffected()` and surface 409 on race.
3. **MEDIUM-2:** Reconsider fail-open behaviour on `sql.ErrNoRows` — recommend fail-closed with structured warning log.
4. **LOW-2:** Cap `reason` length at 4096 chars in handler + OpenAPI schema.
5. **INFO-1:** Add `Invalidate(userID)` API to status cache; call from reactivate handler.

---

## Audit Trail

This report is the immutable record of Kassandra's review of Story 6-5 staged diff on 2026-05-01. Findings are deterministic given the diff state at review time. Future fixes should reference HIGH-1 by report path.

— Kassandra
