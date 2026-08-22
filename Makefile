.DEFAULT_GOAL := help

SDK_KEY   ?= sdk-local-key
ADMIN_KEY ?= admin-local-key
BASE_URL  ?= http://localhost:8090

.PHONY: help tidy fmt vet test cover bench fuzz check run up down logs demo traffic report

help:
	@grep -E '^[a-z-]+:.*?##' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

tidy: ## Resolve dependencies and write go.sum
	go mod tidy

fmt: ## Format
	gofmt -l -w .

vet: ## Static analysis
	go vet ./...

test: ## Unit tests with the race detector
	go test -race ./...

cover: ## Coverage summary
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

bench: ## Benchmarks with allocation counts
	go test -run '^$$' -bench . -benchmem ./internal/expr/ ./internal/bucket/ ./internal/eval/

fuzz: ## Fuzz the parser for 30s
	go test -run '^$$' -fuzz FuzzCompile -fuzztime 30s ./internal/expr/

check: fmt vet test ## Everything before a commit

run: ## Run without a database; state lives in memory
	RULECAST_SDK_KEY=$(SDK_KEY) RULECAST_ADMIN_KEY=$(ADMIN_KEY) go run ./cmd/rulecast

up: ## Start Postgres and the service in containers
	docker compose up -d --build
	@echo "  ui       $(BASE_URL)/index.html"
	@echo "  metrics  $(BASE_URL)/metrics"

down: ## Stop the containers
	docker compose down

logs: ## Tail the service log
	docker compose logs -f app

traffic: ## Drive synthetic traffic so exposures accumulate (USERS=5000 FLAG=…)
	go run ./cmd/rulecastload \
		-url $(BASE_URL) -key $(SDK_KEY) \
		-flag $${FLAG:-pricing-banner-copy} -users $${USERS:-5000}

report: ## Show the exposure split for a flag (FLAG=…)
	@curl -sS "$(BASE_URL)/api/v1/admin/experiments/$${FLAG:-pricing-banner-copy}" \
		-H "X-API-Key: $(ADMIN_KEY)"
	@echo ""

demo: ## Evaluate one flag against the running service
	@curl -sS -X POST $(BASE_URL)/api/v1/sdk/evaluate \
		-H "X-API-Key: $(SDK_KEY)" -H 'Content-Type: application/json' \
		-d '{"flagKey":"checkout-v2","attributes":{"user.key":"user-1234","user.country":"DE","user.tier":"vip","user.spend":400}}'
	@echo ""
