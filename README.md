# edrive

Encrypted folder manager and orchestration.

**CLI-based macOS tool** built on top of **Cryptomator**.  
Syncs the encrypted vault to cloud storage with **rclone**.  
Creates independent recovery backups with **age** encryption.

## Install

macOS Apple Silicon:

```bash
curl -fsSL https://raw.githubusercontent.com/faizahmd2/edrive/v0.4.0/install.sh | sh
```

The installer downloads the matching GitHub Release binary, installs it under `~/.local/bin`, and adds that directory to your shell PATH.

## Setup

Run:

```bash
edrive setup
```

Setup will:

1. Install missing dependencies.
2. Configure cloud storage.
3. Create or recover the Cryptomator vault.
4. Configure the local edrive workspace and recovery device.

Check the whole installation at any time with:

```bash
edrive doctor
```

## Useful commands

```text
edrive open                       Unlock the encrypted workspace
edrive lock                       Lock the workspace
edrive push                       Upload the encrypted vault
edrive pull                       Download the encrypted vault
edrive diff                       Show local/cloud differences

edrive pass <key>                 Read a secret
edrive pass ls                    List secret keys
edrive pass <key> <value>         Set a one-line secret
edrive pass set <key>             Edit a secret in TextEdit
edrive pass migrate               Convert old pass files to .txt

edrive cloud add [provider]       Configure cloud storage
edrive cloud remove               Remove local cloud configuration

edrive backup                     Create an age-encrypted recovery backup
edrive decode <file> <key>        Recover a backup
edrive pwd                        Print the workspace path

edrive help                       Show help
edrive version                    Show version
edrive remove                     Remove local edrive state
```

## Backup

`edrive backup` creates an independent recovery copy of the workspace.

```text
workspace
   ↓
tar + zstd
   ↓
age encryption
   ↓
recovery backup
```

The live cloud copy is already encrypted by **Cryptomator** before rclone uploads it.  
The recovery backup is encrypted separately with **age**.

edrive creates a recovery key for these backups. Keep the recovery key somewhere safe, preferably separate from the backup itself. You can keep additional copies of the backup and key wherever you trust.

Together, this gives you multiple recovery paths without requiring the cloud provider to understand or decrypt your files.

## Remove / purge

```bash
edrive remove
```

This removes local edrive state.

It does **not** delete the remote encrypted vault, recovery backups, or Keychain identities.

During removal, edrive can also uninstall the dependencies it manages. In that sense, `edrive remove` is the full local purge operation.

## Release

The current release version is `0.4.0`.

Build the Apple Silicon release binary:

```bash
make darwin-arm64
```

The release asset should be named:

```text
edrive-darwin-arm64
```

Upload that binary to the GitHub Release for the matching tag.

Then users can install it with:

```bash
curl -fsSL https://raw.githubusercontent.com/faizahmd2/edrive/v0.4.0/install.sh | sh
```
