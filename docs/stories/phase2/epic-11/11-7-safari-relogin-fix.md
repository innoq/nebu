---
status: review
epic: 11
story: 7
security_review: required
matrix: true
ui: true
---

# Story 11.7: Safari SSO Re-Login Bugfix — Element Lands on #/welcome After Logout

Status: review

## Story

As a Nebu user on Safari (or any browser with aggressive HTTP-redirect caching),
I want the logout → re-login flow to land me on the room list,
So that I am not stuck on the #/welcome screen after a fresh SSO login.

**Size:** S

---

## Bug Description

### Symptom

After performing a Matrix logout and immediately re-logging in via Dex OIDC/SSO,
Element Web lands on `#/welcome` instead of the room list. A page reload recovers
the session, but users should never need to do that.

### Root Cause — Three Concurrent Failure Paths

**Path A — Dex cached id_token (no nonce):**
Dex is configured with `expiry.idTokens: 24h`. Within that window, if the
`/auth` request carries no `nonce` parameter, Dex can return the **same JWT string**
it issued during the previous login. PostLogin accepted it, issued that JWT as the
`access_token`. The first `GET /sync` hit the in-memory denylist (the JWT was
invalidated on logout) and returned `401 M_UNKNOWN_TOKEN`. Element interpreted
the 401 as an unauthenticated state and redirected to `#/welcome`.

**Path B — Safari cached 302 redirect (no Cache-Control: no-store):**
Safari aggressively caches HTTP 302 redirects. On re-login, Safari replayed the
**old SSO redirect URL** (containing a state parameter that was already consumed
and deleted from `globalSSOState`). `globalSSOState.pop()` returned `(entry, false)`,
triggering a `400 M_UNKNOWN "Invalid or expired SSO state"` error, after which
Element landed on `#/welcome`.

**Path C — Defense-in-depth gap (no denylist check in PostLogin):**
Even if Path A's id_token had a different nonce (e.g., via direct `POST /login`
with a raw JWT fallback), PostLogin did not consult the denylist. A client submitting
a previously invalidated JWT directly would receive a valid `access_token`, and the
subsequent `/sync` would again return 401.

---

## Three-Layer Fix (already implemented)

### Fix 1 — Nonce in GetSSORedirect + verification in GetSSOCallback

**File:** `gateway/internal/matrix/sso.go`

- `GetSSORedirect` now generates a 16-byte random nonce (32 hex chars), stores it in
  `globalSSOState` alongside the PKCE verifier, and passes it to Dex via
  `oauth2.SetAuthURLParam("nonce", nonce)`.
- `GetSSOCallback` now extracts the `nonce` claim from the returned id_token and
  compares it against the stored nonce. A mismatch → `403 M_FORBIDDEN
  "SSO nonce mismatch"` — the flow is aborted before any opaque loginToken is issued.

This neutralises Path A: even if Dex serves a cached JWT, the stale JWT carries the
old nonce which does not match the freshly generated one stored in `globalSSOState`.

### Fix 2 — Denylist check in PostLogin

**File:** `gateway/internal/matrix/login.go`

`PostLogin` now calls `h.store.IsInvalidated(rawJWT)` before returning a successful
response. If the JWT is in the denylist, it returns `403 M_FORBIDDEN
"Token has been logged out — please log in again"`.

This is defence-in-depth for Path C (direct `POST /login` with a raw JWT) and
provides a second safety net in case Path A's nonce check is somehow bypassed.

The `LoginHandler.store` field (`middleware.TokenStore`) is optional (nil-safe); when
nil, the check is skipped (backwards-compatible with deployments not using a denylist).

**Note on deliberate status-code divergence:** `PostLogin` returns `403 M_FORBIDDEN`
(not `401 M_UNKNOWN_TOKEN`) for denylist hits. `JWTMiddleware` returns `401
M_UNKNOWN_TOKEN` for the same condition on authenticated endpoints. The difference is
intentional: `POST /login` is an authentication *attempt*, not a session validation.
`403` signals the credential itself is forbidden; `401` would imply the request lacks
authentication, which is misleading on a login endpoint. This is documented in a
code comment next to the denylist check in `login.go`.

### Fix 3 — Cache-Control: no-store on SSO redirect

**File:** `gateway/internal/matrix/sso.go`

`GetSSORedirect` sets `w.Header().Set("Cache-Control", "no-store")` before the
`http.Redirect` call. This instructs Safari (and all caches) never to store the
302 response, eliminating Path B.

### Fix 4 — prompt=login in OIDC auth URL (already in place from Story 5.21)

`GetSSORedirect` also passes `oauth2.SetAuthURLParam("prompt", "login")` to force
Dex to show the credential form regardless of any existing Dex session cookie.
This pre-existing fix ensures the user always re-authenticates interactively.

### Fix 5 — SSO state store capacity cap at 10,000 entries

`ssoStateStore.save` returns an error and `GetSSORedirect` responds with
`429 M_LIMIT_EXCEEDED` when the store holds ≥ 10,000 pending states.
This prevents unbounded memory growth from unauthenticated flood attacks.

---

## Acceptance Criteria

**AC1 — Nonce present in OIDC authorization URL:**
Given a valid `redirectUrl`,
When `GET /_matrix/client/v3/login/sso/redirect?redirectUrl=http://localhost:7070/` is called,
Then the `Location` header contains a `nonce` query parameter that is exactly 32 hex characters.

**AC2 — Cache-Control: no-store on SSO redirect:**
Given a valid `redirectUrl`,
When `GET /_matrix/client/v3/login/sso/redirect?redirectUrl=http://localhost:7070/` is called,
Then the response contains `Cache-Control: no-store`.

**AC3 — Nonce mismatch rejected in GetSSOCallback:**
Given the `globalSSOState` holds a state entry with nonce "aabbccdd…" (32 hex),
When `GetSSOCallback` receives an id_token whose `nonce` claim is "11223344…" (different),
Then the response is `403 M_FORBIDDEN "SSO nonce mismatch"` and no loginToken is issued.

**AC4 — Denylist check rejects invalidated JWT in PostLogin:**
Given a JWT that was previously invalidated via logout (i.e. `denylist.Invalidate(jwt, exp)` called),
When `POST /_matrix/client/v3/login` is called with `{"type":"m.login.token","token":"<that JWT>"}`,
Then the response is `403 M_FORBIDDEN` with `errcode: "M_FORBIDDEN"`.

**AC5 — SSO state store caps at 10,000 entries:**
Given `globalSSOState` already holds 10,000 entries (none expired),
When a 10,001st SSO redirect request is attempted,
Then the response is `429 M_LIMIT_EXCEEDED`.

**AC6 — Playwright E2E: logout → re-login lands on room list (not #/welcome):**
Given alex is logged in via Element Web and is on the room list,
When alex logs out (user menu → "Sign out" / "Remove this device"),
And alex performs a fresh SSO login via Dex immediately afterwards (no browser restart),
Then Element Web lands on the room list (`.mx_LeftPanel` visible),
And the URL does NOT contain `#/welcome`.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **TestSSORedirect_PromptLoginParameter** (extended) — Go unit test (`gateway/internal/matrix/sso_test.go`)
   - Given: OIDC mock server + `LoginHandler` with valid config
   - When: `GET /sso/redirect?redirectUrl=http://localhost:7070/`
   - Then: `Location` contains `prompt=login`, `nonce` (32 hex chars), and response has `Cache-Control: no-store`
   - Status: WRITTEN AND GREEN (covers AC1 + AC2)

2. **TestPostLogin_DenylistRejectsInvalidatedJWT** — Go unit test (`gateway/internal/matrix/login_test.go`)
   - Given: JWT signed by mock OIDC server, pre-added to `middleware.NewDenylist()` via `Invalidate()`
   - When: `POST /login` with `{"type":"m.login.token","token":"<jwt>"}`
   - Then: HTTP 403, `errcode: "M_FORBIDDEN"`
   - Status: WRITTEN AND GREEN (covers AC4)

3. **TestSSOStateStore_Rejects10001stEntry** — Go unit test (`gateway/internal/matrix/sso_test.go`)
   - Given: `ssoStateStore` filled with exactly 10,000 non-expired entries
   - When: `store.save("state-overflow", ...)` called
   - Then: returns non-nil error
   - Status: WRITTEN AND GREEN (covers AC5)

4. **Nonce mismatch test** — Go unit test (to be written in `gateway/internal/matrix/sso_test.go`)
   - Given: `GetSSOCallback` receives a `code` + `state` with stored `nonce="abc..."`,
     but the id_token returned by the mock Dex carries `nonce="xyz..."` (different)
   - When: `GetSSOCallback` is invoked
   - Then: HTTP 403, body `"SSO nonce mismatch"`
   - Status: GREEN (covers AC3)

5. **Playwright+Cucumber E2E: logout → relogin lands on room list** —
   `e2e/features/element/login.feature` (new scenario `@ac6-safari-relogin`) +
   `e2e/step-definitions/element/login.steps.ts` (new step definitions)
   - Given: alex is logged in and on the room list
   - When: alex logs out, then immediately performs SSO login (same browser session, no restart)
   - Then: `.mx_LeftPanel` is visible, URL does NOT contain `#/welcome`
   - Status: GREEN (covers AC6)

---

## Implementation Notes

### Files Modified (already done — no re-implementation needed)

| File | Change |
|---|---|
| `gateway/internal/matrix/sso.go` | nonce generation + storage in `ssoStateEntry`; nonce verification in `GetSSOCallback`; `Cache-Control: no-store` header; `ssoStateStore.save` now returns `error`; cap 10,000 |
| `gateway/internal/matrix/login.go` | denylist check in `PostLogin` via `h.store.IsInvalidated(rawJWT)` |
| `gateway/internal/matrix/sso_test.go` | Extended `TestSSORedirect_PromptLoginParameter` (nonce + Cache-Control); `TestSSOStateStore_Rejects10001stEntry` |
| `gateway/internal/matrix/login_test.go` | `TestPostLogin_DenylistRejectsInvalidatedJWT` |

### Files That Need New Tests (this story's remaining work)

| File | What to add |
|---|---|
| `gateway/internal/matrix/sso_test.go` | `TestSSOCallback_NonceMismatch` — mock OIDC server returns id_token with wrong nonce |
| `e2e/features/element/login.feature` | New scenario `@ac6-safari-relogin`: logout → immediate SSO relogin → room list |
| `e2e/step-definitions/element/login.steps.ts` | Step: `{word} logs out and immediately logs back in via SSO` (or reuse existing logout + login steps) |

### `ssoStateEntry` struct (current state after fix)

```go
type ssoStateEntry struct {
    verifier    string
    redirectURL string
    nonce       string   // NEW: 32 hex chars (16 random bytes), verified in GetSSOCallback
    exp         time.Time
}
```

### `ssoStateStore.save` signature change

```go
// Before fix (no error return):
func (s *ssoStateStore) save(state, verifier, redirectURL string, ttl time.Duration)

// After fix (returns error when at capacity):
func (s *ssoStateStore) save(state, verifier, redirectURL, nonce string, ttl time.Duration) error
```

This is a breaking change inside the `matrix` package only. All call sites in
`sso.go` are already updated. Tests referencing the old signature were already
updated in `sso_test.go`.

### LoginHandler.store nil-safety

`PostLogin` guards the denylist check:

```go
if h.store != nil && h.store.IsInvalidated(rawJWT) {
    ...
}
```

`h.store` is set via `LoginConfig.Store` (optional). When `nil` (e.g., test
setups that do not inject a denylist), the check is skipped. This preserves
backwards compatibility with all existing unit tests that use `NewLoginHandler`
without a `Store`.

### Playwright E2E — Relogin Scenario Design

The new Playwright scenario should:

1. Reuse the existing `{word} is logged in via Element Web` step (from
   `login.steps.ts` / `common/room-setup.steps.ts`) to establish the initial session.
2. Reuse the existing `{word} opens the user menu and clicks {string}` step to
   perform logout (maps "Sign out" → "Remove this device" in Element 1.12.15).
3. After logout, **do NOT clear browser storage** (this is the critical path — the
   bug was triggered by the browser replaying a cached state, not by a fresh browser).
4. Call the full SSO login flow again: SSO button → Dex credentials → redirect back.
5. Assert `.mx_LeftPanel` is visible and URL does not contain `#/welcome`.

The scenario tag `@ac6-safari-relogin` allows it to be run selectively in CI:

```bash
npx playwright test --grep @ac6-safari-relogin
```

### OIDC / Auth Testing Standard

Per CLAUDE.md: All Gherkin tests involving OIDC must use Authorization Code + PKCE.
Never use `grant_type=password` shortcuts. Use `DEX_TEST_PASSWORD` from
`e2e/fixtures/users.ts`. Dex test user: `alex@example.com`.

---

## Dev Notes

- The three Go unit tests (`TestPostLogin_DenylistRejectsInvalidatedJWT`,
  extended `TestSSORedirect_PromptLoginParameter`, `TestSSOStateStore_Rejects10001stEntry`)
  are already written and passing. Run `make test-unit-go` to verify.
- The remaining work is: one more Go unit test (`TestSSOCallback_NonceMismatch`)
  and the Playwright+Cucumber E2E scenario (`@ac6-safari-relogin`).
- The `ssoStateStore.save` signature change (adds `nonce string` parameter, returns
  `error`) is already implemented. Do not revert it.
- The `loginTokenStore` is unchanged by this story — it remains a single-use opaque
  token store with 30s TTL.
- The `globalLoginTokens` store is NOT involved in the nonce or denylist checks.
  Those checks happen on the raw JWT extracted after popping the opaque token.
- Do NOT add `prompt=login` again — it is already present in `sso.go` from Story 5.21.
  This story only adds the nonce + Cache-Control + denylist pieces.

---

## Definition of Done

- [x] `TestSSOCallback_NonceMismatch` written and green (covers AC3)
- [x] `make test-unit-go` passes (all existing + new Go tests)
- [x] Playwright E2E scenario `@ac6-safari-relogin` written in `.feature` + step definitions
- [x] `make test-integration` or `npx playwright test --grep @ac6-safari-relogin` passes
- [ ] `security_review: required` — Kassandra security review passed (auth/SSO changes)
- [x] No regression in existing login, SSO, and logout test scenarios
