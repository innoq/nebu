# Nebu — Docker-only build system
# All build commands run inside Docker containers.
# No local Go, Elixir, or buf installation required.

DOCKER_GO     = docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
DOCKER_BUF    = docker run --rm -v $(PWD):/workspace -w /workspace bufbuild/buf

.PHONY: build-gateway build-core dev setup test-unit-go test-unit-elixir test-integration proto gen-api

## build-gateway: Build the Go Gateway Docker image (multi-stage)
build-gateway:
	docker build -t nebu-gateway:dev ./gateway

## build-core: Compile the Elixir/OTP Core inside container (mix compile)
build-core:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix compile"

## dev: Start the full local development stack (gateway, core, postgres, dex)
dev:
	docker compose up

## setup: First-time setup — generate .secrets/internal_secret and test keys
setup:
	@mkdir -p .secrets
	@if [ ! -f .secrets/internal_secret ]; then \
		openssl rand -hex 32 > .secrets/internal_secret; \
		echo "Generated .secrets/internal_secret"; \
	else \
		echo ".secrets/internal_secret already exists, skipping"; \
	fi
	@echo ""
	@echo "Dev credentials (Dex local users):"
	@echo "  kai@example.com        / changeme  (instance_admin)"
	@echo "  compliance@example.com / changeme  (compliance_officer)"
	@echo "  alex@example.com       / changeme  (user)"

## test-unit-go: Run Go unit tests inside container
test-unit-go:
	$(DOCKER_GO) sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -race ./..."

## test-unit-elixir: Run Elixir unit tests inside container
test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix test --warnings-as-errors"

## test-integration: Run full stack integration tests (Godog / Gherkin)
## The test runner joins the nebu_default compose network so it can reach
## gateway:8080 and core:4000 by service name — works locally and in DinD CI.
test-integration:
	docker compose up -d --wait && \
	docker run --rm -v $(PWD):/workspace -w /workspace \
		--network=nebu_default \
		-e NEBU_TEST_GATEWAY_URL=http://gateway:8080 \
		-e NEBU_TEST_CORE_URL=http://core:4000 \
		golang:1.26-alpine \
		sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v -tags integration ./test/integration/..."; \
	EXIT=$$?; docker compose down; exit $$EXIT

## proto: Generate gRPC stubs from .proto definitions (via buf + protoc)
## Step 1: buf generates Go stubs using remote plugins
## Step 2: protoc + protoc-gen-elixir generates Elixir stubs
proto:
	docker run --rm -v $(PWD):/workspace -w /workspace/proto bufbuild/buf generate
	docker run --rm -v $(PWD):/workspace -w /workspace/proto \
		elixir:1.19-alpine sh -c '\
		apk add -q --no-cache protobuf && \
		mix local.hex --force --quiet && \
		mix escript.install --force hex protobuf && \
		mkdir -p ../core/apps/event_dispatcher/lib/pb && \
		protoc --plugin=protoc-gen-elixir=/root/.mix/escripts/protoc-gen-elixir --elixir_out=../core/apps/event_dispatcher/lib/pb --proto_path=. core.proto'

## gen-api: Generate Go server stubs from openapi.yaml (oapi-codegen)
gen-api:
	$(DOCKER_GO) sh -c "go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
		-generate types,std-http-server \
		-package admin \
		-o gateway/internal/admin/api_gen.go \
		gateway/api/openapi.yaml"
