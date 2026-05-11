---
story_id: 5-admin-e2e
title: "E2E Test Coverage: Admin UI Bootstrap Wizard Full Flow"
type: feature
severity: medium
epic: 5
status: ready-for-dev
security_review: optional
created: 2026-04-29
---

## Summary

The `admin_ui.feature` file (Gherkin) describes a complete Bootstrap Wizard E2E test flow, but the corresponding Playwright tests are incomplete. The existing `bootstrap.spec.ts` and `bootstrap-happy-path.spec.ts` files cover only partial scenarios. A comprehensive E2E test suite is needed to validate the full bootstrap wizard flow including OIDC login.

## Current State

### Existing Tests

| File | Coverage | Gaps |
|------|----------|------|
| `bootstrap.spec.ts` | Layout, navigation, individual steps | No full wizard flow, no OIDC login |
| `bootstrap-happy-path.spec.ts` | Full wizard + first-admin OIDC | Limited error scenarios |
| `admin_ui.feature` | Gherkin spec for full flow | No Playwright implementation |

### Missing Scenarios

1. **Full wizard click-through with real OIDC login**
   - Steps 1-4 in sequence
   - Real Dex OIDC authorization
   - OIDC callback → `bootstrap_completed` write
   - Redirect to `/admin/bootstrap/done`

2. **Error handling scenarios**
   - Invalid OIDC credentials
   - OIDC connection timeout
   - Key generation failure
   - Database constraint violations

3. **Regression scenarios**
   - Retry after failed OIDC login (already in `bootstrap-happy-path.spec.ts`)
   - Interrupted wizard (browser back/refresh)
   - Concurrent bootstrap attempts

## Acceptance Criteria

### AC1: Full wizard flow test exists and passes
- `e2e/tests/features/admin/bootstrap-full-flow.spec.ts` implements the `admin_ui.feature` Scenario 1
- Test completes steps 1-4 in sequence
- Real Dex OIDC login works (not mocked)
- Final redirect to `/admin/bootstrap/done` succeeds

### AC2: Error scenarios tested
- Invalid OIDC issuer URL shows error
- Unreachable OIDC provider shows warning
- Key generation failure is handled
- Database constraint violations return 422 (not 500)

### AC3: OIDC login error handling
- Failed OIDC credentials → retry allowed
- OIDC timeout → user can retry
- No 500 errors on retry (verified via `bootstrap-happy-path.spec.ts` Test 2)

## Implementation Guide

### Test File: `e2e/tests/features/admin/bootstrap-full-flow.spec.ts`

```typescript
import { test, expect } from '@playwright/test';

test.describe('Bootstrap Wizard — Full Happy Path', () => {
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
});
```

### Error Scenario Tests

```typescript
test('OIDC connection timeout shows user-friendly error', async ({ page }) => {
  await page.getByRole('textbox', { name: 'OIDC Issuer URL' }).fill('http://localhost:9999/nonexistent');
  await page.getByRole('button', { name: 'Test Connection' }).click();
  await expect(page.locator('#oidc-test-result')).toContainText('✗');
  await expect(page.locator('#oidc-test-result')).toContainText('Connection failed');
});

test('Invalid OIDC credentials can be retried', async ({ page }) => {
  // Complete wizard with wrong credentials
  await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('wrong-secret');
  await page.getByRole('button', { name: 'Complete Setup' }).click();
  
  // Should not return 500
  await expect(page).not.toHaveURL(/\/admin\/bootstrap\/error/);
  
  // Should allow retry
  await page.goto('/admin/bootstrap');
  await page.getByRole('textbox', { name: 'OIDC Client Secret' }).fill('nebu-admin-secret');
  await page.getByRole('button', { name: 'Complete Setup' }).click();
  
  // Should succeed on retry
  await expect(page).toHaveURL(/\/admin\/bootstrap\/done/);
});
```

## Related Files

- `e2e/tests/features/admin/bootstrap.spec.ts` - Partial coverage
- `e2e/tests/features/admin/bootstrap-happy-path.spec.ts` - Happy path + retry
- `gateway/internal/admin/bootstrap.go` - Bootstrap handler implementation
- `gateway/api/openapi.yaml` - Bootstrap API spec

## Testing Checklist

- [ ] Full wizard flow with real OIDC login
- [ ] OIDC connection timeout handling
- [ ] Invalid credentials retry
- [ ] Database constraint error (422, not 500)
- [ ] Browser back/refresh preserves form data
- [ ] CSRF protection on all bootstrap forms
- [ ] Key generation success/error paths

## References

- Gherkin spec: `e2e/features/admin_ui.feature` Scenario 1
- Bootstrap handler: `gateway/internal/admin/bootstrap.go`
- Bootstrap template: `gateway/internal/admin/templates/bootstrap.html`
