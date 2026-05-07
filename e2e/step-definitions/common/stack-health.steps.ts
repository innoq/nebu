/**
 * Common step: "Given the Nebu stack is running"
 *
 * Story 9-26 — Phase 1, AC4.
 * Story 9-26a — Fix M-8: use test.skip() (raises Playwright's SkipError) instead
 * of $test.skip() whose runtime semantics were unverified.
 *
 * Checks that Element Web (port 7070) and Dex (port 5556) are reachable.
 * If either is unreachable, skips the test (shows as 'skipped', not 'failed').
 */

import { Given, test } from '../../fixtures/nebu-fixtures';

const ELEMENT_URL = process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070';
const DEX_HEALTH  = 'http://localhost:5556/dex/.well-known/openid-configuration';

Given(
  'the Nebu stack is running',
  async ({
    request,
  }: {
    request: import('@playwright/test').APIRequestContext;
  }) => {
    const [elementResp, dexResp] = await Promise.all([
      request.get(ELEMENT_URL).catch(() => null),
      request.get(DEX_HEALTH).catch(() => null),
    ]);

    const elementOk = elementResp?.ok() ?? false;
    const dexOk     = dexResp?.ok() ?? false;

    if (!elementOk || !dexOk) {
      const missing: string[] = [];
      if (!elementOk) missing.push(`Element Web (${ELEMENT_URL})`);
      if (!dexOk)     missing.push(`Dex (${DEX_HEALTH})`);

      // M-8 fix: test.skip() raises Playwright's SkipError — correctly shows as 'skipped'
      const reason = `Stack unreachable: ${missing.join(', ')}. Run: make dev`;
      test.skip(true, reason);
    }
  }
);
