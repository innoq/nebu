# Security Review — Epic 9 (Final SEC Gate 2 — supersedes 2026-05-05 first pass) — 2026-05-05

**Agent:** Kassandra
**Diff base:** `git diff 4e968cc..HEAD` (entire Epic 9 — 17 stories)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=Opus 4.7`

> **Note on supersession.** This report replaces the previous epic-9 SEC Gate 2 (commit `c814977`) which was issued before stories 9.15–9.18 were merged. The earlier report is preserved in git history. All earlier findings are re-verified below; the new delta (commits `7a8823d`, `0875258`, `fd12294`, `0318bbf`, `d40f3d6`) is analysed in dedicated sub-sections.

## Executive Summary

Epic 9 is the final coding epic for the MVP and is broad in scope: 18 stories spanning new admin gRPC RPCs (user/room/config/member management), Admin UI HTTP→gRPC integration, full state-event handling with a Matrix-spec whitelist, atomic room version upgrade, archive TOCTOU fix, OIDC silent token refresh with refresh_token encryption-at-rest, an additional read-only Admin RPC for the room member panel (9.18), an event-ordering correctness fix in `create_room`/`upgrade_room`, a sync-time membership deduplication fix, and arc42 docs. Total surface across the epic: ~168 files, ~28.8k inserted / ~0.9k deleted lines.

The critical security primitives carry through unchanged through stories 9.15–9.18: every new admin POST is wrapped in `bodyLimit64KiB(csrf(sessionGuard(...)))` with the `SessionGuardWithRefresh` upgrade; admin gRPC RPCs are gated by the PSK auth interceptor on the Elixir side and by the admin SessionGuard on the Go side; state events are gated through the new `:change_state` power-level check (SEC Gate 1 fix to Story 9-7); room upgrades enforce `power_level >= 100` BEFORE any state mutation (verified again at `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:2312`, after the new try/catch wrapper for stale Horde PIDs); the Story 9-9 TOCTOU fix correctly serialises archive/send via `SELECT … FOR UPDATE`; refresh tokens are AES-256-GCM encrypted with a per-row nonce; and the new Story 9-6 whitelist catches unknown state event types at the gateway boundary before Core ever sees them.

**Delta verdict (9.15–9.18):**

- 9.15 (Admin UI bug-fixes — select bg, compliance btn-outline, room fallback name) — pure CSS/template polish + Direct Chat fallback string. No security-relevant change. The user-controlled `room.MemberCount` integer is rendered via `html/template` auto-escape inside an HTML body context. CLEAN.
- 9.16 (Test-only — m.room.join state-copy order) — adds Godog scenarios + an audit doc; no production code. CLEAN.
- 9.17 (Test-only — Admin PATCH rooms max_members & visibility test) — adds upgrade_room steps; no production code. CLEAN.
- 9.18 (Admin UI Room Detail — Member List + dedup_member_state_events fix). New read-only admin RPC `ListAdminRoomMembers` follows the established 9-1/9-3 pattern (parameterised SQL, decrypt-with-fail-closed, no audit-log on read-only). The `dedup_member_state_events/2` defect fix correctly re-keys deduplication on `state_key` (the membership subject) instead of `sender_id` (the inviter). CLEAN.
- Stale-PID hardening in `upgrade_room` (commit `0875258`) — wraps `room_registry_module().get_state/1` in `try/catch` for `:exit, {:noproc, _}` and surfaces `emit_state_event` DB errors as `GRPC.RPCError{:internal}` instead of crashing on an unmatched `{:error, _}`. **Power-level check still runs first** (line 2312, before any DB mutation). Fail-closed semantics preserved. CLEAN.

No CRITICAL findings. No HIGH findings. Three MEDIUM observations and two LOW observations are noted below — none block the epic close. The report consolidates and closes out the prior partial epic-9 review (2026-05-01), the per-story Kassandra reviews of 9-1 through 9-14, the per-story 9-18 review, and the new analysis for 9-15 through 9-17.

## Findings

### [MEDIUM-1] internal_secret PSK is reused as the refresh-token encryption key

- **CWE / OWASP:** CWE-1188 (Insecure Default Initialization of Resource) / A02:2021
- **File:** `gateway/internal/admin/auth.go:769`, `gateway/internal/admin/middleware.go:298`, `gateway/internal/admin/crypto.go:13-58`
- **Beschreibung:** `encryptAES256GCM`/`decryptAES256GCM` derive the AES-256 key from the internal PSK via `sha256.Sum256(secret)`. The same `internalSecret` value is used as (a) the HMAC key for `admin_session` cookies, (b) the PSK for gRPC node authentication between Gateway and Core, (c) the password for `admin_oidc_state` cookie integrity, and now (d) the master key for refresh-token encryption-at-rest. The KEK used elsewhere (`NEBU_KEY_ENCRYPTION_KEY`) for the Ed25519 compliance key was NOT used for refresh tokens.
- **Impact:** Compromise of the PSK file (`.secrets/internal_secret`) leaks every primitive at once: cookie forgery, gRPC peer impersonation, AND the ability to decrypt every stored refresh token. PSK rotation invalidates all stored refresh tokens. There is no clean path to rotate refresh-token encryption independent of cookie/gRPC trust.
- **Empfehlung:** Story 9-14 follow-up: derive the refresh-token encryption key from `NEBU_KEY_ENCRYPTION_KEY` (already used for the compliance signing key in `cmd/gateway/main.go:1007-1023`) instead of `internalSecret`. This separates compromise blast radius and aligns with the existing per-purpose key-management pattern.
- **Why MEDIUM, not HIGH:** No direct attacker path exists today. PSK is mounted as a Compose secret with file permissions, never logged, never returned in any response. The key-reuse becomes exploitable only after a separate PSK file disclosure, which is itself a CRITICAL pre-condition. Documenting as defense-in-depth.
- **Referenz:** OWASP ASVS V6.4.1 (cryptographic key management), NIST SP 800-57 §6.2.

### [MEDIUM-2] Flash-message allowlist drops several new Epic 9 messages silently

- **CWE / OWASP:** CWE-684 (Incorrect Provision of Specified Functionality) — reliability rather than security
- **File:** `gateway/internal/admin/flash.go:5-18`
- **Beschreibung:** `allowedFlashMessages` is the sanitisation allowlist that gates every `?flash=…` query value. Epic 9 introduced new flash strings in `compliance_handler.go`, `users.go`, `rooms.go`, and `config.go` that are NOT in the allowlist: `"Self-approval is not permitted"`, `"Already decided"`, `"Rejection reason is too long"`, `"Error rejecting request"`, `"Error approving request"`, `"Name update not yet available"`, `"Room not found"`, `"User not found"`, `"Error reactivating user"`, `"Error deactivating user"`, `"Error updating role"`, `"Error archiving room"`, `"Error unarchiving room"`, `"Settings updated"`, `"Error updating settings"`. All are silently dropped — admin sees no feedback after a failed action.
- **Impact:** Functional/UX, not security. The allowlist is a hard XSS guard (Story 7-18), so failure mode is fail-closed — any unintended payload is dropped. No XSS escalation possible. Admins receive no error feedback for legitimate failure paths (e.g. self-approval rejection, missing user). Sentinel error states are observable only via server logs.
- **Empfehlung:** Add the missing strings to `allowedFlashMessages`. As a follow-up, consider replacing the literal allowlist with a typed enum (`FlashKey`) generated from constants used at write sites — this prevents future drift.
- **Why not LOW:** The integrity-of-feedback gap touches the Compliance four-eyes self-approval path (`admin: blocked self-approval attempt`) where the user must see "Self-approval is not permitted". Currently they see no flash at all and assume the action succeeded.
- **Referenz:** N/A — defensive reliability finding.

### [MEDIUM-3] `Core.update_room_settings/2` does not persist `max_members` to the rooms table

- **CWE / OWASP:** CWE-841 (Improper Enforcement of Behavioural Workflow)
- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:297-313`
- **Beschreibung:** The Elixir handler for `UpdateRoomSettings` only invokes `Nebu.Room.Server.update_settings(room_id, %{max_members: ...})` — a `handle_cast/2` updating in-memory GenServer state. There is no DB write. Comment at line 308: "If the room is not started, the new max_members will be loaded from DB on next init/1." But the DB row is never updated, so on next init the original value is reloaded.
- **Impact:** When admin sets `max_members=10` from the Admin UI, the new limit applies to the running GenServer until: (a) the GenServer restarts (Horde failover, deploy, crash) or (b) Core restarts. After that, the previous `max_members` is restored from DB. Joins that were rejected as `M_ROOM_FULL` would suddenly be accepted again with no admin visibility. From a security perspective: capacity-control intended by the operator does NOT survive process boundaries. Audit log records `server_config_updated` even when nothing was persisted (the call returns `ok: true` always).
- **Empfehlung:** In `update_room_settings/2`, `UPDATE rooms SET max_members = $1 WHERE room_id = $2` (use the same Ecto/Repo pattern as `archive_room_atomic/1`) BEFORE casting to the GenServer. Mirrors the Story 9-1 atomic-archive pattern.
- **Why MEDIUM, not HIGH:** No direct attacker exploit. Admin-controlled setting whose enforcement degrades silently. Worst case is a closed room reopens after a deploy. No data leak.
- **Referenz:** OWASP ASVS V11.1.4 (workflow integrity).

### [LOW-1] Admin gRPC RPCs do not re-verify role on the Elixir side

- **CWE / OWASP:** CWE-602 (Client-Side Enforcement of Server-Side Security)
- **File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — applies to all 12 admin RPCs, including the new `list_admin_room_members` (line 2065).
- **Beschreibung:** The Elixir handlers extract `actor_id` from gRPC metadata (`x-user-id`) but do NOT verify that this user holds `instance_admin`. The trust is delegated entirely to the Go Gateway's SessionGuard. PSK auth on the gRPC channel is the only defence in the trust boundary between any-PSK-holder and the admin operations.
- **Impact:** A network-internal attacker who steals the PSK file (`.secrets/internal_secret`) can call ANY admin RPC by injecting an arbitrary `x-user-id` header. Audit log will record whatever attacker-controlled `x-user-id` they sent. Pre-condition (PSK compromise) is itself CRITICAL, so this is defence-in-depth only. Story 9.18 added one more such RPC (`ListAdminRoomMembers`) with the same property — it inherits this LOW, no escalation.
- **Empfehlung:** Add an `Elixir.RoleChecker.require_role!(stream, "instance_admin")` guard at the top of each admin handler. Read `users.system_role` for the metadata sub. This adds a second layer beyond PSK so that PSK compromise alone does not equal admin-level control of the data plane.
- **Why LOW, not MEDIUM:** Mirrors the existing trust model for `compliance_session_revoke` and other admin RPCs. Change is consistent design discipline rather than a new exposure.
- **Referenz:** NIST SP 800-53 AC-3 (Access Enforcement).

### [LOW-2] OIDC token refresh uses the same broad scope set on every refresh

- **CWE / OWASP:** CWE-272 (Least Privilege Violation) — minor
- **File:** `gateway/internal/admin/middleware.go:323`
- **Beschreibung:** `attemptTokenRefresh` re-requests `[ScopeOpenID, profile, email, groups, offline_access]` on every silent refresh. Per RFC 6749 §6, scope on refresh is optional and may be narrower than initial issuance. Re-issuing `offline_access` on every refresh ensures continued long-lived access; some IdPs interpret it as "refresh the refresh token's lifetime" which keeps the credential alive forever.
- **Impact:** Silent extension of refresh token lifetime past the operator's intended session policy. With Dex's default refresh-token-rotation off, a leaked refresh token has indefinite life. The Go layer caps the `admin_session` cookie to 8h max, but the underlying Dex refresh token can still be used to reissue.
- **Empfehlung:** Consider whether the refresh path should request a narrower scope (e.g. drop `offline_access` on refresh so the IdP applies its standard refresh-token TTL). Optional — depends on operator policy.
- **Why LOW:** Refresh tokens are encrypted at rest; a leaked DB row is not directly usable without the gateway's PSK. Concern is theoretical.
- **Referenz:** RFC 6749 §6.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ |
| Audit-log immutability                      | ✅ |
| `instance_admin` notification (if in-scope) | ✅ |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ |
| Matrix Power Level checks                   | ✅ |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ⚠️ |
| AES-256-GCM correctness                     | ✅ |
| Ed25519 verify-before-accept                | ✅ |
| No secrets in logs / error messages         | ✅ |

**Notes:**

- **Compliance RSP / `reason` / Audit immutability:** No new compliance migrations in Epic 9. Existing migrations 38 and 39 only touch `events.state_key` (no compliance fields) and `admin_sessions.refresh_token` (admin-only table, not under RSP). The existing `audit_log` policies are unchanged. `DBComplianceApprovalClient.decide` enforces four-eyes (`requesterUserID == approverSub` rejects), 4096-char note cap, and writes both an audit row AND a `compliance_requests.status` UPDATE inside a guarded path that returns `errComplianceSelfDecision` / `errComplianceNotPending` distinctly.
- **OIDC token validation:** `CallbackHandler` verifies `iss` (via `provider.Verifier`), `aud` (`ClientID`), `exp` (Verifier check), `nonce` (lines 661-672), and signing alg (via `validate.SupportedAlgs()`). Empty nonce → 403 Forbidden (line 661). Issuer URL is validated through `validateIssuerURL` for HTTPS (line 398, 598).
- **Matrix Power Levels:**
  - Story 9-6 whitelist (`gateway/internal/matrix/state_event_types.go`) rejects unknown state-event types at the gateway boundary.
  - Story 9-7 SEC Gate 1 fix (`core/apps/room_manager/lib/nebu/room/server.ex:364`) routes state events through `:change_state` (state_default = 50) instead of `:send_event` (events_default = 0).
  - Story 9-8 `upgrade_room` enforces `power_level >= 100` at `event_dispatcher/server.ex:2312` BEFORE any state mutation. **Re-verified post-`0875258`:** the new `try/catch :noproc` wrapper around `get_state/1` is positioned before the `requester_level < 100` check, so a stale Horde PID raises `:not_found` (no power-level bypass possible — failure is closed).
  - Story 9.18 `list_admin_room_members` is read-only — no power-level check required (system-role gating at the gateway is the design — admin views global, not room-scoped).
- **No hardcoded secrets:** No new credentials in Epic 9 source. Dev fixtures in `dev/dex/config.yaml` / `docs/getting-started.md` are scoped to the dev compose stack and were already covered by the prior 2026-05-01 review.
- **TLS 1.3 enforcement (⚠️):** Public HTTPS server still uses `tls.VersionTLS12` as MinVersion (`cmd/gateway/main.go:172`). Pre-existing finding from earlier epics. Not Epic 9 scope.
- **AES-256-GCM correctness:** `crypto.go` uses `cipher.NewGCM`, fresh `gcm.NonceSize()`-byte nonce per encryption from `crypto/rand`, nonce prepended to ciphertext. Decrypt path validates length before slicing (line 49). No nonce reuse, no ECB. Display-name decryption in `decrypt_display_name/1` (server.ex:2168–2178) returns `""` on decrypt failure — no plaintext oracle.
- **Ed25519 verify-before-accept:** `emit_state_event/5` in `server.ex:2404` and `emit_membership_event/3` in `room/server.ex` sign-then-persist correctly. The `create_room` and `upgrade_room` event-order changes (m.room.create FIRST) only affect persistence ordering, NOT signing — every event is still signed before persistence and verified before EventBus delivery.
- **No secrets in logs:** Reviewed `slog.Warn`/`slog.Error` calls in all new admin handlers, OIDC refresh path, and Elixir admin handlers. No token, password, or refresh-token bodies are logged. `slog.Warn("admin: ListAdminRoomMembers gRPC error", "room_id", roomID, "err", membErr)` (rooms.go:223) logs only the gRPC status wrapper, never user PII.

## Cross-Cutting Analysis Beyond Per-Story Scope

### Combined attack surfaces

The new admin endpoints and admin gRPC RPCs share a single chain: `bodyLimit64KiB → csrf → sessionGuard(WithRefresh) → handler → contextWithAdminIdentity → gRPC PSK → AuthInterceptor → handler`. No bypass path was introduced in any of the 18 stories — every state-changing admin POST goes through the full chain (verified at `cmd/gateway/main.go:323-364`). Story 9.18's new GET `/admin/rooms/{roomId}` route (re-using the existing `RoomsHandler.DetailHandler`) inherits `csrf(sessionGuard(...))` — no new mux entry, only a new branch inside the existing handler.

### Combined admin-RPC attack surface (kumulativer State)

Per-RPC count after Epic 9: **12 admin RPCs** (5 user, 5 room — including `ListAdminRoomMembers`, 2 server-config). All share the same trust model: PSK gRPC + Go SessionGuard. No combination of the 12 RPCs grants escalation that any one of them does not already grant individually. There is no "chain N RPCs to get X" path — each RPC is independently scoped to a single read or write, and audit log entries are per-RPC where applicable. The sole side-effect chaining we considered:

1. `ListAdminRoomMembers` (read PII) → `UpdateUserRole` (escalate)
2. `ListAdminRooms` → `ListAdminRoomMembers` (PII enumerate) → `DeactivateUser` (DoS)

Both chains require a pre-authenticated `instance_admin` and are exactly what the role is for. No privilege escalation beyond the role itself.

### New DB query — `list_room_members` — PII surface

The new SQL in `core/apps/event_dispatcher/lib/nebu/admin/db.ex:355-382` selects:
`rm.user_id, u.display_name_encrypted, u.display_name_nonce, rm.joined_at`
parameterised by `room_id` (positional `$1`). Critical PII review:
1. **Email is not selected** — Tier 2 PII (sensitive) is correctly omitted. `proto/core.proto:645-649` defines `AdminRoomMemberProto` with only `user_id`, `display_name`, `joined_at`. Verified.
2. **`display_name` is decrypted only via `decrypt_display_name/1`** (server.ex:2164–2178) which fails closed to `""` on AES-GCM decrypt error. Raw ciphertext, nonce, and ephemeral pubkey never leave the Core process.
3. **Filter by `left_at IS NULL`** — only currently-joined members are returned. Soft-deleted membership state (left/banned) is not exposed via this RPC; viewing historical membership requires a different code path.
4. **No timing oracle** — the SQL is a single index-friendly JOIN; rejection (decrypt failure) returns the same shape (`""` display_name) as a successful empty display_name. Defender cannot distinguish via response.
5. **Raw `inspect(reason)` in error message** (server.ex:2079) — same as the 9-3 MEDIUM-1 carry-forward (operational-log defence-in-depth gap; not user-facing because the gateway swallows the error and renders an empty member list, rooms.go:222-224).

### dedup_member_state_events fix — does the correctness change introduce a security regression?

**No.** Verified analysis:

The function (server.ex:1232–1245) removes `m.room.member` entries from `state_events` when the same `state_key` appears in `timeline_events`. Field-naming review confirmed (different proto types use different names):
- Timeline events come from `Core.Event` (proto field 5: `event_type`); the dedup uses `ev.event_type == "m.room.member"` (line 1235). ✅
- State events come from `Core.SyncRoomStateEvent` (proto field 1: `type`); the dedup uses `ev.type == "m.room.member"` (line 1242). ✅

The previous defective version keyed dedup on `sender_id`. For invites/kicks (sender ≠ target), it incorrectly dropped the **inviter's** state membership instead of the **invitee's**, leaving a stale invite/leave entry visible. The fix re-keys to `state_key` (the membership subject), which is canonical Matrix membership semantics.

**Security implications of the correctness fix:**

1. **Authorisation impact:** None. State events removed from the response are replaced (for the same user) by a fresher event in the timeline. Both events carry the same membership transitions; the homeserver-side authorisation never relies on the client's view of the state set.
2. **Information disclosure:** None new. The same membership data was already in the response (in `timeline_events`); dedup only removes an obsolete duplicate from `state_events`. No user gets fewer or more visible memberships than before.
3. **State-event visibility for users NOT in the timeline window:** Preserved (line 1238–1240: when `MapSet.size(member_state_keys) == 0` is false but a state_key is not in the timeline set, `Enum.reject` keeps it). Verified by the new `server_dedup_test.exs` (394 lines) which explicitly tests Self-Join, Invite, Kick, and the limited=true / historic-join boundary.
4. **No leakage of the new `state_key` proto field beyond intended scope:** `Core.Event` field 8 (`state_key`) carries the same string the client already sees in events delivered via `/sync` and `/messages`. For `m.room.member`, it equals the target user_id (already public room state). For `m.room.power_levels`, `m.room.create`, etc. it is `""`. No PII or session token is carried in this field. Verified at server.ex:471 and server.ex:1416.

### upgrade_room: stale Horde PID guard (commit 0875258)

The fix wraps `room_registry_module().get_state/1` in a `try/catch :exit, {:noproc, _}` and converts a process-down error to `GRPC.RPCError{:not_found}`. Security review:

- **Power-level check ordering preserved.** The `try/catch` block is followed by `requester_level = get_in(old_state.power_levels, ["users", requester_id]) || 0` and the `if requester_level < 100` check (lines 2312–2316). On `:noproc`, the `raise` short-circuits BEFORE the power-level check is evaluated — meaning a stale registry never grants upgrade. Fail-closed semantics correct.
- **`emit_state_event` failure now visible.** Previously, a bare pattern match on `{:ok, tombstone_event_id} = emit_state_event(...)` would crash the gRPC call with an unmatched `MatchError`, surfacing as `:unknown` to the client. The new `case` raises `GRPC.RPCError{:internal}` with `inspect(reason)` — same defence-in-depth concern as 9-3 MEDIUM-1 (operational-log gap; not user-facing because the gateway never propagates internal Postgres detail to the Matrix HTTP response). No regression.
- **No partial state corruption:** the new room creation block runs AFTER the tombstone was successfully written; if tombstone fails, the new room WAS already started (via `Nebu.Room.RoomSupervisor.start_room`). Story 9-8's documented compensating action (the new room remains empty and orphaned on tombstone failure) is unchanged. Operational concern, not a security boundary.

### OIDC offline_access + admin session management (Story 9-14)

The interaction with Story 5-12 (server-side session revocation) is correct: `LogoutHandler` calls `sessionStore.Revoke(ctx, sid)` BEFORE clearing the cookie. After revoke, `SessionGuardWithRefresh` sees `sess.RevokedAt != nil` and short-circuits to redirect (line 209-211) WITHOUT calling `attemptTokenRefresh`. So a revoked session cannot be silently re-extended even if the cookie is replayed. Refresh failure path (line 222-237) revokes the row + clears the cookie, ensuring that a stolen refresh token cannot resurrect a session if Dex rejects it.

Concurrency: two simultaneous requests in the refresh window can both call `attemptTokenRefresh`. Dex single-use refresh-rotation will let one succeed and reject the other; the rejected request's `cfg.Store.Revoke` will then kill the session. This is the documented concurrency-race finding in the per-story 9-14 review (CLEAN-2). Acceptable per operator policy — admin-only path with low concurrency.

### Migrations 000038 and 000039

- `000038` adds nullable `state_key` column + partial index. No grants change. Compatible with existing RLS.
- `000039` adds nullable `refresh_token` column on `admin_sessions`. Stored as TEXT. Encryption is application-layer (not DB). No new grants. Existing `admin_sessions` RLS unaffected.

Both migrations are reversible via the corresponding `*.down.sql`. Stories 9.15–9.18 add no new migrations.

### Admin user/room management RPCs

- All 5 user-management RPCs validate input on the Elixir side (`@valid_roles` allowlist for `update_user_role`, deactivate triggers `destroy_session/1` AFTER DB commit per the security invariant comment at line 1851).
- `archive_room_atomic/1` correctly uses `SELECT … FOR UPDATE` inside `Nebu.Repo.transaction/1` — no race window.
- `list_admin_users` does NOT return raw `email_encrypted` to the gateway; `mask_email/1` produces `u***@domain` per design.
- `get_server_config` excludes `oidc_client_secret` at the SQL level (line 304: `WHERE key != 'oidc_client_secret'`). The Elixir layer cannot leak the client secret even if the proto added a field. Defence-in-depth.
- `list_admin_room_members` (NEW, 9.18) returns only `user_id`, decrypted `display_name`, and `joined_at` per `AdminRoomMemberProto`. Email and other Tier 2 PII intentionally omitted.

### State-event whitelist (Story 9-6)

The whitelist is the authoritative source — verified that no other code path in `gateway/internal/matrix/` accepts a state event type that bypasses it. `set_room_state.go` (line 357) calls `allowedStateEventTypes[eventType]` BEFORE body decoding so unknown types are rejected before any work is done.

### Admin UI templates — XSS (Story 9.18 + 9.15)

The new `templates/rooms.html` member-list block emits:
```
<a href="/admin/users/{{ .UserID }}" class="...">
  {{ if .DisplayName }}{{ .DisplayName }}{{ else }}{{ .UserID }}{{ end }}
</a>
```
Both `.UserID` (Matrix user ID) and `.DisplayName` (decrypted Tier 1 PII) flow into HTML body / URL contexts. The template is `html/template`-rendered (`gateway/internal/admin/handler.go:5`), which auto-escapes:
- HTML body context: `<` `>` `&` `"` `'` are entity-encoded.
- URL context inside `href="..."`: `html/template` applies URL-percent-encoding for non-canonical URL characters. A Matrix user_id starts with `@` which `html/template` escapes correctly when embedded in a URL path component.

Verified: no `template.HTML` / `template.URL` / `safehtml` raw-string injection is used in any of the 9.18 / 9.15 changes. The 9.15 Direct-Chat fallback string `(Direct Chat &middot; {{ .MemberCount }} members)` substitutes a server-controlled integer — no user-controlled string flows into the fallback. CLEAN.

### Per-story prior reviews (no new findings)

Stories 9-9, 9-14, and 9-18 each carried 1-2 MEDIUM observations from the per-story reviewer. Re-checked all of them in current code; none have escalated:

- 9-9 MEDIUM (TOCTOU fail-open on DB error): still present at `room/server.ex:386-391`; documented as intentional fail-open philosophy mirroring `init/1`. Acceptable trade-off.
- 9-14 MEDIUM (issuer URL re-validation): `validateIssuerURL` runs at login (line 398) and callback (line 598). Refresh path does NOT re-validate (line 292 reads issuer from DB and trusts it). Acceptable: the issuer is set by the operator in `server_config`, not user-controlled.
- 9-14 MEDIUM (concurrency race on refresh): documented above; not exploitable.
- 9-18 INFO (`inspect(reason)` in gRPC error): inherited operational-log gap; not user-facing because the gateway swallows the error and renders an empty member list.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 3 |
| LOW       | 2 |
| INFO      | 0 |

## Pipeline Decision

- **CLEAN — no CRITICAL / HIGH findings. Pipeline may proceed.**

The 3 MEDIUM findings should be tracked as Epic 10 follow-up stories:

1. Refresh-token encryption-key separation from internalSecret (MEDIUM-1).
2. Flash-message allowlist update for the Epic 9 strings (MEDIUM-2).
3. Persist `max_members` in `rooms.max_members` from `update_room_settings/2` (MEDIUM-3).

The 2 LOW findings are defence-in-depth observations that can be deferred or accepted as risks depending on operator threat model.

Epic 9 is approved for closure from a security perspective.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
