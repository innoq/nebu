# Security Review — Story 12.9

**Diff scope:** Media gateway audit trail canonicalization — `formatMatrixUserID` + `sanitiseLocalpart` added to `media/internal/upload/upload.go`; mandatory `NEBU_SERVER_NAME` check in `media/cmd/media/main.go`; migration 000047 column comment; docker-compose.yml updated.

**Date:** 2026-05-13

**Reviewer:** Kassandra (nebu-agent-kassandra)

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `media/internal/upload/upload.go:formatMatrixUserID` | `serverName` stored verbatim — operator could set `NEBU_SERVER_NAME` to a value containing colons (e.g. `evil:admin`), producing `@alice:evil:admin` in `uploader_user_id`. Matrix user ID parsers splitting on `:` might be confused. | Add server name format validation at startup (e.g. reject values containing `:`). Operator-controlled, not user-controlled — low risk. |

---

## Detail

**Finding #1 — Server name not validated for Matrix format** [LOW]

`formatMatrixUserID(localpart, serverName string)` concatenates the sanitised localpart with `serverName` verbatim. If `NEBU_SERVER_NAME=evil:admin` (unlikely but possible), the stored `uploader_user_id` would be `@alice:evil:admin`. Since `serverName` comes exclusively from the operator-controlled environment variable (not user input), and the value is stored in an audit log field (not used for authentication decisions), the exploitability is negligible. The sanitised localpart already prevents injection. This is a hardening note.

**Remediation (optional, can be deferred):** In `main.go`, add a check that `serverName` does not contain `:` or other Matrix-invalid characters.

---

## Dimensions Reviewed

- SQL injection: formatMatrixUserID output stored via parameterized query ($7 in InsertMediaFile) — CLEAN
- Path traversal: no file paths involved — CLEAN
- Auth bypass: format change only affects audit record, not auth decision — CLEAN
- JWT validation: no changes to OIDC verification logic — CLEAN
- Secret handling: no tokens or secrets logged in new code paths — CLEAN
- Input validation: OIDC subject is token-verified before sanitisation — CLEAN
- Cryptography: no cryptographic changes — CLEAN
- Migration 000047: comment-only DDL, no RLS or constraint changes — CLEAN
- docker-compose.yml: `${NEBU_SERVER_NAME:-localhost}` default — safe for local dev — CLEAN

---

## Summary

CRITICAL: 0 — no blocking findings
HIGH: 0 — no blocking findings
MEDIUM: 0
LOW: 1 — advisory (operator-controlled server name not validated for Matrix format; deferred acceptable)

**Verdict: APPROVED — CLEAN**
