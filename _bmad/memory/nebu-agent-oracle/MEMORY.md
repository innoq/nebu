# Memory

## Nebu Implementation Scope

Spec version: Matrix CS API v1.18.

Media Gateway (Epic 12) — implemented:
- `POST /_matrix/media/v3/upload`
- `GET /_matrix/media/v3/download/{serverName}/{mediaId}` (deprecated since v1.11)
- `GET /_matrix/media/v3/thumbnail/{serverName}/{mediaId}` (deprecated since v1.11)

Media Gateway (Epic 12) — missing (post-epic open follow-ups):
- `GET /_matrix/client/v1/media/config` — CRITICAL (auth required, Element Web pre-upload check)
- `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}` — CRITICAL (auth required, freeze expected since v1.12)
- `GET /_matrix/client/v1/media/thumbnail/{serverName}/{mediaId}` — CRITICAL (auth required)
- `GET /_matrix/client/v1/media/download/{serverName}/{mediaId}/{fileName}` — MEDIUM
- `GET /_matrix/media/v3/config` — MEDIUM (deprecated but old clients call it)
- `GET /_matrix/media/v3/download/{serverName}/{mediaId}/{fileName}` — MEDIUM
- `POST /_matrix/media/v1/create` + `PUT /_matrix/media/v3/upload/{serverName}/{mediaId}` — LOW (async upload)
- `GET /_matrix/client/v1/media/preview_url` — LOW

Threading (Epic 9):
- `GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread` — required for thread panel; status unknown (check before threading epic).

## Known Spec Decisions
_Deliberate implementation choices — accepted deviations, spec ambiguity resolutions. Not re-flagged as findings._

## Spec Quirks for This Codebase

### Media — Authenticated Endpoints Freeze (v1.11/v1.12)
Per §Media Repository v1.18: since v1.11, `/_matrix/media/v3/` download/thumbnail/config are deprecated. Since v1.12, servers SHOULD "freeze" them — newly uploaded media only accessible via authenticated `/_matrix/client/v1/media/` endpoints. Element Web (current) will prefer the authenticated form. Without those endpoints, images uploaded after a hypothetical freeze will not display.

### Media — SHOULD Headers Missing from download + thumbnail handlers
`media/internal/download/handler.go` and `media/internal/thumbnail/handler.go` set `X-Content-Type-Options: nosniff` but are missing:
- `Content-Security-Policy: sandbox; default-src 'none'; script-src 'none'; plugin-types application/pdf; style-src 'unsafe-inline'; object-src 'self';` — SHOULD (§Media Repository)
- `Cross-Origin-Resource-Policy: cross-origin` — SHOULD (§Media Repository, added v1.4)

### Threading — Bundled Aggregations + /relations
First reply in thread not visible in Element Web because:
1. Bundled aggregations on thread root not sent in non-limited sync (spec: only when `limited: true`, but Element Web needs them to discover threads)
2. `GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread` endpoint (v1 prefix) required to populate thread timeline — see session 2026-05-08.

## Open Questions

- Is `GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread` implemented? (threading endpoint, v1 prefix — see session 2026-05-08)
- Are the authenticated media endpoints (`/_matrix/client/v1/media/`) planned for a follow-up epic?
