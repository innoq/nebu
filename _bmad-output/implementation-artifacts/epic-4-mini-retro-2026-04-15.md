# Epic 4 Mini-Retrospective — Stories 4-24 to 4-28

**Date:** 2026-04-15
**Scope:** Post-retro stories: 4-24 (Element Web E2E), 4-25 (GET /filter), 4-26 (GET /members), 4-27 (POST /read_markers), 4-28 (E2E gap coverage)
**Facilitator:** Bob (Scrum Master)
**Project Lead:** Phil

---

## Epic 4 Action Item Status

| # | Item | Status | Result |
|---|---|---|---|
| A1 | FluffyChat Happy-Path Smoke Test | ✅ Done | Story 4-24 (pivoted to Element Web) ran the test; console log revealed 3 missing endpoints |
| A2 | FluffyChat Findings → Story Backlog | ✅ Done | Console log analysis → Stories 4-25, 4-26, 4-27 created and implemented |
| A3 | ETS NebuTxnDedup TTL Pruning | ⏳ Carry-Over | Still open → Epic 5 sprint planning |
| A4 | Test: Maps > 32 Keys for Jason.OrderedObject | ⏳ Carry-Over | Planned for Story 5-6 ACs |

---

## Story Metrics

| Story | Finding Type | Count | Fixed |
|---|---|---|---|
| 4-24 (Element Web E2E) | MAJOR: marie login used alex credentials | 1 | ✅ |
| 4-24 | MINOR: unpinned Docker :latest tag, missing Makefile targets, nginx resolver | 3 | ✅ |
| 4-25 to 4-28 | MINOR: serverName missing in read_markers_test.go | 1 | ✅ |
| **Total** | **MAJOR** | **1** | ✅ all fixed |

---

## Successes

### 1. Real Client as Diagnostic Tool
The Element Web browser console log identified three missing endpoints within minutes:
- `GET /filter/0` → sync ERROR loop on every page reload (critical blocker)
- `GET /rooms/{roomId}/members` → empty member list
- `POST /rooms/{roomId}/read_markers` → retry storm (hundreds of console errors)

This validates Epic 4's key insight: *happy-path testing with a real client is a quality gate, not an optional extra.*

### 2. ATDD Gate Held Cleanly
All three new handlers (filter.go, members.go, read_markers.go) followed the failing-test-first discipline. Compile errors were exactly the expected missing symbols. 14 new tests, all green after implementation.

### 3. Element Web Test Suite as Gap-Analysis Reference
141 Element Web Playwright specs used as a reference — not copied, but used to identify structural gaps. Adapted three test patterns (stored-credentials, invite-dialog, read-receipts) into Nebu-specific browser E2E tests (Story 4-28).

### 4. Pre-existing Test Rot Surfaced and Fixed
A compile error in rooms_test.go (missing `InviteUser` on mock) had hidden all `internal/matrix` package tests silently. Four related bugs were found and fixed:
- `stream_test.go`: `LeaveRoom` missing on grpc mock
- `login_test.go`: stale user_id assertion
- All matrix test helpers: missing `serverName "test.local"` in JWTMiddleware calls
- `signJWT`: missing `"name"` claim causing non-deterministic user IDs

---

## Challenges

### 1. Multi-User E2E: Credential Bug
`performSsoLogin` hardcoded `alex@example.com` — in the multi-user test (Test 5), marie was logged in as alex. The test passed despite the bug (alex invited himself). **Only the adversarial Opus code review caught this as MAJOR.**

**Root cause:** The helper was built for single-user flows, then reused for multi-user without parameterizing credentials.

**Fix:** Added optional `email` / `password` parameters to `performSsoLogin`, updated the marie call to `performSsoLogin(mariePage, 'marie@example.com', 'changeme')`.

### 2. Story Numbering Inconsistency
During the session, new endpoints were called "Story 5-1/5-2/5-3" in conversation, but registered as "Story 4-25/4-26/4-27" in sprint-status. Test file comments still reference the old numbers. Low-priority cleanup item.

### 3. Unpinned Docker Image Tags
`FROM vectorim/element-web` without a version tag pulled `:latest`. Pinned to `v1.12.15` during code review. This pattern likely exists in other Dockerfiles — latent non-reproducibility risk.

---

## Key Insights

1. **Console-log-driven development is legitimate for endpoint discovery.** A real browser, open console, check for 404s — this surfaces missing endpoints faster than spec analysis. For future endpoint epics: start with a real client before writing stories.

2. **Compile errors in test packages create invisible test drift.** When a package fails to compile, all its tests are invisible to CI. Test rot can accumulate across many commits silently. Rule: compile errors in test packages are immediate — no deferred.

3. **Element Web is the better MVP smoke-test client.** `FROM vectorim/element-web` = 5s build. FluffyChat = Rust + Flutter + WASM stubs = complex multi-stage build. Same diagnostic value, drastically less friction.

4. **Opus adversarial review catches semantic bugs that unit tests cannot.** The marie/alex credential bug was not caught by unit tests, TEA Gate 1, or TEA Gate 2 — only by adversarial code review treating multi-user test semantics as a first-class concern.

---

## Action Items for Epic 5

| # | Item | Owner | Priority |
|---|---|---|---|
| B1 | Harmonize story number references in test comments (5-x → 4-2x) | Phil (opportunistic) | LOW |
| B2 | Audit all Dockerfiles for unpinned `:latest` tags — pin to semver | Dev (next Docker touch) | MEDIUM |
| B3 | Document `performSsoLogin(page, email, password)` credential-parameterization as project-wide pattern for multi-user E2E tests | Bob → CLAUDE.md addition | MEDIUM |
| B4 | ETS NebuTxnDedup TTL pruning (carry-over A3) | Amelia, Epic 5 sprint planning | HIGH |
| B5 | Test: maps > 32 keys for Jason.OrderedObject (carry-over A4) | Murat, Story 5-6 ACs | HIGH |

---

## Epic 5 Preparation Notes

Epic 5 (Compliance) is structurally different from Epic 4:
- No new Matrix client endpoints — audit infrastructure
- PostgreSQL-heavy (audit log, retention, RLS)
- Regulatory requirements: four-eyes principle for compliance access requests must be explicit ACs, not implied

**TEA requirements for Epic 5:**
- Audit Log Writer (Story 5-2): mandatory crash/restart test — atomic guarantee must survive process kill
- Four-Eyes Approval (Story 5-4): needs two-user integration scenario (requestor + approver)
- DSGVO Deletion (Story 5-7): atomicity test — verify deletion fails cleanly on partial error, no partial state

**Carry-over technical debt that may affect Epic 5:**
- ETS NebuTxnDedup unbounded growth (A3/B4) — not a blocker for Epic 5 compliance stories but a background risk
- `Jason.OrderedObject` maps > 32 keys (A4/B5) — directly relevant to Story 5-6 (compliance export)
