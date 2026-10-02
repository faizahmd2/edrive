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
make darwin-arm64

mkdir -p "$INSTALL_ROOT" "$INSTALL_DIR"
cp "$ROOT/bin/edrive-darwin-arm64" "$INSTALL_ROOT/edrive"
chmod 700 "$INSTALL_ROOT/edrive"
ln -sf "$INSTALL_ROOT/edrive" "$INSTALL_DIR/edrive"

printf '%s\n' '[edrive] installed:' "$INSTALL_ROOT/edrive"
printf '%s\n' '[edrive] next: edrive setup'
