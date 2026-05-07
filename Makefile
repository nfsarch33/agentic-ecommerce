.PHONY: test build vet coverage lint docker-build docker-push compose-up compose-down compose-logs compose-config-prod dev dev-down dev-logs migrate-up migrate-down seed rag-seed rag-search-smoke media-seed media-clean compose-config compose-wc-config compose-workers-config temporal-up temporal-down temporal-status compose-temporal-config monitoring-validate redis-ping redis-cli wc-up wc-down wc-logs sync-once sync-run agent-worker agent-run-once temporal-worker release-perf-smoke tf-fmt tf-fmt-check tf-validate

COMPOSE_FILE := docker-compose.dev.yml
COMPOSE_PROD_FILE := docker-compose.yml
DB_URL       ?= postgres://postgres:postgres@127.0.0.1:5432/ecommerce?sslmode=disable
MEDIA_DIR    ?= .local/media-uploads
RAG_LIMIT    ?= 3
COMPOSE      := docker compose -f $(COMPOSE_FILE)
PROD_COMPOSE := docker compose -f $(COMPOSE_PROD_FILE)
WC_PROFILES  := --profile woocommerce --profile tools
SYNC_PROFILE := --profile sync
WORKERS_PROFILE := --profile workers
TEMPORAL_PROFILE := --profile temporal
TEMPORAL_WORKER_PROFILE := --profile temporal-worker
IMAGE        ?= ghcr.io/nfsarch33/agentic-ecommerce
TAG          ?= dev
TF_DIR       := deploy/terraform
TF_VALIDATE_DIRS := \
	$(TF_DIR)/modules/network \
	$(TF_DIR)/modules/postgres \
	$(TF_DIR)/modules/redis \
	$(TF_DIR)/modules/service \
	$(TF_DIR)/aws-ecs \
	$(TF_DIR)/gcp-cloudrun

test:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/mc-api ./cmd/mc-api
	go build -o bin/wc-sync ./cmd/wc-sync
	go build -o bin/content-worker ./cmd/content-worker
	go build -o bin/agent-worker ./cmd/agent-worker
	go build -o bin/temporal-worker ./cmd/temporal-worker

coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

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

docker-build:
	docker build --build-arg TARGET=mc-api -t $(IMAGE):$(TAG) .
	docker build --build-arg TARGET=wc-sync -t $(IMAGE):$(TAG)-wc-sync .
	docker build --build-arg TARGET=content-worker -t $(IMAGE):$(TAG)-content-worker .
	docker build --build-arg TARGET=agent-worker -t $(IMAGE):$(TAG)-agent-worker .
	docker build --build-arg TARGET=temporal-worker -t $(IMAGE):$(TAG)-temporal-worker .

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
	ECOMMERCE_AGENT_WORKER_ENABLED=true ECOMMERCE_AGENT_WORKER_RUN_ONCE=true go run ./cmd/agent-worker

release-perf-smoke:
	GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -run TestReleasePerformanceSmoke -count=1 -v ./cmd/mc-api

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

migrate-down:
	@echo "==> Running migrations DOWN against $(DB_URL)"
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

rag-seed:
	@echo "==> Applying pgvector/RAG migration and deterministic fixtures"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -v ON_ERROR_STOP=1 -f /dev/stdin < migrations/0005_enable_pgvector_rag.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -v ON_ERROR_STOP=1 -f /dev/stdin < seed/rag_documents.sql

rag-search-smoke: rag-seed
	@echo "==> Running deterministic pgvector cosine search smoke"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -v ON_ERROR_STOP=1 -v rag_limit="$(RAG_LIMIT)" -f /dev/stdin < seed/rag_search_smoke.sql

media-seed:
	@echo "==> Seeding local media directory at $(MEDIA_DIR)"
	mkdir -p "$(MEDIA_DIR)"
	cp -R seed/media/. "$(MEDIA_DIR)/"

media-clean:
	@echo "==> Removing local media directory at $(MEDIA_DIR)"
	rm -rf "$(MEDIA_DIR)"

compose-config:
	$(COMPOSE) config --quiet

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

monitoring-validate:
	@if command -v promtool >/dev/null 2>&1; then \
		promtool check config monitoring/prometheus.yml; \
		promtool check rules monitoring/alerts.yml; \
	else \
		echo "promtool not installed; using Go YAML/JSON validation"; \
	fi
	go test ./monitoring
