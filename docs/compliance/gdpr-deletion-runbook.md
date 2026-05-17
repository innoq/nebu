# GDPR Right to Erasure — Operations Runbook

Story 14.4 | GDPR Article 17 | Route: `DELETE /api/v1/admin/users/{userId}`

## Overview

This runbook describes the procedure for processing a GDPR Right to Erasure (Article 17) request in Nebu.
The endpoint orchestrates full PII removal and key destruction for a single user account.

## Prerequisites

- The operator must hold the `instance_admin` system role
- A second admin must perform the deletion (self-deletion is blocked by a four-eyes guard)
- The operator must be authenticated (valid JWT, not expired or denylist-invalidated)

## Deletion Pipeline

The `DELETE /api/v1/admin/users/{userId}` endpoint executes the following steps atomically from the
requester's perspective. Each step is described with its failure behavior.

| Step | Action | Failure Behavior |
|------|--------|-----------------|
| 1 | Role gate: require `instance_admin` | 403 M_FORBIDDEN — request rejected |
| 2 | Path param validation (`userId` ≤ 255 chars) | 400 M_BAD_JSON — request rejected |
| 3 | Self-delete guard (callerSub ≠ userId) | 403 M_FORBIDDEN — four-eyes approval required |
| 4 | gRPC `DeactivateUser` — `is_active=false`, all sessions destroyed | 404 on unknown user, 500 on internal error |
| 5 | gRPC `DeleteUserKeys` — private signing + encryption key material set to NULL | Best-effort: warns on failure, continues |
| 6 | DB anonymization — `profiles.displayname='Deleted User'`, `profiles.avatar_url=NULL`, `users.anonymized_at=<unix_ms>` | 500 — PII erasure MUST NOT be silently skipped |
| 7 | Audit log — emits `gdpr_deletion` event with actor + target | Never-raise: audit failure does not block 200 response |
| 8 | 200 `{"user_id": "...", "status": "gdpr_deleted"}` | — |

## Example Request

```bash
curl -X DELETE \
  -H "Authorization: Bearer $ADMIN_JWT" \
  https://your-nebu-instance/api/v1/admin/users/@alice:example.com
```

## Example Response

```json
{
  "user_id": "@alice:example.com",
  "status": "gdpr_deleted"
}
```

## Error Codes

| HTTP | errcode | Cause |
|------|---------|-------|
| 400 | M_BAD_JSON | Missing or oversized `userId` path parameter |
| 403 | M_FORBIDDEN | Caller is not `instance_admin`, or caller is attempting self-deletion |
| 404 | M_NOT_FOUND | User does not exist in the system |
| 500 | M_UNKNOWN | DB anonymization or gRPC DeactivateUser failed |

**Note on idempotency:** If the user was already deactivated (e.g., from a prior partial deletion), the endpoint
treats this as an acceptable state and continues with the remaining steps (key deletion, anonymization, audit).
The final response is still 200 `{"status": "gdpr_deleted"}`. This behavior is intentional to support
re-running a partial deletion without errors.

## Verification Queries

After a successful deletion, confirm erasure with the following SQL queries:

```sql
-- Confirm PII anonymization
SELECT displayname, avatar_url
FROM profiles
WHERE user_id = '@alice:example.com';
-- Expected: displayname = 'Deleted User', avatar_url = NULL

-- Confirm key material erased
SELECT key_type, private_key IS NULL AS key_erased
FROM user_keys
WHERE user_id = '@alice:example.com';
-- Expected: private_key = NULL for all rows

-- Confirm anonymization timestamp
SELECT anonymized_at
FROM users
WHERE user_id = '@alice:example.com';
-- Expected: non-NULL unix millisecond timestamp

-- Confirm no active sessions
SELECT COUNT(*)
FROM sessions
WHERE user_id = '@alice:example.com' AND revoked_at IS NULL AND expires_at > NOW();
-- Expected: 0

-- Confirm audit record
SELECT actor_user_id, action, target_id, outcome, inserted_at
FROM audit_log
WHERE action = 'gdpr_deletion' AND target_id = '@alice:example.com'
ORDER BY inserted_at DESC
LIMIT 1;
-- Expected: one row with outcome = 'success'
```

## Room History Preservation

Per GDPR recital 65, messages sent by the deleted user remain in room history.
The Matrix event timeline is an immutable append-only log — event content is preserved as sent.
The user's PII is removed from their profile, but message content in rooms is not scrubbed.
This is consistent with Matrix CS API v1.18 behavior.

## Rate Limits

The endpoint is governed by the `complianceRL` rate limiter: 10 requests/minute per IP (burst 10).
For bulk erasure operations, contact the system operator to temporarily raise the limit.

## Idempotency

The endpoint is idempotent with respect to deactivation (step 4 accepts `codes.AlreadyExists`
and continues). Re-running against an already-deleted user will re-apply anonymization and emit
a second audit record. Duplicate audit records are acceptable for traceability.
