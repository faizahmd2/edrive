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
edrive pass <key>
edrive pass ls
edrive pass <key> <value>
edrive pass set <key>
edrive diff
edrive cloud add [provider]
edrive cloud remove
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

When the rclone remote named edrive-cloud does not exist, setup asks how to configure it:

~~~
1) Built-in rclone setup
2) edrive guided setup
~~~

Press Enter for the default rclone method.

The built-in rclone method is unchanged: edrive opens rclone's normal interactive configuration. The guided edrive method keeps rclone as the cloud engine but removes its provider questionnaire from the normal setup flow. It presents a small typed menu:

~~~
1) Google Drive
2) Amazon S3 / S3-compatible
e) Cloudflare R2
3) Backblaze B2
4) Dropbox
5) Microsoft OneDrive
~~~

Google Drive asks for an optional client ID and client secret. Press Enter for both to use rclone's shared client. Rclone's current Google Drive documentation says the shared client is being retired during 2026, so a personal client avoids depending on it. The guided flow then opens the real rclone configuration file in a terminal editor and automatically finishes OAuth authentication in the browser. urlrclone Google Drive configurationhttps://rclone.org/drive/

For S3, R2, and B2, the editor is used for provider credentials and endpoint/region details. Cloudflare R2 uses rclone's S3 backend with the Cloudflare provider and an endpoint based on the Cloudflare account ID. urlrclone S3 and Cloudflare R2 configurationhttps://rclone.org/s3/

Dropbox and OneDrive use the same editor-first flow followed by browser authentication. urlrclone Dropbox configurationhttps://rclone.org/dropbox/ urlrclone OneDrive configurationhttps://rclone.org/onedrive/

The guided editor method requires an editable rclone configuration file. If rclone's configuration file is encrypted, use the built-in rclone method instead. Rclone supports configuration-file encryption separately. urlrclone configuration encryptionhttps://rclone.org/docs/#configuration-encryption

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

The `pass` command is deliberately only a thin convention over files in the encrypted workspace. It is not a separate password database and it does not add another encryption layer.

The files live at:

~~~text
~/.edrive/workspace/pass/
├── insta
├── github
└── aws
~~~

Each key is a filename. The file contains its secret value.

Read a value:

~~~bash
edrive pass insta
~~~

List keys without printing values:

~~~bash
edrive pass ls
edrive pass list
~~~

Set a one-line value directly:

~~~bash
edrive pass insta "new-password"
~~~

Set or edit a multiline value:

~~~bash
edrive pass set certificate
~~~

The editor is a terminal editor selected in this order: `nvim`, `vim`, then the macOS-provided `vi`. `nano` is not used.

For both direct updates and editor updates, edrive unlocks the macOS Keychain before modifying the encrypted workspace. The edited file is written to a protected temporary file first and only replaces the pass entry after the editor exits successfully.

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

## Cloud management

The fixed cloud remote is always named `edrive-cloud`, and the encrypted vault remains at `edrive-cloud:edrive`.

Add a provider through the guided flow:

~~~bash
edrive cloud add
~~~

Or specify a provider directly:

~~~bash
edrive cloud add google
edrive cloud add s3
edrive cloud add r2
edrive cloud add b2
edrive cloud add dropbox
edrive cloud add onedrive
~~~

Remove the local cloud-provider configuration:

~~~bash
edrive cloud remove
~~~

Cloud removal deletes only the `edrive-cloud` rclone configuration. It does not delete the remote encrypted vault, cloud files, or the local encrypted vault. After adding another provider, run `edrive push` to publish the existing local vault to that provider.

Rclone's configuration file is the persistent configuration store. edrive prints its path after guided setup so the saved provider configuration is easy to locate.

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

## Help

Every supported command has command-specific help. These forms are supported:

~~~bash
edrive help
edrive help pass
edrive pass help
edrive pass --help
edrive cloud help
edrive cloud --help
~~~

The same pattern works for `setup`, `doctor`, `open`, `lock`, `push`, `pull`, `backup`, `decode`, `diff`, `remove`, `device`, `pwd`, and `version`.

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
