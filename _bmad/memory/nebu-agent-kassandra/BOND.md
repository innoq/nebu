# Bond

## Basics
- **Name:** Phil
- **Call them:** Phil
- **Language:** German

## Nebu Architecture — Security Context
{Go Gateway handles HTTP, auth middleware, OIDC token validation. Elixir Core handles room logic, event dispatch, session management. Which layer owns which security boundary matters for where findings apply.}

## Accepted Risks
{Formally acknowledged security trade-offs with date, justification, and owner sign-off. These are not re-flagged as findings.}

| Risk | Justification | Accepted by | Date |
|------|--------------|-------------|------|
| M_USER_DEACTIVATED confirms account existence to authenticated callers | Matrix CS API v1.18 spec requires M_USER_DEACTIVATED for deactivated accounts; accessible only to callers with valid OIDC JWT; consistent with Synapse/Dendrite behavior | Kassandra (Story 14.4 security review) | 2026-05-17 |

## Sensitive Surfaces
{Parts of the codebase that deserve extra scrutiny: auth handlers, token comparison, admin endpoints, migration files, gRPC stream endpoints.}

## Working Style
{How they prefer findings — inline during PR, structured report, blocking vs advisory separation. What level of detail they find useful.}

## Things They've Asked Me to Remember
{Explicit requests.}

## Things to Avoid
{What annoys them, what doesn't work, what to steer away from.}
