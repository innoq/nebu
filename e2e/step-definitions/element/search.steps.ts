/**
 * Step definitions for features/element/search.feature
 *
 * Story 11.6 — AC6, AC7, AC8.
 *
 * Implements: Element Web search dialog, result click navigation, empty state.
 * Auth: Authorization Code + PKCE via Dex (no ROPC — dex v2.41+ constraint).
 *
 * Template name "search-test-template" is unique across all feature files in a
 * single test run — do not reuse this name in other feature files.
 *
 * Message pre-seeding (AC6/AC7 Background): messages are sent via direct Matrix
 * API calls (not through the Element Web UI) so search tests are independent of
 * message-send UI timing. Uses getApiSession() + direct PUT /send event call.
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page, APIRequestContext, Browser } from '@playwright/test';
import { getActualRoomName, roomIdByScenario } from '../common/room-setup.steps';
import { getApiSession } from '../../fixtures/dex-auth';
import { NEBU_USERS } from '../../fixtures/users';

const ELEMENT_URL = process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070';
const MATRIX_BASE = process.env.NEBU_MATRIX_BASE ?? 'http://localhost:8008';

// ─── Message pre-seeding ──────────────────────────────────────────────────────

/**
 * "Given alex has sent message {string} in {string}"
 *
 * Sends a message via direct Matrix API (PUT /rooms/{roomId}/send/m.room.message/{txnId}).
 * Called BEFORE the browser login step so the message is indexed before searching.
 */
Given(
  '{word} has sent message {string} in {string}',
  async (
    { request, browser }: { request: APIRequestContext; browser: Browser },
    userName: string,
    message: string,
    roomName: string
  ) => {
    const user = NEBU_USERS[userName as keyof typeof NEBU_USERS];
    if (!user) throw new Error(`Unknown test user: ${userName}`);

    const session = await getApiSession(request, user, browser);
    const roomEntry = roomIdByScenario.get(roomName);
    if (!roomEntry) {
      throw new Error(
        `Room "${roomName}" not found in roomIdByScenario — ensure "a room ${roomName} exists" step ran first`
      );
    }

    const txnId = `search-seed-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const url = `${MATRIX_BASE}/_matrix/client/v3/rooms/${roomEntry.roomId}/send/m.room.message/${txnId}`;
    const resp = await request.put(url, {
      headers: {
        Authorization: `Bearer ${session.token}`,
        'Content-Type': 'application/json',
      },
      data: { msgtype: 'm.text', body: message },
    });

    if (!resp.ok()) {
      const body = await resp.text();
      throw new Error(`Failed to send message "${message}" in room "${roomName}": ${resp.status()} ${body}`);
    }
  }
);

// ─── Search dialog setup for AC7 ─────────────────────────────────────────────

/**
 * "Given {word} has searched for {string} and results are visible"
 *
 * Opens the search dialog, types the term, submits, and waits for at least one
 * result tile to appear. Used as a Given precondition in AC7 so the scenario
 * starts from a fully-populated results panel before clicking a result.
 */
Given(
  '{word} has searched for {string} and results are visible',
  async ({ page }: { page: Page }, _userName: string, term: string) => {
    await openSearchDialog(page);
    await typeInSearchInput(page, term);
    await submitSearch(page);
    // Wait for at least one result tile
    await expect(
      page.locator('.mx_SearchResultTile, .mx_EventTile_searchHighlight, [data-testid="searchResultTile"]').first()
    ).toBeVisible({ timeout: 20_000 });
  }
);

// ─── Search dialog steps ──────────────────────────────────────────────────────

/**
 * "When {word} opens the search dialog"
 *
 * Attempts the search trigger button first; falls back to keyboard shortcut.
 */
When(
  '{word} opens the search dialog',
  async ({ page }: { page: Page }, _userName: string) => {
    await openSearchDialog(page);
  }
);

/**
 * "When {word} types {string} in the search input"
 */
When(
  '{word} types {string} in the search input',
  async ({ page }: { page: Page }, _userName: string, term: string) => {
    await typeInSearchInput(page, term);
  }
);

/**
 * "When {word} submits the search"
 */
When('{word} submits the search', async ({ page }: { page: Page }, _userName: string) => {
  await submitSearch(page);
});

// ─── Assertions ───────────────────────────────────────────────────────────────

/**
 * "Then at least one search result containing {string} is visible"
 */
Then(
  'at least one search result containing {string} is visible',
  async ({ page }: { page: Page }, text: string) => {
    // Wait for the results panel to be non-empty
    const resultPanel = page.locator(
      '.mx_SearchResultsPanel, .mx_RoomSearch_results, [data-testid="searchResults"]'
    ).first();
    await expect(resultPanel).toBeVisible({ timeout: 20_000 });

    // Check that at least one tile contains the expected text
    const tile = page
      .locator(
        '.mx_SearchResultTile, .mx_EventTile_searchHighlight, [data-testid="searchResultTile"]'
      )
      .filter({ hasText: text })
      .first();
    await expect(tile).toBeVisible({ timeout: 10_000 });
  }
);

/**
 * "When {word} clicks on the first search result"
 */
When(
  '{word} clicks on the first search result',
  async ({ page }: { page: Page }, _userName: string) => {
    const firstResult = page
      .locator('.mx_SearchResultTile, .mx_EventTile_searchHighlight, [data-testid="searchResultTile"]')
      .first();
    await expect(firstResult).toBeVisible({ timeout: 10_000 });
    await firstResult.click();
  }
);

// "Then the message {string} is visible in the timeline" is defined in messages.steps.ts

/**
 * "Then an empty state indicator is visible"
 *
 * Waits for the search results panel to settle, then asserts either an explicit
 * empty-state element is visible OR the results panel has zero result tiles.
 */
Then('an empty state indicator is visible', async ({ page }: { page: Page }) => {
  // Wait for the results panel to appear (search must have completed)
  const resultsPanel = page
    .locator('.mx_SearchResultsPanel, .mx_RoomSearch_results, [data-testid="searchResults"]')
    .first();
  await expect(resultsPanel).toBeVisible({ timeout: 20_000 });

  // Check no result tiles are rendered
  const tiles = page.locator('.mx_SearchResultTile, [data-testid="searchResultTile"]');
  await expect(tiles).toHaveCount(0, { timeout: 5_000 });

  // Also accept an explicit empty-state label if Element Web renders one
  // (some versions show a "No results found" banner — we don't require it but accept it)
});

// "Then no error dialog appears" is defined in common/assertions.steps.ts

// ─── Internal helpers ─────────────────────────────────────────────────────────

async function openSearchDialog(page: Page): Promise<void> {
  await page.goto(ELEMENT_URL);

  // Try the search button in the room header or top-level UI
  const searchBtn = page
    .locator('.mx_RoomHeader_search button, [aria-label*="Search"], [data-testid="searchButton"]')
    .first();

  const btnVisible = await searchBtn.isVisible({ timeout: 3_000 }).catch(() => false);
  if (btnVisible) {
    await searchBtn.click();
  } else {
    // Keyboard shortcut fallback (Ctrl+K / Meta+K)
    await page.keyboard.press('Control+k');
  }

  // Wait for the search input to appear
  const input = page
    .locator('.mx_SearchBar_input input')
    .or(page.locator('[data-testid="searchInput"]'))
    .or(page.locator('input[placeholder*="Search"]'))
    .first();
  await expect(input).toBeVisible({ timeout: 10_000 });
}

async function typeInSearchInput(page: Page, term: string): Promise<void> {
  const input = page
    .locator('.mx_SearchBar_input input')
    .or(page.locator('[data-testid="searchInput"]'))
    .or(page.locator('input[placeholder*="Search"]'))
    .first();
  await expect(input).toBeVisible({ timeout: 10_000 });
  await input.fill(term);
}

async function submitSearch(page: Page): Promise<void> {
  await page.keyboard.press('Enter');
  // Wait for results panel or empty state to appear
  await page.waitForSelector(
    '.mx_SearchResultsPanel, .mx_RoomSearch_results, .mx_SearchResultsPanel_noResults, [data-testid="searchResults"]',
    { timeout: 20_000 }
  );
}
