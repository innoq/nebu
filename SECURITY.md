# Security Policy

Nebu takes security seriously. This document describes how to report
vulnerabilities responsibly and what reporters can expect in return.

## Supported Versions

Nebu has not yet reached a 1.0 release. Security updates are applied
exclusively to the `main` branch. There are no versioned release
artifacts at this time.

| Version / Branch          | Supported                  |
|---------------------------|----------------------------|
| `main` (pre-1.0)          | Yes                        |
| Any tagged release < 1.0  | No — use `main`            |

Once Nebu reaches 1.0, this table will be updated to reflect an
N and N-1 release support policy.

## Reporting a Vulnerability

**Do not report security vulnerabilities via public GitHub Issues**,
pull requests, or discussion threads. Public disclosure before a fix
is available puts all Nebu users at risk.

### Preferred channel: GitHub Security Advisories

Use the _"Report a vulnerability"_ button on the
[Security tab](../../security/advisories/new) of this repository. Your
report will be visible only to you and the maintainers until a
coordinated disclosure date is agreed. GitHub Security Advisories
produce a structured record that can be escalated to a CVE when
appropriate.

### Alternative channel

If you prefer encrypted communication, you may contact the maintainer
at the email address listed in the repository's `CODEOWNERS` file or
visible in the Git log. PGP key details will be added here if
requested.

### What to include

- A description of the issue and its potential impact
- Steps to reproduce, or a proof-of-concept
- Affected components (gateway, core, media, database schema, etc.)
- Affected version or commit SHA
- Your name or handle, if you would like to be credited in the fix

## Response Times

After a report is submitted via GitHub Security Advisories:

- _Acknowledgment:_ within 72 hours of receipt.
- _Initial assessment:_ communicated within 7 days (severity
  classification, whether the issue is accepted as in-scope, and a
  preliminary timeline).
- _Fix or mitigation timeline:_ communicated within 14 days of the
  initial assessment. For critical issues, a patch will be
  prioritised immediately.

These SLAs apply to issues submitted through the recommended private
channel. Reports sent via public channels may be delayed while the
maintainer moves them to the private advisory workflow.

## Disclosure Policy

Nebu follows a 90-day responsible disclosure window:

1. The maintainer receives the report privately and acknowledges it
   within 72 hours.
2. The maintainer works on a fix and keeps the reporter updated
   throughout.
3. A coordinated public disclosure date is agreed — typically after a
   fix is available, and no later than 90 days after the initial
   report.
4. If 90 days pass without a fix or an agreed extension, the reporter
   is free to disclose publicly. The maintainer will not take legal or
   community action against researchers who honour this process.

Extensions beyond 90 days may be agreed in writing (via the advisory
thread) for issues requiring complex mitigations. Extensions are not
granted unilaterally by the maintainer.

## Scope

The following vulnerability classes are in scope for this project:

- _Authentication bypass_ — any path that grants access without valid
  credentials or bypasses OIDC token validation.
- _Injection_ — SQL injection, command injection, or template
  injection affecting the Go gateway or Elixir core.
- _Cryptographic weakness_ — use of weak primitives (MD5, SHA-1,
  DES), incorrect Ed25519/X25519 usage, or broken random-number
  generation.
- _Remote code execution (RCE)_ — any path allowing arbitrary code
  execution on the server.
- _Sensitive data exposure_ — leaking user messages, access tokens,
  or private keys in logs, API responses, or error messages.
- _Privilege escalation_ — a user gaining room power levels or admin
  rights they were not granted.
- _Insecure Direct Object Reference (IDOR)_ — accessing another
  user's messages, profile, or keys without authorisation.

## Out of Scope

The following are explicitly out of scope:

- _Denial of service via legitimate workload_ — flooding the server
  with valid messages, sending large payloads within documented
  limits, or consuming CPU through normal Matrix operations.
  Rate limiting is a hardening concern, not a vulnerability class
  for this project at pre-1.0.
- _Third-party and upstream dependencies_ — vulnerabilities in Go
  modules, Elixir/Hex packages, PostgreSQL, Keycloak, or any other
  dependency. Please report those directly to the upstream project.
  We will track and apply upstream patches as part of routine
  maintenance.
- _Federation attacks_ — Nebu intentionally has no federation
  support (`/_matrix/federation/*` is not implemented). Attack
  vectors that require a federated peer are not applicable.
- _Social engineering_ — attacks targeting maintainers or
  contributors rather than the software itself.
- _Issues already documented as known limitations_ — check open
  GitHub Issues and the `docs/architecture/adr/` directory before
  reporting.

## Recognition

Nebu does not currently operate a bug bounty program. We are a small
open-source project and cannot offer financial rewards.

Researchers who report valid, in-scope vulnerabilities responsibly
will receive:

- Public credit by name (or handle, at the reporter's preference) in
  the release notes for the version that fixes the issue.
- An entry in a `SECURITY-ACKNOWLEDGMENTS.md` file that will be
  created when the first coordinated disclosure is completed.

We genuinely appreciate responsible disclosure and will treat
reporters with respect throughout the process.

## References

- [CONTRIBUTING.md](CONTRIBUTING.md) — for non-security bug reports
  and general contribution guidelines.
- [GitHub Security Advisories documentation][gh-advisories]

[gh-advisories]: https://docs.github.com/en/code-security/security-advisories
