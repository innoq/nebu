# Test Design Document — Matrix Client Communication Features
## Nebu Chat Server · Epic 4+ Smoke Coverage

**Test Architect:** Murat  
**Date:** 2026-04-14  
**Scope:** Fundamental Matrix client communication features  
**Risk Threshold:** P1 (all P0 + P1 required before Epic 5 starts)

---

## 1. Risk Assessment Summary

| Risk Category | Items | Max Priority |
|---|---|---|
| Authentication & Session | SSO flow, token expiry, multi-user | P0 |
| Core Messaging | Send, receive, persist | P0 |
| Room Management | Create, join, invite, leave | P0 |
| User Discovery | Search, directory | P1 |
| Real-Time Features | Typing, receipts, presence | P1 |
| UX / Visibility | Room list, message history | P1 |
| Blocked (MVP gaps) | Profile display name, decline invite | P2/Blocked |

**Murat's risk calculation:**  
Message delivery is the core value proposition — any failure here is P0.  
Room management (invite/join/leave) is the social graph — failures cascade to messaging, so P0.  
Real-time indicators are UX polish — broken typing/receipts don't block communication, P1.

---

## 2. Test File Structure

```
e2e/tests/
├── element_e2e.spec.ts          ← existing (5 tests: SSO, create room, send/receive, user search, multi-user)
├── matrix_api.spec.ts           ← NEW: API-level contract tests (no browser, fast)
├── matrix_multiuser.spec.ts     ← NEW: Multi-user E2E scenarios (Element Web UI)
└── helpers/
    ├── sso-login.ts             ← extract performSsoLogin() shared helper
    ├── api-client.ts            ← typed Matrix API client (page.request wrapper)
    └── test-users.ts            ← user constants + bulk login setup
```

**Rationale:** Split by concern —
- `matrix_api.spec.ts` runs in < 30s (CI fast path, no browser startup)
- `matrix_multiuser.spec.ts` runs in 2-5 min (CI slow path, requires e2e profile)
- `element_e2e.spec.ts` keeps existing UI smoke tests

---

## 3. Test Scenarios — Full Inventory

### Legend
- **Level:** API = Matrix HTTP only, E2E = Element Web browser
- **Priority:** P0 = blocking, P1 = high, P2 = nice-to-have
- **Status:** ✅ Implemented | 🔧 To implement | ⛔ Blocked (server gap)

---

### BLOCK A — Authentication & Session (P0)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| A-01 | SSO login alex → user_id = @alex:localhost | E2E | P0 | ✅ | user_id matches, home screen visible |
| A-02 | SSO login marie → user_id = @marie:localhost | API | P0 | ✅ | 200, correct user_id |
| A-03 | SSO login tom → user_id = @tom:localhost | API | P0 | 🔧 | 200, correct user_id |
| A-04 | Expired opaque loginToken → 401 on POST /login | API | P0 | 🔧 | 401 M_FORBIDDEN after TTL |
| A-05 | POST /logout invalidates token | API | P0 | 🔧 | subsequent /sync returns 401 |
| A-06 | Concurrent sessions: alex logged in twice | API | P1 | 🔧 | both tokens valid, each gets sync |

---

### BLOCK B — User Discovery (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| B-01 | Search "marie" → returns @marie:localhost | API | P1 | ✅ | results array contains marie |
| B-02 | Search "tom" → returns @tom:localhost | API | P1 | 🔧 | results array contains tom |
| B-03 | Search unknown term → empty results | API | P1 | 🔧 | results=[], limited=false |
| B-04 | Search respects limit param | API | P2 | 🔧 | limit=1 → at most 1 result |
| B-05 | Element UI: type in "Start chat" search → marie appears | E2E | P1 | 🔧 | marie card visible in search results |

---

### BLOCK C — Direct Message / 1:1 Room (P0)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| C-01 | alex creates DM room with marie (preset: private_chat, invite: [marie]) | API | P0 | ✅ | room_id returned, marie in invite state |
| C-02 | marie sees invite in sync rooms.invite | API | P0 | ✅ | rooms.invite contains room_id |
| C-03 | marie accepts invite (POST /join/{roomId}) | API | P0 | ✅ | 200, room_id in response |
| C-04 | alex sends "Hey Marie!" to DM room | API | P0 | ✅ | event_id returned |
| C-05 | marie reads message via GET /messages | API | P0 | ✅ | chunk contains body="Hey Marie!" |
| C-06 | marie replies "Hey Alex!" | API | P0 | ✅ | event_id returned |
| C-07 | alex reads marie's reply | API | P0 | ✅ | chunk contains body="Hey Alex!" |
| C-08 | **Element UI: marie sees invite card** | E2E | P0 | 🔧 | invite card with Accept button visible |
| C-09 | **Element UI: marie clicks Accept** | E2E | P0 | 🔧 | room view opens, no error |
| C-10 | **Element UI: alex sends message, marie sees it** | E2E | P0 | 🔧 | message text visible in marie's timeline |

---

### BLOCK D — Private Group Room (P0)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| D-01 | alex creates private group (invite: [marie, tom]) | API | P0 | 🔧 | room_id, both invited |
| D-02 | marie and tom each see invite in sync | API | P0 | 🔧 | rooms.invite for both users |
| D-03 | marie joins | API | P0 | ✅ | 200 |
| D-04 | tom joins | API | P0 | 🔧 | 200 |
| D-05 | alex, marie, tom all send messages | API | P0 | ✅ | 3 event_ids |
| D-06 | GET /messages returns all 3 messages for any member | API | P0 | ✅ | chunk.length >= 3 |
| D-07 | Non-member (kai) cannot read messages | API | P0 | 🔧 | 403 M_FORBIDDEN |
| D-08 | **Element UI: 3-user chat all visible** | E2E | P1 | 🔧 | all 3 messages in timeline |

---

### BLOCK E — Public Room (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| E-01 | alex creates public room (visibility: public, preset: public_chat) | API | P1 | ✅ | room_id returned |
| E-02 | marie joins without invite | API | P1 | ✅ | 200 |
| E-03 | tom joins without invite | API | P1 | 🔧 | 200 |
| E-04 | All 3 exchange messages, each reads others | API | P1 | ✅ | messages visible across users |
| E-05 | Unauthenticated user cannot read messages | API | P1 | 🔧 | 401 M_MISSING_TOKEN |
| E-06 | **Element UI: alex creates public room via "Explore public rooms"** | E2E | P1 | 🔧 | room appears, join works |

---

### BLOCK F — Room List / Sidebar Visibility (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| F-01 | Joined rooms appear in /sync rooms.join | API | P1 | ✅ | joined rooms in sync response |
| F-02 | Pending invites appear in /sync rooms.invite | API | P0 | ✅ | invite rooms in sync response |
| F-03 | Left rooms appear in /sync rooms.leave | API | P1 | 🔧 | left rooms tracked |
| F-04 | **Element UI: sidebar shows all joined rooms** | E2E | P1 | ✅ | rooms visible in left panel |
| F-05 | **Element UI: invite card shown for pending invite** | E2E | P0 | 🔧 | accept/decline buttons visible |

---

### BLOCK G — Message History & Pagination (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| G-01 | GET /messages?dir=b&limit=5 returns up to 5 | API | P1 | ✅ | chunk.length <= 5 |
| G-02 | GET /messages with pagination token returns next page | API | P1 | 🔧 | start token from previous end |
| G-03 | Empty room returns empty chunk | API | P1 | 🔧 | chunk=[] |
| G-04 | Messages returned oldest-first (dir=f) | API | P1 | 🔧 | origin_server_ts ascending |
| G-05 | **Element UI: scroll up loads older messages** | E2E | P2 | 🔧 | older messages appear on scroll |

---

### BLOCK H — Typing Indicators (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| H-01 | alex sends PUT /typing with typing=true | API | P1 | 🔧 | 200 |
| H-02 | alex sends PUT /typing with typing=false | API | P1 | 🔧 | 200 |
| H-03 | **Element UI: alex types → marie sees "alex is typing..."** | E2E | P1 | 🔧 | typing indicator appears in marie's UI |
| H-04 | Typing indicator disappears after timeout | E2E | P2 | 🔧 | indicator gone within 30s |

---

### BLOCK I — Read Receipts (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| I-01 | marie POST /receipt/m.read/{eventId} | API | P1 | 🔧 | 200 |
| I-02 | Receipt in sync ephemeral events | API | P1 | 🔧 | receipt event in sync |
| I-03 | **Element UI: message shows read receipt checkmark** | E2E | P2 | 🔧 | ✓ visible after marie reads |

---

### BLOCK J — Presence (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| J-01 | alex SET presence=online | API | P1 | 🔧 | 200 |
| J-02 | GET /presence/@marie:localhost → returns status | API | P1 | 🔧 | 200, presence_state in response |
| J-03 | **Element UI: online indicator visible** | E2E | P2 | 🔧 | green dot next to user avatar |

---

### BLOCK K — Leave Room / Decline Invite (P1)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| K-01 | alex leaves room via POST /leave/{roomId} | API | P1 | 🔧 | 200 |
| K-02 | Left room no longer in sync rooms.join | API | P1 | 🔧 | room absent from next sync |
| K-03 | Rejoining a left room | API | P1 | 🔧 | join succeeds, history preserved |
| K-04 | tom declines invite (POST /leave on invited room) | API | P1 | 🔧 | 200, room in rooms.leave |
| K-05 | **Element UI: click Leave room** | E2E | P2 | 🔧 | room removed from sidebar |

---

### BLOCK L — Profile Display Name (P2 — Blocked Story 7-15)

| ID | Scenario | Level | Priority | Status | AC |
|---|---|---|---|---|---|
| L-01 | PUT /profile/{userId}/displayname sets name | API | P2 | ⛔ | 200, GET /profile returns name |
| L-02 | Display name visible in room member list | E2E | P2 | ⛔ | "alex" shown, not @alex:localhost |
| L-03 | Display name from JWT name claim on login | API | P2 | ✅ | ValidateToken stores displayname |

*Blocked: Story 7-15 (Bootstrap claim mapping). Profile GET reads from profiles table, but display name is in users table. Bridge not yet implemented.*

---

## 4. Test Implementation Plan

### Phase 1 — API Contract Tests (matrix_api.spec.ts) — HIGH VALUE, LOW EFFORT

All P0/P1 API-level tests run headless in ~30 seconds. No Element Web sidecar needed.

**New file: `e2e/tests/matrix_api.spec.ts`**

```typescript
// Pattern: shared login helper returns tokens for all test users
// Setup: all 3 users log in once in beforeAll, tokens reused
// Cleanup: leave all created rooms in afterAll

test.describe('Matrix API — Communication Contract Tests', () => {
  let tokens: Record<string, string>;  // {alex, marie, tom}
  let rooms: string[];                 // cleanup tracking

  test.beforeAll(async ({ request }) => {
    tokens = await loginAllUsers(request);
  });

  // Block B: User Discovery
  // Block C: DM flow (API)
  // Block D: Group room (API)
  // Block E: Public room (API)
  // Block G: Pagination
  // Block H: Typing API
  // Block I: Receipts API
  // Block J: Presence API
  // Block K: Leave/Decline API
});
```

**Estimated implementation time:** 2-3 stories  
**Coverage gain:** 25+ test cases, all P0/P1 API scenarios

---

### Phase 2 — UI Acceptance Tests (matrix_multiuser.spec.ts) — VALIDATES REAL CLIENT

P0 E2E scenarios that require the Element Web browser. These confirm the full user journey.

**New file: `e2e/tests/matrix_multiuser.spec.ts`**

```typescript
// Pattern: 2-browser-context tests (alex in page1, marie in page2)
// Each context: performSsoLogin() → storageState persisted
// Critical: invite acceptance (C-08/C-09) — the "stuck on joining" bug

test.describe('Matrix Multi-User — Element Web E2E', () => {
  test('DM: marie accepts invite and both see messages', async ({ browser }) => {
    const alexCtx = await browser.newContext();
    const marieCtx = await browser.newContext();
    // alex creates room → marie gets invite card → clicks Accept → messages exchange
  });

  test('Public room: tom joins without invite', async ({ browser }) => {
    // alex creates public room → tom navigates to room list → joins → sends message
  });
});
```

**Estimated implementation time:** 3-4 stories  
**Coverage gain:** C-08 to C-10 (the currently failing invite acceptance)

---

### Phase 3 — Real-Time Feature Tests (matrix_realtime.spec.ts) — P1 POLISH

Typing indicators, read receipts, presence. Require timing-aware assertions.

**Estimated implementation time:** 1-2 stories  
**Coverage gain:** H, I, J blocks

---

## 5. Server-Side Gaps to Fix First

Before Phase 2 tests will pass, these server gaps must be resolved:

### GAP-1: Invite Accept "stuck on joining" (BLOCKING C-08/C-09) 🔴
- **Symptom:** Element Web shows "Joining…" spinner indefinitely when accepting invite
- **Root cause:** `rooms.invite` now shows invites correctly (fixed), but after accepting (POST /join), the **incremental sync** (`GetSyncDelta`) does not return the newly-joined room in `rooms.join`
- **Fix needed:** `GetSyncDelta` must detect that the user joined a room since the last sync token and include it in `rooms.join` of the delta response
- **Test to write first (ATDD):** `GET /sync?since=<token>` after POST /join returns the room in `rooms.join`

### GAP-2: Room state events not sent on createRoom (BLOCKING room names)
- **Symptom:** Rooms show as "Empty room" in Element
- **Root cause:** `createRoom` in Elixir core doesn't send `m.room.name` state event
- **Fix needed:** After creating room, send `m.room.name` state event with the `name` field
- **Impact:** P2 UX — doesn't block core messaging, but affects discoverability

### GAP-3: GET /sync rooms.leave not populated
- **Symptom:** Left rooms don't appear in `rooms.leave`
- **Root cause:** `room_members.left_at` is set on leave, but sync delta doesn't return left rooms
- **Fix needed:** Include rooms where `left_at` is between last sync and now in `rooms.leave`

### GAP-4: GET /profile/{userId} returns M_NOT_FOUND
- **Symptom:** Profile display name not visible
- **Root cause:** Profile handler reads from `profiles` table; display name is in `users.display_name_encrypted`
- **Fix needed:** Story 7-15 (Bootstrap claim mapping)

---

## 6. Recommended ATDD First Tests (Write Before Implementing)

Per our TDD standard, write these failing tests BEFORE fixing the server gaps:

### TDD-1 (for GAP-1) — Incremental sync includes newly joined room
```
GIVEN alex created a room and invited marie
WHEN marie calls POST /join/{roomId}
AND marie calls GET /sync?since=<previous_token>&timeout=0
THEN rooms.join contains {roomId}
AND rooms.invite does NOT contain {roomId}
```

### TDD-2 (for GAP-2) — Room name state event on createRoom
```
GIVEN alex calls POST /createRoom with name="My Room"
WHEN alex calls GET /sync?timeout=0
THEN rooms.join[roomId].state.events contains
  {type: "m.room.name", content: {name: "My Room"}}
```

### TDD-3 (for GAP-3) — Left room in sync delta
```
GIVEN alex is in a room
WHEN alex calls POST /rooms/{roomId}/leave
AND alex calls GET /sync?since=<previous_token>&timeout=0
THEN rooms.leave contains {roomId}
AND rooms.join does NOT contain {roomId}
```

---

## 7. CI Integration

```
# Fast path (every PR): ~30s
make test-unit-go      # unit tests
npx playwright test tests/matrix_api.spec.ts   # API contracts (no browser)

# Slow path (merge to main): ~5-10 min
docker compose --profile e2e up -d --wait
npx playwright test tests/element_e2e.spec.ts tests/matrix_multiuser.spec.ts

# Full path (pre-release): ~15 min
npx playwright test   # all test files
```

---

## 8. Coverage Summary

| Block | Scenarios | Implemented | To Build | Blocked |
|---|---|---|---|---|
| A — Auth | 6 | 2 | 4 | 0 |
| B — User Search | 5 | 1 | 3 | 1 (E2E) |
| C — DM | 10 | 7 | 3 | 0 |
| D — Private Group | 8 | 2 | 6 | 0 |
| E — Public Room | 6 | 3 | 3 | 0 |
| F — Room List | 5 | 3 | 2 | 0 |
| G — History | 5 | 1 | 4 | 0 |
| H — Typing | 4 | 0 | 4 | 0 |
| I — Receipts | 3 | 0 | 3 | 0 |
| J — Presence | 3 | 0 | 3 | 0 |
| K — Leave | 5 | 0 | 5 | 0 |
| L — Profile | 3 | 1 | 0 | 2 |
| **Total** | **63** | **20 (32%)** | **40 (63%)** | **3 (5%)** |

**P0 coverage today: ~60%** (core messaging + auth works at API level)  
**P0 coverage target: 100%** before Epic 5

---

*Document generated by Murat (Test Architect) · 2026-04-14*
