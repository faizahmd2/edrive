#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
INSTALL_ROOT="$HOME/.local/lib/edrive"
INSTALL_DIR="$HOME/.local/bin"
ZSHRC="$HOME/.zshrc"

printf '%s\n' '[edrive] bootstrapping edrive...'
command -v go >/dev/null 2>&1 || {
  echo '[edrive] Go is required.' >&2
  exit 1
}

cd "$ROOT"
make darwin-arm64

mkdir -p "$INSTALL_ROOT" "$INSTALL_DIR"
cp "$ROOT/bin/edrive-darwin-arm64" "$INSTALL_ROOT/edrive"
chmod 700 "$INSTALL_ROOT/edrive"
ln -sf "$INSTALL_ROOT/edrive" "$INSTALL_DIR/edrive"

touch "$ZSHRC"
if ! grep -Fq '# >>> edrive shell integration >>>' "$ZSHRC"; then
  cat >>"$ZSHRC" <<'EOF'

# >>> edrive shell integration >>>
edrive() {
  if [[ "$1" == "cd" ]]; then
    shift
    if (( $# > 0 )); then
      echo "edrive cd does not accept arguments" >&2
      return 2
    fi
    local dir
    dir=$("$HOME/.local/bin/edrive" cd) || return
    builtin cd -- "$dir"
    return
  fi
  "$HOME/.local/bin/edrive" "$@"
}
# <<< edrive shell integration <<<
EOF
fi

printf '%s\n' '[edrive] installed:' "$INSTALL_ROOT/edrive"
printf '%s\n' '[edrive] restart the terminal or run: source ~/.zshrc'
printf '%s\n' '[edrive] next: edrive setup'
