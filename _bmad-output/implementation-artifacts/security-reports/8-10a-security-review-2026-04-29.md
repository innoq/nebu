# Security Review — Story 8-10a Pre-Release Bug Fixes (Invite :pg Notification + Profile Upsert on Login) — 2026-04-29

**Agent:** Kassandra
**Diff base:** `git diff --staged`
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

The staged diff fixes two functional bugs (invite long-poll wake-up, profile row provisioning on login) inside `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex`. No new HTTP routes, no new SQL, no crypto changes, no auth path changes. All new code runs on already-authorized paths after token validation and power-level checks. Two minor observations are recorded as INFO; no actionable security findings.

## Findings

### [INFO] `:pg` group key is built from validated invitee_id — confirmed safe

- **CWE / OWASP:** CWE-20 (Input Validation), considered as defense-in-depth
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:300-301`
- **Beschreibung:** The new broadcast `:pg.get_local_members("user:#{invitee}") |> Enum.each(&send(&1, {:new_invite, room_id}))` constructs the `:pg` group key from the gRPC `request.invitee_id`. The group key is a binary, not an atom — there is no `String.to_atom/1` call, so CWE-400 atom exhaustion does not apply. The receivers of the broadcast are all sync-task processes spawned by `do_incremental_sync`, which join `"user:#{user_id}"` where `user_id` comes from `Nebu.Grpc.Metadata.trusted_identity(stream)` (validated x-user-id header). An attacker forging an `invitee_id` to wake another user's long-poll succeeds only if the inviter is already authenticated, a member of the room, and has the `:invite` power level (lines 283-293). The wake-up causes the victim's sync to return `[]`; the Go gateway's `buildInviteRooms` then fetches invite data via authorization-scoped SQL. No data leak, no privilege escalation.
- **Impact:** None beyond a bounded amplification of legitimate invite traffic — an authorized member can already trigger this path by issuing a real invite. Recording for transparency.
- **Empfehlung:** None required. Worth considering as a future hardening step: emit a metric counter for `:pg` invitee broadcasts so abnormal invite-burst patterns surface in observability.
- **Referenz:** OWASP ASVS V5.1.3 (input validation completeness), STRIDE — Repudiation/DoS lens

### [INFO] Profile upsert overwrites custom display name on every successful login

- **CWE / OWASP:** N/A — data-integrity/UX concern, not a security finding
- **Datei:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:582-589`, `core/apps/event_dispatcher/lib/nebu/profile/db.ex:8-11`
- **Beschreibung:** When the OIDC claim provides a non-empty `display_name`, `validate_token/2` calls `upsert_profile(user_id, display_name, nil)`. The SQL `COALESCE(EXCLUDED.displayname, profiles.displayname)` therefore preserves an existing displayname only when the login carries `nil`/empty — but each successful login with a non-empty claim will overwrite a value the user may have set via `PUT /profile/{userId}`. This is functional behaviour, not a vulnerability: the overwrite is performed by the user's own session against their own profile row, with no path for another user to influence it. Recorded so the team is aware before release.
- **Impact:** A user who customises `displayname` via the Matrix profile API will see the custom value reverted to the OIDC `name` claim on the next login. No security impact.
- **Empfehlung:** Optional product decision — consider passing `nil` (instead of the OIDC `display_name`) on subsequent logins to make the OIDC value an *initial* default rather than an authoritative override. Out of scope for this security review.
- **Referenz:** None

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no compliance tables touched) |
| `reason` field on compliance access         | ✅ (no compliance access path touched) |
| Audit-log immutability                      | ✅ (no migration; no UPDATE/DELETE on audit tables) |
| `instance_admin` notification (if in-scope) | ✅ (not in scope) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (validation remains in `Nebu.Session.TokenValidator.validate`; new code only runs on the `{:ok, user}` branch) |
| Matrix Power Level checks                   | ✅ (`invite_user/2` retains `Nebu.Room.PowerLevels.can?(state.power_levels, inviter, :invite)` at line 289 — pre-existing, unchanged) |
| No hardcoded secrets                        | ✅ |
| TLS 1.3 enforcement                         | ✅ (no TLS config changes) |
| AES-256-GCM correctness                     | ✅ (no crypto changes) |
| Ed25519 verify-before-accept                | ✅ (no signature path changes) |
| No secrets in logs / error messages         | ⚠️ Partial — `Logger.warning("validate_token: profile upsert failed for #{user_id}: #{inspect(reason)}")` interpolates a Postgrex error term. Postgrex errors do not normally include connection-string fragments, but `inspect/1` could surface internal schema details if the DB returns a cryptic error. Acceptable for warning-level server logs. To verify fully, enumerate Postgrex error shapes; not blocking. |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 0 |
| INFO      | 2 |

## Pipeline Decision

- **CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The two INFO entries are observations; neither requires a follow-up story. The displayname-overwrite behaviour is a product decision worth noting before release.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
