package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/deps"
	"github.com/faiz/edrive/internal/ui"
)

func (a App) Remove() error {
	fmt.Println("Remove will delete edrive's local workspace, local encrypted vault,")
	fmt.Println("configuration and runtime state.")
	fmt.Println("It will NOT delete the remote encrypted vault, backups, or Keychain identities.")
	fmt.Println()

	ok, err := ui.Confirm("Continue with removing edrive's local state?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}

	if mounted(config.WorkspacePath()) {
		if err := a.Lock(); err != nil {
			return err
		}
	}

	if err := removeLocalState(); err != nil {
		return err
	}
	fmt.Println("edrive local state removed.")

	fmt.Println()
	fmt.Println("Dependencies are independent of edrive and are not removed automatically.")
	if err := maybeUninstallFormula("age", "age"); err != nil {
		return err
	}
	if err := maybeUninstallFormula("zstd", "zstd"); err != nil {
		return err
	}
	if err := maybeUninstallFormula("rclone", "rclone"); err != nil {
		return err
	}
	if err := maybeUninstallCask("FUSE-T", "fuse-t"); err != nil {
		return err
	}

	fmt.Println()
	ok, err = ui.Confirm("Uninstall the edrive-managed Cryptomator CLI too?")
	if err != nil {
		return err
	}
	if ok {
		if err := deps.RemoveManagedCryptomatorCLI(); err != nil {
			return fmt.Errorf("remove Cryptomator CLI: %w", err)
		}
		fmt.Println("Cryptomator CLI removed.")
	} else {
		fmt.Println("Cryptomator CLI left installed.")
	}

	fmt.Println()
	fmt.Println("Cryptomator Desktop is still installed.")
	fmt.Println("Uninstall it separately when you no longer need it:")
	fmt.Println("  brew uninstall --cask cryptomator")
	return nil
}

func maybeUninstallFormula(label, packageName string) error {
	ok, err := ui.Confirm("Uninstall " + label + " installed by Homebrew?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println(label + " left installed.")
		return nil
	}
	if err := deps.UninstallFormula(packageName); err != nil {
		return err
	}
	fmt.Println(label + " removed.")
	return nil
}

func maybeUninstallCask(label, packageName string) error {
	ok, err := ui.Confirm("Uninstall " + label + " installed by Homebrew?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println(label + " left installed.")
		return nil
	}
	if err := deps.UninstallCask(packageName); err != nil {
		return err
	}
	fmt.Println(label + " removed.")
	return nil
}

func removeLocalState() error {
	paths := []string{
		config.WorkspacePath(),
		config.LocalVaultPath(),
		config.RuntimeDir(),
		config.TempDir(),
		config.DevicesPath(),
		filepath.Join(config.Home(), "vault.json"),
		config.DefaultPath(),
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}

	entries, err := os.ReadDir(config.Home())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		return os.Remove(config.Home())
	}
	return nil
}
