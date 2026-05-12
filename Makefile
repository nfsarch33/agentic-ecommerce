.PHONY: test build vet coverage coverage-check integration-pg lint docker-build docker-push docker-image-size compose-up compose-down compose-logs compose-config-prod dev dev-down dev-logs migrate-up migrate-down seed tenant-isolation-seed tenant-isolation-smoke tenant-isolation-test qa-v190-infra media-store-seed media-store-clean media-seed media-clean compose-media-config compose-config compose-wc-config compose-workers-config temporal-up temporal-down temporal-status compose-temporal-config compose-agent-schedules-config agent-schedules-list agent-schedules-smoke n8n-up n8n-down n8n-config n8n-workflows-validate monitoring-validate redis-ping redis-cli wc-up wc-down wc-logs sync-once sync-run agent-worker agent-run-once temporal-worker release-perf-smoke contract-test load-test db-perf-audit govulncheck-scan gitleaks-scan trivy-fs-scan security-refresh sentrux-gate shell-leak qa-v180 tf-fmt tf-fmt-check tf-validate tf-plan-contract uiauto-smoke uiauto-compare compose-uiauto-config uiauto-down uiauto-up

COMPOSE_FILE := docker-compose.dev.yml
COMPOSE_PROD_FILE := docker-compose.yml
DB_URL       ?= postgres://postgres:postgres@127.0.0.1:5432/ecommerce?sslmode=disable
MEDIA_DIR    ?= .local/media-uploads
COMPOSE      := docker compose -f $(COMPOSE_FILE)
PROD_COMPOSE := docker compose -f $(COMPOSE_PROD_FILE)
WC_PROFILES  := --profile woocommerce --profile tools
SYNC_PROFILE := --profile sync
WORKERS_PROFILE := --profile workers
TEMPORAL_PROFILE := --profile temporal
TEMPORAL_WORKER_PROFILE := --profile temporal-worker
MEDIA_PROFILE := --profile media-objectstore
N8N_PROFILE := --profile n8n
N8N_WORKFLOWS_DIR := deploy/n8n/workflows
IMAGE        ?= ghcr.io/nfsarch33/agentic-ecommerce
TAG          ?= dev
K6_SCRIPT    ?= tests/load/k6/backend-comprehensive.js
TF_DIR       := deploy/terraform
TF_VALIDATE_DIRS := \
	$(TF_DIR)/modules/network \
	$(TF_DIR)/modules/objectstore \
	$(TF_DIR)/modules/postgres \
	$(TF_DIR)/modules/redis \
	$(TF_DIR)/modules/service \
	$(TF_DIR)/modules/container_cluster \
	$(TF_DIR)/modules/tenant_provisioning \
	$(TF_DIR)/aws-ecs \
	$(TF_DIR)/gcp-cloudrun
TF_PLAN_DIRS := \
	$(TF_DIR)/aws-ecs \
	$(TF_DIR)/gcp-cloudrun

test:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -race ./...

vet:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go vet ./...

build:
	mkdir -p bin
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/mc-api ./cmd/mc-api
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/wc-sync ./cmd/wc-sync
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/content-worker ./cmd/content-worker
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/agent-worker ./cmd/agent-worker
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/temporal-worker ./cmd/temporal-worker
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/uiauto-compare ./cmd/uiauto-compare
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/ec-cli ./cmd/ec-cli
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build -o bin/evomap-rollup ./cmd/evomap-rollup

coverage:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -race -coverprofile=coverage.out ./...
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go tool cover -func=coverage.out

coverage-check: coverage
	@GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go tool cover -func=coverage.out | awk '/^total:/ { sub(/%/,"",$$3); if ($$3 + 0 < 83) { printf("coverage %.1f%% is below 83%% (target 85%% with 2-point buffer)\n", $$3); exit 1 } printf("coverage %.1f%% >= 83%% (gate)\n", $$3) }'

# integration-pg runs the testcontainers-driven postgres + pgvector
# integration tests gated by the `integration_pg` build tag. Requires
# Docker; tests self-skip when Docker is unreachable.
integration-pg:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -count=1 -tags integration_pg ./internal/adapter/postgres/... ./internal/rag/...

lint:
	golangci-lint run ./...

tf-fmt:
	@if ! command -v terraform >/dev/null 2>&1; then \
		echo "terraform not installed; install Terraform >=1.6 and run: terraform fmt -recursive $(TF_DIR)"; \
		exit 0; \
	fi
	terraform fmt -recursive $(TF_DIR)

tf-fmt-check:
	@if ! command -v terraform >/dev/null 2>&1; then \
		echo "terraform not installed; skipping terraform fmt -check"; \
		exit 0; \
	fi
	terraform fmt -check -recursive $(TF_DIR)

tf-validate:
	@if ! command -v terraform >/dev/null 2>&1; then \
		echo "terraform not installed; install Terraform >=1.6 and run terraform init -backend=false plus terraform validate in each deploy/terraform root"; \
		exit 0; \
	fi
	@set -e; for dir in $(TF_VALIDATE_DIRS); do \
		echo "==> terraform init/validate $$dir"; \
		terraform -chdir=$$dir init -backend=false -input=false >/dev/null; \
		terraform -chdir=$$dir validate; \
	done

tf-plan-contract:
	@if ! command -v terraform >/dev/null 2>&1; then \
		echo "terraform not installed; install Terraform >=1.6 and run terraform plan for credential-free roots"; \
		exit 0; \
	fi
	@set -e; for dir in $(TF_PLAN_DIRS); do \
		echo "==> terraform plan $$dir"; \
		terraform -chdir=$$dir init -backend=false -input=false >/dev/null; \
		terraform -chdir=$$dir plan -refresh=false -lock=false -input=false -no-color >/dev/null; \
	done

docker-build:
	docker build --build-arg TARGET=mc-api -t $(IMAGE):$(TAG) .
	docker build --build-arg TARGET=wc-sync -t $(IMAGE):$(TAG)-wc-sync .
	docker build --build-arg TARGET=content-worker -t $(IMAGE):$(TAG)-content-worker .
	docker build --build-arg TARGET=agent-worker -t $(IMAGE):$(TAG)-agent-worker .
	docker build --build-arg TARGET=temporal-worker -t $(IMAGE):$(TAG)-temporal-worker .

docker-image-size:
	docker build --build-arg TARGET=mc-api -t $(IMAGE):$(TAG)-size-audit .
	docker image ls $(IMAGE):$(TAG)-size-audit
	docker history --no-trunc $(IMAGE):$(TAG)-size-audit

docker-push:
	docker push $(IMAGE):$(TAG)
	docker push $(IMAGE):$(TAG)-wc-sync
	docker push $(IMAGE):$(TAG)-content-worker
	docker push $(IMAGE):$(TAG)-agent-worker
	docker push $(IMAGE):$(TAG)-temporal-worker

compose-up:
	$(PROD_COMPOSE) up -d --build

compose-down:
	$(PROD_COMPOSE) down

compose-logs:
	$(PROD_COMPOSE) logs -f

compose-config-prod:
	$(PROD_COMPOSE) config --quiet

dev:
	$(COMPOSE) up -d --build

dev-down:
	$(COMPOSE) down

dev-logs:
	$(COMPOSE) logs -f

redis-ping:
	$(COMPOSE) exec -T redis redis-cli ping

redis-cli:
	$(COMPOSE) exec redis redis-cli

wc-up:
	$(COMPOSE) $(WC_PROFILES) up -d wc-db wordpress

wc-down:
	$(COMPOSE) $(WC_PROFILES) stop wordpress wc-db
	$(COMPOSE) $(WC_PROFILES) rm -f wordpress wc-db

wc-logs:
	$(COMPOSE) $(WC_PROFILES) logs -f wordpress wc-db

sync-once:
	$(COMPOSE) $(SYNC_PROFILE) run --rm wc-sync

sync-run: sync-once

agent-worker:
	mkdir -p bin
	go build -o bin/agent-worker ./cmd/agent-worker

temporal-worker:
	mkdir -p bin
	go build -o bin/temporal-worker ./cmd/temporal-worker

agent-run-once:
	ECOMMERCE_AGENT_WORKER_ENABLED=true ECOMMERCE_AGENT_WORKER_RUN_ONCE=true ECOMMERCE_AGENT_SCHEDULES_ENABLED=true go run ./cmd/agent-worker

release-perf-smoke:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -run TestReleasePerformanceSmoke -count=1 -v ./cmd/mc-api

contract-test:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -run 'Test.*OpenAPI|Test.*Contract|TestRepresentativeGoldenJSONResponseShapes' -count=1 ./cmd/mc-api

load-test:
	@if ! command -v k6 >/dev/null 2>&1; then \
		echo "k6 not installed; install Grafana k6 and run: BASE_URL=http://127.0.0.1:8080 k6 run $(K6_SCRIPT)"; \
		exit 0; \
	fi; \
	k6 run $(K6_SCRIPT)

db-perf-audit:
	$(COMPOSE) exec -T postgres psql "$(DB_URL)" -f /dev/stdin < scripts/db_performance_audit.sql

govulncheck-scan:
	@if command -v govulncheck >/dev/null 2>&1; then \
		GOTOOLCHAIN=auto GOSUMDB=sum.golang.org govulncheck ./...; \
	else \
		GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go run golang.org/x/vuln/cmd/govulncheck@latest ./...; \
	fi

gitleaks-scan:
	@if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks detect --source=. --no-git --redact --verbose; \
	elif command -v docker >/dev/null 2>&1; then \
		docker run --rm -v "$$PWD:/repo" ghcr.io/gitleaks/gitleaks:v8.30.1 detect --source=/repo --no-git --redact --verbose; \
	else \
		echo "gitleaks and docker not installed; skipping gitleaks local scan"; \
	fi

trivy-fs-scan:
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs --scanners vuln,secret,misconfig --severity CRITICAL,HIGH --ignore-unfixed --exit-code 1 .; \
	else \
		echo "trivy not installed; skipping local Trivy fs scan"; \
	fi

security-refresh: govulncheck-scan gitleaks-scan trivy-fs-scan

sentrux-gate:
	@if command -v sentrux >/dev/null 2>&1; then \
		sentrux gate .; \
	else \
		echo "sentrux not installed; skipping local Sentrux gate"; \
	fi

shell-leak:
	runx shell-leak-scan --repo ecommerce

qa-v180: contract-test release-perf-smoke coverage-check vet monitoring-validate compose-config compose-config-prod security-refresh sentrux-gate shell-leak

migrate-up:
	@echo "==> Running migrations UP against $(DB_URL)"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0001_create_products.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0002_create_orders.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0003_create_product_media_assets.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0004_add_tenant_id.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0005_enable_pgvector_rag.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0006_tenant_settings_compliance_reporting.up.sql

migrate-down:
	@echo "==> Running migrations DOWN against $(DB_URL)"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0006_tenant_settings_compliance_reporting.down.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0005_enable_pgvector_rag.down.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0004_add_tenant_id.down.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0003_create_product_media_assets.down.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0002_create_orders.down.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0001_create_products.down.sql

seed:
	@echo "==> Seeding test data"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < seed/products.sql

tenant-isolation-seed:
	@echo "==> Seeding v1.9.0 tenant isolation fixtures"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < seed/tenant_isolation.sql

tenant-isolation-smoke: tenant-isolation-seed
	@echo "==> Running v1.9.0 tenant isolation smoke assertions"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < seed/tenant_isolation_smoke.sql

tenant-isolation-test:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -count=1 ./internal/tenant ./internal/adapter/postgres ./monitoring

qa-v190-infra: tenant-isolation-test monitoring-validate compose-config compose-config-prod shell-leak sentrux-gate

media-store-seed:
	@echo "==> Seeding local media directory at $(MEDIA_DIR)"
	mkdir -p "$(MEDIA_DIR)"
	cp -R seed/media/. "$(MEDIA_DIR)/"

media-store-clean:
	@echo "==> Removing local media directory at $(MEDIA_DIR)"
	rm -rf "$(MEDIA_DIR)"

media-seed: media-store-seed

media-clean: media-store-clean

compose-config:
	$(COMPOSE) config --quiet

compose-media-config:
	$(PROD_COMPOSE) $(MEDIA_PROFILE) config --quiet
	$(COMPOSE) $(MEDIA_PROFILE) config --quiet

compose-wc-config:
	$(COMPOSE) $(WC_PROFILES) $(SYNC_PROFILE) config --quiet

compose-workers-config:
	$(PROD_COMPOSE) $(WORKERS_PROFILE) config --quiet
	$(COMPOSE) $(WORKERS_PROFILE) config --quiet

temporal-up:
	$(COMPOSE) $(TEMPORAL_PROFILE) up -d temporal

temporal-down:
	$(COMPOSE) $(TEMPORAL_PROFILE) stop temporal
	$(COMPOSE) $(TEMPORAL_PROFILE) rm -f temporal

temporal-status:
	$(COMPOSE) $(TEMPORAL_PROFILE) ps temporal
	@echo "Temporal gRPC: 127.0.0.1:$${TEMPORAL_GRPC_HOST_PORT:-7233}"
	@echo "Temporal UI:   http://127.0.0.1:$${TEMPORAL_UI_HOST_PORT:-8233}"
	@echo "Task queue:    $${ECOMMERCE_TEMPORAL_TASK_QUEUE:-ec-workflows}"

compose-temporal-config:
	$(PROD_COMPOSE) $(TEMPORAL_PROFILE) $(TEMPORAL_WORKER_PROFILE) config --quiet
	$(COMPOSE) $(TEMPORAL_PROFILE) $(TEMPORAL_WORKER_PROFILE) config --quiet

compose-agent-schedules-config:
	$(PROD_COMPOSE) $(WORKERS_PROFILE) $(TEMPORAL_PROFILE) $(TEMPORAL_WORKER_PROFILE) config --quiet
	$(COMPOSE) $(WORKERS_PROFILE) $(TEMPORAL_PROFILE) $(TEMPORAL_WORKER_PROFILE) config --quiet

agent-schedules-list:
	$(COMPOSE) $(TEMPORAL_PROFILE) exec -T temporal temporal schedule list --address 127.0.0.1:7233 --namespace "$${ECOMMERCE_TEMPORAL_NAMESPACE:-default}" --long

agent-schedules-smoke: compose-agent-schedules-config
	$(COMPOSE) $(TEMPORAL_PROFILE) up -d --wait temporal
	$(MAKE) agent-schedules-list

n8n-up:
	$(COMPOSE) $(N8N_PROFILE) up -d n8n

n8n-down:
	$(COMPOSE) $(N8N_PROFILE) stop n8n
	$(COMPOSE) $(N8N_PROFILE) rm -f n8n

n8n-config:
	$(PROD_COMPOSE) $(N8N_PROFILE) config --quiet
	$(COMPOSE) $(N8N_PROFILE) config --quiet
	$(MAKE) n8n-workflows-validate

n8n-workflows-validate:
	python3 scripts/validate_n8n_workflows.py "$(N8N_WORKFLOWS_DIR)"

monitoring-validate:
	@if command -v promtool >/dev/null 2>&1; then \
		promtool check config monitoring/prometheus.yml; \
		promtool check rules monitoring/alerts.yml; \
	else \
		echo "promtool not installed; using Go YAML/JSON validation"; \
	fi
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test ./monitoring

# v2.1.0: uiauto-framework comparison harness. Research-mode integration; no
# CI gate. Driven by docker compose profile `uiauto` defined in
# docker-compose.dev.yml. Uses host paths from $(HOME) for the framework
# checkout and the frontend scenarios so canonical (non-worktree) checkouts
# work out of the box. Override with `make UIAUTO_FRAMEWORK_PATH=... uiauto-smoke`.
UIAUTO_FRAMEWORK_PATH ?= $(HOME)/Code/personal/uiauto-framework
UIAUTO_SCENARIOS_PATH ?= $(HOME)/Code/personal/agentic-ecommerce-web/test/uiauto/scenarios
UIAUTO_EXAMPLE_SCENARIOS_PATH ?= $(CURDIR)/test/uiauto/example
UIAUTO_HARNESS_PATH ?= $(CURDIR)/test/uiauto
UIAUTO_PROFILE := --profile uiauto
UIAUTO_REPORT_DATE ?= $(shell date -u +%Y-%m-%d)
UIAUTO_REPORT_DIR ?= reports/uiauto-comparison/$(UIAUTO_REPORT_DATE)
UIAUTO_PW_RESULTS_DIR ?= $(UIAUTO_REPORT_DIR)/playwright
UIAUTO_RESULTS_DIR ?= $(UIAUTO_REPORT_DIR)/uiauto
UIAUTO_COMPARE_MODE ?= fixtures
UIAUTO_FIXTURES_DIR ?= $(CURDIR)/test/uiauto/fixtures

export UIAUTO_FRAMEWORK_PATH
export UIAUTO_SCENARIOS_PATH
export UIAUTO_EXAMPLE_SCENARIOS_PATH
export UIAUTO_HARNESS_PATH

compose-uiauto-config:
	@test -d "$(UIAUTO_FRAMEWORK_PATH)" || { \
		echo "UIAUTO_FRAMEWORK_PATH=$(UIAUTO_FRAMEWORK_PATH) does not exist; clone nfsarch33/uiauto-framework or set UIAUTO_FRAMEWORK_PATH"; \
		exit 1; \
	}
	$(COMPOSE) $(UIAUTO_PROFILE) config --quiet

uiauto-up:
	@test -d "$(UIAUTO_FRAMEWORK_PATH)" || { \
		echo "UIAUTO_FRAMEWORK_PATH=$(UIAUTO_FRAMEWORK_PATH) does not exist"; \
		exit 1; \
	}
	$(COMPOSE) $(UIAUTO_PROFILE) up -d --wait uiauto-chrome

uiauto-down:
	$(COMPOSE) $(UIAUTO_PROFILE) down --remove-orphans

# uiauto-smoke runs the bundled example.com scenario through the chromedp
# service. Uses --status if docker is unavailable so the gate fails fast with
# a clear message. The success criterion for v2.1.0 is binary: the runner
# image builds, the chromedp endpoint is reachable, and ui-agent reports a
# zero exit code.
uiauto-smoke:
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "uiauto-smoke: docker is not installed; skipping (research-mode advisory gate)"; \
		exit 0; \
	fi
	@test -d "$(UIAUTO_FRAMEWORK_PATH)" || { \
		echo "uiauto-smoke: UIAUTO_FRAMEWORK_PATH=$(UIAUTO_FRAMEWORK_PATH) does not exist"; \
		exit 1; \
	}
	$(COMPOSE) $(UIAUTO_PROFILE) build uiauto-runner
	$(COMPOSE) $(UIAUTO_PROFILE) up -d --wait uiauto-chrome
	$(COMPOSE) $(UIAUTO_PROFILE) run --rm uiauto-runner status
	$(COMPOSE) $(UIAUTO_PROFILE) down --remove-orphans

# uiauto-compare drives the comparison generator. Default mode is `fixtures`
# which reads pre-baked test/uiauto/fixtures/{playwright,uiauto}/*.json so the
# target is hermetic; switch to `--mode=runtime` when you have live results
# from `bun run test:e2e --reporter=json` and ui-agent demo runs.
uiauto-compare: build
	mkdir -p $(UIAUTO_REPORT_DIR)
	./bin/uiauto-compare \
		--mode=$(UIAUTO_COMPARE_MODE) \
		--scenarios-dir="$(UIAUTO_SCENARIOS_PATH)" \
		--fixtures-dir="$(UIAUTO_FIXTURES_DIR)" \
		--playwright-results-dir="$(UIAUTO_PW_RESULTS_DIR)" \
		--uiauto-results-dir="$(UIAUTO_RESULTS_DIR)" \
		--output-dir="$(UIAUTO_REPORT_DIR)"
