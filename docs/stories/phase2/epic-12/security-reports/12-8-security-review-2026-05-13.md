## Security Review — Story 12.8: OIDC Fail-Open Hardening

**Diff scope:** media/cmd/media/main.go (startup hardening), media/internal/upload/upload.go (TokenVerifier interface refactor + nil-verifier 503), test files
**Date:** 2026-05-13
**Reviewed by:** Kassandra (nebu-agent-kassandra)

### Findings

No security issues found.

### Detail

**Scope reviewed:**
- Injection: No SQL, no file paths, no shell commands. CLEAN.
- Web Application: No new HTML routes, no redirect_uri parameters. CLEAN.
- Authentication & Authorization:
  - Empty issuer check in main() + initOIDCVerifier: dual fail-fast. No bypass path. ✓
  - Retry loop: all code paths either return non-nil TokenVerifier or propagate error. ✓
  - nil-verifier 503: fail-closed path, no MVP bypass reachable. ✓
  - OIDCTokenVerifier.VerifyToken: go-oidc validates sig/exp/aud; sub extraction moved from handler. No alg confusion possible. ✓
  - Empty sub/name → 401 M_UNKNOWN_TOKEN. No bypass. ✓
- Secret Handling: No secrets in logs. Structured logging only. No timing attack surface (go-oidc JWKS, not string comparison). ✓
- Input Validation: No new routes, no new body-size exposure. ✓
- Cryptography: No crypto changes. ✓
- Infrastructure: No new migrations, no new HTML endpoints. ✓

**Pattern check (MEMORY.md):**
- `OIDC fail-open at startup`: Directly remediated by this story. ✓
- `uploader_user_id ≠ Matrix user ID`: Pre-existing accepted risk (raw sub claim stored). Not worsened by this story. Not a new finding.

### Summary

CRITICAL: 0 — no blocking issues
HIGH: 0 — no blocking issues
MEDIUM: 0
LOW: 0

**Verdict: APPROVED**

This story correctly closes the OIDC fail-open pattern documented in Kassandra's MEMORY.md. The TokenVerifier interface refactor improves testability without security regression. Nil-verifier is now fail-closed (503) instead of fail-open (any bearer accepted).
