.PHONY: all build test clean proto install

# Variables
BINARY_DAEMON=bin/ipam-daemon
BINARY_CNI=bin/cni-plugin
BINARY_CLI=bin/ipam-cli
PROTO_DIR=pkg/api/proto
GO_FILES=$(shell find . -name '*.go' -type f)

all: build

# Build all binaries
build: proto
	@echo "Building binaries..."
	@mkdir -p bin
	go build -o $(BINARY_DAEMON) ./cmd/ipam-daemon
	go build -o $(BINARY_CNI) ./cmd/cni-plugin
	go build -o $(BINARY_CLI) ./cmd/ipam-cli
	@echo "Build complete!"

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@# Install protoc if needed: https://github.com/protocolbuffers/protobuf/releases
	@# Or use: apt-get install -y protobuf-compiler
	@if command -v protoc >/dev/null 2>&1; then \
		protoc --go_out=. --go_opt=paths=source_relative \
			--go-grpc_out=. --go-grpc_opt=paths=source_relative \
			$(PROTO_DIR)/ipam.proto; \
		echo "Protobuf generation complete!"; \
	else \
		echo "Warning: protoc not found. Skipping proto generation."; \
		echo "Install protoc to generate gRPC code: https://github.com/protocolbuffers/protobuf/releases"; \
	fi

# Run tests
test:
	@echo "Running tests..."
	go test -v -race -cover ./...

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./pkg/allocator
	go test -bench=. -benchmem ./pkg/ipam

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	rm -f $(PROTO_DIR)/*.pb.go

# Install dependencies
install-deps:
	@echo "Installing Go dependencies..."
	go mod tidy
	go mod download

# Install protoc tools
install-proto-tools:
	@echo "Installing protoc tools..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Install: https://golangci-lint.run/usage/install/"; \
	fi

# Run daemon locally (for testing)
run-daemon:
	@echo "Running IPAM daemon..."
	go run ./cmd/ipam-daemon --config configs/daemon.yaml

# Docker build
docker-build:
	@echo "Building Docker images..."
	docker build -t ipam-daemon:latest -f Dockerfile.daemon .
	docker build -t ipam-cni:latest -f Dockerfile.cni .

# Help
help:
	@echo "Available targets:"
	@echo "  make build           - Build all binaries"
	@echo "  make proto           - Generate protobuf code"
	@echo "  make test            - Run tests"
	@echo "  make bench           - Run benchmarks"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make install-deps    - Install Go dependencies"
	@echo "  make fmt             - Format code"
	@echo "  make lint            - Lint code"
	@echo "  make run-daemon      - Run daemon locally"
	@echo "  make docker-build    - Build Docker images"
