# Security Review — Story 5.29c (Audit & Crypto Lifecycle) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` — 24 files, ~2356 insertions / 18 deletions
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

Story 5.29c closes seven Epic-5 lifecycle gaps and the implementation is largely sound: JWT revocation via `token_hash` lookup, the `iss`/`aud` claim check, the `BEFORE INSERT` event-time trigger, the retention upper-bound, the action allowlist, and AES-256-GCM key-at-rest encryption are all wired correctly. One HIGH finding stands out — the new `POST /api/v1/admin/compliance/sessions/{id}/revoke` route is mounted behind `sessionGuard` but **without** the `csrf` middleware that protects every other state-changing admin POST in this gateway. Three MEDIUM findings concern the production posture of the AES-GCM master key (insecure dev default, no rotation path, no boot-time refusal in non-dev mode). Token-hash format is consistent (BYTEA, raw 32 bytes) between issue and validate; AES-GCM nonce handling (random per encrypt, prepended in ciphertext) is correct; the trigger is implementation-correct and `nebu_app` cannot bypass it.

## Findings

### [HIGH] CSRF middleware missing on compliance-session revoke endpoint

- **CWE / OWASP:** CWE-352 / A01:2021 — Broken Access Control (CSRF)
- **Datei:** `gateway/cmd/gateway/main.go:843-844`
- **Beschreibung:** The new route `POST /api/v1/admin/compliance/sessions/{sessionId}/revoke` is wired with `sessionGuard(http.HandlerFunc(revokeSessionHandler.RevokeSession))` only. Every other state-changing admin POST behind `sessionGuard` in this file is wrapped with the `csrf` middleware (lines 295 logout, 318/319 bootstrap, 322 select-claim). The pending-count handler at line 766 is GET and therefore exempt; this revoke endpoint is POST and is not. An authenticated `instance_admin` who visits an attacker-controlled page can have a forged form-POST silently revoke any compliance session by id — a state-changing escalation primitive against a compliance audit-trail control.
- **Impact:** A compliance session held by another officer can be revoked by any third party who lures the admin to a malicious page. Revocation invalidates a legitimate audit-export workflow mid-stream and emits a `compliance_session_revoked` audit row attributed to the victim. Reputationally relevant when the very feature being protected (compliance lifecycle) is the one bypassed.
- **Empfehlung:** Wrap the route with `csrf` and `bodyLimit64KiB` to match the pattern used by `POST /admin/logout` (line 295):
  `mux.Handle("POST /api/v1/admin/compliance/sessions/{sessionId}/revoke", bodyLimit64KiB(csrf(sessionGuard(http.HandlerFunc(revokeSessionHandler.RevokeSession)))))`. Confirm the admin UI submits the existing CSRF cookie + header pair as the dashboard does for other POSTs.
- **Referenz:** OWASP ASVS V4.2.2, Story 5.13 acceptance criterion (CSRF middleware on admin POST).

### [MEDIUM] Insecure dev default for `NEBU_KEY_ENCRYPTION_KEY` (zero-key)

- **CWE / OWASP:** CWE-1188 / CWE-321 — Insecure default initialization / Hardcoded cryptographic key
- **Datei:** `gateway/cmd/gateway/main.go:775-780`
- **Beschreibung:** When `NEBU_KEY_ENCRYPTION_KEY` is unset the gateway falls back to a fixed 32-byte zero key (`"00…00"`) and only logs a `slog.Warn`. The same code path runs in production builds — there is no `cfg.Env == "production"` gate or matching hard-fail. A misconfigured deployment that loses the env variable will boot, encrypt the Ed25519 signing key with the zero KEK, and still produce valid compliance JWTs. Anyone with read access to `server_config.value` can decrypt the private key offline.
- **Impact:** Operationally the failure mode is silent — no 500, no fail-fast, only a warning log line. If the warning is missed in noisy boot logs, the operator believes the encrypted-at-rest invariant holds while it effectively does not. The Ed25519 signing key is the root of trust for compliance-export server signatures and JWT validation.
- **Empfehlung:** Refuse to boot when the env is unset *unless* an explicit dev opt-in is set, e.g. `NEBU_ALLOW_INSECURE_KEK=1`, or gate by `cfg.Env != "production"`. Alternatively read the KEK from a file path (`NEBU_KEK_FILE`) following the existing pattern for the internal secret.
- **Referenz:** OWASP ASVS V6.2.1 / V6.4.1, NIST SP 800-57 §5.2.

### [MEDIUM] No KEK rotation path; KEK compromise requires re-issue of all sessions

- **CWE / OWASP:** CWE-320 — Key Management Errors
- **Datei:** `gateway/internal/compliance/signing_key.go:62-126`, `gateway/cmd/gateway/main.go:809`
- **Beschreibung:** `EnsureComplianceSigningKey` and `LoadComplianceSigningKey` accept exactly one encrypt/decrypt pair. There is no envelope or DEK structure, no `key_version` column, and no migration path for re-encrypting `compliance_signing_key_priv` under a new master key. Once KEK is in service, rotation requires either a custom downtime migration or accepting that all in-flight compliance sessions become unreachable. The story explicitly acknowledges this trade-off but the code does not encode a path forward.
- **Impact:** A KEK leak (e.g. via env-var dump in a misconfigured log) cannot be remediated without manual DBA work. The blast radius of a KEK compromise is the entire compliance-export trust chain for the lifetime of the deployment.
- **Empfehlung:** Add a `key_version` field in the stored ciphertext envelope (`enc:<version>:<hex>`) and an out-of-band re-encrypt entry point in main.go gated by an admin command. Track as follow-up story; not blocking for 5.29c.
- **Referenz:** NIST SP 800-57 §8.2 (Cryptoperiod), OWASP ASVS V6.4.2.

### [MEDIUM] Multi-instance purge scheduler — no leader election, no jitter

- **CWE / OWASP:** CWE-820 / CWE-405 — Missing synchronization / asymmetric resource consumption
- **Datei:** `gateway/internal/audit/scheduler.go:56-70`, `gateway/cmd/gateway/main.go:253-269`
- **Beschreibung:** The purge scheduler ticks every 24h on every gateway instance. `audit_log_purge` is atomic per call, so correctness is preserved; however N instances all tick within seconds of each other on the same boot, generating N concurrent `DELETE FROM audit_log` statements and N audit-of-audit `slog.Info` lines per purge cycle. The story states leader election is acceptable to skip until FB-51-01, but there is no jittered start delay either, so simultaneous boots produce simultaneous DB locks on the audit table.
- **Impact:** Operational. Audit table locking on a horizontally scaled gateway during purge windows; no integrity loss, but observability noise and higher steady-state DB load.
- **Empfehlung:** Add a small `time.Duration` jitter (`time.Duration(rand.Int63n(int64(time.Hour)))`) before the first tick, or move the tick to a randomised initial offset. Document the leader-election deferral inline at the goroutine block and link the FB-51-01 follow-up.
- **Referenz:** OWASP ASVS V11.1.4 (anti-automation), CWE-820.

### [LOW] `MigrateLegacyPlaintextKey` length-only detection misses non-canonical hex

- **CWE / OWASP:** CWE-1023 — Incomplete Comparison
- **Datei:** `gateway/internal/compliance/signing_key.go:208-217`
- **Beschreibung:** Legacy detection refuses anything that is not exactly 128 chars. Acceptable in practice (the only producer was the pre-5.29c `ensureComplianceSigningKey` which always wrote `hex.EncodeToString(priv)` of a 64-byte key), but if any deployment had been edited manually to upper-case hex or with stray whitespace the migration silently no-ops and `LoadComplianceSigningKey` then returns `ErrPlaintextKey`, breaking startup. Boot order ensures this surfaces immediately, so the failure mode is loud — hence LOW.
- **Impact:** Boot failure on edge-case legacy formats. No security loss; refusal is the safer side.
- **Empfehlung:** Either accept `[A-Fa-f0-9]{128}` after `strings.TrimSpace`, or document the constraint in the story acknowledgment. Optional.
- **Referenz:** —

### [INFO] AES-256-GCM construction is correct

- **Datei:** `gateway/cmd/gateway/main.go:933-988`
- **Beschreibung:** `newAES256GCMEncrypt` uses `crypto/rand.Read` for a 12-byte nonce, prepends it to the ciphertext via `gcm.Seal(nonce, nonce, plaintext, nil)`, and `newAES256GCMDecrypt` validates `len(ciphertext) >= gcm.NonceSize()` before splitting. No nonce reuse, no fixed IV, no manual HMAC. Matches Nebu invariant.

### [INFO] Token-hash format consistent between issue and validate

- **Datei:** `gateway/internal/compliance/handler.go:568`, `gateway/internal/compliance/jwt.go:144`
- **Beschreibung:** Both call sites use `sha256.Sum256([]byte(tokenStr))` and pass the raw 32-byte slice into the BYTEA column `compliance_sessions.token_hash`. Hex/encoding mismatch concern is not present.

### [INFO] event_time trigger correctly bound to table-owner privileges

- **Datei:** `gateway/migrations/000025_audit_log_event_time_trigger.up.sql:18-35`
- **Beschreibung:** Trigger function runs as the owner; `nebu_app` cannot `SET session_replication_role = replica` (no BYPASSRLS, no SUPERUSER post-5.29a). Tests bypass via `nebu_migrate`-only `openSeedDB`. Backdate attack via app-role insertion is closed.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ (no new compliance-data read paths) |
| Audit-log immutability                      | ✅ (trigger does not relax UPDATE/DELETE; allowlist tightens write surface) |
| `instance_admin` notification (if in-scope) | ✅ (revoke is admin-self-action; not a new escalation) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (compliance JWT now verifies iss/aud; OIDC layer untouched) |
| Matrix Power Level checks                   | ✅ (not in scope) |
| No hardcoded secrets                        | ⚠️ (zero-byte dev KEK is a known-default; see MEDIUM-1) |
| TLS 1.3 enforcement                         | ✅ (untouched) |
| AES-256-GCM correctness                     | ✅ (random 12-byte nonce, prepended, no reuse) |
| Ed25519 verify-before-accept                | ✅ (signing-key encryption does not change verify path) |
| No secrets in logs / error messages         | ✅ (KEK warn line does not leak key material; errors wrap, never inline private bytes) |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 3 |
| LOW       | 1 |
| INFO      | 3 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning. The CSRF gap on the revoke endpoint must be fixed before the next release; the three MEDIUMs may be tracked as follow-up stories within Epic 5 wrap-up.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
