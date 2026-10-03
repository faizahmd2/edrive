# edrive

Encrypted folder manager and orchestration.

**CLI-based macOS tool** built on top of **Cryptomator**.  
Syncs to cloud storage with **rclone**.  
Creates independent recovery backups with **age** encryption.

## Install

macOS Apple Silicon:

```bash
curl -fsSL https://raw.githubusercontent.com/faizahmd2/edrive/v0.1.0/install.sh | sh
```

## Setup

Run:

```bash
edrive setup
```

Setup will:

1. Install missing dependencies.
2. Configure cloud storage.
3. Create or recover the Cryptomator vault.
4. Configure the local workspace and recovery device.

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
edrive pwd                        Print the workspace path

edrive pass <key>                 Read a secret
edrive pass ls                    List secret keys
edrive pass <key> <value>         Set a one-line secret
edrive pass set <key>             Edit a secret in TextEdit

edrive cloud add [provider]       Configure cloud storage
edrive cloud remove               Remove local cloud configuration

edrive backup                     Create an age-encrypted recovery backup
edrive decode <file> <key>        Recover a backup

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
