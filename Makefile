# Concord Makefile
# Terminal Chat Application

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Binary names
SERVER_BINARY=concord-server
CLIENT_BINARY=concord

# Build directories
BUILD_DIR=build
DIST_DIR=dist

# Version info
VERSION ?= 0.1.0
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Linker flags
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Platforms for cross-compilation
PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build build-server build-client clean test deps run-server run-client install dist help

# Default target
all: build

# Build both server and client
build: build-server build-client

# Build server
build-server:
	@echo "Building server..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) ./cmd/server

# Build client
build-client:
	@echo "Building client..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY) ./cmd/client

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -rf $(DIST_DIR)

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Run server (development)
run-server: build-server
	@echo "Starting server..."
	./$(BUILD_DIR)/$(SERVER_BINARY)

# Run client (development)
run-client: build-client
	@echo "Starting client..."
	./$(BUILD_DIR)/$(CLIENT_BINARY)

# Install binaries to system
install: build
	@echo "Installing binaries..."
	install -d $(DESTDIR)/usr/local/bin
	install -m 755 $(BUILD_DIR)/$(SERVER_BINARY) $(DESTDIR)/usr/local/bin/
	install -m 755 $(BUILD_DIR)/$(CLIENT_BINARY) $(DESTDIR)/usr/local/bin/
	@echo "Installing themes..."
	install -d $(DESTDIR)/usr/local/share/concord/themes
	install -m 644 configs/themes/*.toml $(DESTDIR)/usr/local/share/concord/themes/

# Build distribution packages for all platforms
dist:
	@echo "Building distribution packages..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(SERVER_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/server; \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(CLIENT_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/client; \
		echo "Built for $${platform}"; \
	done

# Build for Windows specifically
build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY).exe ./cmd/server
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY).exe ./cmd/client

# Development: watch for changes and rebuild
dev-server:
	@echo "Starting development server with hot reload..."
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air -c .air.server.toml

dev-client:
	@echo "Starting development client with hot reload..."
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air -c .air.client.toml

# Generate protocol documentation
docs:
	@echo "Generating documentation..."
	@which godoc > /dev/null || (echo "Installing godoc..." && go install golang.org/x/tools/cmd/godoc@latest)
	godoc -http=:6060

# Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

# Help
help:
	@echo "Concord - Terminal Chat Application"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all           Build both server and client (default)"
	@echo "  build         Build both server and client"
	@echo "  build-server  Build only the server"
	@echo "  build-client  Build only the client"
	@echo "  build-windows Build Windows executables"
	@echo "  clean         Remove build artifacts"
	@echo "  test          Run tests"
	@echo "  deps          Download and tidy dependencies"
	@echo "  run-server    Build and run the server"
	@echo "  run-client    Build and run the client"
	@echo "  install       Install binaries to system"
	@echo "  dist          Build for all platforms"
	@echo "  fmt           Format code"
	@echo "  lint          Lint code"
	@echo "  docs          Start documentation server"
	@echo "  help          Show this help"
	@echo ""
	@echo "Environment Variables:"
	@echo "  VERSION       Set version string (default: 0.1.0)"
	@echo "  DESTDIR       Set installation prefix"
