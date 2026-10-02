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
	"github.com/faiz/edrive/internal/deps"
	"github.com/faiz/edrive/internal/device"
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/rclone"
	"github.com/faiz/edrive/internal/ui"
	"github.com/faiz/edrive/internal/vault"
)

type App struct {
	Config      config.Config
	ConfigError error
}

func (a App) Doctor() error {
	fmt.Println("EDRIVE DOCTOR")
	fmt.Println()

	var problems []string
	configReady := a.Config.ConfigFound && a.ConfigError == nil && a.Config.Version == config.CurrentVersion

	if a.ConfigError != nil {
		a.check(false, "configuration", a.ConfigError.Error())
		problems = append(problems, "Run 'edrive setup' to rebuild the configuration.")
	} else if !configReady {
		a.check(false, "configuration", "not initialized")
		problems = append(problems, "Run 'edrive setup' to create the configuration.")
	} else if !fileMode0600(a.Config.ConfigPath) {
		a.check(false, "configuration", "config file permissions are too open")
		problems = append(problems, "Run 'edrive setup' to rebuild the configuration.")
	} else {
		a.check(true, "configuration", a.Config.ConfigPath)
	}

	if runtime.GOOS != "darwin" {
		a.check(false, "platform", "setup currently supports macOS")
		problems = append(problems, "Run edrive on macOS.")
	} else {
		a.check(true, "platform", "macOS")
	}

	workspace := config.WorkspacePath()
	vaultPath := config.LocalVaultPath()

	if !isDir(workspace) {
		a.check(false, "workspace", "not present: "+workspace)
		if configReady {
			problems = append(problems, "Run 'edrive setup' to recreate the workspace.")
		}
	} else if mounted(workspace) {
		a.check(true, "workspace", "mounted and available")
	} else {
		a.check(true, "workspace", "mount point exists and is ready")
	}

	state, err := vault.Inspect(vaultPath)
	if err != nil {
		a.check(false, "local vault", err.Error())
		problems = append(problems, "Run 'edrive setup' to repair the local vault.")
	} else if !state.Exists {
		a.check(false, "local vault", "not present")
		problems = append(problems, "Run 'edrive setup' to pull or create the encrypted vault.")
	} else if !state.Complete {
		a.check(false, "local vault", "incomplete Cryptomator vault")
		problems = append(problems, "Run 'edrive setup' to repair the local vault.")
	} else {
		a.check(true, "local vault", vaultPath)
		if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
			a.check(false, "Cryptomator registration", "vault is not registered on this Mac")
			problems = append(problems, "Open Cryptomator and add the existing vault, then run 'edrive doctor' again.")
		} else {
			a.check(true, "Cryptomator registration", "vault is registered")
		}
	}

	if path := resolveToolPath(a.Config.AgePath, "age"); path != "" {
		a.check(true, "age", path)
	} else {
		a.check(false, "age", "not installed")
		problems = append(problems, "Run 'edrive setup' to install age automatically.")
	}

	if path := resolveToolPath(a.Config.ZstdPath, "zstd"); path != "" {
		a.check(true, "zstd", path)
	} else {
		a.check(false, "zstd", "not installed")
		problems = append(problems, "Run 'edrive setup' to install zstd automatically.")
	}

	if path := resolveToolPath("", "rclone"); path != "" {
		a.check(true, "rclone", path)
		rc, _ := rclone.New(path, config.RcloneRemote, config.RemoteVault)
		if err := rc.EnsureConfigured(); err != nil {
			a.check(false, "cloud login", err.Error())
			problems = append(problems, "Run 'edrive setup' to authenticate the rclone remote.")
		} else {
			a.check(true, "cloud login", config.RcloneRemote)
		}
	} else {
		a.check(false, "rclone", "not installed")
		problems = append(problems, "Run 'edrive setup' to install rclone automatically.")
	}

	if path := deps.FindCryptomatorCLI(a.Config.CryptomatorCLI); path != "" {
		a.check(true, "Cryptomator CLI", path)
	} else {
		a.check(false, "Cryptomator CLI", "not installed")
		problems = append(problems, "Run 'edrive setup' to install Cryptomator CLI automatically.")
	}

	if deps.FUSEInstalled() {
		a.check(true, "FUSE-T", "installed")
	} else {
		a.check(false, "FUSE-T", "not installed")
		problems = append(problems, "Run 'edrive setup' to install FUSE-T automatically.")
	}

	if deps.CryptomatorInstalled() {
		a.check(true, "Cryptomator app", "installed")
	} else {
		a.check(false, "Cryptomator app", "not installed")
		problems = append(problems, "Run 'edrive setup' to install Cryptomator automatically.")
	}

	if configReady {
		devices, err := device.List()
		switch {
		case err != nil:
			a.check(false, "devices", "device registry cannot be read")
			problems = append(problems, "Run 'edrive setup' to rebuild the device registry.")
		case len(devices) == 0:
			a.check(false, "devices", "no device identities are registered")
			problems = append(problems, "Run 'edrive setup' to create the default device.")
		default:
			labels := make([]string, 0, len(devices))
			ok := true
			for _, d := range devices {
				labels = append(labels, d.Label)
				if _, err := keychain.GetIdentity(d.Label); err != nil {
					ok = false
				}
			}
			if ok {
				a.check(true, "devices", strings.Join(labels, ", "))
			} else {
				a.check(false, "devices", strings.Join(labels, ", ")+" (Keychain access problem)")
				problems = append(problems, "Restore the missing device Keychain item or run 'edrive setup'.")
			}
		}
	} else {
		fmt.Println("devices                  -  not initialized until setup")
	}

	if keychain.RecoveryExists() {
		a.check(true, "recovery key", "available in Keychain")
	} else {
		fmt.Println("recovery key             -  not created yet (created on first backup)")
	}

	fmt.Println()
	if len(problems) == 0 {
		fmt.Println("Result: READY")
		return nil
	}

	fmt.Printf("Problems found: %d\n", len(problems))
	for i, problem := range problems {
		fmt.Printf("  %d. %s\n", i+1, problem)
	}
	return fmt.Errorf("doctor found %d problem(s)", len(problems))
}

func (a App) check(ok bool, name, detail string) bool {
	mark := "✓"
	if !ok {
		mark = "✗"
	}
	fmt.Printf("%-24s %s  %s\n", name, mark, detail)
	return ok
}

func (a App) Open() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	fmt.Println("Workspace:", config.WorkspacePath())
	return ui.Open(config.WorkspacePath())
}

func (a App) Lock() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	workspace := config.WorkspacePath()
	if !isDir(workspace) {
		return fmt.Errorf("workspace is missing at %s; run 'edrive setup'", workspace)
	}
	if !mounted(workspace) {
		fmt.Println("Workspace already locked.")
		return nil
	}

	cliPath := deps.FindCryptomatorCLI(a.Config.CryptomatorCLI)
	if cliPath == "" {
		return fmt.Errorf("Cryptomator CLI is unavailable; run 'edrive setup'")
	}

	client := cryptomator.New(cryptomator.Config{
		MountPoint: workspace,
		VaultPath:  config.LocalVaultPath(),
		CLIPath:    cliPath,
		RuntimeDir: config.RuntimeDir(),
	})
	if err := client.Lock(); err != nil {
		return err
	}
	fmt.Println("Workspace locked.")
	return nil
}

func (a App) Push() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if mounted(config.WorkspacePath()) {
		return fmt.Errorf("workspace is unlocked; run 'edrive lock' before push")
	}

	state, err := vault.Inspect(config.LocalVaultPath())
	if err != nil {
		return err
	}
	if !state.Exists || !state.Complete {
		return fmt.Errorf("local Cryptomator vault is missing or incomplete; run 'edrive setup'")
	}

	rc, err := a.rcloneClient()
	if err != nil {
		return err
	}
	if err := rc.EnsureConfigured(); err != nil {
		return err
	}
	if err := rc.EnsureRemoteDir(); err != nil {
		return err
	}
	if err := rc.SyncLocalToRemote(config.LocalVaultPath()); err != nil {
		return err
	}
	fmt.Println("Published encrypted vault.")
	return nil
}

func (a App) Pull() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if mounted(config.WorkspacePath()) {
		return fmt.Errorf("workspace is unlocked; run 'edrive lock' before pull")
	}

	rc, err := a.rcloneClient()
	if err != nil {
		return err
	}
	if err := rc.EnsureConfigured(); err != nil {
		return err
	}
	if err := rc.EnsureRemoteDir(); err != nil {
		return err
	}
	if err := rc.SyncRemoteToLocal(config.LocalVaultPath()); err != nil {
		return err
	}

	state, err := vault.Inspect(config.LocalVaultPath())
	if err != nil {
		return err
	}
	if !state.Complete {
		return fmt.Errorf("the remote data is not a complete Cryptomator vault")
	}
	fmt.Println("Pulled encrypted vault.")
	return nil
}

func (a App) BackupToolPaths() (string, string, error) {
	agePath, err := resolveTool(a.Config.AgePath, "age")
	if err != nil {
		return "", "", err
	}
	zstdPath, err := resolveTool(a.Config.ZstdPath, "zstd")
	if err != nil {
		return "", "", err
	}
	return agePath, zstdPath, nil
}

func (a App) rcloneClient() (*rclone.Client, error) {
	path := resolveToolPath("", "rclone")
	if path == "" {
		return nil, fmt.Errorf("rclone is unavailable; run 'edrive setup'")
	}
	return rclone.New(path, config.RcloneRemote, config.RemoteVault)
}

func (a App) requireConfigured() error {
	if !a.Config.ConfigFound {
		return fmt.Errorf("edrive is not set up; run 'edrive setup' first")
	}
	if a.Config.Version != config.CurrentVersion {
		return fmt.Errorf("edrive configuration must be rebuilt; run 'edrive setup'")
	}
	return nil
}

func (a App) ensureUnlocked() error {
	workspace := config.WorkspacePath()
	if mounted(workspace) {
		return nil
	}
	if !isDir(workspace) {
		return fmt.Errorf("workspace is missing at %s; run 'edrive setup'")
	}

	vaultPath := config.LocalVaultPath()
	state, err := vault.Inspect(vaultPath)
	if err != nil {
		return err
	}
	if !state.Exists || !state.Complete {
		return fmt.Errorf("local Cryptomator vault is missing or incomplete; run 'edrive setup'")
	}

	cliPath := deps.FindCryptomatorCLI(a.Config.CryptomatorCLI)
	if cliPath == "" {
		return fmt.Errorf("Cryptomator CLI is missing; run 'edrive setup'")
	}
	vaultID, err := cryptomator.DiscoverVaultID(vaultPath)
	if err != nil {
		return fmt.Errorf("the edrive vault is not registered in Cryptomator; run 'edrive setup'")
	}
	if !cryptomator.CredentialAvailable(vaultID) {
		return fmt.Errorf("Cryptomator does not have the vault password in Keychain; unlock it once in Cryptomator and enable 'Remember password'")
	}

	client := cryptomator.New(cryptomator.Config{
		VaultPath:  vaultPath,
		VaultID:    vaultID,
		MountPoint: workspace,
		CLIPath:    cliPath,
		RuntimeDir: config.RuntimeDir(),
	})
	return client.Unlock()
}

func resolveTool(configured, name string) (string, error) {
	if path := resolveToolPath(configured, name); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%s is not installed; run 'edrive setup'", name)
}

func resolveToolPath(configured, name string) string {
	if isExecutableFile(configured) {
		return filepath.Clean(configured)
	}
	if path, err := exec.LookPath(name); err == nil && isExecutableFile(path) {
		return filepath.Clean(path)
	}
	return ""
}

func mounted(path string) bool {
	if path == "" {
		return false
	}
	out, err := exec.Command("/sbin/mount").Output()
	if err != nil {
		out, err = exec.Command("mount").Output()
		if err != nil {
			return false
		}
	}
	return strings.Contains(string(out), " on "+filepath.Clean(path)+" (")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func fileMode0600(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
}

func timestampedBackupName() string {
	return "edrive-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.zst.age"
}
