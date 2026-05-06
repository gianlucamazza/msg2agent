# msg2agent Makefile

# Variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GOFLAGS ?=
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"

# Directories
BUILD_DIR := ./build
DIST_DIR  := ./dist

# Binaries
BINARIES := relay mcp-server billing-admin dashboard

# Go commands
GO      := go
GOTEST  := $(GO) test
GOBUILD := $(GO) build $(GOFLAGS) $(LDFLAGS)
GOVET   := $(GO) vet
GOFMT   := gofmt

.PHONY: all build build-relay build-mcp build-billing-admin build-dashboard clean test test-unit test-integration test-e2e test-coverage lint fmt vet docker-build docker-push install help bootstrap demo smoke
.PHONY: dev dev-up dev-down dev-logs dev-ps
.PHONY: scenario-p2p scenario-relay scenario-tls scenario-mcp
.PHONY: compose-sqlite compose-tls compose-observability compose-p2p
.PHONY: test-security test-load test-a2a test-mcp test-all loadtest loadtest-http loadtest-ws
.PHONY: ci ci-e2e setup-certs

## Build

all: build

build: build-relay build-mcp build-billing-admin build-dashboard ## Build all binaries

build-relay: ## Build relay binary
	$(GOBUILD) -o $(BUILD_DIR)/relay ./cmd/relay

build-mcp: ## Build mcp-server binary
	$(GOBUILD) -o $(BUILD_DIR)/mcp-server ./cmd/mcp-server

build-billing-admin: ## Build billing-admin CLI
	$(GOBUILD) -o $(BUILD_DIR)/billing-admin ./cmd/billing-admin

build-dashboard: ## Build dashboard binary
	$(GOBUILD) -o $(BUILD_DIR)/dashboard ./cmd/dashboard

## Testing

test: test-unit ## Run all tests (unit only by default)

test-unit: ## Run unit tests
	$(GOTEST) -v -race ./pkg/... ./cmd/...

test-integration: ## Run integration tests
	$(GOTEST) -v -race -tags=integration ./test/...

test-e2e: build ## Run end-to-end tests (builds binaries first)
	$(GOTEST) -v -tags=e2e ./test/...

test-coverage: ## Run tests with coverage report
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/... ./cmd/...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Code Quality

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code
	$(GOFMT) -w -s .

vet: ## Run go vet
	$(GOVET) ./...

check: fmt vet lint ## Run all code quality checks

## Docker

docker-build: ## Build Docker images
	docker build --target relay -t msg2agent-relay:$(VERSION) -t msg2agent-relay:latest .
	docker build --target mcp-server -t msg2agent-mcp-server:$(VERSION) -t msg2agent-mcp-server:latest .
	docker build --target billing-admin -t msg2agent-billing-admin:$(VERSION) -t msg2agent-billing-admin:latest .
	docker build --target dashboard -t msg2agent-dashboard:$(VERSION) -t msg2agent-dashboard:latest .

docker-push: ## Push Docker images to registry
	docker push msg2agent-relay:$(VERSION)
	docker push msg2agent-relay:latest
	docker push msg2agent-mcp-server:$(VERSION)
	docker push msg2agent-mcp-server:latest
	docker push msg2agent-billing-admin:$(VERSION)
	docker push msg2agent-billing-admin:latest
	docker push msg2agent-dashboard:$(VERSION)
	docker push msg2agent-dashboard:latest

## Installation

install: build ## Install binaries to GOPATH/bin
	$(GO) install ./cmd/relay
	$(GO) install ./cmd/mcp-server
	$(GO) install ./cmd/billing-admin
	$(GO) install ./cmd/dashboard

## Cleanup

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) $(DIST_DIR)
	rm -f coverage.out coverage.html
	rm -f agent relay mcp-server billing-admin dashboard

## Help

help: ## Show this help
	@echo "msg2agent Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## Development

dev: build ## Build and run locally (relay + 2 agents)
	@./scripts/dev-run.sh

dev-up: docker-build ## Start development environment with Docker Compose
	docker compose up -d
	@echo ""
	@echo "Services started. Run 'make dev-logs' to follow logs."
	@echo "  Relay:        http://localhost:8080/health"
	@echo "  MCP Server:   http://localhost:3001/mcp"
	@echo "  Dashboard:    http://localhost:8082"

dev-down: ## Stop development environment
	docker compose down -v

dev-logs: ## Follow development logs
	docker compose logs -f

dev-ps: ## Show running containers
	docker compose ps

## Test Scenarios

scenario-p2p: ## Run P2P direct messaging test
	@./scripts/scenarios/p2p-direct.sh

scenario-relay: ## Run relay messaging test
	@./scripts/scenarios/relay-messaging.sh

scenario-tls: setup-certs ## Run TLS test scenario
	docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d --build
	@sleep 5
	@curl -sf --insecure https://localhost:8443/health && echo " TLS relay healthy"
	docker compose -f docker-compose.yml -f docker-compose.tls.yml down -v

scenario-mcp: ## Run MCP stdio test
	@./scripts/scenarios/mcp-stdio.sh

## Docker Compose Profiles

compose-sqlite: docker-build ## Start with SQLite persistence
	docker compose -f docker-compose.yml -f docker-compose.sqlite.yml up -d

compose-tls: docker-build setup-certs ## Start with TLS enabled
	docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d

compose-observability: docker-build ## Start with Prometheus + Jaeger
	docker compose -f docker-compose.yml -f docker-compose.observability.yml up -d
	@echo ""
	@echo "Observability stack started:"
	@echo "  Prometheus: http://localhost:9090"
	@echo "  Jaeger UI:  http://localhost:16686"

compose-p2p: docker-build ## Start P2P mode (no relay)
	docker compose -f docker-compose.p2p.yml up -d

## Additional Tests

test-security: ## Run security-related tests
	$(GOTEST) -v -race -tags=security ./pkg/security/... ./pkg/crypto/...

test-load: ## Run load/performance tests
	@./scripts/test-load.sh

loadtest: ## Run k6 billing HTTP load test (requires k6)
	k6 run loadtest/billing_http.k6.js \
		-e MCP_URL=$(MCP_URL) \
		-e API_KEY=$(API_KEY) \
		-e API_KEYS_FILE=$(API_KEYS_FILE)

loadtest-http: loadtest ## Alias for loadtest

loadtest-ws: ## Run k6 relay WebSocket load test (requires k6)
	k6 run loadtest/relay_ws.k6.js \
		-e RELAY_URL=$(RELAY_URL)

test-a2a: ## Run A2A adapter tests
	$(GOTEST) -v -race ./adapters/a2a/...

test-mcp: ## Run MCP adapter tests
	$(GOTEST) -v -race ./adapters/mcp/...

test-all: test-unit test-integration test-a2a test-mcp ## Run all tests

## Setup

setup-certs: ## Generate TLS certificates for testing
	@./scripts/setup-certs.sh

bootstrap: build ## First-time setup: identity, billing DB, first tenant + API key
	@./scripts/bootstrap.sh

demo: bootstrap ## Bootstrap + seed demo data (3 tenants, 50 events each)
	@./scripts/seed-demo.sh

smoke: ## Post-deploy smoke test (env: RELAY_URL, MCP_URL, API_KEY)
	@./scripts/smoke-test.sh

## CI

ci: check test-all ## Run full CI pipeline (lint + all tests)
	@echo "CI pipeline complete"

ci-e2e: docker-build ## Run E2E tests with Docker
	docker compose up -d --build
	@sleep 5
	@./scripts/scenarios/relay-messaging.sh --native || (docker compose down -v && exit 1)
	docker compose down -v
	@echo "E2E tests passed"
