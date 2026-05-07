.PHONY: test build vet coverage lint dev dev-down migrate-up migrate-down seed

COMPOSE_FILE := docker-compose.dev.yml
DB_URL       ?= postgres://postgres:postgres@127.0.0.1:5432/ecommerce?sslmode=disable

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

dev:
	docker compose -f $(COMPOSE_FILE) up -d --build

dev-down:
	docker compose -f $(COMPOSE_FILE) down

dev-logs:
	docker compose -f $(COMPOSE_FILE) logs -f

migrate-up:
	@echo "==> Running migrations UP against $(DB_URL)"
	docker compose -f $(COMPOSE_FILE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0001_create_products.up.sql

migrate-down:
	@echo "==> Running migrations DOWN against $(DB_URL)"
	docker compose -f $(COMPOSE_FILE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < migrations/0001_create_products.down.sql

seed:
	@echo "==> Seeding test data"
	docker compose -f $(COMPOSE_FILE) exec -T postgres \
		psql "$(DB_URL)" -f /dev/stdin < seed/products.sql

compose-config:
	docker compose -f $(COMPOSE_FILE) config
