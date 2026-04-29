# Repo Metadata Runbook

This runbook covers how to apply GitHub and GitLab repository metadata
(description, topics) to the Nebu repositories using
`scripts/setup-repo-metadata.sh`.

---

## When to run

Run this script only _after_ Story 8.10 (Initial Public Push) is complete
and both `github.com/innoq/nebu` and
`gitlab.opencode.de/nebu/nebu-server` exist as public repositories.

_Story 8.10 gate statement:_ Repo metadata (topics, description) is applied
manually by a maintainer after the successful initial public push documented
in Story 8.10. Do not run `setup-repo-metadata.sh` against a private or
non-existent repository.

---

## Install gh and glab CLIs

### macOS (Homebrew)

```bash
brew install gh glab
```

### Linux (apt)

```bash
# GitHub CLI
type -p curl >/dev/null || sudo apt install curl -y
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  | sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
sudo apt update && sudo apt install gh -y

# GitLab CLI
curl -fsSL https://gitlab.com/gitlab-org/cli/-/releases/latest/downloads/glab_linux_amd64.tar.gz \
  | tar -xz && sudo mv glab /usr/local/bin/
```

### Authenticate

```bash
# GitHub: opens a browser-based OAuth flow
gh auth login --hostname github.com --git-protocol https

# GitLab (opencode.de instance): uses a personal access token
# Create a token at https://gitlab.opencode.de/-/user_settings/personal_access_tokens
# with scopes: api, read_repository, write_repository
glab auth login --hostname gitlab.opencode.de --token <YOUR_PERSONAL_ACCESS_TOKEN>
```

Verify authentication:

```bash
gh auth status
glab auth status
```

Required permissions:

- `gh`: token needs `repo` scope (read/write metadata)
- `glab`: token needs `api` scope

---

## Run the script

Apply metadata to both platforms in one step:

```bash
bash scripts/setup-repo-metadata.sh --all
```

Or apply per platform:

```bash
# GitHub only
bash scripts/setup-repo-metadata.sh --github

# GitLab only
bash scripts/setup-repo-metadata.sh --gitlab
```

Expected output (all):

```
[setup-repo-metadata] Applying metadata to github.com/innoq/nebu ...
[setup-repo-metadata] GitHub metadata applied.
[setup-repo-metadata] Applying metadata to gitlab.opencode.de/nebu/nebu-server ...
[setup-repo-metadata] GitLab metadata applied.
```

_Note:_ Do not run with `bash -x` or `set -x` in CI environments; the
verbose trace may expose authentication tokens in logs.

---

## Manual fallback

If the CLI tools are unavailable, apply metadata through the web UI.

### GitHub web UI

1. Open `https://github.com/innoq/nebu` and click the gear icon next to
   _About_ in the right sidebar.
2. Set _Description_ to:
   `Nebuchadnezzar -- Enterprise-grade, Matrix Client-Server API compatible
   chat server. Apache 2.0, no federation, horizontally scalable. Replaces
   Slack/Teams with full data sovereignty.`
3. Under _Topics_, add each of the following:
   `matrix`, `chat`, `messaging`, `enterprise`, `go`, `elixir`, `oidc`,
   `apache-2`, `sovereign`, `nebu`
4. Click _Save changes_.

### GitLab web UI

1. Open `https://gitlab.opencode.de/nebu/nebu-server` and click
   _Settings_ > _General_.
2. Under _Project description_, enter the same description text as above.
3. Under _Topics_, add:
   `matrix,chat,messaging,enterprise,go,elixir,oidc,apache-2,sovereign,nebu`
4. Click _Save changes_.

---

## References

- `scripts/setup-repo-metadata.sh` -- automation script
- Story 8.8 -- badge block and metadata setup
- Story 8.9 -- Release-Readiness-Gate (requires this runbook complete)
- Story 8.10 -- Initial Public Push (gate for running this runbook)
