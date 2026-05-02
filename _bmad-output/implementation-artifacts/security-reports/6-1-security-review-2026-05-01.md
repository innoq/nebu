# Security Review — Story 6.1 (OpenAPI Spec-First Setup, codegen pipeline, StrictServerInterface, live endpoint) — 2026-05-01

**Agent:** Kassandra
**Diff base:** `git diff --staged` (15 files, +1783 / −199)
**Classification:** CLEAN
**Config:** `blocking_severity=CRITICAL` (default), `model=opus-4-7`

## Executive Summary

Story 6.1 introduces the OpenAPI 3.1 contract, an `oapi-codegen` strict-server pipeline, and a single live endpoint `GET /api/v1/openapi.yaml` that serves the embedded spec unauthenticated by design (FR51). No CRITICAL or HIGH findings. Two MEDIUM findings worth fixing before the next release: (1) the spec endpoint sits outside `SecurityHeadersMiddleware`, so MIME-sniffing/clickjacking defenses are absent on a route that returns a YAML body; (2) the `gen-api` Makefile target pulls `oapi-codegen@latest` at build time — a build-supply-chain integrity risk and a reproducibility hole. Everything else looks correct: handler is read-only, spec is `//go:embed`'d, generated stubs return 501 cleanly, no Nebu invariants are touched.

## Findings

### [MEDIUM] Spec endpoint is not wrapped by `SecurityHeadersMiddleware`

- **CWE / OWASP:** CWE-693 (Protection Mechanism Failure) / A05:2021 Security Misconfiguration
- **Datei:** `gateway/cmd/gateway/main.go:1103` (route registration) and `:1113–1119` (middleware fork)
- **Beschreibung:** The new route `mux.HandleFunc("GET /api/v1/openapi.yaml", apihandler.OpenAPIYAMLHandler)` is registered on the bare `mux`. The dispatcher at `main.go:1114–1120` only routes paths starting with `/admin` through `admin.SecurityHeadersMiddleware`; everything else (including `/api/v1/openapi.yaml`) bypasses it. As a result the spec response carries no `X-Content-Type-Options: nosniff`, no `Strict-Transport-Security`, no `Content-Security-Policy`, no `X-Frame-Options`, no `Referrer-Policy`.
- **Impact:** A YAML body served without `nosniff` can be MIME-sniffed by older browsers; combined with a same-origin XSS sink elsewhere this becomes a content-confusion vector. Without HSTS the spec endpoint can be downgraded to HTTP at the perimeter. Without CSP/Frame-Options the page can be framed for phishing. None of these is independently exploitable today, but the gap is exactly the kind of defense-in-depth regression Story 5.14 was meant to close.
- **Empfehlung:** Either widen the dispatcher in `main.go` so `/api/v1/*` is also passed through `SecurityHeadersMiddleware`, or wrap `OpenAPIYAMLHandler` directly at registration time (`mux.Handle("GET /api/v1/openapi.yaml", admin.SecurityHeadersMiddleware(...)(http.HandlerFunc(apihandler.OpenAPIYAMLHandler)))`). At minimum set `X-Content-Type-Options: nosniff` inside the handler itself.
- **Referenz:** OWASP ASVS V14.4.1, NIST SP 800-53 SC-8

### [MEDIUM] `gen-api` Makefile target uses `oapi-codegen@latest` (unpinned build-time dependency)

- **CWE / OWASP:** CWE-1357 (Reliance on Insufficiently Trustworthy Component) / A08:2021 Software and Data Integrity Failures
- **Datei:** `Makefile:196`
- **Beschreibung:** The `gen-api` recipe runs `go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest …`. Because `build-gateway` now declares `gen-api` as a prerequisite (`Makefile:44`), every container build will resolve `@latest` for the codegen tool from a public module proxy. A compromised release of `oapi-codegen` would be pulled at build time and used to generate the entire `gateway/internal/api/api_gen.go` (728 LOC of dispatch code that is then linked into the gateway binary).
- **Impact:** Build-supply-chain integrity risk and reproducibility loss. Two reviewers building the same commit can produce different generated code if a new `oapi-codegen` version is published in between. A malicious release would silently inject code into the gateway binary.
- **Empfehlung:** Pin to a specific version: `oapi-codegen@v2.6.0` (the version visible in the previous generated header). Promote `oapi-codegen` to a tools dependency tracked in `gateway/go.mod` (or a `tools.go`) so its hash is in `go.sum`. Re-run `go mod download` and `go mod verify` in CI before generating.
- **Referenz:** SLSA Build Integrity L2; NIST SP 800-53 SA-12

### [LOW] Spec endpoint advertises Admin API attack surface unauthenticated (by design)

- **CWE / OWASP:** CWE-200 (Information Exposure)
- **Datei:** `gateway/internal/api/openapi_handler.go:14` and `gateway/api/openapi.yaml`
- **Beschreibung:** The endpoint exposes a complete inventory of admin paths (`/admin/users`, `/admin/rooms`, `/admin/config`, `/admin/metrics`, `/compliance/access-requests`) and the `BearerAuth` scheme to anonymous callers. This is intentional per FR51 ("API tooling must be able to fetch the spec without credentials") and matches industry practice. Recording it explicitly so future operators know the trade-off.
- **Impact:** Anonymous reconnaissance is faster — an attacker scanning Nebu instances can enumerate admin endpoints from the spec instead of probing. Auth on the actual handlers remains the real perimeter; this is reconnaissance, not bypass.
- **Empfehlung:** Keep as-is unless an operator explicitly requests gating. If gating is later desired, consider serving the full spec only to authenticated clients while keeping a stripped/public summary unauth — but treat that as a future story, not a regression.
- **Referenz:** OWASP ASVS V13.2.1

### [INFO] Generated `AdminServer` stubs are not yet wired into the HTTP mux

- **Datei:** `gateway/internal/api/server.go` and `gateway/cmd/gateway/main.go`
- **Beschreibung:** `AdminServer` implements `StrictServerInterface` and returns 501 from every method, but the `StrictHandler` is not registered on `mux`. The spec advertises `/admin/users`, `/admin/rooms`, etc., yet a real GET to those paths today does not hit `AdminServer` — it falls through to the existing browser-facing `/admin/users` HTML handler at `main.go:315`. There is no security defect here (existing handlers are auth-protected), but the spec/runtime mismatch is worth noting so Epic 6.4–6.10 do not assume the routes are already wired.
- **Empfehlung:** Document explicitly in the Story 6.4 entry-point that `api.NewStrictHandler(&AdminServer{}, …)` must be mounted on `mux` before that story closes — and that the handler chain must include auth + Power-Level checks.

### [INFO] Dependency additions — automated CVE scan unavailable locally

- **Datei:** `gateway/go.mod`, `gateway/go.sum`
- **Beschreibung:** New direct deps: `github.com/getkin/kin-openapi v0.137.0`, `github.com/oapi-codegen/runtime v1.4.0`, `golang.org/x/net v0.49.0` (promoted from indirect). Transitive additions include `mailru/easyjson`, `mohae/deepcopy`, `perimeterx/marshmallow`, `santhosh-tekuri/jsonschema/v6`, `oasdiff/yaml`, `dlclark/regexp2`, `ugorji/go/codec`, `woodsbury/decimal128`. `govulncheck` is not installed in the local environment — the dependency CVE scan was skipped. None of these packages are obviously suspicious; all are mainstream OSS.
- **Empfehlung:** Run `govulncheck ./...` in CI before merging. Recommend adding a CI job (Story 8.5 / 8.6 ecosystem) that runs `govulncheck` on every PR that changes `go.sum`.

### [INFO] Positive: handler design is minimal and side-effect-free

- **Datei:** `gateway/internal/api/openapi_handler.go`
- **Beschreibung:** `OpenAPIYAMLHandler` writes a compile-time embedded byte slice (`apispec.Spec`) to the response. No file I/O, no path resolution from request, no string interpolation, no dynamic templating, no logging of request data. The attack surface of the handler itself is essentially zero. `//go:embed openapi.yaml` in `gateway/api/spec.go` resolves at compile time so there is no runtime path-traversal vector.

### [INFO] Positive: spec uses `BearerAuth` globally with `/health` opt-out

- **Datei:** `gateway/api/openapi.yaml:1–25`
- **Beschreibung:** Global `security: [{BearerAuth: []}]` with the unauthenticated `/health` route explicitly setting `security: []` is the right inversion — opt-out for the rare unauth case rather than opt-in for the many auth cases. The `/api/v1/openapi.yaml` route itself is not in the spec, which is consistent with FR51 (it is a meta-endpoint, not a business endpoint).

## Nebu Invariants Check

| Invariant                                   | Status |
|---------------------------------------------|:------:|
| Compliance RSP coverage                     | ✅ (no DB code in diff) |
| `reason` field on compliance access         | ✅ (no compliance writes in diff) |
| Audit-log immutability                      | ✅ (no migration in diff) |
| `instance_admin` notification (if in-scope) | ✅ (not in scope) |
| OIDC token validation (`iss`/`aud`/`exp`)   | ✅ (auth path unchanged; spec endpoint is intentionally unauth per FR51) |
| Matrix Power Level checks                   | ✅ (no room state-change handlers added) |
| No hardcoded secrets                        | ✅ (verified — only an OpenAPI scheme name `BearerAuth` and embedded spec bytes) |
| TLS 1.3 enforcement                         | ✅ (no TLS config in diff) |
| AES-256-GCM correctness                     | ✅ (no crypto in diff) |
| Ed25519 verify-before-accept                | ✅ (no signature handling in diff) |
| No secrets in logs / error messages         | ✅ (handler does not log; error path is `_ = w.Write(...)`) |

No invariant is violated. ⚠️ would require a downstream verification path; nothing in this diff plausibly affects an invariant area.

## Dependency Scan

- **Go (`govulncheck`):** tool absent in local environment — dependency CVE scan skipped. Recommend running in CI before merge.
- **Elixir (`mix deps.audit`):** not applicable — no `mix.lock` / `mix.exs` change in diff.
- **Erlang (`rebar3`):** not applicable.

Dependency hygiene observations from inspecting the diff itself:

- `golang.org/x/net v0.49.0` — promoted from indirect to direct. Latest stable line. No advisory known.
- `github.com/getkin/kin-openapi v0.137.0` — current release; widely used.
- `github.com/oapi-codegen/runtime v1.4.0` — matches the codegen tooling.
- No version downgrades observed. No Git-URL pseudo-versions added.

## Overall Risk

| Severity  | Count |
|-----------|:-----:|
| CRITICAL  | 0 |
| HIGH      | 0 |
| MEDIUM    | 2 |
| LOW       | 1 |
| INFO      | 4 |

## Pipeline Decision

**CLEAN** — no CRITICAL / HIGH findings. Pipeline may proceed. Two MEDIUM findings (security-headers gap on `/api/v1/openapi.yaml`, unpinned `oapi-codegen@latest` in the build) should be tracked as follow-up work before the next release; neither is a release blocker on its own but both will become harder to fix once Epic 6 sub-stories add more routes that inherit the same gaps.

---

*Generated by Kassandra — BMAD Security Review Agent. This report is an immutable audit artifact — do not edit retrospectively; create a new review if re-analysis is required.*
