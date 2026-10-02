# edrive

edrive is a local-first CLI orchestrator for a personal encrypted developer data
environment.

It does not run a remote edrive service, proxy user data, manage a Google
account, or implement custom cryptography.

## Architecture

Daily data flow:

    Finder
      -> Cryptomator / FUSE-T
      -> mounted plaintext view
      -> local encrypted vault
      -> Google Drive sync

Recovery flow:

    mounted vault
      -> tar
      -> zstd
      -> age
      -> .tar.zst.age

Cryptomator protects the live vault. Google Drive provides storage/synchronization.
age provides an independent recovery format. edrive orchestrates the local
workflow.

Recovery does not require the edrive binary or Cryptomator to decrypt an
archive.

## Local layout

By default edrive keeps one data root under the user's home directory:

    ~/edrive/
    ├── google-drive-remote/
    │   └── edrive/       # Cryptomator encrypted vault
    ├── edrive/           # live mounted plaintext view
    └── recovery/         # local recovery snapshots

Machine configuration, runtime state, recipients, and the Mac identity live
under:

    ~/Library/Application Support/edrive/
    ├── config.sh
    ├── recipients.txt
    ├── identities/
    └── runtime/

The recovery identity is intentionally kept outside both locations.

## Setup

The intended flow on a new Mac is:

    ./bootstrap.sh
    edrive setup

The default data root is:

    ~/edrive

Setup asks for a different root only on the first run. Later runs reuse the
recorded root and continue from the current state.

Setup is state-aware. It writes the local configuration as progress is made, so
a failure halfway through does not require starting over.

### What setup does

1. Checks for required local dependencies.
2. If a dependency is missing, asks before installing it.
3. Creates only the local edrive directories it owns.
4. Opens Google Drive for desktop and Google Drive in the browser for the user
   to authenticate and configure.
5. Uses a dedicated local My Drive mirror directory under the edrive data root.
6. Finds or downloads the Cryptomator CLI.
7. Reuses or creates the Mac age identity.
8. Uses the existing offline recovery identity.
9. Maintains the local public recipient list.
10. Finds the Cryptomator vault and discovers its vault ID.
11. Writes the final machine-specific config.
12. Leaves the user at edrive doctor for final diagnostics.

edrive never receives the Google password or OAuth credentials.

For Google Drive, the user must configure My Drive mirroring themselves. The
expected local path is:

    <EDRIVE_DATA_ROOT>/google-drive-remote

edrive does not scan arbitrary Google Drive locations and does not change
Google Drive settings automatically.

For Cryptomator, the CLI is enough for normal edrive operation. The desktop
Cryptomator application is only needed for one-time creation or registration
of a new vault. Setup will tell the user the exact path and can open Cryptomator
when it is required.

## Commands

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

    edrive remove
    edrive purge

Operational command errors are intentionally concise and point to:

    edrive doctor

The doctor command is the place to inspect dependencies, paths, identities,
vault registration, Keychain state, and Google Drive availability.

## Identity model

The identity set is deliberately small:

- Mac identity
- offline recovery identity
- iPhone identity later

A private age identity stays on its device. Recovery snapshots are encrypted to
the configured public recipients.

## Recovery

Snapshots are streamed without creating a plaintext archive on disk:

    files
      -> tar
      -> zstd
      -> age
      -> recovery snapshot

The resulting .tar.zst.age file can be recovered with standard age, zstd, and
tar tooling.

## Cleanup

edrive remove removes edrive's local configuration and runtime state while
preserving the data root, Google Drive mirror, recovery snapshots, and
identities.

edrive purge is destructive for edrive-owned local state. It requires an
explicit PURGE confirmation, refuses to run while the vault is mounted, and
does not delete the Google Drive mirror or the external recovery identity.

System applications and the Google account are intentionally outside these
commands.

## Deliberate constraints

- No custom cryptography.
- No remote edrive server.
- No Google OAuth credentials handled by edrive.
- No plaintext secrets in the Git repository.
- No encrypted vault data in Git.
- Public recipients are machine-specific configuration, not project source.
- Recovery remains independent of the edrive binary.
