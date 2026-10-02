# edrive

`edrive` is a local-first developer identity toolkit. Version 0.1 intentionally keeps
Cryptomator as the daily live encrypted filesystem and adds an independent,
portable recovery path based on standard `age` encryption.

## Current architecture

```text
Mac
  -> Cryptomator / FUSE-T
  -> mounted vault
  -> edrive backup
  -> tar -> zstd -> age
  -> independent recovery snapshot

Google Drive remains the remote storage for the Cryptomator vault.

Recovery does not depend on Cryptomator, Google Drive, or edrive.
```

## What this version does

- `edrive doctor` checks the local setup.
- `edrive status` shows whether the Cryptomator mount is available.
- `edrive backup` creates a streaming `.tar.zst.age` snapshot.
- `edrive backups` lists snapshots.
- `edrive verify --identity ...` decrypts a snapshot and verifies every file hash.
- `edrive restore --identity ... --output ...` restores into an empty directory.
- `edrive identity generate` creates a post-quantum age identity.
- `edrive identity recipient` prints its public recipient.
- `edrive identity add` appends a public recipient to the configured recipients file.

## Deliberate constraints

- No custom cryptography.
- No remote edrive server.
- No plaintext secrets in the repo.
- Recovery identity is external to the normal working tree.
- The first release uses the official `age` and `zstd` executables instead of reimplementing
  those libraries. Once the workflow is proven, the Go age library can be embedded to
  reduce external dependencies without changing the recovery format.

## Setup

```sh
cp config/config.sh.example ~/Desktop/local-infra/edrive/config.sh
chmod 600 ~/Desktop/local-infra/edrive/config.sh
```

Edit only paths/policy in `config.sh`.

Install dependencies and build the Apple Silicon binary:

```sh
./bootstrap.sh
```

Then:

```sh
edrive doctor
edrive status
```

## Identity model

The intended long-term recipient set is small:

- Mac device identity
- mobile device identity
- offline recovery identity

A recovery snapshot can be encrypted to all configured public recipients. Losing one device
therefore does not imply rotating the entire vault. The current release only provides the
primitive for creating and registering age identities; OS Keychain/Keystore integration is
intentionally deferred.
