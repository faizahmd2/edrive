#!/bin/sh
set -eu

VERSION="0.4.0"
REPO="faizahmd2/edrive"
INSTALL_ROOT="$HOME/.local/lib/edrive"
INSTALL_DIR="$HOME/.local/bin"
ASSET="edrive-darwin-arm64"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "[edrive] macOS is required." >&2
  exit 1
fi

case "$(uname -m)" in
  arm64)
    ;;
  *)
    echo "[edrive] this release supports Apple Silicon (arm64) macOS." >&2
    exit 1
    ;;
esac

URL="https://github.com/$REPO/releases/download/v$VERSION/$ASSET"

mkdir -p "$INSTALL_ROOT" "$INSTALL_DIR"
TMP="$INSTALL_ROOT/.edrive-download.$$"
trap 'rm -f "$TMP"' EXIT INT TERM

echo "[edrive] downloading edrive v$VERSION..."
curl -fL --retry 3 --output "$TMP" "$URL"

chmod 700 "$TMP"
mv "$TMP" "$INSTALL_ROOT/edrive"
ln -sf "$INSTALL_ROOT/edrive" "$INSTALL_DIR/edrive"

PATH_LINE='export PATH="$HOME/.local/bin:$PATH"'

add_path_line() {
  file="$1"
  if [ -f "$file" ] && grep -Fqx "$PATH_LINE" "$file"; then
    return
  fi
  printf '\n%s\n' "$PATH_LINE" >> "$file"
}

add_path_line "$HOME/.zprofile"
if [ -f "$HOME/.bash_profile" ]; then
  add_path_line "$HOME/.bash_profile"
fi

echo "[edrive] installed: $INSTALL_DIR/edrive"
echo
echo "[edrive] open a new terminal, or run:"
echo "  source ~/.zprofile"
echo
echo "[edrive] then run:"
echo "  edrive setup"
