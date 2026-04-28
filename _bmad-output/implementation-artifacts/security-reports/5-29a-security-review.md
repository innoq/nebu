# Security Review — Story 5.29a (Trust Model Tightening) — 2026-04-23

**Agent:** Kassandra
**Diff base:** `git diff --staged` (18 files, ~1430 insertions)
**Classification:** HIGH
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7-1m`

## Executive Summary

The DB role split (Block A) and gRPC transport auth (Block B) close the two HIGH carry-overs FB-51-01 and FB-52-01 architecturally. The interceptor design, the constant-time HMAC comparison, the port-9000 unbinding, and the test coverage are all correct.

One regression is introduced by the role split itself: `audit_log_purge()` runs as `nebu_migrate` after migration 000024, and `nebu_migrate` is NOT BYPASSRLS. With `audit_log` under FORCE ROW LEVEL SECURITY and a `DELETE USING (false)` policy, the function's internal `DELETE` is filtered to zero rows — silently. The retention purge becomes a no-op. The integration test that would catch this (`TestAuditLogPurge_AppRoleCanCallSecurityDefiner`, `TestAuditLogPurge_SecurityDefinerElevatesAppRole`) was not run by the developer ("verified by `make test-integration` in CI" per Dev Agent Record).

Read first: HIGH-1 (audit retention silently broken). Everything else is INFO/LOW.

## Findings

### [HIGH] HIGH-1 — `audit_log_purge` silently deletes 0 rows after role-owner transfer

- **CWE / OWASP:** CWE-840 (Business Logic Errors) / A04:2021 — Insecure Design
- **Datei:** `gateway/migrations/000018_audit_log.up.sql:58-77` × `gateway/migrations/000024_transfer_ownership_and_grants.up.sql:58-74`
- **Beschreibung:**
  PostgreSQL `FORCE ROW LEVEL SECURITY` keeps the table owner subject to RLS policies. Only roles with the `BYPASSRLS` attribute (or superusers) escape the policy filter. Pre-5-29a the function owner was `nebu`, the superuser provisioned by `POSTGRES_USER` — the inner `DELETE FROM audit_log` succeeded because superusers bypass RLS unconditionally. Migration 000024 step 2a transfers function ownership to `nebu_migrate`, which is created in `dev/postgres/init/01-roles.sql:20` with only `CREATEDB` (no superuser, no BYPASSRLS). When `audit_log_purge()` runs as `nebu_migrate` via SECURITY DEFINER, the existing `audit_log_no_delete` policy (`USING (false)`) AND-eliminates every candidate row. The function does not error — it returns `deleted_count = 0`.

  The function body has `SET search_path = pg_catalog, public` but no `SET row_security = OFF` (which would not help under FORCE anyway).

- **Impact:**
  Audit retention silently stops working in production once this story is deployed. `audit_log` grows unbounded; the cleanup goroutine in `gateway/internal/audit/audit.go:33` reports success ("deleted 0 rows") with no error to operations. No security exposure to users, but a regression of an audit-immutability guarantee combined with a storage-availability time bomb. Two existing integration tests would have caught this (`TestAuditLogPurge_SecurityDefinerElevatesAppRole`, `TestAuditLogPurge_AppRoleCanCallSecurityDefiner`) — the Dev Agent Record states they were not executed prior to marking the story `review`.

- **Empfehlung:**
  Pick exactly one of the following. All three are valid; preference order reflects architectural intent.

  1. (Preferred — minimal scope) Grant `BYPASSRLS` to `nebu_migrate` in `dev/postgres/init/01-roles.sql` and document it as an intentional property of the migration role. The role is privileged by definition (table owner, runs SECURITY DEFINER functions). This restores the pre-5-29a behavior of `audit_log_purge` while keeping `nebu_app` strictly RLS-bound. Add an explicit assertion in `TestAppRole_IsNotSuperuser` that `nebu_app.rolbypassrls = false` (already covered) and `nebu_migrate.rolbypassrls = true` (new) so the invariant is tested both ways.
  2. Add an explicit DELETE policy on `audit_log`: `CREATE POLICY audit_log_purge_delete_allow ON audit_log FOR DELETE TO nebu_migrate USING (true);`. Narrower than (1) but ties the policy to the role name, which is a coupling between the schema and the deployment model.
  3. Drop FORCE RLS on `audit_log` and rely on REVOKE UPDATE/DELETE from `nebu_app` alone (defense-in-depth becomes single-line-of-defense). Not recommended — Story 5-1's invariant explicitly chose FORCE.

  Whichever option is picked: actually run `make test-integration` against the staged code before merging — both purge tests must produce `deleted >= 1`.

- **Referenz:** PostgreSQL 16 docs §5.8 (Row Security Policies), OWASP ASVS V8.3.1 (Logging integrity)

### [INFO] INFO-1 — Per-RPC file read in Elixir interceptor

- **Datei:** `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex:60-101`
- **Beschreibung:**
  `verify_token/1` calls `read_internal_secret/0`, which calls `File.read/1` on every gRPC RPC. The Implementation Note in the story says "Elixir interceptor + Go gRPC client both read the secret once at startup" — the Go side does, but the Elixir side re-reads per call. Not a security defect (the file is on disk-mounted Compose secret, not network-loaded), but a misalignment between the documented behavior and the implementation, plus a needless syscall under load.
- **Empfehlung:**
  Cache the secret in a `:persistent_term` or in a GenServer at application start. Update Implementation Note. No urgency.

### [INFO] INFO-2 — Hardcoded dev passwords in `01-roles.sql`

- **Datei:** `dev/postgres/init/01-roles.sql:20,24`
- **Beschreibung:**
  `nebu_migrate_dev_pw` and `nebu_app_dev_pw` are hardcoded in the init script (and mirrored in `docker-compose.yml`). The file header marks this as DEV ONLY and points to "Vault / AWS Secrets Manager / Kubernetes Secrets" for production. The hardcoded passwords match the pattern of the existing `POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-nebu_dev_password}` legacy setup.
- **Empfehlung:**
  Acceptable for dev. Track a follow-up story (Epic 6 or production-readiness epic) that documents the production-deployment substitution path: either env-substituted SQL (`PGPASSWORD` / `POSTGRES_INITDB_ARGS`), or removal of the init-script in favor of a Helm-chart bootstrapping job.

### [INFO] INFO-3 — Down-migration is a no-op

- **Datei:** `gateway/migrations/000024_transfer_ownership_and_grants.down.sql:1-6`
- **Beschreibung:**
  The down-migration intentionally does nothing. The justification ("rolling back would re-enable BYPASSRLS behavior") is correct and aligns with the story's intent. Note for future operators: `migrate down 1` after 000024 will not actually undo the ownership transfer, which may surprise. The file documents this clearly.
- **Empfehlung:**
  None. Accept as-is.

### [INFO] INFO-4 — Legacy `nebu` superuser remains provisioned

- **Datei:** `docker-compose.yml:12`, `gateway/migrations/000024_transfer_ownership_and_grants.up.sql:96`
- **Beschreibung:**
  `POSTGRES_USER: nebu` and `GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu;` remain. The story comment states "backward compat: keep existing grant to legacy nebu superuser role". Acceptable transitional state; the legacy role is no longer used by gateway or core (both env-vars now point to `nebu_app`). The trust boundary is the role split, not the role removal.
- **Empfehlung:**
  Track a follow-up to remove `POSTGRES_USER: nebu` once it is confirmed nothing references it (likely Epic 6).

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ |
| `reason` field on compliance access         | ✅ |
| Audit-log immutability                      | ⚠️ — RLS now genuinely binds `nebu_app` (correct), but `audit_log_purge` is broken under the new owner (HIGH-1). |
| `instance_admin` notification (if in-scope) | ✅ (out of scope for this diff) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (unchanged by this diff) |
| Matrix Power Level checks                   | ✅ (unchanged by this diff) |
| No hardcoded secrets                        | ⚠️ — dev-only passwords in `01-roles.sql` and `docker-compose.yml`; documented and scoped (INFO-2). |
| TLS 1.3 enforcement                         | ✅ (unchanged) |
| AES-256-GCM correctness                     | ✅ (unchanged) |
| Ed25519 verify-before-accept                | ✅ (unchanged) |
| No secrets in logs / error messages         | ✅ — interceptor logs path on read failure but not the secret content; `slog.Error` in `main.go:119` logs path, not bytes. |

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 1 |
| MEDIUM    | 0 |
| LOW       | 0 |
| INFO      | 4 |

## Pipeline Decision

**HIGH findings present, `blocking_severity: CRITICAL` (default)** — Pipeline proceeds with warning.

HIGH-1 is functionally a regression of audit retention and must either be fixed in this story (preferred — the integration tests will fail in CI anyway) or scheduled as a follow-up before Epic 5 is closed. Recommend: fix HIGH-1 in 5-29a (option 1, three-line change in `01-roles.sql`) and re-run `make test-integration` before commit. Without that fix, two staged integration tests are dead-on-arrival.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
