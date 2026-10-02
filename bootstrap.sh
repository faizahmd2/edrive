#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
CONFIG_DIR="$HOME/Desktop/local-infra/edrive"

printf '%s\n' '[edrive] checking dependencies...'
command -v go >/dev/null 2>&1 || { echo '[edrive] Go is required.' >&2; exit 1; }
command -v brew >/dev/null 2>&1 || { echo '[edrive] Homebrew is required for age/zstd installation.' >&2; exit 1; }

if ! command -v age >/dev/null 2>&1; then
  brew install age
fi

if ! command -v zstd >/dev/null 2>&1; then
  brew install zstd
fi

mkdir -p "$CONFIG_DIR/config"
if [ ! -f "$CONFIG_DIR/config.sh" ]; then
  cp "$ROOT/config/config.sh.example" "$CONFIG_DIR/config.sh"
  chmod 600 "$CONFIG_DIR/config.sh"
  printf '%s\n' '[edrive] created:' "$CONFIG_DIR/config.sh"
  printf '%s\n' '[edrive] review the paths before using edrive.'
fi

cd "$ROOT"
make darwin-arm64
mkdir -p "$CONFIG_DIR/bin"
cp "bin/edrive-darwin-arm64" "$CONFIG_DIR/bin/edrive"
chmod 700 "$CONFIG_DIR/bin/edrive"

mkdir -p "$HOME/.local/bin"
ln -sf "$CONFIG_DIR/bin/edrive" "$HOME/.local/bin/edrive"

printf '%s\n' '[edrive] installed:' "$CONFIG_DIR/bin/edrive"
printf '%s\n' '[edrive] run: edrive doctor'

printf '%s\n' '[edrive] available as: edrive'
printf '%s\n' "[edrive] command: $HOME/.local/bin/edrive"
