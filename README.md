# edrive

An encrypted workspace for your Mac, synced to your own cloud.

- **Plain files while you work.** Use Finder, VS Code or anything else.
- **Encrypted everywhere else.** Built on [Cryptomator](https://cryptomator.org), so the same vault opens in the Cryptomator mobile apps.
- **Touch ID to unlock.** Locks again by itself.
- **Your cloud, via [rclone](https://rclone.org).** Google Drive, Dropbox, OneDrive, S3, R2 or B2.

## Install

Apple Silicon Mac with [Homebrew](https://brew.sh):

```bash
curl -fsSL https://raw.githubusercontent.com/faizahmd2/edrive/v0.5.0/install.sh | sh
edrive setup
```

`setup` installs rclone and FUSE-T, connects your cloud, then downloads your vault (or helps you create one) and asks for the vault password once. It's safe to re-run at any time and only fixes what is missing.

## Everyday use

```text
edrive open                 open the workspace in Finder (Touch ID)
edrive lock                 lock it now
edrive pass github          print a secret
edrive pass github -c       copy it (clipboard clears after 30s)
edrive pass set github      save a secret (typed hidden; or pipe it in)
edrive pass ls              list secrets
edrive status               open/locked, sync state
```

**Opening.** `edrive open` unlocks with Touch ID (or your Mac password) and opens Finder. It never waits for the network. The workspace stays open until any of these happens:

- you close its Finder window
- the screen locks or the Mac sleeps
- 30 minutes pass (`edrive open --for 2h` to change)

Changes upload in the background after it locks.

**Secrets.** `edrive pass` unlocks, reads and locks again immediately. If the workspace is already open, it reads straight away with no prompt. Secrets are plain files in the workspace's `pass/` folder, so you can also edit them in Finder.

**Other editors.** `code "$(edrive pwd)"` unlocks for 30 minutes without the Finder rule.

## Sync

Everything except sync works offline. Sync runs by itself only in the background, right after an unlock (to bring in changes from your phone) and after a lock (to upload yours, if anything changed). Nothing is scheduled and nothing retries on its own. `edrive status` shows the last sync time from this Mac's own records and suggests `edrive sync` when changes are waiting or it has been over 24 hours. `edrive sync` (or `push` / `pull`) syncs by hand, and `edrive diff` previews it.

- Changes go both ways. If the same file changed on two devices, the newest copy wins.
- **Nothing is deleted.** Replaced or removed files are moved to a trash folder: `~/.edrive/trash` on the Mac and `.edrive-trash` inside the cloud vault.
- If a large part of the vault suddenly disappears on one side, edrive does not copy those removals to the other side. Use `edrive sync --allow-deletes` if the removal was intended.
- If you're offline, nothing fails. Changes wait until the next lock, unlock or `edrive sync`.

## Backup

```bash
edrive backup                    # writes ~/.edrive/backups/edrive-backup.tar.zst.age
edrive decode edrive-backup.tar.zst.age
```

A backup is one standalone, encrypted copy of the workspace, and each new one replaces the previous one. `edrive backup` creates a new passphrase and shows it **once**. Save it in your password manager; edrive doesn't store it anywhere. Use `--passphrase` to type your own instead.

Restoring needs no setup, no cloud and no Cryptomator. It works with `edrive decode`, or with standard tools:

```bash
age -d edrive-backup.tar.zst.age | zstd -d | tar x
```

## Security model

| Threat | Protection |
|---|---|
| Cloud provider or stolen cloud account | Only Cryptomator-encrypted data ever leaves the Mac. |
| Scripts and apps on your Mac (e.g. a malicious npm package) | The vault password is in a Keychain item that **only the edrive binary** can read. Other programs, including `security find-generic-password`, trigger a macOS password prompt. |
| Something running `edrive` itself | Every unlock needs Touch ID or your Mac password, and a script can't fake that. An unexpected prompt is your warning sign. |
| A modified edrive binary | Keychain recognises edrive by a fingerprint of the binary. A changed binary gets a Keychain prompt instead of silent access. |
| Forgetting to lock | Locks automatically when you close the Finder window, the screen locks, the Mac sleeps, or after 30 min. |
| Secrets in shell history or clipboard managers | `pass set` reads hidden input; `pass -c` marks the clipboard as concealed and clears it. |
| Tampered downloads | The Cryptomator CLI is pinned by SHA-256 and its code signature is checked. The installer checks edrive's SHA-256. |

**Limit:** while the workspace is open, any program running as your user can read it. That's true of every encrypted-folder tool. edrive keeps that window short and makes opening it a deliberate act.

**After updating edrive**, macOS asks once whether the new binary may use the vault password. Choose *Always Allow*. This is the same check that blocks a modified binary.

## Files

```text
~/.edrive/vault        encrypted vault (synced)
~/.edrive/workspace    where it is mounted while open
~/.edrive/trash        files replaced or removed by sync
~/.edrive/backups      standalone backups
```

## Troubleshooting

`edrive doctor` checks every part in about a second and never prompts. `edrive setup` repairs whatever it reports. The background auto-lock writes to `~/.edrive/runtime/guard.log`.

## Remove

`edrive remove` deletes edrive's local state. It deletes the local vault only after the cloud confirms it has everything. Cloud data, trash and backups are left alone.

## Build from source

```bash
make build      # needs Go 1.26+ and Xcode command line tools (cgo)
make test
```
