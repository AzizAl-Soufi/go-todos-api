YELLOW  := \033[33m
GREEN   := \033[32m
CYAN    := \033[36m
RESET   := \033[0m

ifneq (,$(wildcard ./.env))
    include .env
    export
endif

GOOSE_DRIVER ?= postgres
GOOSE_MIGRATION_DIR ?= ./internal/platform/database/postgres/migrations
GOOSE_DBSTRING ?= $(DATABASE_URI)

.DEFAULT_GOAL := help

ifeq (goose,$(firstword $(MAKECMDGOALS)))
  GOOSE_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
  $(foreach arg,$(GOOSE_ARGS),$(eval $(arg):;@:))
else

.PHONY: help run worker build test test-integration test-db-down fmt lint migrate sqlc

# Databases from docker-compose.test.yml. directConnection skips replica-set
# discovery, whose member address is only reachable inside the container.
TEST_POSTGRES_URI ?= postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable
TEST_MONGO_URI ?= mongodb://localhost:57017/?directConnection=true
TEST_REDIS_URL ?= redis://localhost:56379/0

# Built with the go.mod toolchain (a prebuilt binary may lag the Go version);
# set GOLANGCI_LINT=golangci-lint to use an installed one.
GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

help: ## Show this help message
	@echo "Usage:  make [OPTIONS] COMMAND"
	@echo ""
	@echo "A self-sufficient runtime for your Go backend"
	@awk '\
		BEGIN {FS = ":.*## "} \
		/^[a-zA-Z_-]+:.*?## / { printf "  $(GREEN)%-15s$(RESET) %s\n", $$1, $$2 } \
		/^##@/ { printf "\n%s\n", substr($$0, 5) } \
	' $(MAKEFILE_LIST)

##@ Common Commands:
run:    ## Run the API
	go run ./cmd/api

worker: ## Run the background worker (not with DB_TYPE=memory: set JOBS_RUN_IN_API instead)
	go run ./cmd/worker

build:  ## Build the api, worker and migrate binaries into bin/
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/migrate ./cmd/migrate

test:   ## Run unit tests (database integration tests are skipped)
	go test -race ./...

test-integration: ## Run all tests against real PostgreSQL, MongoDB and Redis (needs Docker)
	docker compose -f docker-compose.test.yml up -d --wait
	TEST_POSTGRES_URI="$(TEST_POSTGRES_URI)" TEST_MONGO_URI="$(TEST_MONGO_URI)" TEST_REDIS_URL="$(TEST_REDIS_URL)" go test -race -count=1 ./...
	@for backend in postgres mongo; do \
		echo "$(CYAN)end-to-end suite on $$backend$(RESET)"; \
		TEST_E2E_BACKEND=$$backend TEST_POSTGRES_URI="$(TEST_POSTGRES_URI)" TEST_MONGO_URI="$(TEST_MONGO_URI)" \
			go test -race -count=1 ./cmd/api || exit 1; \
	done

test-db-down: ## Stop the integration-test databases
	docker compose -f docker-compose.test.yml down

fmt:    ## Format Go files
	gofmt -s -w .

lint:   ## Run golangci-lint (.golangci.yml)
	$(GOLANGCI_LINT) run ./...

##@ Database Commands:
migrate: ## Apply migrations for DB_TYPE (postgres: embedded SQL, mongo: cmd/migrate/mongo.yaml)
	go run ./cmd/migrate

sqlc:    ## Generate each module's Go queries from its queries.sql
	sqlc generate

endif

.PHONY: goose

goose: ## Manage database migrations (Usage: make goose <command>)
	@if [ -z "$(GOOSE_ARGS)" ]; then \
		echo "Usage:  make goose COMMAND\n"; \
		echo "A database migration tool\n"; \
		echo "Commands:"; \
		echo "  $(GREEN)up$(RESET)          Migrate the DB to the most recent version"; \
		echo "  $(GREEN)down$(RESET)        Roll back the version by 1"; \
		echo "  $(GREEN)status$(RESET)      Print the migration status for the current DB"; \
		echo "  $(GREEN)reset$(RESET)       Roll back all migrations"; \
		echo "\nRun 'make goose help' for raw goose documentation."; \
	elif [ "$(GOOSE_ARGS)" = "help" ]; then \
		goose -h; \
	else \
		GOOSE_DRIVER=$(GOOSE_DRIVER) GOOSE_DBSTRING="$(GOOSE_DBSTRING)" goose -dir $(GOOSE_MIGRATION_DIR) $(GOOSE_ARGS); \
	fi