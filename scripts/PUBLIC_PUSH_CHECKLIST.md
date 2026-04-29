# PUBLIC PUSH CHECKLIST

Run sequentially. Do not skip steps.

_This checklist gates the initial public push to_
_`github.com/innoq/nebu` and `gitlab.opencode.de/nebu/nebu-server`._

_Do not commit `release-readiness-report.json` to history._

---

## Steps

### Step 1: Run release-readiness-gate.sh --all

Run the master gate to confirm all seven verify suites pass.

```bash
bash scripts/release-readiness-gate.sh --all
```

_Expected result:_ `GATE: 7/7 PASS` and exit code 0.

_Rollback:_ Investigate the failing suite (output names the failing script).
Fix the underlying issue before proceeding. Do not proceed to Step 2 with
a non-zero exit from this gate.

---

### Step 2: Execute history-rewrite tooling (Story 8.1)

Remove all `Co-Authored-By: Claude` trailers from the public commit history.
Run on every branch that will be pushed publicly.

```bash
git checkout main
bash scripts/rewrite-coauthored-trailer.sh --dry-run
# Review the count; if N > 0 proceed:
bash scripts/rewrite-coauthored-trailer.sh --run
```

_Expected result:_ Script prints the backup branch name and exits 0.

_Rollback:_ The backup branch
`backup/pre-history-rewrite-<TIMESTAMP>` preserves the pre-rewrite
state. Run `git reset --hard backup/pre-history-rewrite-<TIMESTAMP>`
to restore. See `scripts/REWRITE_HISTORY_RUNBOOK.md` for details.

---

### Step 3: Verify rewrite via --verify

Confirm that the rewrite succeeded and the backup branch is intact.

```bash
bash scripts/rewrite-coauthored-trailer.sh --verify
```

_Expected result:_ All five sub-checks report `PASS`:
`[no-trailer]`, `[backup-exists]`, `[count-unchanged]`,
`[metadata-match]`, `[backup-integrity]`.

_Rollback:_ If any check reports `FAIL`, do not proceed to Step 4.
Reset to the backup branch and investigate.

---

### Step 4: Force-push to BOTH remotes (GitHub + opencode)

Use `--force-with-lease` (never plain `--force`) so the push aborts
if the remote has moved since the last fetch.

```bash
git fetch origin
EXPECTED_MAIN=$(git rev-parse origin/main)
git push --force-with-lease="main:${EXPECTED_MAIN}" origin main

# Repeat for the opencode remote (adjust remote name as configured):
EXPECTED_OC=$(git rev-parse opencode/main)
git push --force-with-lease="main:${EXPECTED_OC}" opencode main
```

_Expected result:_ Both pushes complete without "rejected" errors.

_Rollback:_ If either push is rejected, run
`git push --force-with-lease` again after `git fetch`. If the remote
has diverged unexpectedly, investigate before forcing.

---

### Step 5: Apply repo metadata via setup-repo-metadata.sh --all

Set description and topics on both GitHub and GitLab after both
repositories exist as public repos.

```bash
bash scripts/setup-repo-metadata.sh --all
```

_Expected result:_

```text
[setup-repo-metadata] GitHub metadata applied.
[setup-repo-metadata] GitLab metadata applied.
```

_Rollback:_ Metadata can be re-applied at any time by re-running
the script. Changes are non-destructive.

---

### Step 6: Configure branch protection (Story 8.10)

Apply branch protection rules on both platforms as defined in
Story 8.10. This step cannot be scripted in advance; follow the
Story 8.10 implementation checklist.

_Expected result:_ `main` is protected on both GitHub and GitLab;
force-push to `main` is disabled.

_Rollback:_ Branch protection rules can be modified via the web UI
on both platforms at any time.
