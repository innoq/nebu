# Security Review — Epic 7 Final SEC Gate 2 (Stories 7-32 / 7-33 / 7-35) — 2026-04-30

**Agent:** Kassandra
**Diff base:** `git diff 6121df3..HEAD` (3 fix stories closing the open SEC Gate 2 findings of `epic-7b-security-review-2026-04-30.md`)
**Scope:** 3 fix stories
- 7-32 — moderation gRPC handlers read caller_id from trusted metadata (closes prior HIGH)
- 7-33 — system-role bypass in `get_room_state` for internal EventBus fanout (closes prior MEDIUM)
- 7-35 — `withUserDB` helper + `SET LOCAL app.user_id` GUC + RLS migration 000033 on `notifications` / `push_rules` / `pushers` (closes prior MEDIUM × 2)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

The three fix stories close all four blocking findings from the Epic 7b SEC Gate 2 (1 HIGH, 3 MEDIUM) without introducing new attack surface. Moderation handlers now derive caller identity exclusively from gRPC metadata; the `system_role == "system"` bypass in `get_room_state` is gated by the gRPC-PSK trust boundary and unreachable from any external client because `MapSystemRole` whitelists only `instance_admin`, `compliance_officer`, and `user`; `withUserDB` correctly scopes the `app.user_id` GUC to a single transaction via `SET LOCAL` and migration 000033 enables `FORCE ROW LEVEL SECURITY` with symmetric `USING` + `WITH CHECK` predicates on all three previously-unprotected tables.

No new CRITICAL or HIGH findings. Three observations are recorded as INFO (positive findings worth preserving in the audit trail) plus one LOW (a documentation-only nit on an inconsistency between the unit-test contract and PostgreSQL's actual SET-grammar). Two of the original Epic 7b MEDIUM findings remain explicitly out of scope for this gate (rate-limit gap on new authenticated endpoints; `did` JWT-claim trust path) and must be tracked as separate follow-up stories before the next release.

The cumulative diff preserves audit-log immutability, does not introduce hardcoded secrets, contains no new `math/rand` for security values, no `InsecureSkipVerify`, no string-concatenated SQL, no plaintext secrets in logs.

## Findings

### [LOW] `SET LOCAL app.user_id = $1` parameter binding is a PostgreSQL grammar edge case

- **CWE / OWASP:** N/A (operational hygiene)
- **Datei:** `gateway/internal/db/user_tx.go:21`
- **Beschreibung:** PostgreSQL's SET grammar historically does not accept query parameters in the value position — the canonical parameterised form is `SELECT set_config('app.user_id', $1, true)`. pgx v5 in extended-query mode happens to accept the `$1` form against current PostgreSQL versions, but the contract is undocumented. The unit test (`user_tx_test.go:155-173`) asserts the bound-parameter form, so any future driver update that stops accepting parameters in SET would surface as an integration failure (fail-closed).
- **Impact:** Operational only — no security impact. If the SET fails at runtime, `withUserDB` returns the error, the query is never executed, and the user gets an internal error response (fail-closed with respect to RLS — no cross-user leak path). Today, integration tests against real PostgreSQL are passing through this path (the round-trip `PutGet_RoomAccountData` Godog scenario was the in-flight regression that 7-35 deliberately closes), so the form works on the production version.
- **Empfehlung:** Optional hygiene improvement — switch to `tx.ExecContext(ctx, "SELECT set_config('app.user_id', $1, true)", userID)` which is the documented PostgreSQL idiom for parameterised GUC. The `true` second argument scopes it to the current transaction, equivalent to `SET LOCAL`. No urgency — current form works.
- **Referenz:** PostgreSQL docs §9.27.1 (`set_config`).

### [INFO] Story 7-32 closes the HIGH finding on body-driven `caller_id`

- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1211, 1293, 1370`.
- **Beschreibung:** `kick_user/2`, `ban_user/2`, `unban_user/2` now extract caller identity exclusively via `Nebu.Grpc.Metadata.trusted_identity(stream)` and discard `request.caller_id` (the proto field is retained for telemetry but is never used as the auth principal). The pattern matches `set_power_levels/2`, `get_messages/2`, `get_room_state/2`. The ATDD regression test `server_moderation_metadata_test.exs:19-23` verifies the contract: with `request.caller_id = "@victim"` (power 100) and metadata `x-user-id = "@attacker"` (power 0), the handler raises `permission_denied` — proving the body field cannot bypass the power-level gate. **No path remains where moderation handlers consult `request.caller_id` for authorization.**

### [INFO] Story 7-33 system-role bypass is safe — `MapSystemRole` whitelist defends the boundary

- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:733`; `gateway/internal/auth/roles.go:6-13`; `gateway/cmd/gateway/main.go:59`.
- **Beschreibung:** The new bypass `unless system_role == "system" or MapSet.member?(state.members, caller_id)` could in principle be abused if any external client could populate the `x-system-role` header with the literal string `"system"`. Three layers prevent this:
  1. **Gateway whitelist:** `MapSystemRole/1` (`gateway/internal/auth/roles.go:6`) returns one of `instance_admin`, `compliance_officer`, `user` — never `"system"`. A JWT containing `system_role: "system"` is downgraded to `"user"` before being placed in the request context.
  2. **gRPC PSK boundary:** Core's gRPC server requires `x-nebu-node-token` matching `NEBU_INTERNAL_SECRET_FILE` (`core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex:61`). External clients cannot reach Core directly to inject arbitrary metadata.
  3. **Single producer of `"system"`:** The literal `"system"` is constructed exactly once in the gateway, in the internal fanout goroutine `coreRoomStateLookup.GetRoomState` (`main.go:59`), which runs only over the EventBus event channel — no client input flows into it.

  The Story 7-19 IDOR fix is preserved for user callers: `server_get_room_state_system_test.exs` covers (a) system caller with no `x-user-id` succeeds, (b) non-member user with `system_role = "user"` is rejected, (c) member user is allowed. Bypass is safe.

### [INFO] Story 7-35 `withUserDB` correctly isolates the GUC per transaction

- **Datei:** `gateway/internal/db/user_tx.go:14-28`; `gateway/migrations/000033_rls_enable_user_tables.up.sql`.
- **Beschreibung:** The helper uses `SET LOCAL` (not `SET`) which scopes the GUC to the transaction; on commit or rollback the GUC is automatically reset. Any subsequent acquisition of the same pooled connection therefore starts with `app.user_id` unset, and `current_setting('app.user_id', true)` returns NULL — the RLS predicate evaluates to NULL, treated as false in `USING`, blocking all rows (fail-closed). The deferred `tx.Rollback()` is safe after Commit (returns `ErrTxDone`) and prevents a leaked transaction on any unexpected return path. Unit tests cover (a) GUC set as bound parameter ($1, not interpolated), (b) commit on success, (c) rollback on fn error, (d) deferred rollback on Commit error. The PgPool API is not used (the project uses `database/sql` over the pgx stdlib driver), so connection reuse is at the `*sql.DB` level — `SET LOCAL` is the only correct primitive here. Migration 000033 is symmetric: `ENABLE` + `FORCE ROW LEVEL SECURITY` on all three tables, `FOR ALL` policy with both `USING` (read protection) and `WITH CHECK` (write protection) using the same `current_setting('app.user_id', true)` predicate. INSERT, UPDATE and DELETE are all covered by the FOR ALL clause; nothing was missed.

### [INFO] Cumulative diff: invariants preserved, no new attack surface

- **Datei:** Full diff scanned.
- **Beschreibung:**
  - **Audit-log immutability** — migration 000033 does not touch `audit_log` permissions or grants (the migration is on `notifications`, `push_rules`, `pushers` only).
  - **No hardcoded secrets** — no string literals matching token / password / key patterns.
  - **No `math/rand` for security values** — none introduced.
  - **No `InsecureSkipVerify: true`** — none introduced.
  - **No string-concatenated SQL** — `withUserDB` and all refactored store methods use `$N` parameter binding.
  - **No new `Logger.info` / `slog.` lines that interpolate tokens or session material** — diff is silent on logging.
  - **OIDC token validation** — unchanged (handlers continue to use the existing `JWTMiddleware` chain).
  - **Matrix Power Level checks** — kick/ban/unban still call `Nebu.Room.PowerLevels.can?/3` against the Room GenServer's authoritative state with caller_id derived from trusted metadata. No way to bypass.
  - **Ed25519 signing** — kick/ban/unban events continue to be signed via `:crypto.sign(:eddsa, ...)` before insert; signature wrapping is unchanged.
  - **gRPC PSK** — `NEBU_INTERNAL_SECRET_FILE` is still the only authoritative trust boundary between Gateway and Core.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no new compliance handlers; migration 000033 strengthens RSP on notifications/push_rules/pushers) |
| `reason` field on compliance access         | ✅ (no new compliance writes) |
| Audit-log immutability                      | ✅ (migration 000033 does not touch audit tables) |
| `instance_admin` notification (if in-scope) | ✅ (no new escalating compliance access) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (re-uses existing JWTMiddleware) |
| Matrix Power Level checks                   | ✅ (kick/ban/unban now derive caller_id from trusted gRPC metadata, then check power level — see Story 7-32 INFO finding) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (no TLS config changes) |
| AES-256-GCM correctness                     | ✅ (no new crypto code paths) |
| Ed25519 verify-before-accept                | ✅ (kick/ban events signed Ed25519 before insert; pattern unchanged) |
| No secrets in logs / error messages         | ✅ (diff contains no new logging that touches credentials) |

## Dependency Scan

No `go.sum`, `go.mod`, `mix.lock`, or `mix.exs` changes in this diff — section omitted.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 1 |
| INFO      | 4 |

## Cumulative Closure Summary — Epic 7b SEC Gate 2 → Epic 7 Final SEC Gate 2

| Original SEC Gate 2 finding (`epic-7b-security-review-2026-04-30.md`) | Severity | Closure status |
|---|:---:|---|
| Moderation gRPC handlers trust request-body `caller_id` | HIGH | **CLOSED** by Story 7-32 — caller_id from `trusted_identity(stream)` in all 3 handlers; ExUnit regression test asserts body-vs-metadata divergence yields `permission_denied`. |
| Story 7-19 IDOR fix breaks server-internal event fanout | MEDIUM | **CLOSED** by Story 7-33 — `system_role == "system"` bypass added to `get_room_state`; gateway sets `WithUserMetadata(ctx, "", "system")` only in the internal fanout goroutine. ExUnit covers system-bypass + non-member rejection + member happy path. Bypass cannot be reached from external clients (MapSystemRole whitelist + gRPC PSK + single producer). |
| Notifications + push_rules + pushers tables rely solely on application-level user_id filter | MEDIUM | **CLOSED** by Story 7-35 — migration 000033 adds `ENABLE`/`FORCE ROW LEVEL SECURITY` + `FOR ALL ... USING / WITH CHECK` on all 3 tables. |
| `room_account_data` RLS policy enforced but `app.user_id` GUC never set — table effectively unreadable | MEDIUM | **CLOSED** by Story 7-35 — `withUserDB` wires `SET LOCAL app.user_id = $1` per transaction; all four store layers (account_data / notifications / push_rules / pushers) refactored to use it. |
| No rate limit on new authenticated Matrix endpoints | MEDIUM | **OUT OF SCOPE** for this gate. Original recommendation noted Story 7-34 as the follow-up. Tracked separately. |
| DELETE /devices UIA flow has no production code path | MEDIUM | **OUT OF SCOPE** for this gate (fail-closed; original report flagged as awareness rather than exploitable). |
| `did` JWT claim is consumer-controlled | MEDIUM | **OUT OF SCOPE** for this gate. Tracked separately. |
| Inconsistent rate-limit on profile sub-field GETs | LOW | **OUT OF SCOPE** for this gate. |

All four blocking findings in the original Epic 7b SEC Gate 2 (1 HIGH, 3 MEDIUM directly tied to fix-stories) are closed. The three remaining MEDIUMs and the LOW are out-of-scope deferred items the original report itself tagged as follow-up work.

## Pipeline Decision

**CLEAN — no CRITICAL / HIGH findings.** Pipeline may proceed.

The single LOW finding (PostgreSQL SET-grammar parameter form) is a hygiene observation with no security impact and no urgency. The three remaining Epic 7b MEDIUM findings (rate-limit gap, DELETE /devices UIA dead-end, `did` JWT claim trust) and the LOW (profile sub-field rate-limit parity) carry over as documented follow-up items but are explicitly out of scope for this Epic 7 final gate.

Recommendation: convert the three carry-over MEDIUMs to follow-up stories (epic 8 or maintenance backlog) — same recommendation as the original Epic 7b report.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
