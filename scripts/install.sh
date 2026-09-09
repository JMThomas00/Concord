#!/bin/sh
# Concord install script -- detects OS/arch and installs the latest
# concord-client (or concord-server/-hub, via CONCORD_INSTALL_BINARY) from
# the project's GitHub Releases, built by .github/workflows/release.yml.
#
# Usage:
#   curl -fsSL https://concord.chat/install.sh | sh
#   curl -fsSL https://concord.chat/install.sh | CONCORD_INSTALL_BINARY=concord-server sh
#
# NOT run or verified anywhere yet -- item 12 (8d) on the pre-v0.1.0 to-do.
# This depends on .github/workflows/release.yml actually having produced a
# tagged release with real assets first; there's nothing to download until
# then. The concord.chat URL above is a placeholder -- wherever this script
# actually ends up hosted (GitHub raw, a redirect, project site) is
# Jordan's call, not something to assume/provision here.
#
# Mirrors the shape of well-known installers like rustup/Homebrew's own
# install script: no server-side component, just a static script that
# resolves the right release asset and drops the binary on PATH.

set -eu

REPO="concord-chat/concord"
BINARY="${CONCORD_INSTALL_BINARY:-concord-client}"

case "$BINARY" in
  concord-client|concord-server|concord-hub) ;;
  *)
    echo "error: CONCORD_INSTALL_BINARY must be one of concord-client, concord-server, concord-hub (got: $BINARY)" >&2
    exit 1
    ;;
esac

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux)
    platform="linux-amd64"
    archive_ext="tar.gz"
    ;;
  Darwin)
    case "$arch" in
      arm64) platform="macos-arm64" ;;
      x86_64) platform="macos-x86_64" ;;
      *)
        echo "error: unsupported macOS architecture: $arch" >&2
        exit 1
        ;;
    esac
    archive_ext="tar.gz"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    echo "error: this script is for Linux/macOS. On Windows, download concord-windows-amd64.zip directly from:" >&2
    echo "  https://github.com/$REPO/releases/latest" >&2
    exit 1
    ;;
  *)
    echo "error: unsupported OS: $os" >&2
    exit 1
    ;;
esac

if [ "$os" = "Linux" ] && [ "$arch" != "x86_64" ]; then
  echo "error: only linux-amd64 release assets are published today (got arch: $arch)" >&2
  exit 1
fi

asset="concord-${platform}.${archive_ext}"
url="https://github.com/$REPO/releases/latest/download/$asset"

install_dir="${CONCORD_INSTALL_DIR:-/usr/local/bin}"
if [ ! -w "$install_dir" ] 2>/dev/null; then
  install_dir="$HOME/.local/bin"
  mkdir -p "$install_dir"
  echo "note: /usr/local/bin isn't writable, installing to $install_dir instead"
  echo "      (make sure it's on your PATH)"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "Downloading $asset..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$tmp_dir/$asset"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$url" -O "$tmp_dir/$asset"
else
  echo "error: need curl or wget to download the release archive" >&2
  exit 1
fi

echo "Extracting..."
tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"

install -m 755 "$tmp_dir/$BINARY" "$install_dir/$BINARY"

echo "Installed $BINARY to $install_dir/$BINARY"
echo "Run '$BINARY' to get started (or see README.md's Quick Start)."
