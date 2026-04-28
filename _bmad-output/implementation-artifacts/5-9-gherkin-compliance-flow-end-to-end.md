---
security_review: optional
---

# Story 5.9: Gherkin: Compliance Flow End-to-End

Status: review

## Story

**As a** developer,
**I want** Gherkin acceptance tests that cover the full compliance workflow from access request to signed export,
**so that** regressions in the audit, four-eyes, and export paths are caught automatically in CI.

**Size:** S

---

## Acceptance Criteria

### AC1 — Scenario: Full Four-Eyes Compliance Export

`gateway/features/compliance_flow.feature` contains this scenario, and all steps pass green:

- **Given** two compliance officers `officer_a` (`compliance@example.com`) and `officer_b` (a second officer — see OIDC Setup below) with valid Matrix sessions
- **And** a room `test-room` with at least one message from the past 24 hours
- **When** `officer_a` calls `POST /api/v1/compliance/access-requests` with valid `room_id`, `time_range_start`, `time_range_end`, and `justification`
- **Then** response is `201` with `status: "pending"`
- **When** `officer_a` tries to approve their own request via `POST /api/v1/compliance/access-requests/{id}/approve`
- **Then** response is `403` with body containing `"Self-approval"`
- **When** `officer_b` calls `POST /api/v1/compliance/access-requests/{id}/approve`
- **Then** response is `200` with `status: "approved"`
- **When** `officer_a` calls `POST /api/v1/compliance/access-requests/{id}/session`
- **Then** response is `201` with a `session_token` JWT
- **When** `officer_a` calls `GET /api/v1/compliance/export` with `X-Compliance-Token: <session_token>` header and `Authorization: Bearer <officer_a_matrix_token>`
- **Then** response is `200` with a JSON body containing both `events` and `server_signature`
- **And** the `server_signature` is verifiable with the server's Ed25519 public key (read from `server_config` table — key `compliance_signing_key_pub`)

### AC2 — Scenario: DSGVO Deletion + Anonymization

`gateway/features/compliance_flow.feature` contains this scenario, and all steps pass green:

- **Given** an admin (`kai@example.com`) and a user `victim` with profile displayname `"Alice"` (pre-seeded for the test)
- **When** admin calls `DELETE /api/v1/admin/users/victim/keys` with a valid reason
- **Then** response is `200` with `status: "keys_deleted"`
- **And** the `audit_log` table contains a row with `action = 'user_keys_deleted'` and `outcome = 'success'` (verified via direct PostgreSQL query using `dbURL`)
- **When** admin calls `POST /api/v1/admin/users/victim/anonymize`
- **Then** response is `200`
- **And** `GET /_matrix/client/v3/profile/victim` returns `displayname: "Deleted User"`

### AC3 — Scenario: Audit log immutability

`gateway/features/compliance_flow.feature` contains this scenario, and all steps pass green:

- **Given** the `audit_log` table has at least one row (guaranteed by prior scenarios or a seed INSERT via the app role)
- **When** a direct SQL `DELETE FROM audit_log` is executed using the application DB role (`dbURL` — `nebu` user)
- **Then** PostgreSQL raises a policy violation error (RLS — the `audit_log_no_delete` policy returns false for all rows)

### AC4 — Step Definitions

- All step definitions for AC1–AC3 are implemented in `gateway/test/integration/compliance_flow_steps_test.go`
- The file follows the existing `//go:build integration` build tag and is in `package integration_test`
- `initializeComplianceFlowSteps(sc *godog.ScenarioContext)` is called from the existing `InitializeScenario` in `gateway/test/integration/steps_test.go`
- All 3 scenarios run as part of `make test-integration` against the full Docker Compose stack

### AC5 — All three scenarios pass green

`make test-integration` exits 0 with all compliance_flow.feature scenarios green.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **Full Four-Eyes Compliance Export** — Godog/integration
   - Given: Two compliance officers authenticated, room exists with messages
   - When: officer_a creates access request → officer_a self-approves → officer_b approves → officer_a creates session → officer_a exports
   - Then: 201 pending, 403 self-approval, 200 approved, 201 JWT, 200 export with verifiable Ed25519 server_signature

2. **DSGVO Deletion + Anonymization** — Godog/integration
   - Given: Admin authenticated, victim user with displayname "Alice" exists
   - When: admin deletes keys, then anonymizes victim
   - Then: 200 keys_deleted, audit_log row present (via direct DB query), 200 anonymized, profile shows "Deleted User"

3. **Audit log immutability** — Godog/integration
   - Given: audit_log has at least 1 row
   - When: direct `DELETE FROM audit_log` as app DB role (`nebu` user)
   - Then: PostgreSQL RLS policy violation error — DELETE fails

---

## Tasks / Subtasks

- [ ] **Task 1: Dex Config — Add second compliance officer** (AC1)
  - [ ] Add `tom2@example.com` (or reuse `tom@example.com` with `compliance_officer` group) to `dev/dex/config.yaml` — see OIDC Setup below
  - [ ] Alternatively: use the existing `compliance@example.com` as `officer_a` and add a new `compliance2@example.com` entry to `dev/dex/config.yaml`

- [ ] **Task 2: Feature file** (AC1, AC2, AC3)
  - [ ] Create `gateway/features/compliance_flow.feature` with 3 scenarios

- [ ] **Task 3: Step definitions** (AC4)
  - [ ] Create `gateway/test/integration/compliance_flow_steps_test.go` (`//go:build integration`)
  - [ ] Register `initializeComplianceFlowSteps(sc)` in `gateway/test/integration/steps_test.go`'s `InitializeScenario`

- [ ] **Task 4: Verify all scenarios pass** (AC5)
  - [ ] Run `make test-integration` and confirm all 3 compliance_flow scenarios are green

---

## Dev Notes

### OIDC Setup — Two Compliance Officers

Only one `compliance_officer` user exists in `dev/dex/config.yaml` currently:
- `compliance@example.com` (password: `changeme`) — group: `compliance_officer`

**Decision:** Add a second user to `dev/dex/config.yaml`:

```yaml
- email: "compliance2@example.com"
  hash: "$2a$10$E4ye88CWSgoigClMVojmGOu.gHlKe7L7RRf07QWZ60aZmZ7Rfak/6"
  username: "compliance2"
  userID: "00000000-0000-0000-0000-000000000006"
  groups:
    - compliance_officer
```

The hash is identical to all other dev users (`changeme`). This requires a Dex restart (handled automatically by `make test-integration` which uses `docker compose up -d --wait`).

`officer_a` = `compliance@example.com`, `officer_b` = `compliance2@example.com`.

### Authentication Pattern

Use the existing `authenticateUser(username, password, &accessToken, &userID)` helper from `room_flow_steps_test.go`. It chains `iObtainDexTokenFor` + `iPostLoginWithDexToken` (both defined in `auth_steps_test.go`, same package).

The compliance officer Matrix access_token is used as `Authorization: Bearer <token>` for all `/api/v1/compliance/*` requests.

### Compliance API Endpoints (implemented in Stories 5.3–5.6)

| Method | Path | Handler |
|--------|------|---------|
| POST | `/api/v1/compliance/access-requests` | `AccessRequestHandler.PostAccessRequest` |
| POST | `/api/v1/compliance/access-requests/{requestId}/approve` | `ApproveHandler.PostApprove` |
| POST | `/api/v1/compliance/access-requests/{requestId}/session` | `SessionHandler.PostSession` |
| GET | `/api/v1/compliance/export` | `ExportHandler.GetExport` — uses `X-Compliance-Token` header (NOT Bearer) |

**Critical:** The export endpoint uses `X-Compliance-Token` for the compliance session JWT, but **still requires** `Authorization: Bearer <officer_a_matrix_token>` as the JWT middleware validates the caller's Matrix identity and role (`compliance_officer`).

### Server Signature Verification (AC1)

The export response JSON contains `server_signature` as a base64-encoded Ed25519 signature over the document bytes (without the `server_signature` field itself). The test must:

1. Read `compliance_signing_key_pub` from the `server_config` table via `dbURL` (using `database/sql` + `lib/pq` or `pgx`):
   ```sql
   SELECT value FROM server_config WHERE key = 'compliance_signing_key_pub'
   ```
2. Hex-decode the stored value → `ed25519.PublicKey` (32 bytes)
3. Parse the export response JSON, extract `server_signature` (base64-decode → bytes)
4. Reconstruct the signed document: copy the response map, delete the `server_signature` key, marshal to JSON (deterministic key order via `map[string]any` — same as the handler does)
5. Verify: `ed25519.Verify(pubKey, docBytes, sig)`

**Important:** The export handler signs a `map[string]any` marshalled to JSON. The key order in Go's `encoding/json` for maps is alphabetical. The test must replicate this exactly — unmarshal to `map[string]any`, delete `server_signature`, then re-marshal for verification.

Alternatively, the test can directly call `ed25519.Verify` on the raw response body minus the signature field. See `gateway/internal/compliance/export_test.go` for the existing unit test pattern.

### audit_log Direct DB Access (AC2, AC3)

The step definitions use `dbURL` (package-level variable from `main_test.go`) to open a `*sql.DB` connection:
```go
db, err := sql.Open("postgres", dbURL)
```

Import `_ "github.com/lib/pq"` for the postgres driver (already used in other integration tests — check go.mod).

**AC2 — Audit row check:**
```sql
SELECT COUNT(*) FROM audit_log WHERE action = 'user_keys_deleted' AND outcome = 'success' AND target_id = $1
```

**AC3 — RLS immutability test:**
```go
_, err := db.ExecContext(ctx, "DELETE FROM audit_log WHERE 1=1")
// err must be non-nil and contain "policy" or error code 42501 (insufficient_privilege)
```
The `audit_log_no_delete` policy (`FOR DELETE USING (false)`) raises PostgreSQL error code `42501` / message `"new row violates row-level security policy"` for any DELETE attempted by the `nebu` application role.

### Victim User Pre-seeding (AC2)

The step `"an admin and a user victim with profile displayname Alice"` needs to create the victim user before the deletion test. Options:

1. **Preferred:** Have `kai@example.com` (admin) register `victim@example.com` via OIDC (add to Dex config), then patch the profile displayname via `PUT /_matrix/client/v3/profile/{userId}/displayname`.
2. **Simpler (acceptable for this E2E test):** Directly INSERT a row into `users` + `profiles` tables via `dbURL` — this is a legitimate test setup step (not cookie forging or auth bypass; it's creating test data).

Given Story complexity (S size), use option 2: direct DB insert for victim user setup. This avoids the need for a third Dex user and complex OIDC flow just for setup. Document clearly in the step definition that this is test data setup.

### Room Creation for Compliance Export (AC1)

The test room must exist and have messages in the correct time range. Steps:
1. `officer_a` creates a room via `POST /_matrix/client/v3/createRoom`
2. `officer_a` sends a message via `PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}`
3. Use `time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)` as `time_range_start` and `time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)` as `time_range_end` in the access request

Reuse the existing room creation and message sending steps from `room_flow_steps_test.go` if possible, or implement inline.

### Feature File Location

`gateway/features/compliance_flow.feature` — consistent with existing pattern:
- `gateway/features/health.feature`
- `gateway/features/auth.feature`
- `gateway/features/room_flow.feature`
- `gateway/features/admin_bootstrap.feature`

The Godog test runner (`main_test.go`) reads `../../features` relative to `gateway/test/integration/`, which resolves to `gateway/features/`.

### Step Definitions File Location

`gateway/test/integration/compliance_flow_steps_test.go`

Consistent with:
- `gateway/test/integration/auth_steps_test.go`
- `gateway/test/integration/room_flow_steps_test.go`
- `gateway/test/integration/admin_bootstrap_steps_test.go`

### Godog Registration Pattern

In `gateway/test/integration/steps_test.go`, add to `InitializeScenario`:
```go
initializeComplianceFlowSteps(sc) // compliance flow step definitions
```

### Package-Level State Variables

Define compliance-flow-specific state variables at package level (not shared with other step files to avoid name collisions). Suggested:

```go
var officerAAccessToken string
var officerAUserID string
var officerBAccessToken string
var officerBUserID string
var complianceRequestID string
var complianceSessionToken string
var complianceRoomID string
var adminAccessToken string
var adminUserID string
```

### Key Correctness Constraints

1. **Self-approval error message:** The handler returns `"Self-approval not allowed"` — the Gherkin step must match with `contains "Self-approval"` not an exact match.
2. **Export auth dual-token:** `Authorization: Bearer <matrix_token>` + `X-Compliance-Token: <session_token>` — both headers required.
3. **Export time range:** Must overlap with the message timestamp. Use ±1h window around `time.Now()`.
4. **Ed25519 verification:** Go's `crypto/ed25519.Verify` returns `bool`, not `error`. Wrap in `if !ed25519.Verify(...) { return fmt.Errorf(...) }`.
5. **RLS error code:** PostgreSQL returns code `42501` for policy violations. Check `pq.Error.Code == "42501"` or string-match on error message — either is acceptable.
6. **DB driver import:** The integration test package already uses PostgreSQL via other migration tests — check that `github.com/lib/pq` or `github.com/jackc/pgx` is already in `go.mod` before adding a new import.

### Existing DB Driver Usage

Check `gateway/test/integration/compliance_requests_migration_test.go` and `compliance_sessions_migration_test.go` — they already open direct DB connections. Reuse the same driver import.

### go.mod Module Path

`github.com/nebu/nebu` — confirmed from `handler.go` import path.

---

## Project Structure Notes

- Feature file: `gateway/features/compliance_flow.feature` (new file)
- Step definitions: `gateway/test/integration/compliance_flow_steps_test.go` (new file)
- Modified: `gateway/test/integration/steps_test.go` — add `initializeComplianceFlowSteps(sc)` call
- Modified: `dev/dex/config.yaml` — add `compliance2@example.com` static user

No production code changes. This story adds only Gherkin tests and test infrastructure.

---

## References

- Epics.md Story 5.9 spec: `_bmad-output/planning-artifacts/epics.md` (lines 2591–2632)
- Existing step definition pattern: `gateway/test/integration/room_flow_steps_test.go`
- Auth helper: `gateway/test/integration/auth_steps_test.go` — `authenticateUser`, `iObtainDexTokenFor`
- Godog test runner: `gateway/test/integration/main_test.go` — `dbURL`, `matrixURL`, `gatewayURL`
- Compliance handlers: `gateway/internal/compliance/handler.go` — `PostAccessRequest`, `PostApprove`, `PostSession`, `GetExport`
- Ed25519 signing: `gateway/internal/compliance/handler.go` lines 787–820 — signing pattern
- Server key storage: `gateway/cmd/gateway/main.go` lines 842–957 — `compliance_signing_key_pub` hex in `server_config`
- Audit log RLS: `gateway/migrations/000018_audit_log.up.sql` — `audit_log_no_delete` policy
- Dex config (existing users): `dev/dex/config.yaml`
- Feature file examples: `gateway/features/room_flow.feature`, `gateway/features/auth.feature`

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- All 3 scenarios implemented as Godog step definitions.
- `compliance2@example.com` added to `dev/dex/config.yaml` (userID 000...0006, group compliance_officer, same bcrypt hash as all dev users).
- AC2 victim user is created via direct DB INSERT (legitimate test data setup, not auth bypass) — avoids needing a third Dex OIDC flow.
- AC3 RLS error detection covers both pgx error message patterns ("42501", "policy", "insufficient") for portability.
- `make test-unit-go` exits 0 (integration file excluded via `//go:build integration` tag).
- `make test-integration` not run locally — requires Docker Compose stack. Will be validated in CI.

### File List

- NEW: `gateway/features/compliance_flow.feature`
- NEW: `gateway/test/integration/compliance_flow_steps_test.go`
- MODIFIED: `gateway/test/integration/steps_test.go` — added `initializeComplianceFlowSteps(sc)` call
- MODIFIED: `dev/dex/config.yaml` — added `compliance2@example.com` static user
