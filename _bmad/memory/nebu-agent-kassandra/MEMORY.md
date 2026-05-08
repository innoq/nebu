# Memory

## Architecture Security Context
_Filled during First Breath — Go Gateway auth layer, Elixir Core boundaries, sensitive surfaces._

## Accepted Risks
_Formally acknowledged trade-offs with date, justification, and owner sign-off._

| Risk | Justification | Accepted by | Date |
|------|--------------|-------------|------|

## Recurring Patterns
_Finding types that appear across multiple stories — indicators of systemic issues._

## Recurring Patterns

| Pattern | Epics | Description |
|---------|-------|-------------|
| Missing RLS on new tables | 9 | New tables (forgotten_rooms) added without RLS policy, breaking defense-in-depth. Check every new table migration for ENABLE ROW LEVEL SECURITY + policy. |
| Device-ID threading gaps | 9 | When per-device columns are added to existing queries, all dependent query helpers must be updated to pass device_id. Check all query helpers when schema adds device_id to PK. |
| Nullable state_key + equality filter | 11 | Any `WHERE state_key = '...'` filter misses NULL rows because in three-valued SQL logic `NULL = ''` is NULL. The events.state_key column (mig 000038) is nullable. Defense-in-depth: prefer event_type-only checks for "is room encrypted/redacted/etc.", or include `OR state_key IS NULL` explicitly. |
| DB-module user_id trust-boundary docstring | 11 | New DB modules taking `user_id` for authorization scoping must document loudly that it MUST come from the validated session, not from request payload. The hand-off to gRPC handler stories is the natural spot to lose that invariant — see Story 11.3. |

## Epic Review History
_Summary of completed epic-end reviews._

| Epic | Date | CRITICAL | HIGH | MEDIUM | LOW | Report |
|------|------|---------|------|--------|-----|--------|
| 9 (9-19 to 9-25) | 2026-05-06 | 0 | 0 | 2 | 1 | epic-9-sec-gate2-final-2026-05-06.md |
