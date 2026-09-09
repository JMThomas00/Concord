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

# Build client (voice enabled by default — requires a C toolchain).
# CC=clang: MSYS2's mingw-w64-gcc 16.2.0 has a real, reproducible internal-compiler-error/
# segfault bug on miniaudio.c (~40-70% failure rate, confirmed via repeated-compile testing
# 2026-09-06 — not hardware, not fixed by retrying gcc). clang builds this reliably; keep
# this override until the GCC bug is fixed upstream or a newer package resolves it.
build-client:
	@echo "Building client (with voice)..."
	@mkdir -p $(BUILD_DIR)
	CC=clang CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY) ./cmd/client

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
# voice-included concord-client.exe. This target stays CGO-disabled/novoice-only
# by design — it's the quick local "get something running on another OS to test
# protocol-level stuff" path; .github/workflows/release.yml's native per-OS CI
# matrix is the actual path to real voice-included cross-platform releases.
# Includes the hub — a past version of this target omitted it entirely despite
# the hub being just as CGO-free/cross-compile-clean as the server.
dist:
	@echo "Building distribution packages (novoice — use build-windows on Windows for a voice-included client)..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		CGO_ENABLED=0 GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(SERVER_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/server; \
		CGO_ENABLED=0 GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) -tags novoice $(LDFLAGS) -o $(DIST_DIR)/$(CLIENT_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/client; \
		CGO_ENABLED=0 GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(HUB_BINARY)-$${platform%/*}-$${platform#*/}$(if $(findstring windows,$${platform%/*}),.exe,) ./cmd/hub; \
		echo "Built for $${platform}"; \
	done

# Build all three Windows binaries — THE canonical Windows build (requires MSYS2's
# clang via `pacman -S mingw-w64-x86_64-clang` for the client's voice engine — see
# note below on why clang, not gcc). Voice is compiled into concord-client.exe
# unconditionally; there is no separate "with voice" target because there is no
# without-voice product.
# Output: build/concord-server.exe, build/concord-client.exe, build/concord-hub.exe
#
# Why clang and not gcc: MSYS2's mingw-w64-gcc 16.2.0 has a real, reproducible
# internal-compiler-error/segfault bug compiling miniaudio.c (the malgo/voice
# dependency) — ~40-70% failure rate measured via repeated back-to-back compiles
# of the identical file, different crash site every time. Confirmed via a fully
# serialized build crashing in 22s on a trivial standard file (rules out
# load/thermal causes) and via clang building the same file with a near-zero
# failure rate. Not yet filed upstream. Full investigation: vault note
# "Concord - Voice Client Build Toolchain Bug". Revisit CC=clang once a GCC
# point release fixes this.
# Voice deps are linked STATICALLY so concord-client.exe doesn't depend on
# libogg-0.dll/libopus-0.dll/libopusfile-0.dll being present alongside it --
# confirmed 2026-09-08 that without this, only malgo's bundled miniaudio.c
# portion was actually static; opus/opusfile were always linked dynamically
# against MSYS2's import libraries, making concord-client.exe not a true
# single-file executable despite everything else about it being one.
# Requires MSYS2's static archives (`pacman -S mingw-w64-x86_64-opus
# mingw-w64-x86_64-opusfile` already provides these alongside the .dlls,
# no extra package needed).
#
# `-Wl,-Bstatic ... -Wl,-Bdynamic` around just the opus/opusfile/ogg libs
# is required, not optional -- `pkg-config --static --libs opus opusfile`
# alone (the seemingly-obvious approach) does NOT force static resolution
# on MinGW; it only adds the extra transitive libs (-logg) that dynamic
# linking normally hides. Without the explicit -Bstatic/-Bdynamic wrap,
# MinGW's linker still prefers each lib's .dll.a import archive over its
# .a static archive regardless of pkg-config's --static flag, silently
# reproducing the exact DLL dependency this is meant to remove. Verified
# 2026-09-08 via objdump -p on the resulting .exe (confirms KERNEL32.dll/
# msvcrt.dll only, no libopus/libopusfile/libogg) and by launching it with
# only C:\Windows\System32 on PATH -- runs fine with zero MSYS2 DLLs
# reachable at all. -lm stays outside the static wrap (libm's relevant
# symbols are effectively part of msvcrt on Windows either way).
#
# CGO_LDFLAGS hardcodes the MSYS2 mingw64 lib path rather than shelling
# out to pkg-config, so it doesn't depend on the PATH-prefix-vs-command-
# substitution evaluation-order pitfall that affects Make/shell recipes
# (a bare $(shell pkg-config ...) at Make-parse time, or an inline shell
# command substitution on the same line as a PATH= prefix, both evaluate
# before that PATH override is actually in effect for the shell doing the
# resolving) -- simpler to hardcode the one path than work around that.
build-windows:
	@echo "Building for Windows (server, client with voice, hub)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY).exe ./cmd/server
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(HUB_BINARY).exe ./cmd/hub
	PATH="/c/msys64/mingw64/bin:$(PATH)" CC=clang CGO_ENABLED=1 \
		CGO_LDFLAGS="-LC:/msys64/mingw64/lib -Wl,-Bstatic -lopusfile -logg -lopus -Wl,-Bdynamic -lm" \
		$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(CLIENT_BINARY).exe ./cmd/client
	@echo "Build complete: $(BUILD_DIR)/$(SERVER_BINARY).exe $(BUILD_DIR)/$(CLIENT_BINARY).exe $(BUILD_DIR)/$(HUB_BINARY).exe"
	@echo "Verify with: objdump -p $(BUILD_DIR)/$(CLIENT_BINARY).exe | grep 'DLL Name' (or move it to a dir with no MSYS2 DLLs on PATH and launch it) -- should show no libogg/libopus/libopusfile dependency."

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
	@echo "  build-windows         Build all 3 Windows binaries — server, client (voice included, built with clang), hub"
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
