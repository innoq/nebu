---
security_review: optional
---

# Story 8.7: Issue/PR-Templates for Dual-Host (.github/ + .gitlab/)

Status: ready-for-dev

## Story

**As a** new contributor opening their first issue or pull/merge request on either the GitHub or opencode.de GitLab mirror,
**I want** structured templates guiding me to provide the right information,
**so that** maintainer triage is fast and consistent regardless of which platform the contributor uses.

**Size:** S

---

## Background

### Strategic Pivot: Dual-Host Templates

The original story title was "GitHub Issue/PR-Templates + .github/-Infrastructure". With the dual-host sovereign-OSS strategy (GitHub Actions + GitLab CI, established in Story 8.6), template infrastructure must cover both platforms:

- `github.com/innoq/nebu` — GitHub issue templates (`.github/ISSUE_TEMPLATE/*.yml`) + PR template (`.github/pull_request_template.md`)
- `gitlab.opencode.de/nebu/nebu-server` — GitLab issue templates (`.gitlab/issue_templates/*.md`) + MR template (`.gitlab/merge_request_templates/*.md`)

GitLab does not support YAML form-field templates for issue templates (only Markdown). GitHub supports both YAML form-fields and Markdown; this story uses YAML form-fields for GitHub issue templates for richer UX, while GitLab templates mirror the same content as Markdown.

### GitHub Infrastructure

Beyond templates, this story delivers:

- `.github/CODEOWNERS` — ensures every PR is automatically reviewed by the global maintainer (`@philippbeyerlein`). Other owners can be added later as the contributor base grows.
- `.github/dependabot.yml` — automated dependency updates for all package ecosystems used by this project: Go modules, Elixir/Mix, GitHub Actions, and Docker base images. Weekly cadence to avoid alert fatigue.

### Connections to Other Stories

- Story 8.3 (CONTRIBUTING.md) — forward-references issue templates ("see `.github/ISSUE_TEMPLATE/`"); this story resolves that reference.
- Story 8.4 (SECURITY.md) — `config.yml` links to SECURITY.md's "Report a vulnerability" flow; the link must resolve.
- Story 8.6 (CI) — `dependabot.yml` updates GitHub Actions workflow dependencies on a `github-actions` schedule.
- Story 8.8 (Badges/Metadata) — CODEOWNERS is part of repo infrastructure referenced by the release readiness gate.
- Story 8.9 (Release-Readiness-Gate) — verifies this story's artefacts via `scripts/verify-issue-pr-templates.sh`.

### No `FUNDING.yml`

The epics.md originally listed `.github/FUNDING.yml` as optional. Since no sponsoring programme is currently active and this file has no functional impact on contributor triage, it is explicitly excluded from this story to keep scope tight.

---

## Acceptance Criteria

1. **`.github/ISSUE_TEMPLATE/bug_report.yml` exists**, is parseable as YAML, and contains form fields for: `description`, `steps_to_reproduce`, `expected_behaviour`, `actual_behaviour`, `version` (with a `main`/commit-SHA placeholder), and `environment` (Go version, Elixir/OTP version, OS). The `name:` field is `Bug Report` and `labels:` includes `bug`.

2. **`.github/ISSUE_TEMPLATE/feature_request.yml` exists**, is parseable as YAML, and contains form fields for: `motivation`, `proposed_solution`, `alternatives_considered`, and `willing_to_contribute` (checkbox). The `name:` field is `Feature Request` and `labels:` includes `enhancement`.

3. **`.github/ISSUE_TEMPLATE/config.yml` exists**, is parseable as YAML, contains `blank_issues_enabled: false`, and includes `contact_links` entries pointing to `SECURITY.md` (for security issues) and GitHub Discussions (or the issues page) as the fallback for questions.

4. **`.github/pull_request_template.md` exists** and contains all of the following sections: `## What / Why / How`, `## Linked Issue(s)`, `## Acceptance Criteria`, `## Tests`, a DCO reminder (`git commit -s`), and a _negative_ reminder that `Co-Authored-By: Claude` trailers MUST NOT be added to commits.

5. **`.github/CODEOWNERS` exists** and contains a global ownership line (`*`) assigned to `@philippbeyerlein`. Comments explain that ownership can be narrowed later.

6. **`.github/dependabot.yml` exists**, is parseable as YAML, and defines update schedules for exactly four package ecosystems: `gomod`, `mix` (or `hex`), `github-actions`, and `docker`. All schedules use `interval: "weekly"`.

7. **`.gitlab/issue_templates/Bug.md` exists** and its content mirrors the GitHub `bug_report.yml` fields: description, steps to reproduce, expected vs actual behaviour, version, and environment.

8. **`.gitlab/issue_templates/Feature.md` exists** and its content mirrors the GitHub `feature_request.yml` fields: motivation, proposed solution, alternatives considered, and willingness to contribute.

9. **`.gitlab/merge_request_templates/Default.md` exists** and mirrors the GitHub PR template: same sections (`What / Why / How`, `Linked Issue(s)`, `Acceptance Criteria`, `Tests`), DCO reminder, and the no-`Co-Authored-By: Claude` reminder.

10. **No emojis** in any template or infrastructure file created by this story. The Python3 Unicode range check (U+1F000–U+1FFFF, U+2600–U+27BF) must return zero matches for all ten files.

11. **Markdownlint clean**: All `.md` files created by this story pass `markdownlint` with zero new errors (baseline-delta approach: only new files are checked; existing files' baseline is not changed).

12. **`scripts/verify-issue-pr-templates.sh` exists**, is executable, is shellcheck clean, and exits 0 when all 13 acceptance tests pass, 1 when any fail.

---

## Acceptance Tests

### Tests written FIRST (before implementation code):

All tests are implemented as Bash test functions in `scripts/verify-issue-pr-templates.sh`. The script uses `REPO_ROOT` (set via `cd "$(git rev-parse --show-toplevel)"` and all file paths are relative to that root). It uses `mktemp -d` + `trap cleanup EXIT` for any temp-file operations. No external test framework is required. Exit 0 on all-pass, 1 on any failure, with per-test PASS/FAIL summary.

1. **`test_bug_report_yml_valid_yaml`** — Python3 YAML parse check
   - Given: `.github/ISSUE_TEMPLATE/bug_report.yml` exists
   - When: `python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)" < .github/ISSUE_TEMPLATE/bug_report.yml`
   - Then: Exit code 0; no YAML parse error

2. **`test_feature_request_yml_valid_yaml`** — Python3 YAML parse check
   - Given: `.github/ISSUE_TEMPLATE/feature_request.yml` exists
   - When: `python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)" < .github/ISSUE_TEMPLATE/feature_request.yml`
   - Then: Exit code 0; no YAML parse error

3. **`test_config_yml_blank_issues_disabled`** — Python3 YAML parse + key check
   - Given: `.github/ISSUE_TEMPLATE/config.yml` exists
   - When: `python3 -c "import yaml, sys; d=yaml.safe_load(sys.stdin); sys.exit(0 if d.get('blank_issues_enabled') is False else 1)" < .github/ISSUE_TEMPLATE/config.yml`
   - Then: Exit code 0 (i.e., `blank_issues_enabled` is exactly `false`)

4. **`test_pr_template_required_sections`** — grep checks against `.github/pull_request_template.md`
   - Given: `.github/pull_request_template.md` exists
   - When (4a): `grep -q "Acceptance Criteria" .github/pull_request_template.md`
   - Then (4a): Match found
   - When (4b): `grep -q "DCO\|git commit -s" .github/pull_request_template.md`
   - Then (4b): Match found
   - When (4c): `grep -q "Co-Authored-By.*Claude\|Co-Authored-By: Claude" .github/pull_request_template.md`
   - Then (4c): Match found (the trailer name is mentioned in the prohibition; the test verifies the phrase is present, not that it is absent — the _content_ of the template instructs not to use it)

5. **`test_codeowners_has_global_owner`** — grep check
   - Given: `.github/CODEOWNERS` exists
   - When: `grep -qE "^\* +@" .github/CODEOWNERS`
   - Then: Match found (anchored `*` followed by at least one GitHub handle)

6. **`test_dependabot_yml_valid_yaml_and_four_ecosystems`** — Python3 check
   - Given: `.github/dependabot.yml` exists
   - When: Parse YAML and count entries in `updates[]`; assert count equals 4
   - Then: Exit code 0; exactly four update entries present
   - Ecosystems verified: `gomod`, `mix` (or `hex`), `github-actions`, `docker`

7. **`test_gitlab_bug_md_exists`** — filesystem check
   - Given: `.gitlab/issue_templates/Bug.md` created
   - When: `[[ -f .gitlab/issue_templates/Bug.md ]]`
   - Then: True

8. **`test_gitlab_feature_md_exists`** — filesystem check
   - Given: `.gitlab/issue_templates/Feature.md` created
   - When: `[[ -f .gitlab/issue_templates/Feature.md ]]`
   - Then: True

9. **`test_gitlab_default_mr_template_required_sections`** — grep checks against `.gitlab/merge_request_templates/Default.md`
   - Given: `.gitlab/merge_request_templates/Default.md` exists
   - When (9a): `grep -q "Acceptance Criteria" .gitlab/merge_request_templates/Default.md`
   - Then (9a): Match found
   - When (9b): `grep -q "DCO\|git commit -s" .gitlab/merge_request_templates/Default.md`
   - Then (9b): Match found

10. **`test_required_fields_parity_github_gitlab_bug`** — grep checks for shared required fields
    - Given: `.github/ISSUE_TEMPLATE/bug_report.yml` and `.gitlab/issue_templates/Bug.md` both exist
    - When (10a): GitHub template contains "version" (case-insensitive)
    - Then (10a): Match found
    - When (10b): GitLab template contains "version" (case-insensitive)
    - Then (10b): Match found
    - When (10c): GitHub template contains "environment" or "Environment"
    - Then (10c): Match found
    - When (10d): GitLab template contains "environment" or "Environment"
    - Then (10d): Match found
    - When (10e): GitHub template contains "steps" (case-insensitive)
    - Then (10e): Match found
    - When (10f): GitLab template contains "steps" (case-insensitive)
    - Then (10f): Match found

11. **`test_no_emojis_in_any_template`** — Python3 Unicode range scan
    - Given: All ten files created by this story
    - When: Python3 scans each file for characters in ranges U+1F000–U+1FFFF and U+2600–U+27BF
    - Then: Zero matches across all files

12. **`test_md_templates_markdownlint_clean`** — markdownlint delta check
    - Given: The four `.md` files: `pull_request_template.md`, `Bug.md`, `Feature.md`, `Default.md`
    - When: `cd ${REPO_ROOT} && npx --yes markdownlint-cli <file>` for each
    - Then: Exit code 0, zero errors per file
    - Note: If `npx` is not available, test prints a SKIP message and exits 0 for that sub-check only

13. **`test_verify_script_shellcheck_clean`** — shellcheck
    - Given: `scripts/verify-issue-pr-templates.sh` exists
    - When: `shellcheck --severity=error scripts/verify-issue-pr-templates.sh` (SKIP with exit 0 if shellcheck not in PATH)
    - Then: Exit code 0; no errors

**Persistenz-Strategie:** Not applicable — static configuration files and a shell script; no application state, no crash/restart test required.

---

## Risks and Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **GitHub YAML form-field syntax changes** — GitHub's issue form schema evolves | LOW | YAML is validated via python3 parse (tests 1/2/3). Fields use documented `type: textarea`/`type: checkboxes` syntax; review GitHub docs before implementation. |
| **GitLab-GitHub content drift over time** — templates diverge as one platform gets updates | LOW | Test 10 (parity check on required fields) catches drift for the most critical fields. Content should be kept in sync manually on each update. |
| **CODEOWNERS handle mismatch** — wrong username blocks auto-review assignment | LOW | Handle `@philippbeyerlein` is the GitHub username of the maintainer. Test 5 checks the format, not the handle validity — a comment in CODEOWNERS advises updating on handle change. |
| **`dependabot.yml` ecosystem key names** — `mix` vs `hex` naming varies in docs | LOW | Verified: GitHub Dependabot uses `mix` as the ecosystem key for Elixir projects. Test 6 checks for either `mix` or `hex` to be resilient. |
| **Markdownlint MD041 (first line must be heading)** — PR templates often start with a comment | LOW | PR/MR templates should start with a Markdown heading. If a comment block is needed before content, use an HTML comment `<!-- -->` which markdownlint ignores. |
| **`contact_links` SECURITY.md URL** — must point to the correct relative path | LOW | SECURITY.md lives in the repo root; GitHub resolves relative URLs in `contact_links` from repo root. Use the full GitHub URL for the `url:` field to be safe. |

---

## Implementation Notes

### `.github/ISSUE_TEMPLATE/bug_report.yml` — Outline

```yaml
name: Bug Report
description: Report a bug or unexpected behaviour in Nebu.
labels: ["bug"]
body:
  - type: markdown
    attributes:
      value: |
        Before submitting, please search existing issues to avoid duplicates.
        For security vulnerabilities, do NOT use this form — see SECURITY.md.
  - type: textarea
    id: description
    attributes:
      label: Description
      description: A clear and concise description of the bug.
    validations:
      required: true
  - type: textarea
    id: steps_to_reproduce
    attributes:
      label: Steps to Reproduce
      description: Step-by-step instructions to reproduce the behaviour.
      placeholder: |
        1. Start the server with ...
        2. Send request to ...
        3. Observe ...
    validations:
      required: true
  - type: textarea
    id: expected_behaviour
    attributes:
      label: Expected Behaviour
      description: What did you expect to happen?
    validations:
      required: true
  - type: textarea
    id: actual_behaviour
    attributes:
      label: Actual Behaviour
      description: What actually happened?
    validations:
      required: true
  - type: input
    id: version
    attributes:
      label: Nebu Version
      description: "Branch (main) or commit SHA. Example: main / abc1234"
      placeholder: "main"
    validations:
      required: true
  - type: textarea
    id: environment
    attributes:
      label: Environment
      description: Go version, Elixir/OTP version, OS.
      placeholder: |
        Go: 1.26
        Elixir: 1.19 / OTP 27
        OS: Ubuntu 24.04
    validations:
      required: true
```

### `.github/ISSUE_TEMPLATE/feature_request.yml` — Outline

```yaml
name: Feature Request
description: Propose a new feature or enhancement for Nebu.
labels: ["enhancement"]
body:
  - type: markdown
    attributes:
      value: |
        Please describe the problem you want to solve, not just the solution.
  - type: textarea
    id: motivation
    attributes:
      label: Motivation
      description: What problem does this feature solve? What is the use case?
    validations:
      required: true
  - type: textarea
    id: proposed_solution
    attributes:
      label: Proposed Solution
      description: Describe the solution you would like.
    validations:
      required: true
  - type: textarea
    id: alternatives_considered
    attributes:
      label: Alternatives Considered
      description: What alternative solutions or features have you considered?
  - type: checkboxes
    id: willing_to_contribute
    attributes:
      label: Contribution
      options:
        - label: I am willing to submit a pull request for this feature.
```

### `.github/ISSUE_TEMPLATE/config.yml` — Outline

```yaml
blank_issues_enabled: false
contact_links:
  - name: Security Vulnerability
    url: https://github.com/innoq/nebu/security/advisories/new
    about: >
      Report a security vulnerability via GitHub Security Advisories.
      Do NOT open a public issue for vulnerabilities. See SECURITY.md.
  - name: Questions and Discussion
    url: https://github.com/innoq/nebu/discussions
    about: Ask questions or discuss ideas in GitHub Discussions.
```

### `.github/pull_request_template.md` — Outline

```markdown
## What / Why / How

<!-- Summarise what this PR changes, why the change is needed, and
     how it is implemented. Link to the relevant story file if applicable. -->

## Linked Issue(s)

<!-- Closes #NNN -->

## Acceptance Criteria

- [ ] All acceptance criteria from the linked story are met.
- [ ] (List specific ACs here if helpful for the reviewer.)

## Tests

- [ ] New unit tests added (or N/A with reason)
- [ ] Integration tests updated (or N/A with reason)
- [ ] All tests pass locally (`make test-unit-go`, `make test-unit-elixir`)

## Checklist

- [ ] `git commit -s` (DCO sign-off) applied to all commits in this PR.
- [ ] `Co-Authored-By: Claude` trailers are NOT present in any commit message.
  Adding AI-attribution trailers is prohibited — see CONTRIBUTING.md.
```

### `.github/CODEOWNERS` — Outline

```
# CODEOWNERS — initial global ownership
# All files are owned by the primary maintainer.
# Ownership can be narrowed by path as the contributor base grows.
# See: https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners

* @philippbeyerlein
```

### `.github/dependabot.yml` — Outline

```yaml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 5

  - package-ecosystem: "mix"
    directory: "/core"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 5

  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 5

  - package-ecosystem: "docker"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 5
```

Note: `mix` uses `/core` as the directory since Elixir code lives under `core/`. Adjust if the `mix.exs` entrypoint moves.

### `.gitlab/issue_templates/Bug.md` — Outline

GitLab Markdown issue templates use section headers and prompt text (no form fields):

```markdown
## Description

<!-- A clear and concise description of the bug. -->

## Steps to Reproduce

<!-- Step-by-step instructions to reproduce the behaviour.
1.
2.
3. -->

## Expected Behaviour

<!-- What did you expect to happen? -->

## Actual Behaviour

<!-- What actually happened? -->

## Version

<!-- Branch (main) or commit SHA. Example: main / abc1234 -->

## Environment

<!-- Go version, Elixir/OTP version, OS.
Example:
Go: 1.26
Elixir: 1.19 / OTP 27
OS: Ubuntu 24.04 -->
```

### `.gitlab/issue_templates/Feature.md` — Outline

```markdown
## Motivation

<!-- What problem does this feature solve? What is the use case? -->

## Proposed Solution

<!-- Describe the solution you would like. -->

## Alternatives Considered

<!-- What alternative solutions or features have you considered? -->

## Contribution

<!-- Are you willing to submit a merge request for this feature? (yes / no) -->
```

### `.gitlab/merge_request_templates/Default.md` — Outline

```markdown
## What / Why / How

<!-- Summarise what this MR changes, why the change is needed, and
     how it is implemented. Link to the relevant story file if applicable. -->

## Linked Issue(s)

<!-- Closes #NNN -->

## Acceptance Criteria

- [ ] All acceptance criteria from the linked story are met.
- [ ] (List specific ACs here if helpful for the reviewer.)

## Tests

- [ ] New unit tests added (or N/A with reason)
- [ ] Integration tests updated (or N/A with reason)
- [ ] All tests pass locally (`make test-unit-go`, `make test-unit-elixir`)

## Checklist

- [ ] `git commit -s` (DCO sign-off) applied to all commits in this MR.
- [ ] `Co-Authored-By: Claude` trailers are NOT present in any commit message.
  Adding AI-attribution trailers is prohibited — see CONTRIBUTING.md.
```

### `scripts/verify-issue-pr-templates.sh` — Design Notes

Follow the established pattern from `scripts/verify-ci-config.sh` (Story 8.6) and `scripts/verify-contributing.sh` (Story 8.3):

- Set `REPO_ROOT` via `git rev-parse --show-toplevel`; `cd "${REPO_ROOT}"` at the top, use relative paths throughout (lesson from 8.2/8.3 markdownlint false-negatives).
- `run_test` helper function: accepts test name + exit-code variable; increments pass/fail counters.
- `cleanup` function registered via `trap cleanup EXIT`; uses `mktemp -d` for temp sandboxes.
- Python3 YAML validation via `-c "import yaml, sys; yaml.safe_load(sys.stdin)"` (lesson from 8.6 test_1/2/3).
- Python3 emoji scan: `python3 -c "import sys; data=open(f).read(); bad=[c for c in data if ord(c) in range(0x1F000,0x20000) or ord(c) in range(0x2600,0x27C0)]; sys.exit(1 if bad else 0)"` — same pattern as 8.2/8.3.
- markdownlint check uses `cd ${REPO_ROOT}` before `npx` invocation (lesson from 8.3 AC10 false-negative).
- `shellcheck` check includes SKIP fallback: `if ! command -v shellcheck &>/dev/null; then echo "SKIP: shellcheck not found"; return 0; fi` (lesson from 8.5/8.6).
- All grep patterns are anchored where intent is anchored (lesson from 8.4 code review).
- `return 0` at end of each test function (not `exit 0`) to allow the harness to continue (lesson from 8.2 mktemp/cleanup-trap).
- The script self-tests: test 13 shellchecks the script itself.

---

## Files to Create

| File | Action |
|---|---|
| `.github/ISSUE_TEMPLATE/bug_report.yml` | CREATE |
| `.github/ISSUE_TEMPLATE/feature_request.yml` | CREATE |
| `.github/ISSUE_TEMPLATE/config.yml` | CREATE |
| `.github/pull_request_template.md` | CREATE |
| `.github/CODEOWNERS` | CREATE |
| `.github/dependabot.yml` | CREATE |
| `.gitlab/issue_templates/Bug.md` | CREATE |
| `.gitlab/issue_templates/Feature.md` | CREATE |
| `.gitlab/merge_request_templates/Default.md` | CREATE |
| `scripts/verify-issue-pr-templates.sh` | CREATE |
| `_bmad-output/implementation-artifacts/sprint-status-epic-8.yaml` | UPDATE: story `8-7-github-issue-pr-templates-github-infrastructure` → `done` after merge |

**Explicitly NOT part of this story:**

- `.github/FUNDING.yml` — no active sponsoring programme; deferred indefinitely.
- Any `security_disclosure.yml` issue template — SECURITY.md (Story 8.4) and `config.yml`'s `contact_links` already route security reporters to GitHub Security Advisories. A separate form template is redundant.
- Any changes to existing CI workflows, README, CONTRIBUTING.md, or SECURITY.md.
- Any changes to source code, migrations, or application logic.

---

## Context: Epic 8

Epic 8 prepares Nebu for public release on GitHub and the opencode.de GitLab mirror. Story 8.7 delivers the contributor-facing triage infrastructure that CONTRIBUTING.md (Story 8.3) forward-referenced. With this story, both platforms have equivalent structured templates so contributor experience is consistent regardless of where an issue or PR is opened.

Dependencies:

- **Story 8.3** (CONTRIBUTING.md) — forward-references issue templates; this story resolves those references.
- **Story 8.4** (SECURITY.md) — `config.yml` contact link points to the GitHub Security Advisories flow; SECURITY.md must exist.
- **Story 8.6** (CI) — `dependabot.yml` manages GitHub Actions workflow updates; CI workflows must exist for Dependabot to track them.
- **Story 8.8** (Badges/Metadata) — CODEOWNERS is part of repo infrastructure.
- **Story 8.9** (Release-Readiness-Gate) — `scripts/verify-issue-pr-templates.sh` must exit 0 before the gate can pass green.
