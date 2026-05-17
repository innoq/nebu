---
title: "Fix CI lint-go vet error — sync/atomic.Bool noCopy in oidc_directory_test.go"
story_id: "14-5"
epic: 14
priority: P0
status: backlog
created: 2026-05-17
updated: 2026-05-17
description: |
  The CI pipeline fails at the `lint-go` step due to a `go vet` error in
  `gateway/internal/admin/oidc_directory_test.go:250`. The line `_ = warningLogged`
  copies a `sync/atomic.Bool` value, which contains `sync/atomic.noCopy`. This triggers
  a vet failure and blocks the entire CI pipeline (lint-go is the first gate; `set -euo pipefail`
  stops further jobs).

  **Root cause:** The `warningLogged` variable was declared as a placeholder for logging a
  truncation warning but is never actually used. The test only checks `buf.String()` for
  log output. The `_ = warningLogged` line was added to silence an "unused variable" warning
  but inadvertently copies the `atomic.Bool` struct.

acceptance_criteria:
  - AC1: "CI pipeline passes all jobs (lint-go, test-unit-go, test-unit-elixir, secret-scan, verify-docs)"
  - AC2: "go vet ./... passes with zero findings in gateway/"
  - AC3: "The fix does not change test behavior or assertions — only removes the unused variable + no-op assignment"

tests:
  - name: "lint-go passes (go vet ./...)"
    type: CI
    description: "Run `go vet ./...` in the gateway directory; expect zero findings"
  - name: "full ci-local.sh passes"
    type: CI
    description: "Run `scripts/ci-local.sh --all`; expect all jobs green"

security_review: not-needed

---

## Problem

```
$ scripts/ci-local.sh --all
==> lint-go
...
internal/admin/oidc_directory_test.go:250:6: assignment copies lock value to _: sync/atomic.Bool contains sync/atomic.noCopy
```

The `lint-go` job runs `go vet ./...` inside a Docker container. It fails on line 250:

```go
var warningLogged atomic.Bool   // line 246 — never used
_ = warningLogged               // line 250 — copies the struct, triggers noCopy panic
```

This blocks the entire CI pipeline. No subsequent jobs (test-unit-go, test-unit-elixir, etc.) run.

## Context

The `TestOIDCDirectoryService_ResponseSizeLimit` test (line 243) creates an `atomic.Bool` as a
placeholder for tracking whether a truncation warning was logged. However, the test never
actually sets or checks this variable — it only inspects `buf.String()` for log output.

The `_ = warningLogged` line was added to prevent the Go compiler from complaining about an
unused variable, but it inadvertently copies the `atomic.Bool` struct value, which contains
a `noCopy` marker that `go vet` detects.

## Implementation

1. Remove `var warningLogged atomic.Bool` (line 246)
2. Remove `_ = warningLogged // used via buf check below` (line 250)

These are both dead code — the variable is never read or written beyond its declaration.

## Acceptance Tests

### Tests written FIRST (before implementation code):

1. **lint-go passes** — [CI / ci-local.sh]
    - Given: the fix is applied (warningLogged variable and assignment removed)
    - When: `go vet ./...` runs in the gateway directory
    - Then: exit code 0, zero findings

2. **full CI pipeline passes** — [CI / ci-local.sh --all]
    - Given: the fix is applied
    - When: `scripts/ci-local.sh --all` runs
    - Then: all jobs (lint-go, test-unit-go, test-unit-elixir, secret-scan, verify-docs) pass
