.PHONY: build test test-e2e test-all lint clean install run dev

# Build variables
BINARY_NAME=cbox
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X github.com/bobbyrathore/cbox/internal/cli.Version=$(VERSION) -X github.com/bobbyrathore/cbox/internal/cli.Commit=$(COMMIT) -X github.com/bobbyrathore/cbox/internal/cli.BuildTime=$(BUILD_TIME)"

# Default target
all: build

# Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/cbox

# Build for release (smaller binary)
build-release:
	CGO_ENABLED=0 go build $(LDFLAGS) -ldflags "-s -w" -o bin/$(BINARY_NAME) ./cmd/cbox

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run e2e tests (requires Docker)
test-e2e: build
	go test -v -timeout 10m ./tests/e2e/...

# Run all tests
test-all: test test-e2e

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	gofmt -s -w .

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Install binary to GOPATH/bin
install: build
	cp bin/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)

# Run the CLI (for development)
run: build
	./bin/$(BINARY_NAME)

# Run in dev mode
dev:
	go run ./cmd/cbox $(ARGS)

# Tidy dependencies
tidy:
	go mod tidy

# Download dependencies
deps:
	go mod download

# Check for vulnerabilities
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
