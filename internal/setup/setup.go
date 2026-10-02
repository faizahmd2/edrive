package setup

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
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
)

var input = bufio.NewReader(os.Stdin)

func Run() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("setup currently supports macOS")
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}

	legacy := legacyValues()
	migrated := seedFromLegacy(&cfg, legacy)

	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(config.TempDir(), 0700); err != nil {
		return err
	}

	if err := chooseWorkspace(&cfg, migrated); err != nil {
		return err
	}
	if err := ensureWorkspace(cfg.DataRoot, cfg.ConfigFound); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := ensureDependencies(&cfg); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	drive := provider.GoogleDrive{
		PreferredRoot: cfg.StorageRoot,
		StorageName:   cfg.StorageName,
	}
	root, err := drive.Root()
	if err != nil {
		_ = provider.EnsureRunning()
		root, err = drive.Root()
		if err != nil {
			return fmt.Errorf("Google Drive storage is unavailable")
		}
	}
	cfg.StorageRoot = root
	if err := cfg.Save(); err != nil {
		return err
	}

	vaultPath, err := drive.VaultPath(cfg.StorageName)
	if err != nil {
		return err
	}
	if err := ensureVault(vaultPath); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	keygenPath, err := ageutil.KeygenPath(cfg.AgePath)
	if err != nil {
		return err
	}
	if err := ensureDefaultDevice(keygenPath); err != nil {
		return err
	}
	if err := migrateRecoveryKey(); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println("Workspace:", cfg.DataRoot)
	fmt.Println()
	fmt.Println("Use:")
	fmt.Println("  edrive open")
	fmt.Println("  edrive cd")
	fmt.Println("  edrive backup")
	return nil
}

func chooseWorkspace(cfg *config.Config, migrated bool) error {
	if cfg.ConfigFound || migrated || cfg.DataRoot != config.DefaultDataRoot() {
		fmt.Println("Reusing workspace:", cfg.DataRoot)
		return nil
	}

	answer, err := askPath("Where should edrive keep the working folder? ["+cfg.DataRoot+"]: ", cfg.DataRoot)
	if err != nil {
		return err
	}
	cfg.DataRoot = answer
	return nil
}

func ensureWorkspace(path string, existingConfig bool) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if mounted(path) {
		return nil
	}
	if existingConfig {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("workspace must be empty before first setup: %s", path)
	}
	return nil
}

func ensureDependencies(cfg *config.Config) error {
	agePath, err := ensureAge()
	if err != nil {
		return err
	}
	zstdPath, err := ensureExactTool("zstd", toolchain.ZstdVersion)
	if err != nil {
		return err
	}
	cliPath, err := findCryptomatorCLI(cfg)
	if err != nil {
		return err
	}

	cfg.AgePath = agePath
	cfg.ZstdPath = zstdPath
	cfg.CryptomatorCLI = cliPath

	if !isDir("/Applications/Google Drive.app") {
		return fmt.Errorf("Google Drive for desktop is required")
	}
	if !brewCaskInstalled("fuse-t") {
		return fmt.Errorf("FUSE-T is required")
	}
	if !isDir("/Applications/Cryptomator.app") {
		return fmt.Errorf("Cryptomator is required")
	}
	return nil
}

func ensureAge() (string, error) {
	if path, err := exec.LookPath("age"); err == nil && toolVersionMatches(path, toolchain.AgeVersion) {
		return path, nil
	}

	path := filepath.Join(config.ToolsDir(), "age", toolchain.AgeVersion, "age")
	if isExecutable(path) && toolVersionMatches(path, toolchain.AgeVersion) {
		return path, nil
	}

	return "", fmt.Errorf("age %s is required; install that exact version and run setup again", toolchain.AgeVersion)
}

func ensureExactTool(name, version string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s %s is required; install that exact version and run setup again", name, version)
	}
	if !toolVersionMatches(path, version) {
		return "", fmt.Errorf("%s version mismatch: edrive requires %s", name, version)
	}
	return path, nil
}

func findCryptomatorCLI(cfg *config.Config) (string, error) {
	candidates := []string{}
	if cfg.CryptomatorCLI != "" {
		candidates = append(candidates, cfg.CryptomatorCLI)
	}
	home, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(config.ToolsDir(), "cryptomator-cli", toolchain.CryptomatorCLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
		filepath.Join(home, "Desktop/local-infra/tools/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
	)
	for _, path := range unique(candidates) {
		if isExecutable(path) && toolVersionMatches(path, toolchain.CryptomatorCLIVersion) {
			return path, nil
		}
	}
	return "", fmt.Errorf("Cryptomator CLI %s is required; install the pinned CLI and run setup again", toolchain.CryptomatorCLIVersion)
}

func ensureVault(vaultPath string) error {
	marker := filepath.Join(vaultPath, "vault.cryptomator")
	if isFile(marker) {
		if _, err := cryptomator.DiscoverVaultID(vaultPath); err == nil {
			return nil
		}
	}

	if err := os.MkdirAll(vaultPath, 0700); err != nil {
		return fmt.Errorf("create encrypted storage folder: %w", err)
	}

	_ = ui.OpenApplication("Cryptomator")
	fmt.Println()
	fmt.Println("Cryptomator needs this one-time action:")
	fmt.Println("create or add the edrive vault using this folder:")
	fmt.Println(vaultPath)
	fmt.Println()
	fmt.Print("Press Enter after the vault is ready: ")
	if _, err := input.ReadString('\n'); err != nil && err != io.EOF {
		return err
	}

	if !isFile(marker) {
		return fmt.Errorf("edrive vault was not created")
	}
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
		return fmt.Errorf("edrive vault is not registered with Cryptomator")
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
			return fmt.Errorf("device Keychain access was not granted")
		}
		_, err = device.Import("mac-1", identity, ageKeygen)
		return err
	}
	if keychain.LegacyIdentityExists("device-identity") {
		value, err := keychain.GetLegacyIdentity("device-identity")
		if err != nil {
			return fmt.Errorf("legacy device Keychain access was not granted")
		}
		if err := keychain.SetIdentity("mac-1", value); err != nil {
			return err
		}
		_ = keychain.DeleteLegacyIdentity("device-identity")
		_, err = device.Import("mac-1", value, ageKeygen)
		return err
	}

	home, _ := os.UserHomeDir()
	legacyPath := filepath.Join(home, "Library/Application Support/edrive/identities/mac.identity")
	if b, err := os.ReadFile(legacyPath); err == nil && strings.TrimSpace(string(b)) != "" {
		_, err = device.Import("mac-1", string(b), ageKeygen)
		return err
	}

	_, err := device.AddGenerated("mac-1", ageKeygen)
	return err
}

func migrateRecoveryKey() error {
	if keychain.RecoveryExists() {
		return nil
	}
	if keychain.LegacyRecoveryExists() {
		value, err := keychain.GetLegacyRecovery()
		if err != nil {
			return fmt.Errorf("legacy recovery Keychain access was not granted")
		}
		if err := keychain.SetRecovery(value); err != nil {
			return err
		}
		_ = keychain.DeleteLegacyRecovery()
		return nil
	}

	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, "Desktop/local-infra/configs/edrive/edrive-recovery-identity.txt"),
		filepath.Join(home, "Desktop/local-infra/edrive/edrive-recovery-identity.txt"),
	}
	for _, path := range paths {
		if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
			return keychain.SetRecovery(string(b))
		}
	}
	return nil
}

func seedFromLegacy(cfg *config.Config, values map[string]string) bool {
	migrated := false
	if cfg.DataRoot == config.DefaultDataRoot() {
		if value := config.Expand(values["EDRIVE_MOUNT"]); value != "" {
			cfg.DataRoot = value
			migrated = true
		}
	}
	if cfg.CryptomatorCLI == "" {
		if value := config.Expand(values["EDRIVE_CRYPTOMATOR_CLI"]); value != "" {
			cfg.CryptomatorCLI = value
			migrated = true
		}
	}
	if cfg.StorageRoot == "" {
		if value := config.Expand(values["EDRIVE_GOOGLE_DRIVE_ROOT"]); value != "" {
			cfg.StorageRoot = value
			migrated = true
		} else if value := config.Expand(values["EDRIVE_CRYPTOMATOR_VAULT"]); value != "" {
			cfg.StorageRoot = filepath.Dir(value)
			migrated = true
		}
	}
	return migrated
}

func legacyValues() map[string]string {
	values := make(map[string]string)
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(home, "Library/Application Support/edrive/config.sh"),
		filepath.Join(home, "Desktop/local-infra/edrive/config.sh"),
		filepath.Join(home, "Desktop/local-infra/configs/edrive/config.sh"),
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			values[key] = os.ExpandEnv(value)
		}
	}
	return values
}

func askPath(prompt, fallback string) (string, error) {
	fmt.Print(prompt)
	line, err := input.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		answer = fallback
	}
	if answer == "" {
		return "", fmt.Errorf("path is required")
	}
	return config.Expand(answer), nil
}

func mounted(path string) bool {
	out, err := exec.Command("/sbin/mount").Output()
	if err != nil {
		return false
	}
	return bytes.Contains(out, []byte(" on "+filepath.Clean(path)+" ("))
}

func brewCaskInstalled(name string) bool {
	return exec.Command("brew", "list", "--cask", name).Run() == nil
}

func toolVersionMatches(path, version string) bool {
	out, err := exec.Command(path, "--version").CombinedOutput()
	return err == nil && bytes.Contains(out, []byte(version))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func unique(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
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
