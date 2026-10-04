package help

import "fmt"

var details = map[string]string{
	"setup": `edrive setup [--password]
  Install what is missing, connect your cloud, and download or create the vault.
  Safe to run again at any time: it only fixes what is broken.
  --password   re-enter the vault password (e.g. after changing it)`,

	"doctor": `edrive doctor
  Check that every part works. Fast, read-only, never prompts.`,

	"open": `edrive open [--for 1h]
  Unlock with Touch ID and open the workspace in Finder. It locks by itself
  when you close that Finder window, lock the screen, the Mac sleeps, or after
  30 minutes (or --for). Works offline; syncing happens in the background.`,

	"lock": `edrive lock
  Lock the workspace now. Changes upload in the background.`,

	"pwd": `edrive pwd
  Print the workspace path, unlocking it for 30 minutes if needed.
  Example: code "$(edrive pwd)"`,

	"status": `edrive status
  Show whether the workspace is open, when it locks, and the sync state.
  Reads only local records (no network), and suggests 'edrive sync' when needed.`,

	"pass": `edrive pass
  Secrets stored as plain files in the encrypted workspace (folder "pass").
  If the workspace is locked, edrive unlocks it with Touch ID, reads or writes,
  and locks it again straight away.

  edrive pass <key>           print a secret
  edrive pass <key> -c        copy it; the clipboard clears after 30s and
                              clipboard managers are asked not to record it
  edrive pass ls              list keys
  edrive pass set <key>       save a secret (typed hidden)
  pbpaste | edrive pass set <key>   save a multi-line secret`,

	"sync": `edrive sync [--allow-deletes]     (aliases: push, pull)
  Bring this Mac and the cloud together. edrive also syncs by itself in the
  background right after an unlock or a lock (never on a timer, never making
  you wait); everything else works offline. Changes flow both ways; if a file
  changed on two devices the newest wins. Nothing is ever deleted: replaced
  or removed files go to a trash folder (~/.edrive/trash and
  .edrive-trash in the cloud). If a lot of files disappear at once, edrive
  will not mirror that unless you pass --allow-deletes.`,

	"diff": `edrive diff
  Show what a sync would change, without changing anything.`,

	"backup": `edrive backup [-o file] [--passphrase]
  Write one encrypted copy of the workspace (replacing the previous one).
  A new passphrase is generated and shown once; it is not stored anywhere.
  --passphrase   type your own passphrase instead
  Restore with 'edrive decode', or with: age -d file | zstd -d | tar x`,

	"decode": `edrive decode <backup-file> [old-recovery-key-file]
  Restore a backup into a folder next to it. Works on any Mac with no setup.
  Backups from edrive before 0.5 need their recovery key file.`,

	"cloud": `edrive cloud add [provider]
edrive cloud remove
  Configure the rclone remote "edrive-cloud". Removing it never deletes cloud data.
  Providers: google, s3, r2, b2, dropbox, onedrive`,

	"remove": `edrive remove
  Remove edrive from this Mac. The local vault is deleted only after the cloud
  confirms it has everything. Cloud data, trash and backups are not touched.`,
}

var order = []string{"open", "lock", "pwd", "status", "pass", "sync", "diff", "backup", "decode", "setup", "doctor", "cloud", "remove"}

func Print(command string) {
	if d, ok := details[command]; ok {
		fmt.Println(d)
		return
	}
	switch command {
	case "push", "pull":
		fmt.Println(details["sync"])
	default:
		PrintAll()
	}
}

func PrintAll() {
	fmt.Print(`edrive - encrypted workspace for your Mac, synced to your cloud

Everyday:
  edrive open                 open the workspace in Finder (Touch ID)
  edrive lock                 lock it now
  edrive pass <key> [-c]      read a secret (or copy it)
  edrive pass set <key>       save a secret
  edrive pass ls              list secrets
  edrive status               open/locked and sync state

Sometimes:
  edrive pwd                  print the workspace path (unlocks for 30 min)
  edrive sync                 sync now (also: push, pull)
  edrive diff                 preview a sync
  edrive backup               standalone encrypted backup
  edrive decode <file>        restore a backup (no setup needed)

Setup:
  edrive setup                install, connect cloud, prepare vault
  edrive doctor               check everything
  edrive cloud add|remove     change cloud provider
  edrive remove               remove edrive from this Mac

More: edrive help <command>
`)
}
