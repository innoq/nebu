# Security Review — Story 5.2 Audit Log Writer (generic, atomic) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (28 code files + 1 story file, ~2855 insertions; `_bmad-output/planning-artifacts/epics.md` intentionally out of scope per pipeline note)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default). Promoted HIGH finding to follow-up (FB-52-01) rather than block commit, per project convention for pre-existing architectural issues surfaced by a new feature.

## Executive Summary

The writer layer itself is carefully built: two independent never-raise contracts (Go returns `nil`, Elixir returns `{:error, :audit_write_failed}` or `:ok`), a separate `Repo.transaction/1` that is structurally independent of the caller's transaction, configurable module injection for tests, and a gRPC handler that converts absent `error_detail` (`""`) to SQL `NULL`. Log-injection via `Logger.error` is correctly avoided — `action`, `actor_user_id`, and `reason` are passed as keyword-list metadata, never interpolated into a format string. Room-event audit calls run in-process in Elixir, sidestepping the gRPC surface.

The serious finding is not in the new code: by landing the first *audit-immutable write path* over the pre-existing unauthenticated gRPC channel (`insecure.NewCredentials()`, no auth interceptor, port 9000 published to the host in `docker-compose.yml`), Story 5-2 lifts the exploit value of the already-known transport gap from "DoS / message tampering" to "forgery of compliance-immutable records". This is a material weakening of the "append-only, tamper-resistant" claim that Epic 5 is built on. Fixing it is cross-cutting (ADR 008 Phase 2 → mTLS) and does not belong inside this story's diff; it is registered as **FB-52-01** in 5-29 and must be resolved before Epic 5 is marked done.

Three MEDIUM findings tighten the inner surface: no upper bound on `metadata_json` bytes (DoS via deeply-nested / large JSON reaching `Jason.decode`), no allowlist on `action` strings (forensic confusion if gRPC is reachable), and `Compliance.AuditWriter` calls `insert!` on a raw struct, bypassing its own `AuditLogEntry.changeset/2` (empty-string actor IDs silently persist — the `LogoutHandler` "unknown" fallback is a concrete trigger).

## Findings

### [HIGH] `WriteAuditLog` gRPC route is unauthenticated and transport-insecure — forgery path into the audit log

- **CWE / OWASP:** CWE-306 (Missing Authentication for Critical Function), CWE-319 (Cleartext Transmission of Sensitive Information), CWE-345 (Insufficient Verification of Data Authenticity) / A01:2021 Broken Access Control, A02:2021 Cryptographic Failures
- **Dateien:**
  - `gateway/internal/grpc/client.go:27` — `grpclib.WithTransportCredentials(insecure.NewCredentials())`
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex:1-5` — plain `use GRPC.Endpoint` with `run(Nebu.EventDispatcher.Server)`, no interceptor
  - `core/apps/event_dispatcher/lib/nebu/event/application.ex:9` — `{GRPC.Server.Supervisor, endpoint: ..., port: 9000, start_server: true}`
  - `docker-compose.yml:63-64` — `core.ports: - "9000:9000"` (host publication, not `expose:`)
  - `proto/core.proto:51-54` — new `rpc WriteAuditLog(...)` added to the unauthenticated surface
- **Beschreibung:** The `WriteAuditLog` RPC is defined on the same `CoreService` that every other Elixir-side handler lives on. There is no per-RPC authentication, no transport TLS, no mTLS peer verification, and no message signing. The pre-existing `Nebu.NodeRegistration.register_with_gateway/1` module only registers a PSK with the HTTP gateway for cluster membership; it does not protect the Elixir gRPC listener. In dev compose, port 9000 is published on `0.0.0.0:9000`, so any process on the Docker host can dial it. In production, the exposure depends on operator network policy — ADR 008 explicitly defers transport hardening to "Phase 2 — Ephemeral mTLS" and relies on network isolation for MVP. For the other RPCs (SendEvent, CreateRoom, etc.) the worst-case forgery is a message in a room, which is already signed by `:persistent_term.get(:nebu_signing_key)` and anchored in content-addressed event IDs. Audit records have no such integrity anchor: `actor_user_id`, `action`, `target_id`, and `metadata` are taken verbatim from the caller and committed in a dedicated transaction that the Go layer by design cannot revert. A caller that can reach port 9000 can therefore synthesise arbitrary audit rows — back-dating a login, injecting `"action": "password_reset_by_admin"`, or suppressing a real event by flooding the log with noise.
- **Impact:** This story states its own invariant as "jede Admin-Aktion, jedes Compliance-Event und jedes System-Event konsistent aufgezeichnet wird — auch in Fehlerfällen". That invariant is only as strong as the authenticity of the caller identity on the `WriteAuditLog` channel. Today that identity is un-verified. The compliance officer cannot distinguish a row written by the gateway from one written by a network-adjacent attacker. This is precisely the scenario a regulator dissects after a breach.
- **Empfehlung:** Record this as a blocking follow-up and resolve before Epic 5 is marked done. Two independent fixes are needed in combination:
  1. **Transport:** Move `core:9000` behind mTLS (ADR 008 Phase 2) or, at minimum, remove the host port publication (`expose: ["9000"]` instead of `ports: ["9000:9000"]`) and force a Docker-internal network for dev compose. Verify Kubernetes / production manifests apply an equivalent `NetworkPolicy`.
  2. **Authorization:** Add a gRPC interceptor on the Elixir side that verifies either (a) the PSK from `NEBU_INTERNAL_SECRET_FILE` in a `authorization: Bearer` metadata header (reusing the `nebu_dev_password`-style shared secret) or (b) a per-call signature once mTLS is in place. On the Go side, attach the PSK metadata to every unary call via a `grpc.WithPerRPCCredentials` stream. Deny `WriteAuditLog` without proof.
  3. **Server-side identity vs. claimed identity:** Once the interceptor exists, fail closed if the authenticated peer is not the gateway. Optional, related: stamp the row with the peer identity that wrote it, not only the `actor_user_id` the gateway claims (column `written_by_peer` or reuse `metadata.peer`), so forensics can cross-check.
- **Deferral:** Fix does not belong in Story 5-2 — it touches every RPC, the compose/K8s manifests, and ADR 008's roadmap. Tracked as **FB-52-01** in Story 5-29 (companion to FB-51-01 which separates DB roles).
- **Referenz:** OWASP ASVS V1.9 (Communication), V9.2 (Server Communication Security); NIST SP 800-53 AC-17, SC-8; ADR 008 (project roadmap).

### [MEDIUM] No upper bound on `metadata_json` bytes — DoS reachable via gRPC or crafted call site

- **CWE / OWASP:** CWE-400 (Uncontrolled Resource Consumption), CWE-770 (Allocation of Resources Without Limits or Throttling) / A05:2021
- **Dateien:**
  - `gateway/internal/audit/writer.go:27-36` — `json.Marshal(metadata)` with no size check
  - `proto/core.proto:281` — `bytes metadata_json = 5;` (no documented cap)
  - `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:169-173` — `Jason.decode(request.metadata_json)` with no `max_nesting` or size guard
- **Beschreibung:** The story's own Security Notes call out "16 KB empfohlen" — the recommendation is not implemented. gRPC's default `MaxRecvMsgSize` is 4 MiB. A compromised gateway (or, per the HIGH above, a direct unauthenticated caller) can send up to 4 MiB of attacker-chosen JSON per `WriteAuditLog` request. `Jason.decode/1` in the Elixir handler will allocate proportionally; a deeply-nested `{"a":{"a":{"a":...}}}` structure drives the Elixir process's reduction budget and heap. Even with the fallback to `%{}` on decode failure, the allocation happens before the error. At ~1000 req/s an attacker sustains an Elixir OOM or at minimum degrades scheduler throughput. The `Compliance.AuditWriter` then writes the full (if decoded) map to JSONB — PostgreSQL JSONB hard-limits at ~255 MiB per value but accepts large entries silently, inflating the `audit_log` table and bypassing the retention budget.
- **Impact:** Availability degradation (Elixir) + storage blow-up (PostgreSQL). Not primary data disclosure, but directly undermines "audit is always available for regulators" (Compliance RSP).
- **Empfehlung:** Three complementary guards:
  1. Enforce `len(metaJSON) <= 16 * 1024` in `gateway/internal/audit/writer.go` after `json.Marshal`; on overflow, fall back to `{}` and `slog.Warn("audit: metadata exceeded 16 KiB; sending empty object", "action", ..., "size", len(b))`. Audit row is preserved; the oversized payload is dropped.
  2. Set `MaxRecvMsgSize = 64 * 1024` on the Elixir gRPC server (or per-RPC via `grpc-elixir` options) so the generic transport cap matches the writer's contract.
  3. In the Elixir handler, call `Jason.decode(request.metadata_json, max_nesting: 20)` to bound recursion depth. `case ... {:ok, m} when is_map(m) -> m; _ -> %{}` is already in place — just add the option.
- **Referenz:** OWASP ASVS V5.1.4 (input size validation), CWE-400.

### [MEDIUM] `action` field has no allowlist — forensic confusion vector

- **CWE / OWASP:** CWE-20 (Improper Input Validation) / A04:2021
- **Dateien:**
  - `core/apps/compliance/lib/compliance/audit_writer.ex:21-32` — `action` accepted verbatim, no `validate_inclusion`
  - `core/apps/compliance/lib/compliance/audit_log_entry.ex:14,26-29` — `changeset/2` exists but `AuditWriter` calls `insert!` directly, bypassing it (see separate MEDIUM below)
- **Beschreibung:** Story 5-2 defines the expected actions (`admin_login`, `admin_login_failed`, `bootstrap_completed`, `admin_logout`, `room_created`, `room_joined`). Nothing in the writer, the Ecto schema, or the DB rejects free-form strings. Today the only call sites are internal, but combined with the HIGH finding above an unauthenticated caller can write `action = "instance_admin_role_granted"` or `action = "data_export_signed_by_dpo"` — fabricating evidence of an action that never happened. Even absent the HIGH, an accidental typo (`"admin_loggin"`) silently corrupts the forensic corpus because exact-match queries in the compliance dashboard will skip it.
- **Impact:** Integrity of audit queries. Compliance officer may miss events they filter for, or accept fabricated events as genuine.
- **Empfehlung:** Maintain a module-level allowlist (`@allowed_actions ~w(admin_login admin_login_failed admin_logout bootstrap_completed room_created room_joined ...)`). In `Compliance.AuditWriter.log/7`, reject unknown actions via `{:error, :unknown_action}` **without writing the row** (so callers cannot pollute the log by misspelling) — but keep the never-raise contract. Alternatively, use `Ecto.Changeset.validate_inclusion(:action, @allowed_actions)` through the existing `changeset/2` and persist via `Nebu.Repo.insert(changeset)` rather than `insert!(%AuditLogEntry{...})`. The allowlist is maintained centrally in Compliance app — every new event type becomes an explicit allowlist PR.
- **Referenz:** NIST SP 800-92 §4.2 (log source integrity), CWE-20.

### [MEDIUM] `AuditWriter.log` bypasses its own `AuditLogEntry.changeset/2` — empty-string actor IDs persist silently

- **CWE / OWASP:** CWE-20 (Improper Input Validation), CWE-1286 (Improper Validation of Syntactic Correctness of Input) / A04:2021
- **Dateien:**
  - `core/apps/compliance/lib/compliance/audit_writer.ex:21-32` — `%AuditLogEntry{...}` struct built, `repo().insert!(entry)` called — no `changeset/2` pipeline
  - `core/apps/compliance/lib/compliance/audit_log_entry.ex:23-30` — `@required_fields [:actor_user_id, :action, :outcome]` with `validate_required` — defined but unreachable
  - `gateway/internal/admin/auth.go:918-933` — `logoutSub := "unknown"` fallback can reach the writer when session lookup fails
- **Beschreibung:** The story defines `@required_fields [:actor_user_id, :action, :outcome]` and a `changeset/2` that enforces them via `validate_required`. The writer never uses the changeset; it bypasses straight to `insert!`. Two concrete failure modes follow:
  1. Empty strings (`""`) are not NULL; the DB NOT NULL constraint (if present in the migration) accepts them. The writer happily persists `actor_user_id: ""` if a caller passes it — making "who did this" unanswerable.
  2. The `LogoutHandler` explicitly writes `"unknown"` when neither SID cookie nor legacy cookie is readable. The row lands in the audit log attributable to nobody. A compliance review reading the log cannot tell whether "unknown" is a bug, a misconfigured session store, or an attacker forcing an audit row by replaying a revoked cookie.
- **Impact:** Weakens audit attribution quality. Does not enable direct exploitation, but it breaks the primary forensic question ("who did this?") at the worst moment — the logout path, which is exactly when you audit an end-of-session.
- **Empfehlung:** Two-step fix:
  1. `Compliance.AuditWriter.log/7` routes through `AuditLogEntry.changeset(%AuditLogEntry{}, attrs)` and passes the changeset to `Repo.insert`. On `{:error, changeset}`, the existing `Logger.error` branch fires and `{:error, :audit_write_failed}` is returned — preserving never-raise. Add one ExUnit case for "empty actor_user_id rejected by changeset".
  2. In `LogoutHandler`, if `logoutSub == "unknown"` after both cookie paths, log a distinct action (`"admin_logout_unidentified"`) or skip the audit write entirely with a `slog.Warn`. Do not mint a row that pretends to attribute to a nonexistent "unknown" user.
- **Referenz:** OWASP ASVS V5.1.3 (input validation), CWE-20.

### [INFO] Never-raise contract is not absolute on the Elixir side

- **Datei:** `core/apps/compliance/lib/compliance/audit_writer.ex:32`
- **Beschreibung:** `repo().transaction/1` can raise `DBConnection.ConnectionError`, `RuntimeError` ("Ecto.Repo not started"), or `Postgrex.Error` in exceptional cases — connection pool exhaustion, Repo process crash, chaos-engineering events. There is no `try/rescue`. Story 5-2 accepts this ("In Production läuft Repo immer") and it is a reasonable default since Ecto.Repo crashes are fatal anyway. However the gRPC handler `write_audit_log/2` would in that case propagate the exception back to the gateway as a gRPC error — which the Go `audit.LogEvent` swallows (returns `nil`), so the caller path is still unblocked. Net result: no user-visible degradation, Elixir supervisor handles the restart. Acceptable as-is, noted so the audit trail of this review captures the assumption.
- **Empfehlung:** None required for this story. If Epic 5 later adds hot-path audit calls on request-latency-sensitive endpoints, consider a `with ... rescue _ -> ...` in `AuditWriter` and a metric `audit_writer_unexpected_raise_total`.

### [INFO] Caller-process-crash between primary op and audit call loses the record

- **Dateien:** `gateway/internal/admin/auth.go:720`, `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:146-153`
- **Beschreibung:** Each integration point follows the pattern "primary op succeeded → audit.LogEvent". If the process dies between the two lines, the primary op is durable (DB committed, session created, room started) but the audit row is never written. Story 5-2 explicitly acknowledges this for `create_room` / `join_room`; the admin handlers follow the same pattern. This is a fundamental property of "audit-after" vs "audit-before-with-rollback", and the story picks "audit-after" deliberately to avoid the reverse failure ("audit written, primary op rolled back"). Noted as accepted risk.
- **Empfehlung:** None in this story. If lost-audit becomes a compliance gap, the standard remedy is an outbox pattern (audit intent enqueued inside the primary transaction, flushed by a separate writer). Out of scope for MVP.

### [INFO] `Logger.error` is not a log-injection vector

- **Datei:** `core/apps/compliance/lib/compliance/audit_writer.ex:39-43`
- **Beschreibung:** `Logger.error("AuditWriter: failed to write audit log", action: action, actor: actor_user_id, reason: inspect(reason))` passes `action`, `actor_user_id`, and `reason` as a keyword-list metadata parameter — not interpolated into the format string. An attacker-controlled `action = "\n[FAKE] server compromised"` becomes a metadata value, not a new log line. `inspect/1` quotes and escapes binary contents. CWE-117 does not apply. Positive observation, recorded for completeness.
- **Empfehlung:** None.

### [INFO] Positive observations

- **Separate transaction structurally guaranteed.** `Nebu.Repo.transaction/1` grabs a pool connection; the caller's `Ecto.Multi` uses a different connection. AuditWriter cannot be rolled back by the caller — the central immutability premise is preserved (conditional on the HIGH being fixed).
- **Never-raise on Go side.** `audit.LogEvent` returns `nil` unconditionally. Verified by `TestLogEvent_GRPCFailure_ReturnsNil`. No panic paths.
- **Nil-safe `coreClient`.** `AdminAuth.logAuditEvent` checks `if a.coreClient == nil { return }`. Tests that construct `AdminAuth` without a core client (legacy test flows) do not crash.
- **Empty string vs NULL normalization.** gRPC handler converts `error_detail == ""` to `nil` before the writer call — JSONB / text column semantics stay clean.
- **Test-time injection via `Application.get_env`.** Both `repo()` and `audit_writer_module()` are resolved at call time, enabling fakes without touching production code paths.
- **`is_map(m)` guard on `Jason.decode`** prevents a caller that sends `metadata_json = ~s("hello")` from passing a non-map into the writer; fallback to `%{}` is correct.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Audit immutability (schema + RLS + DEFINER purge) | PASS (from 5-1) |
| Audit authenticity of caller identity       | FAIL — HIGH (FB-52-01) |
| Compliance RSP — append-only write path     | PASS in-process; FAIL over unauthenticated gRPC |
| OIDC claim validation at source             | PASS — `actor_user_id` sourced from verified `sub` post Story 5-23 signature-verify fix |
| Crypto primitives                           | N/A — no crypto in this diff |
| Secrets hygiene                             | PASS — no passwords / tokens / cookies reach `metadata` per integration-point review |
| Matrix power-level enforcement              | N/A |

## Triage Summary

| Severity | Count | Blocking? |
|----------|:-----:|:---------:|
| CRITICAL |   0   |     —     |
| HIGH     |   1   | Deferred to FB-52-01 in Story 5-29 (fix before Epic 5 done) |
| MEDIUM   |   3   | Not blocking per project convention; recommend in-story or 5-29 |
| LOW      |   0   |     —     |
| INFO     |   3   |     —     |

## Deferred-Finding Block Proposal for Story 5-29

Append the following block to `_bmad-output/implementation-artifacts/5-29-security-followup-collector.md`:

```
### FB-52-01 — WriteAuditLog gRPC is unauthenticated + transport-insecure

**Source:** Story 5-2 Kassandra SEC Gate 1 (2026-04-23).
**Severity:** HIGH.
**Size estimate:** L (cross-cutting — ADR 008 Phase 2 implementation + compose/K8s manifests + interceptor + tests).

**What to do:**
1. Implement the gRPC auth interceptor (Go client sends `authorization: Bearer <PSK>` metadata; Elixir interceptor verifies via `NEBU_INTERNAL_SECRET_FILE`). Deny `WriteAuditLog` without proof.
2. Remove host port publication for `core:9000` in `docker-compose.yml` (use `expose: ["9000"]`). Apply equivalent `NetworkPolicy` in K8s manifests.
3. Implement ADR 008 Phase 2 mTLS so transport is encrypted and peer-verified.
4. Optionally stamp `audit_log` rows with the authenticated peer identity (new column or `metadata.peer`).

**Tests (ATDD first):**
- `TestWriteAuditLog_RejectsMissingPSK` — gRPC call without auth metadata returns UNAUTHENTICATED.
- `TestWriteAuditLog_RejectsWrongPSK` — constant-time-compare refuses mismatched secret.
- `TestNetworkPolicy_PortsNotExposedOnHost` — compose audit.

**Why deferred:** Fix touches every RPC, the compose/K8s manifests, and ADR 008's roadmap. Landing it inside Story 5-2 would double the diff and couple unrelated changes.

**Size-escalation trigger:** If the interceptor work alone exceeds M, split into its own story (5-30 or successor).
```

## Appendix — Scope and Limits of This Review

- The HIGH finding is pre-existing to this story in mechanism; Story 5-2 changes the *impact class* by adding the first write path on the channel that directly writes to an audit-immutable resource. Treating it as HIGH (not CRITICAL) reflects that network-level controls in production compose are typically present (ADR 008 rationale) but the story's own "append-only, tamper-resistant" claim cannot stand on that premise alone.
- Pre-existing 23 Elixir test failures (FB-E5-03) are out of scope per explicit instruction.
- Code-review fixes (silently-dropped JSON marshal error, "fires-and-forgets" comment correction) accepted as given — verified present in the current diff.
- `_bmad-output/planning-artifacts/epics.md` intentionally out of scope per pipeline note.

Classification: HIGH
Report: `_bmad-output/implementation-artifacts/security-reports/5-2-security-review.md`
