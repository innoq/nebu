# Story 4.28: Element Web Browser E2E — Gap Coverage (Stories 4-25/26/27 + Patterns)

Status: done

## Story

As a developer,
I want browser-level E2E tests in `element_e2e.spec.ts` that verify the 3 new endpoints work correctly with Element Web as a real client,
so that regressions in filter, member list, and read markers are caught before release.

---

## Background / Motivation

The Matrix API contract tests (`matrix_api.spec.ts`) validate at HTTP/API level but do not cover browser-side behavior.
Analysis of Element Web's own Playwright test suite (141 specs in `apps/web/playwright/e2e/`) revealed 3 gaps directly related to the new endpoints.

Patterns adapted from:
- `stored-credentials.spec.ts` → reconnect / filter test
- `invite-dialog.spec.ts` → member list test
- `read-receipts/high-level.spec.ts` → read markers test

---

## Acceptance Criteria

1. New test "Reconnect after reload — no sync ERROR loop" in `element_e2e.spec.ts`:
   - SSO login → create room → page reload → Element reconnects
   - Zero "Getting filter failed" console errors after reload
   - Room list visible (sync healthy)

2. New test "Member list populated after joining room":
   - SSO login → create room → navigate to room → open member panel
   - Member list container visible; at least one member name ("alex") appears

3. New test "No read_markers retry loop when entering room":
   - SSO login → create room with message → navigate to room
   - After 5 seconds: zero console errors containing "fully_read" or "Error"

4. All 3 tests auto-skip when Element Web (`localhost:7070`) or Dex (`localhost:5556`) is unreachable.

5. Tests share the existing `performSsoLogin` helper (no duplication).

6. New `test.describe` block has its own `beforeAll` reachability check and `test.setTimeout(120_000)`.

---

## Acceptance Tests

These ARE the acceptance tests (browser E2E = the story itself).

---

## Implementation Notes

- File: `e2e/tests/element_e2e.spec.ts` (append-only, new `test.describe` block at end)
- Pattern: API-setup + browser-verify (Element Web docs best practice: "minimize UI driving for setup")
- No Synapse-specific fixtures used — SSO via Dex, direct HTTP calls for room setup
- `test.skip` guards replace `homeserver` fixture from Element Web test suite
- Unused `Request` type import removed
