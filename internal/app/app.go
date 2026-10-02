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
	configReady := a.Config.ConfigFound && a.ConfigError == nil

	if a.ConfigError != nil {
		a.check(false, "configuration", a.ConfigError.Error())
		problems = append(problems, "The configuration cannot be trusted. Run 'edrive setup' to rebuild it.")
	} else if !configReady {
		a.check(false, "configuration", "not initialized")
		problems = append(problems, "Run 'edrive setup' to create the configuration.")
	} else if !fileMode0600(a.Config.ConfigPath) {
		a.check(false, "configuration", "config file permissions are too open or the file is missing")
		problems = append(problems, "Run 'edrive setup' to rebuild the configuration.")
	} else {
		a.check(true, "configuration", a.Config.ConfigPath)
	}

	if runtime.GOOS != "darwin" {
		a.check(false, "platform", "edrive setup currently supports macOS")
		problems = append(problems, "Run edrive on macOS.")
	} else {
		a.check(true, "platform", "macOS")
	}

	if configReady {
		switch {
		case !isDir(a.Config.DataRoot):
			a.check(false, "workspace", "configured folder is missing: "+a.Config.DataRoot)
			problems = append(problems, "Run 'edrive setup' and choose a new working folder.")
		case mounted(a.Config.DataRoot):
			a.check(true, "workspace", "mounted and available")
		default:
			a.check(true, "workspace", "mount point exists and is ready")
		}
	} else {
		fmt.Println("workspace                -  not configured yet")
	}

	if isDir("/Applications/Google Drive.app") {
		a.check(true, "Google Drive app", "installed")
	} else {
		a.check(false, "Google Drive app", "not installed")
		problems = append(problems, "Install Google Drive for desktop, then run 'edrive setup'.")
	}

	drive := provider.GoogleDrive{PreferredRoot: a.Config.StorageRoot, StorageName: a.Config.StorageName}
	candidates, err := drive.Candidates()
	root := ""
	switch {
	case err != nil:
		a.check(false, "Google Drive files", err.Error())
		problems = append(problems, "Run 'edrive setup' after Google Drive local files are available.")
	case isDir(a.Config.StorageRoot):
		root = filepath.Clean(a.Config.StorageRoot)
		a.check(true, "Google Drive files", "saved local location is available")
	case len(candidates) == 1 && !configReady:
		root = candidates[0]
		a.check(true, "Google Drive files", "one local My Drive location is available")
	case len(candidates) == 1:
		root = candidates[0]
		a.check(false, "Google Drive files", "saved local location is gone; another local location is available")
		problems = append(problems, "Run 'edrive setup' and select the currently available Google Drive folder.")
	case len(candidates) == 0:
		a.check(false, "Google Drive files", "no local My Drive folder is available")
		problems = append(problems, "Open Google Drive and make My Drive available locally, then run 'edrive setup'.")
	default:
		a.check(false, "Google Drive files", fmt.Sprintf("%d local My Drive locations found; edrive will not guess", len(candidates)))
		problems = append(problems, "Run 'edrive setup' and choose which local Google Drive My Drive belongs to this setup.")
	}

	if configReady && root != "" {
		name := strings.TrimSpace(a.Config.StorageName)
		if name == "" {
			name = "edrive"
		}
		vaultPath := filepath.Join(root, name)

		if binding, ok, err := vault.ReadBinding(); err != nil {
			a.check(false, "edrive ownership", "saved vault ownership record is unreadable")
			problems = append(problems, "Run 'edrive setup' and explicitly select the correct edrive vault; the ownership record will be rebuilt.")
		} else if ok && binding.Path != filepath.Clean(vaultPath) {
			a.check(false, "edrive ownership", "saved ownership points to another vault")
			problems = append(problems, "Run 'edrive setup' and choose whether to use the currently selected vault or the previously bound one.")
		} else {
			a.check(true, "edrive ownership", "vault binding is consistent")
		}

		state, err := vault.Inspect(vaultPath)
		switch {
		case err != nil:
			a.check(false, "edrive vault", err.Error())
			problems = append(problems, "Run 'edrive setup' and choose or create a valid edrive vault.")
		case !state.Exists:
			a.check(false, "edrive vault", "configured vault folder is missing: "+vaultPath)
			problems = append(problems, "Run 'edrive setup'; it will create a new vault or let you choose another one.")
		case state.Managed && !state.Complete:
			a.check(false, "edrive vault", "edrive-owned vault is incomplete")
			problems = append(problems, "Run 'edrive setup' and choose a healthy vault or create a new one. edrive will not overwrite the incomplete one.")
		case state.HasCryptomatorFiles && !state.Managed:
			a.check(false, "edrive vault", "a Cryptomator vault exists here but is not identified as edrive-owned")
			problems = append(problems, "Run 'edrive setup'; it will ask before adopting this vault.")
		case !state.Managed:
			a.check(false, "edrive vault", "configured folder is not identified as an edrive vault")
			problems = append(problems, "Run 'edrive setup' and explicitly adopt or create the correct vault.")
		default:
			a.check(true, "edrive vault", "edrive-owned Cryptomator vault is present")
			if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
				a.check(false, "Cryptomator registration", "edrive vault is not registered on this Mac")
				problems = append(problems, "Run 'edrive setup'; it will open Cryptomator and guide you through adding the vault.")
			} else {
				a.check(true, "Cryptomator registration", "vault is registered")
			}
		}
	} else if configReady {
		a.check(false, "edrive vault", "cannot inspect until Google Drive local storage is available")
		problems = append(problems, "Restore Google Drive local storage and run 'edrive doctor' again.")
	} else {
		fmt.Println("edrive vault             -  not checked until setup")
	}

	if path := resolveToolPath(a.Config.AgePath, "age", toolchain.AgeVersion); path != "" {
		a.check(true, "age", path)
	} else {
		a.check(false, "age", "required version "+toolchain.AgeVersion+" is unavailable")
		problems = append(problems, "Install age "+toolchain.AgeVersion+" or restore it, then run 'edrive setup'.")
	}
	if path := resolveToolPath(a.Config.ZstdPath, "zstd", toolchain.ZstdVersion); path != "" {
		a.check(true, "zstd", path)
	} else {
		a.check(false, "zstd", "required version "+toolchain.ZstdVersion+" is unavailable")
		problems = append(problems, "Install zstd "+toolchain.ZstdVersion+" or restore it, then run 'edrive setup'.")
	}
	if path := cryptomatorCLI(a.Config.CryptomatorCLI); path != "" {
		a.check(true, "Cryptomator CLI", path)
	} else {
		a.check(false, "Cryptomator CLI", "required version "+toolchain.CryptomatorCLIVersion+" is unavailable")
		problems = append(problems, "Restore Cryptomator CLI "+toolchain.CryptomatorCLIVersion+" and run 'edrive setup'.")
	}
	if runtime.GOOS != "darwin" || brewCaskInstalled("fuse-t") {
		a.check(true, "FUSE-T", "installed")
	} else {
		a.check(false, "FUSE-T", "not installed")
		problems = append(problems, "Install FUSE-T, then run 'edrive setup'.")
	}

	if configReady {
		devices, err := device.List()
		switch {
		case err != nil:
			a.check(false, "devices", "device registry cannot be read")
			problems = append(problems, "Run 'edrive setup' to rebuild the local device registry.")
		case len(devices) == 0:
			a.check(false, "devices", "no device identities are registered")
			problems = append(problems, "Run 'edrive setup' to create the default device.")
		default:
			labels := make([]string, 0, len(devices))
			devicesOK := true
			for _, d := range devices {
				labels = append(labels, d.Label)
				if _, err := keychain.GetIdentity(d.Label); err != nil {
					devicesOK = false
				}
			}
			if devicesOK {
				a.check(true, "devices", strings.Join(labels, ", "))
			} else {
				a.check(false, "devices", strings.Join(labels, ", ")+" (one or more Keychain identities are missing or inaccessible)")
				problems = append(problems, "Restore the missing device Keychain item or run 'edrive setup' to rebuild the device registry.")
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
	return ui.Open(a.Config.DataRoot)
}

func (a App) CD() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	fmt.Println(a.Config.DataRoot)
	return nil
}

func (a App) Unlock() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	fmt.Println("Workspace unlocked.")
	return nil
}

func (a App) Lock() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}
	if !isDir(a.Config.DataRoot) {
		return fmt.Errorf("working folder is missing: %s; run 'edrive setup' to choose a new one", a.Config.DataRoot)
	}
	if !mounted(a.Config.DataRoot) {
		fmt.Println("Workspace already locked.")
		return nil
	}
	client := cryptomator.New(cryptomator.Config{
		MountPoint: a.Config.DataRoot,
		CLIPath:    cryptomatorCLI(a.Config.CryptomatorCLI),
		RuntimeDir: config.RuntimeDir(),
	})
	if client == nil {
		return fmt.Errorf("Cryptomator CLI is unavailable; restore it and run 'edrive setup'")
	}
	if err := client.Lock(); err != nil {
		return err
	}
	fmt.Println("Workspace locked.")
	return nil
}

func (a App) requireConfigured() error {
	if !a.Config.ConfigFound {
		return fmt.Errorf("edrive is not set up; run 'edrive setup' first")
	}
	if strings.TrimSpace(a.Config.DataRoot) == "" || strings.TrimSpace(a.Config.StorageRoot) == "" || strings.TrimSpace(a.Config.StorageName) == "" {
		return fmt.Errorf("edrive configuration is incomplete; run 'edrive setup' again")
	}
	return nil
}

func (a App) ensureUnlocked() error {
	if mounted(a.Config.DataRoot) {
		return nil
	}
	if !isDir(a.Config.DataRoot) {
		return fmt.Errorf("working folder is missing: %s; run 'edrive setup' to choose a new one", a.Config.DataRoot)
	}

	cliPath := cryptomatorCLI(a.Config.CryptomatorCLI)
	if cliPath == "" {
		return fmt.Errorf("Cryptomator CLI %s is missing or changed; restore it and run 'edrive setup'", toolchain.CryptomatorCLIVersion)
	}

	drive := provider.GoogleDrive{PreferredRoot: a.Config.StorageRoot, StorageName: a.Config.StorageName}
	storageRoot, err := drive.Root()
	if err != nil {
		return fmt.Errorf("Google Drive local My Drive is unavailable; run 'edrive doctor', then make Google Drive files available or run 'edrive setup'")
	}
	vaultPath, err := drive.VaultPath(a.Config.StorageName)
	if err != nil {
		return err
	}
	state, err := vault.Inspect(vaultPath)
	if err != nil {
		return err
	}
	if !state.Exists || !state.Complete {
		return fmt.Errorf("edrive encrypted vault is missing or incomplete at %s; run 'edrive setup'", vaultPath)
	}
	if !state.Managed {
		return fmt.Errorf("the Cryptomator vault at %s is not identified as an edrive vault; run 'edrive setup' to confirm or create the correct vault", vaultPath)
	}
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
		return fmt.Errorf("the edrive vault is not registered in Cryptomator; run 'edrive setup' and follow the guided add-vault step")
	}

	client := cryptomator.New(cryptomator.Config{
		VaultPath:  filepath.Join(storageRoot, a.Config.StorageName),
		MountPoint: a.Config.DataRoot,
		CLIPath:    cliPath,
		RuntimeDir: config.RuntimeDir(),
	})
	if err := client.Unlock(); err != nil {
		return err
	}
	return nil
}

func (a App) BackupToolPaths() (string, string, error) {
	agePath, err := resolveTool(a.Config.AgePath, "age", toolchain.AgeVersion)
	if err != nil {
		return "", "", err
	}
	zstdPath, err := resolveTool(a.Config.ZstdPath, "zstd", toolchain.ZstdVersion)
	if err != nil {
		return "", "", err
	}
	return agePath, zstdPath, nil
}

func resolveTool(configured, name, version string) (string, error) {
	if path := resolveToolPath(configured, name, version); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%s %s is no longer available; restore it or run 'edrive setup'", name, version)
}

func resolveToolPath(configured, name, version string) string {
	if isExecutableFile(configured) && toolVersionMatches(configured, version) {
		return filepath.Clean(configured)
	}
	if path, err := exec.LookPath(name); err == nil && toolVersionMatches(path, version) {
		return filepath.Clean(path)
	}
	return ""
}

func cryptomatorCLI(configured string) string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		configured,
		filepath.Join(config.ToolsDir(), "cryptomator-cli", toolchain.CryptomatorCLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
		filepath.Join(home, "Desktop/local-infra/tools/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
	}
	if path, err := exec.LookPath("cryptomator-cli"); err == nil {
		candidates = append(candidates, path)
	}
	for _, path := range candidates {
		if isExecutableFile(path) && toolVersionMatches(path, toolchain.CryptomatorCLIVersion) {
			return filepath.Clean(path)
		}
	}
	return ""
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
	return err == nil && info.Mode().IsRegular()
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
