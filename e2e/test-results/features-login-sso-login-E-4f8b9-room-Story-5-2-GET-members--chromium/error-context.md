# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: features/login/sso-login.spec.ts >> Element Web — New endpoint smoke tests (Stories 5-1/5-2/5-3) >> Member list populated after joining room (Story 5-2: GET /members)
- Location: tests/features/login/sso-login.spec.ts:337:7

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: locator('.mx_MemberList, [data-testid="member-list"]').first().or(locator('[class*="MemberList"], [class*="memberList"]').first())
Expected: visible
Timeout: 15000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" with timeout 15000ms
  - waiting for locator('.mx_MemberList, [data-testid="member-list"]').first().or(locator('[class*="MemberList"], [class*="memberList"]').first())

```

# Page snapshot

```yaml
- generic [ref=e1]:
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
            - option "Open room members-test-room" [active] [selected] [ref=e89] [cursor=pointer]:
              - generic [ref=e90]:
                - text: m
                - generic [ref=e91]:
                  - generic "members-test-room" [ref=e93]
                  - generic [ref=e94]:
                    - button "More Options" [ref=e95]:
                      - img [ref=e97]
                    - button "Notification options" [ref=e99]:
                      - img [ref=e101]
            - option "Open room invite-test-1777492065657 invitation." [ref=e104] [cursor=pointer]:
              - generic [ref=e105]:
                - text: i
                - generic [ref=e106]:
                  - generic "invite-test-1777492065657" [ref=e108]
                  - img [ref=e111]
            - option "Open room invite-test-1777491301724 invitation." [ref=e114] [cursor=pointer]:
              - generic [ref=e115]:
                - text: i
                - generic [ref=e116]:
                  - generic "invite-test-1777491301724" [ref=e118]
                  - img [ref=e121]
            - option "Open room invite-test-1777490464926 invitation." [ref=e124] [cursor=pointer]:
              - generic [ref=e125]:
                - text: i
                - generic [ref=e126]:
                  - generic "invite-test-1777490464926" [ref=e128]
                  - img [ref=e131]
            - option "Open room e2e-multiuser" [ref=e134] [cursor=pointer]:
              - generic [ref=e135]:
                - text: e
                - generic "e2e-multiuser" [ref=e138]
            - option "Open room reconnect-test-room" [ref=e144] [cursor=pointer]:
              - generic [ref=e145]:
                - text: r
                - generic "reconnect-test-room" [ref=e148]
            - 'option "Open room DM: Marie ↔ Alex" [ref=e150] [cursor=pointer]':
              - generic [ref=e151]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e154]'
            - option "Open room nav-b-1777492276562" [ref=e160] [cursor=pointer]:
              - generic [ref=e161]:
                - text: "n"
                - generic "nav-b-1777492276562" [ref=e164]
            - option "Open room nav-a-1777492276562" [ref=e166] [cursor=pointer]:
              - generic [ref=e167]:
                - text: "n"
                - generic "nav-a-1777492276562" [ref=e170]
            - option "Open room create-test-1777492137282" [ref=e172] [cursor=pointer]:
              - generic [ref=e173]:
                - text: c
                - generic "create-test-1777492137282" [ref=e176]
            - option "Open room @marie:localhost" [ref=e178] [cursor=pointer]:
              - generic [ref=e179]:
                - text: m
                - generic "@marie:localhost" [ref=e182]
            - option "Open room Empty room" [ref=e188] [cursor=pointer]:
              - generic [ref=e189]:
                - text: E
                - generic "Empty room" [ref=e192]
            - option "Open room read-markers-test-room" [ref=e194] [cursor=pointer]:
              - generic [ref=e195]:
                - text: r
                - generic "read-markers-test-room" [ref=e198]
            - option "Open room members-test-room" [ref=e200] [cursor=pointer]:
              - generic [ref=e201]:
                - text: m
                - generic "members-test-room" [ref=e204]
            - option "Open room e2e-multiuser" [ref=e206] [cursor=pointer]:
              - generic [ref=e207]:
                - text: e
                - generic "e2e-multiuser" [ref=e210]
            - option "Open room reconnect-test-room" [ref=e216] [cursor=pointer]:
              - generic [ref=e217]:
                - text: r
                - generic "reconnect-test-room" [ref=e220]
            - 'option "Open room DM: Marie ↔ Alex" [ref=e222] [cursor=pointer]':
              - generic [ref=e223]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e226]'
            - option "Open room nav-b-1777491502011" [ref=e232] [cursor=pointer]:
              - generic [ref=e233]:
                - text: "n"
                - generic "nav-b-1777491502011" [ref=e236]
            - option "Open room nav-a-1777491502011" [ref=e238] [cursor=pointer]:
              - generic [ref=e239]:
                - text: "n"
                - generic "nav-a-1777491502011" [ref=e242]
            - option "Open room create-test-1777491373308" [ref=e244] [cursor=pointer]:
              - generic [ref=e245]:
                - text: c
                - generic "create-test-1777491373308" [ref=e248]
            - option "Open room @marie:localhost" [ref=e250] [cursor=pointer]:
              - generic [ref=e251]:
                - text: m
                - generic "@marie:localhost" [ref=e254]
            - option "Open room Empty room" [ref=e260] [cursor=pointer]:
              - generic [ref=e261]:
                - text: E
                - generic "Empty room" [ref=e264]
            - option "Open room read-markers-test-room" [ref=e266] [cursor=pointer]:
              - generic [ref=e267]:
                - text: r
                - generic "read-markers-test-room" [ref=e270]
            - option "Open room members-test-room" [ref=e272] [cursor=pointer]:
              - generic [ref=e273]:
                - text: m
                - generic "members-test-room" [ref=e276]
            - option "Open room e2e-multiuser" [ref=e278] [cursor=pointer]:
              - generic [ref=e279]:
                - text: e
                - generic "e2e-multiuser" [ref=e282]
            - option "Open room reconnect-test-room" [ref=e288] [cursor=pointer]:
              - generic [ref=e289]:
                - text: r
                - generic "reconnect-test-room" [ref=e292]
            - 'option "Open room DM: Marie ↔ Alex" [ref=e294] [cursor=pointer]':
              - generic [ref=e295]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e298]'
            - 'option "Open room DM: Marie ↔ Alex" [ref=e304] [cursor=pointer]':
              - generic [ref=e305]:
                - text: D
                - 'generic "DM: Marie ↔ Alex" [ref=e308]'
            - option "Open room Empty room" [ref=e314] [cursor=pointer]:
              - generic [ref=e315]:
                - text: E
                - generic "Empty room" [ref=e318]
            - option "Open room Empty room" [ref=e320] [cursor=pointer]:
              - generic [ref=e321]:
                - text: E
                - generic "Empty room" [ref=e324]
            - option "Open room Empty room" [ref=e326] [cursor=pointer]:
              - generic [ref=e327]:
                - text: E
                - generic "Empty room" [ref=e330]
            - option "Open room Empty room" [ref=e332] [cursor=pointer]:
              - generic [ref=e333]:
                - text: E
                - generic "Empty room" [ref=e336]
            - option "Open room Empty room" [ref=e338] [cursor=pointer]:
              - generic [ref=e339]:
                - text: E
                - generic "Empty room" [ref=e342]
      - generic [ref=e348]:
        - banner [ref=e349]:
          - button "Open room settings" [ref=e350] [cursor=pointer]: m
          - button "Room info" [ref=e351] [cursor=pointer]:
            - heading "members-test-room" [level=1] [ref=e353]:
              - generic [ref=e354]: members-test-room
          - button "Video call" [ref=e355] [cursor=pointer]:
            - img "Video call" [ref=e357]
          - button "Voice call" [ref=e359] [cursor=pointer]:
            - img "Voice call" [ref=e361]
          - button "Threads" [ref=e363] [cursor=pointer]:
            - img [ref=e365]
          - button "Room info" [ref=e367] [cursor=pointer]:
            - img [ref=e369]
          - button "People" [ref=e372] [cursor=pointer]:
            - generic [ref=e373]: a
            - text: "1"
        - region
        - main [ref=e374]:
          - list [ref=e377]:
            - listitem
        - region "Room status bar"
        - region "Message composer" [ref=e380]:
          - generic [ref=e382]:
            - img "Messages in this room are not end-to-end encrypted" [ref=e384]
            - textbox "Send an unencrypted message…" [ref=e388]:
              - generic [ref=e389]: Send an unencrypted message…
            - generic [ref=e390]:
              - button "Emoji" [ref=e391] [cursor=pointer]:
                - img [ref=e392]
              - button "Attachment" [ref=e395] [cursor=pointer]:
                - img [ref=e396]
              - button "More options" [ref=e398] [cursor=pointer]:
                - img [ref=e399]
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
      - generic: Video call
  - generic:
    - generic:
      - img
      - generic: Voice call
  - generic:
    - generic:
      - img
      - generic: Threads
  - generic:
    - generic:
      - img
      - generic: Room info
  - generic:
    - generic:
      - img
      - generic: People
  - img [ref=e403]
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
      - generic: Messages in this room are not end-to-end encrypted
```

# Test source

```ts
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
  283 |       ).toBeVisible({ timeout: 20_000 });
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
> 369 |     ).toBeVisible({ timeout: 15_000 });
      |       ^ Error: expect(locator).toBeVisible() failed
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
  384 |       data: { name: 'read-markers-test-room', visibility: 'private' },
  385 |     });
  386 |     expect(createResp.status()).toBe(200);
  387 |     const { room_id: roomId } = await createResp.json();
  388 | 
  389 |     await page.request.put(
  390 |       `${ELEMENT_URL}/_matrix/client/v3/rooms/${encodeURIComponent(roomId)}/send/m.room.message/txn-rm-1`,
  391 |       {
  392 |         headers: { Authorization: `Bearer ${session.accessToken}`, 'Content-Type': 'application/json' },
  393 |         data: { msgtype: 'm.text', body: 'test message for read markers' },
  394 |       },
  395 |     );
  396 | 
  397 |     // Collect errors — read_markers retry produces repeated "Error sending fully_read"
  398 |     const readMarkerErrors: string[] = [];
  399 |     page.on('console', (msg) => {
  400 |       if (msg.text().includes('fully_read') || msg.text().includes('read_markers')) {
  401 |         readMarkerErrors.push(`[${msg.type()}] ${msg.text()}`);
  402 |       }
  403 |     });
  404 | 
  405 |     // Navigate to the room — Element will POST /read_markers on entry
  406 |     await page.goto(`${ELEMENT_URL}/#/room/${roomId}`);
  407 |     await dismissKeyDialog(page);
  408 | 
  409 |     // Wait for timeline to render
  410 |     await expect(page.locator('.mx_RoomView_timeline, [class*="timeline"]').first())
  411 |       .toBeVisible({ timeout: 20_000 });
  412 | 
  413 |     // Negative assertion: we need to observe that NO read_markers error appears over a window of time.
  414 |     // There is no DOM element or event to await — the absence of a retry storm is the signal.
  415 |     // A fixed 5 s wait is the pragmatic approach here (TEA-reviewed, INFO-1 in test-review).
  416 |     await page.waitForTimeout(5_000);
  417 | 
  418 |     const errors = readMarkerErrors.filter(m => m.toLowerCase().includes('error'));
  419 |     expect(errors.length, `read_markers retry storm detected:\n${errors.join('\n')}`).toBe(0);
  420 |   });
  421 | });
  422 | 
```