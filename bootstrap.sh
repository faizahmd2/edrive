#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
INSTALL_DIR="$HOME/.local/bin"

printf '%s\n' '[edrive] bootstrapping edrive...'
command -v go >/dev/null 2>&1 || {
  echo '[edrive] Go is required.' >&2
  exit 1
}

cd "$ROOT"
make darwin-arm64

mkdir -p "$INSTALL_DIR"
mkdir -p "$ROOT/bin"
cp "$ROOT/bin/edrive-darwin-arm64" "$ROOT/bin/edrive"
chmod 700 "$ROOT/bin/edrive"
ln -sf "$ROOT/bin/edrive" "$INSTALL_DIR/edrive"

printf '%s\n' '[edrive] installed: edrive'
printf '%s\n' '[edrive] next: edrive setup'
