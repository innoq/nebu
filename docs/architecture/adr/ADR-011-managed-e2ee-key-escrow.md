# ADR-011: Managed E2EE Key Escrow

## Status

Proposed — Decision pending (see GitHub Issue tracker)

## Context

Nebu currently does not implement end-to-end encryption (E2EE). This is an intentional design
decision for the MVP: Nebu targets compliance-first organizations that require server-side message
visibility for audit logging, four-eyes compliance access, and legal export.

True E2EE (where only the communicating parties hold decryption keys) is incompatible with
server-side compliance access. However, a "Managed E2EE" or "Key Escrow" model may provide
a middle ground:
- Client encrypts messages
- Server holds escrow keys (encrypted with the organization's compliance key)
- Compliance access is still possible via escrow key + four-eyes approval

This model would preserve the compliance-first value proposition while offering E2EE for users
who want protection against server-operator data access.

Currently:
- `POST /_matrix/client/v3/keys/upload` and `keys/query` return stubs
- Element Web's cross-signing dialog is silenced via UIA dummy flow
- No actual key material is stored

This ADR must be resolved before implementing any E2EE functionality.

## Decision

Decision pending — no E2EE implementation until this ADR reaches Accepted status.

E2EE stubs (`keys/upload`, `keys/query`, `keys/device_signing/upload`, `room_keys/*`) remain
in place to prevent client error dialogs.

## Consequences

When accepted, consequences will be documented here based on the chosen approach.

_Source: `README.md`, §Current Limitations (No End-to-End Encryption); `memory/project_e2ee_direction.md` (Server-side decryption model — Managed E2EE); Story 9-1 Dev Notes_
