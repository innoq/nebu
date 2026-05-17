# Security Review — Story 13-9 (OIDC Deployment Profile: Dex Sidecar vs External)

**Diff scope:** Staged changes in `deploy/tofu/examples/stackit/` (variables.tf, main.tf, cloud-init.tftpl, terraform.tfvars.example, RUNBOOK.md). Replaces Keycloak sidecar with two-profile OIDC model: `oidc_mode = "dex"` deploys a Dex IdP sidecar on port 5556 (plain HTTP, in-memory storage, static client + static admin user); `oidc_mode = "external"` deploys no bundled IdP. Adds new `oidc_mode`, `dex_static_password_hash` variables; new `inbound_dex` security-group rule (port 5556, conditional); new Dex config write_file at `/opt/nebu/dex/config.yaml` (0600); conditional Dex service block in docker-compose with operator-supplied bcrypt hash; lifecycle preconditions; Keycloak PostgresFlex user+database resources removed.

**Date:** 2026-05-14
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Story file:** `docs/stories/phase3/epic-13/13-9-oidc-deployment-profile-dex-sidecar-vs-external.md`

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | HIGH | `deploy/tofu/examples/stackit/main.tf:80-92` (`inbound_dex` rule) + `cloud-init.tftpl:76-82` (static admin user) | Internet-exposed Dex IdP with hardcoded admin-group user — brute-force surface to instance-admin role | Restrict `inbound_dex` source to an operator-defined CIDR variable (`dex_allowed_cidr`, no `0.0.0.0/0` default) AND add an explicit `dex_static_password_min_length` or `dex_static_password_complexity` gate before production-like use. Mirror the SSH rule pattern (`# restrict to a bastion CIDR`) and surface the same warning in RUNBOOK "OIDC Profiles". |
| 2 | MEDIUM | `cloud-init.tftpl:51,72-73` + `main.tf:141` | Entire OIDC flow runs over plain HTTP in dex mode (gateway↔Dex, browser↔Dex, browser↔gateway:8008) — auth codes, id_tokens, and the `nebu-gateway` client secret traverse plaintext on every login | The story explicitly scopes dex mode to "test/demo". Strengthen the existing RUNBOOK warning into a `terraform.tfvars.example` BANNER comment AND add a `lifecycle.precondition` that requires `var.environment != "prod"` (or an explicit acknowledgement variable `dex_accept_plaintext_oidc = true`) when `oidc_mode = "dex"`. |
| 3 | MEDIUM | `variables.tf:147` (bcrypt validation) + RUNBOOK `htpasswd … sed 's/$2y/$2a/'` | Validation accepts `$2a$` / `$2b$` but not `$2y$` — the format htpasswd actually emits. Operators who copy-paste htpasswd output without the `sed` step hit a cryptic validation error; some will work around it by editing the variable validation or the documented command, weakening the hash class signal | Extend the `startswith` validation to accept `$2y$` (functionally identical to `$2a$` per OpenBSD/PHP convention) so the documented workflow no longer depends on a brittle `sed` post-processing step. Update the RUNBOOK generation command to remove the now-unnecessary `sed`. |
| 4 | LOW | `variables.tf:117-120` (`oidc_client_secret` validation) | Validation rejects `"` and `\` but allows newline (`\n`), carriage return (`\r`), `$`, and backtick — values that can break `.env` shell parsing or YAML interpolation downstream | Extend the second validation block: `condition = !can(regex("[\"\\\\\\n\\r`$]", var.oidc_client_secret))` (or split into individual `strcontains` clauses) and update the error message accordingly. |
| 5 | LOW | `cloud-init.tftpl:77-82` (`operator@example.com`, hardcoded `userID` UUID-like literal `operator-0000…0001`) | Static admin email + userID are deterministic across every dex-mode deployment. If multiple test environments are aggregated into one audit pipeline (e.g. shared SIEM), authentication events become indistinguishable; also predictable target for credential-stuffing | Surface operator email and group claim as variables (`dex_operator_email`, `dex_admin_group`) with safe defaults but no hardcoded literal in the template. Document rotation in RUNBOOK. |
| 6 | LOW | `variables.tf:140-150` (`dex_static_password_hash`) | Variable validates *format* of bcrypt hash but not strength of underlying password (operator can supply hash of "1234"). Combined with HIGH 1 (internet-exposed login) this is a meaningful weakening | Add a sentence to the variable description and RUNBOOK section explicitly requiring `length(password) >= 16` before hashing. Optionally enforce `cost >= 10` by parsing the `$2a$NN$` cost field in the validation expression. |
| 7 | INFO | `cloud-init.tftpl:21,32-40` + `main.tf:188-203` | Terraform state contains plaintext `internal_secret`, `oidc_client_secret`, `dex_static_password_hash`, and `pg_password`. State backend choice (encrypted vs unencrypted) is operator-controlled | Already documented in RUNBOOK line 5 — no change required. Pattern noted for MEMORY.md. |
| 8 | INFO | `variables.tf:35-39` (`network_cidr`) + `main.tf:231` (`acl = [var.network_cidr]`) | Pre-existing pattern documented in MEMORY.md ("Singleton-list passthrough into ACL/SG inputs"). Default `10.0.0.0/24` is safe, but no validation prevents future copy-paste of `0.0.0.0/0` into PostgresFlex ACL | Not introduced by 13-9 — flagged for separate hardening story. |

---

## Detail

### Finding #1 — Internet-exposed Dex login with hardcoded admin-group static user [HIGH]

**What the code does**

```hcl
# main.tf:80-92
resource "stackit_security_group_rule" "inbound_dex" {
  count             = var.oidc_mode == "dex" ? 1 : 0
  ...
  direction         = "ingress"
  port_range        = { min = 5556, max = 5556 }
  # No `ethertype` filter and no source-CIDR field — Stackit security-group rules
  # without an explicit allow-source default to 0.0.0.0/0 (matches inbound_ssh,
  # inbound_https, inbound_matrix behaviour in this same file).
}
```

Combined with `cloud-init.tftpl:76-82`:

```yaml
staticPasswords:
  - email: "operator@example.com"
    hash: "${dex_static_password_hash}"
    username: "operator"
    userID: "operator-00000000-0000-0000-0000-000000000001"
    groups:
      - instance_admin
```

**Why it's exploitable**

1. `inbound_dex` opens port 5556 to the entire internet whenever `oidc_mode = "dex"`. (Same wide pattern as `inbound_ssh`, but the SSH rule at least carries the inline comment "restrict to a bastion CIDR in production"; the new Dex rule has no such warning.)
2. The Dex login page is reachable at `http://<server_name>:5556/dex/auth` — discoverable via Shodan, certificate-transparency logs of the Nebu domain, or trivial guessing.
3. The static user is deterministically `operator@example.com` / `operator` and is placed in the `instance_admin` group claim. The Nebu Bootstrap Wizard documentation (RUNBOOK line 51) instructs operators to map `instance_admin` → admin role. **Successful login = full instance-admin access to Nebu**.
4. Dex `staticPasswords` does not implement rate limiting (upstream Dex issue #1620 — closed `wontfix`). bcrypt cost 12 (~250 ms/attempt) yields ~14k tries/hour against a single host. Against a 6-character password: < 24 h. Against an 8-character lowercase password: weeks — but the operator controls the password, and operators historically use short test passwords in test/demo environments ("changeme", "password", `${env}123`).
5. There is no defense-in-depth: no fail2ban, no `ufw` limit rules in cloud-init, no Cloudflare/WAF in front (ALB does TCP passthrough only).

The argument that "dex mode is test/demo only" does not retire this finding because:
- The variable default for `network_cidr` (private) is not mirrored for the dex SG rule — an operator following the RUNBOOK has no signal that port 5556 should be restricted.
- Test/demo environments are exactly where operators reuse weak passwords and exactly where instance-admin access can pivot into the Nebu source-of-truth database (`stackit_postgresflex_user.nebu.password` is in Terraform state, retrievable once the operator has admin shell).

**Concrete remediation**

Add to `variables.tf`:

```hcl
variable "dex_allowed_cidr" {
  description = "CIDR block allowed to reach the Dex IdP on port 5556 in dex mode. There is no safe default for internet exposure — operators MUST set this to the office or bastion CIDR. Use '0.0.0.0/0' explicitly only if you accept brute-force exposure of the instance-admin login."
  type        = string
  default     = null
  validation {
    condition     = var.dex_allowed_cidr == null || can(cidrhost(var.dex_allowed_cidr, 0))
    error_message = "dex_allowed_cidr must be a valid CIDR."
  }
}
```

Add a `lifecycle.precondition` on `stackit_server.nebu` requiring `var.oidc_mode != "dex" || var.dex_allowed_cidr != null`.

Add to `main.tf` `inbound_dex` rule a `remote_ip_prefix` (or whichever attribute Stackit SG rules expose for source CIDR — verify against the provider schema) set to `var.dex_allowed_cidr`. If the provider does not support source-CIDR on rules, document this gap and pair the dex profile with a documented `ufw` configuration in cloud-init.

Mirror the warning in RUNBOOK "OIDC Profiles" section under "When to use".

---

### Finding #2 — Plain-HTTP OIDC across the entire authentication flow [MEDIUM]

**What the code does**

- `main.tf:141` — `effective_oidc_issuer = "http://${var.server_name}:5556/dex"`
- `cloud-init.tftpl:51` — Dex's own `issuer:` config value matches
- `cloud-init.tftpl:72-73` — redirect URIs are `http://${server_name}:8008/_matrix/client/v3/login/sso/redirect/oidc` and `http://${server_name}:8008/admin/callback`

**Why it's exploitable**

Every OIDC step in dex mode traverses unencrypted HTTP across the public internet:

| Step | Channel | Sensitive payload |
|---|---|---|
| Browser → Dex `/auth` | HTTP :5556 | Login form (operator password, PKCE challenge) |
| Browser → gateway `/login/sso/redirect/oidc?code=…` | HTTP :8008 | Authorization code |
| Gateway → Dex token endpoint | HTTP :5556 (hairpin via VM public IP) | `oidc_client_secret` in POST body, code |
| Dex → Gateway (response) | HTTP :5556 | id_token JWT (operator identity + admin claim) |
| Gateway → browser `Set-Cookie` | HTTP :8008 | Session cookie or access token |

A passive network attacker on the same coffee-shop wifi or on any hop between the operator and the VM:
- Reads the operator password from the login form POST
- Reads the `oidc_client_secret` (would allow them to mint their own valid auth requests)
- Reads the id_token (signature-verified, but the bearer JWT itself is a credential pre-expiry)
- Reads the resulting Nebu session cookie

Notes:
- go-oidc validates signature over the id_token — content tampering is detected. But the `oidc_client_secret` and the session cookie are bearer credentials that don't need to be tampered with to be exploitable; observation alone is sufficient.
- The MITM hairpin between gateway and Dex is across `lo` (localhost-on-host) once SNAT completes — narrow attack surface inside the VM. The browser path is the wide one.

The story explicitly scopes dex mode to "test/demo environments". The story acknowledges this in §7.1 and the RUNBOOK warning. However, the warning is in prose; the `tfvars.example` and the variable default do not enforce it.

**Concrete remediation**

Two layered options:

1. **Defensive variable:** Add `dex_accept_plaintext_oidc = false` (default) and a precondition `var.oidc_mode != "dex" || var.dex_accept_plaintext_oidc` so that an unaware operator cannot apply dex mode without explicitly opting in. Error message names the risk.
2. **Banner in `terraform.tfvars.example`:** Move the existing comment to a `!!!! WARNING !!!!` block immediately above `oidc_mode = "external"` so it is impossible to set `"dex"` without seeing it.

Long-term: the TODO already documented in `main.tf:186-187` (host-based nginx routing with TLS) is the correct fix. Track it as a separate story before any non-dev use of dex mode.

---

### Finding #3 — bcrypt validation rejects `$2y$` (the htpasswd default) [MEDIUM]

**What the code does**

```hcl
# variables.tf:147
condition = var.dex_static_password_hash == null
         || startswith(var.dex_static_password_hash, "$2a$")
         || startswith(var.dex_static_password_hash, "$2b$")
```

Documented generation command in RUNBOOK and the variable description:

```
htpasswd -bnBC 12 '' 'yourpassword' | tr -d ':' | sed 's/$2y/$2a/'
```

**Why it's a problem**

`htpasswd` emits `$2y$` by default (OpenBSD's labelling convention for bcrypt). The documented command pipes through `sed 's/$2y/$2a/'` to satisfy the Terraform validation. This is fragile:

- An operator who runs only the first half of the command (`htpasswd -bnBC 12 '' 'yourpassword' | tr -d ':'`) gets a `$2y$` hash, pastes it into tfvars, and gets a cryptic `startswith` error.
- A small fraction of operators will "fix" the error by editing the validation block to accept `$2y$`, or — worse — by switching to a weaker hash format that the validation accepts (e.g., a precomputed `$2a$04$…` from an online tool, lowering cost).
- bcrypt `$2y$` and `$2a$` are functionally identical implementations; the leading prefix is purely a labelling convention. Rejecting `$2y$` provides no security benefit.

**Concrete remediation**

```hcl
validation {
  condition = var.dex_static_password_hash == null || (
    startswith(var.dex_static_password_hash, "$2a$") ||
    startswith(var.dex_static_password_hash, "$2b$") ||
    startswith(var.dex_static_password_hash, "$2y$")
  )
  error_message = "dex_static_password_hash must be a bcrypt hash ($2a$, $2b$, or $2y$ prefix)."
}
```

Remove the `sed 's/$2y/$2a/'` from the documented commands in both `variables.tf` and `RUNBOOK.md` once `$2y$` is accepted.

Optionally tighten cost: add `tonumber(substr(var.dex_static_password_hash, 4, 2)) >= 10` to enforce minimum work factor 10.

---

### Finding #4 — `oidc_client_secret` validation allows newline, CR, `$`, backtick [LOW]

**What the code does**

```hcl
# variables.tf:117-120
condition = !strcontains(var.oidc_client_secret, "\"")
         && !strcontains(var.oidc_client_secret, "\\")
```

**Why it's a hardening gap**

The secret value flows into two contexts:

1. `.env` file (`NEBU_OIDC_CLIENT_SECRET=${oidc_client_secret}`) — read by Docker Compose, which treats `${VAR}` references in env files literally and supports `\n` line splits. A secret containing `\n` would corrupt later env vars.
2. YAML literal inside Dex config (`secret: "${oidc_client_secret}"`) — newlines, `$`, and backtick are not special in a double-quoted YAML scalar, but the surrounding template may double-process `${…}` if expansion is enabled. The current cloud-init template appears safe, but adding belt-and-suspenders here costs nothing.

Operator-controlled value, low exploitability, hardening recommendation.

**Concrete remediation**

```hcl
validation {
  condition = !can(regex("[\"\\\\\\n\\r]", var.oidc_client_secret))
  error_message = "oidc_client_secret must not contain double-quote, backslash, newline, or carriage return."
}
```

---

### Finding #5 — Hardcoded operator email and userID in template [LOW]

**Location:** `cloud-init.tftpl:77-82`

```yaml
staticPasswords:
  - email: "operator@example.com"
    hash: "${dex_static_password_hash}"
    username: "operator"
    userID: "operator-00000000-0000-0000-0000-000000000001"
    groups:
      - instance_admin
```

The literal email, username, and userID are identical across every dex-mode deployment of Nebu. This:

- Makes audit-log correlation across environments brittle (the same `sub` claim everywhere).
- Provides a known username (`operator`) to attackers regardless of how the operator chooses the password — enumeration step is free.

**Remediation:** Surface as variables (`dex_operator_email`, `dex_admin_group`) with non-blocking defaults but template references rather than literals. Document in RUNBOOK that rotation/customisation is recommended.

---

### Finding #6 — No password-strength gate behind the bcrypt hash [LOW]

**Location:** `variables.tf:140-150`

The validation only checks bcrypt format. An operator can supply `bcrypt("1234")` and Terraform accepts it. Combined with HIGH 1 (internet-exposed login), this is a meaningful weakening: a 4-character password hashed at cost 12 still falls in minutes against a determined attacker.

**Remediation:** No way to validate plaintext strength after hashing — document loudly in the variable description and RUNBOOK that the underlying password must be ≥ 16 chars from a diceware or password-manager source. Strengthen by adding a cost-floor validation: parse `substr(hash, 4, 2)` as a number and require `>= 10`.

---

### Finding #7 — Secrets in Terraform state [INFO]

Already documented in RUNBOOK line 5 (encrypted state backend required, never commit `.tfstate`). The new `dex_static_password_hash` variable inherits this property. Noted for completeness; no code change required.

---

### Finding #8 — `network_cidr` flows into PostgresFlex ACL without internet-CIDR guard [INFO]

Pre-existing pattern noted in MEMORY.md "Singleton-list passthrough into ACL/SG inputs". Not introduced or modified by 13-9. Flagged for the epic-end retro; track as a separate hardening story for IaC variables.

---

## Summary

| Severity | Count | Action |
|---|---|---|
| CRITICAL | 0 | — |
| HIGH | 1 | **Block commit** — restrict `inbound_dex` source CIDR; mirror the SSH rule's bastion-CIDR pattern. |
| MEDIUM | 2 | Address before epic end — defensive precondition for dex-mode plaintext acceptance; accept `$2y$` bcrypt prefix to align validation with documented tooling. |
| LOW | 3 | Advisory — strengthen secret-character validation; parametrise operator identity; document password strength floor. |
| INFO | 2 | No action — pre-existing or already documented. |

**Verdict: BLOCKED** — Finding #1 (internet-exposed Dex login + hardcoded admin-group static user) is exploitable end-to-end and inconsistent with the project's existing pattern of marking world-open ingress ports with a "restrict in production" warning. Resolve before merging to feature branch tip. Findings #2 and #3 should also be addressed in the same cycle — both compound the risk surface and reflect simple, low-cost fixes.

---

## Recurring Patterns to Add to MEMORY.md

- **Internet-open IdP ingress in test/demo profiles** (Epic 13): Test/demo deployment modes that introduce a new SG rule (Dex, Keycloak, Authentik) must default to a non-internet source CIDR or carry an explicit `_allowed_cidr` variable. Free-form `0.0.0.0/0` defaults for IdP ports compound any weakness in the static-credential surface.
- **bcrypt validation prefix-mismatch with htpasswd output** (Epic 13): `$2a$` / `$2b$` / `$2y$` are functionally identical bcrypt; validation that rejects `$2y$` (the htpasswd default) creates a documentation footgun where operators bypass the validation rather than understand it.
- **Static-credential admin users in IaC templates**: When a bundled IdP creates a deterministic admin account (`operator@example.com` + `instance_admin` group), exposing the login surface to the internet is equivalent to a credential-stuffing target with admin-level outcome. Either parametrise the identity, restrict the network surface, or both.
