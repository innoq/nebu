---
status: ready-for-dev
epic: 11
story: 6
security_review: not-needed
matrix: true
ui: true
---

# Story 11.6: Gherkin E2E Search Flow (Element UI Tests)

Status: ready-for-dev

## Story

As a developer,
I want Godog Gherkin scenarios covering the full search flow AND Playwright+Cucumber E2E scenarios
verifying search works end-to-end through Element Web,
So that search correctness, access-control scoping, error states, and the browser-level UX
are continuously verified in CI.

**Size:** M

---

## Acceptance Criteria

**AC1 — Godog happy path: search finds a message:**
Given user kai creates a room, sends a message containing "findme-unique-11-6",
When kai calls `POST /_matrix/client/v3/search` with `search_term = "findme-unique-11-6"`,
Then the response is 200 with `search_categories.room_events.results` containing at least one
result whose `result.content.body` includes "findme-unique-11-6" and whose `rank` is > 0

**AC2 — Godog auth enforcement: no token → 401:**
Given no Bearer token is provided,
When `POST /_matrix/client/v3/search` is called,
Then the response is `401 M_UNKNOWN_TOKEN`

**AC3 — Godog membership scoping: non-member gets zero results:**
Given kai has a private room with a message "member-only-content-11-6",
And alex has NOT been invited to that room,
When alex calls `POST /search` with `search_term = "member-only-content-11-6"`,
Then the response is 200 AND `search_categories.room_events.count` is 0 AND `results` is empty
(NOT 403 — membership scoping is enforced at the SQL layer, not at the HTTP layer)

**AC4 — Godog input validation: empty search_term → 400:**
Given a valid Bearer token,
When `POST /search` is called with `search_categories.room_events.search_term = ""`,
Then the response is `400 M_INVALID_PARAM`

**AC5 — Godog rate limit: 11th request in one minute → 429:**
Given kai is authenticated,
When kai sends 11 consecutive `POST /search` requests within one minute,
Then the 11th response is `429 M_LIMIT_EXCEEDED` with `retry_after_ms` present in the body

**AC6 — Playwright: user searches for a sent message, sees results:**
Given alex is logged into Element Web,
And alex has sent a message "playwright-search-target-11-6" in a room,
When alex opens the search dialog and types "playwright-search-target-11-6",
And alex submits the search,
Then at least one result appears containing "playwright-search-target-11-6"

**AC7 — Playwright: clicking a result navigates to the message in the timeline:**
Given search results are shown for "playwright-search-target-11-6",
When alex clicks on the first search result,
Then Element Web navigates to the room and the message is visible (highlighted) in the timeline

**AC8 — Playwright: search with no results shows empty state:**
Given alex is logged into Element Web,
When alex opens the search dialog and types "zzz-no-results-should-exist-xyzzy-11-6",
And alex submits the search,
Then an empty state indicator is visible (no results message or empty list)
And no error dialog appears

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**Godog tests (gateway/features/search.feature + gateway/test/integration/search_steps_test.go):**

**1. `Scenario: Happy path search finds a sent message` (AC1) — Godog**
- Given: the docker compose stack is started, kai is authenticated via OIDC
- Given: kai creates a room, sends message "findme-unique-11-6"
- When: kai calls POST /_matrix/client/v3/search with Bearer token + search_term "findme-unique-11-6"
- Then: status 200
- Then: response body contains "findme-unique-11-6"
- Then: response body contains "rank" with a non-zero value

**2. `Scenario: Unauthenticated search returns 401` (AC2) — Godog**
- Given: the docker compose stack is started
- When: POST /_matrix/client/v3/search is called with no Authorization header
- Then: status 401
- Then: response body has errcode "M_UNKNOWN_TOKEN"

**3. `Scenario: Non-member search returns zero results` (AC3) — Godog**
- Given: the docker compose stack is started, kai and alex are authenticated via OIDC
- Given: kai creates a room (NOT inviting alex), sends message "member-only-content-11-6"
- When: alex calls POST /_matrix/client/v3/search with search_term "member-only-content-11-6"
- Then: status 200
- Then: response body contains "count" with value 0
- Then: response body does NOT contain "member-only-content-11-6" in results

**4. `Scenario: Empty search_term returns 400` (AC4) — Godog**
- Given: the docker compose stack is started, kai is authenticated
- When: POST /search is called with search_categories.room_events.search_term = ""
- Then: status 400
- Then: response body has errcode "M_INVALID_PARAM"

**5. `Scenario: Rate limit blocks 11th request in one minute` (AC5) — Godog**
- Given: the docker compose stack is started, kai is authenticated
- Given: NEBU_RATE_LIMIT_DISABLED is not set (production mode in integration stack)
- When: kai sends 10 POST /search requests (any search term) — all must return non-429
- When: kai sends the 11th POST /search request
- Then: status 429
- Then: response body has errcode "M_LIMIT_EXCEEDED"
- Then: response body contains "retry_after_ms" with a positive value

**Playwright+Cucumber tests (e2e/features/element/search.feature + e2e/step-definitions/element/search.steps.ts):**

**6. `Scenario: Search_finds_sent_message` (AC6) — Playwright**
- Given: the Nebu stack is running
- Given: a room "search-test-template" exists and alex is a member
- Given: alex has sent message "playwright-search-target-11-6" in "search-test-template"
- Given: alex is logged in via Element Web
- When: alex opens the search dialog
- When: alex types "playwright-search-target-11-6" in the search input
- When: alex submits the search
- Then: at least one search result containing "playwright-search-target-11-6" is visible

**7. `Scenario: Search_result_click_navigates_to_message` (AC7) — Playwright**
- Continues from AC6 (or new scenario with same setup)
- When: alex clicks on the first search result
- Then: Element Web navigates to the room
- Then: the message "playwright-search-target-11-6" is visible in the timeline

**8. `Scenario: Search_with_no_results_shows_empty_state` (AC8) — Playwright**
- Given: alex is logged in via Element Web
- When: alex opens the search dialog
- When: alex types "zzz-no-results-should-exist-xyzzy-11-6" in the search input
- When: alex submits the search
- Then: an empty state indicator is visible
- Then: no error dialog appears

---

## Tasks / Subtasks

- [ ] Task 1: Create `gateway/features/search.feature` — all 5 Godog scenarios (red phase, BEFORE step definitions)
  - [ ] Scenario: Happy path search finds a sent message
  - [ ] Scenario: Unauthenticated search returns 401
  - [ ] Scenario: Non-member search returns zero results (AC3 — mandatory assertion: count = 0)
  - [ ] Scenario: Empty search_term returns 400
  - [ ] Scenario: Rate limit blocks 11th request in one minute

- [ ] Task 2: Create `gateway/test/integration/search_steps_test.go` — step definitions
  - [ ] `//go:build integration` build tag
  - [ ] `package integration_test`
  - [ ] Implement all steps for the 5 Godog scenarios
  - [ ] Register via `initializeSearchSteps(sc)` in `steps_test.go`
  - [ ] Re-use shared vars: `kaiAccessToken`, `alexAccessToken`, `kaiUserID`, `alexUserID`, `lastRoomID`, `lastStatusCode`, `lastBody`
  - [ ] `lastSearchBody` package-level var for search response JSON (avoids stomping `lastBody` if needed)

- [ ] Task 3: Wire `initializeSearchSteps` in `gateway/test/integration/steps_test.go`
  - [ ] Add `initializeSearchSteps(sc)` call at the end of `InitializeScenario`
  - [ ] Add comment: `// search Gherkin E2E step definitions (Story 11.6)`

- [ ] Task 4: Create `e2e/features/element/search.feature` — all 3 Playwright scenarios (red phase)
  - [ ] `@ac6-search-finds-message` scenario
  - [ ] `@ac7-search-result-click` scenario
  - [ ] `@ac8-search-no-results` scenario

- [ ] Task 5: Create `e2e/step-definitions/element/search.steps.ts` — Playwright step definitions
  - [ ] Import from `../../fixtures/nebu-fixtures`
  - [ ] Import `expect` from `@playwright/test`
  - [ ] Import `getActualRoomName` from `../common/room-setup.steps`
  - [ ] Import `sendMessage` helper (from `messages.steps.ts` if exported, otherwise inline the API call via `dex-auth.ts`)
  - [ ] Steps: "alex opens the search dialog", "alex types {string} in the search input", "alex submits the search"
  - [ ] Steps: "at least one search result containing {string} is visible"
  - [ ] Steps: "alex clicks on the first search result"
  - [ ] Steps: "the message {string} is visible in the timeline"
  - [ ] Steps: "an empty state indicator is visible"
  - [ ] Steps: "no error dialog appears"

- [ ] Task 6: Verify all tests FAIL (red phase) before implementing
  - [ ] Godog: `make test-integration` fails on new scenarios (endpoint may not exist yet — it does, but steps are new)
  - [ ] Playwright: `npx bddgen && npx playwright test` fails on new search steps (step definitions not yet complete)

- [ ] Task 7: Run tests green + CI validation
  - [ ] All 5 Godog scenarios pass
  - [ ] All 3 Playwright scenarios pass
  - [ ] No regressions in existing integration suite

---

## Dev Notes

### Background: What Stories 11.1–11.5 Built

This story builds on a complete backend stack:

| Story | What it built | Key files |
|---|---|---|
| 11-1 | DB migration: `search_vector tsvector` GIN index + trigger + backfill on `events` table | `migrations/000X_search_vector.up.sql` |
| 11-2 | `Nebu.Search.DB` Elixir module — SQL with `WHERE room_id IN (membership subquery)` | `core/apps/room_manager/lib/nebu/search/db.ex` |
| 11-3 | `SearchMessages` gRPC handler in Elixir Core (reads user_id from x-user-id metadata) | `core/apps/room_manager/lib/nebu_web/grpc/server.ex` |
| 11-4 | `POST /_matrix/client/v3/search` handler in Go Gateway + spec §11.14 response format | `gateway/internal/matrix/search.go` |
| 11-5 | `NewUserRateLimiter` middleware, 10 req/min per user, wired into `/search` route | `gateway/internal/middleware/ratelimit.go` |

The search handler (`gateway/internal/matrix/search.go`) is fully implemented. This story writes the E2E tests that verify it works end-to-end.

### Godog Scenario: Rate Limit (AC5) — CRITICAL IMPLEMENTATION NOTE

The rate limiter from Story 11.5 uses `Burst: 10`, meaning the bucket starts full and the first 10 requests pass immediately. The 11th consecutive request (no delay) is blocked.

**Problem:** The integration test suite runs many scenarios. If `kaiAccessToken` is used for the rate-limit scenario AND other search scenarios in the same test run, the bucket may already be partially consumed. The rate-limit scenario MUST use a **dedicated user** or **fresh token** to avoid cross-scenario interference.

**Resolution options (pick one):**
1. Use a unique user not used by other search scenarios — e.g. `marie` (already available via `marieAccessToken`). Marie is used rarely in existing scenarios.
2. OR: send requests with unique random search terms, then assert on the 11th — the bucket state is per-user, so as long as < 10 requests were made for this user in the current minute, it works.
3. **Recommended:** Use `marieAccessToken` for the rate-limit scenario. Marie is authenticated in the Background step (already in `room_flow_steps_test.go`). This isolates the bucket from `kaiAccessToken`.

**Alternative (simpler):** The rate-limit scenario sends 11 requests rapidly with the same user. Since the suite runs after `docker compose up`, the rate-limiter LRU cache starts empty. If this scenario runs first or the bucket was never touched for that user, 10 pass and the 11th is blocked. Add a comment in the step file warning about cross-scenario ordering.

### Godog Step Implementation Pattern

Follow the established pattern from `tags_steps_test.go`, `compliance_flow_steps_test.go`:

```go
//go:build integration

package integration_test

// Story 11.6: Godog search E2E step definitions
// gateway/features/search.feature — written FIRST (ATDD gate).
//
// ALL steps call the real running gateway via matrixURL (port 8008).
// The POST /search endpoint is implemented in gateway/internal/matrix/search.go.
// The rate limiter is NewUserRateLimiter (middleware/ratelimit.go, Story 11.5).
//
// Shared package vars (from room_flow_steps_test.go):
//   kaiAccessToken, alexAccessToken, marieAccessToken
//   kaiUserID, alexUserID, marieUserID
//   lastRoomID, lastEventID
//   lastStatusCode, lastBody
// New var added here:
//   kaiPrivateRoomID — room kai creates without inviting alex (AC3)

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"

    "github.com/cucumber/godog"
)

var kaiPrivateRoomID string   // AC3: room alex is NOT a member of
var lastSearchBody    string  // stores POST /search response for assertions

// callPostSearch calls POST /_matrix/client/v3/search with the given token and term.
// Stores result in lastStatusCode and lastSearchBody.
func callPostSearch(token, term string) error {
    body := fmt.Sprintf(`{"search_categories":{"room_events":{"search_term":%q,"order_by":"rank"}}}`, term)
    req, err := http.NewRequest(http.MethodPost,
        matrixURL+"/_matrix/client/v3/search",
        bytes.NewBufferString(body))
    if err != nil {
        return fmt.Errorf("building POST /search request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    if token != "" {
        req.Header.Set("Authorization", "Bearer "+token)
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("POST /search failed: %w", err)
    }
    defer resp.Body.Close()
    b, _ := io.ReadAll(resp.Body)
    lastStatusCode = resp.StatusCode
    lastSearchBody = string(b)
    lastBody = lastSearchBody // keep lastBody in sync for shared assertions
    return nil
}
```

### Godog Feature File Pattern

Follow `gateway/features/room_flow.feature` and `gateway/features/compliance_flow.feature`:

```gherkin
Feature: Search — POST /_matrix/client/v3/search E2E
  As a developer
  I want to verify the full search flow including auth and membership scoping
  So that CI catches regressions in the search API

  Scenario: Happy path search finds a sent message
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    When kai creates a room named "search-e2e-room"
    And kai sends the message "findme-unique-11-6" to the room
    When kai calls POST /search with term "findme-unique-11-6"
    Then the response status is 200
    And the response body contains "findme-unique-11-6"
    And the search results contain a non-zero rank
```

**IMPORTANT:** Do NOT invent new authentication step wording. Reuse existing steps from `room_flow_steps_test.go`:
- `Given kai is authenticated via OIDC` → calls `kaiIsAuthenticated()`
- `Given alex is authenticated via OIDC` → calls `alexIsAuthenticated()`
- `When kai creates a room named {string}` → calls `kaiCreatesARoom(name)`
- `And kai sends the message {string} to the room` → calls `kaiSendsMessage(msg)`

New steps needed only for search-specific behavior.

### Godog Step Registration

`steps_test.go` `InitializeScenario` must be updated to add:
```go
initializeSearchSteps(sc)  // search Gherkin E2E step definitions (Story 11.6)
```

This follows the existing pattern for every other feature area.

### Playwright: Element Web Search UI Selectors

Element Web 1.11.x search UI selectors (as of 2026-05 — verify against actual Element Web version in docker-compose):

| UI Element | Selector strategy |
|---|---|
| Search trigger button | `.mx_RoomHeader_search`, `.mx_SearchBar_search`, or keyboard shortcut `Meta+K` / `Ctrl+K` |
| Search input field | `.mx_SearchBar_input input`, `[data-testid="searchInput"]`, or `input[placeholder*="Search"]` |
| Search results container | `.mx_RoomSearch_results`, `.mx_EventTile` within search context |
| Individual result | `.mx_SearchResultTile`, `.mx_EventTile_searchHighlight` |
| Empty state | `.mx_SearchResultsPanel_noResults`, `text=No results found` |
| Error dialog | `.mx_Dialog`, `.mx_QuestionDialog` |

**IMPORTANT:** Use Playwright's `or()` chaining with multiple selector strategies (as established in `messages.steps.ts`) to handle Element Web version differences:
```typescript
const searchInput = page.locator('.mx_SearchBar_input input')
  .or(page.locator('[data-testid="searchInput"]'))
  .or(page.locator('input[placeholder*="Search"]'))
  .first();
```

**Send message setup (AC6 Background):** The `send.feature` uses `Given a room {string} exists and {word} is a member` step + `When alex sends {string}`. However, the search scenario needs the message to be **pre-existing** (not sent during the scenario). Use the common `room-setup.steps.ts` API-based helper to send the message via the Matrix API (not through the UI), so the test doesn't depend on UI timing:
```typescript
// Use getApiSession() + createRoom() + direct Matrix API send event
// Pattern established in room-setup.steps.ts
```

### Playwright: Search Flow Implementation Pattern

```typescript
/**
 * Step definitions for features/element/search.feature
 * Story 11.6 — AC6, AC7, AC8.
 *
 * Implements: Element Web search dialog, result click navigation, empty state.
 * Auth: Authorization Code + PKCE via Dex (no ROPC — dex v2.41+ constraint).
 * Template names must be unique across ALL feature files in a single test run.
 */

import { Given, When, Then } from '../../fixtures/nebu-fixtures';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { getActualRoomName } from '../common/room-setup.steps';
import { getApiSession } from '../../fixtures/dex-auth';
import { NEBU_USERS } from '../../fixtures/users';
import type { APIRequestContext } from '@playwright/test';

const ELEMENT_URL = process.env.NEBU_ELEMENT_URL ?? 'http://localhost:7070';
const MATRIX_BASE = process.env.NEBU_MATRIX_BASE ?? 'http://localhost:8008';

// ─── Pre-send message via Matrix API ─────────────────────────────────────────

Given(
  '{word} has sent message {string} in {string}',
  async ({ request }: { request: APIRequestContext }, userName: string, message: string, roomName: string) => {
    const user = NEBU_USERS[userName as keyof typeof NEBU_USERS];
    if (!user) throw new Error(`Unknown user: ${userName}`);
    const session = await getApiSession(user, request);
    const actualRoomName = getActualRoomName(roomName);
    // get room ID — call /sync or use the room ID from the room-setup.steps Map
    // then send a message via PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}
    // ... (implementation details per established pattern in dex-auth.ts)
  }
);

// ─── Search dialog steps ──────────────────────────────────────────────────────

When(
  '{word} opens the search dialog',
  async ({ page }: { page: Page }, _userName: string) => {
    // Element Web: search can be opened via the search button in the header
    // or keyboard shortcut. Try button first, fall back to keyboard shortcut.
    const searchBtn = page.locator('.mx_RoomHeader_search button, [aria-label*="Search"], [data-testid="searchButton"]').first();
    if (await searchBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
      await searchBtn.click();
    } else {
      // Keyboard shortcut fallback
      await page.keyboard.press('Control+k');
    }
    // Wait for input to appear
    const input = page.locator('.mx_SearchBar_input input, [data-testid="searchInput"], input[placeholder*="Search"]').first();
    await expect(input).toBeVisible({ timeout: 10_000 });
  }
);

When(
  '{word} types {string} in the search input',
  async ({ page }: { page: Page }, _userName: string, term: string) => {
    const input = page.locator('.mx_SearchBar_input input, [data-testid="searchInput"], input[placeholder*="Search"]').first();
    await expect(input).toBeVisible({ timeout: 10_000 });
    await input.fill(term);
  }
);

When('{word} submits the search', async ({ page }: { page: Page }, _userName: string) => {
  await page.keyboard.press('Enter');
  // Wait for search to complete (results container or empty state to appear)
  await page.waitForSelector(
    '.mx_SearchResultsPanel, .mx_RoomSearch_results, .mx_SearchResultTile, [data-testid="searchResults"]',
    { timeout: 20_000 }
  );
});
```

### Playwright: Template Name Uniqueness

Per CLAUDE.md convention and `room-setup.steps.ts` comments: template names MUST be unique across ALL feature files in a single test run. Use:
- `"search-test-template"` — for AC6/AC7 search scenarios
- Do NOT reuse `"msg-send-template"` (used by `send.feature`) or any other existing template name

### Playwright: Storage State and Auth Patterns

From Story 9-26 learnings (memory):
- Element Web 1.11+ stores `mx_access_token` in IndexedDB, NOT localStorage
- `storageState()` only captures localStorage + cookies — NOT IndexedDB
- **Fix:** each test context performs a fresh OIDC login (no storageState restore)
- `getApiSession()` uses the token sidecar file (`auth-state/{user}.token.json`) written during login

The Background step `Given alex is logged in via Element Web` handles this correctly via `login.steps.ts`.

### AC3 Implementation: The "Zero Results, Not 403" Invariant

This is the most important correctness test. The membership scoping in `Nebu.Search.DB` (Story 11.2) uses:
```sql
WHERE events.room_id IN (
  SELECT room_id FROM room_members
  WHERE user_id = $1 AND membership = 'join'
)
```

Non-member alex searching for kai's private message MUST get 200 with `count: 0`, not a 403. The Godog step assertion must check:
1. `lastStatusCode == 200` (not 403)
2. Parsed JSON: `search_categories.room_events.count == 0`
3. Parsed JSON: `search_categories.room_events.results` is an empty array `[]`
4. `lastSearchBody` does NOT contain the unique message text

This is mandatory per the epic design — the Gateway never knows about room membership directly. The Core enforces it at SQL level.

### Room Name Uniqueness in Godog Scenarios

The Godog integration tests use package-level vars (e.g., `kaiPrivateRoomID`). If multiple scenarios call `kaiCreatesARoom()`, the `lastRoomID` will be overwritten. The AC3 scenario needs its own room that alex is NOT invited to. Use `kaiPrivateRoomID` as a separate var to hold the exclusive room:

```go
// In kaiCreatesKaiPrivateRoom step (AC3):
func kaiCreatesAPrivateRoomForSearchTest() error {
    // Use kaiCreatesARoom with a unique name, then store room ID in kaiPrivateRoomID
    if err := kaiCreatesARoom("search-private-room-11-6"); err != nil {
        return err
    }
    kaiPrivateRoomID = lastRoomID
    return nil
}
```

This avoids overwriting `lastRoomID` used by AC1 (happy path room).

### What NOT to Do

- Do NOT write plain `.spec.ts` files — every Playwright test MUST have a `.feature` counterpart (CLAUDE.md convention, established in Story 9-26)
- Do NOT use ROPC (`grant_type=password`) in any auth step — Dex v2.41+ removed support. Use the existing `iObtainDexTokenFor` (Auth Code + PKCE flow) for Godog, and `getApiSession()` / `login.steps.ts` for Playwright.
- Do NOT forge cookies or inject DB state for search tests — use real API calls (send real messages via Matrix API)
- Do NOT add a security review to this story (`security_review: not-needed`) — all auth/security coverage was reviewed by Kassandra in Stories 11.2, 11.3, 11.4, 11.5
- Do NOT re-register already-registered Godog steps. All existing Background steps (`kaiIsAuthenticated`, `kaiCreatesARoom`, `kaiSendsMessage`) are already defined in `room_flow_steps_test.go` and usable from `search_steps_test.go` (same package).
- Do NOT put the Playwright steps in a new subdirectory — place them in `e2e/step-definitions/element/search.steps.ts` following the existing pattern
- Do NOT add the feature file to `e2e/features/admin/` — it belongs in `e2e/features/element/search.feature`

---

## Files to Create / Modify

| File | Action | Notes |
|---|---|---|
| `gateway/features/search.feature` | CREATE | 5 Godog scenarios (AC1–AC5) — written FIRST (red phase) |
| `gateway/test/integration/search_steps_test.go` | CREATE | Godog step definitions — `//go:build integration`, `package integration_test` |
| `gateway/test/integration/steps_test.go` | MODIFY | Add `initializeSearchSteps(sc)` call in `InitializeScenario` |
| `e2e/features/element/search.feature` | CREATE | 3 Playwright+Cucumber scenarios (AC6–AC8) — written FIRST (red phase) |
| `e2e/step-definitions/element/search.steps.ts` | CREATE | Playwright step definitions for Element Web search UI |

No Gateway handler changes. No Elixir changes. No proto changes. No migrations. No new middleware.

---

## Previous Story Intelligence (11.5)

Story 11.5 explicitly documented: "Do NOT add Godog scenarios in this story — Story 11.6 handles E2E tests (including the rate-limit scenario)."

The `/search` route in `gateway/cmd/gateway/main.go` is registered as:
```go
// Story 11.5: searchRL wraps the JWT-authenticated handler so userID is in context.
// 10 req/min per user, burst 10. Keyed on user_id (not IP) for per-user fairness.
mux.Handle("POST /_matrix/client/v3/search",
    bodyLimit1MiB(jwtWithStatusCheck(searchRL(http.HandlerFunc(searchHandler.PostSearch)))))
```

The middleware chain (outermost → innermost):
1. `bodyLimit1MiB` — caps body size at 1 MiB
2. `jwtWithStatusCheck` — validates Bearer token, sets `ContextKeyUserID` in context, returns 401 for missing/invalid tokens
3. `searchRL` — per-user rate limiter (reads `ContextKeyUserID` set by JWT middleware), returns 429 on 11th+ request/minute
4. `searchHandler.PostSearch` — the actual handler

**AC5 implication:** Because `searchRL` is INSIDE `jwtWithStatusCheck`, unauthenticated requests are rejected with 401 BEFORE hitting the rate limiter. The rate-limit scenario MUST use a valid Bearer token — it's testing the rate limiter, not the auth rejection.

The existing `compliance_rate_limit_test.go` shows the pattern for rate-limit integration tests (sends 10 requests then asserts 429 on 11th). However, it uses `X-Forwarded-For` to isolate IP buckets. For search, the bucket key is `user_id`, so use a user not used by other search scenarios (marie).

---

## Architecture References

- `gateway/internal/matrix/search.go` — PostSearch handler (complete implementation)
- `gateway/internal/middleware/ratelimit.go` — `NewUserRateLimiter` (Story 11.5)
- `gateway/internal/middleware/auth.go:142` — `ContextKeyUserID contextKey = "user_id"`
- `gateway/cmd/gateway/main.go` — `/search` route registration with full middleware chain
- `gateway/test/integration/steps_test.go` — `InitializeScenario` to add `initializeSearchSteps`
- `gateway/test/integration/room_flow_steps_test.go` — shared package vars + `authenticateUser`, `kaiCreatesARoom`, `kaiSendsMessage`
- `gateway/test/integration/compliance_rate_limit_test.go` — rate-limit test pattern
- `e2e/features/element/messages/send.feature` — Playwright feature file pattern
- `e2e/step-definitions/element/messages.steps.ts` — Playwright step pattern (pressSequentially, .or() locators)
- `e2e/step-definitions/common/room-setup.steps.ts` — API-based room setup + `getActualRoomName`
- `e2e/fixtures/dex-auth.ts` — `getApiSession`, `createRoom`, `inviteUser`, `sendMessage` helpers
- `e2e/fixtures/users.ts` — `NEBU_USERS` (alex, marie, kai, tom)
- `docs/architecture/adr/ADR-010-fts-strategy.md` — FTS architecture

---

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

### File List

## Change Log

| Date | Change |
|---|---|
| 2026-05-11 | Story created: ready-for-dev |
