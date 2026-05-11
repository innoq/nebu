/**
 * Step definitions for features/element/messages/thread_messages.feature
 *
 * Story 9-30 — Bug fix: /relations returns 500, thread messages broken.
 *
 * Root cause: event_map_to_proto passes a %Postgrex.JSONB{decoded: map} struct
 * to Jason.encode! → Protocol.UndefinedError → gRPC INTERNAL → HTTP 500.
 *
 * Fix: In server.ex event_map_to_proto/1, add a Postgrex.JSONB struct branch that
 * extracts the decoded map before encoding.
 *
 * AC1: Thread indicator appears on the root message after a reply is sent (bundled
 *      aggregation visible in /sync response).
 * AC2: Thread panel opens and shows the reply when the indicator is clicked.
 * AC3: /relations returns 200 (network assertion; no 500 from Postgrex.JSONB bug).
 *
 * Regression guard — these tests fail if the Postgrex.JSONB fix in server.ex is reverted.
 *
 * Selector notes for Element Web 1.12.15:
 *   Thread reply button:  [aria-label="Reply in thread"], .mx_MessageContextMenu_threadReply,
 *                         or right-click context menu item
 *   Thread composer:      .mx_ThreadPanel .mx_BasicMessageComposer_input,
 *                         .mx_ThreadView .mx_BasicMessageComposer_input
 *   Thread summary:       .mx_ThreadSummary, .mx_ThreadInfo
 *   Thread panel:         .mx_ThreadPanel, .mx_RightPanel .mx_BaseCard
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page, APIRequestContext, Browser } from '@playwright/test';
import { getActualRoomName, roomIdByScenario } from '../common/room-setup.steps';
import { getApiSession } from '../../fixtures/dex-auth';
import { NEBU_USERS } from '../../fixtures/users';
import type { Response } from '@playwright/test';


const MATRIX_BASE = process.env.NEBU_MATRIX_BASE ?? 'http://localhost:8008';

// Module-level promise for the /relations capture pattern.
// Registered BEFORE the panel-open action via "alex captures the next /relations response",
// awaited in "the /relations request returns 200".
let capturedRelationsPromise: Promise<Response> | null = null;

// ─── Helpers ─────────────────────────────────────────────────────────────────

/**
 * Hover over an event tile containing the given text.
 * Element Web shows the action bar (including thread reply) on hover.
 */
async function hoverOverMessage(page: Page, text: string): Promise<void> {
  const tile = page.locator('.mx_EventTile', { hasText: text }).last();
  await expect(tile).toBeVisible({ timeout: 20_000 });
  await tile.hover();
}

/**
 * Get the thread composer inside the thread panel / thread view.
 * Tries multiple selectors for Element Web version compatibility.
 */
function getThreadComposer(page: Page) {
  return page.locator('.mx_ThreadPanel .mx_BasicMessageComposer_input')
    .or(page.locator('.mx_ThreadView .mx_BasicMessageComposer_input'))
    .or(page.locator('.mx_RightPanel .mx_BasicMessageComposer_input'))
    .first();
}

/**
 * Get the thread panel container.
 */
function getThreadPanel(page: Page) {
  return page.locator('.mx_ThreadPanel, .mx_ThreadView, .mx_RightPanel .mx_BaseCard').first();
}

// ─── Given ───────────────────────────────────────────────────────────────────

// No new Given steps needed — reuses:
//   "Given a room ... exists and ... is a member"         → room-setup.steps.ts
//   "Given marie is a member of room ..."                 → room-setup.steps.ts
//   "Given alex is logged in via Element Web"             → auth.steps.ts
//   "Given marie is logged in via Element Web in a second browser context" → messages.steps.ts

// ─── When ────────────────────────────────────────────────────────────────────

/**
 * "When marie navigates to room {string} in the second context"
 *
 * Uses the secondPage fixture (marie's browser context).
 * Resolves the actual (suffixed) room name from the template name.
 */
When(
  'marie navigates to room {string} in the second context',
  async ({ secondPage }: { secondPage: Page }, roomName: string) => {
    const actualName = getActualRoomName(roomName);

    // Strategy 1 (Element Web 1.12.15+): aria-label
    const byAriaLabel = secondPage.locator(
      `[aria-label="Open room ${actualName}"], [aria-label="Open room ${actualName} invitation."]`
    );
    // Strategy 2: title attribute
    const byTitle = secondPage.locator(`.mx_RoomTile[title="${actualName}"]`)
      .or(secondPage.getByTestId('room-list').locator(`[title="${actualName}"]`));
    // Strategy 3: text in left panel
    const byText = secondPage.locator('.mx_LeftPanel').getByText(actualName, { exact: true });

    if (await byAriaLabel.first().isVisible({ timeout: 10_000 }).catch(() => false)) {
      await byAriaLabel.first().click();
    } else if (await byTitle.first().isVisible({ timeout: 5_000 }).catch(() => false)) {
      await byTitle.first().click();
    } else {
      await expect(byText.first()).toBeVisible({ timeout: 20_000 });
      await byText.first().click();
    }

    // Accept invite if shown — dialog can take several seconds to render after tile click
    const acceptBtn = secondPage.getByRole('button', { name: /^accept$|^join/i });
    const roomView = secondPage.locator('.mx_RoomView_body, .mx_BasicMessageComposer_input').first();

    // Wait for either the room view (already joined) or the accept button (invitation)
    await Promise.race([
      roomView.waitFor({ state: 'visible', timeout: 10_000 }).catch(() => null),
      acceptBtn.first().waitFor({ state: 'visible', timeout: 10_000 }).catch(() => null),
    ]);

    if (await acceptBtn.first().isVisible({ timeout: 500 }).catch(() => false)) {
      await acceptBtn.first().click();
    }

    await roomView.waitFor({ state: 'visible', timeout: 20_000 });
  }
);

/**
 * "When {word} opens the thread panel for the message {string}"
 *
 * Hovers over the message tile to reveal the action bar, then clicks
 * "Reply in thread". Falls back to right-click context menu if action
 * bar button is not visible.
 *
 * Used for both alex (page fixture) and marie (secondPage fixture).
 * The {word} capture is for readability — both use their respective page fixtures
 * but share identical logic via this one step (alex → page, marie → secondPage).
 */
When(
  'alex opens the thread panel for the message {string}',
  async ({ page }: { page: Page }, text: string) => {
    await hoverOverMessage(page, text);

    // Primary: "Reply in thread" button in the action bar
    const replyInThread = page.locator('[aria-label="Reply in thread"]')
      .or(page.locator('.mx_MessageActionBar [aria-label="Reply in thread"]'))
      .or(page.locator('.mx_MessageActionBar button[title="Reply in thread"]'))
      .first();

    if (await replyInThread.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await replyInThread.click();
    } else {
      // Fallback: right-click context menu
      const tile = page.locator('.mx_EventTile', { hasText: text }).last();
      await tile.click({ button: 'right' });
      const menuItem = page.getByRole('menuitem', { name: /reply in thread/i })
        .or(page.locator('.mx_ContextualMenu [aria-label*="thread" i]'));
      await expect(menuItem.first()).toBeVisible({ timeout: 5_000 });
      await menuItem.first().click();
    }

    // Wait for thread panel to open
    await expect(getThreadPanel(page)).toBeVisible({ timeout: 15_000 });
  }
);

When(
  'marie opens the thread panel for the message {string}',
  async ({ secondPage }: { secondPage: Page }, text: string) => {
    await hoverOverMessage(secondPage, text);

    const replyInThread = secondPage.locator('[aria-label="Reply in thread"]')
      .or(secondPage.locator('.mx_MessageActionBar [aria-label="Reply in thread"]'))
      .or(secondPage.locator('.mx_MessageActionBar button[title="Reply in thread"]'))
      .first();

    if (await replyInThread.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await replyInThread.click();
    } else {
      const tile = secondPage.locator('.mx_EventTile', { hasText: text }).last();
      await tile.click({ button: 'right' });
      const menuItem = secondPage.getByRole('menuitem', { name: /reply in thread/i })
        .or(secondPage.locator('.mx_ContextualMenu [aria-label*="thread" i]'));
      await expect(menuItem.first()).toBeVisible({ timeout: 5_000 });
      await menuItem.first().click();
    }

    await expect(getThreadPanel(secondPage)).toBeVisible({ timeout: 15_000 });
  }
);

/**
 * "When {word} types {string} in the thread composer"
 *
 * Uses the thread panel composer (inside .mx_ThreadPanel or .mx_ThreadView),
 * not the main room composer.
 */
When(
  'alex types {string} in the thread composer',
  async ({ page }: { page: Page }, message: string) => {
    const composer = getThreadComposer(page);
    await expect(composer).toBeVisible({ timeout: 15_000 });
    await composer.click();
    await composer.pressSequentially(message, { delay: 10 });
  }
);

When(
  'marie types {string} in the thread composer',
  async ({ secondPage }: { secondPage: Page }, message: string) => {
    const composer = getThreadComposer(secondPage);
    await expect(composer).toBeVisible({ timeout: 15_000 });
    await composer.click();
    await composer.pressSequentially(message, { delay: 10 });
  }
);

/**
 * "When {word} sends the thread reply"
 *
 * Presses Enter in the thread composer to submit the reply.
 */
When(
  'alex sends the thread reply',
  async ({ page }: { page: Page }) => {
    const composer = getThreadComposer(page);
    await composer.press('Enter');
  }
);

When(
  'marie sends the thread reply',
  async ({ secondPage }: { secondPage: Page }) => {
    const composer = getThreadComposer(secondPage);
    await composer.press('Enter');
  }
);

/**
 * "When alex closes the thread panel"
 *
 * Closes the thread panel by clicking the X / close button.
 * Needed in AC3 to reset state before clicking the thread indicator,
 * which is what reliably triggers GET /relations (fetching an existing thread).
 */
When(
  'alex closes the thread panel',
  async ({ page }: { page: Page }) => {
    // Use getByRole which matches accessible name "Close" reliably
    const closeBtn = page.getByRole('button', { name: 'Close', exact: true }).first();

    if (await closeBtn.isVisible({ timeout: 5_000 }).catch(() => false)) {
      await closeBtn.click();
    } else {
      await page.keyboard.press('Escape');
    }

    // Use a specific selector (not the broad .mx_RightPanel .mx_BaseCard)
    await expect(page.locator('.mx_ThreadPanel, .mx_ThreadView').first())
      .toBeHidden({ timeout: 8_000 });
  }
);

/**
 * "When alex captures the next /relations response"
 *
 * Registers a Playwright waitForResponse promise BEFORE the triggering action
 * (alex clicks the thread indicator) to avoid the race where the response arrives
 * before the Then step registers its listener.
 *
 * The URL filter matches all Matrix CS API v1 /relations variants:
 *   /_matrix/client/v1/rooms/{roomId}/relations/{eventId}[/{relType}[/{eventType}]]
 */
When(
  'alex captures the next \\/relations response',
  async ({ page }: { page: Page }) => {
    capturedRelationsPromise = page.waitForResponse(
      (resp) =>
        /\/_matrix\/client\/v[13]\/rooms\/[^/]+\/relations\//.test(resp.url()) &&
        resp.request().method() === 'GET',
      { timeout: 45_000 }
    );
  }
);

/**
 * "When alex clicks the thread indicator on {string}"
 *
 * Finds the thread summary / indicator below the given message and clicks it
 * to open the thread panel.
 *
 * Selectors:
 *   Element Web 1.12.15: .mx_ThreadSummary, .mx_ThreadInfo, or
 *                         a button with text matching N replies / N reply
 */
When(
  'alex clicks the thread indicator on {string}',
  async ({ page }: { page: Page }, text: string) => {
    // Locate the event tile containing the root message
    const tile = page.locator('.mx_EventTile', { hasText: text }).last();
    await expect(tile).toBeVisible({ timeout: 20_000 });

    // Thread summary appears below the tile or as a child element
    const threadSummary = tile.locator('.mx_ThreadSummary, .mx_ThreadInfo')
      .or(page.locator('.mx_ThreadSummary, .mx_ThreadInfo').filter({ hasText: /repl/i }))
      .first();

    await expect(threadSummary).toBeVisible({ timeout: 20_000 });
    await threadSummary.click();

    // Wait for thread panel to open
    await expect(getThreadPanel(page)).toBeVisible({ timeout: 15_000 });
  }
);

// ─── Then ────────────────────────────────────────────────────────────────────

/**
 * "Then the message {string} shows a thread indicator in alex's timeline"
 *
 * After a thread reply is sent and /sync delivers the bundled aggregation,
 * Element Web renders a .mx_ThreadSummary below the root message.
 * This assertion fails if /relations returns 500 (Postgrex.JSONB bug).
 */
Then(
  'the message {string} shows a thread indicator in alex\'s timeline',
  async ({ page }: { page: Page }, text: string) => {
    const tile = page.locator('.mx_EventTile', { hasText: text }).last();
    await expect(tile).toBeVisible({ timeout: 20_000 });

    // Thread summary / indicator must appear below the root tile
    const threadSummary = tile.locator('.mx_ThreadSummary, .mx_ThreadInfo')
      .or(page.locator('.mx_ThreadSummary, .mx_ThreadInfo').filter({ hasText: /repl/i }))
      .first();

    await expect(threadSummary).toBeVisible({ timeout: 30_000 });
  }
);

/**
 * "Then the thread indicator shows at least 1 reply"
 */
Then(
  'the thread indicator shows at least {int} reply',
  async ({ page }: { page: Page }, count: number) => {
    const indicator = page.locator('.mx_ThreadSummary, .mx_ThreadInfo').first();
    await expect(indicator).toBeVisible({ timeout: 20_000 });
    // Matches "1 reply", "2 replies" (EN), "N Antworten" (DE), "N respuestas" (ES)
    const indicatorText = await indicator.textContent({ timeout: 10_000 });
    const match = indicatorText?.match(/(\d+)\s+(repl|antwort|respuest)/i);
    const replyCount = match ? parseInt(match[1], 10) : 0;
    expect(replyCount).toBeGreaterThanOrEqual(count);
  }
);

/**
 * "Then the thread panel is visible"
 */
Then(
  'the thread panel is visible',
  async ({ page }: { page: Page }) => {
    await expect(getThreadPanel(page)).toBeVisible({ timeout: 15_000 });
  }
);

/**
 * "Then the thread panel contains the message {string}"
 */
Then(
  'the thread panel contains the message {string}',
  async ({ page }: { page: Page }, message: string) => {
    const panel = getThreadPanel(page);
    await expect(panel).toBeVisible({ timeout: 15_000 });

    // The reply must appear inside the thread panel
    const replyTile = panel.locator('.mx_EventTile', { hasText: message })
      .or(panel.getByText(message, { exact: false }))
      .first();

    await expect(replyTile).toBeVisible({ timeout: 20_000 });
  }
);

/**
 * "Then the /relations request returns 200"
 *
 * Awaits the promise registered by "alex captures the next /relations response"
 * (which must appear as a When step BEFORE the triggering action).
 * Asserts HTTP 200 — before the Postgrex.JSONB fix this was 500.
 */
Then(
  'the \\/relations request returns 200',
  async () => {
    if (!capturedRelationsPromise) {
      throw new Error(
        'No /relations capture registered. ' +
        'Add "When alex captures the next /relations response" BEFORE the triggering action.'
      );
    }
    const resp = await capturedRelationsPromise;
    capturedRelationsPromise = null;
    expect(resp.status()).toBe(200);
  }
);

/**
 * "Then GET /relations for {string} returns 200"
 *
 * AC3 regression guard (JSONB bug). Element Web may not call GET /relations
 * when there is only one reply (bundled aggregation from /sync suffices).
 * This step makes the call directly via the Matrix API to guarantee coverage.
 *
 * Uses page.evaluate() to extract the current token from Element Web's active
 * MatrixClient (avoids OIDC re-auth overhead of getApiSession in this context).
 */
Then(
  'GET \\/relations for {string} returns 200',
  async ({ page }: { page: Page }, roomName: string) => {
    const roomEntry = roomIdByScenario.get(roomName);
    if (!roomEntry) {
      throw new Error(`Room "${roomName}" not found in roomIdByScenario — ensure Background ran`);
    }

    // Extract the Bearer token from Element Web's active MatrixClient.
    // page.evaluate() runs in the browser; the token is then used via page.request
    // (Node.js side) to bypass CORS restrictions on cross-origin API calls.
    const token = await page.evaluate((): string | null => {
      const w = window as unknown as Record<string, unknown>;
      const peg = w['mxMatrixClientPeg'] as
        | { get?: () => { getAccessToken: () => string } }
        | undefined;
      return (
        peg?.get?.()?.getAccessToken() ??
        (localStorage.getItem('mx_access_token')) ??
        null
      );
    });

    if (!token) {
      throw new Error('GET /relations step: no token available from Element Web page context');
    }

    const { roomId } = roomEntry;
    const headers = { Authorization: `Bearer ${token}` };

    // Find the root event via GET /messages
    const msgsResp = await page.request.get(
      `${MATRIX_BASE}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/messages?dir=b&limit=20`,
      { headers }
    );
    if (!msgsResp.ok()) {
      throw new Error(`GET /messages failed: ${msgsResp.status()} ${await msgsResp.text()}`);
    }
    const msgsBody = await msgsResp.json() as {
      chunk?: Array<{ type: string; content?: { body?: string }; event_id: string }>;
    };
    const rootEvent = (msgsBody.chunk ?? []).find(
      (ev) => ev.type === 'm.room.message' && ev.content?.body === 'Message to verify relations endpoint'
    );
    if (!rootEvent) {
      throw new Error('Root message event not found — ensure "alex sends" step ran first');
    }

    // Call GET /relations — was returning 500 before the Postgrex.JSONB fix
    const relResp = await page.request.get(
      `${MATRIX_BASE}/_matrix/client/v1/rooms/${encodeURIComponent(roomId)}/relations/${encodeURIComponent(rootEvent.event_id)}`,
      { headers }
    );
    const relBody = await relResp.text();
    expect(relResp.status(), `GET /relations returned ${relResp.status()}: ${relBody}`).toBe(200);
  }
);
