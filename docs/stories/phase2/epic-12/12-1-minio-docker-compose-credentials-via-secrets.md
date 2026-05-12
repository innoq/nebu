---
status: done
epic: 12
story: 1
security_review: required
matrix: false
ui: false
---

# Story 12.1: MinIO Docker Compose + Credentials via Secrets

Status: done

## Story

As a system operator,
I want MinIO integrated into the Docker Compose stack with credentials managed via Docker Secrets,
so that object storage is available locally and no credentials are committed to Git.

**Size:** S

---

## Acceptance Criteria

**AC1 — MinIO service starts and is healthy:**
Given `docker-compose.yml` is updated,
When `make dev` runs,
Then a MinIO service starts, is healthy, and a bucket `nebu-media` is initialized automatically via a MinIO `mc` init container (or mc commands in the MinIO startup).

**AC2 — Credentials via Docker Secrets (not hardcoded):**
Given `make setup` is run,
When it completes,
Then `MINIO_ROOT_USER` and `MINIO_ROOT_PASSWORD` are generated and stored in `.secrets/minio_root_user` and `.secrets/minio_root_password` (not hardcoded in docker-compose.yml); values are referenced via Docker Secrets (`secrets:` block in docker-compose.yml).

**AC3 — `.gitignore` excludes `.secrets/`:**
Given `.gitignore` is inspected,
When `.secrets/` is listed,
Then it is excluded from Git tracking (already present — verify no regression).

**AC4 — README and ADR-013 carry credentials warning:**
Given the README and ADR-013 are inspected,
When the credentials section is read,
Then an explicit warning is present: "These are example credentials. Replace before first production start."

**AC5 — No `.secrets/` files in git history:**
Given `git log -- .secrets/` is run,
When the output is inspected,
Then no `.secrets/` files have ever been committed (verified via gitleaks scan passing).

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

**AT-1: MinIO service defined in docker-compose.yml with secrets**
- Framework: Shell / `docker compose config` assertion (Go test or Makefile smoke)
- Given: `docker-compose.yml` is updated
- When: `docker compose config --format json` is parsed
- Then: a `minio` service entry exists; it references `minio_root_user` and `minio_root_password` secrets

**AT-2: make setup generates minio credential files**
- Framework: Shell / Makefile test
- Given: `.secrets/minio_root_user` and `.secrets/minio_root_password` do not exist
- When: `make setup` runs
- Then: both files are created, each containing a non-empty value; the existing `internal_secret` file is untouched (idempotent guard)

**AT-3: `.gitignore` still excludes `.secrets/`**
- Framework: Shell test (`git check-ignore`)
- Given: `.secrets/minio_root_user` exists as a new file
- When: `git check-ignore -q .secrets/minio_root_user` is run
- Then: exit code 0 (file is ignored); no `.secrets/` file appears in `git status`

**AT-4: gitleaks scan clean**
- Framework: gitleaks scan (CI gate already runs this; add explicit test)
- Given: the changes are staged
- When: `gitleaks detect --source . --no-git` is run
- Then: no findings; exit code 0

**AT-5: ADR-013 exists with credentials warning**
- Framework: File content assertion
- Given: `docs/architecture/adr/ADR-013-minio-object-storage.md` exists
- When: its content is read
- Then: it contains the string "Replace before first production start"

**AT-6: README credentials warning**
- Framework: File content assertion
- Given: `README.md` is read
- When: the MinIO/credentials section is inspected
- Then: it contains the string "Replace before first production start"

---

## Tasks / Subtasks

- [ ] **T1: Author ADR-013** (AC4)
  - [ ] Create `docs/architecture/adr/ADR-013-minio-object-storage.md`
  - [ ] Document decision: MinIO as default S3-compatible backend for MVP; decision space (MinIO vs. AWS S3 vs. local FS); consequences
  - [ ] Include credentials warning: "These are example credentials. Replace before first production start."

- [ ] **T2: Update `make setup`** (AC2)
  - [ ] Add generation of `.secrets/minio_root_user` (idempotent — skip if exists)
  - [ ] Add generation of `.secrets/minio_root_password` (idempotent — skip if exists)
  - [ ] Values: `openssl rand -hex 16` (32-char hex string; MinIO requires ≥8 chars for root password)
  - [ ] Print credentials warning after generation

- [ ] **T3: Add MinIO service to `docker-compose.yml`** (AC1, AC2)
  - [ ] Add `minio_root_user` and `minio_root_password` to top-level `secrets:` block
  - [ ] Add `minio` service using `minio/minio:latest` (or pin a stable tag)
  - [ ] Configure service to read credentials from secrets via `MINIO_ROOT_USER_FILE` / `MINIO_ROOT_PASSWORD_FILE` env vars
  - [ ] Add healthcheck (`mc ready local` or `curl -f http://localhost:9000/minio/health/live`)
  - [ ] Add `minio_data` volume for persistence
  - [ ] Add `createbuckets` init service (or MC sidecar) that creates the `nebu-media` bucket after MinIO is healthy
  - [ ] Expose port 9000 (API) and 9001 (MinIO Console) for local dev use

- [ ] **T4: Update `README.md`** (AC4)
  - [ ] Add MinIO section to Quick Start table (URL: http://localhost:9001, purpose: Object storage console)
  - [ ] Add credentials warning: "These are example credentials. Replace before first production start."

- [ ] **T5: Verify `.gitignore`** (AC3)
  - [ ] Confirm `.secrets/` pattern is present (already in `.gitignore` — no change needed; explicitly verify)

- [ ] **T6: Update `.gitleaks.toml`** (AC5)
  - [ ] Verify gitleaks allowlist covers any new patterns introduced (MinIO placeholder values in docker-compose.yml use secrets, not hardcoded strings — should be clean; document if any false-positives arise)

- [ ] **T7: Write / verify tests** (all ATs)
  - [ ] Add shell-level smoke tests as a Makefile target `test-compose-minio` (mirrors `test-compose-ports` pattern)
  - [ ] AT-1 through AT-6 assertions

---

## Dev Notes

### Project Context

This is Story **12.1**, the first story of Epic 12 (Media Gateway Phase 2). No MinIO service exists yet in the stack. The epic's Pre-Epic-TODO requires ADR-013 to be finalized before implementation — this story delivers that ADR.

### Critical Architecture Patterns to Follow

**Docker Secrets pattern (existing pattern in docker-compose.yml):**

```yaml
# Top-level secrets block (mirrors internal_secret pattern):
secrets:
  internal_secret:
    file: .secrets/internal_secret
  minio_root_user:
    file: .secrets/minio_root_user
  minio_root_password:
    file: .secrets/minio_root_password
```

MinIO supports reading credentials from files via:
- `MINIO_ROOT_USER_FILE=/run/secrets/minio_root_user`
- `MINIO_ROOT_PASSWORD_FILE=/run/secrets/minio_root_password`

**MinIO image tag:** Use `minio/minio:RELEASE.2024-01-18T22-51-28Z` or a similarly pinned stable release. Avoid `latest` in production-facing configs — for this story, a pinned tag is strongly preferred.

**Bucket init pattern:** MinIO does not auto-create buckets on startup. Two approaches:
1. **mc init container** — a one-shot container using `minio/mc:latest` that waits for MinIO to be healthy, then runs `mc mb minio/nebu-media`. This is the cleanest pattern.
2. **MINIO_VOLUMES + mc in entrypoint** — more complex. Avoid.

Recommended init container pattern:
```yaml
  createbuckets:
    image: minio/mc
    depends_on:
      minio:
        condition: service_healthy
    entrypoint: >
      /bin/sh -c "
      mc alias set minio http://minio:9000 $$(cat /run/secrets/minio_root_user) $$(cat /run/secrets/minio_root_password);
      mc mb --ignore-existing minio/nebu-media;
      exit 0;
      "
    secrets: [minio_root_user, minio_root_password]
```

**make setup pattern (existing in Makefile):**

```makefile
@if [ ! -f .secrets/minio_root_user ]; then \
    openssl rand -hex 16 > .secrets/minio_root_user; \
    echo "Generated .secrets/minio_root_user"; \
else \
    echo ".secrets/minio_root_user already exists, skipping"; \
fi
```

Mirror the `internal_secret` generation block exactly. Use `openssl rand -hex 16` for 32-char values (exceeds MinIO's 8-char minimum).

**ADR-013 scope:** Document:
- Decision: MinIO as S3-compatible object store for MVP (over: local filesystem, AWS S3)
- Thumbnail library: deferred to Story 12.5 (not decided here)
- Pre-Signed URL policy: deferred to Story 12.4 (not decided here)
- Credentials model: Docker Secrets for local dev, operator-injected env vars for production

### Files to Create / Modify

| Action | File | Notes |
|--------|------|-------|
| CREATE | `docs/architecture/adr/ADR-013-minio-object-storage.md` | ADR with credentials warning |
| MODIFY | `docker-compose.yml` | Add minio + createbuckets services, secrets block |
| MODIFY | `Makefile` | Extend `setup` target with minio credential generation |
| MODIFY | `README.md` | Add MinIO to Quick Start table + credentials warning |
| VERIFY | `.gitignore` | `.secrets/` already present — no change; assert in test |

### ADR-012 Consistency

ADR-012 established that this repo is a reference implementation with example-only credentials. ADR-013 must align: MinIO credentials in docker-compose.yml are example/dev values, never committed to `.secrets/`. The secret _files_ are gitignored; only the Docker Secrets _references_ (the `secrets:` block pointing to gitignored files) appear in `docker-compose.yml`.

### test-compose-ports Pattern

The existing `test-compose-ports` Makefile target (Story 5.29a) provides a good template for a `test-compose-minio` target that asserts:
1. MinIO service exists in compose config
2. Port 9000 is published
3. MinIO console port 9001 is published
4. `minio_root_user` and `minio_root_password` secrets are referenced

```makefile
test-compose-minio:
    @echo "Checking MinIO service configuration..."
    @docker compose config --format json 2>/dev/null | python3 -c "\
    import json,sys; cfg=json.load(sys.stdin); \
    svc=cfg.get('services',{}); \
    assert 'minio' in svc, 'FAIL: minio service missing'; \
    secrets=cfg.get('secrets',{}); \
    assert 'minio_root_user' in secrets, 'FAIL: minio_root_user secret missing'; \
    assert 'minio_root_password' in secrets, 'FAIL: minio_root_password secret missing'; \
    print('PASS: MinIO service and secrets configured correctly')"
```

### gitleaks Considerations

The MinIO root user/password values are stored in gitignored `.secrets/` files. The `docker-compose.yml` changes reference the secrets by name only (no values). No gitleaks allowlist additions should be required. If the `createbuckets` init container entrypoint reads credentials from `/run/secrets/` at runtime (not hardcoded), no gitleaks false-positives are expected.

If gitleaks flags any pattern, add a targeted path-scoped allowlist entry in `.gitleaks.toml` following the existing pattern with a mandatory inline comment.

### Security Concerns (why security_review: required)

- New Docker secret handling for MinIO credentials
- MinIO API port exposed to host (9000)
- Bucket `nebu-media` initialized with no public access policy — verify this is enforced in Story 12.3

For this story, the main risk is credential exposure. The Docker Secrets pattern prevents hardcoded values; `.gitignore` prevents file commits. Kassandra will verify both.

### No Matrix, No ATDD skip

This story is `matrix: false`. The Oracle Gate (Step 1b) is skipped. ATDD will generate shell-level / file-assertion tests (not Godog/Playwright) — these are infrastructure tests, but not "pure Dockerfile" infra (the Makefile setup logic and ADR content are observable and testable). Do NOT skip ATDD.

---

### ATDD Artifacts

- Test file: `gateway/test/integration/minio_compose_test.go` (6 tests — 4 failing, 2 regression guards)
- Run: `go test -tags=integration ./test/integration/ -v -run 'TestMinIO_'`
- Red phase verified: AC1, AC2, AC4 tests fail; AC3, AC5 guards pass

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6

### Debug Log References

### Completion Notes List

- All 6 acceptance tests passing (7/7 including security guard)
- `minio/mc:latest` → pinned to `RELEASE.2024-01-18T07-18-52Z` (MINOR code review fix)
- `restart: unless-stopped` added to minio service (MINOR code review fix)
- Security review: 0 CRITICAL, 0 HIGH, 2 MEDIUM advisory (dev-only ports + file permissions)

### File List

- `docs/architecture/adr/ADR-013-minio-object-storage.md` (CREATE)
- `docker-compose.yml` (MODIFY — minio + createbuckets services + secrets block)
- `Makefile` (MODIFY — setup target + test-compose-minio target)
- `README.md` (MODIFY — MinIO rows + credentials warning)
- `docs/architecture/07-deployment.md` (MODIFY — MinIO in topology + port map + setup docs)
- `docs/architecture/12-glossary.md` (MODIFY — MinIO + nebu-media terms)
