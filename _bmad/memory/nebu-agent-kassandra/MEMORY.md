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
| Permissive RLS UPDATE without key-scope | 11, 12 | `CREATE POLICY ... FOR UPDATE USING (true) WITH CHECK (true)` on shared key-value tables (e.g., `server_config`) destroys the per-key immutability invariant. Always scope UPDATE policies by `key IN (...)` allowlist. Migration 000045 is the cautionary tale. |
| "// for MVP" auth shortcut → live vulnerability | 4 → 12 | A `// for MVP, accept any bearer` comment lived in media/upload for 8 epics; Story 12.3 wired the service into compose and exposed port 8009. The dormant defect became reachable. Rule: when promoting a code-only module to a deployed compose service, re-audit every `// MVP` / `// placeholder` / `// TODO` token in the affected files. |
| MinIO mc entrypoint secret leak via argv | 12 | `$(cat /run/secrets/x)` in shell entrypoints expands secrets into `mc` argv → recoverable via `/proc/<pid>/cmdline`. Defeats Docker Secrets. Use `MC_HOST_<alias>` env var or `--stdin` form. |
| Unauthenticated media endpoint + unbounded resource | 12 | Thumbnail (`width`/`height`) and download (`io.ReadAll`) handlers do expensive per-request work without auth and without bounds. Every unauthenticated Matrix v3 endpoint MUST have hard caps on input dimensions, output size, and concurrency. |
| Inline Content-Type echoed back with `inline` disposition | 12 (4) | Stored XSS surface: attacker uploads `Content-Type: text/html`, download serves it back inline. Matrix spec v1.12+ requires `attachment` for unsafe types. Add `X-Content-Type-Options: nosniff` always; allowlist server-controlled types on download. |
| OIDC fail-open at startup | 12 | When an OIDC-required service falls back to "any bearer accepted" if the provider can't be reached at boot, an operator misconfiguration (empty issuer env, Dex offline) silently produces an anonymous-upload surface. Pattern: services that require OIDC must support a `_REQUIRED=true` knob to refuse startup when the verifier is nil. Logged `slog.Warn` is necessary but insufficient. |
| `uploader_user_id` ≠ Matrix user ID | 12 | When media stores identity claims (raw `sub` or `name`) without going through the gateway's `FormatUserIDFromClaims` translation, audit-trail correlation breaks. Any service that records "user IDs" must store the canonical Matrix `@localpart:server` form, not raw OIDC claims. Story 12.9 partially closed this: added `@localpart:server` format + sanitisation, but still extracts from `claims.Sub` (with `Name` fallback) instead of operator-configured `oidc_user_id_claim`. Full fix requires reading that config (DB or env) at media startup. |
| XFF rate-limit spoofing on directly-exposed services | 12 | When a service exposes a port directly to the host (`ports: ["8009:8009"]`) and the rate-limiter trusts `X-Forwarded-For` without a `trustedProxy` toggle, attackers rotate the rightmost XFF entry to force fresh sync.Map buckets and bypass per-IP rate limits entirely. Always gate XFF parsing behind an explicit `NEBU_TRUSTED_PROXY` env (default `false`), mirroring `gateway/internal/middleware/ratelimit.go` (Story 12.10 missed this). |
| Per-story CLEAN reviews can stack into combined HIGH at epic-end | 12 | 12.8, 12.9, 12.10 each reviewed CLEAN in isolation. Together they created (a) audit-correlation defeat and (b) bypassable rate-limit on shipping topology. Per-story reviews must consider deployment topology, not just the diff in isolation. Epic-end review is non-redundant. |

## Epic Review History
_Summary of completed epic-end reviews._

| Epic | Date | CRITICAL | HIGH | MEDIUM | LOW | Report |
|------|------|---------|------|--------|-----|--------|
| 9 (9-19 to 9-25) | 2026-05-06 | 0 | 0 | 2 | 1 | epic-9-sec-gate2-final-2026-05-06.md |
| 12 (12.1–12.6 + 11.7–11.11 carry-over) | 2026-05-12 | 0 | 3 | 3 | 5 | epic-12-security-review-2026-05-12.md |
| 12 (re-review after 12.7 remediation) | 2026-05-13 | 0 | 0 | 0 | 3+1 | epic-12-security-review-2026-05-13.md — PASS |
| 12 (final SEC Gate 2 after 12.8/12.9/12.10) | 2026-05-13 | 0 | 1 | 1 | 3 | epic-12-security-review-final-2026-05-13.md — BLOCKED (F-2 XFF spoof HIGH, F-1 canonical claim MEDIUM) |
