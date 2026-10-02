#!/bin/sh
set -eu

# End-to-end test using only temporary data. Run this on a Mac with age, zstd and edrive built.
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

mkdir -p "$TMP/mount/active/faiz/github" "$TMP/recovery" "$TMP/identity" "$TMP/restore"
printf '%s\n' 'hello from edrive smoke test' > "$TMP/mount/active/faiz/github/test.txt"
printf '%s\n' 'another file' > "$TMP/mount/active/notes.txt"

age-keygen -pq -o "$TMP/identity/recovery.txt" >/dev/null 2>&1
age-keygen -y "$TMP/identity/recovery.txt" > "$TMP/recipients.txt"

cat > "$TMP/config.sh" <<CFG
EDRIVE_HOME="$TMP"
EDRIVE_MOUNT="$TMP/mount"
EDRIVE_RECOVERY_DIR="$TMP/recovery"
EDRIVE_RECIPIENTS="$TMP/recipients.txt"
EDRIVE_SNAPSHOT_KEEP=5
CFG

EDRIVE_CONFIG="$TMP/config.sh" "$ROOT/bin/edrive" doctor
EDRIVE_CONFIG="$TMP/config.sh" "$ROOT/bin/edrive" backup
SNAPSHOT="$(ls -1t "$TMP/recovery"/*.tar.zst.age | head -1)"
EDRIVE_CONFIG="$TMP/config.sh" "$ROOT/bin/edrive" verify "$SNAPSHOT" --identity "$TMP/identity/recovery.txt"
EDRIVE_CONFIG="$TMP/config.sh" "$ROOT/bin/edrive" restore "$SNAPSHOT" --identity "$TMP/identity/recovery.txt" --output "$TMP/restore"

diff -ru "$TMP/mount" "$TMP/restore" --exclude='.edrive-manifest.json'
printf '%s\n' 'SMOKE TEST: PASS'
