# Security Review — Story 14-3a: BulkImportUsers gRPC RPC + Core Provisioning

**Date:** 2026-05-16
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Scope:** Staged diff for story 14-3a
**Classification:** CLEAN (0 CRITICAL, 0 HIGH)

---

## Findings

### LOW-1: system_role field in BulkImportUsersRequest not validated in Core

**Severity:** LOW
**Location:** `core/apps/session_manager/lib/nebu/session/bulk_importer.ex` → `import_one/2`

**Description:**
The `system_role` field from `OIDCUserClaims` is passed directly to `UserStore.upsert_user/2` without validation. A compromised Go gateway or an attacker with access to the internal gRPC channel could construct a `BulkImportUsersRequest` with `system_role = "instance_admin"` and create admin-level users via bulk import.

**Mitigation (existing):**
- PSK authentication on the gRPC channel (Node Registration, ADR-008)
- The Go gateway enforces admin-only access to the bulk import HTTP endpoint
- The gRPC channel is internal-only (no external exposure)
- Consistent with ADR G2: Core trusts the Go gateway completely

**Residual risk:** Internal attacker or compromised Go gateway node could escalate role via bulk import.

**Recommendation:** Document this trade-off in ADR G2 update. Optionally: validate that `system_role` is one of `["user", "instance_admin", "compliance_officer"]` in `BulkImporter`, and reject unknown roles with `{:error, :invalid_role}`. This is a defense-in-depth measure, not a blocker.

**Action:** Deferred to follow-up story (defense-in-depth). Does not block this story.

---

### INFO-1: PII flow — correct encryption verified

**Severity:** INFO

`display_name` (Tier 1) and `email` (Tier 2) follow the same encryption path as `validate_token`: `encrypt_operational_pii/2` (AES-256-GCM, server key) and `encrypt_sensitive_pii/2` (X25519 ECDH + AES-256-GCM, user's public key). Confirmed correct.

Neither field appears in any log output.

---

### INFO-2: No token forwarding in gRPC handler

**Severity:** INFO

`bulk_import_users/2` in `server.ex` does not accept or forward OIDC tokens. Claims arrive pre-validated from the Go gateway. No JWT parsing or verification happens in Core. Correct.

---

## Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 1 (deferred — known ADR G2 trade-off) |
| INFO | 2 |

**Result: CLEAN — story can be committed.**
