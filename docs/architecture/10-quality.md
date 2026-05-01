# 10 Quality Requirements

## Quality Tree

```
Quality
├── Performance
│   ├── NFR-P1: Message latency ≤500ms under Silver load (500 concurrent/m5.large)
│   ├── NFR-P2: /sync response ≤1s under normal load
│   ├── NFR-P3: Silver-tier (>500 concurrent), Gold (>1000), Platinum (>5000) without Redis/NATS
│   └── NFR-P4: Gateway cold-start ≤5s
├── Security
│   ├── NFR-S1: All external connections TLS 1.2+ (1.3 preferred)
│   ├── NFR-S2: Sensitive PII encrypted at rest (X25519 key)
│   ├── NFR-S3: Audit log append-only + Ed25519-signed
│   ├── NFR-S4: OIDC token validation on every API request
│   ├── NFR-S5: Ed25519 key deletion is irreversible (GDPR compliance)
│   └── NFR-S6: Bootstrap mode disables permanently after first admin setup
├── Scalability
│   ├── NFR-SC1: Go Gateway horizontally scalable without session affinity
│   ├── NFR-SC2: Elixir/OTP Core supports cluster operation (Phase 2: libcluster)
│   └── NFR-SC3: No external middleware layer required (PostgreSQL only)
├── Reliability
│   ├── NFR-R1: OTP process isolation (Room crash → only that room affected)
│   ├── NFR-R2: No data loss on Gateway restart (PostgreSQL is source of truth)
│   └── NFR-R3: Rolling updates without full downtime (stateless Gateway)
├── Operability
│   ├── NFR-O1: Full deployment via docker compose up in ≤10 minutes
│   ├── NFR-O2: Health/readiness endpoints respond ≤200ms under load
│   ├── NFR-O3: Admin UI fully embedded in Gateway binary (no external deps)
│   └── NFR-O4: All Admin UI states reproducible via URL
├── Compliance
│   ├── NFR-C1: GDPR Right-to-be-forgotten via cryptographic key deletion
│   ├── NFR-C2: Audit log retention configurable (default: 7 years)
│   └── NFR-C3: All data in operator-controlled PostgreSQL (on-premise capable)
├── Matrix Protocol Conformance
│   ├── NFR-M1: Compatible with Element, FluffyChat, Hydrogen (incompatibilities = bugs)
│   └── NFR-M2: OIDC integration via m.login.sso per Matrix OIDC Specification
├── Accessibility
│   ├── NFR-A1: Admin UI WCAG 2.1 Level AA
│   ├── NFR-A2: Admin UI fully keyboard navigable
│   └── NFR-A3: Admin UI usable with screen readers (semantic HTML, ARIA)
└── Crypto Agility
    └── NFR-CR1: Cryptographic primitives modularized (replaceable without core refactor)
```

## Performance Scenarios

| Scenario | Stimulus | Response | Measure |
|---|---|---|---|
| Silver-tier load | 500 concurrent users (60% sync, 20% send, 10% presence/typing, 5% room ops) | ≤500ms message latency | Loadtest on 2x AWS m5.large |
| Gateway restart | SIGTERM | HTTP traffic resumes | ≤5s cold start |
| Core restart | docker restart core | Events resume delivery, no cold-sync | Recovery via ETS + PostgreSQL checkpoint |
| gRPC stream lost | Network partition | message_buffer absorbs writes; drain on reconnect | 0 message loss |

## Security Scenarios

| Scenario | Concern | Response |
|---|---|---|
| Compromised token | JWT stolen | Short-lived OIDC token + deactivation endpoint invalidates all sessions |
| GDPR deletion request | User wants data removed | Delete private keys → all sensitive PII irrecoverable (audit log intact) |
| Compliance audit request | Legal request for message content | Four-eyes approval + 24h session limit + signed export |

_Source: `_bmad-output/planning-artifacts/prd.md`, §Non-Functional Requirements; `_bmad-output/planning-artifacts/architecture.md`, §Gaps & Open Questions_
