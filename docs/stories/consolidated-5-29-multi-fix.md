---
story_id: 5-29g
title: "Bug Fixes: Sync Long-Poll Timeout, DM Creation Hang, Admin UI E2E Coverage"
type: epic
severity: high
epic: 5
status: ready-for-dev
security_review: required
created: 2026-04-29
---

## Summary

This consolidated story addresses three critical issues discovered during E2E testing and production use:

### Issue 1: Sync Long-Poll Times Out (Story 4-29f)
When new messages or membership events are broadcast via `:pg` (PostgreSQL Process Groups), the sync long-poll sometimes sleeps for the full 30-second timeout instead of waking within seconds. Multiple E2E tests use 35-second timeouts as a workaround.

**Root cause**: Incomplete `:pg` broadcast implementation in `event_dispatcher` - some event types (invites, leave events) don't trigger proper broadcasts, or there's a race between event emission and listener registration.

### Issue 2: DM Creation Hangs (Story 5-29e)
Direct Message creation hangs when creating a DM with bootstrap-provisioned users:
- **Bug 2a**: `GET /profile/{userId}` returns 404 for bootstrap users (no `profiles` table row upserted during OIDC login)
- **Bug 2b**: `POST /keys/query` returns empty `device_keys` map for known users
- **Result**: Client shows "Profile not found" warning and infinite spinner "Chat mit @{userId} wird erstellt"

### Issue 3: Admin UI E2E Coverage Gap (Story 5-admin-e2e)
The `admin_ui.feature` Gherkin spec describes a complete Bootstrap Wizard E2E test flow, but the corresponding Playwright tests are incomplete. Missing full wizard flow with real OIDC login, error scenarios, and regression coverage.

## Acceptance Criteria

### AC1: Sync long-poll wakes within 5 seconds for all event types
- `rooms.invite` events wake sync within 5 seconds
- `rooms.leave` events wake sync within 5 seconds  
- `m.room.message` events wake sync within 5 seconds
- All membership change events wake sync within 5 seconds
- **Verification**: E2E tests `invites.spec.ts` and `room-lifecycle.spec.ts` pass with 10-second timeout (currently 35s workaround)

### AC2: No 30-second long-poll timeouts in happy path
- When events are broadcast, sync returns immediately (< 5s)
- Long-poll only sleeps full 30 seconds when no events exist (expected behavior)

### AC3: Profile lookup returns 200 for all provisioned users
- `GET /_matrix/client/v3/profile/@alex:localhost` returns 200 OK
- Response includes `displayname` field from OIDC `preferred_username`
- Applies to all users provisioned via OIDC bootstrap login
- **Implementation**: Modify `ValidateToken` callback to upsert profile row on successful login

### AC4: keys/query returns device_keys entry for known users
- `POST /_matrix/client/v3/keys/query` includes known users in `device_keys` map
- User `@alex:localhost` appears as a key even if device list is empty: `{"@alex:localhost": []}`
- User is NOT present in `failures` map if they exist in the database
- **Implementation**: Fix `keys/query` handler to return known users from database

### AC5: DM creation completes without spinner
- `POST /_matrix/client/v3/createRoom` with `is_direct=true` returns 200 OK
- `POST /_matrix/client/v3/join/{roomId}` returns 200 OK
- `PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}` returns 200 OK
- No infinite spinner, no "Profile not found" warning

### AC6: Admin UI Bootstrap Wizard E2E test exists and passes
- `e2e/tests/features/admin/bootstrap-full-flow.spec.ts` implements full wizard flow
- Real Dex OIDC login works (not mocked)
- Final redirect to `/admin/bootstrap/done` succeeds
- Error scenarios tested (invalid OIDC issuer, connection timeout, invalid credentials)
- No 500 errors on retry (verified via error scenario tests)

## Implementation Notes

### Files to Investigate/Modify

#### Sync Long-Poll Fix (Issue 1)
1. `core/apps/event_dispatcher/` - Event broadcast logic
2. `core/apps/event_dispatcher/pglisten.go` - PostgreSQL listener implementation
3. `gateway/internal/matrix/sync_handler.go` - Long-poll implementation
4. `core/apps/room_manager/` - Membership event emission

**Questions to answer:**
- Does `emit_membership_event` broadcast for ALL event types (invite, leave, join, kick)?
- Is the `pglisten` listener registered before the long-poll starts?
- Are there any code paths where events are emitted without broadcasting?
- Is there a race between event emission and listener registration?

#### DM Creation Fix (Issue 2)
1. `gateway/internal/auth/oidc.go` - Add profile upsert in `ValidateToken` callback
2. `core/apps/signature/handlers.go` - Fix keys/query stub to return known users
3. `core/apps/signature/keys_query_handler.go` - Implement proper user existence check

**Database schema:**
```sql
CREATE TABLE IF NOT EXISTS profiles (
    user_id TEXT PRIMARY KEY,
    displayname TEXT,
    avatar_url TEXT,
    set_at TIMESTAMPTZ DEFAULT NOW()
);
```

#### Admin UI E2E Tests (Issue 3)
1. `e2e/tests/features/admin/bootstrap-full-flow.spec.ts` - Create new test file
2. `e2e/tests/features/admin/bootstrap-error-scenarios.spec.ts` - Error handling tests
3. Existing: `bootstrap.spec.ts`, `bootstrap-happy-path.spec.ts`

### Database Changes

**New table**: `profiles` (if not exists)
```sql
CREATE TABLE IF NOT EXISTS profiles (
    user_id TEXT PRIMARY KEY,
    displayname TEXT,
    avatar_url TEXT,
    set_at TIMESTAMPTZ DEFAULT NOW()
);
```

**Migration file**: `gateway/migrations/XXXX_create_profiles_table.sql`

## Test Coverage

### E2E Tests (to be updated)

#### Sync Long-Poll (Issue 1)
```typescript
// Current: 35-second timeout (workaround)
await Promise.race([
  syncWithInvitePromise,
  new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Invite not delivered in sync within 35 s')), 35_000)
  ),
]);

// Expected: 10-second timeout (after fix)
await Promise.race([
  syncWithInvitePromise,
  new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error('Invite not delivered in sync within 10 s')), 10_000)
  ),
]);
```

#### DM Creation (Issue 2)
```typescript
test('AC2-a: GET /profile/@alex:localhost returns 200', async ({ request }) => {
  const r = await request.get(
    `${BASE}/_matrix/client/v3/profile/${encodeURIComponent(ALEX.userId)}`
  );
  expect(r.status()).toBe(200);
  const body = await r.json();
  expect(body).toHaveProperty('displayname');
});

test('AC3: POST keys/query for @alex:localhost returns device_keys entry', async ({ request }) => {
  const r = await request.post(`${BASE}/_matrix/client/v3/keys/query`, {
    headers: { Authorization: `Bearer ${marieToken}` },
    data: { device_keys: { [ALEX.userId]: [] } },
  });
  expect(r.status()).toBe(200);
  const body = await r.json();
  expect(body.device_keys).toHaveProperty(ALEX.userId);
  expect(body.failures ?? {}).not.toHaveProperty(ALEX.userId);
});

test('AC4: Marie can create a DM room with Alex via Matrix API', async ({ request }) => {
  const createResp = await request.post(`${BASE}/_matrix/client/v3/createRoom`, {
    headers: { Authorization: `Bearer ${marieToken}` },
    data: { invite: [ALEX.userId], is_direct: true, preset: 'trusted_private_chat' },
  });
  expect(createResp.status()).toBe(200);
  // ... join and send message tests
});
```

#### Admin UI E2E (Issue 3)
```typescript
test('Operator completes Bootstrap Wizard via real OIDC login', async ({ page }) => {
  // Given: no bootstrap_completed in server_config
  
  // Step 1: Instance Name
  await page.goto('/admin/bootstrap');
  await expect(page.getByRole('heading', { name: 'Step 1: Instance Name' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Instance Name' }).fill('test-nebu');
  await page.getByRole('button', { name: 'Next' }).click();
  
  // Step 2: OIDC Configuration
  await expect(page.getByRole('heading', { name: 'Step 2: OIDC Configuration' })).toBeVisible();
  await page.getByRole('textbox', { name: 'OIDC Issuer URL' }).fill('http://dex:5556/dex');
  await page.getByRole('textbox', { name: 'OIDC Client ID' }).fill('nebu-admin');
  await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('nebu-admin-secret');
  await page.getByRole('button', { name: 'Next' }).click();
  
  // Step 3: Key Generation
  await expect(page.getByRole('heading', { name: 'Step 3: Key Generation' })).toBeVisible();
  await page.getByRole('button', { name: 'Generate Keys' }).click();
  await expect(page.locator('#keys-result')).toContainText('Keys generated');
  await page.getByRole('button', { name: 'Next' }).click();
  
  // Step 4: Complete Setup
  await expect(page.getByRole('heading', { name: 'Step 4: Complete Setup' })).toBeVisible();
  await page.getByRole('button', { name: 'Complete Setup' }).click();
  
  // Dex OIDC login
  await page.waitForURL(/dex.*\/auth/);
  await page.locator('input[name="login"]').fill('kai@example.com');
  await page.locator('input[name="password"]').fill('changeme');
  await page.locator('button[type="submit"]').click();
  
  // Final redirect
  await expect(page).toHaveURL(/\/admin\/bootstrap\/done/);
  await expect(page.getByRole('heading', { name: 'Nebu is ready' })).toBeVisible();
});
```

### Integration Test (to be added)

```go
// gateway/test/integration/sync_long_poll_test.go
func TestSyncWakesOnPgNotification(t *testing.T) {
  // Create room, send message
  // Measure time from message send to sync response
  // Assert: < 5 seconds
}
```

## Debug Steps

### Sync Long-Poll Investigation
1. Add logging to `pglisten` when notifications arrive
2. Add logging to `emit_membership_event` when events are broadcast
3. Measure time from event emission to sync response in test environment
4. Check if `pglisten` channel name matches in both sender and receiver

### DM Creation Investigation
1. Verify `profiles` table exists and has data after OIDC bootstrap login
2. Check `ValidateToken` callback implementation in `oidc.go`
3. Review `keys/query` handler user lookup logic
4. Test with Dex developer tools to see actual OIDC flow

## Related Stories

- **Story 4-29**: Room lifecycle (invite, leave, join)
- **Story 5-1**: GET /filter (sync integration)
- **Story 5-2**: GET /members (room member list)
- **Story 5-3**: POST /read_markers (read receipts)
- **Story 4-29f**: Sync Long-Poll Times Out (this consolidated story)
- **Story 5-29e**: DM Creation Hangs (this consolidated story)
- **Story 5-admin-e2e**: Admin UI E2E Coverage (this consolidated story)

## References

### Sync Long-Poll
- E2E Test: `e2e/tests/features/room/invites.spec.ts` line 74-101
- E2E Test: `e2e/tests/features/room/room-lifecycle.spec.ts` line 91-124
- Matrix Spec: [Sync API](https://spec.matrix.org/v1.3/client-server-api/#syncing)

### DM Creation
- Source: `e2e/tests/features/dm/dm_create_bug_5_29e.spec.ts` (2026-04-23)
- Test findings: `tmp/test-findings.md`
- Matrix Spec: [Create Room](https://spec.matrix.org/v1.3/client-server-api/#create-room), [Keys Query](https://spec.matrix.org/v1.3/client-server-api/#get_matrixclientv3keysquery)

### Admin UI
- Gherkin spec: `e2e/features/admin_ui.feature` Scenario 1
- Bootstrap handler: `gateway/internal/admin/bootstrap.go`
- Bootstrap template: `gateway/internal/admin/templates/bootstrap.html`

## Acceptance Tests

### Tests written FIRST (before implementation code):

#### Sync Long-Poll Tests

1. **[Test: Sync wakes on pg notification]** — Integration Test (Go)
   - Given: Running sync long-poll with no events
   - When: Broadcast `:pg` event for new room message
   - Then: Sync response returns within 5 seconds

2. **[Test: Invite event triggers pg broadcast]** — Integration Test (Go)
   - Given: User receives room invite
   - When: Event dispatcher calls `emit_membership_event`
   - Then: `pglisten` notification sent to correct channel

3. **[Test: Membership change wakes sync]** — E2E Test (Playwright)
   - Given: User A invites User B to room
   - When: User B receives sync
   - Then: `rooms.invite` delivered within 10 seconds (not 35s)

#### DM Creation Tests

4. **[Test: Profile lookup returns 200 for bootstrap users]** — E2E Test (Playwright)
   - Given: User Alex provisioned via OIDC bootstrap login
   - When: GET /_matrix/client/v3/profile/@alex:localhost
   - Then: 200 OK with displayname from OIDC preferred_username

5. **[Test: keys/query returns known users]** — E2E Test (Playwright)
   - Given: User Alex exists in database (no devices)
   - When: POST /_matrix/client/v3/keys/query for Alex
   - Then: Alex appears in device_keys map (empty array), not in failures

6. **[Test: DM creation completes without spinner]** — E2E Test (Playwright)
   - Given: Marie wants to create DM with Alex
   - When: POST /_matrix/client/v3/createRoom with is_direct=true
   - Then: 200 OK, no "Profile not found" warning, no infinite spinner

#### Admin UI Tests

7. **[Test: Full wizard flow with real OIDC]** — E2E Test (Playwright)
   - Given: No bootstrap_completed in server_config
   - When: Complete Steps 1-4 with real Dex OIDC login
   - Then: Redirect to /admin/bootstrap/done succeeds

8. **[Test: OIDC connection timeout handled]** — E2E Test (Playwright)
   - Given: Invalid OIDC issuer URL configured
   - When: Click "Test Connection"
   - Then: User-friendly error shown, no 500 response

9. **[Test: Invalid credentials retry]** — E2E Test (Playwright)
   - Given: Wrong OIDC client secret entered
   - When: Click "Complete Setup"
   - Then: Can retry with correct credentials, succeeds on retry

### ATDD Test (to be generated via `/bmad-testarch-atdd`)

The ATDD test should verify:
1. Alice provisions via OIDC → profile row exists
2. Bob queries Alice's keys → response includes Alice in device_keys
3. Bob creates DM with Alice → 200 OK, no spinner
4. Alice joins DM → 200 OK
5. Bob sends message → 200 OK, message appears in timeline
6. Sync long-poll wakes within 5 seconds for all event types
7. Admin wizard completes with real OIDC login

## Implementation Priority

**Recommended order:**
1. **DM Creation Fix** (critical severity) - Blocks core Matrix DM functionality
2. **Sync Long-Poll Fix** (high severity) - Degrades UX significantly
3. **Admin UI E2E Tests** (medium severity) - Improves test coverage

**Dependencies:**
- DM Creation Fix requires `profiles` table migration
- Sync Long-Poll Fix requires investigation of `event_dispatcher` code
- Admin UI E2E tests require existing bootstrap wizard to be functional

## Notes

- All three issues were discovered during E2E testing and production use
- DM creation hang is a showstopper for core Matrix functionality
- Sync long-poll timeout indicates incomplete fix from previous story (4-29)
- Admin UI E2E gap identified during test coverage review
- Security review required due to OIDC authentication changes
