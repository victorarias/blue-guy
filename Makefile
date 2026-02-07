.PHONY: build build-fuse run test lint proto clean help

BINARY := blue-guy
BUILD_DIR := bin
GO := go
PROTO_DIR := proto
GEN_DIR := internal/proto/gen
VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build host-only binary (no FUSE dependency)
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/blue-guy

build-fuse: ## Build with FUSE support (requires fuse-t: brew install fuse-t)
	CGO_ENABLED=1 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/blue-guy

run: build ## Build and run in host mode
	./$(BUILD_DIR)/$(BINARY)

test: ## Run tests
	CGO_ENABLED=0 $(GO) test ./cmd/... ./internal/host/... ./internal/gitops/...

lint: ## Run linters
	golangci-lint run

proto: ## Generate protobuf/gRPC code
	protoc \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(PROTO_DIR)/blueguy.proto

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
