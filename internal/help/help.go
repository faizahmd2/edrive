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

	"decode": `edrive decode <vault-folder> <new-folder>
  Read a Cryptomator vault with only its password, and write every file
  into <new-folder> as plain files. Needs no setup, no Cryptomator and no
  internet, so the vault stays readable even if Cryptomator disappears.
  Example: edrive decode ~/.edrive/vault ~/Desktop/vault-plain`,

	"mirror": `edrive mirror init
edrive mirror sync [--allow-deletes]      (also: edrive sync mirror)
  Copy a folder of sealed secrets (age files an app keeps on this Mac,
  ~/f2/f2drive/sealed unless config.json says "mirror_from") into the vault,
  one way: the app on this Mac is the only place a secret changes, and the
  vault keeps a readable copy for the Cryptomator apps and for a new Mac.
  init   make the mirror key inside the vault (once) and print its public
         half for the app. Never replaces a key that exists.
  sync   one Touch ID: open each sealed file (read only) and write its value
         to mirror/<path>.txt. Replaced and removed values go to
         mirror/.trash, never deleted. A copy edited by hand in the vault is
         kept there and the Mac's value comes back. If many secrets would
         vanish at once, nothing is copied unless you pass --allow-deletes.
         A damaged sealed file is skipped by name.`,

	"verify": `edrive verify
  Prove 'edrive decode' works on your vault: decodes it in memory with
  edrive's own decoder and compares every file with what Cryptomator shows.
  Writes nothing. 'edrive doctor' reminds you after 90 days.`,

	"cloud": `edrive cloud add [provider]
edrive cloud remove
  Configure the rclone remote "edrive-cloud". Removing it never deletes cloud data.
  Providers: google, s3, r2, b2, dropbox, onedrive`,

	"remove": `edrive remove
  Remove edrive from this Mac. The local vault is deleted only after the cloud
  confirms it has everything. Cloud data, trash and old backups are not touched.`,
}

var order = []string{"open", "lock", "pwd", "status", "pass", "sync", "diff", "mirror", "decode", "verify", "setup", "doctor", "cloud", "remove"}

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
  edrive mirror sync          copy sealed secrets into the vault
  edrive decode <vault> <dir> read the vault without Cryptomator
  edrive verify               prove that decode reads your vault

Setup:
  edrive setup                install, connect cloud, prepare vault
  edrive doctor               check everything
  edrive cloud add|remove     change cloud provider
  edrive remove               remove edrive from this Mac

More: edrive help <command>
`)
}
