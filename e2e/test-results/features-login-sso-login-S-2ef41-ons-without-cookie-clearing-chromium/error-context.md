# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: features/login/sso-login.spec.ts >> SSO Logout / Re-Login regression (bugfix-logout-oidc-dex-session) >> [P0] Login → Logout → Re-Login cycle survives 3 iterations without cookie clearing
- Location: tests/features/login/sso-login.spec.ts:247:7

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: locator('[placeholder*="Search"]').first()
Expected: visible
Timeout: 20000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" with timeout 20000ms
  - waiting for locator('[placeholder*="Search"]').first()

```

# Page snapshot

```yaml
- generic [active] [ref=e1]:
  - generic [ref=e2]:
    - generic [ref=e5]:
      - navigation "Spaces" [ref=e6]:
        - generic [ref=e7]:
          - button "User menu" [ref=e8] [cursor=pointer]:
            - generic [ref=e9]: a
          - button "Expand" [ref=e10] [cursor=pointer]:
            - img [ref=e11]
        - tree "Spaces" [ref=e13]:
          - treeitem "Home" [selected] [ref=e14]:
            - button "Home" [ref=e15] [cursor=pointer]:
              - generic [ref=e17]:
                - img [ref=e20]
                - button "3" [ref=e23]:
                  - generic [ref=e24]: "3"
          - treeitem "Create a space" [ref=e25]:
            - button "Create a space" [ref=e26] [cursor=pointer]:
              - img [ref=e31]
        - button "Threads" [ref=e34] [cursor=pointer]:
          - img [ref=e36]
        - button "Quick settings" [ref=e38] [cursor=pointer]:
          - img [ref=e40]
      - navigation "Room list" [ref=e49]:
        - search [ref=e50]:
          - button "Search Ctrl K" [ref=e51] [cursor=pointer]:
            - img [ref=e52]
            - generic [ref=e54]:
              - generic [ref=e55]: Search
              - generic [ref=e56]: Ctrl K
          - button "Explore rooms" [ref=e57] [cursor=pointer]:
            - img [ref=e58]
        - generic "Room options" [ref=e60]:
          - generic [ref=e61]:
            - heading "Home" [level=1] [ref=e63]
            - generic [ref=e64]:
              - button "Room Options" [ref=e65] [cursor=pointer]:
                - img [ref=e67]
              - button "New conversation" [ref=e69] [cursor=pointer]:
                - img [ref=e71]
        - generic [ref=e75]:
          - button "Expand filter list" [ref=e76] [cursor=pointer]:
            - img [ref=e78]
          - listbox "Room list filters" [ref=e80]:
            - option "Unreads" [ref=e81] [cursor=pointer]
            - option "People" [ref=e82] [cursor=pointer]
            - option "Rooms" [ref=e83] [cursor=pointer]
            - option "Favourites" [ref=e84] [cursor=pointer]
        - listbox "Room list" [ref=e85]:
          - generic [ref=e87]:
            - option "Open room invite-test-1777492065657 invitation." [ref=e89] [cursor=pointer]:
              - generic [ref=e90]:
                - text: i
                - generic [ref=e91]:
                  - generic "invite-test-1777492065657" [ref=e93]
                  - img [ref=e96]
            - option "Open room invite-test-1777491301724 invitation." [ref=e99] [cursor=pointer]:
              - generic [ref=e100]:
                - text: i
                - generic [ref=e101]:
                  - generic "invite-test-1777491301724" [ref=e103]
                  - img [ref=e106]
            - option "Open room invite-test-1777490464926 invitation." [ref=e109] [cursor=pointer]:
              - generic [ref=e110]:
                - text: i
                - generic [ref=e111]:
                  - generic "invite-test-1777490464926" [ref=e113]
                  - img [ref=e116]
            - option "Open room e2e-multiuser" [ref=e119] [cursor=pointer]:
              - generic [ref=e120]:
                - text: e
                - generic "e2e-multiuser" [ref=e123]
            - option "Open room reconnect-test-room" [ref=e129] [cursor=pointer]:
              - generic [ref=e130]:
                - text: r
                - generic "reconnect-test-room" [ref=e133]
            - 'option "Open room DM: Marie ↔ Alex" [ref=e135] [cursor=pointer]':
              - generic [ref=e136]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e139]'
            - option "Open room nav-b-1777492276562" [ref=e145] [cursor=pointer]:
              - generic [ref=e146]:
                - text: "n"
                - generic "nav-b-1777492276562" [ref=e149]
            - option "Open room nav-a-1777492276562" [ref=e151] [cursor=pointer]:
              - generic [ref=e152]:
                - text: "n"
                - generic "nav-a-1777492276562" [ref=e155]
            - option "Open room create-test-1777492137282" [ref=e157] [cursor=pointer]:
              - generic [ref=e158]:
                - text: c
                - generic "create-test-1777492137282" [ref=e161]
            - option "Open room @marie:localhost" [ref=e163] [cursor=pointer]:
              - generic [ref=e164]:
                - text: m
                - generic "@marie:localhost" [ref=e167]
            - option "Open room Empty room" [ref=e173] [cursor=pointer]:
              - generic [ref=e174]:
                - text: E
                - generic "Empty room" [ref=e177]
            - option "Open room read-markers-test-room" [ref=e179] [cursor=pointer]:
              - generic [ref=e180]:
                - text: r
                - generic "read-markers-test-room" [ref=e183]
            - option "Open room members-test-room" [ref=e185] [cursor=pointer]:
              - generic [ref=e186]:
                - text: m
                - generic "members-test-room" [ref=e189]
            - option "Open room e2e-multiuser" [ref=e191] [cursor=pointer]:
              - generic [ref=e192]:
                - text: e
                - generic "e2e-multiuser" [ref=e195]
            - option "Open room reconnect-test-room" [ref=e201] [cursor=pointer]:
              - generic [ref=e202]:
                - text: r
                - generic "reconnect-test-room" [ref=e205]
            - 'option "Open room DM: Marie ↔ Alex" [ref=e207] [cursor=pointer]':
              - generic [ref=e208]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e211]'
            - option "Open room nav-b-1777491502011" [ref=e217] [cursor=pointer]:
              - generic [ref=e218]:
                - text: "n"
                - generic "nav-b-1777491502011" [ref=e221]
            - option "Open room nav-a-1777491502011" [ref=e223] [cursor=pointer]:
              - generic [ref=e224]:
                - text: "n"
                - generic "nav-a-1777491502011" [ref=e227]
            - option "Open room create-test-1777491373308" [ref=e229] [cursor=pointer]:
              - generic [ref=e230]:
                - text: c
                - generic "create-test-1777491373308" [ref=e233]
            - option "Open room @marie:localhost" [ref=e235] [cursor=pointer]:
              - generic [ref=e236]:
                - text: m
                - generic "@marie:localhost" [ref=e239]
            - option "Open room Empty room" [ref=e245] [cursor=pointer]:
              - generic [ref=e246]:
                - text: E
                - generic "Empty room" [ref=e249]
            - option "Open room read-markers-test-room" [ref=e251] [cursor=pointer]:
              - generic [ref=e252]:
                - text: r
                - generic "read-markers-test-room" [ref=e255]
            - option "Open room members-test-room" [ref=e257] [cursor=pointer]:
              - generic [ref=e258]:
                - text: m
                - generic "members-test-room" [ref=e261]
            - option "Open room e2e-multiuser" [ref=e263] [cursor=pointer]:
              - generic [ref=e264]:
                - text: e
                - generic "e2e-multiuser" [ref=e267]
            - option "Open room reconnect-test-room" [ref=e273] [cursor=pointer]:
              - generic [ref=e274]:
                - text: r
                - generic "reconnect-test-room" [ref=e277]
            - 'option "Open room DM: Marie ↔ Alex" [ref=e279] [cursor=pointer]':
              - generic [ref=e280]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e283]'
            - 'option "Open room DM: Marie ↔ Alex" [ref=e289] [cursor=pointer]':
              - generic [ref=e290]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e293]'
            - option "Open room Empty room" [ref=e299] [cursor=pointer]:
              - generic [ref=e300]:
                - text: E
                - generic "Empty room" [ref=e303]
            - option "Open room Empty room" [ref=e305] [cursor=pointer]:
              - generic [ref=e306]:
                - text: E
                - generic "Empty room" [ref=e309]
            - option "Open room Empty room" [ref=e311] [cursor=pointer]:
              - generic [ref=e312]:
                - text: E
                - generic "Empty room" [ref=e315]
            - option "Open room Empty room" [ref=e317] [cursor=pointer]:
              - generic [ref=e318]:
                - text: E
                - generic "Empty room" [ref=e321]
            - option "Open room Empty room" [ref=e323] [cursor=pointer]:
              - generic [ref=e324]:
                - text: E
                - generic "Empty room" [ref=e327]
      - main [ref=e331]:
        - generic [ref=e332]:
          - generic [ref=e333]:
            - button "Add a photo so people know it's you." [ref=e334] [cursor=pointer]:
              - text: a
              - img [ref=e336]
            - heading "Welcome alex" [level=1] [ref=e338]
            - heading "Now, let's help you get started" [level=2] [ref=e339]
          - generic [ref=e340]:
            - button "Send a Direct Message" [ref=e341] [cursor=pointer]:
              - img [ref=e342]
              - text: Send a Direct Message
            - button "Explore Public Rooms" [ref=e344] [cursor=pointer]:
              - img [ref=e345]
              - text: Explore Public Rooms
            - button "Create a Group Chat" [ref=e347] [cursor=pointer]:
              - img [ref=e348]
              - text: Create a Group Chat
    - alert
  - generic:
    - generic:
      - img
      - generic: Threads
  - generic:
    - generic:
      - img
      - generic: Quick settings
  - generic:
    - generic:
      - img
      - generic: Room Options
  - generic:
    - generic:
      - img
      - generic: New conversation
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
  - generic:
    - generic:
      - img
      - generic: More Options
  - generic:
    - generic:
      - img
      - generic: Notification options
```

# Test source

```ts
  183 |           data: {},
  184 |         },
  185 |       );
  186 |       expect(joinResp.status()).toBe(200);
  187 | 
  188 |       const marieReply = await page.request.put(
  189 |         `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/txn-sso-multi-2`,
  190 |         {
  191 |           headers: { Authorization: `Bearer ${marieSession.accessToken}`, 'Content-Type': 'application/json' },
  192 |           data: { msgtype: 'm.text', body: 'Hey Alex from marie!' },
  193 |         },
  194 |       );
  195 |       expect(marieReply.status()).toBe(200);
  196 |     }
  197 | 
  198 |     // Verify messages via Matrix API
  199 |     const msgsResp = await page.request.get(
  200 |       `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/messages?dir=b&limit=10`,
  201 |       { headers: { Authorization: `Bearer ${alexSession.accessToken}` } },
  202 |     );
  203 |     expect(msgsResp.status()).toBe(200);
  204 |     const msgs = await msgsResp.json();
  205 |     const bodies = (msgs.chunk as Array<{ content?: { body?: string } }>)
  206 |       .map(e => e.content?.body).filter(Boolean);
  207 |     expect(bodies).toContain('Hey Marie from alex!');
  208 |     if (marieSession) {
  209 |       expect(bodies).toContain('Hey Alex from marie!');
  210 |     }
  211 |   });
  212 | });
  213 | 
  214 | // ---------------------------------------------------------------------------
  215 | // Login → Logout → Re-Login regression (bugfix-logout-oidc-dex-session)
  216 | // ---------------------------------------------------------------------------
  217 | 
  218 | test.describe('SSO Logout / Re-Login regression (bugfix-logout-oidc-dex-session)', () => {
  219 |   test.setTimeout(180_000);
  220 | 
  221 |   test.beforeAll(async () => {
  222 |     const [elemOk, dexOk] = await Promise.all([isElementReachable(), isDexReachable()]);
  223 |     test.skip(
  224 |       !elemOk,
  225 |       `Element Web at ${ELEMENT_URL} is unreachable. Run: docker compose --profile e2e up -d --wait`,
  226 |     );
  227 |     test.skip(
  228 |       !dexOk,
  229 |       'Dex unreachable at localhost:5556 — add "127.0.0.1 dex" to /etc/hosts',
  230 |     );
  231 |   });
  232 | 
  233 |   /**
  234 |    * [P0] Login → Logout → Re-Login cycle — 3 iterations without cookie clearing.
  235 |    *
  236 |    * AC 2 — bugfix-logout-oidc-dex-session
  237 |    *
  238 |    * Before the fix (prompt=login missing in GetSSORedirect):
  239 |    *   Dex reuses the same session cookie → returns the same id_token → the JWT
  240 |    *   is already in the denylist → sync returns 401 → Element lands on #/welcome.
  241 |    *
  242 |    * After the fix (prompt=login added):
  243 |    *   Dex is forced to re-authenticate the user on every SSO redirect →
  244 |    *   fresh JWT with new iat/exp/jti → different denylist hash → sync succeeds →
  245 |    *   Element shows the room list.
  246 |    */
  247 |   test('[P0] Login → Logout → Re-Login cycle survives 3 iterations without cookie clearing', async ({ page }) => {
  248 |     const ITERATIONS = 3;
  249 | 
  250 |     for (let iteration = 1; iteration <= ITERATIONS; iteration++) {
  251 |       // ── Step 1: Navigate to Element and click Sign In ────────────────────
  252 |       await page.goto(ELEMENT_URL);
  253 |       await expect(
  254 |         page.getByRole('heading', { name: /welcome to element|willkommen bei element/i }),
  255 |       ).toBeVisible({ timeout: 15_000 });
  256 | 
  257 |       await page.getByRole('link', { name: /sign in|anmelden/i }).click();
  258 |       await expect(
  259 |         page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }),
  260 |       ).toBeVisible({ timeout: 15_000 });
  261 | 
  262 |       // ── Step 2: Click SSO → Dex form must appear (forced by prompt=login) ─
  263 |       await page.getByRole('button', { name: /continue with sso|mit sso fortfahren|weiter mit sso/i }).click();
  264 |       await page.waitForURL(/dex.*\/auth/i, { timeout: 15_000 });
  265 | 
  266 |       // With prompt=login, Dex MUST show the credential form even if a session cookie exists.
  267 |       // If the form is missing, the test fails here — indicating prompt=login is not set.
  268 |       await expect(page.locator('input[name="login"]')).toBeVisible({ timeout: 10_000 });
  269 | 
  270 |       // ── Step 3: Fill credentials and submit ──────────────────────────────
  271 |       await page.locator('input[name="login"]').fill('alex@example.com');
  272 |       await page.locator('input[name="password"]').fill('changeme');
  273 |       await page.locator('button[type="submit"]').click();
  274 | 
  275 |       // ── Step 4: Back to Element — dismiss key dialog, assert room list ───
  276 |       await page.waitForURL(/localhost:7070/, { timeout: 20_000 });
  277 |       await dismissKeyDialog(page);
  278 | 
  279 |       // AC 2: room list (Search placeholder) must be visible — NOT #/welcome.
  280 |       // Before the fix this assertion fails on iteration 2+ because sync returns 401.
  281 |       await expect(
  282 |         page.locator('[placeholder*="Search"]').first(),
> 283 |       ).toBeVisible({ timeout: 20_000 });
      |         ^ Error: expect(locator).toBeVisible() failed
  284 | 
  285 |       // Explicit guard: if the URL contains #/welcome the bug is present.
  286 |       const afterLoginUrl = page.url();
  287 |       if (afterLoginUrl.includes('#/welcome')) {
  288 |         throw new Error(
  289 |           `Iteration ${iteration}/${ITERATIONS}: landed on #/welcome after SSO login.\n` +
  290 |           'Root cause: Dex returned a cached (denylist\'d) id_token because prompt=login is missing.\n' +
  291 |           'Fix: add oauth2.SetAuthURLParam("prompt", "login") to GetSSORedirect in sso.go',
  292 |         );
  293 |       }
  294 | 
  295 |       // ── Step 5: Sign Out (skip on last iteration — no need to log out) ───
  296 |       if (iteration < ITERATIONS) {
  297 |         // Open user menu and sign out
  298 |         const avatarButton = page.locator('[data-testid="user-menu-trigger"]')
  299 |           .or(page.locator('.mx_UserMenu_userAvatarButton'))
  300 |           .or(page.locator('[aria-label*="user menu" i]'))
  301 |           .first();
  302 |         await avatarButton.click({ timeout: 10_000 });
  303 | 
  304 |         await page.getByRole('menuitem', { name: /sign out|abmelden/i }).click({ timeout: 10_000 });
  305 | 
  306 |         // Confirm sign-out dialog if present (scoped inside dialog to avoid matching menu items)
  307 |         const dialog = page.getByRole('dialog');
  308 |         const confirmBtn = dialog.getByRole('button', { name: /sign out|abmelden/i });
  309 |         if (await confirmBtn.isVisible({ timeout: 3_000 }).catch(() => false)) {
  310 |           await confirmBtn.click();
  311 |         }
  312 | 
  313 |         // Wait for welcome screen — confirms logout completed
  314 |         await expect(
  315 |           page.getByRole('heading', { name: /welcome to element|willkommen bei element/i }),
  316 |         ).toBeVisible({ timeout: 20_000 });
  317 |       }
  318 |     }
  319 |   });
  320 | });
  321 | 
  322 | // ---------------------------------------------------------------------------
  323 | // New endpoint smoke tests (Stories 5-1/5-2/5-3)
  324 | // ---------------------------------------------------------------------------
  325 | 
  326 | test.describe('Element Web — New endpoint smoke tests (Stories 5-1/5-2/5-3)', () => {
  327 |   test.setTimeout(120_000);
  328 | 
  329 |   test.beforeAll(async () => {
  330 |     const [elemOk, dexOk] = await Promise.all([isElementReachable(), isDexReachable()]);
  331 |     test.skip(!elemOk, `Element Web at ${ELEMENT_URL} is unreachable`);
  332 |     test.skip(!dexOk, 'Dex unreachable — add "127.0.0.1 dex" to /etc/hosts');
  333 |   });
  334 | 
  335 |   // ── Story 5-1: GET /user/{userId}/filter/{filterId} ───────────────────────
  336 | 
  337 |   test('Member list populated after joining room (Story 5-2: GET /members)', async ({ page }) => {
  338 |     const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
  339 | 
  340 |     // Create a room and navigate into it
  341 |     const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
  342 |       headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
  343 |       data: { name: 'members-test-room', visibility: 'private' },
  344 |     });
  345 |     expect(createResp.status()).toBe(200);
  346 |     const { room_id: roomId } = await createResp.json();
  347 | 
  348 |     // Navigate to the room
  349 |     await page.goto(`${ELEMENT_URL}/#/room/${roomId}`);
  350 |     await dismissKeyDialog(page);
  351 | 
  352 |     // Wait for the room header to load
  353 |     await expect(page.locator('.mx_RoomHeader, [data-testid="room-header"]').first())
  354 |       .toBeVisible({ timeout: 20_000 });
  355 | 
  356 |     // Open the member list via the room info button or member count
  357 |     const memberButton = page
  358 |       .getByRole('button', { name: /\d+ member/i })
  359 |       .or(page.locator('[aria-label*="member" i]').first());
  360 |     if (await memberButton.isVisible({ timeout: 5_000 }).catch(() => false)) {
  361 |       await memberButton.click();
  362 |     } else {
  363 |       await page.getByRole('button', { name: /room info/i }).first().click().catch(() => {});
  364 |     }
  365 | 
  366 |     await expect(
  367 |       page.locator('.mx_MemberList, [data-testid="member-list"]').first()
  368 |         .or(page.locator('[class*="MemberList"], [class*="memberList"]').first()),
  369 |     ).toBeVisible({ timeout: 15_000 });
  370 | 
  371 |     await expect(
  372 |       page.getByText(/alex/i, { exact: false }).first()
  373 |         .or(page.locator('[class*="memberInfo"], [class*="MemberInfo"]').first()),
  374 |     ).toBeVisible({ timeout: 10_000 });
  375 |   });
  376 | 
  377 |   // ── Story 5-3: POST /rooms/{roomId}/read_markers ──────────────────────────
  378 | 
  379 |   test('No read_markers retry loop when entering room (Story 5-3: POST /read_markers)', async ({ page }) => {
  380 |     const session = await loginViaOidc(page, 'alex@example.com', 'changeme');
  381 | 
  382 |     const createResp = await page.request.post(`${ELEMENT_URL}/_matrix/client/v3/createRoom`, {
  383 |       headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
```