---
status: ready-for-dev
epic: 14
story: 4
security_review: required
matrix: true
ui: false
---

# Story 14.4: GDPR Right to Erasure — End-to-End Verification

**Status:** ready-for-dev

## Story

As a compliance officer,
I want to verify that deleting a user in Nebu correctly erases all PII and key material end-to-end,
So that GDPR Article 17 (Right to Erasure) can be attested with evidence.

**Size:** M
**security_review:** required (PII erasure correctness, audit trail integrity, deactivation enforcement, M_USER_DEACTIVATED on re-login)

---

## Acceptance Criteria

**AC1 — Full GDPR deletion flow verified in Godog:**
Given a user exists with: display name, avatar URL, Ed25519 key, X25519 key, encrypted PII, session records, compliance access requests, and sent messages,
When `DELETE /api/v1/admin/users/{userId}` is called (orchestrating the GDPR deletion flow),
Then the following are verified in a Godog scenario:
  - `users.display_name` (i.e. `profiles.displayname`) → anonymized (`"Deleted User"`)
  - `users.avatar_url` (i.e. `profiles.avatar_url`) → NULL
  - `user_keys.ed25519_public_key` (private_key in `user_keys` where key_type='signing') → NULL / deleted
  - `user_keys.x25519_public_key` (private_key in `user_keys` where key_type='encryption') → NULL / deleted
  - `operational_pii` (users table: `anonymized_at` set, encrypted PII fields nulled or set per existing anonymization) → user is anonymized
  - `sessions` → all sessions for the user invalidated and deleted
  - `events` content NOT modified (messages remain for room history — by design, per ADR-007)
  - `audit_log` contains a `gdpr_deletion` event for the user

**AC2 — Matrix profile returns anonymized data after deletion:**
Given the deleted user's Matrix User ID (`@alice:nebu.example`),
When `GET /_matrix/client/v3/profile/@alice:nebu.example` is called,
Then `displayname` returns `"Deleted User"` and `avatar_url` is absent from the response

**AC3 — OIDC login blocked with M_USER_DEACTIVATED:**
Given the deletion has been performed (user is deactivated: `is_active=false`),
When the deleted user attempts to log in via OIDC (Matrix SSO login flow → `GET /_matrix/client/v3/login/sso/redirect/oidc` → callback → POST implicit token),
Then login fails with `403 M_USER_DEACTIVATED`

**AC4 — Godog integration test passes:**
Given a Godog scenario `gateway/features/gdpr_deletion.feature`,
When `make test-integration` runs,
Then all GDPR deletion scenarios pass (including the room-history-preservation assertion)

**AC5 — Compliance runbook created:**
Given `docs/compliance/gdpr-deletion-runbook.md`,
When it is read,
Then it documents: deletion procedure, evidence items for GDPR audit, known limitations (messages in room history are not deleted — per Matrix spec design)

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **AT-1 — Godog: Full erasure assertions** — `gateway/features/gdpr_deletion.feature`
   - Given: user `alice` exists with profile, Ed25519/X25519 keys, sessions, messages in a room
   - When: `DELETE /api/v1/admin/users/{userId}` is called
   - Then: `profiles.displayname = 'Deleted User'`, `profiles.avatar_url = NULL`, `user_keys` private_key NULL for both key types, `users.anonymized_at` set, no active sessions, `audit_log` has `gdpr_deletion` row
   - And: `events` table rows for alice's messages are NOT deleted

2. **AT-2 — Matrix profile shows anonymized data** — `gateway/features/gdpr_deletion.feature`
   - Given: user deleted (AT-1 done)
   - When: `GET /_matrix/client/v3/profile/@alice:nebu.example`
   - Then: `{"displayname": "Deleted User"}` — `avatar_url` key is absent

3. **AT-3 — OIDC login blocked with M_USER_DEACTIVATED** — `gateway/features/gdpr_deletion.feature`
   - Given: user deleted (is_active=false)
   - When: Matrix SSO callback attempts to provision/validate the deleted user via `ValidateToken` gRPC
   - Then: response is `403` with errcode `M_USER_DEACTIVATED`

4. **AT-4 — Unit: GdprDeleteHandler calls all required steps** — `gateway/internal/compliance/gdpr_delete_test.go`
   - Given: mock CoreClient, mock DB
   - When: `DELETE /api/v1/admin/users/{userId}` is handled
   - Then: `DeactivateUser` gRPC called → sessions deleted → `DeleteUserKeys` gRPC called → anonymize DB txn committed → audit `gdpr_deletion` emitted

5. **AT-5 — Unit: non-admin returns 403** — `gateway/internal/compliance/gdpr_delete_test.go`
   - Given: caller system_role = "user"
   - When: handler called
   - Then: 403 M_FORBIDDEN

6. **AT-6 — Unit: unknown user returns 404** — `gateway/internal/compliance/gdpr_delete_test.go`
   - Given: DeactivateUser gRPC returns NOT_FOUND
   - When: handler called
   - Then: 404 M_NOT_FOUND

---

## Dev Notes

### What This Story Implements

This story creates a **single orchestrating endpoint** `DELETE /api/v1/admin/users/{userId}` that chains the **already-implemented** GDPR building blocks in the correct order:

1. **Deactivate user** (existing: `DeactivateUser` gRPC → `core/.../server.ex` → `set_is_active(false)` + session invalidation)
2. **Delete user keys** (existing: `DeleteUserKeys` gRPC → `Compliance.UserDeletion.delete_user_keys/3`)
3. **Anonymize user** (existing: `POST /api/v1/admin/users/{userId}/anonymize` → `user_anonymization.go` → profile + avatar + anonymized_at)
4. **Emit `gdpr_deletion` audit event** (NEW: `gdpr_deletion` must be added to `@known_actions` in `Compliance.AuditWriter`)

**Important:** `DELETE /api/v1/admin/users/{userId}` does NOT exist yet. The individual steps exist as separate endpoints. This story adds the orchestrating endpoint and the Godog feature file proving end-to-end correctness.

**Also NEW:** The Matrix login handler currently does NOT return `M_USER_DEACTIVATED` — it returns `500 M_UNKNOWN` when `ValidateToken` raises `PERMISSION_DENIED`. The login handler must map `codes.PermissionDenied` with the "deactivated" message to `403 M_USER_DEACTIVATED`.

### Architecture — Existing Building Blocks

#### Key Deletion (Core Elixir — existing)
- `gateway/internal/compliance/user_key_deletion.go` — `DeleteUserKeys` handler
- `DELETE /api/v1/admin/users/{userId}/keys` — sends gRPC `DeleteUserKeysRequest` to Elixir Core
- Core: `Compliance.UserDeletion.delete_user_keys/3` — atomically nulls `user_keys.private_key`
- Core: emits `user_keys_deleted` audit event

#### Anonymization (Go Gateway — existing)
- `gateway/internal/compliance/user_anonymization.go` — `AnonymizeUser` handler
- `POST /api/v1/admin/users/{userId}/anonymize` — pure Go DB operation
- Sets `profiles.displayname = 'Deleted User'`, `profiles.avatar_url = NULL`, `users.anonymized_at = <now_ms>`
- Emits `user_anonymized` audit event

#### Deactivation (Core Elixir via gRPC — existing)
- `gateway/internal/grpc/client.go` — `DeactivateUser` method
- Core: `server.ex deactivate_user/2` → sets `is_active=false`, calls `destroy_session/1`
- Emits `user_deactivated` audit event
- Sessions: invalidated in ETS + deleted from DB

#### OIDC Login — Deactivated User Mapping (Gateway — **partially missing**)
- `gateway/internal/matrix/login.go` — `PostLogin` handler
- Currently: `ValidateToken` gRPC error → `500 M_UNKNOWN` for ALL errors
- **Must fix:** if gRPC status code is `PERMISSION_DENIED` and message contains "deactivated" → return `403 M_USER_DEACTIVATED`
- Matrix spec v1.18: `M_USER_DEACTIVATED` is a valid error code for deactivated users at login

#### Audit Writer (Core — **needs update**)
- `core/apps/compliance/lib/compliance/audit_writer.ex`
- `@known_actions` allowlist does NOT contain `gdpr_deletion` yet
- **Must add** `gdpr_deletion` to `@known_actions` (before the Go handler can emit it)

### Files to CREATE

1. `gateway/internal/compliance/gdpr_delete.go` — `GdprDeleteHandler` struct + `DeleteUser` method
2. `gateway/internal/compliance/gdpr_delete_test.go` — unit tests AT-4..AT-6
3. `gateway/features/gdpr_deletion.feature` — Godog scenarios AT-1..AT-3
4. `gateway/test/integration/gdpr_deletion_steps_test.go` — Godog step definitions
5. `docs/compliance/gdpr-deletion-runbook.md` — compliance runbook (AC5)

### Files to UPDATE

6. `gateway/cmd/gateway/main.go` — register `DELETE /api/v1/admin/users/{userId}` route (wrapped in `complianceRL` + `jwtWithStatusCheck`)
7. `gateway/internal/matrix/login.go` — map `codes.PermissionDenied` + "deactivated" message → `403 M_USER_DEACTIVATED`
8. `core/apps/compliance/lib/compliance/audit_writer.ex` — add `gdpr_deletion` to `@known_actions`

### GdprDeleteHandler Implementation Pattern

```go
// gateway/internal/compliance/gdpr_delete.go
package compliance

type GdprDeleteHandler struct {
    DB         *sql.DB
    CoreClient pb.CoreServiceClient
    StoragePath string
    FileRemover FileRemover
}

// DeleteUser handles DELETE /api/v1/admin/users/{userId}
// Orchestrates: DeactivateUser → DeleteUserKeys → AnonymizeUser → gdpr_deletion audit
// All steps must succeed; partial failure is logged but the response is 500 if
// the DeactivateUser or DeleteUserKeys calls fail.
func (h *GdprDeleteHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
    // 1. Role gate: must be instance_admin
    // 2. userId path param validation
    // 3. Self-delete guard (same as anonymize: four-eyes required)
    // 4. gRPC: DeactivateUser (sets is_active=false, invalidates sessions)
    //    → 404 on NOT_FOUND, 409 on ALREADY_EXISTS/already deactivated
    // 5. gRPC: DeleteUserKeys (nulls private keys)
    //    → log warn on error, continue (keys may already be deleted)
    // 6. Call anonymization logic (reuse from user_anonymization.go or call directly)
    //    → sets profiles.displayname='Deleted User', avatar_url=NULL, users.anonymized_at
    // 7. Emit audit: gdpr_deletion (never-raise, 500ms timeout)
    // 8. 200 {"user_id": userId, "status": "gdpr_deleted"}
}
```

**Error handling:**
- Step 4 (DeactivateUser) fails → 404 M_NOT_FOUND or 500 M_UNKNOWN
- Step 5 (DeleteUserKeys) fails → log warn, continue (best-effort key deletion; keys may already be deleted if `deletion_status='keys_deleted'`)
- Step 6 (Anonymize) fails → 500 M_UNKNOWN (cannot silently skip PII erasure)
- Step 7 (Audit) fails → never-raise, log warn, return 200 (audit failure must not block the GDPR deletion response)

### Login Handler Fix — M_USER_DEACTIVATED

In `gateway/internal/matrix/login.go`, the `PostLogin` function currently does:
```go
_, err = h.coreClient.ValidateToken(grpcCtx, &pb.ValidateTokenRequest{...})
if err != nil {
    slog.Error("ValidateToken gRPC failed", "err", err, "user_id", userID)
    writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
    return
}
```

**Fix:** Check gRPC status code before the generic 500:
```go
import (
    "google.golang.org/grpc/codes"
    grpcstatus "google.golang.org/grpc/status"
)

_, err = h.coreClient.ValidateToken(grpcCtx, &pb.ValidateTokenRequest{...})
if err != nil {
    if st, ok := grpcstatus.FromError(err); ok && st.Code() == codes.PermissionDenied {
        // Core raises PERMISSION_DENIED with message "user account is deactivated"
        writeMatrixError(w, http.StatusForbidden, "M_USER_DEACTIVATED", "Account has been deactivated")
        return
    }
    slog.Error("ValidateToken gRPC failed", "err", err, "user_id", userID)
    writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
    return
}
```

**Note:** Matrix spec v1.18 says servers MAY use `M_FORBIDDEN` instead of `M_USER_DEACTIVATED` for privacy, but the story AC3 explicitly requires `M_USER_DEACTIVATED`.

### Route Registration in main.go

```go
// After the existing compliance routes:
mux.Handle("DELETE /api/v1/admin/users/{userId}",
    complianceRL(jwtWithStatusCheck(http.HandlerFunc(gdprDeleteHandler.DeleteUser))))
```

Wire `gdprDeleteHandler` similarly to `anonymizationHandler`:
```go
gdprDeleteHandler := &compliance.GdprDeleteHandler{
    DB:          complianceDB,
    CoreClient:  coreClient,
    StoragePath: os.Getenv("NEBU_MEDIA_STORAGE_PATH"),
}
```

### Godog Feature File Structure

```gherkin
# gateway/features/gdpr_deletion.feature
Feature: GDPR Right to Erasure — Story 14.4

  Background:
    Given the docker compose stack is started

  Scenario: Full GDPR erasure verifies PII cleared and sessions deleted
    Given an admin is authenticated and a victim user "alice" exists with profile "Alice Smith" and avatar
    And "alice" has active sessions
    And "alice" has sent a message in a room
    When admin calls DELETE /api/v1/admin/users/{alice_user_id}
    Then the response status is 200
    And the database shows profiles.displayname = "Deleted User" for alice
    And the database shows profiles.avatar_url IS NULL for alice
    And user_keys private_key is NULL for both signing and encryption keys for alice
    And users.anonymized_at is set for alice
    And no active sessions exist for alice
    And audit_log contains action "gdpr_deletion" for alice
    And the events table still contains alice's message (room history preserved)

  Scenario: Profile endpoint returns anonymized data after deletion
    Given a user "alice" has been GDPR-deleted
    When GET /_matrix/client/v3/profile/@alice:nebu.example is called without auth
    Then the response status is 200
    And the response body contains "Deleted User"
    And the response body does not contain "avatar_url"

  Scenario: OIDC login blocked for deleted user
    Given a user "alice" has been GDPR-deleted (is_active=false)
    When the deleted user's OIDC token triggers ValidateToken for alice
    Then the response status is 403
    And the response body contains "M_USER_DEACTIVATED"
```

### Audit Writer Update

```elixir
# core/apps/compliance/lib/compliance/audit_writer.ex
@known_actions ~w(
  admin_login
  admin_login_failed
  admin_logout
  bootstrap_completed
  bootstrap_failed
  room_created
  room_joined
  compliance_access_requested
  compliance_access_approved
  compliance_access_rejected
  compliance_session_issued
  compliance_session_expired
  compliance_session_revoked
  compliance_export_downloaded
  user_keys_deleted
  user_keys_deletion_attempted
  user_anonymized
  user_deactivated
  user_reactivated
  update_user_role
  room_archived
  room_unarchived
  server_config_updated
  room_upgraded
  gdpr_deletion        ← ADD THIS
)
```

### Compliance Runbook Location

```
docs/compliance/gdpr-deletion-runbook.md
```

Must document:
- Step-by-step deletion procedure (endpoint, required role, request body if any)
- What evidence is generated (audit_log rows, DB state)
- What is NOT deleted (room history / events — by Matrix spec design)
- How to verify deletion (SQL queries for audit trail)
- GDPR Article 17 attestation template

### Godog Step Definitions Location

```
gateway/test/integration/gdpr_deletion_steps_test.go
```

Pattern: mirror `compliance_flow_steps_test.go` — reuse the admin auth helper, DB query helpers, and the `doctorStepDefinition` convention used by other step definition files.

### DB Queries for Godog Assertions

```go
// Check profile anonymized
var displayName string
db.QueryRowContext(ctx, "SELECT displayname FROM profiles WHERE user_id=$1", userID).Scan(&displayName)
assert.Equal(t, "Deleted User", displayName)

// Check avatar_url NULL
var avatarURL sql.NullString
db.QueryRowContext(ctx, "SELECT avatar_url FROM profiles WHERE user_id=$1", userID).Scan(&avatarURL)
assert.False(t, avatarURL.Valid)

// Check private keys NULL
var count int
db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_keys WHERE user_id=$1 AND private_key IS NOT NULL", userID).Scan(&count)
assert.Equal(t, 0, count)

// Check sessions deleted
db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE user_id=$1", userID).Scan(&count)
assert.Equal(t, 0, count)

// Check audit_log contains gdpr_deletion
var auditCount int
db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log WHERE action='gdpr_deletion' AND target_id=$1", userID).Scan(&auditCount)
assert.Greater(t, auditCount, 0)

// Check events NOT deleted (room history preserved)
var eventCount int
db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE sender=$1", "@"+localpart+":"+serverName).Scan(&eventCount)
assert.Greater(t, eventCount, 0)  // events exist → room history preserved
```

### Matrix CS API — Oracle Context

**Relevant endpoints and spec rules for this story (Matrix v1.18):**

- `GET /_matrix/client/v3/profile/{userId}` — Public endpoint (no auth required). Returns `{"displayname": "...", "avatar_url": "..."}`. Fields with empty/NULL values MAY be omitted (spec SHOULD omit). After deletion, `avatar_url` must be absent (NULL → omit).
- `POST /_matrix/client/v3/login` (m.login.token via SSO) — On deactivated user: HTTP 403 with `{"errcode": "M_USER_DEACTIVATED", "error": "..."}`. Matrix spec v1.18 §5.7.1.
- `M_USER_DEACTIVATED` — Valid errcode for login failure. Servers MAY use M_FORBIDDEN instead for privacy. Story requires M_USER_DEACTIVATED explicitly.

**Spec-defined behavior:**
- Room events (messages) MUST NOT be deleted when a user is deactivated or deleted — per Matrix spec, event history is immutable. The `events` table rows remain.
- Profile fields: spec allows servers to return 404 for non-existent users OR to return synthetic data for deleted users. Nebu returns `{"displayname": "Deleted User"}` (no 404) — this is compliant.

### Testing Conventions

- **Godog steps** in `gateway/test/integration/gdpr_deletion_steps_test.go` — use `godog.Suite` registration pattern from existing step files
- **DB assertions** via direct `*sql.DB` queries in step definitions (same pattern as `compliance_flow_steps_test.go`)
- **No mocking of Core**: Godog integration tests run against the real Docker stack
- **Admin auth**: reuse the `authenticateAdmin` helper from other step definition files
- **Room history**: create a room, send a message via gRPC, run deletion, assert event count unchanged

### Security Considerations

- **Self-deletion four-eyes guard**: `DELETE /api/v1/admin/users/{userId}` must reject callerSub == userId with 403 (same pattern as `user_anonymization.go`)
- **GDPR audit trail**: the `gdpr_deletion` audit event must be written in a **separate** DB transaction (never inside the DeactivateUser or DeleteUserKeys TX) — never-raise policy applies
- **Partial failure**: if `DeleteUserKeys` fails (e.g., already deleted), log warn and continue — the anonymization (PII erasure) must still proceed
- **No plaintext PII in audit metadata**: the `gdpr_deletion` audit event metadata must NOT contain display names or email addresses
- **Rate limiting**: `complianceRL` (10 req/min) applies to this route — adequate for a deletion endpoint

### Previous Story Context (14-3c)

14-3c added SCIM 2.0 support. The migration chain is now at `000049`. The next migration for this story would be `000050` IF needed. However, **no new DB migration is required** for 14-4 — all necessary DB columns already exist (`profiles.displayname`, `profiles.avatar_url`, `user_keys.private_key`, `users.anonymized_at`, `users.is_active`, `sessions`, `audit_log`).

### Story Completion Checklist

- [ ] `gateway/internal/compliance/gdpr_delete.go` — handler implementing all 7 steps
- [ ] `gateway/internal/compliance/gdpr_delete_test.go` — unit tests AT-4..AT-6
- [ ] `gateway/cmd/gateway/main.go` — route registered
- [ ] `gateway/internal/matrix/login.go` — M_USER_DEACTIVATED fix
- [ ] `core/apps/compliance/lib/compliance/audit_writer.ex` — `gdpr_deletion` in `@known_actions`
- [ ] `gateway/features/gdpr_deletion.feature` — Godog scenarios AT-1..AT-3
- [ ] `gateway/test/integration/gdpr_deletion_steps_test.go` — step definitions
- [ ] `docs/compliance/` directory created
- [ ] `docs/compliance/gdpr-deletion-runbook.md` — compliance runbook
- [ ] All Godog scenarios pass in `make test-integration`
- [ ] All unit tests pass in `make test-unit-go`
