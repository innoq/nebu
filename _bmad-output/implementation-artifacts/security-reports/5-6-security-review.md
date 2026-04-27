# Security Review — Story 5.6: Compliance Data Export + Ed25519 Signature — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (5 files, +1992 / -1)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL`, `model=claude-opus-4-7[1m]`

## Executive Summary

Story 5.6 adds `GET /api/v1/compliance/export` with dual-token auth (identity JWT + scoped X-Compliance-Token), strict-from-claims scope, parameterised SQL, Ed25519 document signing, and never-raise audit emission. The crypto path is sound: EdDSA-pinned token validation reused from 5.5, deterministic map-keyed JSON for signing reconstructable by the verifier, `crypto/ed25519.Sign` over canonical bytes excluding `server_signature`. One MEDIUM observation on TOCTOU between approval-revocation and export, plus an INFO note on signature-determinism dependency. Five Epic-5 carry-overs (FB-51-01, FB-52-01, FB-53-01, FB-55-01, FB-56-01) are intentionally not re-reported here — they remain tracked for Story 5.29.

## Findings

### [MEDIUM] No pre-flight `status='approved'` re-check on `compliance_requests` before export

- **CWE / OWASP:** CWE-367 (TOCTOU) / OWASP A01:2021 — Broken Access Control
- **Datei:** `gateway/internal/compliance/handler.go:704-707`
- **Beschreibung:** The pre-flight DB read selects `requester_user_id, approver_user_id` only — it does not re-validate `compliance_requests.status = 'approved'`. The compliance session token from Story 5.5 has a 24-hour TTL. If an approval is revoked, downgraded, or the row is administratively flagged after the token is issued but before export, the still-valid token will authorise the export download regardless. Authorisation state is captured at token-issuance time and never reconciled at use time.
- **Impact:** A compliance officer whose approval was withdrawn (e.g. policy change, four-eyes reversal in a follow-up story, GDPR-driven data freeze) can still exfiltrate the approved scope until token expiry. Audit trail will show `compliance_export_downloaded` for an officer whose authorisation was already rescinded — embarrassing in a regulator-facing audit.
- **Empfehlung:** Extend the pre-flight SELECT to include `status` and reject (e.g. `403 M_FORBIDDEN "Compliance request no longer approved"`) when `status != 'approved'`. Single column added to existing query, no extra round-trip:
  ```sql
  SELECT requester_user_id, approver_user_id, status FROM compliance_requests WHERE id = $1
  ```
  Acceptance criteria do not mandate this today; raise as MEDIUM follow-up rather than blocking 5.6.
- **Referenz:** OWASP ASVS V8.2.2 (Access control rules enforced server-side at every request)

### [INFO] Signed-doc determinism relies on undocumented Go map-marshal behaviour

- **CWE / OWASP:** N/A
- **Datei:** `gateway/internal/compliance/handler.go:789-812`
- **Beschreibung:** The signed payload is `json.Marshal(map[string]json.RawMessage{...})`. Go's `encoding/json` sorts map keys alphabetically since Go 1.12, and the code relies on this for byte-for-byte reproducibility by the verifier. The behaviour is stable but documented in the `encoding/json` package docs rather than the Go language spec. A code comment notes the alphabetical-sort property, which is the right hedge.
- **Impact:** None today — behaviour is consistent across Go 1.21–1.26. A future Go release that changes map-marshal ordering (extremely unlikely without a proposal cycle) would silently break verification of historical exports.
- **Empfehlung:** Optional — pin signature determinism behind an explicit canonicaliser (e.g. RFC 8785 JCS) when Story 5.29 / FB-56-01 lands canonical-JSON for compliance docs. For MVP, status quo is acceptable; the deferral is already documented in the file header.
- **Referenz:** RFC 8785 JCS, Go `encoding/json` package documentation

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ (carried in token claim from 5.3) |
| Audit-log immutability                      | ✅ (never-raise emits `compliance_export_downloaded`) |
| `instance_admin` notification (if in-scope) | n/a (export, not access-grant) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (identity JWT via `jwtMiddleware`) |
| Matrix Power Level checks                   | n/a (compliance-officer override path) |
| No hardcoded secrets                        | ✅ (signing key from `server_config`, FB-55-01 carry-over) |
| TLS 1.3 enforcement                         | ✅ (terminator-level, unchanged) |
| AES-256-GCM correctness                     | n/a (no symmetric crypto in this story) |
| Ed25519 verify-before-accept                | ✅ (`ValidateComplianceToken` verifies before claim use; export sig is signing-side, EUF-CMA secure) |
| No secrets in logs / error messages         | ✅ (slog logs request_id and err; no token bytes, no signing key, no event content) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 0 |
| INFO      | 1 |

## Pipeline Decision

- **CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The single MEDIUM is a TOCTOU class issue not covered by AC and not regression of any existing control; appropriate as a 5.29 follow-up. The INFO is a deferral note already acknowledged in the codebase.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
