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
#
# Concord ships exactly three binaries: concord-server(.exe), concord-client(.exe),
# concord-hub(.exe). Voice is a foundational feature of the client, not an optional
# add-on — concord-client(.exe) always includes it. The `novoice` build tag / targets
# below exist ONLY as a CGO-free fallback for environments without a C toolchain
# (CI runners, a dev machine without MSYS2 installed, etc.) — they are a build
# convenience, not a second product line, and their output is never named
# concord-client(.exe) so it can't be mistaken for the real thing.
SERVER_BINARY=concord-server
CLIENT_BINARY=concord-client
HUB_BINARY=concord-hub

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

.PHONY: all build build-server build-client build-hub clean test deps run-server run-client run-hub install dist help

# Default target
all: build

# Build both server and client
build: build-server build-client build-hub

# Build server
build-server:
	@echo "Building server..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) ./cmd/server

# Build client (voice enabled by default — requires a C toolchain)
build-client:
	@echo "Building client (with voice)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY) ./cmd/client

# Build hub (pure Go, no CGO)
build-hub:
	@echo "Building hub..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(HUB_BINARY) ./cmd/hub

# Run hub (development)
run-hub: build-hub
	@echo "Starting hub..."
	./$(BUILD_DIR)/$(HUB_BINARY)

# Build client without voice/audio (no C toolchain required)
build-client-novoice:
	@echo "Building client (no voice)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) -tags novoice $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY)-novoice ./cmd/client

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

# Build distribution packages for all platforms (novoice — CGO cross-compilation
# across platforms isn't practical from one host; this is a cross-compile
# limitation, not a product decision). Use build-windows on Windows for the real,
# voice-included concord-client.exe.
dist:
	@echo "Building distribution packages (novoice — use build-windows on Windows for a voice-included client)..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		CGO_ENABLED=0 GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(SERVER_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/server; \
		CGO_ENABLED=0 GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) -tags novoice $(LDFLAGS) -o $(DIST_DIR)/$(CLIENT_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/client; \
		echo "Built for $${platform}"; \
	done

# Build all three Windows binaries — THE canonical Windows build (requires MinGW
# GCC via MSYS2 for the client's voice engine: install from https://www.msys2.org/
# then `pacman -S mingw-w64-x86_64-gcc`). Voice is compiled into concord-client.exe
# unconditionally; there is no separate "with voice" target because there is no
# without-voice product.
# Output: build/concord-server.exe, build/concord-client.exe, build/concord-hub.exe
build-windows:
	@echo "Building for Windows (server, client with voice, hub)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY).exe ./cmd/server
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(HUB_BINARY).exe ./cmd/hub
	PATH="C:/msys64/mingw64/bin:$(PATH)" CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY).exe ./cmd/client
	@echo "Build complete: $(BUILD_DIR)/$(SERVER_BINARY).exe $(BUILD_DIR)/$(CLIENT_BINARY).exe $(BUILD_DIR)/$(HUB_BINARY).exe"

# Fallback only — no MSYS2/GCC available (e.g. CI, or a dev machine without the
# toolchain installed). Produces a CGO-free, voice-STRIPPED client. Deliberately
# named -novoice, never concord-client.exe, so it can't get deployed as if it were
# the real client. Prefer build-windows whenever a C toolchain is available.
build-windows-novoice:
	@echo "Building for Windows without voice (fallback — no C toolchain found)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY).exe ./cmd/server
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(HUB_BINARY).exe ./cmd/hub
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) -tags novoice $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY)-novoice.exe ./cmd/client

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
	@echo "  build-windows         Build all 3 Windows binaries — server, client (voice included), hub"
	@echo "  build-windows-novoice Fallback only: no MSYS2/GCC on this machine, voice-stripped client"
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
