# Security Policy

## Supported versions

Nebu is in early development. Only the `main` branch receives security updates at this stage. Once the project reaches a stable release, this section will be updated with a concrete support matrix.

## Reporting a vulnerability

**Please do not report security issues via public GitHub issues, discussions, or pull requests.**

If you believe you've found a security vulnerability in Nebu, report it privately to:

**security@nebu.chat**

(If that address is not yet live, contact the repository owner directly via the email listed on their GitHub profile.)

Include as much of the following as you can:

- A description of the issue and its potential impact
- Steps to reproduce, or a proof-of-concept
- Affected components (gateway, core, media, database schema, etc.)
- Affected version / commit SHA
- Your name and affiliation, if you'd like to be credited in the fix

We will acknowledge receipt within **3 business days** and aim to provide an initial assessment within **7 business days**.

## Disclosure process

1. You report the issue privately.
2. We confirm the issue and assess severity (CVSS-aligned: Low / Medium / High / Critical).
3. We develop a fix on a private branch.
4. We coordinate a disclosure date with you. For HIGH/CRITICAL issues the default embargo is 90 days from confirmation, shorter if a fix is ready sooner.
5. We release the fix and publish a security advisory crediting the reporter (unless you prefer to remain anonymous).

## Scope

In scope:

- The Go gateway (`gateway/`)
- The Go media gateway (`media/`)
- The Elixir/OTP core (`core/`)
- The gRPC protocol definitions (`proto/`)
- The database schema and migrations (`gateway/migrations/`)
- The admin UI served by the gateway
- The default Docker Compose configuration

Out of scope:

- Vulnerabilities in third-party dependencies — please report those upstream, then let us know so we can pin / patch.
- Issues in local dev tooling (e.g. the bundled Dex container) that don't affect production deployments.
- Social-engineering attacks, physical attacks, DoS via unbounded resources already known to be rate-limited.
- Best-practice findings without a concrete exploit (open an issue for those).

## Known security considerations (by design)

Nebu deliberately does **not** implement end-to-end encryption. Messages are server-readable so that compliance, legal export, and full-text search work as an enterprise messenger requires. This is documented in the architecture decision records and is not a vulnerability. Deployments requiring E2EE should look elsewhere.

## Security-focused contributions

If you'd like to contribute security hardening (additional rate limits, security headers, dependency pinning, fuzz tests, etc.), please open a regular issue or PR — these are welcome and don't need to go through the private reporting channel.

Thank you for helping make Nebu safer.
