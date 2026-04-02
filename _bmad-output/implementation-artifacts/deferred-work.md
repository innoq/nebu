# Deferred Work

## Deferred from: code review of story-3-9 (2026-04-02)

- `extractFirstRoleClaim` takes only the first array element — if OIDC provider returns `["viewer", "instance_admin"]`, only `"viewer"` is used. Should check all elements for target role. Pre-existing OIDC pattern, not caused by current change.
- Catch-all `GET /admin/` handler silently swallows DB errors (returns 404 instead of 500). Unlike `BootstrapGuard` which returns 500 on DB error, the catch-all degrades silently. Operational observability gap.
- `bootstrap-done.html` hardcodes `instance_admin` role name instead of reading from session. Acceptable for bootstrap-only page but should use actual session data if the page is ever reused.
