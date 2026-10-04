#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
INSTALL_ROOT="$HOME/.local/lib/edrive"
INSTALL_DIR="$HOME/.local/bin"

printf '%s\n' '[edrive] building edrive...'
command -v go >/dev/null 2>&1 || {
  echo '[edrive] Go is required to build from source.' >&2
  exit 1
}

cd "$ROOT"
make release

mkdir -p "$INSTALL_ROOT" "$INSTALL_DIR"
# Replace the binary with a new file (never overwrite in place): macOS caches
# code signatures per file and kills a binary whose contents changed under it.
cp "$ROOT/bin/edrive-darwin-arm64" "$INSTALL_ROOT/.edrive.new"
chmod 755 "$INSTALL_ROOT/.edrive.new"
mv -f "$INSTALL_ROOT/.edrive.new" "$INSTALL_ROOT/edrive"

# Stop helpers still running from the previous version.
pkill -f "$INSTALL_ROOT/edrive __guard" 2>/dev/null || true
pkill -f "$HOME/.local/bin/edrive __guard" 2>/dev/null || true
ln -sf "$INSTALL_ROOT/edrive" "$INSTALL_DIR/edrive"

printf '%s\n' '[edrive] installed:' "$INSTALL_ROOT/edrive"
printf '%s\n' '[edrive] next: edrive setup'
