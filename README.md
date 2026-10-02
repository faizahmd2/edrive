# edrive

edrive is a local-first encrypted workspace for one person.

edrive is the control/orchestration layer. Google Drive stores and syncs the encrypted vault. Cryptomator owns the live encryption and password. FUSE-T provides the mounted filesystem. edrive does not replace any of those tools.

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

There is no separate status, verify, backups, restore, or snapshot workflow.

## Setup

Every `edrive setup` is a fresh setup.

If configuration already exists, edrive first asks whether to rebuild it. The prompt tells the user to run `edrive doctor` first when they want to inspect the existing installation.

Fresh setup does not delete Google Drive data, Cryptomator vaults, or Keychain identities. It rebuilds edrive's local configuration and device registry.

The setup flow is:

1. Check all required local applications and pinned tools together. Missing pieces are listed together with the next action.
2. Choose the folder where the decrypted workspace will be mounted. edrive does not silently reuse the old workspace.
3. Discover Google Drive's local My Drive folder. A mirrored folder is valid and a streamed folder is valid. If it is not discoverable, edrive first explains what to do, opens Google Drive, waits for confirmation, and then checks again. If necessary, it lets the user choose the local My Drive folder directly.
4. Resolve the edrive Cryptomator vault. An existing Cryptomator vault is never silently assumed to belong to edrive. edrive uses its own non-secret marker inside its encrypted vault to recognize an edrive-managed vault.

Before opening Cryptomator, setup explains the exact action the user needs to perform. For a new vault it says which vault name and storage location to choose. For an existing unregistered vault it explains how to add that vault. After the user confirms, edrive checks the resulting files and Cryptomator registration.

## Isolation

edrive does not own the Google Drive application or all of its local storage.

edrive does not own the Cryptomator application or every Cryptomator vault on the machine.

Only the selected Google Drive local root and the selected edrive vault are recorded in edrive configuration.

An unrelated Cryptomator vault is left untouched. A foreign vault at the default `edrive` location causes setup to ask before adoption. If the user declines, setup asks for another location instead of overwriting the existing vault.

The encrypted vault contains `.edrive-vault.json` as an ownership marker. The marker is itself inside the encrypted vault, so it is not part of the user's plaintext workspace.

## Live workspace

The configured workspace is a plaintext mount point, for example:

```
~/edrive
```

`edrive open`, `edrive cd`, and `edrive unlock` ensure that the selected edrive vault is available through Cryptomator before exposing the workspace.

`edrive lock` only terminates the Cryptomator process that edrive started and tracks. It does not terminate an unrelated Cryptomator process.

## Doctor

`edrive doctor` is the diagnostic authority for the entire local lifecycle.

It checks the configuration file, workspace, Google Drive application and local files, edrive vault ownership and completeness, Cryptomator registration, pinned tools, FUSE-T, device identities, and recovery-key state.

Doctor continues checking independent parts even when one part is broken. It reports the disease and a concrete recovery direction, such as restoring a deleted CLI, making Google Drive files available, selecting a replacement workspace, or re-registering an edrive vault.

If the configuration file itself is malformed or cannot be opened, doctor still reports that as a configuration problem instead of failing before the diagnostic starts.

## Backup

```
edrive backup
```

Backup first accesses the edrive recovery item in macOS Keychain. On the first backup, the recovery identity is generated once and stored there. On later backups the same identity is reused.

Backup then ensures the live workspace is unlocked before archiving it.

The user chooses the destination folder with a Finder dialog. The last backup directory is remembered and used as the next dialog's starting location.

The selected directory receives:

```
edrive-backup-YYYYMMDD-HHMMSS.tar.zst.age
edrive-recovery-key.txt   # only when the key file is missing
```

The recovery-key file is never printed by edrive. An existing recovery-key file is validated against the current recovery identity and is not overwritten.

Backup streams directly into the final destination through a temporary partial file. Failed partial output is removed.

After a successful backup, the destination folder is opened in Finder.

## Recovery

The Cryptomator password and edrive recovery key are different things.

The Cryptomator password unlocks the live vault and remains under Cryptomator's Keychain management.

The edrive recovery key is independent backup-decryption material. It is stored in macOS Keychain and its matching recipient is included in every future backup.

That separation means backup recovery does not require the live Cryptomator mount to be available.

## Decode anywhere

```
edrive decode /path/to/backup.tar.zst.age /path/to/edrive-recovery-key.txt
```

`edrive decode` is intentionally independent of edrive setup. It only requires the `age` command and the two files supplied by the user.

It removes the `.age` layer and writes the decrypted sibling next to the encrypted input:

```
backup.tar.zst.age  ->  backup.tar.zst
```

It does not automatically extract the tar/zstd archive.

## Devices

Device labels are the user-facing identity model:

```
edrive device add phone-1
edrive device list
edrive device remove phone-1
```

Private device identity material stays in the platform secure store. Only public recipient metadata and the label are kept in the local registry.

Setup creates the default local device `mac-1` when one is not already registered.

## State

edrive keeps private control state under:

```
~/.edrive/
├── config.json
├── devices.json
├── tools/
├── runtime/
└── tmp/
```

No encrypted backup archive or private recovery key is retained there after a backup.

## Pinned dependencies

```
age                1.3.2
zstd               1.5.7
Cryptomator CLI    0.6.2
```

edrive does not fetch a moving `latest` dependency during setup.

## Design constraints

- No remote edrive service.
- No custom cryptography.
- No second sync engine.
- No Google OAuth handling inside edrive.
- No silent adoption of unrelated Google Drive or Cryptomator resources.
- No persistent plaintext backup archive.
- External applications keep ownership of their own credentials and infrastructure.
- Temporary sensitive files have explicit cleanup.
- Runtime behavior does not depend on the development repository layout.
