#!/bin/sh
set -eu

VERSION="0.6.1"
REPO="faizahmd2/edrive"
INSTALL_ROOT="$HOME/.local/lib/edrive"
INSTALL_DIR="$HOME/.local/bin"
ASSET="edrive-darwin-arm64"

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  echo "[edrive] edrive needs an Apple Silicon Mac." >&2
  exit 1
fi

# GitHub release asset IDs for edrive v0.6.0.
# Update these IDs when publishing a new release.
ASSET_ID="726477120"
CHECKSUM_ASSET_ID="726477121"
API_BASE="https://api.github.com/repos/$REPO/releases/assets"

mkdir -p "$INSTALL_ROOT" "$INSTALL_DIR"
TMP="$INSTALL_ROOT/.edrive-download.$$"
trap 'rm -f "$TMP" "$TMP.sha256"' EXIT INT TERM

echo "[edrive] downloading edrive v$VERSION..."
curl -fsSL --retry 3 \
  -H 'Accept: application/octet-stream' \
  --output "$TMP" \
  "$API_BASE/$ASSET_ID"

curl -fsSL --retry 3 \
  -H 'Accept: application/octet-stream' \
  --output "$TMP.sha256" \
  "$API_BASE/$CHECKSUM_ASSET_ID"

expected="$(awk '{print $1}' "$TMP.sha256")"
actual="$(shasum -a 256 "$TMP" | awk '{print $1}')"
if [ "$expected" != "$actual" ]; then
  echo "[edrive] checksum mismatch; not installing." >&2
  exit 1
fi

chmod 755 "$TMP"
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
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    add_path_line "$HOME/.zprofile"
    [ -f "$HOME/.bash_profile" ] && add_path_line "$HOME/.bash_profile"
    echo "[edrive] added $INSTALL_DIR to PATH (open a new terminal, or: source ~/.zprofile)"
    ;;
esac

echo "[edrive] installed v$VERSION: $INSTALL_DIR/edrive"
echo "[edrive] next: edrive setup"
