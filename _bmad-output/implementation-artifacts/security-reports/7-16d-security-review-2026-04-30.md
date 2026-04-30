# Security Review: Story 7.16d — gRPC Auth: HTTP 404 statt Unauthenticated (SEC Gate 1)

**Date:** 2026-04-30
**Reviewer:** Kassandra (Security Agent)
**Story under review:**
- 7.16d — gRPC auth interceptor not running for some RPC paths (`security_review: required`)

**Diff base:** `HEAD` (working-tree changes), branch `feature/github-readiness`
**Changed files in scope:**
- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex` (modified — 4 added `rpc` registrations)

**Files reviewed for impact (not modified by this story):**
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex` — handler bodies for the 4 newly-registered RPCs
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex` — `intercept(Nebu.Grpc.AuthInterceptor)` wiring
- `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex` — PSK comparison + fail-secure
- `core/apps/compliance/lib/compliance/audit_writer.ex` — action-allowlist + changeset
- `core/apps/compliance/lib/compliance/audit_log_entry.ex` — Ecto schema (required-fields)
- `core/apps/compliance/lib/compliance/user_deletion.ex` — atomic key-deletion + failure-invariant audit
- `core/apps/event_dispatcher/lib/nebu/profile/db.ex` — parameterised UPSERT (no SQLi)
- `gateway/internal/matrix/profile.go` — avatar-URL mxc-prefix + `isValidMxcURI` path-traversal guard
- `gateway/internal/audit/writer.go` — never-raise audit writer with 16 KiB metadata cap
- `gateway/internal/compliance/user_key_deletion.go` — DELETE /admin/users/{userId}/keys + role gate

**Classification:** **CLEAN**
**Severity counts:** CRITICAL: 0 · HIGH: 0 · MAJOR: 0 · MINOR: 1 · LOW: 2 · INFO: 3
**Blocking severity threshold:** CRITICAL/HIGH (per project policy — SEC Gate 1)
**Decision:** **APPROVED — no blocking issues, no follow-up story required**

---

## Executive Summary

The fix is small and correct: 4 missing `rpc :Name, ReqType, RespType` lines were added to `Core.CoreService.Service` in `core_grpc.pb.ex`. The change closes a routing gap in `grpc-elixir 0.11.x`: when the service definition does not include an `rpc` entry, the HTTP/2 router rejects the path with HTTP 404 **before** any registered interceptor runs. The interceptor `Nebu.Grpc.AuthInterceptor` is wired correctly in `endpoint.ex` (`intercept(Nebu.Grpc.AuthInterceptor)`), but it only fires for RPCs that resolve through the registered service — the missing entries meant 4 RPCs short-circuited at routing without invoking auth.

The crucial observation for this review is that **the missing-registration condition was a defence-in-depth gap, not an exploitable vulnerability** in itself: an unregistered RPC at `grpc-elixir` returns 404 *before* it dispatches to the handler module — the handler function in `server.ex` was never reached on a missing-registration path. So the pre-fix state could not be used to bypass auth and reach `WriteAuditLog`/`DeleteUserKeys`/`UpdateProfile`/`GetPresence` handlers; both the auth check **and** the handler invocation were skipped together.

Where the pre-fix state was genuinely dangerous was as a **silent test bypass**: a developer adding a new RPC and forgetting to register it would see 404s in tests but never observe the auth interceptor's negative path, which is a maintenance landmine. After the fix all 4 RPCs route through the interceptor correctly. Each newly-routed RPC also has handler-level defences in place (admin-role gate for `DeleteUserKeys`, defence-in-depth `request.user_id == metadata.user_id` for `UpdateProfile`, action allowlist + Ecto changeset for `WriteAuditLog`).

The original SEC Gate 1 questions:

1. **Before the fix — could an unauthenticated attacker actually reach the handler?** — **No.** With `grpc-elixir`, an RPC missing from the service definition is rejected at routing with HTTP 404 (status `Unimplemented`). The handler function in `Nebu.EventDispatcher.Server` was never invoked. No DB rows could be written, no keys deleted, no profiles updated, no presence read — confirmed by the existing integration test `TestAuditForgery_NoRowInserted` which the story makes pass. The pre-fix state was a silent **defence-in-depth** failure (auth never enforced) but the redundant routing-layer 404 happened to provide the same external behaviour as a deny. After the fix, the deny is now enforced *by auth* (`UNAUTHENTICATED`) instead of *by routing accident* (`UNIMPLEMENTED` / 404), which is what the integration tests assert.

2. **After the fix — are all 4 newly-registered RPCs properly protected?** — **Yes.** The interceptor pipeline in `endpoint.ex` wraps `Nebu.EventDispatcher.Server` for the *whole* `Core.CoreService.Service`. There is no per-RPC exemption mechanism in `Nebu.Grpc.AuthInterceptor.call/4`; every `next.(req, stream)` is gated on `verify_token/1 == :ok`. The PSK comparison is constant-time via `:crypto.mac` HMAC equality with a fresh random key (no early-return on prefix mismatch), and the secret read path is fail-secure (empty file or missing env → reject all). The new `rpc :GetPresence`, `:UpdateProfile`, `:WriteAuditLog`, `:DeleteUserKeys` entries inherit this exact pipeline — verified by reading both the service definition and the endpoint module.

3. **`DeleteUserKeys` — is it properly protected? Are there additional authorization checks beyond the PSK?** — **Yes.** Defence-in-depth is layered:
   - **L1 (gRPC):** PSK verified by `Nebu.Grpc.AuthInterceptor` — only the gateway's own secret-file holder can dial the RPC.
   - **L2 (HTTP at the gateway):** `gateway/internal/compliance/user_key_deletion.go` requires JWT (jwtMiddleware), then a role gate `if systemRole != "instance_admin"` returns 403.
   - **L3 (Core business logic):** `Compliance.UserDeletion.delete_user_keys/3` runs Steps 2–5 in a single Ecto transaction with an **atomic conditional UPDATE** in `mark_in_progress` (`WHERE deletion_status IS NULL OR deletion_status = 'active'` + `RETURNING user_id`). This closes the TOCTOU race with the early-exit guard, so concurrent deletions cannot stomp on each other.
   - **L4 (Audit failure-invariant):** `emit_attempted_audit/4` writes a `user_keys_deletion_attempted` row in a **separate** Repo transaction so that DB failures during deletion are still recorded.

   One small caveat: the gateway sets `AdminUserId = callerSub` from the JWT context (`middleware.ContextKeySub`) and Core trusts this value. This is correct given the trust model (PSK-authenticated gateway is allowed to assert the caller's identity), but it means the RPC has no independent way to verify the admin identity — Core relies on the gateway. This is consistent with all other RPCs (`get_messages`, `set_power_levels`, `update_profile`) that read identity from gRPC metadata via `Nebu.Grpc.Metadata.trusted_identity/1`. Worth noting, not a finding: the trust boundary is explicit and the gateway is the only PSK holder. See [INFO-1].

4. **`UpdateProfile` — can it be used to inject malicious avatar_url values that bypass the XSS scrubbing already in place?** — **No** for the production HTTP→gRPC path; **theoretically yes** for any direct gRPC caller, but they would need the PSK. Walking the layers:
   - The **HTTP gateway** path `PUT /_matrix/client/v3/profile/{userId}/avatar_url` enforces `strings.HasPrefix(uri, "mxc://")` AND `isValidMxcURI(uri)` (rejects path-traversal via `..`/`.`/`/`/`\`/`\x00` segments — `gateway/internal/matrix/profile.go:178–235`). javascript:, data:, vbscript:, file: schemes are blocked at this layer.
   - The **Core handler** `update_profile/2` does NOT re-validate the URL format — it accepts the value, normalises empty-string to nil, and forwards to `profile_db_module().upsert_profile/3` which performs a parameterised SQL UPSERT.
   - The **Story 7.16f** companion bugfix adds a database-level `migration 000026_avatar_url_scrub` that scrubs javascript:/data:/vbscript:/file: legacy values, but this story (7.16d) does not include that migration.

   Risk assessment: a direct gRPC caller (PSK holder, i.e. another gateway instance) could write an arbitrary `avatar_url` string into `profiles.avatar_url`. Since the only PSK holder is the gateway itself and the gateway already scrubs at HTTP write time, the practical attack surface is limited. The scrubbing is applied at *storage time* by the gateway and at *read time* by `GET /profile/{userId}` (no execution context — the value is returned as JSON `avatar_url` field, which the Matrix client renders as URL not as HTML). However, defence-in-depth would benefit from `validate_required` on Core side too. See [MINOR-1] for the recommendation.

5. **`WriteAuditLog` — now that it's properly auth-protected, are there any audit-log injection risks?** — **Three small layered defences are in place; one missing field-validation gap.**
   - **action allowlist:** `Compliance.AuditWriter.@known_actions` (lines 25–43) hard-codes the 17 valid action strings. Unknown values are rejected with `{:error, :audit_unknown_action}` and a `Logger.error` entry — no row written. This blocks attacker-controlled action strings (e.g. `room_created\nadmin_login` log-injection attempts via newline embedding) at the action vocabulary level.
   - **Ecto changeset:** `Compliance.AuditLogEntry.changeset/2` requires `actor_user_id`, `action`, `outcome` via `validate_required`. Missing or empty values are refused with `{:error, :audit_write_invalid}` (lines 95–100 of audit_writer.ex).
   - **metadata size cap:** the Go gateway truncates metadata payloads above `MaxMetadataJSONBytes = 16 KiB` and substitutes `"{}"` (gateway/internal/audit/writer.go:18–48). On Core side `Jason.decode/1` falls back to `%{}` on parse failure (server.ex:170–173) — no exception thrown.
   - **separate transaction:** `repo().transaction(fn -> repo().insert!(entry) end)` runs in its own connection so a caller rollback cannot suppress the audit row (audit_writer.ex:82). This is the audit-immutability invariant.

   What is *not* defended at the Core layer: `target_type`, `target_id`, and metadata-map values are inserted as-is. There is no length cap on `target_type` / `target_id` (audit_log_entry.ex declares them as `:string` with no size constraint in the changeset), nor any character-class restriction. A PSK holder could pass a multi-megabyte `target_id` and force a JSONB row of pathological size. Practical risk is low because the only PSK holder is the gateway itself and the gateway constructs `target_id` from validated path parameters (room_id format `!opaque:server_name`, user_id format `@local:server_name`, JWT `sub`). But the changeset *should* enforce length bounds for defence-in-depth. See [LOW-1].

   Log-injection via control characters in `metadata`: `metadata` is a `:map` Ecto field stored as JSONB. PostgreSQL JSONB normalises whitespace and rejects invalid UTF-8 — no log-injection vector via terminal escape sequences in the DB itself. If an operator later `cat`s the JSONB to a terminal they could see embedded `\n` or ANSI escapes, but that is a downstream tooling concern, not an audit-pipeline integrity issue. See [INFO-2].

   One bonus issue spotted in passing: `actor_user_id` is **not** validated for the `@local:server_name` format. The gateway always sends a valid sub from the JWT, but defence-in-depth could regex-validate the userId shape. Same caveat as above — gateway is the only PSK holder. See [INFO-3].

---

## Findings

### MINOR-1 — Core `update_profile` does not re-validate `avatar_url` format

**Severity:** MINOR
**File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:461–490`
**OWASP:** A03:2021 Injection (XSS surface via stored URL); CWE-79

The Core `update_profile/2` handler accepts whatever `request.avatar_url` it receives (after normalising `""` to `nil`) and passes it to `Nebu.Profile.DB.upsert_profile/3`. There is no scheme allowlist, no `mxc://` prefix check, no length cap.

The HTTP gateway already enforces `mxc://` prefix and `isValidMxcURI` (path-traversal guard) before the gRPC call, so the production path is safe. But the Core RPC trusts the gateway entirely for URL safety. Story 7.16f adds a one-shot DB scrub migration, but does not add a forward-going CHECK constraint on `profiles.avatar_url`.

**Suggested mitigation (defence-in-depth):**
```elixir
defp valid_avatar_url?(nil), do: true
defp valid_avatar_url?(url) when is_binary(url) do
  byte_size(url) <= 2048 and
    (url == "" or String.starts_with?(url, "mxc://"))
end
```
Reject with `GRPC.Status.invalid_argument()` when invalid. Mirrors the gateway invariant. Optional and not blocking — the gateway is the sole producer in the current trust model.

**Status:** open — defence-in-depth, not exploitable in the current trust model.

---

### LOW-1 — `audit_log` changeset has no length bounds on `target_type` / `target_id` / `error_detail`

**Severity:** LOW
**File:** `core/apps/compliance/lib/compliance/audit_log_entry.ex:23–30`
**OWASP:** A04:2021 Insecure Design (resource exhaustion); CWE-770

`AuditLogEntry.changeset/2` calls `cast/3` and `validate_required/2` but does not call `validate_length/3` on any field. A misbehaving caller (PSK holder) could submit a multi-megabyte `target_id` or `error_detail` string. The `metadata` map at least has the gateway-side 16 KiB cap; the other free-text fields have none.

DB-side: the audit_log table columns are likely `text` (per migration 000018) which has no PostgreSQL row-size limit beyond TOAST (≈1 GiB).

Practical risk is bounded by gRPC's `MaxRecvMsgSize` (default 4 MiB on `grpc-elixir`'s server) and by the fact that the only PSK holder is the gateway itself, which constructs these fields from validated short identifiers. But the changeset should still cap them — adds zero attacker budget.

**Suggested mitigation:**
```elixir
def changeset(entry, attrs) do
  entry
  |> cast(attrs, @required_fields ++ @optional_fields)
  |> validate_required(@required_fields)
  |> validate_length(:actor_user_id, max: 255)
  |> validate_length(:target_type, max: 64)
  |> validate_length(:target_id, max: 255)
  |> validate_length(:error_detail, max: 4_000)
end
```

**Status:** open — non-blocking; pair with [LOW-2] in a small follow-up if Core ever exposes WriteAuditLog to non-gateway clients.

---

### LOW-2 — Action-allowlist enforcement only triggers when `action` is a non-empty binary

**Severity:** LOW
**File:** `core/apps/compliance/lib/compliance/audit_writer.ex:52`
**OWASP:** A05:2021 Security Misconfiguration; CWE-754

```elixir
if is_binary(action) and action != "" and action not in @known_actions do
```

If `action` is `nil`, `""`, or any non-binary (atom, integer), the condition is false and `do_log/7` is invoked. The Ecto changeset will then reject empty/nil via `validate_required`, which produces `{:error, :audit_write_invalid}` — so the row is not persisted. Functionally correct, but the failure mode is `:audit_write_invalid` rather than `:audit_unknown_action`, which collapses two distinct errors into one log path. A future caller passing `nil` (e.g. a test stub) would not see the "unknown action" warning that operators are watching for.

**Suggested mitigation:** treat `nil`, `""`, and non-binary as `:audit_unknown_action` too:
```elixir
if not (is_binary(action) and action != "" and action in @known_actions) do
  Logger.error("AuditWriter: rejected unknown audit action", ...)
  {:error, :audit_unknown_action}
else
  do_log(...)
end
```

**Status:** open — minor consolidation, not exploitable.

---

### INFO-1 — `actor_user_id` / `admin_user_id` is gateway-asserted with no Core-side cross-check

**Severity:** INFO
**Files:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:178–192` (`write_audit_log`), `:625` (`delete_user_keys`)

For both `WriteAuditLog` and `DeleteUserKeys`, Core trusts the gateway's `actor_user_id` / `admin_user_id` field as-is. This is consistent with the trust model documented in `docs/architecture/adr/008-node-registration.md` (PSK-authenticated gateway is trusted to assert caller identity), and consistent with `set_power_levels`/`get_messages` where Core reads identity from `Nebu.Grpc.Metadata.trusted_identity/1`.

Inconsistency note: `DeleteUserKeys` reads the admin from the **request body** (`req.admin_user_id`), while `set_power_levels` reads from **gRPC metadata** (`x-user-id` header set by `coregrpc.WithUserMetadata`). Both work because PSK bounds them to the gateway, but the convention drift is worth flagging — a future mTLS migration will need to consolidate on one path.

**No action required for this story.** Worth a short ADR clarifying the convention if the divergence persists.

---

### INFO-2 — JSONB metadata could carry control characters readable on terminal cat

**Severity:** INFO
**File:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:170–173`

`Jason.decode/1` accepts any valid JSON, including strings with embedded `\n`, `\r`, `` (ESC). The Ecto `:map` field stores the decoded map in JSONB. PostgreSQL's JSONB type does not strip control characters. If an operator later `psql` queries `audit_log` and pipes to a terminal, ANSI escape sequences in `metadata` could redraw the display. This is a downstream-tooling concern, not an audit integrity issue.

**No action required.** Most operations consume `audit_log` via structured tooling (Compliance UI, CSV export from Story 5.6 with proper escaping). Recommend documenting in `docs/runbooks/` that `audit_log` queries should use `jsonb_pretty` or read-only JSON tooling rather than raw `cat`.

---

### INFO-3 — `actor_user_id` regex format not validated

**Severity:** INFO
**File:** `core/apps/compliance/lib/compliance/audit_log_entry.ex`

The Matrix-spec format for `actor_user_id` is `@localpart:servername`. The changeset accepts any non-empty string. Defence-in-depth could regex-check the format (`@[a-z0-9._=/-]+:[a-z0-9.-]+`) — but this is a Matrix-spec concern, not a security-critical path. The gateway already validates this at JWT-issuance time.

**No action required.**

---

## Test Coverage

The story makes the following tests pass (verified by reading the diff and existing integration tests):

| Test | File | Check |
|---|---|---|
| `TestCoreGRPC_RejectsUnauthenticatedDial` | `gateway/test/integration/grpc_auth_test.go:104–125` | Unauth dial → `codes.Unauthenticated` (was `Unimplemented`) |
| `TestCoreGRPC_RejectsForgedToken` | `gateway/test/integration/grpc_auth_test.go:152–164` | Forged PSK → `codes.Unauthenticated` |
| `TestAuditForgery_NoRowInserted` | `gateway/test/integration/grpc_auth_test.go:261–280` | Forged PSK → 0 audit rows |
| `TestCoreGRPC_AcceptsValidToken` | `gateway/test/integration/grpc_auth_test.go:189–225` | Valid PSK → RPC reaches handler |
| Elixir `AuthInterceptor` ExUnit | `core/apps/event_dispatcher/test/...` | unchanged, must stay green |

The auth-rejection invariant is now actively enforced by the interceptor for all 4 newly-registered RPCs, where previously it was passively enforced by the routing 404. The behavioural difference matters: integration tests assert `codes.Unauthenticated`, which is only producible by the interceptor.

---

## Verdict

**APPROVED.** The fix is minimal, correct, and tightens the defence-in-depth posture:

- **Pre-fix:** auth never enforced for these 4 RPCs, but routing 404 prevented handler execution → no exploitable vulnerability, but a silent invariant violation.
- **Post-fix:** auth enforced for all RPCs in `core.CoreService` → integration tests can verify the auth invariant, and any future addition of an RPC will fail closed if registration is forgotten (the test harness will catch it).

No CRITICAL/HIGH findings. The MINOR/LOW observations (Core-side avatar URL re-validation, length bounds on audit fields, action-allowlist nil-handling) are defence-in-depth improvements consistent with the project's belt-and-suspenders convention. None are blocking under the SEC Gate 1 threshold (CRITICAL/HIGH).

A small follow-up story bundling the three defence-in-depth tightenings (MINOR-1 + LOW-1 + LOW-2) would be welcome but is not required to close 7.16d.

---

## Appendix A — Threat Model Walk

| Threat | Mitigation | Status |
|---|---|---|
| Unauthenticated WriteAuditLog → false audit rows | PSK interceptor (now enforced) + action allowlist + changeset required-fields | MITIGATED |
| Unauthenticated DeleteUserKeys → key destruction | PSK interceptor + gateway role-gate (`instance_admin`) + atomic conditional UPDATE TOCTOU close + failure-invariant audit | MITIGATED |
| Unauthenticated UpdateProfile → stored XSS via avatar_url | PSK interceptor + gateway `mxc://` prefix + path-traversal guard `isValidMxcURI` + Story 7.16f legacy-data scrub | MITIGATED (HTTP path); MINOR (Core has no re-check) |
| Unauthenticated GetPresence → information disclosure | PSK interceptor + gateway JWT middleware on `GET /presence/{userId}/status` | MITIGATED |
| Auth bypass via path manipulation | `grpc-elixir` routes only registered RPCs; interceptor wraps the whole service | MITIGATED |
| Timing attack on PSK comparison | `:crypto.mac` HMAC-SHA256 with random key per call | MITIGATED |
| Audit log injection (action newline / control chars) | `@known_actions` allowlist + Ecto changeset | MITIGATED |
| Audit log size DoS | Gateway-side 16 KiB metadata cap; Core-side no length cap on `target_id`/`target_type` | PARTIAL ([LOW-1]) |
| TOCTOU on key deletion | Atomic conditional UPDATE + `RETURNING` in `mark_in_progress` | MITIGATED |
| Audit suppression via caller rollback | AuditWriter uses separate `Repo.transaction/1` | MITIGATED |
| Path traversal in avatar mxc URI | `isValidMxcURI` checks both segments against `..`/`.`/`/`/`\\`/`\x00` | MITIGATED |

---

## Appendix B — Files & Lines Cited

- `core/apps/event_dispatcher/lib/pb/core_grpc.pb.ex:21–24` — the 4 added `rpc` registrations
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex:7` — `intercept(Nebu.Grpc.AuthInterceptor)`
- `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex:32–49` — `call/4` enforcement
- `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex:106–112` — constant-time `secure_compare/2`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:168–193` — `write_audit_log/2`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:461–490` — `update_profile/2`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:624–649` — `delete_user_keys/2`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:440–459` — `get_presence/2`
- `core/apps/compliance/lib/compliance/audit_writer.ex:25–61` — action allowlist + entry validation
- `core/apps/compliance/lib/compliance/user_deletion.ex:148–162` — atomic conditional UPDATE
- `core/apps/compliance/lib/compliance/user_deletion.ex:213–228` — failure-invariant audit
- `gateway/internal/matrix/profile.go:178–235` — mxc-prefix + path-traversal guard
- `gateway/internal/audit/writer.go:18–48` — 16 KiB metadata cap
- `gateway/internal/compliance/user_key_deletion.go:54–58` — `instance_admin` role gate
