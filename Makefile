# Goonj developer workflow
SHELL := /bin/bash
export PATH := $(HOME)/.local/go/bin:$(PATH)

.PHONY: help up down logs ps build web-install web-dev web-build typecheck \
        go-build go-test go-vet seed migrate docker-build e2e ci

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## Start full dev stack (postgres, redis, api, worker, ws, web, seed)
	docker compose up --build -d
	@echo "web      → http://localhost:3000"
	@echo "api      → http://localhost:8080/api/v1/ping"
	@echo "ws       → ws://localhost:8081/ws"
	@echo "readyz   → http://localhost:8080/readyz"

down: ## Stop the dev stack
	docker compose down

logs: ## Tail all service logs
	docker compose logs -f --tail=100

ps: ## Show service status
	docker compose ps

web-install: ## Install workspace deps with bun
	bun install

web-dev: ## Run Next.js dev server (native, hot reload)
	bun --filter web dev

web-build: ## Production build of the web app
	bun --filter web build

go-build: ## Build all Go binaries to ./bin
	mkdir -p bin
	cd apps/api && go build -o ../../bin/api ./cmd/api
	cd apps/api && go build -o ../../bin/worker ./cmd/worker
	cd apps/api && go build -o ../../bin/ws-gateway ./cmd/ws-gateway
	cd apps/api && go build -o ../../bin/seed ./cmd/seed

go-vet: ## go vet across the Go module
	cd apps/api && go vet ./...

go-test: ## Run Go tests
	cd apps/api && go test ./...

seed: ## Run migrations + seed against local postgres (uses .env DATABASE_URL)
	cd apps/api && go run ./cmd/seed

migrate: ## Print migration status
	cd apps/api && goose -dir migrations postgres "$${DATABASE_URL:-postgres://goonj:goonj@localhost:5432/goonj?sslmode=disable}" status

docker-build: ## Build all images without starting
	docker compose build

typecheck: ## TypeScript check for all workspaces
	bun --filter '*' typecheck

e2e: ## Run Phase 4 end-to-end suite against the live compose stack
	bash scripts/e2e-phase4.sh

ci: go-vet go-test web-install web-build typecheck ## Everything CI runs
