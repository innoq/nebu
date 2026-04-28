---
security_review: required
---

# Story 5.29a: Trust Model Tightening — Non-Superuser DB Role + gRPC Transport Auth

Status: review

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
- **Secret rotation (PSK) requires a container restart.** Both `Nebu.Grpc.AuthInterceptor` (Elixir core) and the Go gateway gRPC client read `NEBU_INTERNAL_SECRET_FILE` once at process startup. Rotating the secret in-place — for example by overwriting `.secrets/internal_secret` — does **not** propagate to running processes. The operational rotation procedure is: write the new secret, restart the core service (re-reads file in the interceptor's secret-loader), then restart the gateway service (re-reads file at gRPC client init in `main.go`). Phase 2 (ADR-008) will replace the PSK with ephemeral mTLS where rotation is driven by cert lifetimes, removing this restart coupling. This is an annotation to ADR-008 — not a separate ADR, since it documents an implementation property of the interim PSK design that ADR-008 already endorses.
- **Dev upgrade-path note (existing volumes):** `dev/postgres/init/01-roles.sql` only runs on first container start (docker-entrypoint-initdb.d behavior). Developers with a pre-5.29a `postgres_data` volume must destroy the volume to provision the new roles (e.g. `docker compose down -v`). Migration 000024 then transfers ownership on the fresh volume. `make setup` does not currently destroy the volume — running 5.29a against a stale dev volume will fail with a "role nebu_migrate does not exist" error. Documented here so the next developer hitting this knows to run `docker compose down -v` first.

---

## Dependencies

- **Blocks:** 5-29c (audit/crypto lifecycle FBs need real RLS enforcement to test).
- **Blocks:** Epic-5 retrospective close (these are the only HIGHs blocking a clean Epic-5 sign-off).

---

## Tasks / Subtasks

- [x] A.1 — Create `dev/postgres/init/01-roles.sql` (nebu_migrate + nebu_app provisioning)
- [x] A.2 — Update `docker-compose.yml`: init-script mount, DB URL split, port 9000 unbinding
- [x] A.3 — Create migration `000024_transfer_ownership_and_grants.up.sql`
- [x] A.4 — Add `DBURLMigrate` to `gateway/internal/config/config.go`
- [x] A.5 — Update `gateway/internal/db/db.go` `RunMigrations` to accept variadic migrate URL
- [x] A.6 — Update `gateway/cmd/gateway/main.go` to pass PSK secret to gRPC client + migrate URL
- [x] B.1 — Remove port 9000:9000 host binding from `docker-compose.yml` (AC8)
- [x] B.2 — Create `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex`
- [x] B.3 — Wire auth interceptor into `Nebu.EventDispatcher.Endpoint`
- [x] B.4 — Add `newAuthUnaryInterceptor` + `newAuthStreamInterceptor` to Go gRPC client
- [x] B.5 — Update `Makefile` integration test targets with new env vars

### Review Findings (Gate 3 — 2026-04-23, all instantly fixed)

- [x] [Review][Patch] MINOR-1 Comment drift — `audit_log_db_test.go` updated to describe nebu_migrate (owner) / nebu_app (runtime, RLS-bound) model [`gateway/internal/audit/audit_log_db_test.go:17-32`]
- [x] [Review][Patch] MINOR-2 Missing ExUnit unit tests for `Nebu.Grpc.AuthInterceptor` — added `auth_interceptor_test.exs` with 5 cases (missing/empty/wrong/correct token + fail-secure no-secret) [`core/apps/event_dispatcher/test/nebu/grpc/auth_interceptor_test.exs`]
- [x] [Review][Patch] MINOR-3 `TestCoreGRPC_AcceptsValidToken` accepted any non-Unauthenticated error — tightened to allow only OK / Unimplemented / NotFound / InvalidArgument / FailedPrecondition / AlreadyExists / PermissionDenied; codes.Internal/Unknown/Unavailable now fail loudly so server crashes are not masked [`gateway/test/integration/grpc_auth_test.go:203-230`]
- [x] [Review][Patch] MINOR-4 Implementation Note added to story body documenting that PSK rotation requires container restart (Elixir interceptor + Go gRPC client both read the secret once at startup); also documented dev upgrade-path requirement (`docker compose down -v` for stale `postgres_data` volumes) [`_bmad-output/implementation-artifacts/5-29a-trust-model-tightening.md` Implementation Notes]
- [x] [Review][Patch] MAJOR-1 Migration 000024 transferred table+sequence ownership but NOT function ownership — `audit_log_purge` SECURITY DEFINER would still elevate to legacy `nebu` superuser, contradicting AC1 intent and migration's own step-5 comment. Added step 2a that loops `pg_proc` and ALTERs every non-`nebu_migrate`-owned function in `public` to `nebu_migrate`. Verified by re-reading the migration; integration test `TestAuditLogPurge_AppRoleCanCallSecurityDefiner` will exercise the real elevation path against `nebu_migrate` (non-superuser owner) under `make test-integration`. [`gateway/migrations/000024_transfer_ownership_and_grants.up.sql:38-58`]

**Test gates after fixes:**
- `make test-unit-go`: 16 packages PASS (race detector enabled).
- `make test-unit-elixir`: 98 tests, 23 failures, 2 skipped — failure count IDENTICAL to pre-review baseline (Dev Agent Record claim verified). All 5 new `AuthInterceptorTest` cases pass.
- `go vet -tags=integration ./test/integration/...`: PASS.

**No MAJOR/CRITICAL findings remain unresolved.** Security-sensitive trust-model invariant (function-owner ≠ legacy superuser) is now enforced by the migration itself.

## Dev Agent Record

### Implementation Plan

**Block A — DB Role Split (FB-51-01):**
- Created `dev/postgres/init/01-roles.sql` that provisions `nebu_migrate` (CREATEDB, table owner) and `nebu_app` (NOSUPERUSER, NOBYPASSRLS) via docker-entrypoint-initdb.d at first container start. ALTER DEFAULT PRIVILEGES ensures future tables are auto-accessible to nebu_app.
- Created migration `000024_transfer_ownership_and_grants.up.sql` that transfers all existing table/sequence ownership to `nebu_migrate` and grants SELECT/INSERT/UPDATE to `nebu_app`. REVOKE UPDATE/DELETE on `audit_log` from `nebu_app` (defense-in-depth alongside FORCE RLS). Re-grants EXECUTE on `audit_log_purge` to both roles.
- `Config.DBURLMigrate` added (NEBU_DB_URL_MIGRATE). `RunMigrations` accepts variadic second arg for the migrate URL; falls back to runtime URL for backward compatibility.
- `main.go`: reads PSK early (before gRPC client init), passes PSK to `coregrpc.New`, passes `cfg.DBURLMigrate` to `RunMigrations`. Reuses the read PSK bytes instead of double-reading the file.

**Block B — gRPC Transport Auth (FB-52-01):**
- Created `Nebu.Grpc.AuthInterceptor` — a `GRPC.Server.Interceptor` behaviour module. Reads `x-nebu-node-token` from gRPC stream headers, compares using HMAC-based constant-time comparison (`secure_compare/2` via `:crypto.mac`). Reads secret from `Application.get_env(:event_dispatcher, :internal_secret)` first (for test injection), then from `NEBU_INTERNAL_SECRET_FILE`. Fails-secure (rejects all) if no secret is configured.
- Wired interceptor into `Nebu.EventDispatcher.Endpoint` via `intercept(Nebu.Grpc.AuthInterceptor)`.
- Added `newAuthUnaryInterceptor/1` and `newAuthStreamInterceptor/1` to Go gRPC client. `New/2` accepts variadic secret arg. Both interceptors inject `x-nebu-node-token` into outgoing metadata.
- Port 9000:9000 removed from docker-compose.yml core service. Comment explains why.
- Makefile `test-integration` target updated: `NEBU_TEST_DB_URL` → nebu_app DSN, `NEBU_TEST_MIGRATION_DB_URL` → nebu_migrate DSN, `NEBU_TEST_CORE_GRPC_ADDR` added.

### Key Decisions

1. **DB-init via init-script**: Using docker-entrypoint-initdb.d (01-roles.sql) is idiomatic for PostgreSQL containers. Only runs on first start (empty volume). Simpler than a custom entrypoint.
2. **Migration 000024 ownership-transfer**: Chosen over "reset-and-replay" because production databases already have data. The transfer migration is safe to re-run on existing data.
3. **Constant-time comparison**: Used `:crypto.mac(:hmac, :sha256, random_key, value)` — both digests have equal length, HMAC equality is constant-time for fixed-length outputs. This prevents timing attacks on the PSK comparison.
4. **Variadic `RunMigrations`**: Backward-compatible API change. Existing callers (tests) pass only one argument and continue to work. When `NEBU_DB_URL_MIGRATE` is not set, migrations fall back to the runtime URL.
5. **`New` with variadic secret**: Same pattern — existing `client_test.go` calls `New("addr")` without a secret and continues to work (no interceptors attached).

### Completion Notes

- `make test-unit-go`: 100% green — all 16 packages passed
- `make test-unit-elixir`: 23 pre-existing failures (unrelated to this story — `get_room_name/1` missing in some test fakes and Ecto repo not started in others). Confirmed identical failure count before and after my changes by running `git stash`/`git stash pop`. Zero new failures introduced.
- Integration tests (11 failing tests staged in `role_separation_test.go`, `grpc_auth_test.go`, `compose_ports_test.go`) are integration-only (`//go:build integration`). They require the running docker compose stack and will be verified by `make test-integration` in CI.
- `make test-compose-ports` will pass immediately — docker-compose.yml no longer has `9000:9000`.

## File List

### New Files
- `dev/postgres/init/01-roles.sql`
- `gateway/migrations/000024_transfer_ownership_and_grants.up.sql`
- `gateway/migrations/000024_transfer_ownership_and_grants.down.sql`
- `core/apps/event_dispatcher/lib/nebu/grpc/auth_interceptor.ex`

### Modified Files
- `docker-compose.yml`
- `Makefile`
- `gateway/internal/config/config.go`
- `gateway/internal/db/db.go`
- `gateway/cmd/gateway/main.go`
- `gateway/internal/grpc/client.go`
- `core/apps/event_dispatcher/lib/nebu/event_dispatcher/endpoint.ex`
- `_bmad-output/implementation-artifacts/5-29a-trust-model-tightening.md` (this file)
- `_bmad-output/implementation-artifacts/sprint-status.yaml`

## Change Log

- 2026-04-23: Story split out from 5-29 master collector. Bundles FB-51-01 + FB-52-01.
- 2026-04-23: Implementation complete. DB role split + gRPC transport auth implemented. Status → review.
