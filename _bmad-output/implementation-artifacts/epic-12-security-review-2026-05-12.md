# Epic 12 Security Review — 2026-05-12

## Scope

- Epic: 12 — Media Gateway Phase 2 (MinIO backend, IAM, thumbnails)
- Base: `5020ec8`
- HEAD: `1c13dd7` (branch `feature/epic-12-media`)
- Reviewer: Kassandra (adversarial security)
- Stories covered:
  - 12.1 — MinIO Docker Compose + Docker Secrets
  - 12.2 — Storage Interface Refactor (Storer / LocalStorer / MinIOStorer)
  - 12.3 — MinIO Backend Wiring + IAM hardening
  - 12.4 — Media Download error classification (404/502 semantics)
  - 12.5 — On-demand thumbnail generation (disintegration/imaging)
  - 12.6 — Blurhash pass-through + animated thumbnail correctness
- Additionally in diff range (Epic 11 leftovers and infra):
  - Story 11.7 — SSO Safari re-login (nonce, denylist, Cache-Control)
  - Story 11.8 — GetEvent + GetRelations fixes
  - Story 11.9 — `GET /info` build metadata
  - Story 11.10 — OIDC claim mapping (migrations 000044, 000045)
  - Story 11.11 — Receipt restart fix

## Method

- `git diff 5020ec8..HEAD` reviewed against the full security scope from `references/security-review.md`.
- Special focus per task brief: MinIO credential exposure paths, pre-signed URL misuse, thumbnail DoS / path traversal, IAM policy gaps, Docker Secrets handling.
- MEMORY.md scanned for prior accepted risks (none applicable to this epic).

## Findings

| # | Severity | Story | Location | Vulnerability | Status |
|---|----------|-------|----------|---------------|--------|
| 1 | HIGH | 12.5 / cross-story | `media/internal/thumbnail/handler.go:96–118`, `media/internal/thumbnail/thumbnail.go:82–102` | Unauthenticated thumbnail endpoint with no upper bound on `width`/`height` → memory-amplification DoS | New |
| 2 | HIGH | 12.3 (newly exposed) / pre-existing in story 4-19 | `media/internal/upload/upload.go:94–103`; `docker-compose.yml:193–217` | Media upload accepts **any** Bearer token without JWT validation; epic 12 deployed the service on port 8009 → unauthenticated 50 MiB-per-request storage write + arbitrary uploader identity stored in `media_files.uploader_user_id` | New (latent before epic) |
| 3 | HIGH | 12.4 / pre-existing in story 4-19 | `media/internal/download/handler.go:127–129`, `media/internal/upload/upload.go:123–125,167` | Stored XSS / drive-by content rendering: upload accepts arbitrary `Content-Type`; download serves it back with `Content-Disposition: inline` and no `X-Content-Type-Options`, no CSP — browsers render attacker HTML/SVG in the Nebu origin | New (latent before epic) |
| 4 | MEDIUM | 12.3 | `docker-compose.yml:180–188` (`createbuckets` entrypoint) | MinIO root + nebu-app access/secret keys passed via `$(cat /run/secrets/...)` as `mc` **command-line arguments** → recoverable via `/proc/<pid>/cmdline` from any process in the same UID/PID namespace; defeats the Docker Secrets isolation goal | New |
| 5 | MEDIUM | 11.10 | `gateway/migrations/000045_server_config_update_policy.up.sql` | New blanket `CREATE POLICY config_update_all ON server_config FOR UPDATE USING (true) WITH CHECK (true)` reverses the ADR G8 immutability guarantee for **all** keys (including `server_name`) without key-prefix scoping | New |
| 6 | MEDIUM | 12.1 | `docker-compose.yml:151` | MinIO image pinned to `RELEASE.2024-01-18T22-51-28Z` (~16 months old); pre-dates several published MinIO CVEs and STS hardening | New |
| 7 | LOW | 11.7 | `gateway/internal/matrix/sso.go:411–413` | Nonce mismatch comparison uses `!=` (variable-time) instead of `crypto/subtle.ConstantTimeCompare`; nonce is 16-byte hex (server-generated) — minimal practical leak, but inconsistent with the project's `subtle` usage elsewhere | New |
| 8 | LOW | 11.7 | `gateway/internal/matrix/sso.go:413` | `entry.nonce[:min(8, len(entry.nonce))]` log line writes the first 8 chars of the server-side nonce to `slog.Error` on mismatch — the nonce is a one-time secret bound to a single in-flight flow, but logging any prefix of a fresh secret is poor hygiene | New |
| 9 | LOW | 12.3 | `media/cmd/media/main.go:124–144` | `NEBU_MINIO_ACCESS_KEY` / `NEBU_MINIO_SECRET_KEY` env vars take precedence over `_FILE` form. Inline env-var credentials defeat the Docker Secrets pattern and risk accidental commit in `.env` files. Documentation only specifies the `_FILE` form. | New |
| 10 | LOW | 12.3 | `media/cmd/media/main.go:211` | `http.ListenAndServe` is used directly — no `ReadHeaderTimeout`, no `ReadTimeout`, no `WriteTimeout`, no `IdleTimeout`. Slowloris vector against the media gateway on its newly exposed `:8009` port. | New |
| 11 | LOW | 12.5 | `media/internal/thumbnail/handler.go:177` (also `download/handler.go:113`) | `io.ReadAll` materialises the full ciphertext (up to 50 MiB) per request; with no rate limit on the gateway and the endpoint unauthenticated, an attacker can amplify ~5 GiB memory pressure with 100 concurrent requests. Hardening only — depends on Finding #1 for impact. | New |

---

## Detail

### Finding #1 — Thumbnail DoS via unbounded `width`/`height` [HIGH]

**Location:** `media/internal/thumbnail/handler.go` lines 96–118 and `media/internal/thumbnail/thumbnail.go` lines 82–102.

**What the code does**

`ServeHTTP` validates that `width > 0` and `height > 0` but enforces no upper bound:

```go
width, err := strconv.Atoi(widthStr)
if err != nil || width <= 0 { ... }
height, err := strconv.Atoi(heightStr)
if err != nil || height <= 0 { ... }
```

These parameters are then passed verbatim into `imaging.Fit(src, width, height, imaging.Lanczos)` (scale path) or `imaging.Fill(...)` (crop path). `imaging` allocates the destination image as `*image.NRGBA`, which is `4 * width * height` bytes.

**Why it's exploitable**

- The thumbnail endpoint is **unauthenticated** (per Matrix v3 deprecated spec — preserved in this implementation).
- An attacker only needs a valid `mediaId` — any successful upload yields one. Once an attacker has uploaded a single 1 KiB JPEG (Finding #2 makes upload trivial), they can request:
  - `GET /_matrix/media/v3/thumbnail/localhost/<id>?width=20000&height=20000` → ≈1.6 GiB allocation per request.
  - Without `method=crop`, `imaging.Fit` only shrinks; oversized targets still allocate the full requested canvas before fit-clamping.
- With `animated=true` against any GIF, every frame is resized individually: `generateAnimatedGIFThumbnail` loops `g.Image` and allocates `image.NewPaletted(bounds, p)` per frame. A 1000-frame GIF (allowed by `gif.DecodeAll`) at `width=10000&height=10000` allocates ≈10 GiB.
- Combined with: no rate-limit middleware on the media gateway, no per-IP throttling, the new Docker Compose service exposed on `:8009`.

**Concrete remediation**

1. Clamp width and height to a hard maximum (e.g. 1024 per Matrix client conventions, definitely ≤ 2048):
   ```go
   const maxThumbDim = 2048
   if width > maxThumbDim || height > maxThumbDim {
       writeError(w, http.StatusBadRequest, "M_BAD_JSON", "width/height exceed maximum")
       return
   }
   ```
2. Reject decoded source images larger than a memory budget. After `imaging.Decode`, check `src.Bounds().Dx() * src.Bounds().Dy()` against e.g. 100 MP and refuse. (Decompression-bomb defence — orthogonal to the requested thumbnail size.)
3. For animated GIFs, also cap frame count (e.g. ≤ 200 frames) before entering the resize loop.
4. Independently, add rate limiting to the media gateway (the gateway has `x/time/rate`-based middleware already; reuse it).

---

### Finding #2 — Media upload has no JWT validation; epic 12 made it reachable [HIGH]

**Location:** `media/internal/upload/upload.go` lines 94–103; deployment in `docker-compose.yml` lines 193–217.

**What the code does**

```go
authHeader := r.Header.Get("Authorization")
if !strings.HasPrefix(authHeader, "Bearer ") {
    writeError(w, http.StatusUnauthorized, ...)
    return
}
// Extract sub from token — for MVP, use the raw token value as user ID.
// A real implementation would validate the JWT; here we accept any bearer.
uploaderUserID := strings.TrimPrefix(authHeader, "Bearer ")
```

**Why it's exploitable**

- The handler accepts literally any string after `Bearer ` as a valid token. There is no signature verification, no expiry check, no audience check, no denylist consultation.
- Pre-existing in code from Story 4-19, but Story 12.3 promoted the `media` service to a docker-compose-managed container exposed on host port 8009 (`ports: ["8009:8009"]`). Reachable from any host that can talk to the Compose stack.
- Each accepted request writes one encrypted object (up to 50 MiB) into the MinIO `nebu-media` bucket and inserts one row into `media_files` with the attacker's chosen `uploader_user_id`. This is an unbounded storage-write primitive.
- Attacker can also impersonate any user in the audit trail by sending `Authorization: Bearer @victim:localhost`. The DB column `uploader_user_id` is the only record of who uploaded.

**Cross-story relevance**

This is the canonical example of cumulative epic risk: the code defect was dormant in Epic 4 (no compose deployment), tolerated through 7 epics, then activated by Story 12.3 wiring the service into the live stack. The CLAUDE.md note "Upstream repo = reference implementation only; no official deployments" (ADR-012) softens the production blast radius, but the dev compose stack is reachable from any developer's `localhost:8009`.

**Concrete remediation**

1. Add a JWT verification step: reuse `gateway/internal/middleware/auth.go::JWTMiddleware` (or refactor a shared `auth.VerifyAccessToken(ctx, raw)` function) and call it from the upload handler. Reject with 401 `M_UNKNOWN_TOKEN` on any of: bad signature, expired, denylisted, wrong audience, missing `sub`.
2. The media gateway must share the OIDC provider configuration with the API gateway (same `OIDC_ISSUER`, same `client_id`). Add `NEBU_OIDC_ISSUER` to the `media` service env in compose.
3. Until #1 is shipped, remove `ports: ["8009:8009"]` from compose and route uploads through the api gateway (proxy).

---

### Finding #3 — Stored XSS via attacker-controlled Content-Type + `inline` disposition [HIGH]

**Location:** upload at `media/internal/upload/upload.go:123–125, 167`; download at `media/internal/download/handler.go:127–129`.

**What the code does**

Upload:
```go
contentType := r.Header.Get("Content-Type")
if contentType == "" { contentType = "application/octet-stream" }
// ... stored in MediaFileRow.ContentType
```

Download:
```go
w.Header().Set("Content-Type", row.ContentType)
w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", mediaID))
```

**Why it's exploitable**

- An attacker uploads a payload with `Content-Type: text/html` (or `image/svg+xml`, which most browsers render as HTML when served with that type).
- Victim opens `/_matrix/media/v3/download/<server>/<id>` (or it is embedded as an `<img>` / link by another client). The browser interprets the response as HTML and executes attacker JavaScript in the Nebu origin.
- Matrix spec v1.12+ media safety rules require the server to return `Content-Disposition: attachment` for non-safe content types (anything outside the whitelist of `image/*`, `audio/*`, `video/*`, `application/pdf`, `text/plain`, and even some of these conditionally). Nebu unconditionally uses `inline`.
- No `X-Content-Type-Options: nosniff`, no `Content-Security-Policy`, no `Cross-Origin-Resource-Policy` header is set on download responses.

**Concrete remediation**

1. Whitelist server-controlled content types on download. Build a small allowlist of safe types (`image/jpeg`, `image/png`, `image/gif`, `image/webp`, `audio/mpeg`, `audio/wav`, `video/mp4`, `video/webm`, `application/pdf`, `text/plain`). For anything else, force `Content-Type: application/octet-stream` and `Content-Disposition: attachment; filename=…`.
2. Always emit `X-Content-Type-Options: nosniff` to disable MIME sniffing in legacy browsers.
3. Emit a default-restrictive CSP for media responses (`Content-Security-Policy: default-src 'none'; sandbox`).
4. Refuse to render `text/html`, `application/xhtml+xml`, `image/svg+xml` inline regardless of `Content-Type` matching — explicitly map them to `application/octet-stream` + `attachment`.
5. Also: at **upload** time, reject `text/html`, `application/javascript`, `image/svg+xml`, and similar known-dangerous types altogether (no use case in Matrix chat).

This finding compounds with Finding #2 — an attacker who can upload anonymously can plant the XSS payload themselves.

---

### Finding #4 — MinIO credentials leaked via `/proc/<pid>/cmdline` [MEDIUM]

**Location:** `docker-compose.yml` lines 180–188 (`createbuckets` service entrypoint).

**What the code does**

```yaml
entrypoint: >
  /bin/sh -c "
  mc alias set minio http://minio:9000 $$(cat /run/secrets/minio_root_user) $$(cat /run/secrets/minio_root_password);
  ...
  mc admin user add minio $$(cat /run/secrets/minio_app_access_key) $$(cat /run/secrets/minio_app_secret_key) || true;
  ...
  "
```

**Why it's exploitable**

- `$(cat /run/secrets/...)` is shell-expanded **before** `mc` is invoked, so the access key and secret key become argv arguments of the `mc` process.
- Process argv is exposed via `/proc/<pid>/cmdline`, world-readable inside the container by default.
- The whole point of using Docker Secrets is to keep credentials off command-lines, off env vars, and out of layer history. This entrypoint defeats that.
- Risk vectors: another sidecar/extension container that runs as the same user; a future debugging script that walks `/proc`; a future container image that runs `ps -ef` for diagnostics; CI log capture of `docker compose logs createbuckets` if `mc` ever echoes args on error.
- Severity is MEDIUM (not HIGH) because the `createbuckets` container exits after init and the secrets are dev-only ephemeral (regenerated by `make setup`). But the pattern is fragile and will propagate to production deployers who copy the compose recipe.

**Concrete remediation**

Use `mc`'s env-var form, which `mc` itself documents as the supported way to pass credentials:

```yaml
entrypoint: |
  /bin/sh -c '
  export MC_HOST_minio="http://$(cat /run/secrets/minio_root_user):$(cat /run/secrets/minio_root_password)@minio:9000"
  mc mb --ignore-existing minio/nebu-media
  APP_KEY=$(cat /run/secrets/minio_app_access_key)
  APP_SECRET=$(cat /run/secrets/minio_app_secret_key)
  printf "%s\n%s\n" "$APP_KEY" "$APP_SECRET" | mc admin user add minio --stdin || true
  ...
  '
```
or pipe the credentials in via stdin (the MinIO `mc` client supports `--stdin` for `admin user add` since 2023). Either form keeps the secret out of `argv`.

---

### Finding #5 — `server_config` UPDATE policy reverses ADR G8 immutability for all keys [MEDIUM]

**Location:** `gateway/migrations/000045_server_config_update_policy.up.sql`.

**What the code does**

```sql
CREATE POLICY config_update_all ON server_config
    FOR UPDATE
    USING (true)
    WITH CHECK (true);
```

**Why it's a regression**

- Migration 000003 deliberately omits an UPDATE policy because ADR G8 declared `server_name` (and other bootstrap values) immutable after first set. The original comment on migration 000003: *"Only INSERT is allowed. No UPDATE, no DELETE policy → those operations are denied."*
- The new policy is **unconditional**: every row, every key, can be UPDATE'd by any `nebu_app` connection. This includes `server_name`, `bootstrap_completed`, `oidc_issuer`, the encrypted `oidc_client_secret`, etc.
- Not currently exploitable because the application-layer handlers (`auth.go`, `bootstrap.go`, `claim_mapping.go`) only upsert specific keys. But the defence-in-depth invariant ("the DB will refuse server_name updates even if a code bug tries them") is gone.
- This is a recurring pattern from MEMORY.md (Epic 9 had similar RLS oversights on new tables). Worth elevating to a systemic rule.

**Concrete remediation**

Replace the blanket policy with key-scoped UPDATE policies. The known mutable keys are: `oidc_user_id_claim`, `oidc_displayname_claim`, `oidc_email_claim`, `admin_group_claim` (plus any future opt-in mutable keys). The immutable keys are: `server_name`, `bootstrap_completed`, and historically `oidc_issuer`/`oidc_client_id`/`oidc_client_secret` (which are now also bootstrap-mutable via UI — confirm intent).

```sql
DROP POLICY IF EXISTS config_update_all ON server_config;
CREATE POLICY config_update_mutable ON server_config
    FOR UPDATE
    USING  (key IN ('oidc_user_id_claim','oidc_displayname_claim','oidc_email_claim','admin_group_claim','oidc_issuer','oidc_client_id','oidc_client_secret', ...))
    WITH CHECK (key IN (...same allowlist...));
```

A follow-up story should also add a CHECK constraint or partial-unique index documenting the immutable-key set.

---

### Finding #6 — MinIO image is ~16 months old [MEDIUM]

**Location:** `docker-compose.yml:151` — `image: minio/minio:RELEASE.2024-01-18T22-51-28Z`.

The pinned image pre-dates multiple MinIO releases that addressed STS issues, console XSS, and admin-API authorisation hardening. The Compose stack is *example-only* (ADR-012), but a development-stack zero-day on the Console port (`9001`) exposed on `localhost` is still a real risk for any developer running the stack. Suggest re-pinning to the latest stable RELEASE tag and adding a comment that the deployer is responsible for bumping it.

`createbuckets` (`minio/mc:RELEASE.2024-01-18T07-18-52Z`) shares this concern.

**Concrete remediation:** bump both images to the latest stable `RELEASE.2026-*` tag; document a hardening checklist in the deployer guide that includes regular image refresh.

---

### Finding #7 — Variable-time nonce comparison in SSO callback [LOW]

**Location:** `gateway/internal/matrix/sso.go:411`.

```go
if entry.nonce == "" || nonceClaims.Nonce != entry.nonce { ... }
```

The nonce is a server-generated 16-byte hex string with 10-min TTL; the attacker would have to brute-force an in-flight nonce within the validity window to gain anything from timing leaks. Practical risk is minimal. But the project already uses `crypto/subtle.ConstantTimeCompare` for `internal_secret` and JWT denylist comparisons — staying consistent reduces the chance that someone copies this pattern elsewhere with a more sensitive secret.

**Concrete remediation:** Use `subtle.ConstantTimeCompare([]byte(nonceClaims.Nonce), []byte(entry.nonce)) != 1` (after the empty-string guard).

---

### Finding #8 — Nonce-prefix logging on mismatch [LOW]

**Location:** `gateway/internal/matrix/sso.go:413`.

```go
slog.Error("matrix SSO: nonce mismatch — Dex returned a stale or cached id_token",
    "want_prefix", entry.nonce[:min(8, len(entry.nonce))], "got", nonceClaims.Nonce)
```

The expected nonce is server-generated, single-use, and tied to one in-flight SSO flow — it's not a persistent credential. Logging the first 8 hex chars on a mismatch is operationally useful and minimally sensitive. However, the `got` value comes from Dex's id_token and is also logged in full: if the attacker can read gateway logs *and* trigger a mismatch, they could correlate with their own request to extract some bits. Marginal risk.

**Concrete remediation:** Either redact `got` to a length/hash, or drop the `want_prefix` field and rely on the `want_prefix == got` predicate that returned false to be evidence enough. The audit-trail benefit is low.

---

### Finding #9 — Inline env-var credentials take precedence over `_FILE` form [LOW]

**Location:** `media/cmd/media/main.go:124–144`.

```go
minioAccessKey := getenv("NEBU_MINIO_ACCESS_KEY", "")
if minioAccessKey == "" {
    if keyFile := getenv("NEBU_MINIO_ACCESS_KEY_FILE", ""); keyFile != "" {
        minioAccessKey, err = readSecretFile(keyFile)
        ...
    }
}
```

Env-var form takes precedence over file form. Deployer who reads the docs ("use `_FILE` for Docker Secrets") might still accidentally commit `NEBU_MINIO_ACCESS_KEY=…` in an `.env` file, defeating the secret-management pattern.

**Concrete remediation:** Reverse the precedence (or warn loudly when both are set). Aligns with how the gateway's `NEBU_INTERNAL_SECRET_FILE` is treated (the gateway only reads the file).

---

### Finding #10 — Media server uses `http.ListenAndServe` without timeouts [LOW]

**Location:** `media/cmd/media/main.go:211`.

```go
if err := http.ListenAndServe(listenAddr, mux); err != nil { ... }
```

No `ReadHeaderTimeout`, no `ReadTimeout`, no `WriteTimeout`, no `IdleTimeout`. Slowloris vector: an attacker holds the TCP connection open and dribbles header bytes, exhausting goroutines / file descriptors.

**Concrete remediation:**

```go
srv := &http.Server{
    Addr:              listenAddr,
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
    ReadTimeout:       60 * time.Second,
    WriteTimeout:      120 * time.Second,
    IdleTimeout:       120 * time.Second,
    MaxHeaderBytes:    1 << 20,
}
slog.Info(...)
err := srv.ListenAndServe()
```

---

### Finding #11 — `io.ReadAll` materialises full ciphertext per request [LOW]

**Location:** `media/internal/thumbnail/handler.go:177`, `media/internal/download/handler.go:113`.

Each download/thumbnail request reads the entire encrypted body into memory before responding (`io.ReadAll(rc)`). With `NEBU_MEDIA_MAX_UPLOAD_BYTES=50MiB` default and no upstream rate limiting, 100 concurrent requests can pin ≈5 GiB of memory in the media gateway.

For AES-GCM decryption the whole ciphertext is needed in memory anyway (auth tag is at the end), so a fully streaming download is non-trivial; but the operational risk should at least be documented and bounded by an explicit `runtime.SetMemoryLimit` or per-process throttle.

**Concrete remediation:** add IP-based rate limiting (the gateway already has rate-limit middleware — bring it into the media gateway). Long-term, consider streaming decryption with chunked GCM-CBC (defer to a future story).

---

## Cross-Story Patterns

1. **"MVP" auth shortcut becomes prod risk when service is wired into the stack.** Finding #2 is the textbook case: a dormant `// for MVP, accept any bearer` lived in `media/internal/upload/upload.go` since Story 4-19, and Epic 12 promoted the service to a deployed container without revisiting the auth path. Recommend a project rule: any code with `// for MVP` / `// placeholder` in an auth/identity path **must** be re-reviewed when the calling service moves from "code-only" to "deployed-in-compose".

2. **RLS regressions on shared tables.** Finding #5 fits the recurring pattern recorded in MEMORY.md ("Missing RLS on new tables" from Epic 9). Now joined by "Permissive UPDATE policies introduced without key-scope". Add a code-review checklist item: "Any new RLS policy with `USING (true)` requires written justification."

3. **Docker Secrets pattern vs. shell entrypoint.** Finding #4 shows that introducing Docker Secrets is not enough — the entrypoint script must also avoid leaking the secrets back through `argv` or environment. Add to the deployer-hardening guide.

4. **Unauthenticated endpoints + expensive per-request work.** Findings #1 and #11 share a root cause: the media gateway is an unauthenticated-by-spec endpoint (Matrix v3 download/thumbnail) that performs unbounded server work per request. Without explicit per-route resource bounds (size/dimension caps, rate limits, memory ceilings), every such endpoint is a DoS surface.

5. **Trust-boundary docstring on gRPC handlers (carry-over from Epic 11).** Epic 11's `GetEvent` correctly takes identity from `Nebu.Grpc.Metadata.trusted_identity(stream)`; the proto-level `user_id` field is documented as "documentation/auditing only". Good. Continues the established pattern from MEMORY.md.

---

## Accepted Risks

None pre-existing in MEMORY.md for Epic 12.

---

## Follow-up Stories Required

The CRITICAL/HIGH findings below **must** become follow-up stories (or be explicitly accepted as risk with written justification and `instance_admin` sign-off) before Epic 12 is marked `done`:

1. **HIGH — Story 12-FU-1 — Clamp thumbnail dimensions + decompression-bomb defence.** Fix Finding #1. Required acceptance tests:
   - Reject `?width=10001` (or chosen ceiling +1) with 400 M_BAD_JSON.
   - Reject decoded source images exceeding configured megapixel cap.
   - Cap GIF frame count.
   - Unit test for resource-budget math.

2. **HIGH — Story 12-FU-2 — JWT validation on media upload.** Fix Finding #2. Required acceptance tests:
   - Upload with malformed Bearer → 401.
   - Upload with expired token → 401.
   - Upload with denylisted token → 401.
   - Upload with valid token → 200; `uploader_user_id` matches verified `sub`/`name` claim.

3. **HIGH — Story 12-FU-3 — Content-Type allowlist + `attachment` disposition for unsafe types.** Fix Finding #3. Required acceptance tests:
   - Upload with `Content-Type: text/html` → download returns `Content-Type: application/octet-stream`, `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`.
   - Upload with `image/svg+xml` → blocked at upload (or attachment-only on download).
   - Upload with `image/png` → download still `inline`, type preserved.
   - All download responses include `X-Content-Type-Options: nosniff`.

4. **MEDIUM — Story 12-FU-4 — Fix `createbuckets` entrypoint to keep secrets off argv.** Fix Finding #4. Acceptance test: a `ps -ef` snapshot or a `cat /proc/1/cmdline` capture inside the `createbuckets` container during init does NOT contain the literal values of `minio_root_password` or `minio_app_secret_key`.

5. **MEDIUM — Story 12-FU-5 — Scope `server_config` UPDATE policy to mutable keys.** Fix Finding #5. Acceptance test: attempt to `UPDATE server_config SET value='evil' WHERE key='server_name'` as `nebu_app` raises an RLS violation; same UPDATE on `oidc_user_id_claim` succeeds.

6. **MEDIUM — Story 12-FU-6 — Bump pinned MinIO + mc image tags.** Fix Finding #6. Acceptance test: the pinned tag is dated 2026 or later.

The LOW findings (#7–#11) may be batched into a single "media gateway hardening" follow-up story or addressed inline before epic close.

---

## Summary

CRITICAL: 0
HIGH: 3 — **block epic-done**
MEDIUM: 3 — must be addressed or accepted before epic close
LOW: 5 — advisory

Follow-up stories required: 6
Accepted risks: 0

**Epic security gate: BLOCKED** — requires follow-up stories for Findings #1, #2, #3 (HIGH) and a written decision on #4, #5, #6 (MEDIUM).

The three HIGH findings together describe an attacker walking into the media gateway, anonymously uploading attacker-chosen content with attacker-chosen identity (#2), planting an XSS payload via attacker-chosen Content-Type that renders inline (#3), and then knocking the gateway over with a single `?width=20000&height=20000` thumbnail request (#1). Epic 12 wired up these endpoints into the live compose stack — they cannot ship in this state.

The Epic-12-specific deliverables (MinIO backend, IAM policy with least privilege, on-demand thumbnails, blurhash pass-through, error classification) are well-engineered: the IAM policy correctly excludes `s3:DeleteObject`, `s3:*`, `s3:ListBucket` and scopes to `nebu-media/*`; the `Storer` interface cleanly separates concerns; `ClassifyMinIOError` uses `errors.As` to handle wrapping; the Docker Secrets wiring on the **media** service itself is correct. The findings above concern surrounding infrastructure (createbuckets entrypoint), pre-existing latent issues that the epic activated by deploying the service, and a missing decompression-bomb defence on the new thumbnail handler.
