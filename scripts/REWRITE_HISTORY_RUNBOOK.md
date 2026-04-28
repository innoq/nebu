# Runbook: Co-Authored-By: Claude Trailer Removal

**Story:** 8.1  
**Status:** Tooling delivered — execution is manual, after Epic 5 is done on `main`

---

## When to Execute

Execute this runbook **only** when all three conditions are met:

1. Epic 5 is fully merged into `main` (all Stories 5.x are `done`)
2. `main` has no open PRs / in-flight stories that would add new commits
3. Story 8.5 (Secret Scan Gate) has **not yet run** — it should run against the cleaned history

Immediately before: **Story 8.10** (Initial Public Push to GitHub)

---

## Which Branches Are Affected

The rewrite runs on the **currently checked-out branch**. Repeat for each branch you intend to push publicly:

| Branch | Action |
|---|---|
| `main` | Required — primary public branch |
| `feature/github-readiness` | Required if not yet merged to `main` |
| Other long-lived branches | Required if they will be pushed to GitHub |

Do **not** rewrite branches that exist solely as backups or that will be deleted.

---

## Pre-Conditions Checklist

Before running the script, verify:

- [ ] `git status` is clean (no uncommitted changes, no staged files)
- [ ] `git-filter-repo` is installed: `which git-filter-repo`
- [ ] No existing backup branch: `git branch --list "backup/pre-history-rewrite-*"`
- [ ] You are on the correct branch: `git branch --show-current`
- [ ] Remote is up to date: `git fetch origin && git status`

---

## Step-by-Step Execution

### Step 1: Dry Run (Inspect Before Changing Anything)

```bash
git checkout main
bash scripts/rewrite-coauthored-trailer.sh --dry-run
```

Expected output: `Dry-run: would rewrite N commit(s) to remove Co-Authored-By: Claude trailers.`

If `N` is 0, there is nothing to rewrite — skip to Step 6.

### Step 2: Run the Rewrite

```bash
bash scripts/rewrite-coauthored-trailer.sh --run
```

The script will:
1. Check pre-flight conditions (clean tree, filter-repo available, no duplicate backup)
2. Create a backup branch: `backup/pre-history-rewrite-<YYYYMMDD-HHMMSS>`
3. Rewrite history via `git filter-repo --refs refs/heads/<current-branch>` (the `--refs` scope keeps the backup branch pointing at the original SHA)
4. Print the backup branch name — **note this name for rollback**

### Step 3: Verify the Rewrite

```bash
bash scripts/rewrite-coauthored-trailer.sh --verify
```

Expected output: all five sub-checks report `PASS`:
- `[no-trailer]` — zero Co-Authored-By: Claude trailers on HEAD
- `[backup-exists]` — backup branch exists
- `[count-unchanged]` — commit count identical to pre-rewrite
- `[metadata-match]` — author/committer/timestamp metadata unchanged
- `[backup-integrity]` — backup branch still carries the original Claude trailers (rollback point intact)

If any check reports `FAIL`, do **not** proceed. See Rollback section below.

### Step 4: Manual Spot-Check

```bash
# Confirm zero Claude trailers on the rewritten branch (HEAD only —
# the backup branch intentionally still carries the original trailers).
git log HEAD --oneline --grep="Co-Authored-By: Claude"
# Expected: (no output)

# Confirm human Co-Authored-By trailers (if any) are still present
git log HEAD --oneline --grep="Co-Authored-By:" | head -5

# Check a specific commit to confirm message format
git show HEAD --format="%B" | head -20
```

### Step 5: Force-Push to Remote

**This step is intentionally manual and is NOT performed by the script.**

Review the list of branches and remote before executing.

**Use `--force-with-lease` (NOT plain `--force`).** `--force-with-lease=ref:expected-sha` aborts the push if the remote has moved since your last `git fetch` — this catches accidental overwrites of work pushed by collaborators or CI bots in parallel. Plain `--force` would silently clobber such commits.

```bash
# Refresh remote-tracking refs so --force-with-lease has a current expected SHA
git fetch origin

# Inspect what will be pushed
git log --oneline origin/main..main | head -5

# Capture the expected remote SHAs (the leases)
EXPECTED_MAIN=$(git rev-parse origin/main)
EXPECTED_FEATURE=$(git rev-parse origin/feature/github-readiness)

# Force-push main with a lease
git push --force-with-lease="main:${EXPECTED_MAIN}" origin main

# Force-push feature/github-readiness with a lease (if still active)
git push --force-with-lease="feature/github-readiness:${EXPECTED_FEATURE}" origin feature/github-readiness
```

**Warning:** Force-push rewrites remote history. Coordinate with all team members beforehand. In the current single-maintainer setup (no other active contributors), this is safe to proceed without coordination — but `--force-with-lease` is still the right default in case CI bots have pushed in the meantime.

### Step 6: Post-Push Remote Verification

```bash
# Verify remote HEAD matches local HEAD
git ls-remote origin HEAD
git rev-parse HEAD
# Both should match

# Verify no Claude trailers remain on the pushed branch (fetch + check
# the remote-tracking ref, NOT --all — the local backup branch is not pushed).
git fetch origin
git log origin/main --grep="Co-Authored-By: Claude" | wc -l
# Expected: 0
```

### Step 7: Cleanup (After Story 8.10 Is Complete)

Once the public GitHub repository is live and verified:

```bash
# List backup branches
git branch --list "backup/pre-history-rewrite-*"

# Delete backup branch (only after public push is verified)
git branch -d backup/pre-history-rewrite-<TIMESTAMP>
```

---

## Rollback Procedure

If the rewrite is incorrect (wrong regex, data loss, metadata corruption):

### Local Rollback

```bash
# Find the backup branch name
git branch --list "backup/pre-history-rewrite-*"

# Reset current branch to pre-rewrite state
git reset --hard backup/pre-history-rewrite-<TIMESTAMP>

# Verify
git log --oneline | head -5
git log --grep="Co-Authored-By: Claude" | wc -l
# Expected: non-zero (original state restored)
```

### Remote Rollback (if force-push already happened)

```bash
# Refresh remote-tracking refs first
git fetch origin

# Capture expected remote SHA for the lease
EXPECTED_MAIN=$(git rev-parse origin/main)

# Force-push the backup branch content back to remote (with lease)
git push --force-with-lease="main:${EXPECTED_MAIN}" \
    origin backup/pre-history-rewrite-<TIMESTAMP>:main

# Verify remote is restored
git fetch origin
git log --oneline origin/main | head -5
```

**Emergency override:** If the remote is in a state you cannot reason about and you accept overwriting whatever is there, replace `--force-with-lease=...` with plain `--force`. Document the reason in the rollback log.

### Rollback Window

The rollback window is unlimited as long as:
1. The backup branch `backup/pre-history-rewrite-<TIMESTAMP>` exists locally
2. GitLab accepts a force-push back to the original state

The backup branch must **not** be deleted until Story 8.10 (Initial Public Push to GitHub) is marked `done` and the public repository state is verified.

---

## SHA Reference (Pre-Rewrite)

**HEAD before any rewrite:** `7180486828aef34a5168563e8614a6b69b4258b0`  
(Feature branch `feature/github-readiness` as of 2026-04-28)

After the rewrite, SHA references in `_bmad-output/` story files may contain old SHAs. These are preserved as historical audit trail and do **not** need to be updated. Use `grep -r "7180486\|aa1cc89"` over `_bmad-output/` to find all such references if needed.

---

## Why the Backup Branch Still Contains Trailers

`git filter-repo` is invoked with `--refs refs/heads/<current-branch>`, so only the current branch is rewritten. The backup branch (which was created on the original SHA right before the rewrite) is intentionally left untouched and continues to point to the pre-rewrite history. This is what makes it a valid rollback point.

As a consequence, all verification queries during/after the rewrite must scope to `HEAD` (or the specific remote-tracking ref) — using `--all` would also traverse the backup branch and surface the original trailers, which would be misleading. The script's `--verify` mode does this correctly; the manual spot-check commands in Step 4 follow the same convention.
