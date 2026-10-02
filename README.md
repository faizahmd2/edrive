# edrive

edrive is a local-first encrypted workspace orchestrator for one person.

The user chooses one location for the working workspace. edrive handles the
rest locally and keeps its own control state separate from the development
repository.

## Model

```
Google Drive local storage
        |
        v
  edrive/                encrypted Cryptomator vault
        |
        v
Cryptomator + FUSE-T
        |
        v
~/edrive                 mounted plaintext workspace
```

The exact Google Drive local path is provider-owned. edrive discovers the local
My Drive location and creates/uses one folder named `edrive` inside it. The
user does not configure a Google Drive mirror path for edrive.

The mounted workspace is the only place where normal files are edited.

## Local state

```
~/.edrive/
├── config.json
├── recipients.txt
├── tools/
├── runtime/
└── tmp/
```

The development repository is separate from the installed runtime. The
installed binary is copied under `~/.local/lib/edrive`.

Private age identities are protected by the macOS Keychain. edrive keeps the
public recipient list as local configuration.

## Setup

```
./bootstrap.sh
source ~/.zshrc
edrive setup
```

Setup asks for the workspace location only on the first run. The default is
`~/edrive`. Later runs reuse the recorded location and continue from the
current state.

Setup:

- discovers the local Google Drive storage location;
- creates or reuses the `edrive` storage folder;
- reuses a tested Cryptomator CLI or installs the exact pinned release;
- creates or imports the device identity into the macOS Keychain;
- creates or imports the recovery identity into the macOS Keychain;
- writes the recipient list;
- opens Cryptomator for the one-time vault creation/registration step.

edrive never receives the Google password or the Cryptomator vault password.

The Cryptomator desktop application is only needed for first-time vault
creation/registration. Normal open/close operations use the CLI and the
Cryptomator Keychain credential.

## Daily commands

```
edrive open
edrive cd
edrive close

edrive backup
edrive restore

edrive status
edrive doctor
```

`edrive open` unlocks the encrypted workspace and opens the mounted folder in
Finder.

`edrive cd` unlocks the workspace and prints its path. The shell integration
installed by `bootstrap.sh` turns this into a real shell `cd`, so:

```
edrive cd
code .
```

works without edrive needing to know which editor the user prefers.

## Backup

There is one backup operation.

```
edrive backup
```

edrive:

1. requests access to the recovery identity in the macOS Keychain;
2. unlocks the workspace if needed, using the Cryptomator Keychain credential;
3. creates the encrypted snapshot as a stream;
4. verifies the encrypted snapshot before export;
5. opens a folder chooser at the previous backup destination;
6. saves an `edrive-backup-YYYYMMDD-HHMMSS` directory there;
7. opens the saved backup directory in Finder.

The backup directory contains:

```
backup.tar.zst.age
recovery-key.txt
README.txt
```

The recovery key is not displayed during setup or backup.

The encrypted snapshot is created as:

```
files
  -> tar
  -> zstd
  -> age
  -> backup.tar.zst.age
```

No plaintext archive is written to disk.

After the backup is saved, the temporary working copy under `~/.edrive/tmp`
is removed.

edrive remembers the last directory selected for backup and opens the folder
chooser there next time.

## Recovery key

The recovery key is the private age identity. It is not the Cryptomator
password.

Cryptomator protects the live workspace.

The age recovery identity protects the independent backup archive.

Each backup is encrypted to both:

```
device recipient
recovery recipient
```

The recovery identity itself is protected by the macOS Keychain during normal
operation. When a backup is exported, edrive copies that recovery identity into
`recovery-key.txt` in the user-selected backup directory so the encrypted
archive can be recovered even if the Mac is lost.

Keep the backup directory secure. Anyone who gets the recovery key can decrypt
the backups encrypted to its corresponding recipient.

## Restore

```
edrive restore
```

Select a backup directory. edrive uses the recovery identity already in the
Keychain when available. On a new Mac, it can import `recovery-key.txt` from
the selected backup directory into the Keychain.

Then select an empty restore directory. edrive decrypts, decompresses, verifies
file hashes, and restores the files.

## Failure policy

Normal commands do not investigate or repair the environment.

On error they report the problem and point to:

```
edrive doctor
```

`edrive doctor` is the diagnostic authority for dependencies, workspace,
Google Drive discovery, Cryptomator registration, tool versions, recipients,
and Keychain identities.

## Version policy

edrive does not use upstream `latest` release discovery.

The current pinned toolchain is:

```
age                 1.3.2
zstd                1.5.7
Cryptomator CLI     0.6.2
FUSE-T              provider-managed
Google Drive        provider-managed
Cryptomator desktop provider-managed
```

The Cryptomator CLI download URL is pinned to the exact 0.6.2 macOS asset.
The age macOS ARM64 asset is pinned and SHA-256 verified.

A dependency version changes only as part of an intentional edrive release.

edrive does not upgrade provider applications behind the user's back.

## Design constraints

- No remote edrive service.
- No custom cryptography.
- No Google OAuth credentials handled by edrive.
- No second local synchronization engine.
- No permanent backup copies in edrive's control directory.
- No plaintext archive intermediate.
- Private identities stay on-device in Keychain.
- Temporary secret material is removed after use.
- Installed runtime state is separate from the development repository.
- Resource ownership and cleanup are explicit for files, processes, pipes,
  temporary directories, and mounts.
