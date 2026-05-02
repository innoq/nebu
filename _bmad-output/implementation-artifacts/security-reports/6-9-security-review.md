# Security Review — Story 6.9 Room Archivierung — 2026-05-01

**Agent:** Kassandra
**Diff base:** `git diff --staged`
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-opus-4-7[1m]`

## Executive Summary

The archive/unarchive flow is correctly gated by `instance_admin` role at the HTTP boundary, the gRPC channel is PSK-authenticated, and the append-only event log is preserved. Two real exploitable gaps remain: a TOCTOU race between `GetRoomStatus` and `Core.SendEvent` lets a client slip a single message into a room that the admin has just archived, and the `reason` field has no upper bound combined with no body-size limit on `/api/v1/admin/*` — both bounded in impact and limited to authenticated paths, so neither is CRITICAL. Read first: Findings 1 (race condition) and 3 (no body limit).

## Findings

### [HIGH] TOCTOU race: SendEvent can land an event after archive

- **CWE / OWASP:** CWE-367 (TOCTOU) / A04:2021 Insecure Design
- **Datei:** `gateway/internal/matrix/rooms.go:457–467`, `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1534–1563`
- **Beschreibung:** `PutSendEvent` calls `h.statusChecker.GetRoomStatus(ctx, roomID)` then, on `"active"`, proceeds to call `Core.SendEvent`. There is no second check on Core's side. Between the gateway's `GetRoomStatus` query and Core's `Room.Server.send_event/5`, the admin can complete `POST /archive`: the DB row flips to `archived`, the gRPC `ArchiveRoom` terminates the GenServer, but a concurrent `SendEvent` already past the status check still acquires (or re-spawns) the Room GenServer (Horde DynamicSupervisor lazily restarts on `via` lookup) and persists the event into the `events` table with a valid Ed25519 signature. The append-only invariant is preserved, but the archive contract — "no new events after archived_at" — is violated.
- **Impact:** A motivated user with co-conspirator timing can leave one message in a room after the admin has visibly archived it. The event_id and sender are recorded, so it is forensically attributable, but for compliance/legal-hold scenarios where archive marks an evidentiary cut-off, even a single post-archive event is a defect. Reputational risk is bounded — narrow window, append-only is intact, attribution preserved — hence HIGH not CRITICAL.
- **Empfehlung:** Add the archived check inside Core. In `Nebu.EventDispatcher.Server.send_event/2`, after `lookup_room` succeeds, call `rooms_db_module().get_room_status(room_id)` and raise `GRPC.RPCError` with `permission_denied` and message `"room is archived"` when status is `"archived"`. Alternatively, push the check into `Nebu.Room.Server.handle_call({:send_event, ...})` so it runs under the GenServer's own serialization. The gateway's pre-check stays as a fast-path 403 — but the authoritative check belongs next to the write.
- **Referenz:** OWASP ASVS V11.1.4 (state transitions); NIST SI-10 (input validation, integrity)

### [MEDIUM] `reason` field has no `maxLength`; no body-size limit on `/api/v1/admin/*`

- **CWE / OWASP:** CWE-400 / A04:2021 Insecure Design
- **Datei:** `gateway/api/openapi.yaml:209–216`, `gateway/internal/api/router.go:74–77`, `gateway/cmd/gateway/main.go:1143`
- **Beschreibung:** `ArchiveRoomRequest.reason` declares only `minLength: 10`. The handler decodes the full JSON body before validating length: `json.Decode → strings.TrimSpace(body.Reason)`. `RegisterAdminRoutes` does not wrap any route with `middleware.BodyLimitMiddleware`, so an `instance_admin` request can carry an arbitrarily large `reason` — gigabytes if the upstream proxy permits. Audit `LogEvent` caps metadata at 16 KiB and drops oversized payloads (good), but the gateway has already buffered the full JSON in memory by then. Stories 6.5/6.6/6.7/6.8 share the same gap; this story inherits it.
- **Impact:** Privileged DoS only — caller must already hold `instance_admin`. Memory amplification per request, no escape to unprivileged users. MEDIUM rather than HIGH because the trust boundary is high (admin-only).
- **Empfehlung:** Two changes. (1) Add `maxLength: 4096` to `ArchiveRoomRequest.reason` in `openapi.yaml` and rerun `make gen-api`; the generated validator will enforce it before the handler runs. (2) In `RegisterAdminRoutes`, wrap each `mux.Handle` chain with `middleware.BodyLimitMiddleware(64 * 1024)` exactly as `mux.Handle("POST /admin/users/{userId}/role", bodyLimit64KiB(...))` does in `main.go:318`. Track as a follow-up story so the fix lands consistently across the whole `RegisterAdminRoutes` surface.
- **Referenz:** OWASP ASVS V13.1.1 (request body size); CWE-400

### [MEDIUM] `GetRoomStatus` failures fail open in PutSendEvent

- **CWE / OWASP:** CWE-754 (improper check for unusual conditions) / A04:2021
- **Datei:** `gateway/internal/matrix/rooms.go:457–467`
- **Beschreibung:** When `GetRoomStatus` returns an error, the handler logs at warn level and proceeds — `slog.Warn(...) ; // fail-open`. A DB outage on the gateway's `rooms` table therefore lets every send pass through, including for archived rooms. The intent (documented in story Dev Notes) is availability over enforcement, since Core would otherwise be the authoritative gate — but Finding 1 shows Core has no such gate today.
- **Impact:** Combined with Finding 1, this means a transient `pg_bouncer` blip or replica lag yields *every* archived room temporarily writable. Bounded by the duration of the DB error and recovers automatically. MEDIUM — scope is narrow and documented as a deliberate trade-off; it becomes LOW once Finding 1 is fixed (Core enforces independently).
- **Empfehlung:** Either close the gap (return 503 `M_UNAVAILABLE` on DB error; clients retry) or — preferred — implement Finding 1's Core-side check so the gateway-side fail-open is a defense-in-depth fast path rather than the only check. Document the chosen trade-off in code, not just in the story file.
- **Referenz:** OWASP ASVS V11.1.7 (fail-secure on error)

### [INFO] gRPC `ArchiveRoom` cannot terminate arbitrary rooms — PSK gate verified

- **CWE / OWASP:** —
- **Datei:** `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex:30–48`, `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:1534`
- **Beschreibung:** Verified the threat model bullet "Kann ArchiveRoom missbraucht werden um andere Rooms zu terminieren?". The Elixir gRPC server is wrapped by `Nebu.Grpc.AuthInterceptor`, which mandates `x-nebu-node-token` and uses constant-time comparison against the shared secret. The HTTP path that calls it (`AdminServer.ArchiveAdminRoom`) is gated by `RequireRole("instance_admin", checker)`. No path exists for a non-admin or external caller to drive `Horde.DynamicSupervisor.terminate_child` for a chosen room. Recording for audit completeness.
- **Impact:** None — confirms the design holds.

### [INFO] Append-only invariant preserved for archive operations

- **Datei:** `gateway/internal/api/rooms_repo.go:291–352`
- **Beschreibung:** `ArchiveRoom` and `UnarchiveRoom` only mutate `rooms.status`, `rooms.archived_at`, and `rooms.archive_reason`. No `DELETE` or `UPDATE` against `events`. `audit_log` retains its `audit_log_no_update` and `audit_log_no_delete` policies (migration 000018, lines 32 and 38). The story intentionally preserves the event history for compliance — confirmed.

### [INFO] DoS via archiving a busy room is by design, not a vulnerability

- **Datei:** `_bmad-output/implementation-artifacts/6-9-room-archivierung-...md` (story spec, AC#1)
- **Beschreibung:** No "active members" precondition before archive. An `instance_admin` can archive a room with 5,000 active users; subsequent sends from those users return 403 `M_ROOM_ARCHIVED`. The story explicitly defines this behaviour (`PUT /send/* returns 403 M_ROOM_ARCHIVED`) — read paths (`/messages`, `/sync`) continue to work. This is the intended product behaviour for compliance archival, not a security flaw. Recording so the next reviewer doesn't re-raise it.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅     |
| `reason` field on compliance access         | ✅ — `reason` validated `≥10` chars before DB write; included in audit metadata |
| Audit-log immutability                      | ✅ — RLS on `audit_log` (no UPDATE/DELETE) untouched |
| `instance_admin` notification (if in-scope) | ✅ — N/A; archive is itself an admin action, no escalation hook required |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ — routes wrapped by `jwtMW`; same chain as 6.5–6.8 |
| Matrix Power Level checks                   | ✅ — admin RBAC supersedes power levels per Story 6.x design (instance_admin overrides) |
| No hardcoded secrets                        | ✅     |
| TLS 1.3 enforcement                         | ⚠️ — not changed by this diff; see Epic 5 baseline |
| AES-256-GCM correctness                     | ✅ — no crypto added |
| Ed25519 verify-before-accept                | ✅ — Room.Server signing flow unchanged |
| No secrets in logs / error messages         | ✅ — `slog.Warn(... err: grpcErr)` paths surface only gRPC status codes |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 2 |
| LOW       | 0 |
| INFO      | 3 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning. The HIGH finding (Core-side archived check missing) must be scheduled as a follow-up story or explicitly accepted before the next release. The two MEDIUM findings should be folded into the same follow-up to land the body-size and `maxLength` controls together.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
