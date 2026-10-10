#!/bin/sh
# Concord's one-line installer, for Linux and macOS:
#
#   curl -fsSL https://github.com/JMThomas00/Concord/releases/latest/download/install.sh | sh
#
# It fetches concord-install (cmd/install) for this computer from the
# latest release, checks it against the release's SHA256SUMS, and runs it:
# a guided form that installs the client, server and/or hub, with
# everything they need. Arguments pass through:
#
#   curl -fsSL .../install.sh | sh -s -- --dry-run
#
# CONCORD_RELEASE=v0.1.0 picks a release other than the latest.
set -eu

REPO="JMThomas00/Concord"
if [ -n "${CONCORD_RELEASE:-}" ]; then
  BASE="https://github.com/$REPO/releases/download/$CONCORD_RELEASE"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi

purple() { printf '\033[38;2;189;147;249m%s\033[0m\n' "$1"; }
dim() { printf '\033[38;2;98;114;164m%s\033[0m\n' "$1"; }
fail() { printf '\033[38;2;255;85;85m%s\033[0m\n' "$1" >&2; exit 1; }

# Questions need the keyboard even though this script arrives on stdin.
if (exec </dev/tty) 2>/dev/null; then TTY=/dev/tty; else TTY=""; fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=macos ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *)
    [ -n "$TTY" ] || fail "Concord couldn't tell which system this is."
    printf 'Which system is this computer running?\n  1) macOS\n  2) Linux\n  3) Windows\n> '
    read -r pick < "$TTY"
    case "$pick" in 1*|m*|M*) os=macos ;; 2*|l*|L*) os=linux ;; 3*|w*|W*) os=windows ;; *) fail "No problem: run this again when you know." ;; esac
    ;;
esac
if [ "$os" = windows ]; then
  echo "On Windows, paste this into PowerShell instead:"
  purple "  irm https://github.com/$REPO/releases/latest/download/install.ps1 | iex"
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "Concord doesn't have a build for $(uname -m) yet." ;;
esac
[ "$os" = macos ] && [ "$arch" = amd64 ] && arch=x86_64

asset="concord-install-$os-$arch"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

purple "🍇 Fetching the Concord installer…"
fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL --retry 3 --retry-delay 2 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -q --tries=3 "$1" -O "$2"
  else fail "This needs curl or wget to download Concord."
  fi
}
fetch "$BASE/$asset" "$tmp/concord-install" || fail "Couldn't download $asset (is there a published release yet? https://github.com/$REPO/releases)"

if fetch "$BASE/SHA256SUMS" "$tmp/SHA256SUMS" 2>/dev/null; then
  want="$(grep " \*\{0,1\}$asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)"
  if command -v sha256sum >/dev/null 2>&1; then got="$(sha256sum "$tmp/concord-install" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null 2>&1; then got="$(shasum -a 256 "$tmp/concord-install" | cut -d' ' -f1)"
  else got=""
  fi
  if [ -n "$want" ] && [ -n "$got" ] && [ "$want" != "$got" ]; then
    fail "The installer doesn't match its published checksum, so it wasn't run. Try again in a minute."
  fi
fi

chmod +x "$tmp/concord-install"
if [ -n "$TTY" ]; then
  "$tmp/concord-install" "$@" < "$TTY"
else
  "$tmp/concord-install" "$@"
fi
