---
security_review: required
---

# Story 5.29: Security Follow-up Collector — Deferred Findings from Epic 5

Status: ready-for-dev (living document — new blocks appended during Epic 5 pipeline runs)

## Story

As a security-conscious operator,
I want a single, tracked story that collects every security finding from Epic 5 that was
intentionally deferred from its source story (because it was out of scope, too complex to fix
in-diff, or cross-cutting across multiple handlers),
so that nothing falls through the cracks and each deferred finding has a clear place to be
fixed — either here, or by being split into its own story when the scope grows too large.

---

## Background / Motivation

This story is the **collector/umbrella** for deferred security work during Epic 5. It was
originally created (2026-04-23) from Story 5-27's code-review MAJOR-B (scope reduction:
the Matrix validator roll-out to remaining handlers was too broad to safely land in 5-27).

**Collector pattern:**
Each of the remaining Epic 5 stories (5-1 … 5-9, plus the epic-wide gate 5-28) may surface
MAJOR-severity findings in the TEA / Code-Review / Kassandra gates that are either
(a) too complex to fix in-diff without breaking the source story's coherence, or
(b) cross-cutting concerns that belong together rather than scattered across commits.

When the pipeline for story **5-X** encounters such a finding:
1. The code-reviewer or Kassandra proposes the finding as a **new block** in this document
   (append to "Finding Blocks" below, with a unique ID like `FB-5X-01`).
2. The source story keeps its commit moving with a clear deferral note.
3. The pipeline picks up 5-29 **after 5-28 completes** (the epic-wide gate may add further
   blocks).
4. If a single block becomes too large (> Size M or > 200 LOC impact), it gets **split out
   into its own follow-up story** (5-30, 5-31, …) during the dev pass.

---

## Finding Blocks

### FB-527-01 — Matrix Validator & JSON-Hardening Roll-out to Remaining Handlers

**Source:** Story 5-27 code-review MAJOR-B (2026-04-23).
**Severity:** MAJOR (scope gap against AC2/AC3/AC4 as originally worded).
**Size estimate:** M (≈120–150 LOC impl + ≈200 LOC tests, ~10 handlers).

**What to do:**

1. **Roll-out of `ValidateMatrixRoomID`** to every handler that accepts a `roomId` / `roomIdOrAlias` path param before gRPC/DB:
   - `PostJoinRoom` (`POST /join/{roomIdOrAlias}` — alias vs. roomId branching kept)
   - `PostJoinRoomById` (`POST /rooms/{roomId}/join`)
   - `PostInviteUser` (`POST /rooms/{roomId}/invite`)
   - `PutSetRoomState` (`PUT /rooms/{roomId}/state/{eventType}` + variant with stateKey)
   - `PutSendEvent` (`PUT /rooms/{roomId}/send/{eventType}/{txnId}`)
   - `PostReadMarkers` (`POST /rooms/{roomId}/read_markers`)
   - `PutTyping` (`PUT /rooms/{roomId}/typing/{userId}`)
   - Invalid → 400 `M_INVALID_PARAM` with current body shape.

2. **Roll-out of `ValidateMatrixUserID`** to every handler with a `userId` path param:
   - `GetProfile`, `PutDisplayname`, `PutAvatarURL` (`profile.go`)
   - `GetPresenceStatus` (`presence.go`) — unauthenticated endpoint, MUST still validate format.
   - `PutTyping` (`userId` component — should equal authenticated user; AC8).

3. **Roll-out of `ValidateMatrixEventID` / `eventType`**:
   - `PutSendEvent` and `PutSetRoomState` accept `eventType`. Reject empty and reject values > 255 bytes (Matrix spec limit). No strict reverse-DNS regex (breaks Element custom types) — just length + non-empty + no control chars.
   - Add `ValidateMatrixEventType(s) error` in `validate.go` (new validator).

4. **`requireJSON` roll-out** to every JSON-body handler: `PostLogin`, `PostInviteUser`, `PutSetRoomState`, `PutSendEvent`, `PutDisplayname`, `PutAvatarURL`, `PostReadMarkers`, `PutTyping`, `user_directory.Search`.

5. **`DisallowUnknownFields`** roll-out to every typed-struct decoder:
   - `login.go:PostLogin` (`LoginRequest`)
   - `profile.go:PutDisplayname` (displayname body)
   - `profile.go:PutAvatarURL` (avatar_url body)
   - `user_directory.go:Search` (search_term/limit)
   - `typing.go:PutTyping` (typingRequestBody)
   - Unknown field → 400 `M_BAD_JSON`.

6. `PutTyping` enforces `path userId == authenticated userID` analogous to 5-27 AC5 for presence. Mismatch → 403 `M_FORBIDDEN`.

7. **Backward compatibility regression check:** FluffyChat smoke test (Playwright scripted) still logs in, joins a room, sends a message, sets presence, sets typing, changes displayname, changes avatar.

**Tests (ATDD first):**
- `TestAllRoomHandlers_RejectInvalidRoomID` (parameterized table over 7 handlers).
- `TestAllUserHandlers_RejectInvalidUserID` (parameterized table over 5 handlers).
- `TestEventType_RejectsOverlong` + `TestEventType_RejectsEmpty`.
- `TestAllJsonHandlers_RejectFormEncoded` (parameterized).
- `TestAllTypedStructHandlers_RejectUnknownField` (parameterized).
- `TestTyping_RejectsUserMismatch` + `TestTyping_HappyPath`.
- Playwright `e2e/features/fluffychat-compat-after-5-29.spec.ts`.

---

<!--
  Appending pattern (for pipeline runs of 5-1 through 5-9 and 5-28):

  ### FB-{storyId}-{NN} — {short title}

  **Source:** Story 5-X {gate: code-review | kassandra | TEA} (date).
  **Severity:** {MAJOR|HIGH|CRITICAL}.
  **Size estimate:** {S|M|L}.

  **What to do:** …
  **Tests (ATDD first):** …
  **Why deferred (instead of fixed in source story):** …
-->

### FB-51-01 — Non-superuser DB app role for real RLS enforcement

**Source:** Story 5-1 code-review (2026-04-23, deferred architectural finding).
**Severity:** HIGH (security posture gap — defense-in-depth broken in current dev setup, likely in prod too unless Compose/K8s configuration is corrected).
**Size estimate:** M (touches docker-compose.yml, K8s manifests, migration config, test env setup, potentially CI).

**Observation:** The `nebu` database user has `BYPASSRLS=t` and `rolsuper=t` (verified empirically against the running `nebu-postgres-1` container, 2026-04-23). `ALTER TABLE audit_log FORCE ROW LEVEL SECURITY` plus the implicit deny-all DELETE/UPDATE policies are **nominally present but functionally bypassed** for the only DB user the application ever connects as. The integration tests `TestAuditLogMigration_DeleteDenied`, `TestAuditLogMigration_UpdateDenied`, and `TestAuditLogPurge_SecurityDefinerElevatesAppRole` will fail with "DELETE succeeded as app role" in any env where `nebu` is a superuser.

**Impact:**
- Story 5-1's AC2/AC5 (append-only audit_log) behaviourally not provable in dev, likely not enforced in prod.
- SECURITY DEFINER elevation for `audit_log_purge()` is irrelevant if the caller already has superuser.
- Blocks future stories (5-2 audit writer, 5-6 data export, 5-7 GDPR deletion) from relying on DB-side authorization.

**What to do:**
1. **Two distinct DB roles:**
   - `nebu_migrate` — owner of all tables, used only by golang-migrate. Needs DDL + GRANT privileges.
   - `nebu_app` — plain role, **not superuser, not BYPASSRLS**. Granted only SELECT/INSERT on app tables + EXECUTE on SECURITY DEFINER functions. Runtime connection.
2. Update `docker-compose.yml` (Compose secrets/env) and K8s manifests to provision both roles at DB-init.
3. Update `gateway/internal/db/` so runtime uses `nebu_app`, migrations use `nebu_migrate` via separate `NEBU_DB_URL_MIGRATE`.
4. Update test env vars (`NEBU_TEST_DB_URL` → `nebu_app`, `NEBU_TEST_MIGRATION_DB_URL` → `nebu_migrate`) in README + CI.
5. Audit all 18 existing migrations for GRANT statements — add where `nebu_app` needs minimal privileges.
6. Run full integration suite against the new setup — `TestAuditLogMigration_DeleteDenied` et al. must pass for the right reason.

**Tests (ATDD first):**
- Extend `TestAuditLogMigration_DeleteDenied` to assert the error message contains "row-level security" or "permission denied".
- New `TestAppRole_CannotCreateTable` — confirms `nebu_app` lacks DDL privilege.
- CI smoke test: `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'nebu_app'` returns `(false, false)`.

**Why deferred (instead of fixed in Story 5-1):**
Cross-cutting Compose/K8s/Ops concern affecting every existing migration (18 of them). Landing it inside 5-1 would double the diff and require a separate ATDD pass for role-split. Belongs in the 5-29 collector because it recurs in 5-2, 5-6, 5-7.

**Size-escalation trigger:** If step 5 (per-migration GRANT audit) needs > 5 migrations, split this block into its own **Story 5-30** rather than land in 5-29.

### FB-52-01 — Core gRPC listener (port 9000) has no transport auth → audit-log forgery risk

**Source:** Story 5-2 Kassandra SEC Gate 1 (2026-04-23). Pre-existing ADR-008-Phase-1 transport weakness, but 5-2 adds the first *immutability-dependent* endpoint (`WriteAuditLog`) to this listener, escalating the impact.
**Severity:** HIGH (forgery of regulator-visible records).
**Size estimate:** L (cuts across Compose, K8s, Elixir grpc-elixir config, Go grpc-go dial, test harness).

**Observation:** `docker-compose.yml` publishes port 9000 on the host (`ports: ["9000:9000"]`), the Go client dials with `insecure.NewCredentials()`, and the Elixir grpc-elixir server has no auth interceptor. `Nebu.NodeRegistration` is a one-shot HTTP registration — it does not protect the gRPC listener at all. Any process on the host with L4 access to 9000 can call `WriteAuditLog` with arbitrary payloads. Before 5-2, forgery meant sending chat messages; from 5-2 onward, it means minting regulator-visible audit records.

**What to do:**
1. Land ADR-008 Phase 2 for real: ephemeral mTLS between gateway and core (or at minimum, a shared-secret interceptor on both sides, rotated via the existing PSK pathway).
2. Remove the `ports: 9000:9000` host exposure from `docker-compose.yml` — the port should be reachable only via the internal Compose network.
3. Add an Elixir grpc-elixir interceptor that rejects RPCs without a valid node-registration token in the request metadata.
4. Update all Go call-sites (not just audit) to attach the token in gRPC metadata.

**Tests (ATDD first):**
- `TestCoreGRPC_RejectsUnauthenticatedWriteAuditLog` — a raw gRPC dial without credentials → `Unauthenticated` error.
- `TestCoreGRPC_RejectsForgedNodeToken` — dial with a random token → rejected.
- `TestAuditForgery_NoRowInserted` — end-to-end: unauthenticated call attempts `WriteAuditLog`, assert 0 new `audit_log` rows.
- CI smoke: `docker compose ps` verifies port 9000 is **not** in the published ports list.

**Why deferred:** Cross-cutting ops + transport-layer work. Pairs with FB-51-01 (non-superuser DB app role) because both are "the infrastructure trust model that Epic 5 assumes isn't actually there yet." They should be planned together.

**Size-escalation trigger:** If mTLS cert management is non-trivial (cert rotation, distribution), split cert-management into its own **Story 5-31** and keep the interceptor + port-unbinding in 5-29's FB-52-01 scope.

---

### FB-52-02 — `action` field has no allowlist (forensic clarity, not integrity)

**Source:** Story 5-2 Kassandra SEC Gate 1 (2026-04-23).
**Severity:** MEDIUM.
**Size estimate:** S (one validation function + migration pattern for known action strings).

**Observation:** `WriteAuditLog` accepts any `action` string. A compromised gateway (or, combined with FB-52-01, any external caller) could write `"bootstrap_completed"` or `"security_alert_acknowledged"` without the corresponding system event actually occurring, confusing regulators and forensics. This is not an integrity-breach of the audit trail itself (rows are append-only), but it dilutes the signal.

**What to do:**
1. Define an `@known_actions` allowlist in `Compliance.AuditWriter` covering: `admin_login`, `admin_login_failed`, `admin_logout`, `bootstrap_completed`, `bootstrap_failed`, `room_created`, `room_joined`, `compliance_access_requested`, `compliance_access_approved`, `compliance_access_rejected`, `compliance_data_exported`, `user_deleted`, `user_anonymized` (and update as new stories introduce events).
2. `AuditWriter.log` rejects unknown actions with `{:error, :audit_unknown_action}` and Logger.error — same pattern as current `validate_required`.
3. New stories (5-3..5-9, plus future) must add their action strings to the allowlist in the same PR that introduces them.

**Tests (ATDD first):**
- `TestAuditWriter_UnknownAction_IsRejected` — `log("user", "rogue_event", ...)` → `{:error, :audit_unknown_action}`.
- `TestAuditWriter_KnownActions_AllPass` — parametrized table over all allowlisted actions.

**Why deferred:** Needs a definition decision across the Epic 5 action vocabulary, not just 5-2's four actions. Better to land once 5-3..5-9 pipeline runs have established the full set.

---

### FB-53-01 — Rate-limit `/api/v1/compliance/*` endpoints

**Source:** Story 5-3 Kassandra SEC Gate 1 (2026-04-23).
**Severity:** MEDIUM (DoS via authenticated officer token).
**Size estimate:** S (one middleware wrapping decision).

**Observation:** `POST /api/v1/compliance/access-requests` is wrapped in `bodyLimit64KiB(jwtMiddleware(...))` but no rate-limit middleware. A compromised compliance-officer token can flood the endpoint, generating arbitrary `compliance_requests` rows + audit log entries until DB pressure causes secondary failures elsewhere. Same pattern will apply to all stories 5-4..5-9 endpoints.

**What to do:**
1. Decide tier: `strictRL` (low burst, e.g. 10 req/min/IP) is appropriate — compliance request creation is rare.
2. Wrap each `/api/v1/compliance/*` route in `strictRL(...)` in `main.go`.
3. Test: parametrized table over compliance routes, asserts 11th request in the same window → 429.

**Why deferred:** Same fix-shape will repeat across stories 5-4 through 5-9; bundling avoids 7 separate decisions.

---

### FB-53-02 — Verify XSS escaping for `justification` in Admin UI (Story 5-4 follow-on)

**Source:** Story 5-3 Kassandra SEC Gate 1 (2026-04-23).
**Severity:** LOW (depends on Story 5-4's rendering).
**Size estimate:** XS (one Playwright assert + one server-side check).

**Observation:** `justification` field accepts up to 64 KiB (body-size limit), no max-length validation. Go html/template auto-escapes by default, but Story 5-4 will render `justification` in the Admin Dashboard pending-list — verify no `template.HTML` raw escape, no innerHTML in client-side JS.

**What to do:**
1. Add a Playwright test in 5-4: submit a request with `justification = "<script>alert(1)</script>"`, render in admin pending-list, assert no script execution.
2. Add a max-length validator (e.g. 4096 chars) in 5-3's request body handler — kindly informs the user instead of silently truncating in UI.

**Why deferred:** Test belongs in 5-4 since that's the story rendering the field. Length cap can land in 5-29 or as a small follow-up to 5-3.

---

### FB-53-03 — Restrict `time_range` to a sensible window

**Source:** Story 5-3 Kassandra SEC Gate 1 (2026-04-23).
**Severity:** LOW (resource exhaustion in Story 5-5/5-6).
**Size estimate:** XS.

**Observation:** `time_range_start` / `time_range_end` accept year 0001–9999 (RFC3339 range). A compliance officer can request a 1000-year window, which Story 5-5 (session) and 5-6 (data export) will then translate into expensive event-table scans.

**What to do:**
1. In 5-3 handler: reject if `(end - start) > 365 days` with 400 `M_BAD_JSON: "time_range exceeds maximum 365 days"`.
2. In 5-3 handler: reject if `start < NOW() - 7 years` (older than retention) with 400.

**Why deferred:** Tied to retention-config decision; can land in 5-29 alongside 5-5/5-6 work.

---

### FB-E5-03 — Elixir event_dispatcher: 23 pre-existing test failures (Nebu.Repo + FakeDB drift)

**Source:** Discovered during Story 5-2 TEA Gate 2 (2026-04-23). Not a security issue — listed here so the collector captures all Epic-5 test-debt in one place for epic-close decision-making.
**Severity:** MEDIUM (test pipeline hygiene, not production security).
**Size estimate:** S (mostly fake-db updates + conditional skips).

**Observation:** `make test-unit-elixir` in `event_dispatcher` reports `93 tests, 23 failures, 2 skipped`. Verified pre-existing (not caused by 5-2) via `git stash --include-untracked` on 2026-04-23: same 23 failures at HEAD before 5-2 staging. Two root causes:

1. **`could not lookup Ecto repo Nebu.Repo`** — tests call code paths that use `Nebu.Room.DB` (which calls `Nebu.Repo`), and `Nebu.Repo` is only configured for `:prod`/`:dev` envs. Affected: `create_room_test` (when `name` != ""), most `sync_test` scenarios, parts of `join_room_test`.
2. **`FakeInviteDB.accept_invitation/2 is undefined or private`** and **`SyncTestFakeDB.get_room_name/1 is undefined`** — test fakes out of sync with `Nebu.Room.DB` interface expansions in later stories.

**What to do:**
1. Decide strategy per test: (a) inject a configurable `messages_db_module` override (same pattern as `audit_writer_module()` in 5-2), or (b) mark the test as `@tag :integration` and run it only against the real DB.
2. Update all `FakeDB`/`FakeInviteDB`/`SyncTestFakeDB` modules to implement the current interface surface (add missing `accept_invitation/2`, `get_room_name/1`, etc.). Consider introducing a `@behaviour Nebu.Room.DBBehaviour` (deferred-work.md already flagged this) so fakes are compile-time-checked.

**Tests (ATDD first):**
- Re-run `make test-unit-elixir` after each fix. Target: 0 failures, 0 skipped (or skipped with documented reason).
- Add a compile-time behaviour conformance test for `Nebu.Room.DB` fakes.

**Why deferred:** Pre-existing tech debt across multiple earlier epics. Rescoping this into Story 5-2 would have doubled its diff and delayed audit-log landing. Must be resolved before epic close for a clean pipeline signal.

---

### FB-51-02 — `audit_log.event_time` should be trigger-enforced + retention upper-bound

**Source:** Story 5-1 Kassandra SEC Gate 1 (2026-04-23).
**Severity:** MEDIUM (immutability defense-in-depth) + LOW (DoS resilience).
**Size estimate:** S (one trigger + one guard + two tests).

**Two related sub-items, bundled because they both tighten what the app role can do within the RLS envelope — they will be landed alongside FB-51-01 role separation:**

**(a) MEDIUM — `event_time` trigger enforcement:**
Currently `audit_log.event_time` is `DEFAULT NOW()`, but nothing forbids an INSERT caller from providing a custom value. A compromised gateway process could backdate, future-date, or place entries directly into the purge window. Add a `BEFORE INSERT` trigger that unconditionally sets `NEW.event_time := NOW()`. The existing integration test `TestAuditLogRetentionCleanup_DeletesOldRows` explicitly seeds old rows via `INSERT ... VALUES ($1, ...)` with hand-chosen timestamps — that path must be re-routed: either seed via the **migration role** (which is exempt from app-role RLS/trigger via its own path) or use the SECURITY DEFINER pattern to insert historical rows in tests. This is precisely why this block sits in FB-51-01's vicinity — once roles are split, the seed-path becomes clean.

**(b) LOW — retention_days upper bound:**
`audit.RunCleanup` rejects `retentionDays < 1` but not absurd values. `make_interval(days => 2^31 - 1)` raises `interval out of range` — pathological input crashes the purge but is not an exploit. Add `if retentionDays > 36500` (≈100 years) guard in Go and a matching `RAISE EXCEPTION` in `audit_log_purge`.

**Tests (ATDD first):**
- `TestAuditLog_EventTimeTrigger_ForcesNow` — INSERT with `event_time = '2000-01-01'` → row actually has `NOW()`.
- `TestRunCleanup_RejectsExtremeRetentionDays` — `RunCleanup(ctx, db, 50000)` → `ErrInvalidRetentionDays`.

**Why deferred:**
The trigger fix interacts directly with the test-seed strategy which depends on the role split (FB-51-01). Landing it in Story 5-1 would either break existing integration tests or require a parallel refactor of seed paths that 5-29's role split solves more cleanly.

---

## Acceptance Criteria (for when 5-29 itself enters the pipeline)

1. Every `FB-*` block in this document is either:
   (a) fully addressed (code + tests + green pipeline gates), or
   (b) split into its own story (5-30, 5-31, …) with this document updated to reference the split story.

2. No `FB-*` block may be silently dropped — dropping requires an explicit `**Accepted as risk:** …` note with justification, signed by the instance admin.

3. Each `FB-*` block with size L or complexity exceeding M (per Nebu T-shirt sizing) MUST be split out rather than landed in 5-29's commit.

4. After landing, `make test-unit-go` and `make test-integration` both exit 0. Playwright smoke (FB-527-01 only) exits 0.

5. Kassandra re-runs on the 5-29 diff (SEC Gate 1) and reports CLEAN or MEDIUM-only.

---

## Acceptance Tests

(Tests per `FB-*` block — see each block's "Tests (ATDD first)" section.)

---

## Implementation Notes

- This story is a **living document**. The dev pass reads all blocks at the time of pickup
  (not at story creation). The pipeline may append blocks between story creation and dev.
- When splitting a block, update the block to read: `**Split into Story 5-XX** — see {link}`.
- Use table-driven tests where natural to avoid copy-pasted boilerplate.
- Validators and helpers already exist in `gateway/internal/matrix/validate.go` from 5-27 —
  only call-sites are added.
- Size estimate for the whole collector: **L** if all blocks land here; **M** if FB-527-01
  is the only block; larger blocks should split out.

---

## Dependencies

- **Blocked by:** Stories 5-1 through 5-9 must complete their pipeline first (so all their
  deferred findings are captured as blocks).
- **Blocked by:** Story 5-28 (Epic-5 security gate) must complete first (so any cross-cutting
  findings from the epic-wide scan are captured).
- **Blocks:** None — 5-29 is the last substantive story in Epic 5 before retrospective.

---

## Change Log

- 2026-04-23: Story created as follow-up of 5-27 code-review MAJOR-B (initial scope: Matrix validator roll-out, now block `FB-527-01`).
- 2026-04-23: Reframed as **Security Follow-up Collector** — living document for deferred findings across Epic 5 stories 5-1 through 5-9 and the 5-28 epic gate. Pattern documented (append `FB-{storyId}-{NN}` blocks; split into new story if > Size M).
