# Security Review — Epic 6: Admin API (SEC Gate 2 Epic-End) — 2026-05-02

**Agent:** Kassandra
**Diff base:** `git diff a7a806f..HEAD` (entire Epic 6 — 11 stories, 120 files, +28 035 / −584)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-opus-4-7[1m]`

## Executive Summary

Epic 6 introduces the entire programmatic Admin API surface — 14 new HTTP routes under
`/api/v1/admin/*`, four new gRPC RPCs (`InvalidateUserSessions`, `UpdateRoomSettings`,
`ArchiveRoom`/`UnarchiveRoom`, `InvalidateAllAdminSessions`), four new migrations
(`users.deactivated_at`/`reason`, `role_overrides`, `rooms` admin columns, `room_defaults`),
and a DB-backed role-override mechanism layered on top of OIDC JWT claims. The
auth chain is mostly correct: every admin route is wired through
`jwtMiddleware → WithUserStatusCheck → RequireRole("instance_admin", checker)`, the
gRPC endpoint inherits `Nebu.Grpc.AuthInterceptor` (PSK with constant-time compare),
SQL is parameter-bound throughout including the dynamic `UPDATE rooms` builder, and
`oidc_client_secret` is correctly write-only with AES-256-GCM at rest plus a generic
JSON 5xx envelope at the router level (the per-story HIGH from 6-10 was fixed in
`router.go:42–47`).

**Two HIGH findings remain** at epic close. **HIGH-1** (cross-cutting): the combination of
`WithUserStatusCheck` failing open on DB error and `RequireRole`'s role-override
fail-open path produces a privilege-escalation window during a PostgreSQL outage —
any authenticated regular user (no `instance_admin` JWT claim) hitting `/api/v1/admin/*`
is permitted through. Both fail-open paths are deliberate, tested, and individually
defensible, but their composition was not analysed end-to-end. **HIGH-2** is the
unfixed Story 6-9 finding: the gateway fail-open on `GetRoomStatus` plus the absence
of a Core-side `archived` check in `Nebu.EventDispatcher.Server.send_event` lets a
single message land after a visible archive — the per-story review documented this
and the epic ships with the gap unaddressed.

Five MEDIUM findings cluster around two patterns: (a) `RegisterAdminRoutes` has no
`bodyLimit` and no `adminRL` rate limiter wired anywhere — every PATCH/POST/PUT in the
new admin surface is unbounded for `instance_admin` callers, (b) the deactivation
flow has a TOCTOU race because `UPDATE users SET is_active=false` lacks a
`WHERE is_active=true` precondition, and `DBUserStatusChecker.IsUserActive` fails open
on `sql.ErrNoRows` — a JWT for a never-provisioned user passes the active-check.

The four new migrations introduce two tables (`role_overrides`, `room_defaults`)
without RLS. `role_overrides` is the more sensitive of the two (it determines who
can call admin endpoints) and a defence-in-depth `ENABLE ROW LEVEL SECURITY`
+ `FORCE ROW LEVEL SECURITY` would restrict bypass via the migration role —
flagged MEDIUM. No CRITICAL findings; no audit-table grant changes; no hardcoded
secrets; AES-256-GCM nonce handling is correct everywhere it appears.

## Findings

### [HIGH] Composed fail-open in `WithUserStatusCheck` + `RequireRole` — privilege escalation under DB outage

- **CWE / OWASP:** CWE-285 (Improper Authorization) + CWE-754 (Improper Check) / A01:2021
- **Datei:**
  - `gateway/internal/middleware/auth.go:38–42` (`IsUserActive` returns `(true, nil)` on `sql.ErrNoRows`)
  - `gateway/internal/middleware/auth.go:67–69` (`makeStatusChecker` returns `(true, err)` on DB error)
  - `gateway/internal/middleware/auth.go:101–103` (`WithUserStatusCheck` allows the request through on error, only logs warn)
  - `gateway/internal/api/middleware.go:82–88` (`RequireRole` allows the request through when `checker` returns an error, only logs warn)
- **Beschreibung:** Two distinct fail-open paths stack on the admin API. The OIDC
  status check fails open on every DB error and on `sql.ErrNoRows` for unknown users.
  The DB-backed role override check fails open on every DB error. Both decisions are
  individually defensible at the per-story level (Story 6-5 MEDIUM-2, Story 6-6 design
  note: "DB outage must not lock out all users"). Composed, the sequence
  `JWT-claim role mismatch → role-override DB lookup → fail-open` lets any
  authenticated, role-less user through to `instance_admin`-gated routes whenever
  the `role_overrides` query errors — for example during pg_bouncer reload,
  replica failover, or a transient connection-pool exhaustion. The path runs the
  full admin handler with `actorID` set to the regular user's ID, so deactivations,
  role grants, room archives, and config patches succeed and emit a (correctly attributed)
  audit row. The same JWT immediately stops working again once the DB recovers.
- **Impact:** A motivated attacker who controls a regular OIDC account can probe
  the admin API in a tight loop and hit the window the next time PostgreSQL hiccups.
  In a multi-replica deployment the cache is per-process, so a single replica with a
  stale connection pool is enough. Reputational test: a CVE titled "Nebu admin API
  privilege escalation during DB outage" would land on a security advisory. This
  passes the Rufschädigungs-Test for HIGH not CRITICAL only because (a) the
  precondition is a real DB issue (not always reachable on demand), (b) the JWT
  claim path is the primary gate and it does not fail open, and (c) every admin
  action is audit-logged so post-incident reconstruction is possible. Picking the
  lower of two severities per triage rubric.
- **Empfehlung:** Pick **fail-closed for the role override path** specifically, even if
  the user-status path keeps fail-open. The role-override semantics — "this user has
  no JWT admin claim, so we are explicitly looking up whether they have a DB-granted
  role" — must not interpret "DB error" as "yes". Replace `gateway/internal/api/middleware.go:82–88` with:
  ```go
  if err != nil {
      slog.Warn("RequireRole DB override check failed — denying", "user_id", userID, "role", role, "err", err)
      writeAdminError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "role authorization unavailable — retry")
      return
  }
  ```
  503 (rather than 403) is honest about the cause and tells the client to retry.
  Add a regression test that asserts a DB-error mock returns 503, not 200.
  Separately, reconsider `IsUserActive` returning `(true, nil)` on `sql.ErrNoRows`:
  for the admin API surface the safe answer for an unknown user is "not active";
  the existing 6-5 MEDIUM-2 stands.
- **Referenz:** OWASP ASVS V4.1.5 (deny by default), NIST SP 800-53 AC-3, CWE-285.

### [HIGH] TOCTOU race: `Core.send_event` does not check `archived` — message lands post-archive

- **CWE / OWASP:** CWE-367 / A04:2021 (Insecure Design)
- **Datei:**
  - `gateway/internal/matrix/rooms.go:457–467` (status check + fail-open on DB error)
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:66–112` (`send_event/2` — no archived check)
- **Beschreibung:** Already documented in the per-story review for 6-9 and not fixed
  before the epic close. The gateway checks `rooms.status` once, and on `"active"`
  proceeds to call `Core.SendEvent`. If the admin's `POST /archive` interleaves
  between gateway-check and Core-write, Core happily appends the event because its
  `send_event` only verifies `RoomSupervisor.lookup_room` and membership — never
  consults `rooms_db_module().get_room_status(room_id)`. Worse, `GetRoomStatus`
  failures on the gateway are logged-and-proceeded (`rooms.go:460`, "fail-open"),
  so a transient DB error makes every archived room briefly writable. Append-only
  invariant is preserved (signature, attribution, event_id are all valid), but the
  archive contract — "no new events after archived_at" — is violated.
- **Impact:** A motivated user with a co-conspirator timing can leave one or more
  messages in a room after the admin has visibly archived it. For compliance /
  legal-hold scenarios where archive is an evidentiary cut-off, even one
  post-archive event is a defect. Bounded window, attributable, no cross-room
  spread — HIGH not CRITICAL.
- **Empfehlung:** Add a `case messages_db_module().get_room_status(room_id)` (or
  equivalent via the existing `rooms_db_module()`) at the top of
  `Nebu.EventDispatcher.Server.send_event/2`, after `lookup_room` succeeds,
  raising `GRPC.RPCError{status: permission_denied, message: "room is archived"}`
  on `"archived"`. This becomes the authoritative gate; the gateway pre-check
  remains as a fast-path 403 with documented best-effort semantics. Convert this
  finding to a follow-up story before the next release; the story already exists
  in 6-9's MEDIUM follow-ups but has not been picked up.
- **Referenz:** OWASP ASVS V11.1.4 (state transitions); NIST SI-10.

### [MEDIUM] No body-size limit and no rate limiter on `/api/v1/admin/*` — every state-changing route

- **CWE / OWASP:** CWE-400 / A04:2021
- **Datei:**
  - `gateway/internal/api/router.go:30–102` (`RegisterAdminRoutes` — chains only `jwtMW + RequireRole`, no body limit, no rate limiter)
  - `gateway/cmd/gateway/main.go:1148` (`apihandler.RegisterAdminRoutes(mux, adminSrv, jwtWithStatusCheck, rolesRepo)` — caller passes no limiter)
- **Beschreibung:** The pattern in the rest of the gateway is consistent —
  `bodyLimit64KiB` for state-changing browser admin routes (`main.go:307–341`),
  `bodyLimit1MiB` for Matrix API (`main.go:487–640`), and per-route `adminRL` /
  `complianceRL` / `strictRL` rate limiters. The new programmatic Admin API
  surface (Stories 6-5/6-6/6-8/6-9/6-10 + AC#1 from 6-3) has none of them.
  An authenticated `instance_admin` (or stolen admin JWT) can submit gigabyte
  bodies; the strict-handler decoder reads them fully into memory before
  `len(reason) < 10` rejects the request. The `reason` field on
  `/deactivate` and `/archive` declares only `minLength: 10` — no `maxLength`,
  so the only bound is the upstream proxy's limit. Each per-story review
  (6-3 MEDIUM, 6-8 MEDIUM, 6-9 MEDIUM, 6-5 LOW-2) flagged this with a "track
  as follow-up" recommendation. The follow-up was never picked up; the epic
  ships with the unbounded surface for every route added since 6-3.
- **Impact:** Privileged DoS. Caller must already hold `instance_admin` (or
  the equivalent override). Memory amplification per request, no escape to
  unprivileged users. Repeated PATCH `/admin/config` with a single OIDC field
  also triggers `InvalidateAllAdminSessions` on every call (Story 6-10
  MEDIUM-2 — also unfixed) — sequential per-user `destroy_session` over all
  ETS sessions, pure admin-self-DoS. MEDIUM rather than HIGH because the
  admin trust boundary is high.
- **Empfehlung:** Inside `RegisterAdminRoutes`, wrap every POST / PATCH / PUT
  chain with `bodyLimit64KiB` (matching the browser admin routes; admin JSON
  bodies are tiny). Mirror this for OpenAPI: add `maxLength: 4096` to
  `ArchiveRoomRequest.reason` and `DeactivateRequest.reason`. Add an
  `adminAPIRL` (e.g. 60 req/min, burst 20) at registration time so the
  Story 6-3 TODO does not survive into Epic 7. Single small commit; the entire
  surface inherits the limits.
- **Referenz:** OWASP ASVS V13.1.1 / V13.1.3; CWE-400.

### [MEDIUM] TOCTOU race on `DeactivateUser` — UPDATE without `WHERE is_active = true`

- **CWE / OWASP:** CWE-367 / CWE-841 / A04:2021
- **Datei:**
  - `gateway/internal/api/server.go:651–664` (`GetUserStatus` then `DeactivateUser` — non-atomic)
  - `gateway/internal/api/deactivation_repo.go:67–77` (`UPDATE users SET is_active = false ...` — no precondition)
  - same shape on `ReactivateUser` at `deactivation_repo.go:80–89`
- **Beschreibung:** Documented in the per-story 6-5 MEDIUM-1 with a one-line fix
  (add `AND is_active = true` / `AND is_active = false` to the UPDATE WHERE
  clause and check `RowsAffected()`). The fix did not land. Two concurrent
  admin requests can both observe `is_active = true`, both UPDATE, and both
  emit `audit.LogEvent("user_deactivated", ...)`. The deactivation_reason is
  silently overwritten by the second admin's value. The reactivation race is
  worse — admin A deactivates at T=0, admin B reactivates at T=0+ε — both
  succeed, the user is now active, but their sessions were invalidated and
  the audit trail loses temporal ordering between the two admin actions
  (no row records that A's invalidation happened in between).
- **Impact:** Admin-only race. Outcome converges (the user ends up
  deactivated), but the audit trail is duplicated and the reactivation path
  loses session-invalidation correlation. Compliance posture (NIST AU-2)
  takes a small hit. MEDIUM because the attacker pool is `instance_admin`s
  and the data is recoverable.
- **Empfehlung:** Apply the per-story fix:
  ```sql
  UPDATE users SET is_active = false, deactivated_at = $2, deactivation_reason = $3
   WHERE user_id = $1 AND is_active = true
  ```
  Check `RowsAffected()` and surface 409 to the second caller.

### [MEDIUM] `role_overrides` and `room_defaults` ship without `ENABLE ROW LEVEL SECURITY`

- **CWE / OWASP:** CWE-732 (Incorrect Permission Assignment) / A04:2021
- **Datei:**
  - `gateway/migrations/000035_role_overrides.up.sql` (no `ENABLE ROW LEVEL SECURITY`, no `FORCE ROW LEVEL SECURITY`, no `GRANT` clause)
  - `gateway/migrations/000037_room_defaults.up.sql` (same)
- **Beschreibung:** `role_overrides` decides who can call `/api/v1/admin/*` —
  it is the most authority-sensitive new table in the epic. Story 6-7's
  review correctly noted that `rooms` is global metadata and does not need
  RLS; the same does not apply to `role_overrides`. The Nebu invariant
  ("compliance/user-scoped tables only") classifies `role_overrides` as
  user-scoped *by design* (`user_id` is the primary key half). Without
  `ENABLE ROW LEVEL SECURITY` + `FORCE ROW LEVEL SECURITY`, any DB role with
  `INSERT` privilege on the table can grant itself `instance_admin` and
  bypass the entire admin gate on next request — including the migration
  role and any future read-only role that is granted `INSERT` by accident.
  `room_defaults` is non-user-scoped server config (low-risk to leak), but
  follows the same pattern as `server_config` (which DOES have RLS per the
  migration comment).
- **Impact:** Defence-in-depth gap rather than active exploit. The current
  set of DB roles does not include any role that could be confused — the
  application role is intended to have `INSERT` on `role_overrides` and
  reads its own row. Becomes HIGH if a future migration grants an unrelated
  role `INSERT` privilege.
- **Empfehlung:** Add to a follow-up migration:
  ```sql
  ALTER TABLE role_overrides ENABLE ROW LEVEL SECURITY;
  ALTER TABLE role_overrides FORCE ROW LEVEL SECURITY;
  CREATE POLICY role_overrides_app_rw ON role_overrides
      USING (true) WITH CHECK (true);
  REVOKE INSERT, UPDATE, DELETE ON role_overrides FROM PUBLIC;
  GRANT  INSERT, UPDATE, DELETE ON role_overrides TO nebu_app;
  ```
  Same shape for `room_defaults` if applied for consistency. Track as
  Epic 6 follow-up alongside the body-limit story.

### [MEDIUM] Mass session invalidation on `PATCH /admin/config` — no rate limit, no idempotency, self-DoS

- **CWE / OWASP:** CWE-770 / A04:2021
- **Datei:**
  - `gateway/internal/api/server.go:163–170` (`oidcChanged` triggers `InvalidateAllAdminSessions` unconditionally)
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:721–738` (sequential per-user `destroy_session`, no batching, no upper bound)
- **Beschreibung:** Carried forward from the per-story 6-10 MEDIUM. Any
  PATCH that touches `oidc_issuer`, `oidc_client_id`, or `oidc_client_secret`
  triggers a sweep over the entire `:NebuSessions` ETS table. There is no
  comparison against the existing value, so a no-op PATCH (same value
  re-sent) re-runs the sweep. There is no per-PATCH rate limit (see MEDIUM
  above), so a confused or compromised admin can lock everyone out
  including themselves on a tight loop. Audit metadata records `changed_keys`
  but not whether the value actually changed.
- **Impact:** Admin-only self-DoS. MEDIUM not HIGH because of the trust
  boundary. Becomes more painful at instance scale (10k+ sessions =
  sequential `Enum.each` over the full table).
- **Empfehlung:** (1) Compare `body.OidcIssuer` etc. against the existing
  row before upserting; only mark `oidcChanged = true` when the value
  actually differs. (2) Skip the actor's own session in the sweep so the
  admin does not log themselves out. (3) Combine with the body-limit /
  rate-limit follow-up.

### [LOW] `reason` fields ship without `maxLength` in OpenAPI

- **CWE / OWASP:** CWE-770 / A04:2021
- **Datei:**
  - `gateway/api/openapi.yaml:209–216` (`ArchiveRoomRequest.reason` — `minLength: 10` only)
  - same shape for `DeactivateRequest.reason` (Story 6-5 LOW-2, unfixed)
- **Beschreibung:** TEXT column in PostgreSQL has no length limit by default;
  audit metadata caps at 16 KiB and silently drops oversized payloads. A
  multi-MB `reason` bloats the row, slows queries, and silently breaks the
  audit trail (the audit row records `room_archived` but not the reason).
  Caller must hold `instance_admin` so practical exploit is admin-only DoS.
- **Empfehlung:** Add `maxLength: 4096` to every `reason` field in
  `openapi.yaml` and rerun `make gen-api`. Cap in handler before DB write.

### [LOW] `default_max_members` upper bound missing — admin can trip Postgres INTEGER overflow

- **CWE / OWASP:** CWE-20 / A04:2021
- **Datei:** `gateway/internal/api/server.go:1138–1142` (only `< 0` check),
  `gateway/api/openapi.yaml` (`PutRoomDefaultsRequest.default_max_members:
  minimum: 0`, no `maximum`)
- **Beschreibung:** Story 6-8 MEDIUM, unfixed. JSON `9999999999` parses
  cleanly into Go `int`, fails at the SQL layer with `pq: integer out of
  range`. The PATCH `/admin/rooms/{roomId}` path is correctly bounded to
  `[2, 100000]` — only the room-defaults PUT endpoint is affected.
- **Empfehlung:** Add `maximum: 100000` to the OpenAPI schema and check in
  handler. Same fix as 6-8 review.

### [INFO] HIGH-1 from Story 6-10 was correctly fixed at the router level

- **Datei:** `gateway/internal/api/router.go:35–48` (`StrictHTTPServerOptions`
  with custom `RequestErrorHandlerFunc` and `ResponseErrorHandlerFunc`)
- **Beobachtung:** The verbose 5xx leak documented in 6-10 HIGH-1 (where
  `http.Error(w, err.Error(), 500)` would surface SQL key names like
  `oidc_client_secret` and the raw pq driver message) was closed by
  replacing the strict-handler default error responder with a generic
  `M_UNKNOWN` JSON envelope plus server-side `slog.ErrorContext`. The
  recommendation in the per-story review (option (b) — wire it via
  `NewStrictHandlerWithOptions`) was followed exactly. Every Admin API
  handler — current and future — inherits the sanitised 5xx automatically.

### [INFO] HIGH-1 from Story 6-5 was correctly fixed: `WithUserStatusCheck` wraps every authenticated route

- **Datei:** `gateway/cmd/gateway/main.go:445`
  (`jwtWithStatusCheck := middleware.WithUserStatusCheck(jwtMiddleware, ...)`)
  followed by 80+ route registrations using `jwtWithStatusCheck` instead
  of the bare `jwtMiddleware`.
- **Beobachtung:** The deactivation-bypass HIGH from 6-5 is closed: every
  Matrix and admin route inherits the `is_active` check. Verified by line
  count — there are zero remaining call sites of `jwtMiddleware` outside
  the `WithUserStatusCheck` wrapping. The unrelated fail-open finding
  documented in this report (HIGH-1) is a separate concern that was not
  in scope for the per-story 6-5 review.

### [INFO] gRPC PSK interceptor covers all five new admin RPCs

- **Datei:**
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex:6–7`
    (`intercept(Nebu.Grpc.AuthInterceptor)`)
  - `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex` (constant-time `:crypto.strong_compare/2`)
- **Beobachtung:** The endpoint-level interceptor wraps the entire
  `Nebu.EventDispatcher.Server`, so `InvalidateUserSessions`,
  `UpdateRoomSettings`, `ArchiveRoom`, `UnarchiveRoom`, and
  `InvalidateAllAdminSessions` all enforce the `x-nebu-node-token` PSK
  with constant-time comparison. No new RPC bypasses the interceptor; no
  ad-hoc Plug router is added. Confirmed the trust model: Gateway is the
  only gRPC client, the channel is PSK-authenticated, and these admin
  primitives are only reachable via the Gateway's authenticated HTTP
  surface.

### [INFO] Hardcoded secrets — none introduced

- **Beobachtung:** Searched the diff for password / secret / API-key string
  literals. The only matches are: (a) the field name `oidc_client_secret`
  used as a `server_config` key string, (b) `s.Secret` and
  `internalSecret` referring to byte slices read from
  `NEBU_INTERNAL_SECRET_FILE` at startup, (c) test-helper crypto setup
  values. No production secret material is in source.

### [INFO] AES-256-GCM use in `encryptAES256GCMForAPI` is correct

- **Datei:** `gateway/internal/api/server.go:253–272`
- **Beobachtung:** SHA-256 key derivation, 12-byte nonce from
  `crypto/rand.Reader`, `gcm.Seal(nonce, nonce, plaintext, nil)` so the
  nonce is prepended to the ciphertext, hex output. No nonce reuse path
  exists — every call generates a fresh random nonce. Empty-key path
  refuses to write rather than falling back to plaintext
  (`errEncryptionKeyMissing`). Mirrors the proven pattern from
  `gateway/internal/admin/crypto.go`. End-to-end: secret enters via
  `body.OidcClientSecret`, is encrypted with `internalSecret` as the
  AES key (SHA-256-derived to a 32-byte AES-256 key), is upserted into
  `server_config.value`, is never read back into `ServerConfigData`
  (the struct has no field for it), is never decrypted on any read path
  in this epic, never appears in any 200/400/500 response body.

### [INFO] Reuse of `internalSecret` for AES-256-GCM key — pre-existing trade-off

- **Datei:** `gateway/cmd/gateway/main.go:1144`
  (`Secret: []byte(internalSecret)`)
- **Beobachtung:** The same byte sequence is used as PSK for the gRPC
  `AuthInterceptor`, as cookie signing key for the admin browser session,
  and as AES key for `oidc_client_secret`. This is documented in Story
  5.29c and the 6-10 INFO finding; it is not introduced by Epic 6. The
  long-term fix is per-purpose key separation (HKDF-derived sub-keys per
  use). Recording for the audit trail; not blocking.

### [INFO] No audit-table grants modified

- **Beobachtung:** None of `000034`–`000037` touch `audit_log`, the
  `audit_log_no_update` policy, or the `audit_log_no_delete` policy.
  Audit-immutability invariant holds.

### [INFO] Admin override of room state changes bypasses Matrix Power Levels — by design

- **Datei:** `gateway/internal/api/server.go:1033–1121`
  (`PatchAdminRoom` updates `name`/`topic`/`visibility`/`max_members`
  via direct `UPDATE rooms` without consulting Power Level state events).
- **Beobachtung:** Documented in the 6-8 review. The `RequireRole("instance_admin")`
  gate is the explicit substitute for in-room PL. The handler does not
  emit corresponding `m.room.name` / `m.room.topic` Matrix state events,
  so a Matrix client may see a divergence between admin-set DB values and
  the canonical room state. Out of scope for security; flag for a UX/
  correctness story.

### [INFO] No cookie forging or DB seeding in Epic 6 integration tests

- **Datei:** `gateway/test/integration/admin_api_steps_test.go`
- **Beobachtung:** The new Gherkin admin API CRUD flow (Story 6-11) uses
  the real OIDC + JWT chain to obtain authenticated sessions; no
  `setCookie` / `seedSession` shortcuts. Test integrity invariant
  preserved.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ — no compliance-scoped tables touched (rooms is global metadata; users column-add is non-RLS) |
| `reason` field on compliance access         | ✅ — n/a (no compliance handlers added; archive/deactivate `reason` is admin-internal, validated `≥ 10`, captured in audit metadata) |
| Audit-log immutability                      | ✅ — no migration touches `audit_log`, no UPDATE/DELETE grant change |
| `instance_admin` notification (if in-scope) | ✅ — n/a (admins are themselves the actor; no cross-tenant escalation hook) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — every admin route wrapped via `jwtMiddleware → WithUserStatusCheck`; `WithUserStatusCheck` is wired at construction (`main.go:445`) and inherited by all 80+ authenticated routes; no bypass |
| Matrix Power Level checks                   | ⚠️ — deliberate `instance_admin` override on PatchAdminRoom / Archive / Unarchive (per-story design; not a violation) |
| No hardcoded secrets                        | ✅ — searched diff; only `internalSecret` byte slice from file mount, `oidc_client_secret` is a server_config key string |
| TLS 1.3 enforcement                         | ✅ — no TLS surface added |
| AES-256-GCM correctness                     | ✅ — `encryptAES256GCMForAPI` random nonce per encryption, no reuse path; matches existing admin/crypto pattern |
| Ed25519 verify-before-accept                | ✅ — no signature path touched (archive/unarchive only mutate `rooms.status`; events table untouched) |
| No secrets in logs / error messages         | ✅ — `slog.Warn` paths log `user_id`, `room_id`, `err`; the verbose-error 5xx leak (6-10 HIGH) was fixed at the router level via `ResponseErrorHandlerFunc` returning generic `M_UNKNOWN` |

## Migration Review

| Migration | Subject | Findings |
|-----------|---------|----------|
| `000034_users_deactivation` | adds `deactivated_at`, `deactivation_reason` to `users` | Schema-additive; no RLS change (column-add on existing RLS-managed table); rollback symmetric |
| `000035_role_overrides`     | new `role_overrides` table | **MEDIUM** — no `ENABLE ROW LEVEL SECURITY` despite user-scoped data; CHECK constraint on role enum is correct; no FK to `users` is the documented design (allows pre-grant) |
| `000036_rooms_admin_columns` | adds `topic`, `creator_user_id`, `max_members`, `status`, `archive_reason` to `rooms` + 2 indexes | Schema-additive on existing global-metadata table; CHECK constraint on `status` correct; no audit-table touch |
| `000037_room_defaults`      | new `room_defaults` table (single-row config) | **MEDIUM** — no RLS (consistency with `server_config` would call for it; non-user-scoped data lowers risk) |

No migration drops compliance data, no migration relaxes audit-log permissions, no
migration grants ALL on a sensitive schema, no `SECURITY DEFINER` function added.
All `down` migrations are schema-symmetric.

## Cross-Story Attack-Surface Analysis (Caller Override Focus)

Per the caller's expanded scope, the following angles were specifically tested:

1. **Combined endpoint Auth-Bypass.** The `WithUserStatusCheck` + `RequireRole`
   composition produces the HIGH-1 finding above. No other route-pair combinations
   open additional auth paths (verified by reading every `mux.Handle` call in
   `RegisterAdminRoutes`).

2. **Accumulated patterns — body-limit / audit-on-GET.** Body-limit gap is
   documented as MEDIUM. Audit on GET endpoints: `ListAdminUsers`,
   `GetAdminUser`, `ListAdminRooms`, `GetAdminRoom` all emit
   `admin_user_viewed` / `admin_room_viewed`; `GetAdminConfig` and
   `GetAdminMetrics` do not (per-story 6-10 MEDIUM, unfixed). Recording in
   the report under HIGH-1's neighbouring MEDIUMs.

3. **IDOR between User and Room endpoints.** No path exists for a non-admin
   to drive a room operation. Every handler is gated by
   `RequireRole("instance_admin")`. Once admin, the design intentionally
   permits cross-room mutation (no per-room membership check) — by spec.
   No IDOR.

4. **Race conditions: Deactivation × Role-Grant × Session-Invalidation.**
   Deactivate has the documented TOCTOU MEDIUM. Role-Grant uses
   `INSERT ... ON CONFLICT DO UPDATE` so it is idempotent at the DB layer.
   InvalidateUserSessions runs after DeactivateUser unconditionally — but
   if an admin grants `instance_admin` to a deactivated user, the user
   stays deactivated (the `WithUserStatusCheck` gate runs at every request
   and is independent of the role gate). No path exists for "deactivated
   user gains admin via role grant and bypasses deactivation".

5. **gRPC handlers — middleware bypass risk.** All five new RPCs
   (`InvalidateUserSessions`, `UpdateRoomSettings`, `ArchiveRoom`,
   `UnarchiveRoom`, `InvalidateAllAdminSessions`) are attached to
   `Nebu.EventDispatcher.Server`, which the endpoint declares with
   `intercept(Nebu.Grpc.AuthInterceptor)`. PSK constant-time check applies.
   No alternate RPC service (no second `use GRPC.Server`) bypasses the
   interceptor.

6. **DB-Migration Sicherheit — RLS on new tables.** Documented as MEDIUM
   under "role_overrides + room_defaults without RLS".

7. **Secrets-Hygiene — `oidc_client_secret` end-to-end.** Verified at
   write (encryption with random nonce), at storage (encrypted in
   `server_config.value`), at read (never queried — `GetServerConfig`
   excludes the key), at response (struct has no field). The literal key
   name `"oidc_client_secret"` does appear in error wrap chains
   (`UpsertServerConfigKey(%q): ...`), but the router-level
   `ResponseErrorHandlerFunc` replaces these with a generic `M_UNKNOWN`
   envelope before reaching the client. The plaintext secret never crosses
   the API boundary. ✅.

## Dependency Scan

`go.sum` and `go.mod` are in the diff (Story 6-1 added `kin-openapi`,
`oapi-codegen/runtime`, promoted `golang.org/x/net`). `govulncheck` is not
available in the local environment — scan skipped, same as the per-story
6-1 review. Recommend running in CI before the epic is marked done. No
`mix.lock` / `mix.exs` change in the diff.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 2 |
| MEDIUM    | 4 |
| LOW       | 2 |
| INFO      | 7 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline
proceeds with warning. The two HIGH findings must be scheduled as follow-up
stories or explicitly accepted with written justification before the next
release. Concrete next steps:

1. **HIGH-1 (composed fail-open)** — Smallest viable fix is to flip
   `RequireRole`'s DB-error path to fail-closed (503). One file
   (`gateway/internal/api/middleware.go`), ~5 lines, plus one regression test.
   Estimated effort: 1 hour.
2. **HIGH-2 (Core archived check missing)** — Pick up the existing 6-9
   follow-up. One Elixir change in `Nebu.EventDispatcher.Server.send_event`,
   one ExUnit test asserting the post-archive race no longer succeeds.
   Estimated effort: 2–3 hours.

The four MEDIUM findings cluster around two physical fixes:
(a) one body-limit + rate-limit + maxLength sweep across `RegisterAdminRoutes`
covers MEDIUM-1, LOW-1, LOW-2, plus the 6-10 MEDIUM-2; (b) one migration
to add RLS to `role_overrides` (and optionally `room_defaults`) closes
MEDIUM-4. The deactivation TOCTOU MEDIUM is one SQL clause + one test.

The Epic 6 audit trail is now complete — eleven per-story reviews
(6-1, 6-3, 6-4, 6-5, 6-7, 6-8, 6-9, 6-10) plus this epic-end review.
Findings that survive both passes are the two HIGHs above.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an
immutable audit artifact — do not edit retrospectively; create a new review
if re-analysis is required.*
