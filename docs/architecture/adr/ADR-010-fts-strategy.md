# ADR-010: Full-Text Search Strategy

## Status

Proposed — Decision pending (see GitHub Issue tracker)

## Context

The Matrix Client-Server API defines `POST /_matrix/client/v3/search` for full-text message
search. Nebu does not yet implement this endpoint because the search strategy has not been decided.

Two primary options are under consideration:

1. **PostgreSQL `tsvector` / `tsquery`:** Native PostgreSQL full-text search using `tsvector`
   columns and GIN indexes. Low operational complexity (no additional service), multilingual
   support via text search configurations. Limited to keyword matching — no semantic search.

2. **pgvector (semantic search):** PostgreSQL extension for vector similarity search.
   Requires embedding generation (external ML model or API call per message). Enables semantic
   search ("find messages about the meeting even if the word 'meeting' isn't used"). Higher
   operational complexity; privacy implications for external embedding APIs.

The decision requires input from the community on expected search quality requirements and
willingness to operate an embedding pipeline.

## Decision

Decision pending — no implementation until this ADR reaches Accepted status.

`POST /_matrix/client/v3/search` returns `501 Not Implemented` until this ADR is resolved.

## Consequences

When accepted, consequences will be documented here based on the chosen approach.

_Source: `README.md`, §Current Limitations (No Full-Text Search); Story 9-1 Dev Notes_
