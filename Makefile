.PHONY: build test clean run install docker-build help

# Binary name
BINARY_NAME=mqtt2irc
VERSION?=dev

# Build flags
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/mqtt2irc

build-linux: ## Build for Linux
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux ./cmd/mqtt2irc

build-darwin: ## Build for macOS
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin ./cmd/mqtt2irc

build-all: build-linux build-darwin ## Build for all platforms

test: ## Run tests
	go test -v ./...

test-cover: ## Run tests with coverage
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux $(BINARY_NAME)-darwin
	rm -f coverage.out coverage.html

run: build ## Build and run with example config
	./$(BINARY_NAME) -config configs/config.example.yaml

install: ## Install the binary
	go install $(LDFLAGS) ./cmd/mqtt2irc

fmt: ## Format code
	go fmt ./...

lint: ## Run linter
	golangci-lint run

docker-build: ## Build Docker image
	docker build -t $(BINARY_NAME):$(VERSION) .

docker-compose-up: ## Start test environment (MQTT + IRC)
	docker-compose up -d

docker-compose-down: ## Stop test environment
	docker-compose down

deps: ## Download dependencies
	go mod download
	go mod tidy

.DEFAULT_GOAL := help
