# Security Review — Story 12-3

**Story:** Media Upload — MinIO Backend + IAM Hardening
**Diff scope:** MinIO backend wiring (`selectStorer`, `readSecretFile`), IAM policy JSON, Docker Secrets integration, `createbuckets` entrypoint extension, `media` service in `docker-compose.yml`, media `Dockerfile`, Makefile updates.
**Date:** 2026-05-12
**Reviewer:** Kassandra (nebu-agent-kassandra)
**Verdict:** APPROVED

---

## Findings

| # | Severity | Location | Vulnerability | Remediation |
|---|----------|----------|---------------|-------------|
| 1 | LOW | `docker-compose.yml` createbuckets entrypoint | Shell-interpolated secrets via `$$(cat /run/secrets/...)` — injection possible if secret contains metacharacters | Not exploitable: `openssl rand -hex 16` produces `[0-9a-f]` only; no metacharacters possible. Accept. |
| 2 | LOW | `media/cmd/media/main.go` `readSecretFile` | File path read from env var (`NEBU_MINIO_ACCESS_KEY_FILE`) — path traversal possible | Path set in `docker-compose.yml` by operator, never from user input. Accept for MVP. |
| 3 | LOW | `docker-compose.yml` media service | No TLS on MinIO endpoint (`minioUseSSL: false`) — credentials transmitted in plaintext | Internal Docker network only, not exposed externally. Acceptable for dev/MVP. Enable TLS for production deployment. |
| 4 | LOW | `media/Dockerfile` | `alpine:3.19` base not pinned to SHA digest | Best-practice hardening. Advisory: pin to digest in production images. |
| 5 | LOW | `docker-compose.yml` media service | Static MinIO credentials, no rotation mechanism | MVP limitation. Credential rotation requires service restart. Acceptable. |

---

## Detail

**Finding #1 — Shell injection in createbuckets entrypoint** [LOW]

The createbuckets entrypoint interpolates secrets via `$$(cat /run/secrets/minio_app_access_key)`. If the secret file contained shell metacharacters (e.g. spaces, semicolons), this could be command injection. However, `make setup` generates these values with `openssl rand -hex 16` which produces exactly 32 hexadecimal characters (`[0-9a-f]`). Shell injection is not possible. Accept.

**Finding #2 — readSecretFile path from env var** [LOW]

`readSecretFile` takes a path from `NEBU_MINIO_ACCESS_KEY_FILE` / `NEBU_MINIO_SECRET_KEY_FILE`. If these env vars were user-controlled, an attacker could read arbitrary files. However, these env vars are set in `docker-compose.yml` by the operator and never populated from request data. The path is operator-controlled. Accept for MVP.

**Finding #3 — No TLS on MinIO endpoint** [LOW]

`NEBU_MINIO_USE_SSL: "false"` in docker-compose.yml means MinIO credentials are transmitted in plaintext over the Docker internal network. This network is not accessible externally. For production deployments, TLS should be enabled (`NEBU_MINIO_USE_SSL: "true"`) and MinIO should be provisioned with a valid certificate. Accept for dev.

**Finding #4 — Alpine base image not SHA-pinned** [LOW]

`media/Dockerfile` uses `alpine:3.19` without a digest pin. A rogue registry or MitM could substitute a different image with the same tag. For production: pin to `alpine:3.19@sha256:<digest>`. Advisory.

**Finding #5 — Static credentials, no rotation** [LOW]

The MinIO `nebu-app` credentials are generated once by `make setup` and used for the service lifetime. No rotation mechanism exists. For production: implement credential rotation via Vault or similar. Accept for MVP (restart to rotate).

---

## IAM Hardening Validation

The `dev/minio/nebu-app-policy.json` correctly restricts the `nebu-app` user to:
- `s3:PutObject` — required for media uploads
- `s3:GetObject` — required for media downloads
- Resource: `arn:aws:s3:::nebu-media/*` — scoped to the media bucket, object-level only

Absent (intentionally):
- `s3:DeleteObject` — soft-delete in PostgreSQL, not hard-delete
- `s3:ListBucket` — enumeration prevention
- `s3:*` — no admin operations from app user

The IAM implementation satisfies the principle of least privilege. No CRITICAL or HIGH findings.

---

## Summary

**CRITICAL: 0**
**HIGH: 0**
**MEDIUM: 0**
**LOW: 5** (advisory — all dev/MVP accepted risks)

**Verdict: APPROVED** — Story 12-3 may proceed to commit.
