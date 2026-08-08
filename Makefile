YELLOW  := \033[33m
GREEN   := \033[32m
CYAN    := \033[36m
RESET   := \033[0m

.PHONY: help
help: ## Show help message
	@echo "$(YELLOW) commands:$(RESET)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-10s$(RESET) %s\n", $$1, $$2}'

run:    ## Run the application
	go run ./cmd/*

build:  ## Build the application
	go build -o bin/myapp ./cmd/*

test:   ## Run tests
	go test -v ./...

fmt:    ## Format Go files
	gofmt -w .
