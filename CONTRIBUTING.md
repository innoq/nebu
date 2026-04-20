# Contributing to Nebu

Thanks for your interest in contributing. This document covers what you need to know to get a patch from your machine into `main`.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

---

## Ways to contribute

- **Report bugs** — open an issue with steps to reproduce, expected vs. actual behavior, and environment details (OS, Docker version, browser if UI-related).
- **Suggest features** — open an issue describing the problem you want to solve, _before_ opening a PR. For non-trivial changes we'd rather align on direction first.
- **Fix bugs / add features** — pick an open issue (look for `good-first-issue` if you're new), comment that you're working on it, then open a PR.
- **Improve docs** — corrections, clarifications, and missing pieces in `docs/` are always welcome.
- **Report security issues** — see [SECURITY.md](SECURITY.md). Do **not** open public issues for vulnerabilities.

---

## Development setup

**Prerequisites:** Docker Desktop, `make`, `git`. No local Go or Elixir installation required.

```bash
git clone <your-fork-url> nebu
cd nebu
make setup     # generates .secrets/internal_secret and dev credentials
make dev       # starts the full stack via docker compose
```

Full setup walkthrough including the Bootstrap Wizard and dev users: [`README.md`](README.md#quick-start).

### Running tests

```bash
make test-unit-go         # Go unit tests
make test-unit-elixir     # Elixir unit tests
make test-integration     # Godog HTTP/gRPC integration tests (stack must be running)
make test-e2e             # Playwright browser E2E tests (stack must be running, dex in /etc/hosts)
```

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
  "DELETE FROM server_config WHERE key IN ('bootstrap_completed','oidc_issuer','oidc_client_id','oidc_client_secret','instance_name');"
```

---

## Coding conventions

### Go (gateway, media)

- Explicit error handling. No `panic` in library code.
- `context.Context` is always the first parameter.
- Define interfaces on the consumer side; keep them small.
- Follow `gofmt` / `goimports`. CI enforces this.

### Elixir (core)

- GenServer state changes go through `handle_*` callbacks — never mutate from outside.
- Prefer "let it crash" with supervisors over defensive `try/rescue`.
- All validations belong in Ecto changesets. No direct `Repo.insert!` with unvalidated input.
- Default supervision strategy is `:one_for_one`. Document any deviation in a comment.
- Use `via`-tuples or `Registry` for process registration — never bare atoms.

### SQL / migrations

- Migrations live in `gateway/migrations/` and are owned by the gateway (`golang-migrate`).
- Every migration must be reversible (`.up.sql` + `.down.sql`).
- The audit log is append-only: no `UPDATE`, no `DELETE`, enforced by Row Security Policy.

### Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(auth): add PKCE support to authorization code flow
fix(sync): include m.room.member leave event in rooms.leave state.events
docs(readme): clarify dex /etc/hosts requirement
```

Keep the subject under 70 characters. Use the body to explain _why_, not _what_.

---

## Pull request process

1. **Fork** the repository and create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-change
   ```
2. **Make your changes.** Follow the conventions above.
3. **Add tests.** Every new behavior needs a test. Bug fixes need a regression test.
4. **Run the test suite locally** before pushing:
   ```bash
   make test-unit-go && make test-unit-elixir
   ```
5. **Open a PR** against `main`. Fill out the PR template. Link related issues (`Fixes #123`).
6. **Respond to review feedback.** Push additional commits; we'll squash on merge.

PRs should stay focused — one logical change per PR. If you find unrelated issues along the way, open separate PRs for them.

### What blocks merging

- Failing CI (unit, integration, or E2E tests)
- Unresolved review comments
- Missing tests for new behavior
- Missing docs for user-facing changes
- Security-sensitive changes without a security review — the maintainers will flag these

---

## Architecture and docs

Before making significant changes, skim the relevant architecture docs so your PR aligns with existing decisions:

- High-level architecture: [`docs/architecture/`](docs/architecture/)
- Architecture Decision Records: [`docs/architecture/adr/`](docs/architecture/adr/)
- Matrix API scope: [`docs/matrix-api-scope.md`](docs/matrix-api-scope.md)

If your change contradicts an existing ADR, propose a new ADR as part of your PR explaining the trade-off.

---

## Questions?

Open an issue with the `question` label — that's the canonical place to ask. We prefer public discussion so answers are searchable for the next person.
