---
security_review: optional
---

# Story 5.29d: Test Infrastructure & Dev Environment Hardening

Status: ready-for-dev

## Story

As a developer working in CI/dev,
I want every test in the suite to actually run, the dev OIDC provider to enforce production-equivalent constraints, and Admin UI render paths to be regression-tested for XSS,
so that pipeline signal is honest and security regressions cannot hide behind silently-skipped tests or convenient dev shortcuts.

---

## Background / Motivation

Four findings cluster as "fix the test/dev infrastructure that the production work in 5-1..5-9 inherited or skirted":

---

## Findings rolled into this story

### FB-E5-03 — Elixir event_dispatcher: 23 pre-existing test failures (MEDIUM)
- Source: surfaced during Story 5-2 TEA Gate 2; verified pre-existing via `git stash --include-untracked`. Two root causes:
  1. **`could not lookup Ecto repo Nebu.Repo`** — `Nebu.Repo` only configured for `:prod`/`:dev`. Tests in `create_room_test`, parts of `join_room_test`, and most `sync_test` scenarios hit code paths that call `Nebu.Room.DB` → `Nebu.Repo`.
  2. **Test fakes out of sync with `Nebu.Room.DB` interface** — `FakeInviteDB.accept_invitation/2` undefined, `SyncTestFakeDB.get_room_name/1` undefined, etc.
- Fix:
  1. Per test, decide: configurable `messages_db_module` override (same pattern as `audit_writer_module()` in 5-2), OR `@tag :integration` so the test runs only against a real DB.
  2. Update all `FakeDB` / `FakeInviteDB` / `SyncTestFakeDB` modules to current `Nebu.Room.DB` interface. Introduce `@behaviour Nebu.Room.DBBehaviour` so fakes are compile-time-checked (deferred-work.md already flagged this).

### FB-E5-08 — Dex dev config still has `password` grant_type (LOW, dev-only)
- Source: Epic-5 SEC Gate 2.
- Symptom: `dev/dex/config.yaml` retains `oauth2.passwordConnector: local` and the `password` grant. Convenient for ad-hoc scripts but contradicts CLAUDE.md "Authorization Code + PKCE only".
- Fix: remove `passwordConnector` and `password` grant from Dex dev config; verify all integration step-defs use authcode+PKCE; add a CI smoke that confirms the password grant is rejected by Dex.

### FB-53-02 — XSS escaping verification for `justification` (LOW)
- Source: 5-3 Kassandra (paired with FB-54-01, but FB-54-01 length-cap landed in 5-29b).
- Fix: Playwright test in 5-4 admin-pending-list rendering: submit a request with `justification = "<script>alert(1)</script>"`, render in admin pending-list, assert no script execution. Server-side check: confirm Go html/template auto-escape (no `template.HTML` raw escape, no innerHTML in client-side JS).

### FB-E5-09 — `DeleteUserKeys` gRPC subsumed by FB-52-01 (INFO — close-out only)
- Source: Epic-5 SEC Gate 2.
- Action: no separate fix needed. Once 5-29a Block B (gRPC mTLS/auth) lands, `DeleteUserKeys` is protected by the same interceptor as every other RPC. This block exists only to close the audit trail.

---

## Acceptance Criteria

1. `make test-unit-elixir` reports 0 failures, 0 skipped (or skipped with documented reason). The `event_dispatcher` test count stays the same or grows; failure count goes to 0.
2. `Nebu.Room.DB` has a `@behaviour` (`Nebu.Room.DBBehaviour`) and all production + fake implementations declare it.
3. Dex dev config has no `password` grant. Integration tests still pass (proof that no step-def relied on ROPC).
4. Playwright test `e2e/features/compliance-pending-list-xss.spec.ts` confirms `<script>` payload in `justification` does not execute when rendered in the admin pending list (Story 5-4 surface).
5. Admin pending-list template uses `{{ .Justification }}` (auto-escaped), never `{{ .Justification | safeHTML }}` or equivalent.

---

## Acceptance Tests

- `mix test` exit 0 across the whole umbrella.
- New Behaviour conformance test: every fake DB module compiles against `@behaviour Nebu.Room.DBBehaviour`.
- CI smoke: `curl -X POST .../token -d 'grant_type=password' ...` against dev Dex → 400/501.
- Playwright XSS-payload test → page does not execute the script.

---

## Implementation Notes

- The `@behaviour` introduction will surface every interface drift in one go — expect that fake fixes alone won't be enough; some tests may need to migrate to `@tag :integration`.
- The XSS test only matters once Story 7-11 (Compliance Admin UI) lands. If 7-11 is far off, downgrade FB-53-02 to "deferred until 7-11 lands" and remove the Playwright test from this story's AC.

---

## Dependencies

- Independent of 5-29a, b, c.

---

## Change Log

- 2026-04-23: Story split out from 5-29 master collector. Bundles FB-E5-03, FB-E5-08, FB-53-02, FB-E5-09 (close-out).
