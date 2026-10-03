# edrive

edrive is a local-first encrypted workspace for one person.

The core rule is:

**edrive is not a realtime cloud-sync application.**

Your working files live locally. Cryptomator encrypts them. rclone moves the already-encrypted Cryptomator vault to and from the cloud only when you explicitly run push or pull.

## Architecture

~~~
Cloud provider
     │
rclone remote
     │
edrive-cloud:edrive
     │
edrive pull / push
     │
~/.edrive/vault/
Cryptomator encrypted vault
     │
Cryptomator CLI
     │
~/.edrive/workspace/
plaintext files
~~~

There is no Google Drive Desktop dependency and no local Google Drive mirror.

The same Cryptomator vault can also be opened from the Cryptomator mobile apps. Current Cryptomator documentation lists Google Drive as a native cloud service on Android and iOS and documents adding an existing vault directly.

rclone's normal crypt backend is deliberately not used. It creates an rclone-specific encrypted format, not a Cryptomator vault. The cloud copy therefore remains a normal Cryptomator vault that Cryptomator mobile can understand.

## Commands

~~~
edrive setup
edrive doctor

edrive open
edrive lock

edrive push
edrive pull

edrive backup
edrive pass <name> [value]
edrive diff
edrive decode <encrypted-file> <recovery-key>

edrive remove

edrive device add <label>
edrive device list
edrive device remove <label>

edrive help
edrive version
~~~

There is no cd command, shell integration, Google Drive folder chooser, or realtime sync daemon.

## Fixed local layout

Everything owned by edrive lives under:

~~~
~/.edrive/
├── config.json
├── workspace/       # plaintext mount point
│   └── pass/        # one secret per file; names are the keys
├── vault/           # local Cryptomator encrypted vault
├── backups/         # independent recovery backups
├── devices.json
├── runtime/
├── tools/
└── tmp/
~~~

The workspace and vault locations are fixed. Setup does not ask the user to choose folders.

## Setup

Run:

~~~
edrive setup
~~~

Setup installs missing dependencies through Homebrew on macOS:

~~~
rclone
age
zstd
FUSE-T
Cryptomator
Cryptomator CLI 0.6.2
~~~

When the rclone remote named edrive-cloud does not exist, setup starts rclone config inside the setup flow. Create the remote with that exact name. For Google Drive, choose Google Drive and complete the browser authentication. rclone's official Drive setup is browser based through rclone config.

After login, setup uses the fixed remote path:

~~~
edrive-cloud:edrive
~~~

If that remote already contains a Cryptomator vault, edrive pulls it into the fixed local vault.

If no remote vault exists, setup opens Cryptomator once so the user can create:

~~~
~/.edrive/vault
~~~

That is the only normal GUI operation required for vault creation/registration. Setup then verifies the vault is registered and that its password is stored in macOS Keychain.

There are no Google Drive Desktop checks and no folder-selection dialogs.

Cryptomator itself is not a sync tool. Its desktop documentation expects the encrypted vault to be synchronized by another cloud-sync tool. edrive uses rclone for exactly that role.

## Mac workflow

Open the local vault:

~~~
edrive open
~~~

The plaintext workspace is:

~~~
~/.edrive/workspace
~~~

Work normally.

When finished:

~~~
edrive lock
edrive push
~~~

Push synchronizes only the encrypted Cryptomator vault to:

~~~
edrive-cloud:edrive
~~~

To bring changes from another device back:

~~~
edrive pull
edrive open
~~~

Both push and pull require the workspace to be locked.

This is intentional manual synchronization. There is no background sync process.

## Phone workflow

The phone does not need edrive.

For Google Drive, connect the Google Drive account in Cryptomator Mobile and add the existing edrive vault. Cryptomator documents direct Google Drive access and adding an existing vault on mobile.

A typical monthly phone session is:

~~~
Phone:
  Cryptomator → Google Drive → edrive vault → edit → lock

Later on Mac:
  edrive pull
  edrive open
~~~

Do not publish from a stale Mac vault after changing the vault on the phone. Pull first so the local encrypted vault contains the latest remote state.

edrive intentionally uses a simple single-writer workflow rather than implementing a realtime conflict-resolution engine.

## Push and pull semantics

Push treats the local encrypted vault as authoritative for that operation.

Pull treats the remote encrypted vault as authoritative for that operation.

Because these are explicit operations, edrive can keep the state model simple:

~~~
open
work
lock
push

or

pull
open
work
~~~

Do not run push or pull while the Cryptomator mount is active.

## Pass

The `pass` command is deliberately only a thin convention over files in the encrypted workspace. It is not a separate password database and it does not use another encryption layer.

The files live at:

~~~text
~/.edrive/workspace/pass/
├── insta
├── github
└── aws
~~~

Each file contains one single-line secret value.

Read a value:

~~~bash
edrive pass insta
~~~

Set a value:

~~~bash
edrive pass insta "new-password"
~~~

Reading requires the workspace to be unlocked. Setting first unlocks the macOS Keychain so that the existing Cryptomator Keychain credential can be used without an unnecessary second Keychain-unlock prompt, then writes the file with mode `0600`.

The pass files are protected by Cryptomator together with the rest of the workspace. They are not separately stored in `config.json` or in edrive's own Keychain namespace.

## Diff

`edrive diff` is a read-only comparison between the local encrypted Cryptomator vault and the configured remote vault.

~~~bash
edrive diff
~~~

It compares:

~~~text
~/.edrive/vault/
        vs
edrive-cloud:edrive
~~~

The comparison uses `rclone check --checksum --combined` and reports only differences:

~~~text
+ path    present locally only
- path    present remotely only
* path    present on both sides but different
! path    comparison error
~~~

Unchanged files are omitted. The command does not create the remote directory, reconnect the rclone account, upload, download, or modify either side. Run it with the workspace locked so Cryptomator is not changing the local encrypted vault while it is being inspected.

## Recovery backup

The live cloud vault and recovery backup are separate.

Live cloud data:

~~~
plaintext
   ↓
Cryptomator
   ↓
encrypted vault
   ↓
rclone
   ↓
cloud
~~~

Independent recovery export:

~~~
plaintext workspace
   ↓
tar
   ↓
zstd
   ↓
age
   ↓
~/.edrive/backups/*.tar.zst.age
~~~

age and zstd are only used for this independent recovery path.

The recovery identity is stored in macOS Keychain. The matching recovery key file is written once beside the backups.

Decode remains independent:

~~~
edrive decode backup.tar.zst.age edrive-recovery-key.txt
~~~

It decrypts, decompresses, and extracts into a new sibling folder without requiring setup, rclone, Google Drive, or Cryptomator.

## Remove

Run:

~~~
edrive remove
~~~

Removal removes the local workspace, local encrypted vault, configuration, runtime state, and device registry.

It does not delete:

- the remote encrypted vault
- local recovery backups
- Keychain identities

Dependencies are separate from edrive. Removal asks whether to uninstall:

~~~
age
zstd
rclone
FUSE-T
Cryptomator CLI
~~~

Cryptomator Desktop is deliberately not removed automatically. When you no longer need it, uninstall it separately:

~~~
brew uninstall --cask cryptomator
~~~

## Dependencies and packaging direction

The source bootstrap builds edrive itself. Runtime setup installs missing tools when they are absent.

The intended distribution is a native package for each platform so that the end user eventually gets the same dependency experience from:

~~~
brew install edrive
~~~

or an appropriate Linux package with its declared dependencies.

The current source implementation is end-to-end for macOS. Linux packaging/support should be added only when the Linux Keychain, FUSE, Cryptomator CLI packaging, and service-management pieces are implemented together.

Cryptomator CLI itself is distributed separately from the desktop application and uses a third-party filesystem integration such as FUSE-T on macOS. The pinned CLI version in edrive is 0.6.2.

## Design constraints

- No Google Drive Desktop dependency.
- No realtime sync daemon.
- No local Google Drive mirror.
- No Google Drive path discovery.
- No user-selected workspace path.
- rclone owns cloud login and transport.
- Cryptomator owns live encryption and vault passwords.
- edrive owns orchestration and lifecycle.
- No custom cryptography.
- No remote edrive service.
- No second cloud-sync engine.
- age and zstd are only for independent recovery backups.
- Setup never deletes remote vault data.
- Remove never deletes remote vault data, recovery backups, or Keychain identities.


## Open and Keychain authentication

The normal `edrive open` path reads the existing Cryptomator vault password from the macOS Keychain once and supplies it to Cryptomator CLI through stdin. edrive does not intentionally prompt four times. The unlock flow avoids a separate credential-existence lookup immediately before retrieving the same password, reducing duplicate Keychain access.
