# 3 Context and Scope

## Business Context

Nebu is a self-hosted, Matrix-compatible enterprise chat server. It replaces Slack/Teams for
organizations requiring data sovereignty. The system boundary is a single Nebu instance (one
operator deploys it for their organization). No federation means no cross-instance communication
at the protocol level.

**External actors:**

| Actor | Interface | Protocol |
|---|---|---|
| Matrix clients (Element, FluffyChat, Cinny, …) | Matrix Client-Server API | HTTPS / WSS, TLS 1.3 |
| OIDC identity provider (Keycloak, Azure AD, Dex, Google) | OIDC Authorization Code + PKCE | HTTPS |
| Operator (admin UI) | Admin web UI + Admin API | HTTPS, session cookie |
| Monitoring systems (Prometheus, Grafana) | `/metrics` endpoint | HTTP |
| Compliance officer | Admin UI + Compliance API | HTTPS, JWT + session |

## System Context Diagram

```
  ┌──────────────────┐          ┌──────────────────────────────────────────┐
  │  Matrix Clients  │◄────────►│           Nebu Instance                  │
  │  Element, Cinny  │  HTTPS   │                                          │
  │  FluffyChat, …   │  TLS 1.3 │  ┌────────────────┐  ┌────────────────┐ │
  └──────────────────┘          │  │  Go Gateway    │  │  Go Media GW   │ │
                                │  │  (stateless)   │  │  AES-256-GCM   │ │
  ┌──────────────────┐          │  └───────┬────────┘  └────────────────┘ │
  │  OIDC Provider   │◄────────►│          │ gRPC                          │
  │  Keycloak / Dex  │  OIDC    │  ┌───────▼────────────────────────────┐ │
  │  Azure AD        │          │  │  Elixir/OTP Core                   │ │
  └──────────────────┘          │  │  Horde · ETS · pg groups           │ │
                                │  └───────┬────────────────────────────┘ │
  ┌──────────────────┐          │          │ PostgreSQL protocol           │
  │  Admin / Ops     │◄────────►│  ┌───────▼────────────────────────────┐ │
  │  Browser UI      │  HTTPS   │  │  PostgreSQL 16+                    │ │
  │  Prometheus      │  HTTP    │  │  event log · audit · migrations    │ │
  └──────────────────┘          │  └────────────────────────────────────┘ │
                                └──────────────────────────────────────────┘
```

## Technical Context

**Internal communication boundaries:**

| Boundary | Protocol | Direction |
|---|---|---|
| Go Gateway → Elixir Core | gRPC (TLS in prod) | Bidirectional; streaming EventBus + unary fallback |
| Elixir Core → PostgreSQL | PostgreSQL wire protocol (TLS) | Read/Write for business logic |
| Go Gateway → PostgreSQL | PostgreSQL wire protocol (TLS) | Migrations + message_buffer + admin sessions |
| Go Media → PostgreSQL | PostgreSQL wire protocol (TLS) | Media keys storage |

**Not in scope (explicit exclusions):**
- `/_matrix/federation/*` — no Server-Server API
- `/_matrix/identity/*` — no identity server
- `/_matrix/key/*` — no key server for federation
- E2EE key distribution (keys/upload acknowledged as stubs; no real E2EE in MVP)

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Context, §Architektur-Grenzen; `README.md`, §Architecture_
