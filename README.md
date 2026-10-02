# edrive

edrive is a local-first encrypted workspace for one person.

The user chooses the working folder once. edrive keeps its control state in
`~/.edrive`, while Google Drive and Cryptomator remain external applications.

## Commands

```
edrive setup
edrive doctor

edrive open
edrive cd
edrive unlock
edrive lock

edrive backup
edrive decode <encrypted-file> <recovery-key>

edrive device add <label>
edrive device list
edrive device remove <label>

edrive help
edrive version
```

There is no `status`, `verify`, `backups`, `restore`, or separate
snapshot command.

## Layout

The user's workspace is the mounted plaintext view:

```
~/edrive
```

edrive's private control state is separate:

```
~/.edrive/
├── config.json
├── devices.json
├── tools/
├── runtime/
└── tmp/
```

The encrypted Cryptomator vault stays inside the user's existing Google Drive
storage. The user never configures its local Google Drive path inside edrive.

Google Drive is the sync/storage application. Cryptomator is the live
encryption layer and FUSE-T provides the filesystem mount. edrive only
orchestrates these pieces.

## Setup

```
./bootstrap.sh
source ~/.zshrc
edrive setup
```

Setup asks for the workspace location once. The default is `~/edrive`.

After that, setup automatically:

- discovers the local Google Drive storage;
- creates or reuses the edrive vault location;
- checks the fixed edrive toolchain;
- creates the default device identity `mac-1`;
- opens Cryptomator for the one-time vault creation/registration step.

No recovery key is displayed during setup.

The recovery key is created lazily on the first backup, not during setup.

## Open and lock

`edrive open` unlocks the Cryptomator vault and opens the mounted workspace
in Finder.

`edrive cd` unlocks the workspace and prints its path. The shell integration
installed by `bootstrap.sh` turns that into a real shell `cd`.

```
edrive cd
code .
```

`edrive unlock` and `edrive lock` are the explicit lifecycle commands.

## Backup

There is one backup operation:

```
edrive backup
```

The flow is:

1. edrive accesses the recovery key in macOS Keychain. On the first backup,
   it creates that key once and stores it in Keychain.
2. edrive unlocks the workspace when necessary.
3. edrive builds the encrypted backup.
4. A Finder folder chooser opens at the last backup directory.
5. The encrypted backup is saved directly in the selected folder.
6. The selected folder is opened in Finder.

The backup filename is:

```
edrive-backup-YYYYMMDD-HHMMSS.tar.zst.age
```

edrive remembers the last backup directory.

The recovery key is written as:

```
edrive-recovery-key.txt
```

only when that file is missing from the selected backup directory. The same
recovery key is reused for later backups. edrive never prints the key.

If a selected directory already contains a recovery key, edrive checks that it
belongs to the same recovery identity before using the directory.

No backup archive or private recovery key is kept in `~/.edrive/tmp` after
the command finishes. Temporary sensitive files are removed.

## How the recovery key works

There are two different concepts:

**Cryptomator password**

Protects the live workspace. It stays under Cryptomator's own Keychain and is
used by the Cryptomator CLI during unlock.

**edrive recovery key**

This is an age private identity. It is generated once and stored in the
macOS Keychain under edrive's control. Its matching public recipient is added
to every future backup.

A backup is encrypted for all registered device identities plus the recovery
recipient. That means:

- a registered device can decrypt the backup using its own identity;
- the recovery key can decrypt the backup independently of the Mac and
  independently of Cryptomator.

The recovery key is not the Cryptomator password.

## Decode anywhere

The decode command is intentionally independent of edrive setup:

```
edrive decode /path/to/edrive-backup-20261002-193000.tar.zst.age \
  /path/to/edrive-recovery-key.txt
```

It only needs the `age` command installed.

The decrypted sibling file is created automatically:

```
edrive-backup-20261002-193000.tar.zst
```

No Google Drive, Cryptomator, FUSE-T, edrive configuration, or device registry
is required for this command.

The decoded file is the decrypted archive layer. Normal Unix `tar`/zstd tools
can be used to inspect or extract it separately.

## Devices

Device identities are named, so the user does not need to think in terms of
cryptographic recipient strings.

```
edrive device add mac-1
edrive device add phone-1
edrive device list
edrive device remove phone-1
```

Private identity material is stored in the platform secure store. Only the
label and public recipient metadata are stored in `~/.edrive/devices.json`.

Removing a device stops future backups from including that device. The
recovery identity remains independent.

## Failure model

Normal commands stay small. They report the immediate error and point to
`edrive doctor`.

`edrive doctor` is the diagnostic authority for:

- edrive configuration;
- workspace;
- Google Drive discovery;
- Cryptomator registration;
- age and zstd versions;
- Cryptomator CLI;
- FUSE-T;
- device identities;
- recovery key state.

## Version policy

The edrive-controlled versions are pinned in source and are changed only as
part of an intentional edrive release.

Current values:

```
age                1.3.2
zstd               1.5.7
Cryptomator CLI    0.6.2
```

edrive does not query an upstream `latest` release during setup.

## Constraints

- No remote edrive service.
- No custom cryptography.
- No second sync engine.
- No Google OAuth handling inside edrive.
- No persistent plaintext backup archive.
- No backup copies kept by edrive after export.
- Private identity material is kept in the platform secure store.
- Temporary private material is removed after use.
- Runtime behavior does not depend on the development repository layout.
