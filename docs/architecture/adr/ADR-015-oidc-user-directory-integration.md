# ADR-015: OIDC User Directory Integration Strategy

## Status

Accepted

## Context

Epic 14 adds two capabilities that require reading users from an external identity provider:

1. **Admin UI User Search** — when an admin searches for a user, results should include OIDC users who have never logged into Nebu (displayed with a "Not yet logged in" badge).
2. **Bulk User Import** — an admin can pre-provision all users during Bootstrap Wizard Step 4 (or at any later point via the Admin UI) without waiting for each user to log in organically.

Both capabilities require Nebu to query the identity provider's user directory. No single standard protocol covers all identity providers in Nebu's target market:

- **Dex** (the default Nebu IDP) and **Keycloak** expose a provider-specific HTTP user list API (authenticated via Bearer token). Neither implements SCIM 2.0 natively in typical configurations.
- **Azure Active Directory**, **Okta**, **Ping Identity**, and similar enterprise IDPs do not expose non-standard user list endpoints. Their canonical directory protocol is **SCIM 2.0** (RFC 7644).

A single-protocol choice would leave one deployment class unsupported:

| Choice | Dex / Keycloak | Azure AD / Okta |
|---|---|---|
| OIDC-only (custom endpoint) | ✅ Works | ❌ No SCIM-free list API |
| SCIM-only | ❌ SCIM typically not configured | ✅ Works |
| Both (independent) | ✅ | ✅ |

### Constraints

- No dependency on a SCIM provisioning agent running inside the IDP (push model). Nebu pulls on demand.
- Configuration must be possible without code changes — toggles + endpoints in `server_config`.
- Credentials (bearer tokens, SCIM tokens) must never appear in logs.
- HTTPS is mandatory for both protocols — plaintext endpoints are rejected at startup validation.

## Decision

Nebu supports **two complementary, independently configurable directory protocols**. They are not mutually exclusive; an operator may enable both simultaneously, or neither.

### Protocol A — OIDC Directory Endpoint (custom, provider-specific)

**Use case:** User Search (live, cached), Bootstrap Wizard preview.

**Configuration:**

```
oidc_directory_enabled: boolean  -- default: false
oidc_directory_endpoint: text    -- HTTPS URL; required if enabled
```

**Behavior:**

- Gateway calls `GET {oidc_directory_endpoint}` with a `Bearer` token (the same token used for OIDC admin API calls, or a separately configured token).
- Response is expected to be a JSON array of objects containing at minimum the claims that map to `matrix_user_id_claim` and `displayname`.
- Results are cached in-process for **30 seconds** (TTL configurable via `oidc_directory_cache_ttl_seconds`).
- Rate limit: **5 requests per second per admin session**.
- On unreachable endpoint: returns empty list + warning log. No error propagated to the caller.
- Non-HTTPS endpoint: rejected with a startup validation error — Nebu refuses to start.

**Who uses this:** Dex, Keycloak (both expose user list APIs via their admin REST API), and any custom OIDC provider with a similar endpoint.

**Limitation:** The endpoint format is provider-specific. Nebu does not parse SCIM envelopes or any other structured schema — it expects a flat JSON array of claim objects.

### Protocol B — SCIM 2.0 (RFC 7644)

**Use case:** Bulk user import (`BulkImportUsers` gRPC + Bootstrap Wizard Step 4).

**Configuration:**

```
scim_enabled: boolean          -- default: false
scim_base_url: text            -- HTTPS base URL (e.g. https://idp.example.com/scim/v2)
scim_bearer_token: text        -- SCIM bearer token; stored encrypted in server_config
```

**Behavior:**

- Gateway calls `GET {scim_base_url}/Users` (pagination via `startIndex` + `count` parameters per RFC 7644 §3.4.2).
- Response is parsed as a SCIM `ListResponse<User>` envelope.
- Each SCIM `User` is mapped to a Nebu OIDC claim map using the `matrix_user_id_claim` configuration (e.g., `userName` → sub).
- Mapped claim maps are sent to Core via `BulkImportUsers` gRPC RPC — identical provisioning flow to first-login (`validate_token/2`).
- Import progress is tracked in-process and exposed via `GET /api/v1/admin/bootstrap/import-status` (live `{imported, total, failed}` counts).
- On unreachable SCIM endpoint: import fails with an explicit error — unlike Protocol A, bulk import is a user-initiated action with expected feedback.
- Non-HTTPS endpoint: rejected with a validation error at import-trigger time.

**Who uses this:** Azure Active Directory (via Enterprise Application SCIM provisioning), Okta (SCIM 2.0 app), Ping Identity, and any IDP with RFC 7644 support.

### Protocol Selection Matrix

| Capability | Protocol A (OIDC Endpoint) | Protocol B (SCIM 2.0) |
|---|---|---|
| Admin UI User Search | ✅ Primary | ❌ Not used (latency) |
| Bootstrap Wizard preview | ✅ Preview table | ❌ Not intended |
| Bulk Import (BulkImportUsers) | ❌ Not used | ✅ Primary |
| Live progress during import | N/A | ✅ SSE / polling |
| Provider: Dex, Keycloak | ✅ | ⚠️ Requires SCIM plugin |
| Provider: Azure AD, Okta | ❌ No standard endpoint | ✅ |

### Credential Security

- `scim_bearer_token` is stored encrypted (server-side encryption as per ADR-011 key escrow model) and never returned in API responses or written to logs.
- `oidc_directory_endpoint` bearer token reuses the OIDC admin credential path — same storage and masking rules.
- Both credentials are redacted in `DEBUG`-level HTTP trace logs.

## Module Structure

```
gateway/internal/admin/
  oidc_directory.go      # Protocol A: fetch, cache, rate-limit
  scim_client.go         # Protocol B: RFC 7644 GET /Users, pagination, mapping
  user_import.go         # BulkImportUsers orchestration (calls Core gRPC)
  import_status.go       # Progress tracking + SSE/polling endpoint
```

## Rejected Alternatives

### SCIM 2.0 Only

SCIM 2.0 is not available on Dex in a standard configuration. Enabling SCIM on Keycloak requires additional plugins. For the majority of Nebu's initial target deployments (Dex + Keycloak), this would make user directory integration impossible without significant IDP-side configuration.

### OIDC Directory Endpoint Only

Enterprises using Azure AD, Okta, or Ping Identity do not expose a non-SCIM user list endpoint that can be called with a simple Bearer token. SCIM 2.0 is the canonical interface for these systems. Without SCIM support, large-scale bulk import would require users to individually log in before they appear in Nebu.

### LDAP Direct

LDAP was explicitly rejected for three reasons:
1. No LDAP support is planned in Nebu's auth stack (OIDC-only per ADR — no SAML/LDAP directly).
2. LDAP credentials and firewall rules would add operational complexity.
3. Most modern IDPs that operators would configure with Nebu expose SCIM rather than requiring raw LDAP access.

### Push-based SCIM Provisioning (IDP → Nebu)

An alternative would be to implement a SCIM server endpoint in Nebu and let the IDP push user changes. This was considered but deferred:
- Requires persistent IDP-side configuration that survives Nebu restarts.
- Complicates the data model (SCIM Resource IDs alongside Matrix User IDs).
- Pull-on-demand is simpler for the MVP and sufficient for the bulk import use case.
- Can be added as ADR-016 if demand arises.

## Consequences

### Positive

- Both primary IDP families (Keycloak/Dex and Azure AD/Okta) are supported without requiring operators to configure non-native protocol adapters.
- The two protocols have strictly separated responsibilities — no code path handles both.
- SCIM 2.0 is a well-specified standard (RFC 7644); the client implementation is straightforward to test and validate against mock SCIM endpoints.
- Protocol A can be re-used by any provider that exposes a simple authenticated JSON user list, even if it is not OIDC-native.

### Negative / Risks

- Two separate code paths increase maintenance surface. If the Nebu claim mapping logic changes, both `oidc_directory.go` and `scim_client.go` must be updated.
- Protocol A's "flat JSON array of claim objects" expectation is not standardized. Provider-specific quirks (pagination, nested claims) may require per-provider adapters in the future.
- SCIM token rotation requires an explicit `PATCH /api/v1/admin/config` call from an admin — there is no automatic token refresh mechanism.
- Operators enabling both simultaneously for the same provider may cause redundant requests. Documentation must clearly recommend enabling only the protocol appropriate for their IDP.

### Open Questions (follow-up ADRs)

- **ADR-016**: OpenTofu state backend recommendation per deployment target.
- **ADR-017** (future): SCIM server endpoint (push model) if enterprise customers require IDP-initiated provisioning.
- Open question: Should SCIM pagination be configurable (`scim_page_size`, default 100), or is a fixed page size sufficient for the MVP?

_Decision date: 2026-05-16_
