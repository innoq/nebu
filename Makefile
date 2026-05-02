# Nebu — Docker-only build system
# All build commands run inside Docker containers.
# No local Go, Elixir, or buf installation required.

DOCKER_GO     = docker run --rm -v $(PWD):/workspace -w /workspace golang:1.26-alpine
DOCKER_ELIXIR = docker run --rm -v $(PWD):/workspace -w /workspace elixir:1.19-alpine
DOCKER_BUF    = docker run --rm -v $(PWD):/workspace -w /workspace bufbuild/buf
DOCKER_NODE   = docker run --rm -v $(PWD):/workspace -w /workspace node:22-alpine

.PHONY: build-gateway build-core build-admin-css download-fonts download-vendor dev setup test-unit-go test-unit-elixir test-integration test-e2e test-matrix-compat test-load-silber build-element-e2e test-e2e-element build-fluffychat-e2e test-e2e-fluffychat proto gen-api test-compose-ports

## download-fonts: Download Inter + JetBrains Mono WOFF2 fonts (run once; commit results)
download-fonts:
	docker run --rm -v $(PWD):/workspace -w /workspace alpine:3.19 sh -c "\
		apk add -q --no-cache curl && \
		mkdir -p gateway/internal/admin/static/fonts && \
		curl -fsSL -o gateway/internal/admin/static/fonts/Inter-Regular.woff2 \
			'https://fonts.bunny.net/inter/files/inter-latin-400-normal.woff2' && \
		curl -fsSL -o gateway/internal/admin/static/fonts/Inter-Medium.woff2 \
			'https://fonts.bunny.net/inter/files/inter-latin-500-normal.woff2' && \
		curl -fsSL -o gateway/internal/admin/static/fonts/Inter-SemiBold.woff2 \
			'https://fonts.bunny.net/inter/files/inter-latin-600-normal.woff2' && \
		curl -fsSL -o gateway/internal/admin/static/fonts/JetBrainsMono-Regular.woff2 \
			'https://fonts.bunny.net/jetbrains-mono/files/jetbrains-mono-latin-400-normal.woff2'"

## download-vendor: Download vendored JS files (run once; results are gitignored — downloaded at build time)
download-vendor:
	@[ -f gateway/internal/admin/static/vendor/vue.esm-browser.prod.js ] || \
	docker run --rm -v $(PWD):/workspace -w /workspace alpine:3.19 sh -c "\
		apk add -q --no-cache curl && \
		mkdir -p gateway/internal/admin/static/vendor && \
		curl -fsSL -o gateway/internal/admin/static/vendor/vue.esm-browser.prod.js \
			'https://cdn.jsdelivr.net/npm/vue@3.5.13/dist/vue.esm-browser.prod.js'"

## build-admin-css: Compile Tailwind CSS + DaisyUI into gateway/internal/admin/static/admin.css
build-admin-css:
	$(DOCKER_NODE) sh -c "\
		cd gateway/internal/admin && \
		npm install --silent tailwindcss@3 daisyui@4 && \
		npx tailwindcss \
			--config tailwind.config.js \
			--input tailwind.input.css \
			--output static/admin.css \
			--minify"

## build-gateway: Build the Go Gateway Docker image (multi-stage)
build-gateway: gen-api build-admin-css download-vendor
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
test-unit-go: download-vendor
	$(DOCKER_GO) sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -race ./..."

## test-unit-elixir: Run Elixir unit tests inside container
test-unit-elixir:
	$(DOCKER_ELIXIR) sh -c "cd core && mix local.hex --force && mix deps.get && mix test --warnings-as-errors"

## test-integration: Run full stack integration tests (Godog / Gherkin)
## The test runner joins the nebu_default compose network so it can reach
## gateway:8080 and core:4000 by service name — works locally and in DinD CI.
test-integration: setup
	docker compose up -d --wait && \
	docker run --rm -v $(PWD):/workspace -w /workspace \
		--network=nebu_default \
		-e NEBU_TEST_GATEWAY_URL=http://gateway:8080 \
		-e NEBU_TEST_CORE_URL=http://core:4000 \
		-e NEBU_TEST_DEX_URL=http://dex:5556 \
		-e NEBU_TEST_MATRIX_URL=http://gateway:8008 \
		-e NEBU_TEST_DB_URL=postgresql://nebu_app:nebu_app_dev_pw@postgres:5432/nebu \
		-e NEBU_TEST_MIGRATION_DB_URL=postgresql://nebu_migrate:nebu_migrate_dev_pw@postgres:5432/nebu \
		-e NEBU_TEST_CORE_GRPC_ADDR=core:9000 \
		-e NEBU_TEST_INTERNAL_SECRET=$$(cat .secrets/internal_secret) \
		golang:1.26-alpine \
		sh -c "apk add -q --no-cache gcc musl-dev && cd gateway && go test -v -tags integration ./test/integration/..."; \
	EXIT=$$?; docker compose down; exit $$EXIT

## test-matrix-compat: Matrix SDK compatibility smoke test (optional CI gate — not part of test-integration)
## Validates that a real matrix-js-sdk client can connect, create a room, send a message, and
## receive it back via the room timeline. Requires the full stack to be running (docker compose up -d --wait).
test-matrix-compat:
	docker compose up -d --wait && \
	docker run --rm -v $(PWD):/workspace -w /workspace/tests/matrix_compat \
		--network=nebu_default \
		-e NEBU_MATRIX_URL=http://gateway:8008 \
		-e NEBU_DEX_URL=http://dex:5556 \
		node:22-alpine \
		sh -c "npm ci && node smoke_test.js"

## test-load-silber: Silber-Tier load test — 500 concurrent VUs via k6 (optional gate — not part of test-integration)
## Requires: running stack (docker compose up -d --wait called automatically)
## Override: NEBU_LOAD_TARGET_URL=http://my-host:8008 make test-load-silber
test-load-silber:
	docker compose up -d --wait && \
	docker run --rm -v $(PWD)/tests/load:/scripts \
		--network=nebu_default \
		-e NEBU_LOAD_TARGET_URL=$${NEBU_LOAD_TARGET_URL:-http://gateway:8008} \
		-e NEBU_DEX_URL=$${NEBU_DEX_URL:-http://dex:5556} \
		grafana/k6:0.50.0 run /scripts/k6_chat.js

## test-e2e: Run Playwright E2E tests against a running stack (make dev must be up)
## Requires: 127.0.0.1 dex in /etc/hosts for OIDC redirect flows
## Reset DB to bootstrap state first:
##   docker compose exec postgres psql -U nebu -d nebu -c \
##     "DELETE FROM server_config WHERE key IN ('bootstrap_completed','oidc_issuer','oidc_client_id','oidc_client_secret','instance_name');"
test-e2e:
	cd e2e && \
	npm install --silent && \
	npx playwright install chromium --with-deps --quiet && \
	npx playwright test tests/bootstrap*.spec.ts

## build-element-e2e: Build the Element Web E2E Docker image (uses official vectorim/element-web)
## Fast build (~5s) — no Rust/Flutter compilation required.
build-element-e2e:
	docker build -t nebu-element-e2e:dev -f docker/Dockerfile.element-e2e .

## test-e2e-element: Run Playwright E2E tests against Element Web (real Matrix client)
## Requires: 127.0.0.1 dex in /etc/hosts (for SSO redirect via Dex)
## Starts full stack + element sidecar via --profile e2e.
## Does NOT run docker compose down after tests — leaves stack for debugging.
test-e2e-element:
	docker compose --profile e2e up -d --wait && \
	cd e2e && npm install --silent && \
	npx playwright install chromium --with-deps --quiet && \
	npx playwright test tests/element_e2e.spec.ts; \
	EXIT=$$?; exit $$EXIT

## build-fluffychat-e2e: Build the FluffyChat Web E2E Docker image (Flutter multi-stage — slow first build)
## Requires: tmp/fluffychat/ to contain the FluffyChat source checkout.
build-fluffychat-e2e:
	docker build -t nebu-fluffychat-e2e:dev -f docker/Dockerfile.fluffychat-e2e .

## test-e2e-fluffychat: Run Playwright E2E tests against FluffyChat Web (real Matrix client)
## Requires: 127.0.0.1 dex in /etc/hosts (for SSO redirect via Dex)
## Starts full stack + fluffychat sidecar via --profile e2e.
## Does NOT run docker compose down after tests — leaves stack for debugging.
test-e2e-fluffychat:
	docker compose --profile e2e up -d --wait && \
	cd e2e && npm install --silent && \
	npx playwright install chromium --with-deps --quiet && \
	npx playwright test tests/fluffychat_e2e.spec.ts; \
	EXIT=$$?; exit $$EXIT

## test-compose-ports: CI smoke test — assert that port 9000 is NOT published by the core service.
## Story 5.29a AC8: gRPC port must not be bound to the host.
## Fallback for CI runners where the Go integration test cannot use Docker SDK.
test-compose-ports:
	@echo "Checking that core service does NOT publish port 9000 to the host..."
	@if docker compose config --format json 2>/dev/null | python3 -c \
		"import json,sys; cfg=json.load(sys.stdin); ports=cfg.get('services',{}).get('core',{}).get('ports',[]); \
		bound=[p for p in ports if str(p.get('target',''))==str(9000) and str(p.get('published','')) not in ('','0')]; \
		sys.exit(1) if bound else print('PASS: port 9000 not published on host')"; then \
		true; \
	else \
		echo "FAIL: core service still publishes port 9000 to the host. Remove '- \"9000:9000\"' from docker-compose.yml."; \
		exit 1; \
	fi

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

## gen-api: Generate Go server stubs from openapi.yaml (oapi-codegen, strict-server)
gen-api:
	$(DOCKER_GO) sh -c "go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
		--config gateway/api/oapi-codegen.yaml \
		gateway/api/openapi.yaml"
