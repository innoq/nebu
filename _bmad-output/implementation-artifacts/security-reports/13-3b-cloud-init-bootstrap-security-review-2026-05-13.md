# Security Review — 13-3b cloud-init Docker Compose Bootstrap — 2026-05-13

**Agent:** Kassandra
**Diff base:** git diff HEAD (Story 13-3b, branch feature/phase-3-epic-13)
**Scope:** deploy/tofu/examples/stackit/ — cloud-init template, variables, main.tf, RUNBOOK.md
**Classification:** MEDIUM
**Config:** `blocking_severity=CRITICAL`, `model=claude-sonnet-4-6`

## Executive Summary

The cloud-init bootstrap implementation correctly avoids the most dangerous patterns: no hardcoded secrets, no bash-substitution tricks, no plaintext secrets in environment blocks. The bug-fixes applied (DATABASE_URL removal, trailing-newline fix, `Type=simple`, `package_upgrade: true`) are all clean and correct. One MEDIUM finding: the Postgres init SQL file is written with world-readable permissions (`0644`), which exposes the database password to any process on the host running as a non-root user (e.g. the `ubuntu` default user). The `user_data`/state-storage risk is correctly documented in both `main.tf` and `RUNBOOK.md` — that is the best available mitigation short of a Stackit Secrets Manager integration.

## Findings

### [MEDIUM] init-db.sql written with world-readable permissions (0644)

- **CWE / OWASP:** CWE-732 (Incorrect Permission Assignment for Critical Resource) / OWASP ASVS V2.10.4
- **File:** `deploy/tofu/examples/stackit/cloud-init.tftpl`, write_files entry for `/opt/nebu/init-db.sql`
- **Description:** The `init-db.sql` file contains `CREATE USER keycloak WITH PASSWORD '${db_password}'` and `CREATE USER nebu_app WITH PASSWORD '${db_password}'` — the actual database password rendered at `tofu apply` time. The file is written with `permissions: "0644"` (world-readable). Any user on the VM (including the `ubuntu` user from SSH access) can read `cat /opt/nebu/init-db.sql` and obtain the PostgreSQL credentials after cloud-init completes. The file persists on disk after initial boot.
- **Impact:** Local privilege escalation via credential theft. An SSH session (operator or attacker who gains shell access via leaked SSH key) can read the database password without any elevated privileges.
- **Empfehlung:** Change permissions to `"0600"`. The file only needs to be read by root at container start. After first boot it can be deleted — add a runcmd step: `- rm -f /opt/nebu/init-db.sql` after `nebu.service` starts. Alternatively, pass the init SQL via a Docker secret instead of a bind-mounted file.
- **Referenz:** OWASP ASVS V2.10.4, CWE-732, NIST AC-3

### [INFO] user_data / Terraform State secret exposure — documented

- **CWE / OWASP:** CWE-312 (Cleartext Storage of Sensitive Information)
- **File:** `deploy/tofu/examples/stackit/main.tf` (user_data block), `RUNBOOK.md`
- **Description:** `base64encode(templatefile(...))` renders all secrets into the `user_data` field, which is stored verbatim in Terraform state (base64 is not encryption). The risk is correctly acknowledged with a prominent `# SECURITY NOTE` comment in `main.tf` and a `> **SECURITY WARNING**` block at the top of `RUNBOOK.md`. The `backend "s3"` block for encrypted remote state is documented but commented out.
- **Impact:** Operators who store `.tfstate` locally or commit it will expose all secrets. Mitigated by the documentation; full mitigation requires enabling the encrypted S3 backend.
- **Referenz:** CWE-312, Hashicorp Security Best Practices

### [INFO] Positive: nebu_version rejects "latest" tag

- **File:** `deploy/tofu/examples/stackit/variables.tf`
- **Description:** The validation `condition = var.nebu_version != "latest"` correctly prevents accidental production deployments of unversioned images. Combined with the `!= ""` default, this forces operators to explicitly set a semver tag. Well-implemented.

### [INFO] Positive: Secret variable validations enforce minimum length

- **File:** `deploy/tofu/examples/stackit/variables.tf`
- **Description:** `db_password`, `internal_secret`, and `oidc_client_secret` all have `length(...) >= 16` validations. This prevents obvious weak secrets and surfaces the requirement at `tofu plan` time, not at runtime.

### [INFO] Positive: internal_secret quoted scalar (no trailing newline)

- **File:** `deploy/tofu/examples/stackit/cloud-init.tftpl`
- **Description:** `content: "${internal_secret}"` (quoted scalar) correctly avoids the YAML block-scalar trailing newline that would cause PSK comparison failures at runtime. Clean fix.

### [INFO] Positive: DATABASE_URL bash substitution removed

- **File:** `deploy/tofu/examples/stackit/cloud-init.tftpl`
- **Description:** The removed `DATABASE_URL: ecto://$${NEBU_DB_URL#postgresql://}` would have silently failed inside Docker Compose (which does not execute bash parameter substitutions). The replacement — passing `NEBU_DB_URL` directly — is correct.

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — IaC diff, no SQL handler code |
| `reason` field on compliance access         | ✅ N/A — no application query code |
| Audit-log immutability                      | ✅ N/A — init-db.sql creates app users, no audit table grants |
| `instance_admin` notification (if in-scope) | ✅ N/A |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ N/A — no handler code |
| Matrix Power Level checks                   | ✅ N/A — no room mutation code |
| No hardcoded secrets                        | ✅ All secrets are templatefile variables; tfvars.example uses placeholder strings |
| TLS 1.3 enforcement                         | ✅ N/A — TLS handled at ALB/gateway layer (existing code, not in this diff) |
| AES-256-GCM correctness                     | ✅ N/A |
| Ed25519 verify-before-accept                | ✅ N/A |
| No secrets in logs / error messages         | ✅ No logging code in this diff; `sensitive = true` on all secret variables suppresses tofu plan output |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 1 |
| LOW       | 0 |
| INFO      | 5 |

## Pipeline Decision

**MEDIUM findings only. Pipeline may proceed.** The MEDIUM finding (`init-db.sql 0644`) should be fixed in the same commit — it is a one-line change (`"0644"` → `"0600"`) plus an optional cleanup runcmd. No CRITICAL or HIGH findings. All Nebu invariants pass or are not applicable to this IaC-only diff.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
