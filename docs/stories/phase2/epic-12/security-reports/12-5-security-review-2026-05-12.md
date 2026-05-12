# Security Review — Story 12.5: Thumbnail Generation On-Demand, Sandboxed

**Date:** 2026-05-12
**Reviewer:** Kassandra (security review agent)
**Classification:** CLEAN

## Summary

0 CRITICAL, 0 HIGH, 0 MEDIUM, 2 LOW (advisory)

## Scope

Staged diff for Story 12.5:
- `media/internal/thumbnail/handler.go`
- `media/internal/thumbnail/thumbnail.go`
- `media/cmd/media/main.go` (pgThumbnailStore adapter + handler wiring)
- `media/go.mod` (disintegration/imaging v1.6.2 added)

## Findings

### LOW-1 — serverName path validation not enforced in thumbnail handler

**File:** `media/internal/thumbnail/handler.go`
**Risk:** Pre-existing gap (same in download handler, not introduced by this story)

`serverName` is extracted from the URL path and used directly in `storageKey := serverName + "/" + mediaID`. The `mediaIDPattern` regex validates `mediaID` but not `serverName`. For MinIO, storage keys are arbitrary strings so traversal risk is low. For LocalStorer, `serverName` with path separators could theoretically construct unintended paths.

**Disposition:** Pre-existing issue, not introduced by this story. Same pattern as download handler. Bounded by the fact that serverName typically comes from the server's own configured value. Deferred.

### LOW-2 — Large animated GIF memory pressure

**File:** `media/internal/thumbnail/thumbnail.go:generateAnimatedGIFThumbnail`
**Risk:** An animated GIF with many frames, each resized individually, could cause memory pressure during thumbnail generation.

**Disposition:** Bounded by the upload handler's `MaxBytes` limit enforced at upload time. All stored objects have already passed the size gate. No frame count limit needed at MVP. Advisory only.

## Security Checklist

| Check | Status |
|---|---|
| SQL injection | ✓ Parameterized queries via pgx |
| Path traversal (mediaId) | ✓ Regex `^[A-Za-z0-9_\-]+$` enforced |
| Path traversal (serverName) | ⚠ Pre-existing gap (LOW-1) |
| XSS | ✓ JSON errors, image responses, %q filename quoting |
| CSRF | ✓ GET only, not state-changing |
| Auth bypass | ✓ Unauthenticated per Matrix spec (correct) |
| Timing attacks | ✓ No secret comparisons |
| Body size limits | ✓ GET request, no body |
| Weak crypto | ✓ AES-256-GCM via existing mediacrypto package |
| Plaintext secrets in logs | ✓ Only public identifiers logged |
| Security headers | ✓ Content-Type, Content-Disposition, Cache-Control set |
| Image processing sandbox | ✓ Pure Go, no cgo, no shell exec (disintegration/imaging) |
| JWT/auth validation | ✓ Not applicable (unauthenticated endpoint) |
| DoS via large media | ⚠ Bounded by upload MaxBytes (LOW-2) |
