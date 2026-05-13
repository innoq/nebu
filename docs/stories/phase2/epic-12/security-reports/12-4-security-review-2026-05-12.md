## Security Review — 12.4

**Diff scope:** Storage sentinel errors (`ErrNotFound`, `ErrStorageUnavailable`), `ClassifyMinIOError`, `MinIOStorer.Get` with `obj.Stat()` eager probing, `LocalStorer.Get` alignment, download handler 404/502 error mapping.

**Date:** 2026-05-12

**Reviewer:** Kassandra (nebu-agent-kassandra)

---

### Scope Reviewed

- SQL injection: no new SQL — DB layer unchanged. ✓
- XSS/CSRF: no HTML templates, no state-changing endpoint changes. ✓
- Auth bypass: download endpoint intentionally unauthenticated (Matrix spec); no new routes added. ✓
- JWT validation: unchanged. ✓
- Timing attacks: no secret comparison added. ✓
- Plaintext secrets in logs: `slog.Error` logs classified error only (not raw MinIO SDK error). ✓
- Weak crypto: no new crypto. ✓
- Body size limits: unchanged (pre-existing limits in upload handler). ✓
- Rate limits: unchanged (pre-existing). ✓
- Path traversal: analyzed — see Finding #1. Advisory.
- Information leakage: HTTP response analyzed — see Finding #2. Observability advisory.
- MinIO error handling: `ClassifyMinIOError` uses `errors.As` correctly; 502 mapped for backend unavailability. ✓

---

### Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `media/internal/storage/local.go:42-47` | Path traversal: user-controlled `mediaId` URL param could traverse filesystem if not blocked by DB gate | DB gate prevents exploitation in practice (DB only contains UUID-format keys); add `filepath.Clean` + `strings.HasPrefix` check in `LocalStorer.Get` as defense-in-depth for future API changes |
| 2 | LOW | `media/internal/storage/minio.go:ClassifyMinIOError` | Original MinIO error detail (endpoint URL, SDK error code) discarded before logging; operators cannot diagnose WHY MinIO failed from logs alone | In `MinIOStorer.Get`, log the raw error before calling `ClassifyMinIOError`: `slog.Error("minio.GetObject.Stat failed", "bucket", s.Bucket, "key", key, "err", rawErr)` — ensures operators can diagnose failures without leaking details to clients |

---

### Detail

**Finding #1 — LocalStorer path traversal (LOW)**

`LocalStorer.Get` constructs `filepath.Join(s.BasePath, subDir, name)` where `subDir = serverName` and `name = mediaID` (both user-supplied from URL). A malicious `mediaID` like `../../../etc/passwd` would resolve to a path outside `BasePath`.

**Exploitation blocked by DB gate:** The download handler calls `GetMediaFile(ctx, serverName, mediaID)` before touching storage. The DB query is parameterized (`$1`, `$2`). If `mediaID` is not in `media_files`, the handler returns 404 before reaching `LocalStorer.Get`. Since all valid `media_ids` are UUID v4 values generated server-side, a traversal path would never be found in the DB.

**Recommended defense-in-depth:** Add to `LocalStorer.Get`:
```go
cleanedKey := filepath.Clean(filepath.FromSlash(key))
if strings.Contains(cleanedKey, "..") {
    return nil, fmt.Errorf("%w: invalid key", ErrNotFound)
}
```
This is advisory for MVP; should be added before Phase 3 API expansion.

**Finding #2 — MinIO error observability gap (LOW)**

`ClassifyMinIOError` discards the original SDK error and returns a generic classified error. When `slog.Error` logs `err` in `handler.go`, only the classified message appears (`"storage: storage backend unavailable: minio request failed"`), not the original SDK detail (HTTP status, MinIO response code, timing).

This is NOT a security vulnerability — it prevents log injection of credential strings. But it reduces operational visibility.

**Recommended fix:** In `MinIOStorer.Get`, log the raw error at debug/error level with the bucket and key context before classifying:
```go
if _, err := obj.Stat(); err != nil {
    _ = obj.Close()
    slog.Debug("minio.Object.Stat failed", "bucket", s.Bucket, "key", key, "err", err)
    return nil, ClassifyMinIOError(err)
}
```
Note: Bucket name and object key are not sensitive. MinIO error codes (e.g., `NoSuchKey`, `AccessDenied`) are useful for operator diagnosis and safe to log.

---

### Summary

CRITICAL: 0
HIGH: 0
MEDIUM: 0
LOW: 2 (both advisory — not exploitable at MVP scale)

**Verdict: APPROVED**

Both LOW findings are defense-in-depth advisories. The path traversal (Finding #1) is blocked by the DB gate in all current code paths. The observability gap (Finding #2) is operational and should be addressed in a future story.

_Kassandra signing off — 2026-05-12_
