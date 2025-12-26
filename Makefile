.PHONY: help build install clean test run deps version

# Build variables
BINARY_NAME=agnostech
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE?=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X github.com/agnostech/agnostech/pkg/version.Version=$(VERSION) -X github.com/agnostech/agnostech/pkg/version.Commit=$(COMMIT) -X github.com/agnostech/agnostech/pkg/version.Date=$(BUILD_DATE)"

help: ## Display this help message
	@echo "AgnosTech CLI - Available Commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies installed successfully"

build: deps ## Build the CLI binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p bin
	@go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/agnostech
	@echo "Build complete: bin/$(BINARY_NAME)"

install: build ## Install the CLI to GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@go install $(LDFLAGS) ./cmd/agnostech
	@echo "Installed to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -f $(BINARY_NAME)
	@go clean
	@echo "Clean complete"

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

run: build ## Build and run the CLI with --help
	@./bin/$(BINARY_NAME) --help

version: build ## Display version information
	@./bin/$(BINARY_NAME) version

# Example commands
example-analyze: build ## Run example analyze command
	@echo "Running example analyze command..."
	@./bin/$(BINARY_NAME) analyze ./test/fixtures/sample.tfstate --format table

example-migrate: build ## Run example migrate command
	@echo "Running example migrate command..."
	@./bin/$(BINARY_NAME) migrate ./test/fixtures/sample.tfstate --output ./example-output

example-validate: build ## Run example validate command
	@echo "Running example validate command..."
	@./bin/$(BINARY_NAME) validate ./example-output

dev: ## Run in development mode with verbose output
	@go run ./cmd/agnostech --verbose --help

# Build for multiple platforms
build-all: ## Build for all platforms
	@echo "Building for all platforms..."
	@mkdir -p bin
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/agnostech
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/agnostech
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/agnostech
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/agnostech
	@echo "Build complete for all platforms"

# Web dashboard targets
.PHONY: serve web-install web-build web-dev web-clean

WEB_DIR = web

serve: build ## Start the web dashboard
	./bin/$(BINARY_NAME) serve

web-install: ## Install web dependencies
	cd $(WEB_DIR) && npm install

web-build: web-install ## Build web frontend
	cd $(WEB_DIR) && npm run build
	mkdir -p internal/api/static
	cp -r $(WEB_DIR)/dist/* internal/api/static/

web-dev: ## Run web frontend in dev mode
	cd $(WEB_DIR) && npm run dev

web-clean: ## Clean web build artifacts
	rm -rf $(WEB_DIR)/node_modules $(WEB_DIR)/dist
	rm -rf internal/api/static

build-with-web: web-build build ## Build with embedded web frontend
