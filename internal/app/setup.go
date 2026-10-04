package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/faizahmd2/edrive/internal/cloud"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/cryptomator"
	"github.com/faizahmd2/edrive/internal/macos"
	"github.com/faizahmd2/edrive/internal/ui"
	"github.com/faizahmd2/edrive/internal/vault"
)

const fuseTLibrary = "/usr/local/lib/libfuse-t.dylib"

// Setup is safe to run any number of times: each step checks what is already
// in place and only fixes what is missing or broken.
func (a App) Setup(args []string) error {
	resetPassword := len(args) == 1 && args[0] == "--password"
	if len(args) > 0 && !resetPassword {
		return fmt.Errorf("usage: edrive setup [--password]")
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("edrive runs on macOS")
	}
	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return err
	}

	step := func(n int, title string) { fmt.Printf("\n[%d/5] %s\n", n, title) }

	step(1, "Tools")
	if err := ensureTools(); err != nil {
		return err
	}

	step(2, "Cloud")
	rc, err := a.rclone()
	if err != nil {
		return err
	}
	if err := cloud.EnsureConfigured(rc); err != nil {
		return err
	}
	if err := rc.EnsureRemoteDir(); err != nil {
		return err
	}
	fmt.Println("  ✓ connected to", a.Config.Remote())
	if err := a.Config.Save(); err != nil {
		return err
	}
	a.Config.ConfigFound = true

	step(3, "Encrypted vault")
	if mounted() {
		fmt.Println("  Locking the open workspace first...")
		if err := withLock(a.lock); err != nil {
			return err
		}
	}
	if err := a.ensureVault(); err != nil {
		return err
	}

	step(4, "Vault password")
	if err := ensurePassword(resetPassword); err != nil {
		return err
	}

	step(5, "Workspace")
	if err := prepareMountPoint(); err != nil {
		return err
	}
	_ = os.Remove(config.LegacyDevicesPath()) // public keys from the old backup system
	fmt.Println("  ✓", config.WorkspacePath())
	if !macos.CanAuthenticate() {
		fmt.Println("  ! Touch ID / password prompts are unavailable in this session; edrive needs your logged-in desktop session.")
	}

	fmt.Println()
	fmt.Println("Ready. Try:")
	fmt.Println("  edrive open            open the workspace in Finder")
	fmt.Println("  edrive pass set github save a secret")
	fmt.Println("  edrive pass github     read it back")
	printLegacyNotes()
	return nil
}

func ensureTools() error {
	if _, err := exec.LookPath("rclone"); err != nil {
		if err := brew("install", "rclone"); err != nil {
			return err
		}
	}
	fmt.Println("  ✓ rclone")

	if _, err := os.Stat(fuseTLibrary); err != nil {
		fmt.Println("  Installing FUSE-T (macOS may ask for your password)...")
		if err := brew("install", "--cask", "fuse-t"); err != nil {
			return err
		}
	}
	fmt.Println("  ✓ FUSE-T")

	if err := cryptomator.InstallCLI(config.ToolsDir()); err != nil {
		return err
	}
	fmt.Println("  ✓ Cryptomator CLI", cryptomator.CLIVersion, "(checksum and signature verified)")
	return nil
}

func brew(args ...string) error {
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("Homebrew is needed to install dependencies: https://brew.sh")
	}
	cmd := exec.Command("brew", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew %v failed", args)
	}
	return nil
}

func (a App) ensureVault() error {
	path := config.LocalVaultPath()
	state, err := vault.Inspect(path)
	if err != nil {
		return err
	}
	rc, err := a.rclone()
	if err != nil {
		return err
	}
	top, err := rc.List(a.Config.Remote(), false)
	if err != nil {
		return err
	}
	remoteHasVault, remoteHasOther := false, false
	for _, it := range top {
		switch it.Path {
		case "vault.cryptomator":
			remoteHasVault = true
		case "masterkey.cryptomator", ".edrive-trash", ".DS_Store":
		default:
			remoteHasOther = true
		}
	}

	switch {
	case state.Exists && !state.Empty && !state.Complete:
		return fmt.Errorf("%s has files but is not a complete Cryptomator vault.\nMove that folder somewhere else and run 'edrive setup' again; it will download your vault from the cloud", path)
	case !remoteHasVault && remoteHasOther && !state.Complete:
		return fmt.Errorf("%s already has files that are not an edrive vault; empty it or pick another cloud folder", a.Config.Remote())
	case !state.Complete && !remoteHasVault:
		if err := createVault(path); err != nil {
			return err
		}
	case !state.Complete:
		fmt.Println("  Downloading your vault from the cloud...")
	}

	s := a.syncer()
	if s == nil {
		return fmt.Errorf("cloud remote %q is not configured", a.Config.RcloneRemote)
	}
	res, err := s.Run(false)
	if err != nil {
		return fmt.Errorf("sync vault with the cloud: %w", err)
	}
	fmt.Println("  ✓ vault", path, "-", describeSync(res, nil))
	return nil
}

func createVault(path string) error {
	fmt.Println("  No vault found here or in the cloud, so let's create one.")
	if _, err := os.Stat("/Applications/Cryptomator.app"); err != nil {
		if err := brew("install", "--cask", "cryptomator"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("  In Cryptomator, choose  + Add > Create New Vault  and use:")
	fmt.Println("    Vault name:        vault")
	fmt.Println("    Storage location:  " + config.Home())
	fmt.Println("  Pick a strong password. edrive asks for it next and keeps it in your Keychain.")
	fmt.Println("  (You can leave Cryptomator's own 'remember password' off.)")
	fmt.Println()
	if err := ui.OpenApplication("Cryptomator"); err != nil {
		return fmt.Errorf("open Cryptomator: %w", err)
	}
	for {
		if err := ui.Pause("  Press Enter once the vault is created: "); err != nil {
			return err
		}
		state, err := vault.Inspect(path)
		if err == nil && state.Complete {
			return nil
		}
		fmt.Println("  Not found yet at", path, "- check the name and location.")
	}
}

func ensurePassword(reset bool) error {
	if !reset && macos.KeychainExists(config.KeychainService, config.VaultPasswordAccount) {
		fmt.Println("  ✓ stored in Keychain (readable only by edrive; run 'edrive setup --password' to change)")
		return nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		password, err := ui.ReadSecret("  Vault password: ")
		if err != nil {
			return err
		}
		fmt.Print("  Checking... ")
		err = cryptomator.VerifyPassword(config.LocalVaultPath(), password)
		if errors.Is(err, cryptomator.ErrWrongPassword) {
			ui.Wipe(password)
			fmt.Println("that is not the password for this vault. Try again.")
			continue
		}
		if err != nil {
			ui.Wipe(password)
			return err
		}
		err = macos.KeychainSet(config.KeychainService, config.VaultPasswordAccount, "edrive vault password", password)
		ui.Wipe(password)
		if err != nil {
			return err
		}
		fmt.Println("correct. Stored in Keychain (readable only by edrive).")
		return nil
	}
	return fmt.Errorf("vault password not set; run 'edrive setup' to try again")
}

func printLegacyNotes() {
	var notes []string
	if _, err := os.Stat(config.LegacyRecoveryKeyPath()); err == nil {
		notes = append(notes, "An old unencrypted recovery key is at "+config.LegacyRecoveryKeyPath()+".\n    It only opens backups made before this version. Move it to your password manager, then delete it.")
	}
	if legacyKeychainItem("recovery") || legacyKeychainItem("identity:mac-1") {
		notes = append(notes, "Old backup keys are still in your Keychain (no longer used). After saving them if needed, remove with:\n    security delete-generic-password -s edrive -a recovery\n    security delete-generic-password -s edrive -a identity:mac-1")
	}
	if len(notes) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("Cleanup from the previous version:")
	for _, n := range notes {
		fmt.Println("  - " + n)
	}
}

// legacyKeychainItem checks for an item without reading its secret (no prompt).
func legacyKeychainItem(account string) bool {
	return exec.Command("/usr/bin/security", "find-generic-password", "-s", config.KeychainService, "-a", account).Run() == nil
}
