# Security Review — Story 5-29d (Test Infrastructure & Dev Hardening)

- **Reviewer:** Kassandra (bmad-security-review)
- **Date:** 2026-04-29
- **Diff base:** `git diff --staged` (pre-commit, branch `main`)
- **Story:** 5-29d — Test Infrastructure & Dev Hardening
  - AC1 — Fake DB conformance via `@behaviour` (test-only)
  - AC2 — FB-E5-08: Dex `password` grant removed
  - AC5 — FB-29c-1: KEK production hard-fail
  - AC6 — FB-29c-2: `enc:v1:` versioned envelope
  - AC7 — FB-29c-3: `NewPurgeSchedulerWithJitter`
  - AC3 (FB-53-02 XSS) — deferred to story 7-11
  - AC4 (FB-E5-09) — closed INFO, no code change

## Classification

**CLEAN** — no CRITICAL or HIGH findings. One MEDIUM (defence-in-depth), one LOW, two INFO.

## Severity counter

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH     | 0 |
| MEDIUM   | 1 |
| LOW      | 1 |
| INFO     | 2 |

## Specific concerns — verification

### 1. KEK validation — opt-in bypass

`gateway/cmd/gateway/kek_validation.go:38` uses an exact string compare `allowInsecure == "true"`. No case-folding, no whitespace tolerance — `"True"`, `" true"`, `"1"`, `"yes"` all fail. `kekHex == ""` is the only path that allows the all-zeros key, and that branch is gated by both `env=="production"` AND `allowInsecure=="true"` to bypass. The default `NEBU_ALLOW_INSECURE_KEK` is unset → empty string → hard-fail. Verified secure. **OK.**

### 2. enc:v1: envelope — AAD vs marker

`gateway/internal/compliance/signing_key.go:124` calls `encryptFn(privKey)` with no AAD. The version token `"enc:v1:"` is a **storage-format marker**, not an AAD-bound parameter of AES-256-GCM (`newAES256GCMEncrypt` in `main.go:980` calls `gcm.Seal(nonce, nonce, plaintext, nil)` — explicit nil AAD). Consequence: an attacker with DB-write access could substitute a future `enc:v2:` blob with an `enc:v1:` blob (or vice versa) and decryption would succeed — version is not cryptographically bound.

For Story 5-29d this is acceptable: there is only **one** version in existence, and DB-write is gated by RLS (`server_config` UPDATE requires `nebu_migrate`). However, the story's stated rationale ("future KEK rotation") will become a vulnerability the moment a v2 is added. → **MEDIUM-1.**

### 3. Plaintext-key migration — atomicity / race

`MigrateLegacyPlaintextKey` (signing_key.go:230) runs before `EnsureComplianceSigningKey`. Two gateway instances starting in parallel:

- Both `loadStoredValue` plaintext, both encrypt with their KEK (same KEK in any healthy deployment), both `UPDATE server_config SET value = 'enc:v1:...'`. Last writer wins; both ciphertexts decrypt to the same plaintext key — semantically equivalent. **No corruption risk.**
- One instance migrates, the other reads post-migration → already-`enc:`-prefixed → no-op (line 242). **OK.**
- A genuine race could leave two distinct ciphertexts (different nonces) in flight, but the `UPDATE … WHERE key=…` is a single SQL statement and PostgreSQL provides per-row write atomicity. The post-write `EnsureComplianceSigningKey` re-reads, so whichever blob is final is what's used.

Verified safe. **OK.**

### 4. `math/rand` jitter — predictability

`gateway/internal/audit/scheduler.go:96` uses top-level `rand.Float64()`. Under Go 1.20+ (this project: Go 1.26 per `go.mod`) the global `math/rand` source is auto-seeded with a random value at startup — not deterministic, but **not cryptographically random**. For purge-schedule timing the security model is "spread DB load across instances", not "hide a secret". An adversary predicting tick times gains no exploit path: the purge runs `audit.RunCleanup` with a server-side retention threshold, no client-controlled input, no oracle. **OK** — `math/rand` is appropriate. **INFO-1** for documentation only.

### 5. Dex password grant — residual ROPC paths

`grep -rn "grant_type=password\|ROPC"` over `gateway/`, `core/`, `dev/`, `e2e/`:

- Production code: **zero hits.**
- Test code: `gateway/test/integration/dex_password_grant_test.go` — this is the AC2 *negative* test; it intentionally attempts ROPC and asserts the request fails. Correct.
- Documentation references `password` grant only in CLAUDE.md / memory as an explicit "do not use" warning.

Verified clean. **OK.**

## Findings

### MEDIUM-1 — `enc:v1:` envelope is a marker, not AAD-bound

- **File:** `gateway/internal/compliance/signing_key.go:124,132`; `gateway/cmd/gateway/main.go:980` (`gcm.Seal(nonce, nonce, plaintext, nil)`)
- **CWE:** CWE-345 (Insufficient Verification of Data Authenticity); related to OWASP A02:2021 Cryptographic Failures.
- **Issue:** The version prefix `"enc:v1:"` is a **lexical marker** prepended to the hex of the AES-GCM ciphertext. The string `"v1"` is not passed as Additional Authenticated Data to GCM. When a `v2` envelope is introduced (the documented rotation use-case), an attacker with DB-write access can:
  1. Take a captured `enc:v2:<ct>` blob and rewrite it to `enc:v1:<ct>` — decryption with the v1 KEK fails (different key) → DoS, but not catastrophic.
  2. Take a `enc:v1:<ct>` blob and rewrite it to `enc:v2:<same ct>` — succeeds if the operator left v1 KEK as a fallback during rotation; downgrades to the older KEK silently.
- **Impact today:** None — only `v1` exists; RLS restricts UPDATE to `nebu_migrate`.
- **Impact at v2 introduction:** Cryptographic version-confusion / downgrade. CRITICAL the moment a v2 ships without a fix.
- **Recommendation:** When `v2` is added, pass the version token (and KEK identifier) as AAD: `gcm.Seal(nonce, nonce, plaintext, []byte("nebu/compliance-key/v1"))`. Decryption then fails closed if the version label is rewritten. Document this as a precondition of FB-29c-2 follow-up. **No fix required for 5-29d.**

### LOW-1 — `NEBU_ENV` value space is undocumented

- **File:** `gateway/cmd/gateway/kek_validation.go:37`; `gateway/internal/config/config.go:51`
- **Issue:** Hard-fail triggers only on the **exact** literal `"production"`. Operators using `"prod"`, `"PROD"`, `"Production"`, or a Kubernetes namespace label like `"prd"` would silently skip the guard. The empty default reinforces dev-mode.
- **Impact:** A misconfigured deployment that intends to run in production but uses the wrong env-string would boot with the all-zeros KEK, no error.
- **Recommendation:** Either accept a small set of synonyms (`prod`, `production`, case-insensitive) **or** reject any unknown env value with an explicit error. Document the allowed values in `CLAUDE.md` / Helm values / Compose defaults. Optional for 5-29d; raise as a follow-up.

### INFO-1 — `math/rand` global source is auto-seeded

- **File:** `gateway/internal/audit/scheduler.go:30,96`
- **Note:** Go 1.20+ auto-seeds the global `math/rand` source. Predictability is not a security concern for purge-tick timing (no oracle). Document this so a future contributor doesn't "fix" it by switching to `crypto/rand` (slower, no benefit) or by seeding manually with `time.Now()` (regression to pre-Go-1.20 behaviour).

### INFO-2 — Dex test artifact references intentional

- **File:** `gateway/test/integration/dex_password_grant_test.go`
- **Note:** Verified that all surviving `grant_type=password` strings are inside the AC2 negative test that asserts the configuration removal is effective. No production ROPC code path remains.

## Nebu invariants — pass/fail

| Invariant | Result |
|---|---|
| Compliance RSP (signed audit chain unbroken) | PASS — no audit-emission paths touched |
| Audit immutability | PASS — purge scheduler only DELETEs by retention; no edit |
| OIDC validation | PASS — no OIDC code touched; ROPC removal hardens auth |
| Matrix Power Levels | PASS — no permission code touched |
| Crypto primitives | PASS — AES-256-GCM, Ed25519, hex; no md5/sha1/DES |
| Secrets hygiene | PASS — KEK, internal_secret remain file/env only; no plaintext logs |

## Framework lenses applied

- **OWASP A02 (Cryptographic Failures):** MEDIUM-1 (version not AAD-bound)
- **OWASP A05 (Security Misconfiguration):** LOW-1 (env-value typo bypass)
- **OWASP A07 (ID & Auth Failures):** PASS (ROPC removed)
- **CWE-345 (Insufficient Verification):** MEDIUM-1
- **STRIDE — Tampering:** addressed by RLS + AAD (recommendation)
- **NIST SP 800-53 SC-12 (Key Establishment):** Hard-fail in production satisfies "explicit operator decision"

## Verdict

Story 5-29d is approved for commit. MEDIUM-1 is a **future-state finding** that materialises only when `enc:v2:` ships; track it as a precondition of the next KEK-rotation story. LOW-1 and the two INFOs are documentation-quality items, no code change required for this story.
