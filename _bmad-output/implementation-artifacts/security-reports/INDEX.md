# Audit Trail -- Per-Story Security Reviews (Kassandra)

This index covers only SEC Gate 1 per-story reviews produced during
Epic 8 (GitHub Readiness). It is a point-in-time audit index as of
Story 8.9.

The mandatory SEC Gate 2 epic-end Kassandra review (full-epic diff)
is executed in Story 8.10. That review supersedes this index as the
authoritative audit record for Epic 8.

Open MEDIUMs and LOWs listed below are either fixed inline (noted
in sprint-status) or deferred with explicit justification in the
respective report. No CRITICAL or HIGH findings remain open.

---

## Reports

### Story 8.1 -- Commit-History-Rewrite Tooling

- **File:**
  `_bmad-output/implementation-artifacts/security-reports/8-1-commit-history-rewrite-security-review.md`
- **Classification:** CLEAN
- **CRITICAL:** 0 / **HIGH:** 0 / **MEDIUM:** 2 / **LOW:** 2
- **Open MEDIUMs/LOWs:**
  - MEDIUM-1: No repo-identity guard before `git filter-repo --force`
    (deferred; runbook instructs explicit branch checkout).
  - MEDIUM-2: Runbook used `git push --force` instead of
    `--force-with-lease` -- fixed in PUBLIC_PUSH_CHECKLIST.md (Step 4)
    and REWRITE_HISTORY_RUNBOOK.md (Step 5).
  - LOW-1: Trailer regex may over-match body paragraphs (low
    probability; deferred pending actual history content review).
  - LOW-2: Backup branch may carry historical secrets; runbook now
    warns against `git push --all` while backup branch exists.

---

### Story 8.5 -- Secret-Scan Gate (gitleaks + history scan)

- **File:**
  `_bmad-output/implementation-artifacts/security-reports/8-5-secret-scan-gate-security-review.md`
- **Classification:** CLEAN
- **CRITICAL:** 0 / **HIGH:** 0 / **MEDIUM:** 2 / **LOW:** 2
- **Open MEDIUMs/LOWs:**
  - MEDIUM-1: `.gitleaks.toml` allow-list paths `_bmad/.*` and
    `_bmad-output/.*` are broad; deferred -- accepted risk with
    documented justification (no real credentials in those dirs).
  - MEDIUM-2: Pre-push hook installation via symlink broken (docs
    defect); fixed in SECRET_SCAN_RUNBOOK.md (symlink options removed).
  - LOW-1: `*/testdata/*` allow-list pattern too broad; deferred --
    no vendor `testdata` dirs currently present.
  - LOW-2: `--no-verify` bypass not documented in runbook; note added
    to SECRET_SCAN_RUNBOOK.md.

---

### Story 8.6 -- Dual CI (GitHub Actions + GitLab CI)

- **File:**
  `_bmad-output/implementation-artifacts/security-reports/8-6-dual-ci-security-review.md`
- **Classification:** HIGH (H-1 fixed inline before merge)
- **CRITICAL:** 0 / **HIGH:** 1 / **MEDIUM:** 3 / **LOW:** 2 /
  **INFO:** 3
- **H-1 disposition:** Gitleaks tarball downloaded without integrity
  verification -- fixed inline in `.github/workflows/ci.yml` and
  `.gitlab-ci.yml` before Story 8.6 was marked done.
- **Open MEDIUMs/LOWs:**
  - MEDIUM-1: `ci-local.sh` mounts repo writable into root container
    (deferred as developer-machine-only concern; follow-up story
    recommended).
  - MEDIUM-2: CI cache key on `feature/**` can poison `main` cache
    restore (deferred; insider threat vector, low likelihood).
  - MEDIUM-3: No `concurrency:` block or `timeout-minutes:` on CI
    jobs (deferred as operational hygiene; follow-up story).
  - LOW-1: `pull_request_target` guard documented as not-present
    (positive finding; no action needed).
  - LOW-2: SHA-pinning test does not cross-verify comment version
    (deferred; code review provides compensating control).

---

## Footer

Epic-end SEC Gate 2 (full-epic Kassandra review) for Epic 8 was executed
as part of Story 8.10 (Initial Public Push).

---

## Epic 9 — arc42 Documentation Generation

### Epic 9 SEC Gate 2 (mandatory epic-end review)

- **File:** `_bmad-output/implementation-artifacts/security-reports/epic-9-security-review-2026-05-01.md`
- **Classification:** CLEAN
- **CRITICAL:** 0 / **HIGH:** 0 / **MEDIUM:** 2 / **LOW:** 1
- **Open items:**
  - MEDIUM-1: Dev credentials (`changeme`, `nebu-admin-secret`) documented in `docs/getting-started.md` — dev fixture only, no production impact. Recommendation: add warning box. Suitable as follow-up story acceptance criterion.
  - MEDIUM-2: Bootstrap auto-admin mechanism explicitly documented in public arc42 docs (`08-concepts.md` line 23). Consider softer phrasing. No direct exploit path.
  - LOW-1: `actions/checkout@v6` floating tag in CI `verify-docs` job — pre-existing pattern, not new debt introduced by Epic 9.
