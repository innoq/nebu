---
name: dependency-scan
description: Dependency vulnerability scanning via optional external tools (govulncheck, mix deps.audit).
---

# Dependency Scan

Kassandra does not ship a CVE database. She drives external tools when they exist and reports gracefully when they don't. The pipeline must never fail because a scanner happens to be absent.

Run this only when the diff includes dependency-manifest changes — `go.sum`, `go.mod`, `mix.lock`, `mix.exs`, or `rebar.lock`.

## Go

If `go.sum` or `go.mod` is in the diff:

```bash
govulncheck ./...
```

Handling:
- **Tool absent** (`command not found`) → INFO in the report: "govulncheck not installed — dependency CVE scan skipped. Recommend installing in CI."
- **Tool present, clean** → INFO: "govulncheck: clean."
- **Tool present, findings** → parse the output:
  - Each vulnerable package that is reachable in the runtime code path → **HIGH**
  - Each vulnerable package that is only reachable via test code → **MEDIUM**
  - Each advisory without reachability data → **MEDIUM** (state the uncertainty)
  - Include package name, version, advisory ID (GHSA / CVE) and `govulncheck`'s "Trace" field for reachability

## Elixir

If `mix.lock` or `mix.exs` is in the diff:

```bash
mix deps.audit
```

If `deps_audit` is not configured, try `mix hex.audit` as a fallback.

Handling: same pattern as Go — absent tool is INFO, clean is INFO, findings are HIGH / MEDIUM by reachability.

## Erlang

If `rebar.lock` is in the diff, there is no first-party auditor. Fall back to:

```bash
rebar3 deps | grep -i vuln
```

This is a best-effort — if the output is not structured, report as INFO with a recommendation to add a proper auditor in CI.

## Update hygiene (no scan required)

Independently of CVE data, inspect the diff and flag:

- **Downgrade** (higher version → lower version) — INFO, note the reason if visible in the commit message
- **Major-version jump spanning more than one major** — INFO, recommend breaking-change review
- **New direct dependency pulled from a non-standard source** (Git URL instead of the Go module proxy / Hex / hex.pm) — **MEDIUM**
- **Transitive dependency pulled from an unexpected module path** — note in INFO if suspicious, do not flag blindly

## What not to do

- Do **not** fail the review because the scanner is absent. Dependency scanning is assistive.
- Do **not** classify every outdated dependency as a finding. Only report when a CVE or advisory is actually returned.
- Do **not** build a homegrown CVE matcher from training-data knowledge. If no tool returned a finding, the report says "scan not available" — not "I think package X v1.2 has a known issue".
- Do **not** block on dependency-only diffs unless a scanner returns a CRITICAL reachability finding.