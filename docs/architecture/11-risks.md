# 11 Risks and Technical Debt

## Current Risks

### Architecture Risks

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Elixir/OTP insufficient for middleware elimination thesis | Medium | High | Silver-tier loadtest validates before release; Redis optionally addable |
| OIDC-only excludes too many potential users | Low | Medium | Deliberate scope decision for target audience with existing IdP |
| CRDT consistency edge cases in Horde during netsplit | Low | High | Single-node MVP; cluster tested in Phase 2 before enabling libcluster |
| Cryptographic deletion not legally recognized | Low | High | Legal review pre-MVP; fallback: physical pseudonymization |

### Open Architecture Decisions

| ADR | Blocking Feature | Impact |
|---|---|---|
| ADR-010 (FTS strategy) | `POST /_matrix/client/v3/search` not implemented | Users cannot search message history |
| ADR-011 (Managed E2EE) | E2EE stubs only (keys/upload acknowledged, not stored) | No end-to-end encryption |

### Security Risks

| Finding | Status | Reference |
|---|---|---|
| Token invalidation gap — short-lived tokens not revoked until OIDC expiry | Accepted risk (MVP) | Story 7-26 |
| Compliance session revoke CSRF (fixed) | Resolved | Story 7-16b, Kassandra HIGH-1 |
| Moderation caller_id from request body (fixed) | Resolved | Story 7-32, SEC Gate 2 HIGH |

## Technical Debt

### Known Deferred Work

| Item | Deferred In | Priority |
|---|---|---|
| Real alias storage (`PUT /directory/room/{alias}` currently a stub) | Epic 7 | Medium |
| Room upgrade implementation (`POST /rooms/{id}/upgrade` returns 501) | Epic 7 | Low |
| `GET /joined_rooms` returns empty list (clients use /sync instead) | Epic 7 | Low |
| Multi-instance dashboard for hosters | Epic 7 retrospective | Growth |
| Apple/Google push notifications | PRD §Growth Features | Growth |
| S3 media backend | PRD §Growth Features | Growth |
| Matrix federation protocol | PRD §Phase 3 Vision | Vision |

### Performance Gaps

| Gap | Current State | Target |
|---|---|---|
| N+1 profile lookups in GetJoinedMembers | Accepted for MVP | Batch query (Phase 2) |
| load_factor always returns 1.0 | MVP placeholder | Real calculation (Phase 2) |
| AIMD drain strategy not yet implemented | Linear only (MVP) | Phase 2 |

### Test Coverage Gaps

| Gap | Status |
|---|---|
| Silver-tier loadtest | Planned post-MVP |
| Playwright E2E for all Admin UI pages | Coverage in progress (Epic 7–8) |
| Gherkin coverage for all Matrix endpoints | Partial (traceability matrix at epic end) |

## Monitoring Recommendations

Items that should be set up before production:
- Prometheus alerts for gRPC stream GELB/ROT status transitions
- Alert on `message_buffer` table row count exceeding threshold
- Alert on `message_dead_letter` table non-zero count
- Audit log retention cron alert if purge job fails

_Source: `_bmad-output/implementation-artifacts/sprint-status.yaml`, deferred items and security findings; `_bmad-output/planning-artifacts/prd.md`, §Risk Mitigations_
