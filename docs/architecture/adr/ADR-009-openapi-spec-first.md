# ADR-009: OpenAPI Spec-First with oapi-codegen

## Status

Accepted — 2026-03-18

## Context

The Nebu Admin API (`/api/v1/*`) needs:
- A machine-readable spec for third-party integrations and tooling
- Consistent request/response types shared between spec and implementation
- Validation of request bodies against the spec schema
- A served spec document at `GET /api/v1/openapi.yaml`

Two approaches:
1. **Code-first:** Write Go handlers, generate OpenAPI spec from code annotations. Risk: spec
   drifts from implementation; annotations add noise.
2. **Spec-first:** Write OpenAPI YAML first, generate Go types and `ServerInterface` from spec.
   Implementation must satisfy the interface. Spec is always authoritative.

`oapi-codegen` generates:
- Go structs for all request/response types
- A `ServerInterface` that the implementation must satisfy
- Request-body validation middleware

## Decision

We use **OpenAPI 3.1 spec-first** with `oapi-codegen`.

`gateway/api/openapi.yaml` is the **single source of truth** for the Admin API.

Workflow:
```
1. Edit openapi.yaml (PR review happens here)
2. make gen-api  (codegen via Docker container — no local Go toolchain needed)
3. Implement/update gateway/internal/api/*.go to satisfy the generated ServerInterface
4. openapi.yaml served via go:embed at GET /api/v1/openapi.yaml
```

Makefile:
```makefile
gen-api:
    $(DOCKER_GO) sh -c "go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
        -generate types,server \
        -package api \
        -o gateway/internal/api/api_gen.go \
        gateway/api/openapi.yaml"
```

The generated `api_gen.go` is committed to the repository (generated but tracked).

## Consequences

**Positive:**
- Spec is always authoritative — no spec/implementation drift
- API changes require spec changes first (enforced by PR review)
- Type safety: generated structs prevent `map[string]interface{}` soup
- Free validation middleware from oapi-codegen's `StrictServerInterface`
- Third-party integrations can fetch spec at runtime from `/api/v1/openapi.yaml`

**Negative:**
- `make gen-api` must be run after every spec change (CI enforces this)
- Generated code is committed; PRs show generated diffs (acceptable: diffs are meaningful)
- oapi-codegen version upgrades may require minor handler interface updates

_Source: `_bmad-output/planning-artifacts/architecture.md`, §API Spec-First Workflow (V6); `CLAUDE.md`, §ADR Table_
