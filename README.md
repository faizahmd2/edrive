# edrive

`edrive` is a local-first developer identity toolkit. The live data store is a
Cryptomator vault backed by a local Google Drive filesystem, while recovery is
kept independent through portable `age`-encrypted snapshots.

## Architecture

```text
Daily use:

Finder
  -> Cryptomator / FUSE-T
  -> mounted vault
  -> Google Drive sync

Recovery:

mounted vault
  -> tar
  -> zstd
  -> age
  -> independent recovery snapshot
```

Cryptomator protects the live vault. Google Drive is only the storage/sync
layer. `age` is a separate recovery path. `edrive` orchestrates these pieces.

Recovery does not require Cryptomator or `edrive` to decrypt the archive.

## Commands

```sh
edrive setup
edrive doctor
edrive status
edrive unlock
edrive lock

edrive backup
edrive backups
edrive verify [snapshot] --identity PATH
edrive restore [snapshot] --identity PATH --output DIR

edrive identity generate --output PATH
edrive identity recipient PATH
edrive identity add PATH --to RECIPIENTS_FILE
```

## Setup

The normal installation flow is:

```sh
./bootstrap.sh
edrive setup
```

`edrive setup` is macOS-aware and prepares the local environment. It can install
the required Homebrew packages/casks, detect the local Google Drive location,
locate the official Cryptomator CLI, create the local edrive
directories, reuse/validate the Mac age identity, register known public
recipients, and write the machine-specific config.

Cryptomator vault creation remains a Cryptomator operation. On a fresh machine,
setup will open Cryptomator and tell you the exact vault path to create or add.
After that, run `edrive setup` again and it will discover the vault ID from
Cryptomator's local settings.

The generated config contains absolute machine-specific paths and is ignored by
Git. The real recipients file is also local-only.

## Identity model

For now the identity set is deliberately small:

- Mac identity
- offline recovery identity
- iPhone identity later

Private age identities stay on their respective devices. Only public recipients
are used by `edrive` to encrypt recovery snapshots.

## Recovery

Backups are streamed without creating a plaintext intermediate archive:

```text
files -> tar -> zstd -> age -> .tar.zst.age
```

A recovery snapshot can therefore be decrypted with standard age tooling without
depending on the `edrive` binary.

## Deliberate constraints

- No custom cryptography.
- No remote edrive server.
- No plaintext secrets in the Git repository.
- No encrypted vault data in Git.
- Recovery identity remains outside the normal working tree.
- The first recovery implementation uses the standard `age` and `zstd`
  executables rather than reimplementing their formats.
