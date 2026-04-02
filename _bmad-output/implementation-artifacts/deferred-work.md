# Deferred Work

## Deferred from: code review of story-3-9 (2026-04-02) — RESOLVED in Epic 3 Retrospective (2026-04-02)

- ~~`extractFirstRoleClaim` takes only the first array element~~ → **FIXED**: Replaced with `auth.MatchesAdminGroupClaim` which checks ALL array elements across ALL claims. `admin_group_claim` is now configurable via Bootstrap Wizard.
- ~~Catch-all `GET /admin/` handler silently swallows DB errors~~ → **FIXED**: DB error in catch-all now returns 500 via `admin.Error500`.
- ~~`bootstrap-done.html` hardcodes `instance_admin` role name~~ → **FIXED**: `bootstrap-done.html` and `DoneHandler` removed. Post-bootstrap flow redirects directly to dashboard.
