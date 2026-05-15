# Security Review — 13-2c: AWS ECS Task Definitions, ALB, Secrets Manager — 2026-05-13

**Agent:** Kassandra
**Diff base:** `git diff --staged` — story 13-2c
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=claude-sonnet-4-6`

## Executive Summary

This diff provisions the AWS-facing security perimeter of Nebu: ALB with HTTP→HTTPS redirect,
Secrets Manager resources for all runtime credentials, and ECS task definitions that inject those
secrets via the `secrets` field (never `environment`). The IAM policy is correctly scoped to
`nebu/${environment}/*`. No CRITICAL or HIGH findings. Three LOW items are advisory — all are
standard IaC patterns worth documenting for operators.

## Findings

### [LOW] IAM Resource ARN uses `*:*` for account/region

- **CWE / OWASP:** CWE-732 (Incorrect Permission Assignment)
- **File:** `deploy/tofu/modules/nebu-aws/compute.tf`, IAM policy resource line
- **Description:** The `secretsmanager:GetSecretValue` resource ARN is
  `arn:aws:secretsmanager:*:*:secret:nebu/${var.environment}/*`. The `*:*` wildcards cover all
  regions and all accounts. In practice this has no exploit path because the IAM role is bound
  to `ecs-tasks.amazonaws.com` principal in the same account, and cross-account access would
  require a separate resource-based policy on the secret itself. However, operators who want
  strict account and region lockdown may wish to replace the wildcards with concrete values.
- **Impact:** None in a single-account deployment. In a multi-account setup with shared role ARNs,
  this allows the role to read Nebu secrets in any region, which may violate least-privilege policy.
- **Recommendation:** For production multi-account/multi-region deployments, scope the resource ARN
  to the specific region and account:
  `arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:nebu/${var.environment}/*`
  Add a `data "aws_caller_identity" "current" {}` data source. For single-account deployments,
  the current form is acceptable.
- **Reference:** NIST SP 800-53 AC-6 (Least Privilege)

---

### [LOW] Secrets Manager secret description reveals database username

- **CWE / OWASP:** CWE-200 (Exposure of Sensitive Information)
- **File:** `deploy/tofu/modules/nebu-aws/secrets.tf`, `aws_secretsmanager_secret.db_url`, line ~16
- **Description:** The `description` field reads `"Full PostgreSQL DSN for Nebu. Format:
  postgres://nebu:PASSWORD@host:5432/nebu"`. This description is stored as unencrypted AWS metadata
  and is visible to any principal with `secretsmanager:DescribeSecret` — a broader permission than
  `secretsmanager:GetSecretValue`. It discloses the database username (`nebu`) and database name
  (`nebu`), which slightly reduces the effort required for a targeted attack if an attacker has
  AWS console read access.
- **Impact:** Low. Username is typically derivable from application code anyway. No credential is
  exposed — only the schema format.
- **Recommendation:** Shorten description to `"Full PostgreSQL DSN for Nebu gateway and core."`,
  omitting the format template that reveals the username.
- **Reference:** OWASP ASVS V2.10.4

---

### [LOW] OpenTofu state contains placeholder secret values — S3 bucket encryption advisory

- **CWE / OWASP:** CWE-312 (Cleartext Storage of Sensitive Information)
- **File:** `deploy/tofu/modules/nebu-aws/secrets.tf`, all `aws_secretsmanager_secret_version` resources
- **Description:** `secret_string` is set to `PLACEHOLDER_*` values. These are non-sensitive
  placeholders that `ignore_changes` protects from being overwritten by operators' real values.
  However, once operators rotate the secrets via `aws secretsmanager put-secret-value`, OpenTofu's
  state file will **not** contain the real secret (because of `ignore_changes`). This is the correct
  behaviour. No real credentials will be stored in state. The advisory is that the S3 backend bucket
  holding the state file should have server-side encryption (SSE-S3 or SSE-KMS) enabled — this is
  not enforced by the current IaC.
- **Impact:** None from this diff. Advisory for operators: if S3 state bucket lacks encryption, any
  future secret value accidentally introduced into a resource argument (not covered by `ignore_changes`)
  would be stored in plaintext in state.
- **Recommendation:** Add a `backend "s3"` annotation in `RUNBOOK.md` noting that the state bucket
  must have SSE-KMS or SSE-S3 enabled. Optionally add an `aws_s3_bucket_server_side_encryption_configuration`
  resource to the example if a state bucket is provisioned by this IaC.
- **Reference:** NIST SP 800-53 SC-28 (Protection of Information at Rest)

---

### [INFO] ALB enforces HTTPS correctly — HTTP_301 redirect on port 80

- **File:** `deploy/tofu/modules/nebu-aws/alb.tf`
- **Description:** Port 80 listener performs a permanent `HTTP_301` redirect to HTTPS/443. No
  plaintext traffic is forwarded to the gateway. `drop_invalid_header_fields = true` is set,
  preventing HTTP header injection. TLS policy `ELBSecurityPolicy-TLS13-1-2-2021-06` enforces
  TLS 1.3 on the HTTPS listener. This is the correct and secure configuration.

---

### [INFO] ECS tasks run in private subnets without public IPs

- **File:** `deploy/tofu/modules/nebu-aws/compute.tf`, both `aws_ecs_service` resources
- **Description:** `assign_public_ip = false` confirmed on both gateway and core services.
  Tasks are only reachable via the ALB or from within the VPC. This matches the intended
  network topology from story 13-2a (ECS security group allows inbound on 8008 only from the ALB SG).

---

### [INFO] Secrets Manager secrets use `ignore_changes` — operator rotation is safe

- **File:** `deploy/tofu/modules/nebu-aws/secrets.tf`, all `aws_secretsmanager_secret_version` resources
- **Description:** All six secret version resources use `lifecycle { ignore_changes = [secret_string] }`.
  This ensures that `tofu apply` after operator rotation will not revert the secrets to placeholder
  values. The pattern is correct and prevents an accidental credential reset on re-apply.

---

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ N/A — no application DB code in diff |
| `reason` field on compliance access         | ✅ N/A — no compliance handler in diff |
| Audit-log immutability                      | ✅ N/A — no migration or DB code in diff |
| `instance_admin` notification (if in-scope) | ✅ N/A — no compliance escalation path |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ N/A — no handler code; OIDC issuer URL injected as secret (correct) |
| Matrix Power Level checks                   | ✅ N/A — no room operation code in diff |
| No hardcoded secrets                        | ✅ All values are `PLACEHOLDER_*` or empty strings |
| TLS 1.3 enforcement                         | ✅ `ELBSecurityPolicy-TLS13-1-2-2021-06` on HTTPS listener |
| AES-256-GCM correctness                     | ✅ N/A — no crypto code in diff |
| Ed25519 verify-before-accept                | ✅ N/A — no signature code in diff |
| No secrets in logs / error messages         | ✅ N/A — no application logging code in diff |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 0 |
| LOW       | 3 |
| INFO      | 3 |

## Pipeline Decision

**CLEAN** — no CRITICAL or HIGH findings. Pipeline may proceed.

The three LOW findings are advisory items for operators. The IAM ARN wildcard (LOW-1) and the
DB description disclosure (LOW-2) may be addressed in a follow-up story or operator documentation
update; they do not require blocking this story. The S3 state encryption advisory (LOW-3) should
be captured in the RUNBOOK.md as an operator prerequisite note.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
