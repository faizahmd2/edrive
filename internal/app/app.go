package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/cryptomator"
	"github.com/faiz/edrive/internal/device"
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/provider"
	"github.com/faiz/edrive/internal/toolchain"
	"github.com/faiz/edrive/internal/ui"
)

type App struct {
	Config config.Config
}

func (a App) Doctor() error {
	fmt.Println("EDRIVE DOCTOR")
	fmt.Println()

	ok := true
	ok = a.check("config", a.Config.ConfigFound, a.Config.ConfigPath) && ok
	ok = a.check("workspace", isDir(a.Config.DataRoot), a.Config.DataRoot) && ok

	root, rootErr := (provider.GoogleDrive{PreferredRoot: a.Config.StorageRoot, StorageName: a.Config.StorageName}).Root()
	if rootErr != nil {
		ok = a.check("Google Drive", false, "local storage unavailable") && ok
	} else {
		ok = a.check("Google Drive", true, "available") && ok
		vault := filepath.Join(root, a.Config.StorageName)
		ok = a.check("vault", isFile(filepath.Join(vault, "vault.cryptomator")), "encrypted workspace") && ok
		_, err := cryptomator.DiscoverVaultID(vault)
		ok = a.check("Cryptomator", err == nil, "vault registered") && ok
	}

	ok = a.check("age", toolVersionMatches(a.Config.AgePath, toolchain.AgeVersion), a.Config.AgePath) && ok
	ok = a.check("zstd", toolVersionMatches(a.Config.ZstdPath, toolchain.ZstdVersion), a.Config.ZstdPath) && ok
	ok = a.check("Cryptomator CLI", isExecutableFile(a.Config.CryptomatorCLI), a.Config.CryptomatorCLI) && ok
	ok = a.check("FUSE-T", runtime.GOOS != "darwin" || brewCaskInstalled("fuse-t"), "installed") && ok

	devices, err := device.List()
	if err != nil || len(devices) == 0 {
		ok = a.check("devices", false, "no device identities") && ok
	} else {
		labels := make([]string, 0, len(devices))
		for _, d := range devices {
			labels = append(labels, d.Label)
		}
		ok = a.check("devices", true, strings.Join(labels, ", ")) && ok
	}

	if keychain.RecoveryExists() {
		ok = a.check("recovery key", true, "available in Keychain") && ok
	} else {
		fmt.Println("recovery key            -  not created yet (created on first backup)")
	}

	fmt.Println()
	if ok {
		fmt.Println("Status: HEALTHY")
		return nil
	}
	return fmt.Errorf("doctor found one or more problems")
}

func (a App) check(name string, condition bool, detail string) bool {
	mark := "✓"
	if !condition {
		mark = "✗"
	}
	fmt.Printf("%-22s %s  %s\n", name, mark, detail)
	return condition
}

func (a App) Open() error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	return ui.Open(a.Config.DataRoot)
}

func (a App) CD() error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	fmt.Println(a.Config.DataRoot)
	return nil
}

func (a App) Unlock() error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	fmt.Println("Workspace unlocked.")
	return nil
}

func (a App) Lock() error {
	if !mounted(a.Config.DataRoot) {
		fmt.Println("Workspace already locked.")
		return nil
	}

	client := cryptomator.New(cryptomator.Config{
		MountPoint: a.Config.DataRoot,
		CLIPath:    a.Config.CryptomatorCLI,
		RuntimeDir: config.RuntimeDir(),
	})
	if err := client.Lock(); err != nil {
		return err
	}
	fmt.Println("Workspace locked.")
	return nil
}

func (a App) ensureUnlocked() error {
	if mounted(a.Config.DataRoot) {
		return nil
	}
	if a.Config.CryptomatorCLI == "" {
		return fmt.Errorf("Cryptomator CLI is not configured")
	}

	storageRoot, err := (provider.GoogleDrive{PreferredRoot: a.Config.StorageRoot, StorageName: a.Config.StorageName}).Root()
	if err != nil {
		return fmt.Errorf("Google Drive storage is unavailable")
	}
	vaultPath := filepath.Join(storageRoot, a.Config.StorageName)
	if !isFile(filepath.Join(vaultPath, "vault.cryptomator")) {
		return fmt.Errorf("encrypted workspace is unavailable")
	}

	client := cryptomator.New(cryptomator.Config{
		VaultPath:  vaultPath,
		MountPoint: a.Config.DataRoot,
		CLIPath:    a.Config.CryptomatorCLI,
		RuntimeDir: config.RuntimeDir(),
	})
	if err := client.Unlock(); err != nil {
		return err
	}
	return nil
}

func (a App) requireBackupTools() error {
	if !toolVersionMatches(a.Config.AgePath, toolchain.AgeVersion) {
		return fmt.Errorf("age %s is required", toolchain.AgeVersion)
	}
	if !toolVersionMatches(a.Config.ZstdPath, toolchain.ZstdVersion) {
		return fmt.Errorf("zstd %s is required", toolchain.ZstdVersion)
	}
	if !isDir(a.Config.DataRoot) {
		return fmt.Errorf("workspace is not configured")
	}
	return nil
}

func toolVersionMatches(path, version string) bool {
	if !isExecutableFile(path) {
		return false
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	return err == nil && strings.Contains(string(out), version)
}

func mounted(path string) bool {
	if path == "" {
		return false
	}
	out, err := exec.Command("mount").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), " on "+filepath.Clean(path)+" (")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func fileMode0600(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
}

func brewCaskInstalled(name string) bool {
	return exec.Command("brew", "list", "--cask", name).Run() == nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func defaultHome() string {
	home, _ := os.UserHomeDir()
	return home
}

func timestampedBackupName() string {
	return "edrive-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.zst.age"
}
