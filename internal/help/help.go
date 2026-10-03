package help

import "fmt"

func Print(command string) {
	if command == "" {
		PrintAll()
		return
	}

	switch command {
	case "setup":
		fmt.Println("edrive setup")
		fmt.Println("  Install missing dependencies, configure the cloud remote, and initialize or recover the local Cryptomator vault.")
	case "doctor":
		fmt.Println("edrive doctor")
		fmt.Println("  Check the local edrive installation, Cryptomator registration, Keychain state, dependencies, devices, and cloud access.")
	case "open":
		fmt.Println("edrive open")
		fmt.Println("  Unlock the local Cryptomator vault and open ~/.edrive/workspace.")
	case "lock":
		fmt.Println("edrive lock")
		fmt.Println("  Close the Cryptomator mount. Run this before push, pull, diff, or other encrypted-vault operations.")
	case "push":
		fmt.Println("edrive push")
		fmt.Println("  Synchronize the local encrypted Cryptomator vault to edrive-cloud:edrive.")
	case "pull":
		fmt.Println("edrive pull")
		fmt.Println("  Synchronize the remote encrypted Cryptomator vault from edrive-cloud:edrive to the local vault.")
	case "pass":
		fmt.Println("edrive pass")
		fmt.Println("  Store small or multiline secrets as plain files inside the encrypted workspace.")
		fmt.Println()
		fmt.Println("  edrive pass <key>             Read one value.")
		fmt.Println("  edrive pass ls                List all keys.")
		fmt.Println("  edrive pass list              List all keys.")
		fmt.Println("  edrive pass <key> <value>     Replace with a one-line value.")
		fmt.Println("  edrive pass set <key>         Open the value in TextEdit on macOS for multiline content.")
		fmt.Println("  edrive pass migrate           Rename old extensionless entries to .txt for phone preview.")
	case "diff":
		fmt.Println("edrive diff")
		fmt.Println("  Read-only comparison of the local encrypted vault and edrive-cloud:edrive.")
	case "cloud":
		fmt.Println("edrive cloud")
		fmt.Println("  Manage the fixed rclone remote used by edrive.")
		fmt.Println()
		fmt.Println("  edrive cloud add              Guided provider setup.")
		fmt.Println("  edrive cloud add <provider>   Guided setup for a provider.")
		fmt.Println("  edrive cloud remove           Remove the local cloud-provider configuration without deleting cloud data.")
		fmt.Println()
		fmt.Println("  Guided providers:")
		fmt.Println("    1) Google Drive")
		fmt.Println("    2) Amazon S3 / S3-compatible")
		fmt.Println("    e) Cloudflare R2")
		fmt.Println("    3) Backblaze B2")
		fmt.Println("    4) Dropbox")
		fmt.Println("    5) Microsoft OneDrive")
	case "backup":
		fmt.Println("edrive backup")
		fmt.Println("  Create an independent encrypted recovery archive from the plaintext workspace.")
	case "decode":
		fmt.Println("edrive decode <encrypted-file> <recovery-key>")
		fmt.Println("  Decrypt, decompress, and extract an edrive recovery backup without setup, cloud access, or Cryptomator.")
	case "remove":
		fmt.Println("edrive remove")
		fmt.Println("  Remove edrive's local state. Remote encrypted vaults, recovery backups, and Keychain identities are left untouched.")
	case "device":
		fmt.Println("edrive device")
		fmt.Println("  Manage age device identities used for recovery backups.")
		fmt.Println()
		fmt.Println("  edrive device add <label>")
		fmt.Println("  edrive device list")
		fmt.Println("  edrive device remove <label>")
	case "pwd":
		fmt.Println("edrive pwd")
		fmt.Println("  Unlock the workspace if needed and print its fixed local path.")
	case "version":
		fmt.Println("edrive version")
		fmt.Println("  Print the installed edrive version.")
	case "help":
		PrintAll()
	default:
		fmt.Printf("No detailed help for %q.\n\n", command)
		PrintAll()
	}
}

func PrintAll() {
	fmt.Println("edrive - local encrypted workspace with explicit cloud sync")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  edrive setup")
	fmt.Println("  edrive doctor")
	fmt.Println("  edrive open")
	fmt.Println("  edrive lock")
	fmt.Println("  edrive push")
	fmt.Println("  edrive pull")
	fmt.Println("  edrive pass <key>")
	fmt.Println("  edrive pass ls")
	fmt.Println("  edrive pass list")
	fmt.Println("  edrive pass <key> <value>")
	fmt.Println("  edrive pass set <key>")
	fmt.Println("  edrive pass migrate")
	fmt.Println("  edrive diff")
	fmt.Println("  edrive cloud add [provider]")
	fmt.Println("  edrive cloud remove")
	fmt.Println("  edrive backup")
	fmt.Println("  edrive decode <encrypted-file> <recovery-key>")
	fmt.Println("  edrive remove")
	fmt.Println("  edrive device add <label>")
	fmt.Println("  edrive device list")
	fmt.Println("  edrive device remove <label>")
	fmt.Println("  edrive pwd")
	fmt.Println("  edrive version")
	fmt.Println()
	fmt.Println("Help:")
	fmt.Println("  edrive help")
	fmt.Println("  edrive help <command>")
	fmt.Println("  edrive <command> help")
	fmt.Println("  edrive <command> --help")
	fmt.Println()
	fmt.Println("Fixed paths:")
	fmt.Println("  workspace: ~/.edrive/workspace")
	fmt.Println("  encrypted vault: ~/.edrive/vault")
	fmt.Println("  remote: edrive-cloud:edrive")
}
