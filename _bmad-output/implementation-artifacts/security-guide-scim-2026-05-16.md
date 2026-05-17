# Security Implementation Guide: SCIM 2.0 Bulk Import Integration

**Story scope:** 14.3c — `gateway/internal/admin/scim_client.go` + `user_import.go` + `import_status.go`
**RFC reference:** RFC 7644 (SCIM Protocol), RFC 7643 (SCIM Core Schema)
**Prepared by:** Kassandra (Security Review Agent)
**Date:** 2026-05-16
**Status:** Pre-implementation — read before writing a single line of code

---

## What This Guide Covers

SCIM 2.0 bulk import introduces three attack surfaces that did not exist before Epic 14:

1. A Gateway service that makes authenticated outbound HTTP calls to an admin-configured URL (SSRF surface, credential exposure)
2. A bearer token stored in the database (credential-at-rest surface)
3. A long-running import operation with observable progress state (authorization + race condition surface)

The SCIM protocol itself (RFC 7644) is well-specified. The attack surface is not in the spec — it is in the gap between the spec and the implementation.

---

## CRITICAL Requirements (block the commit if violated)

### CR-1: Bearer token encrypted at rest, never returned in API responses

`scim_bearer_token` is stored in the `server_config` table. It MUST:

1. **Be encrypted at rest** using the server-side encryption model (ADR-011 key escrow). Raw plaintext MUST NOT appear in the `server_config` table.
2. **Never be returned** in `GET /api/v1/admin/config` responses. The API response for this field must be either:
   - Omitted entirely, or
   - A masked sentinel like `"[SET]"` or `"[NOT SET]"` (never the actual value or even its length)
3. **Never appear in logs** — same rules as OIDC directory bearer token:
   - Not in `slog.*` output at any level
   - Not in HTTP trace logs
   - Not in error messages returned to the API caller
   - Not in panic stack traces

```go
// WRONG — returns the token
type ServerConfigResponse struct {
    ScimBearerToken string `json:"scim_bearer_token"`
}

// CORRECT — returns masked status only
type ServerConfigResponse struct {
    ScimBearerTokenSet bool `json:"scim_bearer_token_set"`
}
```

### CR-2: No plaintext HTTP for SCIM endpoint

Same requirement as the OIDC directory guide (CR-1 there):
- Validation at `PATCH /api/v1/admin/config` — reject non-HTTPS `scim_base_url`
- Validation at import trigger time — refuse import if stored URL is non-HTTPS
- No redirect following (see OIDC guide CR-2 for the `CheckRedirect` pattern)

### CR-3: Import progress endpoint is admin-only

`GET /api/v1/admin/bootstrap/import-status` exposes:
- Total number of users in the SCIM directory (→ organization size leak)
- Number imported, skipped, failed (→ user enumeration state)

This endpoint MUST be behind the admin auth middleware. Verify in the router definition — not just in the handler.

Test: unauthenticated GET returns 401, not progress JSON.

### CR-4: SCIM claim mapping uses FormatUserIDFromClaims — no shortcuts

When mapping a SCIM `User` object to a Nebu Matrix User ID, the mapping MUST go through `FormatUserIDFromClaims` (the same function used at first-login). Do NOT:

```go
// WRONG — raw SCIM userName used directly as Matrix localpart
matrixUserID := fmt.Sprintf("@%s:%s", scimUser.UserName, serverDomain)

// CORRECT — through the same claim sanitization as first-login
claims := map[string]interface{}{
    cfg.MatrixUserIDClaim: scimUser.UserName,
    // ... other mapped claims
}
matrixUserID := gateway.FormatUserIDFromClaims(claims, cfg.MatrixUserIDClaim, serverDomain)
```

A SCIM `userName` containing `@`, `:`, spaces, or Unicode can produce an invalid or collision-inducing Matrix User ID if used without sanitization.

### CR-5: BulkImportUsers must be idempotent — no duplicate users on retry

Running import twice (admin clicks the button twice, or a first run times out and is retried) MUST be safe. The Core `BulkImportUsers` handler MUST:

1. Check for existing Matrix User ID before provisioning
2. Skip with `skipped: N` counter increment (not an error)
3. The PostgreSQL INSERT must use `ON CONFLICT DO NOTHING` or an explicit existence check — never a plain `INSERT` that throws on duplicate

This is both a data integrity and a security requirement: duplicate user records with different keypairs could corrupt the key escrow model (ADR-011).

---

## HIGH Requirements (block the commit if violated)

### HR-1: Pagination — enforce client-side page size and total cap

RFC 7644 §3.4.2 defines `startIndex` + `count` pagination. The SCIM server controls how many results it returns per page. A malicious or misconfigured SCIM server could return:
- An unbounded single page (ignoring `count`)
- Responses across thousands of pages

Enforce:

```go
const (
    scimPageSize    = 100           // max users per GET /Users request
    scimMaxTotal    = 100_000       // abort import if total exceeds this
    scimPageTimeout = 30 * time.Second  // per-page HTTP timeout
)
```

If `totalResults` in the first SCIM response exceeds `scimMaxTotal`, abort the import with a clear error to the admin. Do not OOM the Gateway.

### HR-2: SSRF — same as OIDC directory

`scim_base_url` is admin-configured. See OIDC guide HR-2 for the private-IP-blocking approach. Apply identically here.

If Option B (documented trust boundary) is accepted for Epic 14, the same comment block must appear in `scim_client.go`.

### HR-3: No concurrent imports — singleton lock

Two admins triggering import simultaneously creates:
1. Race condition on the in-process progress counter
2. Duplicate `BulkImportUsers` gRPC calls hitting Core concurrently → race on the PostgreSQL upsert
3. Misleading progress state (two separate `imported` counters merged into one endpoint)

Enforce a singleton import lock:

```go
var importMu sync.Mutex
var importInProgress atomic.Bool

func TriggerImport(...) error {
    if !importInProgress.CompareAndSwap(false, true) {
        return ErrImportAlreadyRunning  // HTTP 409 to the admin
    }
    defer importInProgress.Store(false)
    // ...
}
```

Return HTTP 409 Conflict if a second trigger arrives while import is running.

### HR-4: Audit log entry for every import

Every import invocation — including zero-result and failed imports — MUST produce an audit log entry containing:

| Field | Value |
|-------|-------|
| `event_type` | `scim_import_triggered` |
| `admin_user_id` | Matrix User ID of the triggering admin (from JWT, not from request body) |
| `timestamp` | UTC |
| `total` | SCIM totalResults count |
| `imported` | users provisioned |
| `skipped` | users already existing |
| `failed` | users that failed provisioning |
| `scim_base_url` | the endpoint called (for correlation) — NOT the bearer token |

This is required for GDPR Article 30 (records of processing) and for forensic correlation if import-related incidents occur later.

---

## MEDIUM Requirements (must fix before epic-end review)

### MR-1: SCIM error response handling — sanitize before logging/returning

RFC 7644 §3.12 defines the SCIM error response schema:

```json
{
  "schemas": ["urn:ietf:params:scim:api:messages:2.0:Error"],
  "status": "403",
  "detail": "Insufficient permissions to list users."
}
```

The `detail` field comes from the SCIM server and is untrusted. Do NOT:
- Return it verbatim to the admin in the API response (potential injection vector)
- Log it verbatim at INFO or above (potential sensitive data exposure — some IDPs include user claims in error details)

Do:
- Log the sanitized status code and a truncated (max 200 chars) `detail` at WARN level
- Return a Nebu-controlled error message to the admin: `"SCIM provider returned error: {status_code}"`

### MR-2: TLS certificate validation — never disable

The Go HTTP client MUST NOT have `InsecureSkipVerify: true` in its TLS config. This is an absolute requirement, not a preference.

```go
// WRONG — common shortcut that must never appear
client := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    },
}
```

If the SCIM server uses a self-signed certificate, the operator must configure a custom CA bundle via `NEBU_SCIM_CA_CERT_FILE` — not by disabling validation.

### MR-3: SCIM filter injection — document the constraint

The current Epic 14 scope only calls `GET /Users` (full user list). If a future story adds `GET /Users?filter=...` with user-provided search terms, that creates a SCIM filter injection vector (analogous to LDAP injection).

Add a comment to `scim_client.go`:

```go
// SECURITY: This client only supports full user list (GET /Users without filter).
// Adding filter expressions from user input requires proper escaping per RFC 7644 §3.4.2.2.
// Do not pass user-controlled strings directly into filter parameters.
```

### MR-4: Per-page response size limit

Same as OIDC guide CR-4 — each SCIM page response must be size-limited before parsing:

```go
const maxScimPageResponseBytes = 5 * 1024 * 1024  // 5 MB per page

body, err := io.ReadAll(io.LimitReader(resp.Body, maxScimPageResponseBytes))
```

### MR-5: Progress endpoint — no user data in response

`GET /api/v1/admin/bootstrap/import-status` MUST return only aggregate counts:

```json
{ "imported": 42, "total": 150, "failed": 0 }
```

MUST NOT return:
- Individual user records processed so far
- SCIM User IDs or usernames in the "failed" list
- Stack traces from failed individual user imports

Failed user details (for diagnosis) belong in server-side logs with admin access, not in the API response.

---

## SCIM RFC 7644 Protocol Compliance Checklist

The dev agent must implement these correctly. Deviations from the RFC create interoperability and security gaps.

| Requirement | RFC Reference | Implementation note |
|-------------|---------------|---------------------|
| `GET /Users` with `startIndex` + `count` params | §3.4.2 | startIndex is 1-based (not 0-based) |
| Parse `ListResponse.totalResults` for progress tracking | §3.4.2 | Use for max-cap check and progress denominator |
| Parse `ListResponse.Resources[]` as array of User objects | §3.4.2 | Each resource has `id`, `userName`, `displayName`, `emails[]` |
| Continue pagination until `startIndex + count > totalResults` | §3.4.2 | Exit condition — do not loop on empty Resources array alone |
| Accept `urn:ietf:params:scim:api:messages:2.0:ListResponse` schema | §3.4.2 | Validate `schemas` field before parsing |
| Handle HTTP 308 Permanent Redirect as error (no redirect following) | §3.1 | Same CheckRedirect rule as OIDC guide |
| `Authorization: Bearer {token}` header on every request | §2 | Token from encrypted storage, masked in logs |
| Accept `Content-Type: application/scim+json` response | §3.1 | Also accept `application/json` for lenient providers |

---

## Testing Checkpoints

| # | Test | Type | Must verify |
|---|------|------|-------------|
| T1 | Bearer token absent from GET /api/v1/admin/config response | Integration | CR-1 |
| T2 | Bearer token absent from any log output during import | Unit + inspection | CR-1 |
| T3 | Non-HTTPS scim_base_url rejected at config PATCH | Unit | CR-2 |
| T4 | Unauthenticated GET /import-status returns 401 | Integration | CR-3 |
| T5 | Second TriggerImport while first runs returns 409 | Unit | HR-3 |
| T6 | Duplicate user in SCIM — skipped, not errored, not duplicated in DB | Integration | CR-5 |
| T7 | SCIM server returns 50001 users — import aborts with error | Unit (mock) | HR-1 |
| T8 | SCIM page response >5 MB — truncated/errored gracefully | Unit | MR-4 |
| T9 | Import produces audit log entry with correct fields | Integration | HR-4 |
| T10 | `InsecureSkipVerify` absent from all TLS configs | Code inspection | MR-2 |
| T11 | FormatUserIDFromClaims used for Matrix ID derivation | Unit | CR-4 |
| T12 | Two imports in sequence produce stable counts (idempotency) | Integration | CR-5 |

---

## Anti-Patterns from Previous Epics (do not repeat)

| Pattern | Where it appeared | What to do instead |
|---------|-------------------|--------------------|
| Token returned in API response | (pre-emptive — common mistake) | Return `scim_bearer_token_set: bool` only |
| Raw `sub` / `name` used as Matrix User ID | Epic 12 Story 12.9 | `FormatUserIDFromClaims` always |
| Admin endpoint reachable without auth middleware | Epic 4 original review findings | Verify router chain, not just handler |
| `io.ReadAll` without size limit | Epic 12 Media | `io.LimitReader` before `io.ReadAll` |
| Race condition on shared counter | Epic 3 Story 3-8 (`sync.Map`) | `atomic.Int64` + singleton lock |
| `deletion_protection = false` default | Epic 13 IaC | Not directly relevant, but: never trust defaults on destructive operations |
