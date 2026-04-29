# Contributing to Nebu

Thanks for your interest in contributing. Nebu is Apache 2.0 licensed and
welcomes PRs from everyone. This document covers what you need to know to
get a patch from your machine into `main`.

By participating, you agree to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

---

## Ways to Contribute

- **Report bugs** — open an issue with steps to reproduce, expected vs.
  actual behavior, and environment details (OS, Docker version, browser if
  UI-related).
- **Suggest features** — open an issue describing the problem you want to
  solve, _before_ opening a PR. For non-trivial changes we'd rather align on
  direction first.
- **Fix bugs / add features** — pick an open issue (look for
  `good-first-issue` if you're new), comment that you're working on it, then
  open a PR.
- **Improve docs** — corrections, clarifications, and missing pieces in
  `docs/` are always welcome.
- **Report security issues** — see [SECURITY.md](SECURITY.md). Do **not**
  open public issues for vulnerabilities.

---

## Reporting Bugs

Open a GitHub Issue and include:

- Steps to reproduce (minimal reproduction preferred)
- Expected vs. actual behaviour
- Nebu version or commit SHA
- Environment details (OS, Docker version, browser if UI-related)

For security vulnerabilities, see [SECURITY.md](SECURITY.md) and do **not**
open a public issue.

Issue templates in `.github/ISSUE_TEMPLATE/` simplify this process and will
be added in a future story (Story 8.7).

---

## Submitting a Pull Request

### Branch naming

```text
feat/<topic>   — new feature or capability
fix/<topic>    — bug fix
docs/<topic>   — documentation only
chore/<topic>  — maintenance, tooling
```

### Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```text
feat(auth): add PKCE support to authorization code flow
fix(sync): include m.room.member leave event in state.events
docs: update CONTRIBUTING.md
```

Keep the subject under 70 characters. Use the body to explain _why_, not
_what_.

**Important — no AI-attribution trailers:** Do **not** include
`Co-Authored-By: Claude ...` or similar AI-attribution trailers in commit
messages. Attribution is handled at the project level in README.md and
CONTRIBUTING.md. Adding these trailers was a pre-2026-04-23 practice that
has been removed from the history (see Story 8.1).

### Review expectations

PRs will receive a code review from a maintainer. For non-trivial changes,
expect:

- Feedback on test coverage (unit + integration)
- Feedback on API/interface design
- A security pass for auth/middleware/SQL changes

Allow approximately five business days for initial review.

### PR checklist

1. **Fork** the repository and create a feature branch from `main`.
2. **Make your changes.** Follow the conventions below.
3. **Add tests.** Every new behaviour needs a test. Bug fixes need a
   regression test.
4. **Run the test suite locally** before pushing:

   ```bash
   make test-unit-go && make test-unit-elixir
   ```

5. **Open a PR** against `main`. Fill out the PR template. Link related
   issues (`Fixes #123`).
6. **Respond to review feedback.** Push additional commits; we squash on
   merge.

PRs should stay focused — one logical change per PR. If you find unrelated
issues, open separate PRs for them.

### What blocks merging

- Failing CI (unit, integration, or E2E tests)
- Unresolved review comments
- Missing tests for new behaviour
- Missing docs for user-facing changes
- Security-sensitive changes without a security review (maintainers flag
  these)

---

## Development Workflow (BMAD)

Internal maintainers use the
[BMad Method](https://docs.bmad-method.org/)
(BMAD — _Build More Architect Dreams_) pipeline:

```text
Story Creation → Acceptance-Test Scaffold (ATDD) → Implementation
    → Test Review → Code Review → conditional Security Review
```

**External contributors are NOT required to run the BMAD pipeline.** You
may submit a PR directly against `main` without creating a story file or
running any BMAD agent. Maintainers will integrate your change into the
pipeline if it is accepted.

If you want to understand the full pipeline, see
[CLAUDE.md](CLAUDE.md) (checked into the repo) and the `_bmad-output/`
directory for example story files.

---

## Development Setup

**Prerequisites:** Docker Desktop, `make`, `git`. No local Go or Elixir
installation required.

```bash
git clone <your-fork-url> nebu
cd nebu
make setup     # generates .secrets/internal_secret and dev credentials
make dev       # starts the full stack via docker compose
```

Full setup walkthrough including the Bootstrap Wizard and dev users:
[`README.md`](README.md#quick-start).

### Rebuilding images

```bash
make build-gateway        # rebuild gateway image
make build-core           # rebuild core image
# Or, preferred for local iteration:
docker compose up --build
```

### Resetting local state

```bash
# Wipe bootstrap config so you can re-run the wizard:
docker compose exec postgres psql -U nebu -d nebu -c \
  "DELETE FROM server_config WHERE key IN \
  ('bootstrap_completed','oidc_issuer','oidc_client_id', \
   'oidc_client_secret','instance_name');"
```

---

## Code Conventions

For the authoritative Go and Elixir conventions, see
[CLAUDE.md](CLAUDE.md) in the repository root (checked into git).
Key rules for contributors:

### Go (gateway, media)

- Explicit error handling — no `panic` in library code.
- `context.Context` is always the first parameter.
- Define interfaces on the consumer side; keep them small.
- Follow `gofmt` / `goimports`. CI enforces this.

### Elixir (core)

- GenServer state changes go through `handle_*` callbacks — never mutate
  from outside.
- Prefer "let it crash" with supervisors over defensive `try/rescue`.
- All validations belong in Ecto changesets. No direct `Repo.insert!` with
  unvalidated input.
- Default supervision strategy is `:one_for_one`. Document any deviation.
- Use `via`-tuples or `Registry` for process registration — never bare
  atoms.

### SQL / migrations

- Migrations live in `gateway/migrations/` (`golang-migrate`).
- Every migration must be reversible (`.up.sql` + `.down.sql`).
- The audit log is append-only: no `UPDATE`, no `DELETE`, enforced by Row
  Security Policy.

### API changes (Go Gateway)

Nebu follows an OpenAPI-first approach. New or modified HTTP endpoints
require changes to `openapi.yaml` first, followed by `make gen-api` to
regenerate the handler stubs.

---

## Testing

### What is expected

- **Unit tests** for all new functions/modules (Go: `_test.go` files;
  Elixir: ExUnit)
- **Integration tests** for new HTTP endpoints (Godog / `net/http` level)
- **E2E tests** for Admin UI changes (Playwright against the running stack)

### Running tests

```bash
make test-unit-go         # Go unit tests
make test-unit-elixir     # Elixir unit tests
make test-integration     # Godog HTTP/gRPC tests (stack must be running)
make test-e2e             # Playwright E2E tests (stack must be running)
```

### TDD standard

Write the failing test _before_ writing the implementation. The
red-green-refactor cycle (TDD) is the project standard for maintainers, not
a suggestion, and is strongly recommended for external contributors.

PRs that add implementation without tests will be asked to add tests before
merge.

---

## Architecture and Docs

Before making significant changes, skim the relevant architecture docs so
your PR aligns with existing decisions:

- High-level architecture: [`docs/architecture/`](docs/architecture/)
- Architecture Decision Records:
  [`docs/architecture/adr/`](docs/architecture/adr/)
- Matrix API scope: [`docs/matrix-api-scope.md`](docs/matrix-api-scope.md)

If your change contradicts an existing ADR, propose a new ADR as part of
your PR explaining the trade-off.

---

## License and DCO

Nebu is licensed under the **Apache License 2.0**. By submitting a pull
request, you agree (per Apache 2.0, Section 5) that your contribution is
licensed under the same terms. No separate Contributor License Agreement
(CLA) is required.

**DCO sign-off (recommended):** Sign your commits with a Developer
Certificate of Origin (DCO) sign-off for audit-trail purposes:

```bash
git commit -s -m "feat(gateway): your change"
```

This adds a `Signed-off-by: Your Name <email>` trailer. It is not enforced
by CI, but appreciated.

---

## Code of Conduct

We expect respectful, constructive communication in all project spaces
(issues, PRs, discussions). The full text is in
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). The short version: be kind, be
direct, focus on the work.

---

## Questions?

Open an issue with the `question` label — that is the canonical place to
ask. We prefer public discussion so answers are searchable for the next
person.
