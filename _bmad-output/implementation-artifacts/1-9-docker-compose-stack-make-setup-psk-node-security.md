# Story 1.9: Docker Compose Stack + make setup + PSK Node Security

Status: done

## Story

As an operator,
I want to start the complete development stack with a single command and have node security configured automatically,
so that local development requires no manual service configuration or secret management.

## Acceptance Criteria

1. **Given** `docker-compose.yml` in the project root defining services `gateway`, `core`, `postgres`, `dex`, **when** `docker compose config` runs, **then** it validates without errors

2. **Given** `make setup` runs, **when** `.secrets/` directory is created, **then** `.secrets/internal_secret` contains a freshly generated 32-byte hex string (`openssl rand -hex 32`)

3. **Given** `.gitignore`, **when** it is checked, **then** `.secrets/` is listed as an ignored path

4. **Given** `docker-compose.yml` secrets configuration, **when** `secrets:` block references `file: .secrets/internal_secret`, **then** both `gateway` and `core` services mount it at `/run/secrets/internal_secret`

5. **Given** gateway environment configuration, **when** `NEBU_INTERNAL_SECRET_FILE: /run/secrets/internal_secret` is set, **then** gateway reads the PSK from the file path — never from an env var directly

6. **Given** `docker compose up`, **when** all 4 services start, **then** `docker compose ps` shows all services as `running` or `healthy` within 2 minutes

7. **Given** `make dev` in Makefile, **when** executed, **then** it runs `docker compose up` (delegates to Compose)

## Tasks / Subtasks

- [x] Task 1: Extend `docker-compose.yml` with all four services (AC: #1, #4, #5, #6)
  - [x] Add top-level `secrets:` block referencing `file: .secrets/internal_secret`
  - [x] Add `gateway` service: build from `./gateway`, `depends_on: postgres: condition: service_healthy`, env vars (`NEBU_DB_URL`, `NEBU_CORE_GRPC_ADDR`, `NEBU_INTERNAL_SECRET_FILE`, `NEBU_SERVER_NAME`, `NEBU_OIDC_ISSUER`), `secrets: [internal_secret]`, port `8008:8008`
  - [x] Add `core` service: build from `./core`, `depends_on: postgres: condition: service_healthy`, env vars (`NEBU_DB_URL`, `NEBU_INTERNAL_SECRET_FILE`, `DATABASE_URL`), `secrets: [internal_secret]`, port `9000:9000`, `4000:4000`
  - [x] Add `dex` service: `image: ghcr.io/dexidp/dex:v2.40.0`, bind-mount `./dev/dex/config.yaml:/etc/dex/config.yaml:ro`, port `5556:5556`
  - [x] Keep existing `postgres` service unchanged

- [x] Task 2: Create `dev/dex/config.yaml` with 3 test users (AC: #1, #6)
  - [x] Dex issuer: `http://dex:5556`
  - [x] 3 static passwords: `kai@example.com` (hash: `changeme`), `compliance@example.com`, `alex@example.com`
  - [x] OAuth2 connector for Matrix client (clientID: `nebu-gateway`, redirectURIs: `http://localhost:8008/_matrix/client/v3/login/sso/redirect/oidc`)
  - [x] Enable password connector

- [x] Task 3: Add blocking `select {}` to `gateway/cmd/gateway/main.go` (AC: #6)
  - [x] Add `select {}` after the gRPC client initialization block to prevent the gateway process from exiting before Story 1.11 adds the HTTP listener
  - [x] Add comment: `// HTTP listener blocks here — replaced by http.ListenAndServe in Story 1.11`

- [x] Task 4: Verify `make setup` and `.gitignore` are correct (AC: #2, #3)
  - [x] Confirm `.secrets/` is in `.gitignore` (already present — verify only, do NOT duplicate)
  - [x] Confirm `make setup` generates `.secrets/internal_secret` (already implemented — verify the idempotency logic is correct)
  - [x] Confirm `make dev` delegates to `docker compose up` (already implemented)

- [x] Task 5: Validate full stack starts (AC: #6)
  - [x] Run `make setup` to generate `.secrets/internal_secret`
  - [x] Run `docker compose config` to validate syntax
  - [x] Run `docker compose up -d` and confirm all 4 services reach `running` or `healthy` within 2 minutes via `docker compose ps`

## Dev Notes

### What Already Exists — Do NOT Recreate

- **`make setup`** in `Makefile` already generates `.secrets/internal_secret` with idempotency check. Do NOT change it.
- **`make dev`** in `Makefile` already runs `docker compose up`. Do NOT change it.
- **`.gitignore`** already contains `.secrets/` on line 14. Do NOT add it again.
- **`docker-compose.yml`** already has a working `postgres` service with healthcheck. Extend it — do NOT replace it.
- **`gateway/internal/config/config.go`** already defines `InternalSecretFile string` field for `NEBU_INTERNAL_SECRET_FILE`. The config loading is complete.

### Critical: Gateway Process Must Not Exit

The current `gateway/cmd/gateway/main.go` runs migrations, initializes server config, and creates the gRPC client — then exits. Docker Compose will show the gateway as `exited` (not `running`) unless the process blocks.

**Add `select {}` as the final statement in `main()`** to make the process block indefinitely. Story 1.11 will replace this with `http.ListenAndServe(...)`.

```go
// In gateway/cmd/gateway/main.go — add after the gRPC client block:
_ = coreClient // passed to HTTP handlers in Story 1.11

// HTTP listener blocks here — replaced by http.ListenAndServe in Story 1.11
select {}
```

### docker-compose.yml: Complete Target Structure

```yaml
secrets:
  internal_secret:
    file: .secrets/internal_secret

services:
  postgres:
    # existing unchanged
    ...

  gateway:
    build: ./gateway
    depends_on:
      postgres:
        condition: service_healthy
    secrets: [internal_secret]
    environment:
      NEBU_DB_URL: "postgresql://nebu:nebu_dev_password@postgres:5432/nebu"
      NEBU_CORE_GRPC_ADDR: "core:9000"
      NEBU_INTERNAL_SECRET_FILE: "/run/secrets/internal_secret"
      NEBU_SERVER_NAME: "localhost"
      NEBU_OIDC_ISSUER: "http://dex:5556"
    ports:
      - "8008:8008"

  core:
    build: ./core
    depends_on:
      postgres:
        condition: service_healthy
    secrets: [internal_secret]
    environment:
      NEBU_DB_URL: "postgresql://nebu:nebu_dev_password@postgres:5432/nebu"
      DATABASE_URL: "ecto://nebu:nebu_dev_password@postgres:5432/nebu"
      NEBU_INTERNAL_SECRET_FILE: "/run/secrets/internal_secret"
    ports:
      - "9000:9000"
      - "4000:4000"

  dex:
    image: ghcr.io/dexidp/dex:v2.40.0
    volumes:
      - ./dev/dex/config.yaml:/etc/dex/config.yaml:ro
    ports:
      - "5556:5556"

volumes:
  postgres_data:
```

**Note on `DATABASE_URL`:** The Elixir core uses `Ecto` for DB access; `DATABASE_URL` follows the Ecto connection string format (`ecto://user:pass@host:port/db`). `NEBU_DB_URL` follows the PostgreSQL URI format used by the Go gateway. Both must be set.

### dev/dex/config.yaml: Complete Target Structure

```yaml
issuer: http://dex:5556

storage:
  type: memory

web:
  http: 0.0.0.0:5556

enablePasswordDB: true

staticPasswords:
  - email: "kai@example.com"
    hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4u"
    username: "kai"
    userID: "kai-id"
  - email: "compliance@example.com"
    hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4u"
    username: "compliance"
    userID: "compliance-id"
  - email: "alex@example.com"
    hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4u"
    username: "alex"
    userID: "alex-id"

staticClients:
  - id: nebu-gateway
    redirectURIs:
      - "http://localhost:8008/_matrix/client/v3/login/sso/redirect/oidc"
    name: "Nebu Gateway"
    secret: nebu-dev-secret
```

**Hash is bcrypt of `changeme`** — generated once and hardcoded. Do NOT change the hash; all 3 users use the same password `changeme` for dev convenience. [Source: epics.md Additional Requirements — Dex Dev Setup]

### Security: PSK File Loading — Never Env Var

The architecture explicitly prohibits reading the PSK directly from an environment variable (`docker inspect` leaks env vars). The pattern is:

```go
// CORRECT — read from file
secretBytes, err := os.ReadFile(cfg.InternalSecretFile)

// WRONG — never do this
secret := os.Getenv("NEBU_INTERNAL_SECRET")
```

`config.go:InternalSecretFile` already stores the file path from `NEBU_INTERNAL_SECRET_FILE`. Any code reading the PSK must use `os.ReadFile(cfg.InternalSecretFile)`.

[Source: architecture.md — Security Anti-Patterns section; config.go]

### Gateway Port: 8008 (not 8080)

The gateway Dockerfile has `EXPOSE 8008 8448`. Port 8008 is the standard Matrix Client-Server API port. The architecture health endpoint section mentions `:8080` which is incorrect — Story 1.11 will use `:8008` based on the Dockerfile and Matrix spec. For this story, expose `8008:8008` in docker-compose.yml.

### Service Dependencies: Startup Order

```
postgres (healthcheck pg_isready) → gateway (depends_on postgres healthy)
                                  → core    (depends_on postgres healthy)
dex (stateless, no dependencies)
```

The gateway runs migrations at startup (`db.RunMigrations`) — it MUST wait for postgres to be healthy. Use `depends_on: postgres: condition: service_healthy` (not just `depends_on: [postgres]`).

The core also connects to postgres at startup (Ecto repo). Same condition required.

### PSK Validation is Story 1.10 — Out of Scope

This story only sets up the PSK file and mounts it. The actual HTTP middleware that validates `Authorization: Bearer <psk>` on `/internal/*` endpoints is implemented in **Story 1.10**. Do NOT implement PSK validation logic in this story.

### Makefile `make setup` is Already Correct

The existing `make setup` has idempotency: it skips regeneration if `.secrets/internal_secret` already exists. Per architecture: "Nach `docker compose down && up`: `make setup` neu ausführen → neues Secret." Users should delete `.secrets/internal_secret` manually to force regeneration. This is by design.

### Build Commands

```bash
# Validate docker-compose syntax
docker compose config

# Start stack
make dev   # = docker compose up
# OR
docker compose up -d  # detached

# Check status
docker compose ps

# Stop stack
docker compose down
```

No `make build` needed for this story — docker-compose uses `build:` context which builds automatically on first `up`.

### Project Structure Notes

**Files to create:**
```
dev/
  dex/
    config.yaml          ← new (Dex dev OIDC config with 3 test users)
```

**Files to modify:**
```
docker-compose.yml                      ← add gateway, core, dex services + secrets block
gateway/cmd/gateway/main.go             ← add select{} to prevent early exit
```

**Files NOT to touch:**
- `Makefile` — `make setup` and `make dev` are already correct
- `.gitignore` — `.secrets/` is already listed
- `gateway/internal/config/config.go` — `InternalSecretFile` field already defined
- `core/Dockerfile` — existing multi-stage build is sufficient
- `gateway/Dockerfile` — existing multi-stage build is sufficient

### References

- PSK architecture: [Source: architecture.md — V3 Elixir Node-Registrierung: Security-Modell]
- Docker Compose secrets pattern: [Source: architecture.md — MVP PSK code block]
- `make setup` idempotency: [Source: Makefile, lines 24-31]
- `.gitignore` `.secrets/` entry: [Source: .gitignore, line 14]
- Dex 3 test users: [Source: epics.md — Additional Requirements — Dex Dev Setup]
- Gateway env vars: [Source: gateway/internal/config/config.go; architecture.md — Naming Conventions]
- Gateway port 8008: [Source: gateway/Dockerfile — EXPOSE 8008 8448]
- Core gRPC port 9000: [Source: architecture.md — G2; CLAUDE.md — NEBU_CORE_GRPC_ADDR default]
- Service startup order: [Source: gateway/cmd/gateway/main.go — db.RunMigrations call]

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6[1m]

### Debug Log References

- Core build failed: `alpine:3.19` runtime incompatible with `elixir:1.19-alpine` builder (Alpine 3.23, OpenSSL 3.4). Fixed by updating runtime to `alpine:3.23`.
- Core release failed: umbrella `mix.exs` missing `releases:` config. Fixed by adding explicit `nebu` release with all 6 apps.

### Completion Notes List

- `docker-compose.yml` extended with `gateway`, `core`, `dex` services and top-level `secrets:` block. PSK mounted at `/run/secrets/internal_secret` in both `gateway` and `core`.
- `dev/dex/config.yaml` created with 3 test users (`kai`, `compliance`, `alex`, all password `changeme`) and `nebu-gateway` OAuth2 client.
- `gateway/cmd/gateway/main.go`: replaced placeholder comment with `select {}` to block process indefinitely.
- `core/mix.exs`: added `releases: [nebu: [applications: [...]]]` for all 6 umbrella apps — required for `mix release` in umbrella projects.
- `core/Dockerfile`: updated runtime from `alpine:3.19` to `alpine:3.23` to match builder (OpenSSL compatibility fix).
- All 4 services (`postgres`, `gateway`, `core`, `dex`) reach `running`/`healthy` status. All Go unit tests pass.

### Code Review (AI) — 2026-03-23

**Reviewer:** claude-opus-4-6[1m]

**Findings:**
- MEDIUM: Dex service missing `command:` override — default CMD uses `/etc/dex/config.docker.yaml`, but custom config mounted at `/etc/dex/config.yaml`. Without override, Dex ignores custom config (3 test users + OAuth2 client). **Fixed:** Added `command: ["dex", "serve", "/etc/dex/config.yaml"]` to dex service in `docker-compose.yml`.
- LOW (unfixed): No `restart: always` policies on services. Acceptable for dev, should be added before production use.

**Result:** All HIGH/MEDIUM issues fixed. All ACs verified as implemented. Status → done.

### File List

- `docker-compose.yml` (modified)
- `dev/dex/config.yaml` (created)
- `gateway/cmd/gateway/main.go` (modified)
- `core/mix.exs` (modified)
- `core/Dockerfile` (modified)
- `_bmad-output/implementation-artifacts/1-9-docker-compose-stack-make-setup-psk-node-security.md` (story)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (status update)
