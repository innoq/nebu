# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Nebu** (short for Nebuchadnezzar, the ship of the free from The Matrix film) – An enterprise-grade, Matrix Client-Server API compatible chat server. Apache 2.0 licensed, no federation, horizontally scalable. Designed to replace Slack/Teams with full data sovereignty.

This project is currently in early development. The `main.go` is a placeholder; the architecture described in `README.md` is the implementation target.

## Tech Stack

| Layer | Technology |
|---|---|
| API Gateway | Go 1.26+ |
| Media Gateway | Go 1.26+ |
| Core / Messaging | Elixir/OTP 1.19+ |
| Gateway ↔ Core | gRPC (protobuf) |
| Database | PostgreSQL 16+ |
| Sessions / Cache | ETS (Elixir built-in) — kein Redis |
| Pub/Sub | pg Process Groups (built-in) — kein NATS |
| Clustering | libcluster + Horde |

## Architecture

Three-tier design:

1. **Go Gateway** (`gateway/`) — Handles all Matrix Client-Server API HTTP traffic, TLS termination, rate limiting, auth middleware (OIDC-only). Stateless, horizontally scalable. Communicates with Core via gRPC.

2. **Elixir/OTP Core** (`core/`) — Actor-model runtime for Room GenServers (via Horde), Session Manager (ETS + PostgreSQL), Presence, and Event Dispatch. Handles all stateful chat logic. Clusters via libcluster.

3. **Infrastructure** — PostgreSQL as append-only event log and schema owner (via golang-migrate). No Redis. No NATS. No Kafka.

## Planned Directory Structure

```
gateway/
  cmd/gateway/         ← entrypoint (migrate → registry → HTTP)
  internal/
    auth/              ← OIDC-only (no SAML/LDAP directly)
    matrix/            ← Matrix API handlers
    middleware/        ← rate limiting, logging, CORS
    grpc/              ← Core communication (stream + fallback)
    buffer/            ← message_buffer drain strategy
    admin/             ← Admin API + UI (Go Templates + go:embed)
    registry/          ← Elixir node registry
  migrations/          ← golang-migrate SQL files
media/
  cmd/media/           ← entrypoint
  internal/
    upload/ download/ crypto/ storage/
core/                  ← Elixir/OTP Umbrella
  apps/
    room_manager/      ← Horde + Room GenServer
    session_manager/   ← ETS + PostgreSQL hybrid since-token
    presence/          ← Presence Manager
    event_dispatcher/  ← EventBus gRPC Stream + pg Process Groups
    signature/         ← Ed25519 signing + X25519 encryption + Nebu.EventId
    permissions/       ← System roles + room power levels
proto/                 ← gRPC .proto definitions + generated stubs
docs/
  architecture/        ← SAD.md, data-model.md, ADRs
  stories/             ← BMAD epics & stories
```

## Commands

All builds run via Docker containers — no local Go, Elixir, or buf installation required.

### Go Gateway (via build container)

```bash
make build-gateway                # docker multi-stage build
make test-unit-go                 # go test ./... in container
make gen-api                      # oapi-codegen: openapi.yaml → api_gen.go
```

### Elixir/OTP Core (via build container)

```bash
make build-core                   # docker multi-stage build
make test-unit-elixir             # mix test in container
```

### Proto / gRPC

```bash
make proto                        # buf generate (in container)
```

### Local Development (Docker)

```bash
make dev                          # docker compose up (gateway, core, postgres, keycloak)
make setup                        # first-time setup: generate .secrets/internal_secret + test keys
make test-integration             # full stack + Godog Gherkin tests
```

Environment variables for gateway (prefix: `NEBU_`):
- `NEBU_CORE_GRPC_ADDR` — gRPC address of the Elixir core (default: `core:9000`)
- `NEBU_DB_URL` — PostgreSQL connection string
- `NEBU_OIDC_ISSUER` — OIDC provider URL
- `NEBU_INTERNAL_SECRET_FILE` — path to shared node-registration secret file

## Matrix API Scope

**Implemented (MVP target):**
- `POST /_matrix/client/v3/login`
- `POST /_matrix/client/v3/logout`
- `GET  /_matrix/client/v3/sync`
- `PUT  /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}`
- `GET  /_matrix/client/v3/rooms/{roomId}/messages`
- `POST /_matrix/client/v3/createRoom`
- `POST /_matrix/client/v3/join/{roomIdOrAlias}`
- `PUT  /_matrix/client/v3/rooms/{roomId}/typing/{userId}`
- `POST /_matrix/client/v3/rooms/{roomId}/receipt/{receiptType}/{eventId}`
- `GET/PUT /_matrix/client/v3/profile/{userId}`
- `GET  /_matrix/client/v3/presence/{userId}/status`

**Intentionally excluded:** All `/_matrix/federation/*`, `/_matrix/identity/*`, `/_matrix/key/*` (no federation by design).

## Resolved Architecture Decisions

All major decisions are documented in `_bmad-output/planning-artifacts/architecture.md`.
ADRs are tracked in `docs/architecture/adr/`:

| ADR | Decision |
|---|---|
| 001 | Elixir/OTP (not Erlang) — libcluster, Mix tooling |
| 002 | No Redis, No NATS — ETS + pg Process Groups replace both |
| 003 | Content-Hash Event-ID (Matrix Room Version 6+) |
| 004 | Horde Registry + DynamicSupervisor for Room GenServers |
| 005 | gRPC Server-Streaming EventBus + Unary fallback |
| 006 | message_buffer drain strategy (linear MVP, AIMD Phase 2) |
| 007 | Ed25519 (signing) + X25519 (encryption) — two key pairs per user |
| 008 | Node registration: PSK via Compose secrets (MVP) → Ephemeral mTLS (Phase 2) |
| 009 | OpenAPI Spec-First with oapi-codegen |
| 010 | Full-Text Search strategy (deferred) |
| 011 | Managed E2EE key escrow (server-side decryption model) |
| 012 | Upstream repo = reference implementation only; no official deployments; example-only CI credentials |

## BMAD Workflow

This project uses BMAD agents for structured development. The pipeline is:

```
bmad-create-story → bmad-testarch-atdd → bmad-dev-story → bmad-testarch-test-review → bmad-code-review → security-review*
     (SM)              (TEA)               (Dev)              (TEA)                     (Review)         (conditional)
```

`*` — Per-story security review runs conditional (see Gate 4 below); epic-end security review is mandatory.

### Gate 1 — Story Creation (`/bmad-create-story`)

**Owner: Bob (SM)**

After the story file is created and before it moves to `ready-for-dev`:

1. Story file must contain the **"Acceptance Tests"** section (see Testing Conventions below)
2. Run **`/bmad-testarch-atdd`** to generate failing acceptance tests from the story's acceptance criteria
3. The generated failing tests are committed alongside the story — implementation starts against them

**When to skip atdd:** Pure infrastructure stories with no observable behavior (e.g., Dockerfile changes, migration-only). All other stories require it.

### Gate 2 — Implementation (`/bmad-dev-story`)

**Owner: Amelia (Dev)**

The Red-Green-Refactor cycle:

1. Failing tests from Gate 1 are already present — do not write new implementation code before running them
2. Implement until all acceptance tests pass (no new code without a failing test)
3. For complex test scenarios or edge cases: invoke **`/bmad-tea`** as a test design partner
4. Run `make test-unit-go` + `make test-unit-elixir` before marking story as `review`

### Gate 3 — Code Review (`/bmad-code-review`)

**Owner: Code Review Agent**

Before marking a story `done`, the code reviewer MUST invoke **`/bmad-testarch-test-review`** and include its findings in the review report.

The test review checks:
- Every Acceptance Criterion has at least one test
- No hard waits, no hidden assertions, tests are deterministic
- GenServer state stories have a crash/restart test
- No cookie forging or DB-seeding shortcuts in E2E tests

**MAJOR finding if:** Any acceptance criterion has zero test coverage.

### Gate 4 — Security Review (SEC Gate 1 + SEC Gate 2)

**Owner: Code Review Agent / Pipeline**

Added 2026-04-20 after security audit found CRITICAL/HIGH findings that passed through Gates 1–3 across four epics.

**SEC Gate 1 — Per-story (conditional):**

Each story file declares in its frontmatter:

```
---
security_review: required | optional | not-needed
---
```

- `required` — pipeline runs a security-focused `/bmad-code-review` pass in a fresh subagent after Gate 3. CRITICAL/HIGH block the commit.
- `optional` — user decides per-invocation.
- `not-needed` — skipped.
- Flag missing → auto-classified `required` if the staged diff touches `gateway/internal/{auth,middleware,admin,db}/`, `core/apps/{signature,permissions}/`, new routes in `cmd/gateway/main.go`, new SQL migrations, or Elixir code using `:crypto` / external input.

**SEC Gate 2 — Epic-end (mandatory):**

Before the retrospective, `/bmad-pipeline` runs a whole-epic security review against `git diff <epic-base>..HEAD`. The output is saved as `_bmad-output/implementation-artifacts/epic-{N}-security-review-{YYYY-MM-DD}.md` — always, even at zero findings (audit trail). CRITICAL/HIGH must become follow-up stories (or be explicitly accepted as a risk with written justification) before the epic is marked `done`.

**Security scope (both gates, identical prompt):** SQL injection, XSS, CSRF on state-changing endpoints, auth bypass (missing middleware, IDOR), timing attacks on secret comparison, open redirects, missing body-size limits, missing rate limits, weak crypto primitives (md5/sha1/DES), plaintext secrets in logs, missing security headers, path traversal, JWT validation flaws (alg confusion, missing exp/aud/nonce).

### Epic Completion — Traceability

At the end of each epic, run **`/bmad-testarch-trace`** to generate the requirements-to-tests traceability matrix. Gate: ≥80% P0+P1 coverage before epic is marked `done`.

## MCP Tools & Testing Conventions

### Context7 — Current Library Docs
Use the `context7` MCP server before implementing any story that touches external libraries or APIs. Load current docs first, do not rely on training data alone.

**When to use:**
- Any story using `go-oidc`, `grpc-go`, OTP `:crypto`, Dex API, `golang-migrate`, `oapi-codegen`, Tailwind, DaisyUI, Vue.js, Playwright
- When API behavior is unclear or a library version has changed

**How:** Call `mcp__context7__resolve-library-id` → `mcp__context7__query-docs` before writing implementation code.

### Playwright — HTML/UI E2E Tests
Use the `playwright` MCP server for all E2E tests that involve HTML pages, forms, buttons, or browser navigation (Admin UI, Bootstrap Wizard, Dashboard, Matrix web client flows).

**Split:**
- **Playwright + Cucumber/Gherkin** → browser-level tests (HTML pages, form submits, button clicks, redirects, Matrix web client E2E)
- **Godog + net/http** → HTTP/gRPC-level tests (REST API, Matrix API protocol, gRPC endpoints)

**E2E test format (mandatory):** All Playwright E2E tests MUST be written as Gherkin feature files with a Cucumber-based framework (`playwright-bdd` or `@cucumber/cucumber` + Playwright). Plain `.spec.ts` files without a `.feature` counterpart are not accepted.

Structure:
```
e2e/features/<feature>.feature   ← Gherkin scenarios (written first, failing)
e2e/steps/<feature>.steps.ts     ← Cucumber step definitions
```

**Why:** Godog HTTP-level navigation of Authorization Code flows is complex and brittle. Playwright handles real browser flows correctly. Gherkin keeps scenarios readable and directly traceable to acceptance criteria.

**Migration note:** Existing `.spec.ts` files are being migrated to the Gherkin format. New stories must use `.feature` + step definitions from day one — no new plain `.spec.ts` files.

### TDD/ATDD — Tests First, Always

**The Nebu TDD Standard: write the failing test before writing implementation code. This applies to ALL story types, not just UI.**

**Why:** Story 3-8 (sync.Map → 3 review rounds) and recurring MAJOR findings in Epic 4 code reviews show that tests written after the fact do not catch architectural mistakes early enough. A failing test written first forces the correct design.

#### For Elixir GenServer / ETS / Horde Stories:

Write **ExUnit tests first** (failing), then implement:

1. **Happy-path test**: `send_event/5` returns `{:ok, event_id}` — write before the function exists
2. **Crash/restart test**: `Process.exit(pid, :kill)` → state recovered via Horde — write before Horde supervision exists
3. **Persistenz-Strategie** (mandatory for any story with GenServer state):
   - Describe how this state survives a restart
   - Option A: ETS (volatile, recovered via Horde migration) — add crash test
   - Option B: PostgreSQL (persistent, read on start) — add read-on-start test
   - Option C: Stateless — no restart test needed

#### For Matrix API Endpoint Stories:

Write **Godog scenario first** (failing), then implement:

1. Write `gateway/features/<feature>.feature` with happy path + one error case
2. The handler returns `501 Not Implemented` initially
3. Implement handler until Godog scenario is green
4. Add edge case unit tests (Go httptest)
5. **Additionally:** if the API behavior is visible through a web client (send message, join room, sync, etc.), also write a Playwright+Gherkin scenario in `e2e/features/` that tests through a real browser session. Godog alone does not cover the browser-level behavior.

#### For Admin UI Stories (HTML/browser) and Matrix Web Client Stories:

Write **Gherkin feature file first** (failing), then implement:

1. Write `e2e/features/<feature>.feature` (Gherkin scenarios) before any HTML/Go template code
2. Write `e2e/steps/<feature>.steps.ts` (Cucumber step definitions) — initially failing
3. Use real browser flows — no cookie forging, no DB seeding shortcuts
4. Tests run against the real running stack (no mocks) via `playwright-bdd` or `@cucumber/cucumber` + Playwright

**Epic 3 retrospective:** Story 3-8 went through 3 code review rounds because an upfront acceptance test would have revealed the restart-resilience requirement. Story 3-15 forged cookies instead of testing real flows because the test was written last.

### Story Acceptance Test Requirement

Every story document MUST contain an **"Acceptance Tests"** section (written by the story creator, before implementation):

```
## Acceptance Tests

### Tests written FIRST (before implementation code):

1. [Test name] — [ExUnit / Godog (.feature) / Playwright+Cucumber (.feature + .steps.ts)]
   - Given: [starting state]
   - When: [action]
   - Then: [expected outcome]

2. [Crash/Restart test — required for all GenServer state stories]
   - Given: [service running with state]
   - When: [Process.exit(:kill) / service restart]
   - Then: [state recovered / correctly migrated]
```

**Code Review gate:** Does every Acceptance Criterion have at least one test? If not, the story is not done.

### TEA Skills — Quick Reference

| When | Skill | Who | Mandatory |
|---|---|---|---|
| After story created, before dev starts | `/bmad-testarch-atdd` | SM/Dev | **Yes** |
| During implementation, for complex tests | `/bmad-tea` | Dev | On demand |
| During code review | `/bmad-testarch-test-review` | Reviewer | **Yes** |
| End of epic | `/bmad-testarch-trace` | SM | **Yes** |
| End of epic | `/bmad-generate-arc42` | SM | **Yes** |
| When test strategy needed for a new epic | `/bmad-testarch-test-design` | SM/Arch | On demand |
| When NFRs need validation | `/bmad-testarch-nfr` | Dev/Arch | On demand |

See **BMAD Workflow** section above for the full gate sequence.

### OIDC / Auth Testing Standard
All Gherkin tests involving OIDC must use Authorization Code + PKCE. ROPC is not supported by Dex v2.41+. Never use `grant_type=password` shortcuts in E2E tests.

## Elixir Conventions
- GenServer state: immer via handle_* callbacks, nie direkt
- Fehler: let it crash + Supervisor, kein defensive try/rescue
- Ecto: Changesets für alle Validierungen, kein direkt insert!
- Supervisor-Strategien: one_for_one default, begründe Abweichungen
- Keine Prozess-Registrierung ohne via-Tuple oder Registry

## Go Conventions
- Errors: explicit handling, kein panic in Library-Code
- Context: immer als ersten Parameter durchreichen
- Interfaces: klein halten, definiert vom Consumer