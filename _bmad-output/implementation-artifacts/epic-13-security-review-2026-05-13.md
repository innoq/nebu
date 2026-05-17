# Epic 13 Security Review — 2026-05-13

**Agent:** Kassandra (BMAD Security Review Agent)
**Scope:** SEC Gate 2 — whole-epic security review for Epic 13 (Deployment Strategy & IaC + MSC2965 OIDC discovery + libcluster).
**Diff base:** `git diff 8ec8629..HEAD`
**Branch:** `feature/phase-3-epic-13`
**Stories covered:** 13-1 OpenTofu module skeleton, 13-2a/b/c AWS networking + RDS + ECS, 13-3a/b Stackit VM + cloud-init bootstrap, 13-4a/b/c Helm chart, 13-5 k6 Gold/Silver load tests, 13-6 libcluster Horde clustering, 13-7 MSC2965 OIDC discovery endpoints.

---

## Summary

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 4 |
| LOW       | 5 |

**Verdict:** CLEAN at SEC Gate 2. No CRITICAL or HIGH findings — Epic 13 may proceed to retrospective and `done`. MEDIUM findings are tracked for follow-up before any non-example AWS deployment goes to production; they do not block epic closure because all MEDIUM items are confined to example/IaC defaults that operators are explicitly required to override per the README/RUNBOOK.

---

## Application code (gateway + core)

### Story 13-7 — MSC2965 OIDC Discovery (`gateway/internal/matrix/oidc_discovery.go`)

The two new unauthenticated endpoints (`auth_issuer`, `auth_metadata`) are scoped tightly and use the existing `looseRL` per-IP rate limiter (300 req/min, burst 100). The HTTP client used to fetch `/.well-known/openid-configuration` has a 10 s timeout and the request inherits the caller's `context.Context` (`http.NewRequestWithContext(r.Context(), ...)`), so a slow upstream cannot keep gateway workers parked indefinitely. The metadata response is cached for 5 minutes behind a `sync.RWMutex`. On upstream failure the handler returns Matrix-format `503 M_UNAVAILABLE` rather than leaking provider error bodies — good.

### Story 13-6 — libcluster + Horde (`core/config/runtime.exs`, `application.ex`)

Cluster topology is driven by env vars (`CLUSTER_STRATEGY`, `KUBERNETES_NAMESPACE`, `KUBERNETES_SELECTOR`, `KUBERNETES_SERVICE_NAME`). Default is no clustering. The Erlang distribution cookie (`RELEASE_COOKIE`) is correctly sourced from AWS Secrets Manager in `compute.tf` (not hardcoded). No new authentication boundaries are exposed by clustering — the Kubernetes headless Service has `publishNotReadyAddresses: false` and is namespace-scoped via the selector.

### Other gateway changes (`cmd/gateway/main.go`)

The previous "explicit 404" stub for `auth_metadata` is replaced with the new handler. Routes are registered behind `looseRL`. No new state-changing endpoints, no new auth surface.

**No SQL injection, XSS, CSRF, IDOR, JWT, timing-attack, weak-crypto, path-traversal, open-redirect, or plaintext-secret-in-logs findings in the application code diff.**

---

## Findings

### [MEDIUM] AWS example default db_password is the placeholder `"changeme"`

- **Location:** `deploy/tofu/modules/nebu-aws/variables.tf:56`, `deploy/tofu/examples/aws/variables.tf:55`
- **Issue:** Both the module variable and the AWS example default `db_password` to `"changeme"`. The validation only enforces `length >= 8`, which `"changeme"` satisfies. An operator who runs `tofu apply` without setting `db_password` in `terraform.tfvars` will provision a publicly-resolvable RDS instance (still private-subnet-only by network design, but accessible from any ECS task in the cluster) with a trivially guessable master password.
- **Recommendation:** Remove the default (force operator to set it explicitly) or change the validation to require at least 16 characters and reject obvious placeholders (`"changeme"`, `"password"`). The Stackit equivalent already uses `length >= 16` and no default — bring AWS in line.
- **CWE:** CWE-521 (Weak Password Requirements), CWE-1188 (Insecure Default Initialization of Resource).

### [MEDIUM] AWS Secrets Manager `secret_string = "PLACEHOLDER_…"` will be applied if operator forgets to override

- **Location:** `deploy/tofu/modules/nebu-aws/secrets.tf:25,46,67,87,107,127`
- **Issue:** All six Secrets Manager secrets (db_url, db_password, internal_secret, oidc_client_secret, oidc_issuer, release_cookie) ship with `secret_string = "PLACEHOLDER_..."`. The `lifecycle { ignore_changes = [secret_string] }` block correctly prevents tofu from overwriting an operator-rotated value on subsequent applies — but on the *first* apply the placeholder is written to AWS verbatim. If the operator does not immediately rotate via the AWS CLI/console, ECS tasks will start with literal `"PLACEHOLDER_rotate_before_go_live"` as their internal secret and Erlang release cookie, allowing trivial cross-tenant lateral movement within the cluster.
- **Recommendation:** Either (a) require operator-supplied sensitive variables (matching the Stackit example pattern) and reference them in `secret_string`, or (b) generate strong random values at apply time via `random_password` / `random_string` resources and tag the secret as "rotate before go-live" without leaving a guessable default in AWS. The current comment in `secrets.tf` is good, but defence-in-depth is to make the unsafe path impossible.
- **CWE:** CWE-798 (Use of Hard-coded Credentials), CWE-1188.

### [MEDIUM] AWS example: SSH ingress and `deletion_protection = false` on RDS

- **Location:** `deploy/tofu/modules/nebu-aws/database.tf:51` (`deletion_protection = false`)
- **Issue:** The RDS instance is provisioned with `deletion_protection = false`, `skip_final_snapshot` defaulting to `true` (variables.tf:67), and no CMK/KMS key reference. Combined this allows accidental deletion of production data with no recovery snapshot. The Stackit example's tfvars.example also defaults `skip_final_snapshot = true` for AWS. While the module is documented as "MVP / dev", these defaults are dangerous when operators copy the example to production.
- **Recommendation:** Default `deletion_protection = true` for any `environment != "dev"` (use a conditional) and require explicit opt-out. Add a `kms_key_id` variable so customer-managed keys can be specified — without it `storage_encrypted = true` uses the AWS-managed key, which is acceptable but offers no cross-account isolation.
- **CWE:** CWE-1188 (Insecure Default Initialization), CWE-693 (Protection Mechanism Failure).

### [MEDIUM] Stackit example: SSH (port 22) ingress allowed from `0.0.0.0/0` by default

- **Location:** `deploy/tofu/examples/stackit/main.tf:79-90` (`stackit_security_group_rule.inbound_ssh`)
- **Issue:** The Stackit example opens SSH port 22 to the entire internet via the security group rule (no `remote_ip_range` specified, default is allow-all). The inline comment says *"restrict to a bastion CIDR in production"*, but as written the apply-time default exposes every Nebu VM to SSH brute-force from the public internet — even after the cloud-init bootstrap completes.
- **Recommendation:** Add an `ssh_allowed_cidrs` variable (default `[]` = no SSH ingress) and only create the inbound_ssh rule when the list is non-empty, or require it to be set explicitly. At minimum, change the default to disable password auth (Ubuntu 24.04 cloud-init defaults to key-only, which mitigates this — but the surface should still not be open).
- **CWE:** CWE-284 (Improper Access Control), CWE-1188.

### [LOW] AuthMetadataHandler: no body-size limit on upstream OIDC response

- **Location:** `gateway/internal/matrix/oidc_discovery.go:111`
- **Issue:** `io.ReadAll(resp.Body)` consumes the entire upstream response without an `io.LimitReader` bound. A malicious or compromised OIDC provider could return a multi-GB response and exhaust gateway memory. Realistic OIDC discovery documents are <10 KB.
- **Recommendation:** Wrap with `io.LimitReader(resp.Body, 1<<20)` (1 MiB) before `io.ReadAll`. The HTTP client `Timeout` mitigates slowloris but not response-size DoS.
- **CWE:** CWE-400 (Uncontrolled Resource Consumption).

### [LOW] AuthMetadataHandler: cached body served without re-validating Content-Type

- **Location:** `gateway/internal/matrix/oidc_discovery.go:80-84`
- **Issue:** The handler sets `Content-Type: application/json` on cache hits without verifying the upstream actually returned JSON. If the OIDC provider once returned HTML (e.g. an error page captured during the cache-fill window) the handler would later serve HTML labelled as JSON for up to 5 minutes. Low impact because the unreachable/5xx branch protects against most error pages, and clients parse JSON strictly.
- **Recommendation:** Validate the upstream `Content-Type` starts with `application/json` (or that the body parses as JSON) before caching. Alternatively, only cache on successful 2xx upstream and skip the cache on any error.
- **CWE:** CWE-345 (Insufficient Verification of Data Authenticity).

### [LOW] AWS module: ECS security group egress is `0.0.0.0/0` for all protocols

- **Location:** `deploy/tofu/modules/nebu-aws/network.tf:216-224` (`ecs_egress_all`)
- **Issue:** ECS tasks can connect outbound to anywhere on the internet on any port. A compromised gateway container can exfiltrate data or pivot to external services. Minimum needed: PostgreSQL (RDS), the OIDC issuer endpoint, and the container registry. CloudWatch Logs uses VPC endpoints when configured.
- **Recommendation:** Restrict ECS egress to RDS-SG, HTTPS (443) to known OIDC issuer CIDR or use VPC endpoints, and registry pulls via VPC endpoints. Defence-in-depth — not a real-world breach until the gateway is compromised.
- **CWE:** CWE-1327 (Binding to Wildcard Address), CWE-284.

### [LOW] ALB: no `access_logs` configured

- **Location:** `deploy/tofu/modules/nebu-aws/alb.tf:5-18`
- **Issue:** The Application Load Balancer is provisioned without `access_logs` block. For production audit / incident-response, ALB access logs to S3 are the canonical source of HTTP-level forensic evidence.
- **Recommendation:** Add a `var.access_logs_bucket` (optional) and conditionally enable `access_logs { bucket = var.access_logs_bucket, enabled = true }`. Not a vulnerability — operational hardening.
- **CWE:** CWE-778 (Insufficient Logging).

### [LOW] k6 gold-tier.js uses `m.login.password` with default credentials in example

- **Location:** `k6/scenarios/gold-tier.js:67-75`, `k6/scenarios/silver-tier.js` (parallel)
- **Issue:** The k6 scenarios use `m.login.password` (ROPC-style flow) and document `TEST_PASSWORD=changeme` in the example command. Comment correctly says *"used for dev/test stacks only. Production deployments use OIDC."* — but if an operator copy-pastes the example against a hardened stack, they will get cryptic 403 / Dex ROPC-disabled errors. The CLAUDE.md `OIDC / Auth Testing Standard` already forbids ROPC for E2E; the k6 scenarios are not E2E tests but load tests, and the call-out is explicit.
- **Recommendation:** Add a defensive 401/403 check that prints a clear "use OIDC token instead — see k6/README.md" message. Not blocking.
- **CWE:** N/A — informational test-data concern; no production code path uses this.

---

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | N/A — no SQL handler code in epic diff |
| `reason` field on compliance access         | N/A |
| Audit-log immutability                      | N/A |
| `instance_admin` notification (if in-scope) | N/A |
| OIDC token validation (`iss`/`aud`/`exp`)   | N/A — discovery endpoints don't validate tokens, only proxy metadata |
| Matrix Power Level checks                   | N/A |
| No hardcoded secrets in application code    | PASS |
| TLS 1.3 enforcement                         | PASS — `ELBSecurityPolicy-TLS13-1-2-2021-06` on ALB HTTPS listener |
| Constant-time PSK compare                   | N/A — no new PSK comparison code in epic diff |
| Ed25519 verify-before-accept                | N/A |
| No secrets in logs / error messages         | PASS — OIDC errors return generic M_UNAVAILABLE |
| Per-story security review (SEC Gate 1)      | PASS — 13-3b cloud-init review recorded; cloud-init MEDIUM finding (0644 init-db.sql) was fixed in commit `01f0672` |

---

## Verdict

**SEC Gate 2 CLEAN — 0 CRITICAL, 0 HIGH.** Epic 13 may proceed to retrospective.

The four MEDIUM findings are confined to AWS/Stackit example defaults and should be captured as follow-up hardening stories before any production deployment of the example modules:

- **Follow-up story candidates:**
  1. Harden AWS module defaults: remove `db_password = "changeme"` default, generate placeholder secrets via `random_password`, default `deletion_protection = true` for non-dev environments.
  2. Stackit example: gate SSH ingress behind `ssh_allowed_cidrs` variable (default empty).
  3. Gateway: add `io.LimitReader` bound to `AuthMetadataHandler` upstream read.
  4. Optional: ECS egress narrowing + ALB access logs (operational hardening).

LOW findings are non-blocking and recorded for future polish.

✓ Epic 13 SEC Gate 2 — Kassandra clean.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
