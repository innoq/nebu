# ADR-010: Full-Text Search Strategy

## Status

Accepted — 2026-05-08

## Context

The Matrix Client-Server API defines `POST /_matrix/client/v3/search` for full-text message
search. Nebu does not yet implement this endpoint because the search strategy has not been decided.

Two options were under consideration:

1. **PostgreSQL `tsvector` / `tsquery`:** Native PostgreSQL full-text search using `tsvector`
   columns and GIN indexes. Low operational complexity (no additional service), multilingual
   support via text search configurations. Limited to keyword matching — no semantic search.

2. **pgvector (semantic search):** PostgreSQL extension for vector similarity search.
   Requires embedding generation (external ML model or API call per message). Enables semantic
   search ("find messages about the meeting even if the word 'meeting' isn't used"). Higher
   operational complexity; privacy implications for external embedding APIs.

## Decision

**PostgreSQL native `tsvector` / `tsquery` with GIN index.**

No external dependencies, no additional services. Nebu's architecture principle (ADR-002:
no Redis, no NATS) applies equally here — the search infrastructure must not require an
embedding pipeline or vector database.

**Implementation approach:**

- Add a `search_vector tsvector` column to the `events` table (or a dedicated `message_search`
  projection table) populated via a PostgreSQL trigger on insert
- GIN index on `search_vector` for efficient queries
- Use `to_tsvector('simple', body_text)` as the default configuration; `'simple'` avoids
  language-specific stemming, which is appropriate for a multilingual chat server
- `to_tsquery` / `websearch_to_tsquery` for query parsing (the latter handles user-typed
  queries like `hello world` without requiring explicit operators)
- Results ranked via `ts_rank_cd`
- Scope enforcement: `WHERE room_id = ANY($membership_room_ids)` — no cross-room leakage

**Out of scope (Phase 2 candidate):**

- Semantic / vector search via pgvector — deferred; can be added as an opt-in extension
  if community demand materialises, without changing the MVP search schema

## Consequences

- **Story 7-31** (search API stub) is unblocked — Epic 11 can begin
- New migration: `search_vector` column + GIN index + trigger on `events` table
- `POST /_matrix/client/v3/search` will return keyword-match results, not semantic results —
  acceptable for MVP; documented in user-facing limitations
- No additional infrastructure to operate or monitor
- Elixir Core exposes a `SearchMessages` gRPC RPC; Gateway delegates to Core

_Source: `README.md`, §Current Limitations (No Full-Text Search); epics.md §Epic 11_