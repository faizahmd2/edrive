# edrive

An encrypted workspace for your Mac, synced to your own cloud.

- **Plain files while you work.** Use Finder, VS Code or anything else.
- **Encrypted everywhere else.** Built on [Cryptomator](https://cryptomator.org), so the same vault opens in the Cryptomator mobile apps.
- **Touch ID to unlock.** Locks again by itself.
- **Your cloud, via [rclone](https://rclone.org).** Google Drive, Dropbox, OneDrive, S3, R2 or B2.

## Install

Apple Silicon Mac with [Homebrew](https://brew.sh):

```bash
curl -fsSL https://raw.githubusercontent.com/faizahmd2/edrive/v0.6.0/install.sh | sh
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

## Mirroring sealed secrets

```bash
edrive mirror init     # once: makes the mirror key inside the vault, prints its public half
edrive mirror sync     # weekly or monthly: one Touch ID, copies the sealed secrets into the vault
```

An app can keep each secret sealed on this Mac as one [age](https://age-encryption.org) file, opened in everyday use with its own key (for example the Mac's Secure Enclave and Touch ID), and also encrypted to the mirror key. `edrive mirror sync` copies them one way into `mirror/` in the vault, so the Cryptomator apps can read them and a new Mac can get them back. The app on this Mac stays the only place a secret changes:

- edrive only reads the sealed folder (`~/f2/f2drive/sealed`, or `"mirror_from"` in `~/.edrive/config.json`); it never writes there.
- Nothing in the vault is deleted: replaced and removed values go to `mirror/.trash`.
- A copy edited by hand in the vault is kept in that trash, and the Mac's value comes back.
- If many secrets would vanish at once, nothing is copied unless you pass `--allow-deletes`.
- A damaged sealed file is skipped by name; every other secret still copies.

A sealed file is `sec_<id>.age` holding UTF-8 JSON: `{"v":1,"id":"sec_<id>","path":"bank/sbi/netbanking","value":"…"}`. It lands at `mirror/bank/sbi/netbanking.txt`. `edrive status` shows when the mirror last ran, and `edrive doctor` reminds you after 30 days.

## Reading the vault without Cryptomator

```bash
edrive decode ~/.edrive/vault ~/Desktop/vault-plain   # asks for the vault password
edrive verify                                         # proves it on your vault
```

`edrive decode` reads a Cryptomator vault (format 8, the default since Cryptomator 1.6) with nothing but its password. It needs no setup, no Cryptomator, no FUSE and no internet, so your files stay readable even if Cryptomator disappears. It works on a copy of the vault downloaded from your cloud too. It refuses damaged or tampered data instead of guessing.

`edrive verify` decodes your vault in memory with edrive's own decoder and compares every file with what Cryptomator shows. It writes nothing. `edrive doctor` reminds you to run it every 90 days.

There is no separate backup any more: the vault in your cloud is the copy, and `edrive decode` opens it. Backups made by older versions (`~/.edrive/backups/*.tar.zst.age`) still open with standard tools: `age -d file | zstd -d | tar x`.

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
~/.edrive/backups      old backups from before 0.6 (nothing writes here now)
```

## Troubleshooting

`edrive doctor` checks every part in about a second and never prompts. `edrive setup` repairs whatever it reports. The background auto-lock writes to `~/.edrive/runtime/guard.log`.

## Remove

`edrive remove` deletes edrive's local state. It deletes the local vault only after the cloud confirms it has everything. Cloud data, trash and old backups are left alone.

## Build from source

```bash
make build      # needs Go 1.26+ and Xcode command line tools (cgo)
make test
```
