.PHONY: test build vet coverage lint docker-build docker-push compose-up compose-down compose-logs compose-config-prod dev dev-down dev-logs migrate-up migrate-down seed compose-config compose-wc-config redis-ping redis-cli wc-up wc-down wc-logs sync-once sync-run

COMPOSE_FILE := docker-compose.dev.yml
COMPOSE_PROD_FILE := docker-compose.yml
DB_URL       ?= postgres://postgres:postgres@127.0.0.1:5432/ecommerce?sslmode=disable
COMPOSE      := docker compose -f $(COMPOSE_FILE)
PROD_COMPOSE := docker compose -f $(COMPOSE_PROD_FILE)
WC_PROFILES  := --profile woocommerce --profile tools
SYNC_PROFILE := --profile sync
IMAGE        ?= ghcr.io/nfsarch33/agentic-ecommerce
TAG          ?= dev

test:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/mc-api ./cmd/mc-api
	go build -o bin/wc-sync ./cmd/wc-sync
	go build -o bin/content-worker ./cmd/content-worker

coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

docker-build:
	docker build --build-arg TARGET=mc-api -t $(IMAGE):$(TAG) .
	docker build --build-arg TARGET=wc-sync -t $(IMAGE):$(TAG)-wc-sync .
	docker build --build-arg TARGET=content-worker -t $(IMAGE):$(TAG)-content-worker .

docker-push:
	docker push $(IMAGE):$(TAG)
	docker push $(IMAGE):$(TAG)-wc-sync
	docker push $(IMAGE):$(TAG)-content-worker

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

migrate-up:
	@echo "==> Running migrations UP against $(DB_URL)"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0001_create_products.up.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0002_create_orders.up.sql

migrate-down:
	@echo "==> Running migrations DOWN against $(DB_URL)"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0002_create_orders.down.sql
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0001_create_products.down.sql

seed:
	@echo "==> Seeding test data"
	$(COMPOSE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < seed/products.sql

compose-config:
	$(COMPOSE) config --quiet

compose-wc-config:
	$(COMPOSE) $(WC_PROFILES) $(SYNC_PROFILE) config --quiet
