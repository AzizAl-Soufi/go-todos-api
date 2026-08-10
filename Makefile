YELLOW  := \033[33m
GREEN   := \033[32m
CYAN    := \033[36m
RESET   := \033[0m

ifneq (,$(wildcard ./.env))
    include .env
    export
endif

GOOSE_DRIVER ?= postgres
GOOSE_MIGRATION_DIR ?= ./internal/pkg/database/postgres/migrations
GOOSE_DBSTRING ?= $(DATABASE_URI)

.DEFAULT_GOAL := help

ifeq (goose,$(firstword $(MAKECMDGOALS)))
  GOOSE_ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
  $(foreach arg,$(GOOSE_ARGS),$(eval $(arg):;@:))
else

.PHONY: help run build test fmt sqlc-generate

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
run:    ## Create and run the application
	go run ./cmd/...

build:  ## Build the application binary
	go build -o bin/myapp ./cmd/...

test:   ## Run tests
	go test -v ./...

fmt:    ## Format Go files
	gofmt -s -w .

##@ Database Commands:
sqlc:    ## Generate Go models and queries from SQL
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