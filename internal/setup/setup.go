package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/faiz/edrive/internal/ageutil"
	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/cryptomator"
	"github.com/faiz/edrive/internal/device"
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/provider"
	"github.com/faiz/edrive/internal/toolchain"
	"github.com/faiz/edrive/internal/ui"
	"github.com/faiz/edrive/internal/vault"
)

func Run() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("setup currently supports macOS")
	}

	ok, err := confirmFreshSetup()
	if err != nil || !ok {
		return err
	}

	cfg := config.Defaults(config.DefaultPath())
	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return fmt.Errorf("create edrive state: %w", err)
	}
	if err := os.MkdirAll(config.TempDir(), 0700); err != nil {
		return fmt.Errorf("create edrive temporary state: %w", err)
	}

	completed, err := chooseWorkspace(&cfg)
	if err != nil {
		return err
	}
	if !completed {
		return nil
	}
	if err := ensureDependencies(&cfg); err != nil {
		return err
	}

	root, err := ensureGoogleDriveRoot(cfg.DataRoot)
	if err != nil {
		return err
	}
	if root == "" {
		return nil
	}
	cfg.StorageRoot = root

	vaultPath, err := ensureVault(root)
	if err != nil {
		return err
	}
	if vaultPath == "" {
		return nil
	}
	relative, err := filepath.Rel(root, vaultPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("edrive vault must stay inside Google Drive")
	}
	cfg.StorageName = filepath.Clean(relative)

	keygenPath, err := ageutil.KeygenPath(cfg.AgePath)
	if err != nil {
		return err
	}
	if err := ensureDefaultDevice(keygenPath); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println("Working folder:", cfg.DataRoot)
	fmt.Println()
	fmt.Println("Use:")
	fmt.Println("  edrive open")
	fmt.Println("  edrive cd")
	fmt.Println("  edrive backup")
	return nil
}

func confirmFreshSetup() (bool, error) {
	if _, err := os.Stat(config.DefaultPath()); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	fmt.Println("An edrive configuration already exists.")
	fmt.Println("Fresh setup will rebuild edrive from the beginning.")
	fmt.Println("It will not delete your Google Drive files, Cryptomator vaults, or Keychain identities.")
	fmt.Println("Your existing edrive device identities are kept so existing backups remain decryptable.")
	fmt.Println("Run 'edrive doctor' first when you want to inspect the current installation.")
	fmt.Println()

	ok, err := ui.Confirm("Continue with fresh setup?")
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Println("Setup cancelled.")
		return false, nil
	}

	old, loadErr := config.Load(config.DefaultPath())
	if loadErr == nil && old.DataRoot != "" && mounted(old.DataRoot) {
		fmt.Println()
		fmt.Println("The current edrive workspace is still mounted.")
		fmt.Println("Lock it in Cryptomator, then run 'edrive setup' again.")
		fmt.Println("The existing configuration was left unchanged.")
		return false, nil
	}

	if err := os.Remove(config.DefaultPath()); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("remove old edrive configuration: %w", err)
	}
	return true, nil
}

func chooseWorkspace(cfg *config.Config) (bool, error) {
	defaultDir := config.DefaultDataRoot()
	_ = os.MkdirAll(defaultDir, 0700)

	for {
		fmt.Println()
		fmt.Println("Step 2/4: choose the folder where you will see your decrypted edrive files.")
		fmt.Println("Choose an empty folder. edrive will mount the workspace there.")
		path, selected, err := ui.ChooseFolder("Choose edrive working folder", defaultDir)
		if err != nil {
			return false, err
		}
		if !selected {
			fmt.Println("Setup cancelled.")
			return false, nil
		}
		if mounted(path) {
			cfg.DataRoot = filepath.Clean(path)
			return true, nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			fmt.Println("I could not inspect that folder. Choose another one.")
			continue
		}
		if len(entries) != 0 {
			fmt.Println("That folder is not empty. Choose an empty folder.")
			continue
		}
		cfg.DataRoot = filepath.Clean(path)
		return true, nil
	}
}

func ensureDependencies(cfg *config.Config) error {
	var missing []string

	if path, err := ensureAge(); err == nil {
		cfg.AgePath = path
	} else {
		missing = append(missing, "age 1.3.2")
	}
	if path, err := ensureExactTool("zstd", toolchain.ZstdVersion); err == nil {
		cfg.ZstdPath = path
	} else {
		missing = append(missing, "zstd 1.5.7")
	}
	if path, err := findCryptomatorCLI(); err == nil {
		cfg.CryptomatorCLI = path
	} else {
		missing = append(missing, "Cryptomator CLI 0.6.2")
	}
	if !isDir("/Applications/Google Drive.app") {
		missing = append(missing, "Google Drive for desktop")
	}
	if !brewCaskInstalled("fuse-t") {
		missing = append(missing, "FUSE-T")
	}
	if !isDir("/Applications/Cryptomator.app") {
		missing = append(missing, "Cryptomator")
	}

	if len(missing) == 0 {
		fmt.Println()
		fmt.Println("Required components: ready.")
		return nil
	}

	fmt.Println()
	fmt.Println("Setup is paused. These required components are missing or incorrect:")
	for _, item := range missing {
		fmt.Println("  -", item)
	}
	fmt.Println()
	fmt.Println("Restore or install them, then run 'edrive setup' again.")
	return fmt.Errorf("required components are not ready")
}

func ensureGoogleDriveRoot(dataRoot string) (string, error) {
	drive := provider.GoogleDrive{StorageName: "edrive"}
	candidates, err := drive.Candidates()
	if err != nil {
		return "", err
	}

	if len(candidates) == 1 {
		root := provider.NormalizeRoot(candidates[0])
		if err := validateGoogleDriveRoot(root, dataRoot); err != nil {
			return "", err
		}
		fmt.Println()
		fmt.Println("Google Drive local My Drive found:")
		fmt.Println(" ", root)
		return root, nil
	}

	if len(candidates) > 1 {
		fmt.Println()
		fmt.Println("Step 3/4: choose which local Google Drive My Drive edrive should use.")
		fmt.Println()
		fmt.Println("I found more than one local Google Drive My Drive.")
		fmt.Println("Choose one of these known Google Drive locations, or choose another local folder explicitly.")
		fmt.Println()

		options := append(append([]string{}, candidates...), "Choose another local Google Drive folder...")
		choice, selected, err := ui.ChooseOption(
			"Choose the Google Drive location edrive should use",
			options,
			0,
		)
		if err != nil {
			return "", err
		}
		if !selected {
			fmt.Println("Setup cancelled.")
			return "", nil
		}

		if choice == "Choose another local Google Drive folder..." {
			return chooseGoogleDriveFolder(dataRoot, candidates[0])
		}
		root := provider.NormalizeRoot(choice)
		if err := validateGoogleDriveRoot(root, dataRoot); err != nil {
			return "", err
		}
		fmt.Println()
		fmt.Println("Google Drive location selected:")
		fmt.Println(" ", root)
		return root, nil
	}

	fmt.Println()
	fmt.Println("Step 3/4: connect edrive to your local Google Drive files.")
	fmt.Println()
	fmt.Println("If Google Drive My Drive is already available locally (for example, mirror mode), I can use that folder now.")
	fmt.Println("If it is not available yet, I will open Google Drive and guide you through making it available.")
	fmt.Println()
	already, err := ui.Confirm("Is your Google Drive My Drive folder already available locally?")
	if err != nil {
		return "", err
	}
	if already {
		return chooseGoogleDriveFolder(dataRoot, config.Home())
	}

	fmt.Println()
	fmt.Println("Before I open Google Drive:")
	fmt.Println("  1. Make sure you are signed in.")
	fmt.Println("  2. Make My Drive available locally.")
	fmt.Println("  3. You may use mirror mode or streaming mode.")
	fmt.Println("  4. Wait until the local folder is visible in Finder.")
	fmt.Println()
	if err := provider.EnsureRunning(); err != nil {
		return "", fmt.Errorf("open Google Drive: %w", err)
	}
	if err := ui.Pause("Press Enter after Google Drive is ready: "); err != nil {
		return "", err
	}

	candidates, err = drive.Candidates()
	if err != nil {
		return "", err
	}
	if len(candidates) == 1 {
		root := provider.NormalizeRoot(candidates[0])
		if err := validateGoogleDriveRoot(root, dataRoot); err != nil {
			return "", err
		}
		fmt.Println("Google Drive local My Drive found:", root)
		return root, nil
	}
	if len(candidates) > 1 {
		fmt.Println("Multiple local Google Drive locations are available. Please choose one.")
		options := append(append([]string{}, candidates...), "Choose another local Google Drive folder...")
		choice, selected, err := ui.ChooseOption(
			"Choose the Google Drive location edrive should use",
			options,
			0,
		)
		if err != nil {
			return "", err
		}
		if !selected {
			fmt.Println("Setup cancelled.")
			return "", nil
		}
		if choice == "Choose another local Google Drive folder..." {
			return chooseGoogleDriveFolder(dataRoot, candidates[0])
		}
		root := provider.NormalizeRoot(choice)
		if err := validateGoogleDriveRoot(root, dataRoot); err != nil {
			return "", err
		}
		fmt.Println("Google Drive location selected:", root)
		return root, nil
	}

	return chooseGoogleDriveFolder(dataRoot, config.Home())
}

func chooseGoogleDriveFolder(dataRoot, defaultDir string) (string, error) {
	for {
		path, selected, err := ui.ChooseFolder("Choose the local Google Drive My Drive folder edrive should use", defaultDir)
		if err != nil {
			return "", err
		}
		if !selected {
			fmt.Println("Setup cancelled.")
			return "", nil
		}

		root := provider.NormalizeRoot(path)
		if err := validateGoogleDriveRoot(root, dataRoot); err != nil {
			fmt.Println("That folder cannot be used as Google Drive storage:")
			fmt.Println(" ", err)
			fmt.Println("Choose a different local Google Drive folder.")
			continue
		}
		fmt.Println()
		fmt.Println("Google Drive location selected:")
		fmt.Println(" ", root)
		return root, nil
	}
}

func validateGoogleDriveRoot(root, dataRoot string) error {
	root = filepath.Clean(root)
	dataRoot = filepath.Clean(dataRoot)
	if !isDir(root) {
		return fmt.Errorf("the selected Google Drive folder is unavailable")
	}
	if root == dataRoot || pathInside(root, dataRoot) || pathInside(dataRoot, root) {
		return fmt.Errorf("Google Drive storage cannot be the same as, or contain, the edrive workspace")
	}
	return nil
}

func ensureVault(root string) (string, error) {
	if binding, ok, err := vault.ReadBinding(); err != nil {
		fmt.Println()
		fmt.Println("The saved edrive vault ownership record is unreadable.")
		fmt.Println("edrive will not guess which Cryptomator vault belongs to it.")
		fmt.Println("You can explicitly choose the correct vault during setup.")
	} else if ok && pathInside(root, binding.Path) {
		state, stateErr := vault.Inspect(binding.Path)
		if stateErr == nil {
			if state.Complete {
				fmt.Println()
				fmt.Println("Your existing edrive vault was found:")
				fmt.Println(" ", binding.Path)
				if err := ensureRegistered(binding.Path); err != nil {
					return "", err
				}
				return binding.Path, nil
			}
			fmt.Println()
			fmt.Println("Your previous edrive vault is no longer complete:")
			fmt.Println(" ", binding.Path)
			fmt.Println("edrive will not overwrite it.")
		}
	}

	candidate := filepath.Join(root, "edrive")

	for {
		state, err := vault.Inspect(candidate)
		if err != nil {
			return "", err
		}

		if state.Managed {
			if !state.Complete {
				fmt.Println("edrive's previous vault record points here, but the vault is incomplete.")
				return chooseVaultParent(root)
			}
			if err := ensureRegistered(candidate); err != nil {
				return "", err
			}
			return candidate, nil
		}

		if state.HasCryptomatorFiles {
			if !state.Complete {
				fmt.Println("An incomplete Cryptomator vault is at:")
				fmt.Println(" ", candidate)
				fmt.Println("edrive will not touch or repair it.")
				return chooseVaultParent(root)
			}

			fmt.Println()
			fmt.Println("A Cryptomator vault already exists at:")
			fmt.Println(" ", candidate)
			fmt.Println("It may belong to another application or another vault setup.")
			use, err := ui.Confirm("Is this the edrive vault you want to use?")
			if err != nil {
				return "", err
			}
			if use {
				if err := ensureRegistered(candidate); err != nil {
					return "", err
				}
				if err := vault.MarkManaged(candidate); err != nil {
					return "", err
				}
				return candidate, nil
			}
			return chooseVaultParent(root)
		}

		if !state.Empty {
			fmt.Println()
			fmt.Println("The default edrive vault folder already contains other files:")
			fmt.Println(" ", candidate)
			fmt.Println("edrive will not use or modify it.")
			return chooseVaultParent(root)
		}

		if err := createNewVault(candidate); err != nil {
			return "", err
		}
		if err := vault.MarkManaged(candidate); err != nil {
			return "", err
		}
	}
}

func chooseVaultParent(root string) (string, error) {
	for {
		fmt.Println()
		fmt.Println("Choose a different folder inside Google Drive.")
		fmt.Println("edrive will create its own 'edrive' vault folder inside that folder.")
		parent, selected, err := ui.ChooseFolder("Choose an edrive vault parent folder", root)
		if err != nil {
			return "", err
		}
		if !selected {
			fmt.Println("Setup cancelled.")
			return "", nil
		}
		parent = filepath.Clean(parent)
		rel, err := filepath.Rel(root, parent)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			fmt.Println("Please choose a folder inside Google Drive.")
			continue
		}

		candidate := filepath.Join(parent, "edrive")
		state, err := vault.Inspect(candidate)
		if err != nil {
			return "", err
		}
		if state.Exists {
			fmt.Println("That location already uses an 'edrive' folder. Choose another parent.")
			continue
		}
		if err := createNewVault(candidate); err != nil {
			return "", err
		}
		if err := vault.MarkManaged(candidate); err != nil {
			return "", err
		}
		return candidate, nil
	}
}

func createNewVault(vaultPath string) error {
	name := filepath.Base(vaultPath)
	parent := filepath.Dir(vaultPath)

	fmt.Println()
	fmt.Println("Step 4/4: create the edrive encrypted vault.")
	fmt.Println("Before I open Cryptomator, do this:")
	fmt.Println()
	fmt.Println("  1. Choose Add -> Create New Vault.")
	fmt.Println("  2. Vault name:", name)
	fmt.Println("  3. Storage location:", parent)
	fmt.Println("  4. Finish the vault creation.")
	fmt.Println()
	fmt.Println("The resulting vault must be:")
	fmt.Println(" ", vaultPath)
	fmt.Println("Do not select an unrelated existing vault.")
	fmt.Println()

	if err := ui.OpenApplication("Cryptomator"); err != nil {
		return fmt.Errorf("open Cryptomator: %w", err)
	}
	if err := ui.Pause("Press Enter after the new vault is created and visible in Cryptomator: "); err != nil {
		return err
	}
	state, err := vault.Inspect(vaultPath)
	if err != nil {
		return err
	}
	if !state.Complete {
		return fmt.Errorf("the new Cryptomator vault was not found at %s; nothing was modified by edrive", vaultPath)
	}
	return ensureRegistered(vaultPath)
}

func ensureRegistered(vaultPath string) error {
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err == nil {
		return nil
	}

	fmt.Println()
	fmt.Println("The vault exists, but Cryptomator has not registered it on this Mac.")
	fmt.Println("Before I open Cryptomator, do this:")
	fmt.Println()
	fmt.Println("  1. Choose Add -> Open Existing Vault.")
	fmt.Println("  2. Select vault.cryptomator inside:")
	fmt.Println("    ", vaultPath)
	fmt.Println("  3. Finish adding the vault.")
	fmt.Println()
	if err := ui.OpenApplication("Cryptomator"); err != nil {
		return fmt.Errorf("open Cryptomator: %w", err)
	}
	if err := ui.Pause("Press Enter after the vault appears in Cryptomator: "); err != nil {
		return err
	}
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
		return fmt.Errorf("Cryptomator still has not registered this vault; add it in Cryptomator at %s and retry setup", vaultPath)
	}
	return nil
}

func ensureDefaultDevice(ageKeygen string) error {
	devices, err := device.List()
	if err != nil {
		return err
	}
	for _, d := range devices {
		if d.Label == "mac-1" {
			return nil
		}
	}
	if keychain.IdentityExists("mac-1") {
		identity, err := keychain.GetIdentity("mac-1")
		if err != nil {
			return fmt.Errorf("edrive needs access to its existing mac-1 device identity")
		}
		_, err = device.Import("mac-1", identity, ageKeygen)
		return err
	}
	_, err = device.AddGenerated("mac-1", ageKeygen)
	return err
}

func ensureAge() (string, error) {
	if path, err := exec.LookPath("age"); err == nil && toolVersionMatches(path, toolchain.AgeVersion) {
		return path, nil
	}
	path := filepath.Join(config.ToolsDir(), "age", toolchain.AgeVersion, "age")
	if isExecutable(path) && toolVersionMatches(path, toolchain.AgeVersion) {
		return path, nil
	}
	return "", fmt.Errorf("age %s is required", toolchain.AgeVersion)
}

func ensureExactTool(name, version string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil || !toolVersionMatches(path, version) {
		return "", fmt.Errorf("%s %s is required", name, version)
	}
	return path, nil
}

func findCryptomatorCLI() (string, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(config.ToolsDir(), "cryptomator-cli", toolchain.CryptomatorCLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
		filepath.Join(home, "Desktop/local-infra/tools/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
	}
	if path, err := exec.LookPath("cryptomator-cli"); err == nil {
		candidates = append(candidates, path)
	}
	for _, path := range unique(candidates) {
		if isExecutable(path) && toolVersionMatches(path, toolchain.CryptomatorCLIVersion) {
			return path, nil
		}
	}
	return "", fmt.Errorf("Cryptomator CLI %s is required", toolchain.CryptomatorCLIVersion)
}

func mounted(path string) bool {
	if path == "" {
		return false
	}
	out, err := exec.Command("/sbin/mount").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), " on "+filepath.Clean(path)+" (")
}

func pathInside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func brewCaskInstalled(name string) bool {
	return exec.Command("brew", "list", "--cask", name).Run() == nil
}

func toolVersionMatches(path, version string) bool {
	out, err := exec.Command(path, "--version").CombinedOutput()
	return err == nil && strings.Contains(string(out), version)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func unique(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		path = filepath.Clean(path)
		if path == "." {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}
