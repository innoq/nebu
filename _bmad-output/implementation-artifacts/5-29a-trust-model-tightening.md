---
security_review: required
---

# Story 5.29a: Trust Model Tightening — Non-Superuser DB Role + gRPC Transport Auth

Status: ready-for-dev

## Story

As an instance operator,
I want the application's runtime DB role to be a non-superuser bound to RLS, and the internal gRPC channel between gateway and core to be authenticated,
so that the security invariants Epic 5 already declares schema-side (FORCE RLS, audit immutability, four-eyes) and audit-trail-side (signed audit emission) are actually enforced at runtime instead of merely documented.

---

## Background / Motivation

Epic-5 split its FBs into five thematic packages (5-29a..e). This package picks up the **two HIGH carry-overs** that block every other security claim from being load-bearing:

- **FB-51-01** — `nebu` is `BYPASSRLS=t` + `rolsuper=t` in Dev/CI. Every `FORCE ROW LEVEL SECURITY` policy added in Stories 5-1 (audit_log), 5-3 (compliance_requests), 5-5 (compliance_sessions), 5-7 (users.deletion_status), 5-8 (media_files.deleted) is **functionally bypassed**.
- **FB-52-01** — Core gRPC port 9000 is published on the host with `insecure.NewCredentials()`. `WriteAuditLog` and `DeleteUserKeys` (and any future RPC) are **forgeable** by any process with L4 access.

These two are paired because (a) they share the same fix-shape ("define a real trust boundary at the infrastructure layer"), (b) the per-migration RLS work in 5-29c only becomes meaningful once the role split lands.

---

## Acceptance Criteria

### Block A — Non-superuser application DB role (FB-51-01)

1. **Two distinct DB roles** provisioned at DB-init:
   - `nebu_migrate` — owner of all tables and SECURITY DEFINER functions. Used **only** by `golang-migrate` at deploy/startup. May be superuser (or have minimum DDL+GRANT privileges).
   - `nebu_app` — runtime gateway connection. **NOT superuser, NOT BYPASSRLS** (assert via `pg_roles.rolsuper = false AND rolbypassrls = false`).
2. `docker-compose.yml` (and any K8s manifest) provisions both roles via Compose secrets / init scripts.
3. `gateway/internal/db/` connects with `NEBU_DB_URL` (= `nebu_app`); migrations run via `NEBU_DB_URL_MIGRATE` (= `nebu_migrate`).
4. Test env: `NEBU_TEST_DB_URL` → `nebu_app`, `NEBU_TEST_MIGRATION_DB_URL` → `nebu_migrate` (already used in some integration tests; codify).
5. Audit every existing migration (000001..000023) for `GRANT` clauses needed by `nebu_app`. Add missing GRANTs.
6. Re-run the full integration suite. The per-story RLS DELETE-deny tests (`TestAuditLogMigration_DeleteDenied`, `TestAuditLogPurge_SecurityDefinerElevatesAppRole`, `compliance_flow.feature::Audit log immutability`) must now pass for the **right reason**.

### Block B — Authenticated gRPC channel between gateway and core (FB-52-01)

7. ADR-008 Phase 2 implementation: ephemeral mTLS between gateway and core, OR (interim) a shared-secret interceptor on both sides keyed off the existing PSK pathway.
8. Remove `ports: 9000:9000` host exposure from `docker-compose.yml`. Port 9000 reachable only on the internal Compose network.
9. Elixir grpc-elixir interceptor rejects RPCs whose request metadata does not contain a valid node-registration token.
10. All Go gRPC call-sites (audit, room ops, compliance) attach the token in metadata.
11. Negative test: raw `grpcurl` against port 9000 from outside the gateway container → `Unauthenticated`.

---

## Acceptance Tests

### Tests written FIRST (RED-phase):

#### Block A
1. `TestAppRole_IsNotSuperuser` — `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname='nebu_app'` returns `(false, false)`.
2. `TestAppRole_CannotCreateTable` — `nebu_app` connection issues `CREATE TABLE x (...)` → `42501 insufficient_privilege`.
3. `TestAuditLogMigration_DeleteDenied` (existing in `audit_log_db_test.go`) — re-runs and now fails with RLS error for the right reason.
4. `TestAuditLogPurge_SecurityDefinerElevatesAppRole` (existing) — re-runs and now genuinely proves SECURITY DEFINER elevation, not Owner-bypass.

#### Block B
5. `TestCoreGRPC_RejectsUnauthenticated` — raw client dial without token → `Unauthenticated`.
6. `TestCoreGRPC_RejectsForgedToken` — random token → `Unauthenticated`.
7. `TestAuditForgery_NoRowInserted` — end-to-end: unauthenticated `WriteAuditLog` → 0 new rows in `audit_log`.
8. CI smoke: `docker compose ps --format json | jq` confirms port 9000 is **not** in the published port list.

---

## Implementation Notes

- **Splitting trigger:** if step A.5 (per-migration GRANT audit) needs more than 5 migrations, split the GRANT audit into its own follow-up story (5-30) and keep the role provisioning + interceptor + port-unbind in 5-29a.
- mTLS cert management (rotation, distribution) is non-trivial — if it adds significant scope, split as 5-29a.2.
- This story has the largest blast radius in 5-29 — schedule it first, before 5-29c (audit/crypto lifecycle) which depends on the role split for its tests to pass.

---

## Dependencies

- **Blocks:** 5-29c (audit/crypto lifecycle FBs need real RLS enforcement to test).
- **Blocks:** Epic-5 retrospective close (these are the only HIGHs blocking a clean Epic-5 sign-off).

---

## Change Log

- 2026-04-23: Story split out from 5-29 master collector. Bundles FB-51-01 + FB-52-01.
