# Pipeline Results — Story 13-7: MSC2965 OIDC Discovery Endpoints

**Date:** 2026-05-13
**Branch:** feature/phase-3-epic-13
**Status:** DONE

---

## Step 3 — Pre-dev Test Review

**Verdict: PASS (no MAJOR gaps)**

- 7 unit tests reviewed in `gateway/internal/matrix/oidc_discovery_test.go`
- 5 Godog scenarios reviewed in `gateway/features/oidc_discovery.feature`

Findings:
- `TestAuthMetadataHandler_ProviderUnreachable` correctly asserts `errcode=M_UNAVAILABLE` in JSON body (line 205) — not just HTTP 503. PASS.
- Stable path variants (`v1/auth_issuer`, `v1/auth_metadata`) covered: `TestAuthIssuerHandler_StablePath` (unit) + `OIDCDiscovery_AuthIssuer_Stable` and `OIDCDiscovery_AuthMetadata_Stable` (Godog). PASS.
- MINOR gap: No unit test for `v1/auth_metadata` stable path (integration scenario covers it — acceptable).
- MINOR gap: No test for empty `NEBU_OIDC_ISSUER` (startup validation would catch this; not a test-first blocker).

---

## Step 4 — Implementation

Created: `gateway/internal/matrix/oidc_discovery.go`
- `AuthIssuerHandler(cfg)` — returns `{"issuer":"<cfg.OIDCIssuer>"}`, unauthenticated
- `AuthMetadataHandler(cfg, httpClient)` — proxies OIDC discovery document with 5-min TTL cache; 503 M_UNAVAILABLE on failure
- Context-aware HTTP request via `http.NewRequestWithContext(r.Context(), ...)`
- `metadataCache` with `sync.RWMutex` for thread safety

Modified: `gateway/cmd/gateway/main.go`
- Replaced 404 stub with 4 real route registrations (unstable + v1 for each endpoint)
- Uses `looseRL` rate limiter (unauthenticated endpoints)
- 10s timeout on the OIDC HTTP client

---

## Step 5 — CI Gate (unit tests)

`make test-unit-go`: ALL PASS

```
ok  github.com/nebu/nebu/internal/matrix  34.465s
ok  github.com/nebu/nebu/cmd/gateway      1.082s
```

All 7 unit tests green. Full test suite green (no regressions).

Integration (`make test-integration`): Godog scenarios run against live stack — not executed (requires Docker Compose stack). Scenarios cover all 4 route combinations.

---

## Step 6 — Code Review

**Auto-fixed MINOR findings:**
1. Added `http.NewRequestWithContext(r.Context(), ...)` to pass request context to upstream HTTP fetch (client cancellation propagation).

**No MAJOR findings.**

---

## Step 7 — Security Review

Skipped — story frontmatter declares `security_review: not-needed`.

---

## Step 8 — Arc42 / Documentation Update

- `CLAUDE.md` §Matrix API Scope: added 2 new endpoint entries
- `docs/matrix-api-scope.md` §Discovery: replaced stub row with 4 ✅ rows
- `docs/architecture/05-building-blocks.md`: added `oidc_discovery.go` to Level 2 matrix/ section
- Story file: status changed `ready-for-dev` → `done`

---

## Files Changed

- `gateway/internal/matrix/oidc_discovery.go` (new)
- `gateway/cmd/gateway/main.go` (route stub replaced)
- `CLAUDE.md`
- `docs/matrix-api-scope.md`
- `docs/architecture/05-building-blocks.md`
- `docs/stories/phase3/epic-13/13-7-msc2965-oidc-discovery-endpoints.md`
