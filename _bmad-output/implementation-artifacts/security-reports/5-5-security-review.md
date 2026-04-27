# Security Review — Story 5.5 (Compliance Session Handler) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (15 files, +2,740 / -5)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-opus-4-7[1m]`

## Executive Summary

Story 5.5 introduces crypto-sensitive code: a 24-hour Ed25519 compliance JWT, persisted signing key in `server_config`, sub-binding, expiry-audit. The crypto primitives are correct (EdDSA pinning, `crypto/rand` key generation, SHA-256 token hash, alg-confusion test). One MEDIUM finding concerns at-rest storage of the long-lived signing key in plaintext. One MEDIUM concerns missing `iat` future-check. One LOW concerns absent NTP-clock-skew tolerance. No CRITICAL findings — Story 5.5 holds together.

## Findings

### [MEDIUM] Compliance signing key stored as hex plaintext in `server_config`

- **CWE / OWASP:** CWE-312 (Cleartext Storage of Sensitive Information) / A02:2021 Cryptographic Failures
- **Datei:** `gateway/cmd/gateway/main.go:866-879`; `gateway/migrations/000003_server_config.up.sql:1-26`
- **Beschreibung:** `compliance_signing_key_priv` is the long-lived (no-rotation) Ed25519 signing key for 24 h compliance JWTs. It is persisted as `hex(64-byte private key)` in the `server_config` TEXT column with no encryption-at-rest. The Nebu codebase has a precedent for protected-at-rest secret storage (Story 4.7: X25519 + AES-256-GCM for PII). This key is materially more sensitive than other `server_config` entries (`server_name`, `bootstrap_completed`, `admin_group_claim`) because anyone who reads it can forge compliance access tokens for any compliance officer for the lifetime of the key.
- **Impact:** Any actor who reads `server_config` (DB read replica, backup, `pg_dump`, compromised admin shell) gains the ability to mint forged compliance JWTs at will until the key is rotated — which the application currently has no mechanism to do (`server_config` policy denies UPDATE). Forged compliance tokens grant export-level access to message history of arbitrary users. Dev report acknowledges this as "MVP trade-off" — same pattern as `internal_secret`, but the threat model is different: `internal_secret` is a node-cluster PSK used over an internal network; the compliance signing key produces user-facing access tokens whose exfiltration is silent and replays for 24 h.
- **Empfehlung:** Track as Epic-5 follow-up. Reuse the Story 4.7 envelope (`KMS-master-key wraps DEK; DEK-AES-256-GCM encrypts compliance_signing_key_priv`) — at minimum encrypt the row before persistence and decrypt at startup. Decision can be: accept-with-justification for MVP, but document the residual risk in `architecture.md` as a recognised gap and create a follow-up story (e.g. SEC-5-30 "Encrypt compliance_signing_key at rest"). Also: add a key-rotation mechanism — currently `server_config` UPDATE is denied by policy, so rotating the key requires a SECURITY DEFINER function or a manual SQL DDL change.
- **Referenz:** OWASP ASVS V6.4 (Secret Management); NIST SP 800-57 §5.4 (key storage)

### [MEDIUM] Missing `iat` future-check in `ValidateComplianceToken`

- **CWE / OWASP:** CWE-345 (Insufficient Verification of Data Authenticity) / A02:2021
- **Datei:** `gateway/internal/compliance/jwt.go:65-97`
- **Beschreibung:** The validator checks `exp > now()` and `sub == expectedSub` but does not verify that `iat` is in the past or within reasonable bounds. A token with `iat: 9999999999` is accepted as long as `exp > now`. Combined with the current 24 h `exp = now + 86400` issuance, a clock-skewed or malicious issuer (e.g. another service that gets access to the signing key) could mint tokens whose effective lifetime extends beyond the documented 24 h window because validators only enforce `exp`, not the `iat`-`exp` delta.
- **Impact:** Indirect — only exploitable if the signing key is misused (see MEDIUM finding above) or via a flaw in the issuer that lets a caller influence claims. Not a direct attack path today because issuance sets `iat` and `exp` server-side. Defence-in-depth: future code that allows a caller to influence `iat` would silently bypass the 24 h policy.
- **Empfehlung:** Add `if claims.Iat > time.Now().Unix()+60 { return nil, errors.New("iat in the future") }` and `if claims.Exp-claims.Iat > 86400+60 { return nil, errors.New("token lifetime exceeds policy") }`. The 60-second tolerance covers normal NTP drift.
- **Referenz:** RFC 7519 §4.1.6 (`iat` claim); OWASP ASVS V3.5.3

### [MEDIUM] `ensureComplianceSigningKey` INSERT omits `set_at` (NOT NULL)

- **CWE / OWASP:** CWE-754 (Improper Check for Unusual or Exceptional Conditions)
- **Datei:** `gateway/cmd/gateway/main.go:872-880`; `gateway/migrations/000003_server_config.up.sql:5-9`
- **Beschreibung:** The `server_config` schema declares `set_at BIGINT NOT NULL` with no default. All other writers in the codebase (`gateway/internal/admin/auth.go`, `gateway/internal/admin/bootstrap.go`) explicitly populate `set_at`. The new `ensureComplianceSigningKey` INSERT writes only `(key, value)` and will hit a NOT NULL constraint violation on first cold start. The function will return an error and `os.Exit(1)` — gateway never serves traffic. Not a confidentiality / integrity issue, but a hard availability failure on first startup that did not surface in unit tests (the function is called against the real DB only).
- **Impact:** First-deploy failure. No security degradation. Cited as MEDIUM because the security-relevant path (key persistence) is broken and the failure mode is silent until production rollout.
- **Empfehlung:** `INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3), ($4, $5, $6) ON CONFLICT (key) DO NOTHING` with `time.Now().Unix()` as `set_at`. Add an integration test that runs `ensureComplianceSigningKey` against a real Postgres in `//go:build integration` mode.
- **Referenz:** Story 5.5 AC11 (the key seeding contract)

### [LOW] Strict `exp` check without clock-skew tolerance

- **CWE / OWASP:** CWE-367 (TOCTOU between issuer and validator clocks)
- **Datei:** `gateway/internal/compliance/jwt.go:87`
- **Beschreibung:** `time.Now().Unix() > claims.Exp` rejects a token at exactly the second of expiry, with no NTP-skew tolerance. Standard JWT validators (RFC 7519 §4.1.4 reference implementations) typically allow a 30–60 s leeway. With Nebu's containerised deployment and `time.Now()` varying between gateway and Elixir nodes, edge-of-second false rejections are possible.
- **Impact:** Spurious 401s on legitimate calls at the second of expiry. Not a security weakening — fail-closed is the correct direction. Listed as LOW for completeness; the safer alternative is symmetric (`exp >= now - 60`).
- **Empfehlung:** Optional: introduce `const clockSkewTolerance = 60 * time.Second` and use `time.Now().Unix() > claims.Exp+int64(clockSkewTolerance.Seconds())`. Or accept the strict check.
- **Referenz:** RFC 7519 §4.1.4

### [LOW] No `jti` claim — bearer-token replay within 24 h

- **CWE / OWASP:** CWE-294 (Authentication Bypass by Capture-replay)
- **Datei:** `gateway/internal/compliance/jwt.go:33-41`
- **Beschreibung:** Compliance tokens carry `sub`, `compliance_request_id`, `room_id`, `time_range_*`, `iat`, `exp` — but no `jti`. Within the 24 h window the token is a stable bearer credential; if intercepted (TLS-MitM by a privileged operator, browser-history leak, log capture) the token replays freely. The `compliance_sessions.token_hash` column was added in this story — it can serve as a replay-detection store but is not used for validation in 5.5 (per scope, that lives in 5.6 / 5.7).
- **Impact:** Standard bearer-token risk. Bounded by 24 h window and `expires_at` revocation. Listed as LOW because Stories 5.6 / 5.7 are explicitly chartered to wire token-hash lookup into the export path.
- **Empfehlung:** Make sure 5.6 reads `compliance_sessions.token_hash` and rejects tokens whose hash is not in the active-session set, or whose row is `revoked_at IS NOT NULL`. (No change required in 5.5.)
- **Referenz:** OWASP ASVS V3.5.3

### [INFO] Algorithm pinning correctly enforced

- **Datei:** `gateway/internal/compliance/jwt.go:69`
- **Beschreibung:** `jose.ParseSigned(tokenStr, []jose.SignatureAlgorithm{jose.EdDSA})` rejects HS256, RS256, and `none` before signature verification. `TestValidateComplianceToken_AlgConfusion` exercises the HS256-with-pubkey-as-secret attack path. Story 5.18 lesson is correctly applied. Positive observation.

### [INFO] Bootstrap race correctly handled with re-read

- **Datei:** `gateway/cmd/gateway/main.go:866-908`
- **Beschreibung:** `INSERT … ON CONFLICT (key) DO NOTHING` followed by an unconditional re-read ensures concurrent gateway startups converge on the same persisted keypair. The locally-generated key is discarded if another writer won — which preserves any tokens already signed with the persisted key. Correct.

### [INFO] Audit-first / revoke-second ordering in `SessionExpiryWorker`

- **Datei:** `core/apps/compliance/lib/compliance/session_expiry_worker.ex:80-98`
- **Beschreibung:** Worker emits the `compliance_session_expired` audit *before* setting `revoked_at = NOW()`. If the process crashes between the audit and the UPDATE, the next tick re-processes the row → double-audit. This is the correct bias for a compliance trail (over-reporting > under-reporting). DoS-via-crash-loop is bounded by the once-per-hour cadence and the `LIMIT 1000`.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ (carries from 5.3 `justification`; 5.5 propagates `compliance_request_id`) |
| Audit-log immutability                      | ✅ (DELETE policy `USING (false)` on `compliance_sessions`) |
| `instance_admin` notification (if in-scope) | ⚠️ N/A for 5.5 |
| OIDC token validation (`iss`/`aud`/`exp`)   | ⚠️ Compliance JWT does not carry `iss`/`aud`. Token is purpose-specific (compliance-only), so per-issuer namespacing is implicit — but adding `iss="nebu-compliance"` and `aud="nebu-compliance-export"` would add defence-in-depth against future sharing of the signing key with another issuer. Not a blocking finding for 5.5. |
| Matrix Power Level checks                   | ⚠️ N/A for 5.5 |
| No hardcoded secrets                        | ✅ (key generated at first start via `crypto/rand`) |
| TLS 1.3 enforcement                         | ✅ N/A — handler runs behind existing TLS terminator |
| AES-256-GCM correctness                     | ⚠️ Not used. See MEDIUM finding on plaintext key-at-rest. |
| Ed25519 verify-before-accept                | ✅ (`ValidateComplianceToken` parses with strict alg allow-list, then `tok.Verify(pubKey)` before reading claims) |
| No secrets in logs / error messages         | ✅ (signing key never logged; `slog.Error` lines log only `err`, not the key bytes) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 3 |
| LOW       | 2 |
| INFO      | 3 |

## Pipeline Decision

- **CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed.

The three MEDIUM findings should be tracked as Epic-5 follow-up stories (key-at-rest encryption, `iat` future-check, `set_at` INSERT correctness). The `set_at` MEDIUM is a functional bug that will surface on first cold deploy — recommend fixing before Epic-5 closes, separate from but tracked alongside the security follow-ups.

Carry-over (not re-reported per scope): FB-51-01 (`nebu` BYPASSRLS — `compliance_sessions` DELETE permitted by superuser), FB-52-01 (gRPC audit auth), FB-53-01 (rate-limit `/api/v1/compliance/*`) — all tracked under Story 5.29.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
