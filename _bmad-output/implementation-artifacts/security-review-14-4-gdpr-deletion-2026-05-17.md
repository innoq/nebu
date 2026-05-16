# Security Review — Story 14.4: GDPR Right to Erasure

**Diff scope:** `gdpr_delete.go` (258 lines, new handler), `login.go` (13 lines, M_USER_DEACTIVATED mapping), `main.go` (11 lines, route registration), `audit_writer.ex` (1 line, @known_actions), integration test steps (514 lines).
**Date:** 2026-05-17
**Reviewer:** Kassandra (nebu-agent-kassandra)

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `gdpr_delete.go:140-144` | Audit `"success"` outcome emitted even when `DeleteUserKeys` (step 5) silently fails — audit trail may overstate GDPR deletion completeness | Record key deletion outcome in audit metadata: `{"keys_deleted": "confirmed"}` or `{"keys_deleted": "failed_best_effort"}` |
| 2 | LOW | `login.go:251` | `M_USER_DEACTIVATED` response confirms account existence to authenticated callers — technical user enumeration for deactivated accounts | Matrix spec-required behavior; document in BOND.md accepted risks |

---

## Detail

**Finding #1 — Audit outcome overstates key deletion success** [LOW]

The audit event emitted in step 7 always carries `outcome: "success"`, even when step 5 (`DeleteUserKeys`) failed silently (it is best-effort and the handler continues on failure). An auditor reviewing the `audit_log` would see `gdpr_deletion / success` and have no indication that key material may not have been erased. For GDPR Article 17 compliance attestation, the audit record should reflect the actual state of each sub-operation.

Remediation: Update step 7 to pass a metadata map that captures the key deletion outcome:

```go
keysStatus := "confirmed"
if keysErr != nil {
    keysStatus = "failed_best_effort"
}
_ = auditpkg.LogEvent(auditCtx, h.CoreClient, callerSub,
    "gdpr_deletion", "user", userID,
    map[string]any{"keys_deleted": keysStatus},
    "success", "")
```

**Finding #2 — M_USER_DEACTIVATED confirms account existence to authenticated callers** [LOW]

The `PostLogin` change correctly maps `PERMISSION_DENIED + "deactivated"` message → `403 M_USER_DEACTIVATED`. However, the distinct error code (vs `M_FORBIDDEN` for non-existent users) allows an authenticated attacker to enumerate which userIDs are deactivated. This is technically user enumeration — but it is:

1. Required by the Matrix CS API v1.18 spec (servers MUST return `M_USER_DEACTIVATED` for deactivated accounts)
2. Accessible only to callers with a valid OIDC JWT (not unauthenticated enumeration)
3. Consistent with existing Matrix server implementations (Synapse, Dendrite)

Accept as by-design behavior. Document in BOND.md accepted risks.

---

## Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 2 (advisory, non-blocking) |

**Verdict: APPROVED**

The handler correctly implements:
- JWT auth + `instance_admin` role gate (middleware-layer + handler-layer defense-in-depth)
- Self-delete guard (four-eyes principle)
- Parameterized SQL queries (no injection vectors)
- Path traversal protection via `isSafePathSegment` (inherited from `user_anonymization.go`)
- Rate limiting via `complianceRL`
- Body size limit via `bodyLimit64KiB`
- Pipeline abort on `codes.NotFound` (MAJOR fix applied in code-review cycle 1)
- Never-raise audit policy (audit failure does not return 500)
- gRPC PERMISSION_DENIED → 403 M_USER_DEACTIVATED mapping verified against Elixir core implementation
