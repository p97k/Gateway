.PHONY: help build run test test-race cover vet tidy lint docker-up docker-down token clean

GATEWAY_BIN := bin/gateway
MOCK_BIN    := bin/mockservice

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Build gateway and mock service binaries into ./bin
	@mkdir -p bin
	go build -o $(GATEWAY_BIN) ./cmd/gateway
	go build -o $(MOCK_BIN) ./cmd/mockservice

run: ## Run the gateway locally with configs/config.yaml
	go run ./cmd/gateway -config configs/config.yaml

test: ## Run all tests
	go test ./...

test-race: ## Run all tests with the race detector
	go test -race ./...

cover: ## Run tests and print total coverage
	go test ./... -coverpkg=./internal/...,./pkg/... -coverprofile=cover.out
	@go tool cover -func=cover.out | tail -1
	@echo "HTML report: go tool cover -html=cover.out"

vet: ## go vet
	go vet ./...

tidy: ## go mod tidy
	go mod tidy

docker-up: ## Build and start the full stack (gateway + services + prometheus + grafana)
	docker compose up --build

docker-down: ## Stop the stack
	docker compose down -v

token: ## Mint a demo JWT (override: make token ROLE=admin SUB=u1)
	@go run ./cmd/token -role $(or $(ROLE),customer) -sub $(or $(SUB),user-123)

clean: ## Remove build artifacts
	rm -rf bin cover.out
