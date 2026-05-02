# ADR-003: Content-Hash Event IDs (Matrix Room Version 6+)

## Status

Accepted — 2026-03-18

## Context

Matrix events require stable, unique identifiers. Multiple approaches are possible:
- **UUID v4:** Random, simple, but not tamper-evident; different values each generation; not
  deterministic for recovery.
- **Sequential database IDs:** Simple but not portable, not tamper-evident, leaks event ordering.
- **Content-Hash (Matrix Room Version 6+):** `$<base64url(SHA-256(canonical_json(event)))>` —
  deterministic, tamper-evident, reproducible, federation-compatible.

The Matrix specification defines content-hash event IDs starting with Room Version 6. This format
is now the standard for all modern Matrix homeservers.

Non-federation Nebu still benefits from content-hash IDs: if an event is modified in the database,
its stored ID will no longer match the recomputed hash — manipulation is detectable during audit
export. IDs are also reproducible after a database restore from backup, which simplifies recovery.

## Decision

Nebu uses **content-hash event IDs** in the Matrix Room Version 6+ format:
```
$<base64url(SHA-256(canonical_json(event \ {signatures, unsigned})))>
```

`Nebu.EventId.generate/1` in the `signature` Elixir app is the **sole** implementation.
No manual ID construction anywhere in the codebase.

Canonical JSON: keys sorted alphabetically, `signatures` and `unsigned` fields excluded.
Implemented in `Nebu.CanonicalJSON.canonical_json/1`.

## Consequences

**Positive:**
- Tamper-evident: stored ID ≠ recomputed hash → manipulation detected at audit export
- Deterministic: recovery from backup produces identical IDs
- Federation-ready: compatible with Room Version 6+ from day one
- Reuses the same `canonical_json/1` function as the Ed25519 signing pipeline

**Negative:**
- Event ID computation requires the full event content before storage (cannot use DB auto-increment)
- Canonical JSON serialization must be correct — bugs produce wrong IDs silently

**Enforcement:** `event_id = Nebu.EventId.generate(event)` — never `"$" <> UUID.generate()`.

_Source: `_bmad-output/planning-artifacts/architecture.md`, §Core Architectural Decisions (G7); `CLAUDE.md`, §ADR Table_
