---
story_id: 5-29e
title: "Bug: DM Creation Hangs Due to Missing Profile Rows and Empty keys/query Response"
type: bug
severity: critical
epic: 5
status: ready-for-dev
security_review: required
created: 2026-04-29
---

## Summary

Direct Message creation hangs when creating a DM with bootstrap-provisioned users. The client shows "Profile not found" warning and an infinite spinner "Chat mit @{userId} wird erstellt" that never resolves.

## Root Causes

### Bug 2a: GET /profile/{userId} returns 404 for bootstrap users
- When users are provisioned via OIDC bootstrap login, no `profiles` table row is upserted
- `ValidateToken` callback does not persist profile data
- Result: `GET /_matrix/client/v3/profile/@alex:localhost` returns 404 Not Found

### Bug 2b: POST /keys/query returns empty device_keys map for known users
- The keys/query stub returns `{"device_keys":{}, "failures":{}}`
- Known users like `@alex:localhost` are absent from the `device_keys` map
- Result: Client cannot distinguish "user exists, no devices" from "user not found"
- DM creation flow stalls waiting for device keys

## Impact

- **User-facing**: DM creation is completely broken for all bootstrap users
- **Client behavior**: Shows "Profile not found" warning, user can click "Dennoch DM beginnen"
- **After bypass**: Empty room appears, spinner never resolves
- **Severity**: Blocks core Matrix DM functionality

## Acceptance Criteria

### AC1: Profile lookup returns 200 for all provisioned users
- `GET /_matrix/client/v3/profile/@alex:localhost` returns 200 OK
- Response includes `displayname` field from OIDC `preferred_username`
- Applies to all users provisioned via OIDC bootstrap login

### AC2: keys/query returns device_keys entry for known users
- `POST /_matrix/client/v3/keys/query` includes known users in `device_keys` map
- User `@alex:localhost` appears as a key even if device list is empty: `{"@alex:localhost": []}`
- User is NOT present in `failures` map if they exist in the database

### AC3: DM creation completes without spinner
- `POST /_matrix/client/v3/createRoom` with `is_direct=true` returns 200 OK
- `POST /_matrix/client/v3/join/{roomId}` returns 200 OK
- `PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}` returns 200 OK
- No infinite spinner, no "Profile not found" warning

## Implementation Notes

### Fix Scope

**Gateway side (OIDC bootstrap):**
- Modify `ValidateToken` callback to upsert profile row on successful login
- Table: `profiles(user_id, displayname, avatar_url, set_at)`

**Core side (keys/query stub):**
- Implement proper user lookup in keys/query handler
- Return `{"@alex:localhost": []}` for known users (empty device list is acceptable)
- Only add to `failures` map for users that don't exist in database

### Files to Modify

1. `gateway/internal/auth/oidc.go` - Add profile upsert in `ValidateToken` callback
2. `core/apps/signature/handlers.go` - Fix keys/query stub to return known users
3. `core/apps/signature/keys_query_handler.go` - Implement proper user existence check

### Database Schema

```sql
CREATE TABLE IF NOT EXISTS profiles (
    user_id TEXT PRIMARY KEY,
    displayname TEXT,
    avatar_url TEXT,
    set_at TIMESTAMPTZ DEFAULT NOW()
);
```

## Test Coverage

### E2E Tests (existing in `e2e/tests/features/dm/dm_create_bug_5_29e.spec.ts`)

```typescript
test('AC2-a: GET /profile/@alex:localhost returns 200', async ({ request }) => {
  const r = await request.get(`${BASE}/_matrix/client/v3/profile/${encodeURIComponent(ALEX.userId)}`);
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

### ATDD Test (to be generated via `/bmad-testarch-atdd`)

The ATDD test should verify:
1. Alice provisions via OIDC → profile row exists
2. Bob queries Alice's keys → response includes Alice in device_keys
3. Bob creates DM with Alice → 200 OK, no spinner
4. Alice joins DM → 200 OK
5. Bob sends message → 200 OK, message appears in timeline

## Related Stories

- **Story 4-29**: OIDC Bootstrap Login (provides bootstrap user provisioning)
- **Story 5-1**: GET /filter (session management)
- **Story 5-2**: GET /members (room member list)
- **Story 5-3**: POST /read_markers (read receipts)

## References

- Source: `e2e/tests/features/dm/dm_create_bug_5_29e.spec.ts` (2026-04-23)
- Test findings: `tmp/test-findings.md`
- Matrix Spec: [Create Room](https://spec.matrix.org/v1.3/client-server-api/#create-room), [Keys Query](https://spec.matrix.org/v1.3/client-server-api/#get_matrixclientv3keysquery)
