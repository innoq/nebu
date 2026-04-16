---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04b-subagent-e2e-failing']
lastStep: 'step-04b-subagent-e2e-failing'
lastSaved: '2026-04-15'
story: '4-29'
tddPhase: 'RED'
inputDocuments:
  - '_bmad-output/implementation-artifacts/4-29-element-web-playwright-oidc-fixture-mvp-test-adaptation.md'
  - 'e2e/tests/element_e2e.spec.ts'
  - 'e2e/playwright.config.ts'
---

# ATDD Checklist — Story 4-29

## Step 1: Preflight & Context

**Stack detection:** `fullstack` (Go gateway + Elixir core + Playwright E2E)
**Test framework:** `@playwright/test` ^1.44.0
**Test directory:** `e2e/tests/` (recursive, covers `features/` subdirectories)
**Generation mode:** AI generation (no browser recording — selectors documented in story Dev Notes)

**Story:** Element Web Playwright — OIDC Multi-User Fixture & MVP Test Adaptation

**Acceptance Criteria:**
- AC 1: Feature-based folder structure + shared fixtures (oidc.ts, helpers.ts)
- AC 2: room-lifecycle.spec.ts — create, navigation, leave (regression guard)
- AC 3: invites.spec.ts — invite rendering and decline
- AC 4: messages.spec.ts — send (UI) and receive (via sync)
- AC 5: /bmad-tea mandatory gate before implementation (process gate, no test file)
- AC 6: All tests pass cleanly (locale-agnostic selectors, auto-skip guards)
- AC 7: Bug report section after tests pass

---

## Step 2: Generation Mode

**Mode:** AI generation (no Playwright CLI / MCP browser recording needed)
**Reason:** Selectors are fully documented in story Dev Notes from live Element Web inspection (2026-04-15).
Known DOM structure:
- Sidebar: `page.getByRole('option', { name: /open room|öffne den chat/i })`
- Timeline: `.mx_EventTile`
- Compose box: `[contenteditable="true"][data-testid="message-composer-input"]`

---

## Step 3: Test Strategy

### AC 1 — Fixture Extraction
| Criterion | Test | Level | Priority | Failing Before |
|---|---|---|---|---|
| `oidc.ts` exports `loginViaOidc` | `sso-login.spec.ts#loginViaOidc returns accessToken` | E2E | P0 | `loginViaOidc` throws until Task 1 |
| `helpers.ts` exports three helpers | Any spec's `beforeAll` | E2E | P0 | `isElementReachable` throws until Task 1 |

### AC 2 — Room Lifecycle
| Criterion | Test | Level | Priority | Failing Before |
|---|---|---|---|---|
| Create room → sidebar grows | `room-lifecycle.spec.ts#Create room via API → reload → sidebar` | E2E | P0 | Task 3 |
| Leave → sidebar shrinks within 10 s | `room-lifecycle.spec.ts#Leave room → sidebar decreases within 10 s` | E2E | P0 | Task 3 |
| Navigate 2 rooms → compose renders | `room-lifecycle.spec.ts#Navigate between 2 rooms → timeline renders` | E2E | P1 | Task 3 |

### AC 3 — Invites
| Criterion | Test | Level | Priority | Failing Before |
|---|---|---|---|---|
| Invite appears in sidebar within 20 s | `invites.spec.ts#Invite appears in sidebar` | E2E | P0 | Task 4 |
| Decline → invite gone within 10 s | `invites.spec.ts#Decline invite → tile gone within 10 s` | E2E | P0 | Task 4 |

### AC 4 — Messages
| Criterion | Test | Level | Priority | Failing Before |
|---|---|---|---|---|
| Send → appears in `.mx_EventTile` | `messages.spec.ts#Send message appears in timeline` | E2E | P0 | Task 5 |
| Bot API send → sync delivers within 15 s | `messages.spec.ts#Bot message received via sync` | E2E | P0 | Task 5 |

### AC 6 — Quality
| Criterion | Test / Check | Level | Priority |
|---|---|---|---|
| No English-only selectors | Selector audit (Task 7) | Code review | P1 |
| Auto-skip when stack unreachable | `test.beforeAll()` guards in all specs | E2E infrastructure | P0 |
| `make test-e2e` passes | CI run after Task 6 | CI | P0 |

---

## Step 4: Generated Failing Tests (RED PHASE)

### Files Written

| File | AC | Tests | Priority | Failure Reason Until Implemented |
|---|---|---|---|---|
| `e2e/tests/fixtures/oidc.ts` | AC 1 | Fixture (throws) | P0 | loginViaOidc throws NotImplemented |
| `e2e/tests/fixtures/helpers.ts` | AC 1 | Fixture (throws) | P0 | isElementReachable/isDexReachable/dismissKeyDialog throw |
| `e2e/tests/features/login/sso-login.spec.ts` | AC 1, AC 6 | 5 tests (all skip+throw) | P0–P1 | loginViaOidc throws until Task 1 |
| `e2e/tests/features/room/room-lifecycle.spec.ts` | AC 2 | 3 tests (all skip+throw) | P0–P1 | loginViaOidc/helpers throw until Task 1; full impl until Task 3 |
| `e2e/tests/features/room/invites.spec.ts` | AC 3 | 2 tests (all skip+throw) | P0 | loginViaOidc/helpers throw until Task 1; full impl until Task 4 |
| `e2e/tests/features/messages/messages.spec.ts` | AC 4 | 2 tests (all skip+throw) | P0 | loginViaOidc/helpers throw until Task 1; full impl until Task 5 |
| `e2e/tests/features/admin/bootstrap.spec.ts` | AC 1 | 1 migration placeholder (skip) | P1 | Full content migration in Task 6 |

### TDD Phase: RED
All spec tests use `test.skip(...)` wrapping AND the fixture stubs throw `Error('not yet implemented')`.
This double-guard ensures:
1. Playwright reports tests as "skipped" (not "failed") until implementation starts
2. The fixture contract is enforced — any test calling the stubs before Task 1 is complete will throw

### Not Generated (out of scope for ATDD)
- AC 5: `/bmad-tea` gate — process gate, not a test file
- AC 7: Bug report — post-implementation activity
- `bootstrap-happy-path.spec.ts` in features/admin/ — content migration task only (Task 6)

---

## AC Coverage Matrix

| AC | Test Count | P0 | P1 | P2 | Status |
|---|---|---|---|---|---|
| AC 1 (fixtures + structure) | 6 (5 login + 1 admin placeholder) | 5 | 1 | 0 | RED |
| AC 2 (room lifecycle) | 3 | 2 | 1 | 0 | RED |
| AC 3 (invites) | 2 | 2 | 0 | 0 | RED |
| AC 4 (messages) | 2 | 2 | 0 | 0 | RED |
| AC 5 (TEA gate) | — | — | — | — | Process gate |
| AC 6 (quality) | Via all specs + Task 7 audit | P0 | P1 | — | RED |
| AC 7 (bug report) | — | — | — | — | Post-impl |
| **TOTAL** | **13** | **11** | **2** | **0** | |

---

## Implementation Order (for dev agent)

Following CLAUDE.md TDD standard:

1. **Task 1 first**: extract `loginViaOidc` and helpers from `element_e2e.spec.ts`
   → remove `throw` from fixtures → beforeAll guards start working
2. **Task 2**: migrate login tests → `features/login/sso-login.spec.ts` → remove `test.skip()`
3. **Tasks 3–5**: implement room-lifecycle, invites, messages → remove `test.skip()` per test
4. **Task 6**: migrate bootstrap tests → `features/admin/` → delete originals
5. **Task 7**: selector audit + full suite run
6. **Task 8**: document new bugs

The `test.skip()` + fixture stubs mean the red-phase tests are safe to commit without
breaking `make test-e2e` (bootstrap tests unaffected, all new tests are skipped).
