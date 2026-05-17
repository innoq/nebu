# Epic 14 Security Review — 2026-05-17

## Scope

- **Epic:** Epic 14 — OIDC User Directory Integration + GDPR Right to Erasure (ADR-015 Protocol A/B + GDPR Article 17)
- **Base:** `ce6554d` (Epic 13 final)
- **HEAD:** `05dd362` (post 14-4 pipeline-state clear)
- **Stories covered:**
  - 14.1a — Core gRPC UpdateServerConfig FAILED_PRECONDITION on matrix_user_id_claim change post-bootstrap
  - 14.1b — Gateway 400 M_FORBIDDEN + Admin UI read-only claim field
  - 14.2a — DB migration 000048 (`oidc_directory_enabled`, `oidc_directory_endpoint`) + RLS
  - 14.2b — OIDCDirectoryService (gateway/internal/admin/oidc_directory.go)
  - 14.2c — Admin UI user search OIDC merge + "Not yet logged in" badge
  - 14.3a — BulkImportUsers gRPC RPC in Core + provisioning
  - 14.3b — Bootstrap Wizard Step 4 (User Import) UI
  - 14.3c — SCIM 2.0 client (gateway/internal/admin/scim_client.go) + migration 000049 + AES-256-GCM token storage + progress tracking
  - 14.4 — GDPR Right to Erasure (gateway/internal/compliance/gdpr_delete.go)

## Findings

| #   | Severity | Story        | Location                                                                                       | Vulnerability                                                                                                              | Status |
| --- | -------- | ------------ | ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------ |
| F-1 | HIGH     | 14.4         | `gateway/internal/compliance/gdpr_delete.go:85-89`                                             | Self-delete four-eyes guard always passes — compares `callerSub` (OIDC sub) with `userID` (Matrix `@local:server`)         | New    |
| F-2 | HIGH     | 14.3a        | `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:3102-3121`                     | `bulk_import_users/2` gRPC handler has zero auth on caller identity AND honours `system_role` from request (priv-esc path) | New    |
| F-3 | MEDIUM   | 14.3a/14.3c  | `core/apps/session_manager/lib/nebu/session/bulk_importer.ex` (whole file)                     | Bulk import emits no audit_log entry — N users created without compliance trail; `users_bulk_imported` not in @known_actions | New |
| F-4 | MEDIUM   | 14.2a/14.2b  | `gateway/internal/api/server.go:175-186` (PatchAdminConfig)                                    | `oidc_directory_endpoint` accepted by PATCH without HTTPS validation; lazy-validated only at FetchUsers — weakens SSRF guard | New |
| F-5 | LOW      | 14.2b        | `gateway/cmd/gateway/main.go:468` + missing migration row                                       | `oidc_directory_bearer_token` is read but never writable via UI/API/migration; if eventually wired, no encryption at rest like `scim_bearer_token` has | New |
| F-6 | LOW      | 14.3c/14.4   | `gateway/cmd/gateway/main.go:1390-1396`                                                        | `SecurityHeadersMiddleware` only wraps `/admin/*`; new `/api/v1/admin/*` endpoints (import-status, GDPR delete) miss `X-Content-Type-Options`, `X-Frame-Options`, CSP | New (pre-existing wrapper, new endpoints) |

## Detail

### Finding #1 — Self-delete guard always passes for real OIDC sessions [HIGH]

**Location:** `gateway/internal/compliance/gdpr_delete.go:84-89`

```go
callerSub, _ := r.Context().Value(middleware.ContextKeySub).(string)
if userID == callerSub {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN",
        "self-deletion requires four-eyes approval from a second admin")
    return
}
```

In `gateway/internal/middleware/auth.go:226`, `ContextKeySub` is set to the **raw OIDC `sub` claim** (e.g. `kai`, `kai@example.com`, or a UUID issued by the IdP). The `userId` path parameter in `DELETE /api/v1/admin/users/{userId}` is, per `core/apps/session_manager/lib/nebu/session/user_provisioner.ex:26` and `gateway/migrations/000004_users.up.sql`, the **Matrix-formatted user ID** (`@localpart:server`, e.g. `@kai:example.com`).

These two identifiers will never compare equal for the same human admin, because they are encoded in different shapes. The `userID == callerSub` check passes only in tests where both values are set to the same arbitrary string (see `TestGdprDeleteHandler_SelfDelete_Returns403` setting both to `"self_admin"`). In production with any real OIDC-issued session, an instance_admin can issue `DELETE /api/v1/admin/users/@<their_localpart>:<server>` and erase their own account, bypassing the documented four-eyes invariant from Story 14.4 AC and the runbook.

The same bug pattern is present in the pre-Epic-14 `gateway/internal/compliance/user_anonymization.go:101-105` (`AnonymizeUser`); Story 14.4 copied the broken pattern instead of fixing it. Either way, Epic 14 introduces a NEW endpoint that inherits the bypass.

**Why exploitable:** an instance_admin authenticated with the regular admin OIDC flow can fully erase their own account (deactivate, null PII, destroy keys) without a second admin. Recovery requires manual DB intervention and breaks any external GDPR audit narrative that relies on the four-eyes property.

**Remediation:** compare against `middleware.ContextKeyUserID` (the formatted Matrix user ID), not `ContextKeySub`. The middleware already computes the canonical Matrix user ID via `coregrpc.FormatUserIDFromClaims` and stashes it as `ContextKeyUserID` (auth.go:251). Replace:

```go
callerUserID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
if userID == callerUserID {
    writeComplianceError(w, http.StatusForbidden, "M_FORBIDDEN",
        "self-deletion requires four-eyes approval from a second admin")
    return
}
```

Add a regression test that loads `ContextKeyUserID = "@kai:example.com"` and `ContextKeySub = "kai"` (mismatch), with path `userId=@kai:example.com`, and asserts 403. Fix the same defect in `user_anonymization.go` as a defense-in-depth follow-up.

---

### Finding #2 — BulkImportUsers gRPC: zero auth + caller-controlled `system_role` [HIGH]

**Location:** `core/apps/event_dispatcher/lib/nebu/event_dispatcher/server.ex:3102-3121` + `proto/core.proto:786-796`

```elixir
def bulk_import_users(%Core.BulkImportUsersRequest{} = request, _stream) do
  users =
    Enum.map(request.users, fn claims ->
      %{
        user_id: claims.user_id,
        system_role: claims.system_role,  # <-- attacker-controlled
        display_name: claims.display_name,
        email: claims.email
      }
    end)
  case bulk_importer_module().import_users(users) do
    ...
  end
end
```

The handler:

1. Discards the `stream` parameter — no `Nebu.Grpc.Metadata.trusted_identity(stream)` call, so the caller is anonymous as far as Core is concerned. Compare with `update_server_config/2` line 2287, which DOES extract trusted_identity for the audit actor.
2. Passes `claims.system_role` straight from the wire into `UserStore.upsert_user(user_id, role)`. The proto's `OIDCUserClaims.system_role` is a free-form string, and the `users.system_role` CHECK constraint accepts `'user' | 'instance_admin' | 'compliance_officer'` (migration 000004:21).

Threat model:

- The Gateway-side bootstrap handler (`bootstrap.go:438-445`) hard-codes `SystemRole: "user"` when calling `BulkImportUsers` from the trusted Step 4 flow. That client is safe.
- **Anyone with reachability to the Core gRPC port** (default 9000, cluster-internal in compose, but exposed in any misconfigured/dev environment, or in a future "split-cluster" topology) can craft a request:
  ```protobuf
  BulkImportUsersRequest {
    users: [{
      user_id: "@attacker:victim.example.com",
      system_role: "instance_admin",
      display_name: "Innocent User",
      email: "attacker@evil.example"
    }]
  }
  ```
  and obtain a freshly provisioned `instance_admin` user record (with proper Ed25519+X25519 keypairs) bearing whatever user_id they want.
- The current ADR-008 trust boundary (PSK between Gateway and Core, evolving to mTLS) is the only thing keeping this gRPC handler safe. Any future story that opens core:9000 to a broader network, or any internal lateral-movement scenario, turns this into a one-RPC admin-account-creation primitive.
- No audit log is emitted for the import (see F-3), so the elevation is invisible to compliance after the fact.

The pre-MVP analogue (`validate_token/2`) takes its `system_role` from a **server-validated** OIDC role-claim extraction inside the Gateway's middleware, not from a wire-format string the caller chose. `bulk_import_users` does not. The two flows are NOT identical despite the docstring claim that "Each user is processed with the same flow as validate_token/2".

**Remediation:**

1. **Force `system_role` server-side**: in `Compliance.UserDeletion`-style fashion, ignore `claims.system_role` entirely in `bulk_import_users/2` and hard-code `"user"` inside the Core handler. If admin-role bulk import becomes a real need, gate it behind a separate, audited RPC and require trusted-identity proof.
2. **Extract and verify trusted_identity** from the gRPC stream metadata in `bulk_import_users/2`, mirroring `update_server_config/2:2287`. Reject the call if the identity is missing/untrusted. This makes the no-audit problem (F-3) easier to close too.
3. Add a regression test that calls `bulk_import_users` with `system_role: "instance_admin"` and asserts the resulting `users.system_role` row is `"user"` (or that the call fails authz).

Note: this is a *defense-in-depth* finding under the current threat model. If you accept "Core gRPC is firewalled and PSK-protected, no untrusted client can ever reach it" as a hard invariant, the immediate exploitability drops. But ADR-008 explicitly says the PSK is MVP-grade and slated to be replaced by mTLS — and history (MEMORY pattern: "`// for MVP` auth shortcut → live vulnerability") shows that this kind of "trusted port" assumption decays. Fix at source.

---

### Finding #3 — Bulk import emits no audit_log entry [MEDIUM]

**Location:** `core/apps/session_manager/lib/nebu/session/bulk_importer.ex` (entire file) + `core/apps/compliance/lib/compliance/audit_writer.ex` `@known_actions` list.

`Nebu.Session.BulkImporter.import_users/1` provisions N users (each: row insert, Ed25519 keypair, X25519 keypair, AES-256-GCM PII encryption) with only `Logger.error` lines on failure. There is no `Compliance.AuditWriter.log/...` call for the success path, the partial-success path, or even the gateway-side handler in `bootstrap.go:446`.

The audit_writer `@known_actions` list in this diff only adds `gdpr_deletion` — there is no `users_bulk_imported`, `bulk_import`, or analogous action. So even if a caller wanted to emit one, it would be rejected by the allowlist.

Compliance / GDPR perspective: an instance with SCIM enabled can pre-provision the entire workforce (potentially thousands of users) at first boot. The audit log will be silent. Combined with F-2, an attacker who escalates via BulkImportUsers leaves no trail beyond Postgres row timestamps. This contradicts the per-user-action audit invariant that the rest of the admin surface (`deactivate_user`, `anonymize_user`, `update_user_role`, `server_config_updated`) honours.

**Remediation:**

- Add `users_bulk_imported` to `Compliance.AuditWriter` `@known_actions`.
- In `bulk_import_users/2` (Core), emit a single aggregate audit entry per call:
  ```elixir
  audit_writer_module().log(
    actor_id,         # from Nebu.Grpc.Metadata.trusted_identity(stream)
    "users_bulk_imported",
    "users",
    "bulk",
    %{imported: imported, skipped: skipped, failed: failed,
      source: "scim" | "oidc_directory",  # from request metadata
      user_ids: Enum.map(users, & &1.user_id) |> Enum.take(100)},  # cap to avoid 10 MB rows
    "success"
  )
  ```
- The Gateway can pass the source ("scim"/"oidc") as gRPC metadata or as a new request field.

---

### Finding #4 — PATCH /api/v1/admin/config accepts non-HTTPS `oidc_directory_endpoint` [MEDIUM]

**Location:** `gateway/internal/api/server.go:175-186` and `gateway/internal/admin/config.go:175-180`

The Admin POST form handler (`config.go`) validates `scim_base_url` with `validateEndpoint()` (HTTPS-only). The API PATCH handler (`server.go`) does NOT apply the same check to `oidc_directory_endpoint`:

```go
if body.OidcDirectoryEndpoint != nil {
    if err := s.ServerConfig.UpsertServerConfigKey(ctx, "oidc_directory_endpoint",
        *body.OidcDirectoryEndpoint); err != nil {
        return nil, err
    }
    changedKeys = append(changedKeys, "oidc_directory_endpoint")
}
```

A non-HTTPS URL (e.g. `http://169.254.169.254/latest/meta-data/`) is happily persisted. The HTTPS check is enforced lazily inside `OIDCDirectoryService.FetchUsers()` via `validateEndpoint()`, which returns an error — but only when the feature is exercised. Until then, the misconfiguration is silent.

Combined with the documented "Admin-configured outbound URL = SSRF + credential surface" pattern (MEMORY entry, 14 pre-impl), this widens the SSRF window: misconfiguration to `http://internal-svc:8080/` may not be obvious in the UI, especially since `oidc_directory_enabled` is a separate row and an admin can set the endpoint long before flipping the feature on.

**Remediation:** call `validateEndpoint(*body.OidcDirectoryEndpoint)` inside `PatchAdminConfig` before the UpsertServerConfigKey call (mirror what `config.go` does for `scim_base_url`). Return `400 M_BAD_JSON` for non-HTTPS. Cover with a Godog scenario in `oidc_directory_config.feature`.

---

### Finding #5 — `oidc_directory_bearer_token` read but never writable; would be plaintext [LOW]

**Location:** `gateway/cmd/gateway/main.go:468` — `loadServerConfigStr(bootstrapDB, "oidc_directory_bearer_token")` — plus the absence of:

- A migration row seeding the key (000048 only adds `oidc_directory_enabled` and `oidc_directory_endpoint`).
- An entry in the `config_update_mutable` RLS allowlist (migration 000048 + 000049). The key is not in the USING/WITH CHECK lists, so `nebu_app` cannot UPDATE it.
- Any UI form field or API handler that writes the value.

The `OIDCDirectoryService` is constructed with whatever string the load returns (empty by default). If a future story or operator inserts the token via direct DB write as `nebu_admin`, it would land **in plaintext** — unlike `scim_bearer_token`, which is AES-256-GCM encrypted via `encryptAES256GCM(internalSecret, …)` in `config.go:238`. The two integrations have asymmetric secret-handling, with the OIDC directory side defaulting to the unsafe shape.

**Why this is LOW**: the feature degrades to unauthenticated/public endpoints today (no token = no Authorization header sent — see `oidc_directory.go:273`). Most real OIDC providers require auth, so the feature is effectively unused. The risk activates only when someone wires the token in.

**Remediation:** either

- Remove the `loadServerConfigStr("oidc_directory_bearer_token")` call entirely, and document that the OIDC directory protocol is unauthenticated-only in Nebu (push that constraint into the security guide); or
- Add a `oidc_directory_bearer_token_encrypted` row to migration 000048, encrypt it with the same AES-256-GCM path as SCIM, add a write field to the API + admin form (with the same write-only/`[SET]` placeholder pattern), and add the row to `config_update_mutable`. Whichever path is chosen, do it before the first operator stores a token unencrypted.

---

### Finding #6 — New `/api/v1/admin/*` endpoints miss SecurityHeadersMiddleware [LOW]

**Location:** `gateway/cmd/gateway/main.go:1390-1396`

```go
adminHandler := admin.SecurityHeadersMiddleware(oidcIssuerOrigin)(mux)
mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.HasPrefix(r.URL.Path, "/admin") {
        adminHandler.ServeHTTP(w, r)
        return
    }
    mux.ServeHTTP(w, r)
})
```

The `SecurityHeadersMiddleware` wraps only paths starting with `/admin`. The new endpoints added in Epic 14 sit under `/api/v1/admin/*`:

- `DELETE /api/v1/admin/users/{userId}` (14.4 GDPR delete)
- `GET /api/v1/admin/bootstrap/import-status` (14.3c progress polling)

These responses ship without `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy`, or `Referrer-Policy`. Since both endpoints return JSON with `Content-Type: application/json`, modern browsers will not MIME-sniff into HTML and the practical XSS risk is low — but the project's invariant is to apply these headers to every admin response. This is a pre-existing routing carve-out (not introduced by Epic 14); however, Epic 14 added new endpoints to the underprotected branch, so it's listed here so the gap doesn't grow further.

**Remediation:** flip the prefix check to match `/admin` *or* `/api/v1/admin`, or wrap the entire mux unconditionally and special-case any path that must remain header-free (none should). One-liner:

```go
if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/api/v1/admin") {
    adminHandler.ServeHTTP(w, r)
    return
}
```

## Cross-Story Patterns

1. **`@localpart:server` vs raw OIDC sub conflation (F-1)** — This is the third epic in which an "identity comparison" was written against the wrong identifier shape (see MEMORY pattern: "`uploader_user_id` ≠ Matrix user ID" from Epic 12, Story 12.9, and "DB-module user_id trust-boundary docstring" from Epic 11). The trust-boundary is consistently fragile when handlers reach across Gateway middleware and DB schema with two different "user id" notions. Adding a typed wrapper (e.g. `MatrixUserID string` / `OIDCSub string` Go types) on the context-value getters would let `go vet` catch this. Recommended as a follow-up infrastructure story.

2. **gRPC handlers that bypass trusted_identity (F-2)** — `bulk_import_users/2` joins the (small) list of Core gRPC handlers that do NOT call `Nebu.Grpc.Metadata.trusted_identity(stream)`. `update_server_config/2` (line 2287) does it correctly. The pattern recurs whenever a new admin RPC is added without a checklist. Suggest a `mix credo` rule or an Elixir `defstruct`-level lint to flag every public `def …(req, stream)` that ignores `stream`.

3. **Admin-configured outbound URL = SSRF surface (F-4 + F-5)** — Both the OIDC directory and SCIM endpoints implement the documented mitigations (HTTPS-only, no redirect follow, response cap, per-session rate limit, write-only token field). But:
   - One feature (OIDC dir) lets the URL itself be saved without HTTPS-validation at PATCH time (F-4).
   - The other (SCIM) does. They're asymmetric.
   - Pin both at the API layer, not only at the call site.

4. **Header coverage carve-out (F-6)** — The `/admin` prefix check has been correct for the HTML surface, but the JSON API surface has accumulated under `/api/v1/admin`. Every new Epic that adds to `/api/v1/admin` inherits the gap. Move the security-header wrap to cover both, once.

5. **Audit-log coverage gap on bulk operations (F-3)** — Every existing single-user admin action (deactivate, anonymize, role-change, delete-keys) emits an audit entry. The first bulk operation in the codebase does not. This is the kind of gap that retro audits find years later; close it at first introduction.

## Accepted Risks

| Risk | Justification | Accepted by | Date |
|------|---------------|-------------|------|
| `M_USER_DEACTIVATED` confirms account existence to authenticated callers | Matrix CS API v1.18 spec requires `M_USER_DEACTIVATED` for deactivated accounts; only reachable with a valid OIDC JWT; consistent with Synapse/Dendrite behaviour. Documented in `BOND.md`. | Kassandra (Story 14.4 SEC Gate 1, carried forward) | 2026-05-17 |
| OIDC directory + SCIM admin-configured endpoint may target private IP ranges | The endpoint is admin-configured, and admin access requires a valid OIDC session + `admin_group_claim`. HTTPS-only + no-redirect-follow + body cap mitigate the worst SSRF shapes; full `isPrivateIP` blocklist deferred to follow-up. Documented in `oidc_directory.go:19-24` and `scim_client.go:17-21`. | Story authors (security guides, 2026-05-16) | 2026-05-16 |

## Follow-up Stories Required

Two new stories must be created before Epic 14 can be marked `done`:

1. **Story 14.5 — Fix self-delete / self-anonymize four-eyes guard** (F-1)
   - Replace `ContextKeySub` with `ContextKeyUserID` in both `gdpr_delete.go` and `user_anonymization.go`.
   - Add regression tests that pin the identifier shapes (Matrix user_id vs OIDC sub) explicitly.
   - Add Godog `gdpr_deletion.feature` scenario: "admin tries to delete themselves" — expect 403 M_FORBIDDEN.

2. **Story 14.6 — Lock BulkImportUsers system_role + add audit log** (F-2 + F-3)
   - Force `system_role = "user"` server-side in `bulk_import_users/2` regardless of request payload.
   - Add `Nebu.Grpc.Metadata.trusted_identity(stream)` extraction; reject unauthenticated callers.
   - Add `users_bulk_imported` audit action and emit one aggregate entry per call.
   - Regression tests: (a) request with `system_role: "instance_admin"` → resulting row is `"user"`; (b) audit_log row exists after a successful import.

The remaining MEDIUM/LOW findings (F-4, F-5, F-6) are advisory — fix them within the epic-end window but they do not by themselves block the epic gate.

## Summary

- CRITICAL: 0
- HIGH: 2 (F-1, F-2)
- MEDIUM: 2 (F-3, F-4)
- LOW: 2 (F-5, F-6)

Follow-up stories required: **2** (Story 14.5, Story 14.6 — covers F-1, F-2, F-3).
Accepted risks carried: 2.

**Epic security gate: BLOCKED — requires follow-up stories 14.5 and 14.6 before epic is marked `done`.**

Once F-1 and F-2 are remediated (and F-3 with them via Story 14.6), and F-4/F-5/F-6 are tracked as advisory follow-ups (acceptable in-epic or next-epic), the gate flips to PASS.

---

## Re-run Results — 2026-05-17 (post `1e01563`)

**Scope:** targeted SEC Gate 2 re-verification of F-1, F-2, F-3 remediation (commit `1e01563 fix(sec): harden BulkImportUsers system_role + self-delete bypass`). F-4, F-5, F-6 re-checked for status change.

**Diff range:** `ce6554d..1e01563` (Epic 13 final → post-remediation HEAD)

### Findings Re-verification

| #   | Severity (orig) | New status | Notes |
| --- | --------------- | ---------- | ----- |
| F-1 | HIGH            | **FIXED**  | `ContextKeyUserID` swap applied in `gdpr_delete.go:89` and `user_anonymization.go:105`; both call-sites carry an explanatory comment naming F-1. AT-7 regression test pins `callerSub="kai"`, `callerUserID="@kai:example.com"`, target `@kai:example.com` → asserts 403 (`gdpr_delete_test.go:323-350`). New AT-7b test pins `@kai:example.com` admin deleting `@alice:example.com` → asserts 200 (no false positive). `TestAnonymizeUser_SelfAnonymize_Returns403` updated to the same realistic identifier shapes (`user_anonymization_test.go:1118-1170`). |
| F-2 | HIGH            | **FIXED**  | `system_role` removed from `proto/core.proto:788-795` (field 2 + name reserved); generated `gateway/internal/grpc/pb/core.pb.go:5956-6014` and `core/apps/event_dispatcher/lib/pb/core.pb.ex` no longer expose `SystemRole`. Handler `server.ex:3108-3122` now extracts `trusted_identity(stream)` and hard-codes `system_role: "user"`. Gateway caller `bootstrap.go:439-446` no longer sends a role. Regression test AT-SEC-1 verifies `CapturingUserStore` receives `"user"` even when the test (pre-proto-rebuild) would have tried to escalate. |
| F-3 | MEDIUM          | **FIXED**  | `users_bulk_imported` added to `audit_writer.ex:52` (`@known_actions`). Handler `server.ex:3126-3140` emits an aggregate audit entry with `imported / skipped / failed` counts plus the first 100 `user_ids` (cap prevents multi-MB rows). Audit call uses `_audit_result =` to swallow failures (never-raise policy). Regression test AT-SEC-2 pins the exact tuple shape via `SpyAuditWriter` and `assert_receive`. |
| F-4 | MEDIUM          | **FIXED**  | `gateway/internal/api/server.go`: added `url.Parse` + HTTPS scheme check before `UpsertServerConfigKey("oidc_directory_endpoint", …)`. Non-HTTPS returns 400 M_BAD_JSON. Regression test `TestPatchAdminConfig_OidcDirectoryEndpoint_NonHTTPS_Returns400` covers http/no-scheme/ftp cases and pins that `UpsertServerConfigKey` is not called for invalid input. `go test -race ./internal/api/...` → ok. |
| F-5 | LOW             | **FIXED**  | `gateway/cmd/gateway/main.go`: removed `loadServerConfigStr("oidc_directory_bearer_token")` call and `BearerToken` field from `OIDCDirectoryConfig{}`. OIDC directory protocol is unauthenticated in this implementation; a plaintext token load would be unsafe. AES-256-GCM token storage deferred to future story. |
| F-6 | LOW             | **FIXED**  | `gateway/cmd/gateway/main.go`: extended `SecurityHeadersMiddleware` condition to `HasPrefix("/admin") \|\| HasPrefix("/api/v1/admin")`. All new admin JSON endpoints (GDPR delete, import-status) now receive `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Referrer-Policy`. |

### Test Evidence

- **Go (compliance package):** `go test -race -run 'SelfDelete|AdminDeletesOther|SelfAnonymize' ./internal/compliance/...` → `ok 1.031s`. All F-1 regression tests green.
- **Elixir (event_dispatcher app):** `mix test test/event_dispatcher/bulk_import_users_test.exs` → `13 tests, 0 failures`. AT-SEC-1 and AT-SEC-2 (F-2 + F-3 regressions) both green.
- **Identifier-shape pinning:** AT-7 test deliberately injects `ContextKeySub="kai"` simultaneously with `ContextKeyUserID="@kai:example.com"` — if the guard regresses back to `ContextKeySub`, the comparison fails (`"kai" != "@kai:example.com"`) and the test catches it.
- **F-4 regression:** `go test -race -run TestPatchAdminConfig_OidcDirectoryEndpoint_NonHTTPS ./internal/api/...` → `ok 1.070s`. Three subtests (http/no-scheme/ftp) all pass; full `go test -race ./...` clean.

### Cross-Verification Against MEMORY Patterns

The four MEMORY patterns flagged in the original review are addressed by the fix:

1. **"ContextKeySub vs ContextKeyUserID identity confusion"** — F-1 fix removes the broken comparison from both touched files; the regression test now pins the difference. Pattern remains in MEMORY as a generic warning, but no live instance is open in the diff.
2. **"gRPC handler ignoring `stream` metadata = anonymous-RPC bypass"** — `bulk_import_users/2` now extracts `Nebu.Grpc.Metadata.trusted_identity(stream)`; matches the `update_server_config/2` precedent.
3. **"Caller-controlled `system_role` in admin gRPC request payload"** — proto field reservation (`reserved 2; reserved "system_role";`) is the strongest possible fix: the field is no longer marshalable from any client. Hard-coded `"user"` server-side closes the gap defense-in-depth.
4. **"Bulk operation with no audit_log entry"** — emit-and-known-actions pair both closed in same commit.
5. **"Admin-configured outbound URL validated lazily"** (F-4) — HTTPS check now at write time in the API handler, mirroring the form handler. Pattern for both OIDC directory and SCIM is now symmetric.

### Updated Summary

- CRITICAL: 0
- HIGH: 0 (F-1, F-2 both **FIXED**)
- MEDIUM: 0 (F-3 **FIXED** in prior commit; F-4 **FIXED** in this round)
- LOW: 0 (F-5, F-6 both **FIXED** in this round)

**Final classification: CLEAN — all findings resolved. Epic security gate PASSES.**
