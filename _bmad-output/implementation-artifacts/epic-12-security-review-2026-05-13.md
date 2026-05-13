# Epic 12 Security Review — 2026-05-13 (Re-Review after Story 12.7 Remediation)

## Scope

- Epic: 12 — Media Gateway Phase 2 (MinIO backend, IAM, thumbnails) + SEC Gate 2 hardening (Story 12.7)
- Base: `5020ec8`
- HEAD: `4b2dc3c` (branch `feature/epic-12-media`)
- Reviewer: Kassandra (adversarial security)
- Re-review trigger: Story 12.7 added 2026-05-13 to remediate all 10 findings from the 2026-05-12 review.
- Stories covered in diff range:
  - 12.1 — MinIO Docker Compose + Docker Secrets
  - 12.2 — Storage Interface Refactor (Storer / LocalStorer / MinIOStorer)
  - 12.3 — MinIO Backend Wiring + IAM hardening
  - 12.4 — Media Download error classification (404/502 semantics)
  - 12.5 — On-demand thumbnail generation
  - 12.6 — Blurhash pass-through + animated thumbnail correctness
  - **12.7 — SEC Gate 2 Fixes (this review's primary subject)**
  - Plus carry-over from Epic 11 (11.7–11.11) and infra fixes already reviewed in 2026-05-12 report.

## Method

- Reviewed every remediation in the 2026-05-12 report against the post-Story-12.7 diff.
- Adversarial mindset: for each fix, asked "can I still exploit this? did the fix introduce a new bug?"
- Verified `git show 1074120` (Story 12.7 commit) line-by-line for the 10 claimed remediations.
- Looked for new findings introduced by the remediation patches.
- MEMORY.md scanned: no relevant accepted risks for these areas.

## Findings — Remediation Verification

| # (orig) | Severity | Status | Notes |
|---|---|---|---|
| 1 (HIGH-1) | HIGH | **FIXED** | `maxThumbDim = 2048` enforced at handler entry; `maxSourceMegapixels = 100_000_000` checked after decode; `maxGIFFrames = 200` capped before per-frame resize. Tests at AT-1/AT-2/AT-1b verify all three. |
| 2 (HIGH-2) | HIGH | **FIXED (with documented residual fail-open path)** | `TokenVerifier` interface wired into `upload.Handler`; `oidc.NewProvider` + `provider.Verifier(&oidc.Config{ClientID})` validates signature/expiry/audience. Compose sets `NEBU_OIDC_ISSUER=http://dex:5556/dex`. **Caveat:** when `NEBU_OIDC_ISSUER` is unset OR provider init fails at media startup, the handler silently falls back to "any Bearer accepted" (upload.go:158-162). This is the LOW advisory the dev team accepted at SEC Gate 1. See Residual-1 below. |
| 3 (HIGH-3) | HIGH | **FIXED** | Upload: `blockedContentTypes` map rejects `text/html`, `application/xhtml+xml`, `text/javascript`, `application/javascript`, `image/svg+xml`, `application/x-shockwave-flash`. Content-Type normalised via `mime.ParseMediaType` so `text/html; charset=utf-8` is also blocked. Download: `safeInlineContentTypes` allowlist forces `application/octet-stream` + `attachment` for any other type; `X-Content-Type-Options: nosniff` is set unconditionally. Thumbnail handler also emits `nosniff`. |
| 4 (MEDIUM-4) | MEDIUM | **FIXED** | `createbuckets` entrypoint now uses `export MC_HOST_minio=http://...` (sh env, not mc argv), and `printf "%s\n%s\n" "$APP_KEY" "$APP_SECRET" | mc admin user add minio --stdin`. `printf` is a busybox-ash shell builtin in the `minio/mc` image — does not fork, so no argv leak. The `$(cat /run/secrets/...)` substitution lives only in `/bin/sh`'s parent argv (not in mc's argv) and the secret value is not part of the entrypoint string itself. |
| 5 (MEDIUM-5) | MEDIUM | **FIXED** | Migration `000046_server_config_scope_update_policy.up.sql` drops `config_update_all`, creates `config_update_mutable` with explicit `key IN ('oidc_user_id_claim', 'oidc_displayname_claim', 'oidc_email_claim', 'admin_group_claim', 'oidc_issuer', 'oidc_client_id', 'oidc_client_secret')`. `server_name` and `bootstrap_completed` are no longer in the mutable allowlist — DB-level immutability restored. Integration test `migrations_046_integration_test.go` validates both block-immutable and allow-mutable paths. |
| 6 (MEDIUM-6) | MEDIUM | **FIXED** | `minio/minio:RELEASE.2026-04-18T19-53-40Z` and `minio/mc:RELEASE.2026-04-18T09-06-52Z` — both 2026 tags. |
| 7 (LOW-7) | LOW | **FIXED** | `crypto/subtle.ConstantTimeCompare([]byte(nonceClaims.Nonce), []byte(entry.nonce)) == 1` after empty-string guard. |
| 8 (LOW-8) | LOW | **FIXED** | `want_prefix` field removed from `slog.Error`; `got` (claim value) is also no longer logged on mismatch — only a generic "stale or cached id_token" message remains. |
| 9 (LOW-9) | LOW | **FIXED** | Precedence inverted: `NEBU_MINIO_ACCESS_KEY_FILE` and `NEBU_MINIO_SECRET_KEY_FILE` checked first; plain env var is only the fallback. Aligns with the gateway's `NEBU_INTERNAL_SECRET_FILE` pattern. |
| 10 (LOW-10) | LOW | **FIXED** | `http.Server{ReadHeaderTimeout: 10s, ReadTimeout: 60s, WriteTimeout: 120s, IdleTimeout: 120s}` configured before `ListenAndServe()`. Slowloris-resistant. |

## New / Residual Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|----------|-------|----------|---------------|--------|
| R-1 | LOW | 12.7 | `media/cmd/media/main.go:194-209`; `media/internal/upload/upload.go:128-162` | OIDC verifier fail-open when `NEBU_OIDC_ISSUER` is empty OR provider init fails at startup — handler accepts any Bearer string. Same vulnerability class as HIGH-2 but only reachable when operator misconfigures or Dex is offline at startup. | Pre-existing (acknowledged at SEC Gate 1 as 1 LOW advisory) |
| R-2 | LOW | 12.7 | `media/internal/upload/upload.go:148-156` | Verified upload sets `uploader_user_id = claims.Sub` (or `claims.Name` fallback) — raw OIDC subject (typically a UUID), NOT the Matrix user ID `@localpart:server` that the gateway login flow stores. Audit-trail correlation between uploads and Matrix user IDs will require a separate mapping step. Not a security issue per se; audit-quality concern. | New (introduced by HIGH-2 fix) |
| R-3 | LOW | 12.7 | `media/internal/upload/upload.go:129-156` | If the OIDC verifier is configured but the JWT contains neither `sub` nor `name` claims, the handler returns 401 — but uses `M_UNKNOWN_TOKEN` rather than `M_MISSING_PARAM`. Minor Matrix-spec semantics deviation. | New (informational) |
| R-4 | INFO | 12.7 | `media/internal/upload/upload.go:158-162` | Comment "This path should not be reachable in production deployments" — but there is no compile-time or runtime flag (`require_oidc=true`) that enforces this. A production operator who forgets `NEBU_OIDC_ISSUER` will silently get the dev-mode behaviour. Recommend adding `NEBU_OIDC_REQUIRED=true` env that aborts startup when verifier is nil. | New (defence-in-depth hardening) |

### Residual-1 Detail (LOW, accepted)

When `NEBU_OIDC_ISSUER` is empty (operator misconfiguration) or `oidc.NewProvider` fails (Dex unreachable at media start), the verifier is nil and `upload.go:161` accepts the raw bearer string as the `uploader_user_id` — identical to the pre-12.7 behaviour. The compose default wires this correctly and emits `slog.Warn` if it falls back, so an operator examining logs will see it. The risk:

- **Threat:** Operator deploys with empty `NEBU_OIDC_ISSUER`; clients can upload anonymously and impersonate any user in `uploader_user_id`.
- **Likelihood:** Low (Compose default is correct; warning is emitted).
- **Impact:** Same as the original HIGH-2 (anonymous upload + impersonation).
- **Recommendation (R-4 above):** Add a `NEBU_OIDC_REQUIRED=true` (or default-required) env that aborts startup when the verifier is nil, making the dev-mode shortcut explicit and opt-in.

### Residual-2 Detail (LOW, new)

The Story 12.7 fix correctly extracts `claims.Sub` as the uploader identity. However, the gateway's login flow uses `FormatUserIDFromClaims(uidClaim, allClaims, serverName)` (gateway/internal/grpc/metadata.go:54) to build Matrix user IDs `@localpart:serverName` from the configured `oidc_user_id_claim`. The media upload handler:

1. Does not consult `oidc_user_id_claim` from `server_config` — hardcoded to `sub`/`name`.
2. Does not format the value as `@localpart:server` — raw value stored.

Audit queries that try to correlate `media_files.uploader_user_id` with rooms/users will see mismatched IDs (UUID vs. Matrix user ID). This is a quality/observability concern, not a privilege issue. Fix in a follow-up story: reuse the gateway's `userIDClaimLoader` pattern or call back into the API gateway for the canonical user ID.

### Residual-3 Detail (LOW, informational)

`M_MISSING_PARAM` is the typical Matrix CS API errcode for missing required parameters. Using `M_UNKNOWN_TOKEN` for "token missing subject claim" conflates "token invalid" (auth failure) with "token malformed" (semantics). Strictly per spec, neither is perfect — pragmatic enough.

## Cross-Story Patterns (Updated)

1. **MVP-shortcut residue.** Story 12.7 cleanly remediated the dormant `// for MVP, accept any bearer` defect — but the fix preserves a fail-open path for unconfigured deployments. This is the *managed* form of the same pattern: better than the original, still not ideal. Add a `NEBU_OIDC_REQUIRED` knob to make the dev path explicit.

2. **RLS key-scoping pattern formalised.** Migration 000046 demonstrates the corrective pattern for the recurring "Permissive UPDATE without key-scope" finding in MEMORY.md. Going forward, **any new RLS UPDATE policy on `server_config` must list its mutable keys explicitly**.

3. **Argv leak avoidance pattern.** The `MC_HOST_minio` env-var pattern for credential injection into `mc` should become standard for any future shell entrypoints handling Docker Secrets. Document in deployer-hardening guide.

4. **Unauthenticated endpoint + per-request resource bounds.** Thumbnail clamping (2048 dim, 100 MP source, 200 frames) is now the canonical mitigation pattern for any unauthenticated Matrix v3 endpoint that performs expensive work. Apply to future endpoints (e.g., preview_url).

## Accepted Risks

| Risk | Justification | Accepted by | Date |
|------|--------------|-------------|------|
| Residual-1 (OIDC fail-open at media startup) | Compose default is correct; `slog.Warn` is emitted; matches the gateway's startup pattern (Dex unavailable at boot fails open with warnings). Operator hardening guidance to be added with a follow-up. | (pending sign-off — recommended) | 2026-05-13 |

## Follow-up Stories Required

None to block epic-done. The three HIGH findings from 2026-05-12 are remediated and verified.

**Recommended follow-ups (non-blocking):**

- Story 12-FU-7 — Add `NEBU_OIDC_REQUIRED=true` env to fail media startup when verifier is nil (closes Residual-1 + R-4 properly).
- Story 12-FU-8 — Format `uploader_user_id` as canonical Matrix user ID using `oidc_user_id_claim` from server_config (closes Residual-2).
- Story 12-FU-9 — Add per-IP rate limiting to media gateway (`x/time/rate` middleware) — closes the original Finding #11 (LOW) which Story 12.7 did not address explicitly. Without rate limits, even with the dimension caps, an attacker can still issue many concurrent `width=2048&height=2048` requests for 8.4 GB cumulative allocation (≈16 MB × 500 reqs).

## Summary

CRITICAL: 0
HIGH: 0  (3 prior HIGH all remediated)
MEDIUM: 0  (3 prior MEDIUM all remediated)
LOW: 3 new + 1 pre-existing-acknowledged
INFO: 1

Follow-up stories required: 0 (3 recommended, non-blocking)
Accepted risks: 1 (Residual-1, pending sign-off)

**Epic security gate: PASS — eligible for epic-done.**

Story 12.7's remediation is comprehensive and tested. The three HIGH findings are closed with positive controls (constants, allowlists, JWT verification with real go-oidc/v3), MEDIUM regressions are reverted (RLS scope-back, image bumps, secrets off argv), and LOW hygiene items are individually addressed. The remaining 4 LOW/INFO findings are operational hardening recommendations, not exploitable defects in the shipping configuration.

**Classification: CLEAN.**

The epic has moved from "BLOCKED — 3 HIGH" (2026-05-12) to "PASS — 0 HIGH/CRITICAL, 4 LOW/INFO hardening recommendations" (2026-05-13). The team should accept Residual-1 in writing or schedule 12-FU-7 before the next deployer-facing release.
