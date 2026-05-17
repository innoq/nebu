---
status: done
epic: 13
story: 7
security_review: not-needed
matrix: true
ui: false
---

# Story 13.7: MSC2965 OIDC Discovery Endpoints — auth_issuer + auth_metadata

Status: done

## Story

As a Matrix client user,
I want Nebu to respond to the MSC2965 OIDC discovery endpoints (`auth_issuer` and `auth_metadata`),
So that OIDC-aware clients like Element Web can discover the OIDC configuration without showing a "misconfigured server" error.

**Size:** S

**Pre-Story:** Consult `/agent-oracle` for the correct MSC2965 spec: expected response format for both endpoints, difference between `unstable/org.matrix.msc2965/` and stable v1.x paths, and whether Nebu should serve both path variants.

**Background:**
Element Web (and other OIDC-aware clients) send the following requests on startup:
```
GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer
GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata
```
A 404 response triggers a "Your Nebu is misconfigured" error in the Element UI.
`auth_issuer` returns the OIDC issuer URL; `auth_metadata` returns the OIDC discovery document (`.well-known/openid-configuration`) or proxies to it.

---

## Acceptance Criteria

**AC1 — auth_issuer returns configured OIDC issuer:**
Given `GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer`,
When the endpoint is called (no auth required),
Then the response is `200 OK` with JSON body `{"issuer": "<NEBU_OIDC_ISSUER>"}` — the value of the configured `NEBU_OIDC_ISSUER` environment variable

**AC2 — auth_metadata proxies OIDC discovery document:**
Given `GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata`,
When the endpoint is called (no auth required),
Then the response is `200 OK` with the OIDC discovery document fetched from `<NEBU_OIDC_ISSUER>/.well-known/openid-configuration` (proxied or cached)

**AC3 — Stable path variants (if required by Oracle/spec):**
Given the oracle audit of MSC2965,
When stable path variants (`/_matrix/client/v1/auth_issuer` etc.) are also required by the spec,
Then both the `unstable/org.matrix.msc2965/` and the stable paths are registered (same handlers)

**AC4 — No auth required:**
Given both endpoints,
When called without an Authorization header,
Then the response is 200 (not 401/403)

**AC5 — OIDC provider unreachable → 503:**
Given the OIDC provider is temporarily unreachable (for `auth_metadata` proxy),
When the endpoint is called,
Then the response is `503` with `{"errcode":"M_UNAVAILABLE","error":"..."}` — not a 500 crash

**AC6 — Element Web shows no misconfiguration error:**
Given Element Web is opened and pointed at Nebu,
When the initial client startup requests complete,
Then no "misconfigured server" error appears and `auth_issuer` + `auth_metadata` return `200` in the browser console

**AC7 — Godog scenarios pass:**
Given `gateway/features/oidc_discovery.feature`,
When `make test-integration` runs,
Then the following scenarios pass:
  - "auth_issuer returns configured OIDC issuer URL"
  - "auth_metadata returns valid OIDC discovery document"
  - "Both endpoints require no authentication"
  - "auth_metadata returns 503 when OIDC provider is unreachable"

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**1. Godog scenario: "auth_issuer returns configured OIDC issuer URL" — FAILING**
- Given: `gateway/features/oidc_discovery.feature` does NOT exist
- When: `make test-integration` runs
- Then: scenario missing → fail
- [After implementation: `GET /unstable/org.matrix.msc2965/auth_issuer` returns `{"issuer":"http://keycloak:8080/realms/nebu"}`]

**2. Godog scenario: "auth_metadata returns valid OIDC discovery document" — FAILING**
- Given: endpoint not yet registered in gateway router
- When: `GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata` is called
- Then: 404 response
- [After implementation: 200 with openid-configuration JSON]

**3. Go unit test: `TestAuthMetadata_ProviderUnreachable`**
- Given: OIDC provider URL points to an unreachable server
- When: `auth_metadata` handler is called
- Then: response is 503 with `M_UNAVAILABLE` error code

**4. Godog scenario: "Both endpoints require no authentication"**
- Given: request without Authorization header
- When: both endpoints are called
- Then: 200 responses (no 401)
