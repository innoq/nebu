# Security Review — Epic 7b (Stories 7-17 … 7-30) — 2026-04-30

**Agent:** Kassandra
**Diff base:** `git diff c960068..HEAD` (epic-7b — Matrix API gap stories)
**Scope:** 14 stories — admin-hardening (7-17 / 7-18) + 12 new Matrix CS-API endpoints
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

Epic 7b expands the Matrix attack surface by ~20 new endpoints, four new tables and six new gRPC RPCs. The auth wiring is consistent, JWT ownership checks are in place on every userId-path-parametrised endpoint, and SQL is parameterised throughout. The story-7-17 / 7-18 admin hardening (CSRF + body-limit on all admin POSTs, flash allowlist on all admin GETs) is correctly applied across every wired route.

Three findings carry over the boundary into Epic 7b that were not present before:
1. The new moderation gRPC RPCs trust the request-body `caller_id` instead of the trusted gRPC metadata `x-user-id` — inconsistent with the rest of the Core handlers and a defense-in-depth gap.
2. Story 7-19's IDOR fix on `get_room_state` made the function correctly user-scoped, but the gateway's internal event-fanout goroutine still calls it without user metadata — the fanout will silently drop every event in production.
3. Push rules / pushers / notifications / event_context / context routes carry no rate limit beyond the global default — an authenticated user can spam them.

Two functional dead-ends fail closed (DELETE /devices UIA cannot be passed in production; room_account_data RLS policy filters all rows because GUC is never set) and are flagged for awareness rather than as exploitable risks.

No CRITICAL findings. No new SQLi, XSS, missing-auth, or hardcoded-secret regressions. Migrations 000029–000032 do not relax audit immutability or compliance scope.

## Findings

### [HIGH] Moderation gRPC handlers trust request-body `caller_id` instead of trusted metadata

- **CWE / OWASP:** CWE-287 / A01:2021 (Broken Access Control — defense-in-depth)
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1207-1276` (kick_user), `:1289-1354` (ban_user), `:1366-1420` (unban_user); `proto/core.proto:331-355` (KickUserRequest/BanUserRequest/UnbanUserRequest with `caller_id` field).
- **Beschreibung:** `kick_user/2`, `ban_user/2` and `unban_user/2` extract the actor's identity from `request.caller_id` (a request-body field) rather than from `Nebu.Grpc.Metadata.trusted_identity(stream)`. Every other state-changing Core handler (`set_power_levels`, `get_messages`, `get_room_state`, etc.) uses the metadata path. The Go gateway sets both fields from the same JWT claim, so today the values agree; the handler is nonetheless one bug away from accepting an attacker-supplied `caller_id` that disagrees with the authenticated session — for instance, if a future refactor in the gateway forwards a client-supplied user_id verbatim.
- **Impact:** A gateway bug or future feature that forwards client-supplied `caller_id` would let any authenticated user execute kick/ban with an arbitrary actor identity, including bypassing power-level checks by impersonating a higher-privileged member. The gRPC channel is PSK-protected so external attackers cannot reach Core directly today, but the inconsistency erodes the architectural rule that Core trusts only metadata from the gateway.
- **Empfehlung:** In each of the three handlers, replace the line `caller_id = request.caller_id` with `{caller_id, _system_role} = Nebu.Grpc.Metadata.trusted_identity(stream)` (matching `set_power_levels/2`). Optionally remove the `caller_id` field from the proto messages once the metadata-based path is verified, or keep it for telemetry but never as the auth principal. Add an ExUnit test that sends a request with `caller_id = "@victim:nebu.local"` while the metadata identifies `@attacker:nebu.local` and asserts permission_denied.
- **Referenz:** OWASP ASVS V4.1.5 ("verify access controls fail securely"), NIST AC-3, internal architecture rule "auth principal arrives only via metadata".

### [MEDIUM] Story 7-19 IDOR fix breaks server-internal event fanout

- **CWE / OWASP:** CWE-285 (Improper Authorization) — operational regression
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:713-735` (membership check requires `caller_id`); `gateway/cmd/gateway/main.go:56-62` (server-internal call without user metadata); `gateway/internal/buffer/drain.go:32-40` (silent skip on error).
- **Beschreibung:** Story 7-19 added a correct membership check to `get_room_state` so that callers who are not joined receive `permission_denied`. The Go gateway, however, calls the same RPC from the EventBus fanout goroutine (`coreRoomStateLookup.GetRoomState`) without `coregrpc.WithUserMetadata`. With no `x-user-id` header, `Nebu.Grpc.Metadata.trusted_identity/1` returns `nil`, the membership check fails, and the call returns `permission_denied`. `RouteEventToUsers` then logs a warning and drops the event — every published event silently disappears.
- **Impact:** Two-fold. (a) Functional regression: `/sync` long-poll wake-ups stop working — clients only see events on cold polls. (b) Defense-in-depth: the system relies on a fragile separation between "server-internal callers" and "user-scoped callers" with no test or compile-time guarantee. Any future server-internal RPC that requires room data has to pick the right pattern silently.
- **Empfehlung:** Either (a) add an explicit `system_role: "system"` shortcut in `get_room_state` that bypasses the membership check when the metadata identifies a system caller (matches the existing `system_role` propagation in `WithUserMetadata`), or (b) add a separate `get_room_members_for_fanout` unary RPC that does no membership check and is callable only by the system role. Add an integration test that confirms an event published to a room is fanned out to its members in production.
- **Referenz:** CWE-285, Nebu architecture rule "Core trusts only metadata".

### [MEDIUM] No rate limit on new authenticated Matrix endpoints

- **CWE / OWASP:** CWE-770 / A04:2021 (Insufficient Resource Allocation)
- **Datei:** `gateway/cmd/gateway/main.go:474-489` (pushrules/pushers), `:687-688` (event_context), `:696-697` (notifications), `:836-837` (joined_members), `:846-854` (room_state), `:863-864` (room_aliases).
- **Beschreibung:** The new endpoints sit behind only `jwtMiddleware`, with no per-IP or per-user rate limiter. `GET /notifications` can return up to 200 rows per call and is paginated — an attacker with a valid token can run thousands of paginated reads per second. `PUT /pushrules/.../actions` writes JSONB to PostgreSQL on every call with no bound. `GET /rooms/{roomId}/context/{eventId}` triggers a gRPC call to Core for every request.
- **Impact:** Single-user DoS — an authenticated client can exhaust gateway → Core gRPC capacity, fill the audit log, or cause noisy retries on the database. Bounded by being authenticated, so the blast radius is the user's own session, but a credential leak amplifies it dramatically.
- **Empfehlung:** Add a `mediumRL` (30/min, burst 10) wrapper on `GET /pushrules/`, `GET /notifications`, `GET /rooms/.../context/.*`, `GET /rooms/.../joined_members`, `GET /rooms/.../state`, `GET /rooms/.../aliases`. Apply `strictRL` to `PUT /pushrules/.../{actions,enabled}` and `POST /pushers/set` since each write hits PostgreSQL.
- **Referenz:** OWASP ASVS V11.1.1 ("authenticated rate limits"), CWE-770.

### [MEDIUM] Notifications + push_rules + pushers tables rely solely on application-level user_id filter (no RLS)

- **CWE / OWASP:** CWE-285 / A01:2021 (Broken Access Control — defense-in-depth)
- **Datei:** `gateway/migrations/000031_notifications.up.sql:11-13` (RLS deferred), `gateway/migrations/000032_push_rules_pushers.up.sql:1-5` (no RLS).
- **Beschreibung:** Three new tables holding user-scoped data ship without `ENABLE ROW LEVEL SECURITY` and without `FORCE ROW LEVEL SECURITY`. The Go store layer uses `WHERE user_id = $1` consistently, so today every query is correctly scoped; but a single missing WHERE clause in a future query, or an SQL builder that forgets to inject the filter, would expose every user's notifications, pushers and push rules to any authenticated user.
- **Impact:** Defense-in-depth gap. Today the application-layer filter is in place on every query I traced (notifications_store.go, push_rules_store.go, pushers_store.go). Tomorrow's "let me add a debug query" or a tooling shortcut would silently leak the entire notifications history of every user. Compare to `room_account_data` (000029) which adds RLS — the new tables move in the opposite direction without a stated reason.
- **Empfehlung:** Add `ENABLE ROW LEVEL SECURITY` + `FORCE ROW LEVEL SECURITY` + a policy `USING (user_id = current_setting('app.user_id', true))` on `notifications`, `push_rules`, `pushers` in a follow-up migration. Pair with the in-flight work to wire `SET LOCAL app.user_id = $1` per request in the pgx pool — until then RLS would block reads (see `room_account_data` finding below). At minimum: add a recurring SQL grep test in CI that asserts every `SELECT/UPDATE/DELETE FROM notifications|push_rules|pushers` statement contains `user_id`.
- **Referenz:** OWASP ASVS V4.1.1, NIST AC-3.

### [MEDIUM] room_account_data RLS policy is enforced but `app.user_id` GUC is never set — table is effectively unreadable

- **CWE / OWASP:** CWE-285 (Improper Authorization) — fail-closed regression
- **Datei:** `gateway/migrations/000029_room_account_data.up.sql:23-32`; no `SET LOCAL app.user_id = ...` anywhere in `gateway/internal/db/account_data_store.go` or in `bootstrapDB` connection setup.
- **Beschreibung:** Migration 000029 enables RLS with `FORCE ROW LEVEL SECURITY` and a policy `USING (user_id = current_setting('app.user_id', true))`. The runtime gateway runs as `nebu_app` (non-owner, non-BYPASSRLS), so RLS is always enforced. The gateway never executes `SET LOCAL app.user_id = $1` per request — neither in the pgx pool initialisation nor in `PutAccountData` / `GetAccountData`. `current_setting(..., true)` returns `NULL`, the comparison `user_id = NULL` evaluates to NULL (treated as false in USING), and every read returns zero rows / every write violates the WITH CHECK and is rejected.
- **Impact:** Account-data feature is silently broken in production — `/sync` returns no `m.tag` or `m.fully_read` events even after a `PUT`. Security impact is fail-closed (no cross-user leak), but combined with finding above this is a known-broken gap that the team has explicitly documented in the migration 000031 comment ("RLS via app.user_id GUC is deferred").
- **Empfehlung:** Wire `SET LOCAL app.user_id = $1` per request into the pgx pool (acquire-hook in pgxpool, or a small `withUserContext(ctx, userID, fn)` helper called by every store method). Add an integration test that performs `PUT` then `GET` against the same row and confirms it round-trips. Until the GUC is wired, do not extend RLS to additional tables — this finding is the proof that adding RLS without GUC wiring breaks the feature silently.
- **Referenz:** PostgreSQL RLS docs §5.8, OWASP ASVS V4.1.5.

### [MEDIUM] DELETE /devices UIA flow has no production code path that completes the session

- **CWE / OWASP:** CWE-693 (Protection Mechanism Failure) — fail-closed
- **Datei:** `gateway/internal/matrix/uia.go:90-107` (complete), `:134-149` (approveUIASession only used by tests); no caller in the SSO callback handler at `gateway/internal/matrix/login.go` or anywhere else outside `_test.go` files.
- **Beschreibung:** `RequireUIA` issues a 401 challenge with a session UUID and only accepts the request once the session is marked `completed=true`. Marking happens via `uiaStore.complete/2` (or `approveUIASession/2`). Both functions are referenced **only from test files** — the SSO callback handler does not call them. A real Matrix client receiving the 401 challenge has no way to satisfy the SSO stage; the second request hits `uiaStore.check/2` which finds the session in the `completed=false` state and reissues the challenge.
- **Impact:** Functional dead-end — clients cannot delete their own devices in production. Security-wise this is fail-closed (UIA is a soft gate; the JWT auth already restricts to the device owner). The risk is "this looks secure but is actually impossible" — a future operator might disable UIA wholesale to "fix the bug" and accidentally remove the entire challenge.
- **Empfehlung:** Either (a) wire `approveUIASession/2` into the SSO callback handler so a real OIDC re-auth marks the session completed and the original DELETE/POST can proceed, or (b) accept that Nebu doesn't yet support UIA and replace the m.login.sso challenge with `m.login.dummy` (matching the cross-signing-upload pattern at `gateway/cmd/gateway/main.go:608-615`) until proper SSO-UIA is implemented. Document the chosen path in the story's implementation notes.
- **Referenz:** Matrix CS API §6.1, OWASP ASVS V2.2.1.

### [MEDIUM] `did` JWT claim is consumer-controlled but trusted as the "current device" identity for protection checks

- **CWE / OWASP:** CWE-345 (Insufficient Verification of Data Authenticity)
- **Datei:** `gateway/internal/middleware/auth.go:131` (extract `did`), `:147` (insert into context); `gateway/internal/matrix/devices.go:121-127` (currentDeviceID), `:258-262` (current-device protection check).
- **Beschreibung:** The "do not delete the current device" check at `devices.go:258-262` compares the `deviceId` path parameter against the JWT's `did` claim from the context. Dex (and most OIDC providers) does not include a `did` claim. If `did` is empty, the entire current-device protection silently no-ops — the user can delete the device that issued the very token they are using, leaving them in a confusing logged-in-but-deleted state. If a future provider DOES emit a `did` claim, an attacker who modifies their OIDC issuer (e.g. when SSO_REDIRECT_SCHEMES is widened) could choose any `did` value.
- **Impact:** Today: the protection is a no-op (UIA bug above means DELETE never runs anyway). When UIA is fixed: a user can accidentally delete their own active session because the protection silently passes. No cross-user impact — strictly self-inflicted. The "did stays empty in MVP" assumption is the most fragile part.
- **Empfehlung:** Replace the JWT-claim-based current-device detection with a server-side mapping: when the gateway issues an access token (or first sees one), record `(token_jti, device_id)` in the sessions table; the middleware then resolves the request's `device_id` from `jti → device_id` via DB lookup. This removes consumer-controlled input from the auth-protection path. Until then: log a warning when `did` is empty so operators see the gap in production.
- **Referenz:** OWASP ASVS V3.5.1.

### [LOW] Inconsistent rate-limit on profile sub-field GETs vs full profile

- **CWE / OWASP:** CWE-307 (Improper Restriction of Excessive Authentication Attempts) — adjacent
- **Datei:** `gateway/cmd/gateway/main.go:896-902`.
- **Beschreibung:** `GET /profile/{userId}` uses `mediumRL` (30/min, burst 10). The two new sub-field GETs `GET /profile/{userId}/displayname` and `GET /profile/{userId}/avatar_url` use `looseRL` (300/min, burst 100). Both endpoints leak the same data (displayname, avatar_url) and are unauthenticated. An attacker probing for valid usernames can do so at 10× the rate via the sub-field endpoints.
- **Impact:** User-enumeration oracle is 10× faster via sub-field endpoint than via the parent. Bounded by the cache-control 60s headers on 404, which mitigate but do not eliminate it.
- **Empfehlung:** Apply `mediumRL` to the two sub-field handlers for parity. One-line change in `cmd/gateway/main.go`.
- **Referenz:** OWASP ASVS V11.1.4.

### [INFO] All admin POST routes consistently wrapped with `bodyLimit64KiB(csrf(sessionGuard(...)))`

- **Datei:** `gateway/cmd/gateway/main.go:303-367`.
- **Beschreibung:** Spot-checked all 11 admin POST routes (logout, dashboard, users — display-name/role/deactivate/reactivate, rooms — name/archive/unarchive, config, role-mapping, compliance approve/reject, bootstrap, claim-select, compliance-revoke). Every state-changing admin POST has the full `bodyLimit64KiB → csrf → sessionGuard` chain. Story 7-17's hardening goal achieved.

### [INFO] All admin GET handlers route flash through `sanitizeFlash` allowlist

- **Datei:** `gateway/internal/admin/flash.go:5-29`, plus call sites in `users.go:94`, `config.go:24`, `role_mapping.go:31`, `rooms.go:92`, `compliance_handler.go:28`.
- **Beschreibung:** Story 7-18 closes the reflected-XSS / parameter-injection vector via a strict 11-entry allowlist with an 80-character cap. Any value not in the allowlist is silently dropped. Closes the gap I would have flagged otherwise (`?flash=<script>...</script>` injected into trusted templates).

### [INFO] Story 7-19 IDOR fix on `get_room_state` is correct for legitimate user callers

- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:713-735`.
- **Beschreibung:** Membership check via `Nebu.Grpc.Metadata.trusted_identity/1` correctly closes the IDOR — a non-member calling GET /rooms/{roomId}/state via the gateway gets `permission_denied → 403 M_FORBIDDEN`. The fix is the right shape; only the server-internal-caller path needs a separate accommodation (see MEDIUM finding above).

### [INFO] Migrations 000029–000032 do not relax audit immutability

- **Datei:** `gateway/migrations/000029…000032_*.up.sql`.
- **Beschreibung:** No `ALTER TABLE audit_log ADD UPDATE` / `DELETE` / `OWNER TO`. Audit-log immutability invariant is preserved.

### [INFO] No new hardcoded secrets, no `math/rand` for security values, no `InsecureSkipVerify`

- **Datei:** Full diff scanned.
- **Beschreibung:** UIA session IDs use `crypto/rand` (uia.go:57-63). The cross-signing UIA stub at `cmd/gateway/main.go:608-615` also uses `crypto/rand`. No `tls.Config{InsecureSkipVerify: true}` introduced. No production secrets present in literals.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no new compliance handlers) |
| `reason` field on compliance access         | ✅ (no new compliance writes) |
| Audit-log immutability                      | ✅ (migrations 000029–000032 do not touch audit) |
| `instance_admin` notification (if in-scope) | ✅ (no new escalating compliance access) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (re-uses existing JWTMiddleware) |
| Matrix Power Level checks                   | ⚠️ (kick/ban/unban check power levels in Elixir, but caller_id source is body not metadata — see HIGH finding) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (no TLS config changes — pre-existing TLS 1.2 minimum is unchanged) |
| AES-256-GCM correctness                     | ✅ (no new crypto code paths) |
| Ed25519 verify-before-accept                | ✅ (kick/ban/unban events are signed with Ed25519 in Core; existing pattern) |
| No secrets in logs / error messages         | ✅ (slog calls use structured fields, no token/credential leakage) |

## Dependency Scan

No `go.sum`, `go.mod`, `mix.lock`, or `mix.exs` changes in this diff — section omitted.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 6 |
| LOW       | 1 |
| INFO      | 5 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning. The HIGH finding (moderation handlers trust body `caller_id`) and the six MEDIUM findings should be tracked as follow-up stories before the next release. Two of the MEDIUM findings (room_account_data RLS, DELETE /devices UIA) are already known-broken-fail-closed and may be acceptable to defer with written justification; the remaining four (rate limits, RLS on new tables, fanout regression, did-claim trust) deserve concrete follow-ups.

**Recommended follow-up stories:**
1. **7-32 (HIGH):** Switch moderation gRPC handlers to use `Nebu.Grpc.Metadata.trusted_identity/1` instead of `request.caller_id`; add ExUnit test for caller_id ≠ metadata identity.
2. **7-33 (MEDIUM):** Fix server-internal event fanout — either system_role bypass in `get_room_state` or a separate `get_room_members_for_fanout` RPC.
3. **7-34 (MEDIUM):** Add per-IP rate limits to all new authenticated Matrix endpoints (notifications, pushrules, pushers, event_context, joined_members, room_state, room_aliases).
4. **7-35 (MEDIUM):** Wire `SET LOCAL app.user_id = $1` per request in the pgx pool; enable RLS on notifications, push_rules, pushers; add round-trip integration test for room_account_data.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
