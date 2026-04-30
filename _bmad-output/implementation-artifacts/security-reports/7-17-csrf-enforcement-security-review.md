# Security Review: Story 7-17 — CSRF Enforcement + Body Size Limits

Date: 2026-04-30
Reviewer: Kassandra (SEC Gate 1)
Scope: staged diff for Story 7-17 (`git diff --staged`)
Files reviewed:
- `gateway/cmd/gateway/main.go`
- `gateway/internal/admin/{compliance_handler,config,role_mapping,rooms,users}.go`
- `gateway/internal/admin/csrf_body_limit_test.go` (new)
- supporting (read-only): `gateway/internal/admin/middleware.go`, `gateway/internal/middleware/body_limit.go`

## Classification: CLEAN

No CRITICAL or HIGH findings in the scope of this story. The 11 routes are wrapped, the middleware order is correct, the constant-time CSRF compare is preserved, and the body-limit semantics enforce 64 KiB even when the inner handler never reads the body (because `CSRFMiddleware.ParseForm` reads it through the `MaxBytesReader` first). I tried hard to find a bypass — none found.

A handful of MEDIUM/LOW/INFO observations are recorded for follow-up. They do not block the gate.

---

## Findings

| # | Severity | Title | Location | Recommendation |
|---|----------|-------|----------|----------------|
| 1 | MEDIUM | `bufferedResponseWriter` leaks downstream `Header()` mutations into the synthesized 413 response | `gateway/internal/middleware/body_limit.go:42-95` | When the body limit triggers, clear leaked headers before writing the 413 — see detail below. |
| 2 | LOW | Test 2 (`PostWithValidCsrfAccepted`) only asserts `!= 403`; does not assert 200 OK or that the inner handler ran | `gateway/internal/admin/csrf_body_limit_test.go:222-225` | Tighten to `rr.Code == http.StatusOK` so a future regression that breaks pass-through to the handler is not silently green. |
| 3 | LOW | Body-limit tests bypass the production middleware chain — they wrap the handler with `BodyLimitMiddleware` alone, not with `bodyLimit64KiB(csrf(sessionGuard(...)))` | `gateway/internal/admin/csrf_body_limit_test.go:145-153, 240-302` | Add ONE end-to-end integration test that exercises the actual chain (`bodyLimit → csrf → sessionGuard → handler`) on a real route to lock the wiring. The current scaffolds prove each layer in isolation but do not prove the chain is wired correctly in `main.go`. |
| 4 | LOW | Tests skip `sessionGuard` deliberately (lines 19-24 of the test file) — the test would still pass if a future edit removed `sessionGuard()` from the production wiring | `gateway/internal/admin/csrf_body_limit_test.go` | Add a route-wiring assertion (e.g., a smoke test that verifies POST without `admin_session` cookie returns 302 to `/admin/login` for each of the 11 routes). Without this, "session guard accidentally removed" is not caught. |
| 5 | LOW | `UpdateRoleHandler`, `DeactivateUserHandler`, etc. perform no authorization check beyond "is logged in" — any admin can elevate any user to `instance_admin`, deactivate any user, archive any room, approve/reject any compliance request | `gateway/internal/admin/users.go:160-208`, `rooms.go:179-215`, `compliance_handler.go:53-79` | Out of scope for Story 7-17 (story is CSRF+body-limit only) and the handlers are stubs (`TODO(epic-6)`), but flag for Epic 6: enforce role-based authorization (instance_admin only, four-eyes for compliance, prevent self-deactivation/self-demotion). DO NOT close Epic 7 without a follow-up story tracking this. |
| 6 | INFO | `CSRFMiddleware` returns 400 "Bad Request" when `ParseForm` fails on POST. With the 64 KiB limit, an oversized body causes `ParseForm` to fail — but the body-limit wrapper correctly substitutes a 413, so the 400 never escapes. Verified by tracing the buffered-writer flow. | `gateway/internal/admin/middleware.go:282-285` + `body_limit.go:71-95` | None — current behaviour is correct. Documented because the interaction is not obvious. |
| 7 | INFO | Removed-comments review: only `TODO(story-7-csrf): enforce CSRF middleware when wiring in production` lines were deleted. The adjacent `TODO(epic-6): replace stub mutation` comments were preserved. No security-relevant comments were lost. | All five handler files | None. |
| 8 | INFO | `subtle.ConstantTimeCompare` returns 0 in O(1) when input lengths differ. CSRF tokens are fixed-length (43 chars, base64url(32 bytes)), so timing-leak via length is not a practical attack vector. | `gateway/internal/admin/middleware.go:293` | None. |

### Detail on Finding #1 (MEDIUM) — header leakage in 413 path

`bufferedResponseWriter` embeds `http.ResponseWriter`, so the inner handler's `w.Header().Set(...)` calls write directly to the underlying writer's header map. When `BodyLimitMiddleware` detects `lb.exceeded == true`, it writes the 413 directly with `w.Header().Set("Content-Type", "application/json")` and `w.WriteHeader(413)`. Any other headers the inner handler set before the limit was detected — `Set-Cookie`, `Location`, `X-*` — remain on the response.

In the 7-17 chain, this means: if `CSRFMiddleware` calls `r.ParseForm()`, the parse fails on `MaxBytesError`, CSRF calls `http.Error(w, "Bad Request", 400)`. Go's `http.Error` sets `Content-Type: text/plain; charset=utf-8` and `X-Content-Type-Options: nosniff` on the underlying writer. The body-limit handler then overwrites `Content-Type` to `application/json`, but `X-Content-Type-Options: nosniff` survives — harmless, even good.

The realistic exploit: inner handler does `http.Redirect(w, r, "/admin/...", 303)` *before* the body-limit triggers. `Location` is set on the real writer. Body-limit writes 413 — the response is now `413 Request Entity Too Large` *with* a `Location` header. RFC 7231 says clients ignore `Location` on non-3xx, so this is unlikely to mislead browsers. But it is sloppy and would mask debugging. In Story 7-17's chain the inner handlers can only redirect AFTER CSRF has already read the body — so if the limit triggered, CSRF returned 400 first and the inner handler never ran. **Not exploitable in this story's wiring.** Filed as MEDIUM because it is a latent foot-gun for future routes that don't go through CSRF first (e.g., compliance API routes already wired without CSRF).

Fix: in the `if lb.exceeded` branch, snapshot/clear `w.Header()` before writing — e.g., delete every key not in an allowlist, or build a fresh header map. Or: replace the buffered-writer trick with a `MaxBytesError`-aware check earlier in the chain (read body once into a `LimitReader`, fail fast at >max, then hand a fresh `bytes.Reader` to handlers).

---

## Coverage check (the 11 POST routes)

Story scope explicitly enumerated 11 routes. I confirmed each is wrapped with `bodyLimit64KiB(csrf(sessionGuard(...)))`:

1. `POST /admin/users/{userId}/display-name` — main.go:313 ✓
2. `POST /admin/users/{userId}/role` — main.go:314 ✓
3. `POST /admin/users/{userId}/deactivate` — main.go:315 ✓
4. `POST /admin/users/{userId}/reactivate` — main.go:316 ✓
5. `POST /admin/rooms/{roomId}/name` — main.go:320 ✓
6. `POST /admin/rooms/{roomId}/archive` — main.go:321 ✓
7. `POST /admin/rooms/{roomId}/unarchive` — main.go:322 ✓
8. `POST /admin/config` — main.go:326 ✓
9. `POST /admin/config/role-mapping` — main.go:331 ✓
10. `POST /admin/compliance/{id}/approve` — main.go:336 ✓
11. `POST /admin/compliance/{id}/reject` — main.go:337 ✓

Other admin POST routes in `main.go` (out of scope for this story but checked for regression):
- `POST /admin/logout` (line 303) — already wrapped before this story, still wrapped ✓
- `POST /admin/bootstrap` (line 364) — `adminRL(bodyLimit64KiB(csrf(guard(...))))` ✓
- `POST /admin/bootstrap/select-claim` (line 367) — same chain ✓
- `POST /internal/nodes/register` (line 224) — internal PSK route, out of scope, no admin session involved ✓

No admin POST route is left without CSRF or body-limit.

---

## Checklist (story-specific)

- [x] All 11 routes protected with `bodyLimit64KiB(csrf(sessionGuard(...)))`
- [x] Middleware order is `bodyLimit` outermost → `csrf` middle → `sessionGuard` innermost (correct: body limit must wrap CSRF so an oversized body triggers 413, not a 400 from `ParseForm`; CSRF must wrap session guard so token verification runs before any state mutation)
- [x] `CSRFMiddleware` uses `subtle.ConstantTimeCompare` — confirmed at `middleware.go:293`
- [x] 64 KiB limit (`64 * 1024 = 65536`) matches Story 7-17's mandate and matches the test constant
- [x] No admin POST endpoints left unprotected
- [x] Removed comments are only the `TODO(story-7-csrf)` markers; no security-relevant comments deleted
- [x] No SQL injection surface in this diff (handlers mutate stub slices in-memory; no DB calls)
- [x] No XSS surface added (handlers render via `tmpl.render` and PRG-redirect with constant flash strings; user input goes back via `?flash=` only with hard-coded values like `"Role+updated"`)
- [x] No open-redirect surface (redirects use constructed URLs `/admin/users/<userID>?flash=...` — `userID` comes from path; while not validated as a "safe" string, the `Location` value still starts with `/admin/users/` which keeps it same-origin; admins are the only callers and the URL is built server-side, not from form input)
- [x] No path traversal (path values feed string equality lookups against stub slices)
- [x] No auth-bypass: `sessionGuard` is the innermost layer and runs before every handler

---

## Tests in the new file

The new `csrf_body_limit_test.go` contains 8 test groups:
1. POST without CSRF → 403 (per route)
2. POST with valid CSRF → not 403 (per route) — **see Finding #2**
3. POST with oversized body → 413 (per route)
4. POST at exact limit (64 KiB) → not 413 (boundary check)
5. POST with mismatched CSRF cookie/form → 403
6. GET routes pass CSRF middleware
7. GET routes set a non-empty `csrf_token` cookie
8. Completeness guard — exactly 11 POST routes registered

Test design notes:
- Uses subtests with `t.Parallel()` — good.
- Loop-variable capture (`route := route`) is correct (Go ≤ 1.21 idiom; safe under Go 1.22+ too).
- Tests intentionally apply middleware layers in isolation (CSRF only, or BodyLimit only) to make the layer-under-test the FIRST observable failure. This is a defensible test-design choice but creates the gap noted in Findings #3 and #4 — the wiring in `main.go` itself is not asserted.

No test shortcuts that mask security regressions: no cookie forging on real session cookies, no DB seeding shortcuts, no skipping of constant-time compare. The synthetic `matchingToken` in Test 2 is fine because the test only verifies that double-submit accepts equal values; it does not impersonate a real session.

---

## Severity Counts

- CRITICAL: 0
- HIGH: 0
- MEDIUM: 1
- LOW: 4
- INFO: 3

## Summary

Story 7-17 closes a real CSRF gap on 10 admin POST routes (the 11th, `/admin/logout`, was already protected) and adds a 64 KiB body-size cap. The middleware order is correct, the constant-time compare is preserved, the buffered-writer interaction with `MaxBytesError` does the right thing in this chain, and no admin POST escapes the protection. The test scaffolds are well-structured.

Two structural test gaps (Findings #3 and #4) mean the *wiring* in `main.go` is not directly asserted — a future refactor that removes `csrf()` or `sessionGuard()` from one of the lines would not fail the new tests. Recommend a small smoke-style integration test that spins the actual mux for the 11 routes and asserts the chain end-to-end. Not blocking.

Finding #1 (header leakage on 413) is a latent issue in `BodyLimitMiddleware` itself — pre-existing, not introduced by this story, and not exploitable in this story's chain. Worth a follow-up story when time permits.

Authorization on the stub mutators (Finding #5) remains the elephant in the room — but it is explicitly Epic 6 work, not Story 7-17. Make sure it gets a follow-up story and is tracked on the SEC Gate 2 epic-end review.

**Gate decision: PASS** (CLEAN — no CRITICAL/HIGH).
