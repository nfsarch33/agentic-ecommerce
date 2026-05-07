.PHONY: test build vet coverage

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
