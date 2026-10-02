package setup

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/cryptomator"
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/provider"
	"github.com/faiz/edrive/internal/ui"
)

const (
	ageVersion            = "1.3.2"
	zstdVersion           = "1.5.7"
	cryptomatorCLIVersion = "0.6.2"
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

	if !cfg.ConfigFound {
		migrated, err := migrateLegacyState(&cfg)
		if err != nil {
			return err
		}
		if migrated {
			fmt.Println("✓ existing edrive state found and reused")
		}
	}

	dataRoot, err := chooseDataRoot(cfg)
	if err != nil {
		return err
	}

	cfg.DataRoot = config.Expand(filepath.Clean(dataRoot))
	cfg.StorageProvider = "google-drive"
	cfg.StorageName = "edrive"
	cfg.Recipients = config.DefaultRecipients()

	if err := os.MkdirAll(config.DefaultHome(), 0700); err != nil {
		return fmt.Errorf("create edrive control state: %w", err)
	}
	if err := os.MkdirAll(config.DefaultTempDir(), 0700); err != nil {
		return fmt.Errorf("create edrive temporary state: %w", err)
	}
	if err := ensureWorkspace(&cfg); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Preparing edrive...")

	if err := ensureSystemDependencies(&cfg); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	driveRoot, err := ensureGoogleDrive()
	if err != nil {
		return err
	}

	vaultPath := filepath.Join(driveRoot, cfg.StorageName)
	if err := ensureVault(vaultPath); err != nil {
		return err
	}

	if err := ensureIdentities(&cfg); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
		return fmt.Errorf("encrypted workspace is not registered with Cryptomator")
	}

	fmt.Println()
	fmt.Println("✓ workspace:", cfg.DataRoot)
	fmt.Println("✓ storage: Google Drive")
	fmt.Println("✓ encrypted vault: ready")
	fmt.Println("✓ device identity: protected by macOS Keychain")
	fmt.Println("✓ recovery identity: protected by macOS Keychain")
	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println()
	fmt.Println("Use:")
	fmt.Println("  edrive open")
	fmt.Println("  edrive cd")
	fmt.Println("  edrive backup")
	return nil
}

func chooseDataRoot(cfg config.Config) (string, error) {
	if cfg.ConfigFound && strings.TrimSpace(cfg.DataRoot) != "" {
		fmt.Println("✓ reusing workspace:", cfg.DataRoot)
		return cfg.DataRoot, nil
	}
	return askPath("Where should edrive keep its working folder? ["+cfg.DataRoot+"]: ", cfg.DataRoot)
}

func ensureWorkspace(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.DataRoot, 0700); err != nil {
		return fmt.Errorf("create edrive workspace: %w", err)
	}

	entries, err := os.ReadDir(cfg.DataRoot)
	if err != nil {
		return fmt.Errorf("read edrive workspace: %w", err)
	}
	if len(entries) == 0 || mountedByOS(cfg.DataRoot) {
		return nil
	}
	return fmt.Errorf("edrive working folder must be empty before first setup")
}

func ensureSystemDependencies(cfg *config.Config) error {
	fmt.Println("✓ checking fixed edrive toolchain")

	agePath, err := ensureAge()
	if err != nil {
		return err
	}
	zstdPath, err := ensureZstd()
	if err != nil {
		return err
	}
	cliPath, err := ensureCryptomatorCLI()
	if err != nil {
		return err
	}

	cfg.AgePath = agePath
	cfg.ZstdPath = zstdPath
	cfg.CryptomatorCLI = cliPath

	if !isDir("/Applications/Google Drive.app") {
		ok, err := askYesNo("Google Drive for desktop is missing. Install it with Homebrew? [y/N] ", false)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Google Drive for desktop is required")
		}
		if err := brewInstallCask("google-drive"); err != nil {
			return err
		}
	}

	if !brewCaskInstalled("fuse-t") {
		ok, err := askYesNo("FUSE-T is missing. Install it with Homebrew? [y/N] ", false)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("FUSE-T is required")
		}
		if err := brewInstallCask("fuse-t"); err != nil {
			return err
		}
	}

	return nil
}

func ensureAge() (string, error) {
	if path, err := exec.LookPath("age"); err == nil && versionMatches(path, ageVersion) {
		return path, nil
	}

	target := filepath.Join(config.DefaultToolsDir(), "age", ageVersion, "age")
	if isExecutableFile(target) && versionMatches(target, ageVersion) {
		return target, nil
	}

	ok, err := askYesNo("Pinned age 1.3.2 is not available. Download it now? [y/N] ", false)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("age %s is required", ageVersion)
	}
	return downloadAge()
}

func downloadAge() (string, error) {
	url := "https://github.com/FiloSottile/age/releases/download/v1.3.2/age-v1.3.2-darwin-arm64.tar.gz"
	expectedSHA := "e2020b073c44f692685a24d6abc378817eb81ffaaf49fd0531ef8565f767f2f5"
	targetDir := filepath.Join(config.DefaultToolsDir(), "age", ageVersion)

	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(config.DefaultTempDir(), 0700); err != nil {
		return "", err
	}

	tmpDir, err := os.MkdirTemp(config.DefaultTempDir(), ".age-download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	archive := filepath.Join(tmpDir, "age.tar.gz")
	if err := downloadFile(url, archive); err != nil {
		return "", err
	}
	if err := verifySHA256(archive, expectedSHA); err != nil {
		return "", fmt.Errorf("verify pinned age asset: %w", err)
	}
	if err := extractAge(archive, targetDir); err != nil {
		return "", err
	}

	path := filepath.Join(targetDir, "age")
	keygen := filepath.Join(targetDir, "age-keygen")
	if !isExecutableFile(path) || !isExecutableFile(keygen) {
		return "", fmt.Errorf("pinned age %s asset is incomplete", ageVersion)
	}
	if !versionMatches(path, ageVersion) || !versionMatches(keygen, ageVersion) {
		return "", fmt.Errorf("pinned age %s asset failed version verification", ageVersion)
	}
	return path, nil
}

func ensureZstd() (string, error) {
	if path, err := exec.LookPath("zstd"); err == nil && versionMatches(path, zstdVersion) {
		return path, nil
	}

	ok, err := askYesNo("Pinned zstd 1.5.7 is not available. Install zstd with Homebrew? [y/N] ", false)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("zstd %s is required", zstdVersion)
	}
	if err := brewInstall("zstd"); err != nil {
		return "", err
	}

	path, err := exec.LookPath("zstd")
	if err != nil {
		return "", fmt.Errorf("zstd is not available after installation")
	}
	if !versionMatches(path, zstdVersion) {
		return "", fmt.Errorf("zstd version mismatch: edrive requires %s", zstdVersion)
	}
	return path, nil
}

func ensureCryptomatorCLI() (string, error) {
	candidates := []string{
		filepath.Join(config.DefaultToolsDir(), "cryptomator-cli", cryptomatorCLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
	}
	if legacy := legacyValue("EDRIVE_CRYPTOMATOR_CLI"); legacy != "" {
		candidates = append([]string{legacy}, candidates...)
	}
	if path, err := exec.LookPath("cryptomator-cli"); err == nil {
		candidates = append(candidates, path)
	}

	for _, path := range unique(candidates) {
		if isExecutableFile(path) {
			fmt.Println("✓ Cryptomator CLI:", path)
			return path, nil
		}
	}

	ok, err := askYesNo("Pinned Cryptomator CLI 0.6.2 is missing. Download it now? [y/N] ", false)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("Cryptomator CLI %s is required", cryptomatorCLIVersion)
	}
	return downloadCryptomatorCLI()
}

func downloadCryptomatorCLI() (string, error) {
	url := "https://github.com/cryptomator/cli/releases/download/0.6.2/cryptomator-cli-0.6.2-mac-x64.zip"
	targetDir := filepath.Join(config.DefaultToolsDir(), "cryptomator-cli", cryptomatorCLIVersion)

	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(config.DefaultTempDir(), 0700); err != nil {
		return "", err
	}

	tmpDir, err := os.MkdirTemp(config.DefaultTempDir(), ".cryptomator-cli-download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	archive := filepath.Join(tmpDir, "cryptomator-cli.zip")
	if err := downloadFile(url, archive); err != nil {
		return "", err
	}
	if err := unzipSafe(archive, targetDir); err != nil {
		return "", fmt.Errorf("extract Cryptomator CLI %s: %w", cryptomatorCLIVersion, err)
	}

	path := filepath.Join(targetDir, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli")
	if !isExecutableFile(path) {
		return "", fmt.Errorf("pinned Cryptomator CLI %s asset is incomplete", cryptomatorCLIVersion)
	}
	if err := os.Chmod(path, 0700); err != nil {
		return "", err
	}
	fmt.Println("✓ Cryptomator CLI:", path)
	return path, nil
}

func ensureGoogleDrive() (string, error) {
	p := provider.GoogleDrive{StorageName: "edrive"}
	root, err := p.Root()
	if err == nil {
		return root, nil
	}
	if strings.Contains(err.Error(), "multiple Google Drive") {
		return "", err
	}

	_ = provider.EnsureRunning()
	for i := 0; i < 20; i++ {
		root, err = p.Root()
		if err == nil {
			return root, nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("Google Drive local storage is unavailable")
}

func ensureVault(vaultPath string) error {
	marker := filepath.Join(vaultPath, "vault.cryptomator")
	if fileExists(marker) {
		if _, err := cryptomator.DiscoverVaultID(vaultPath); err == nil {
			return nil
		}
		_ = ui.OpenApplication("Cryptomator")
		fmt.Println("The encrypted workspace exists but is not registered yet.")
		fmt.Println("Cryptomator will open so it can be added.")
		fmt.Print("Press Enter after the workspace has been added to Cryptomator: ")
		if _, err := input.ReadString('
'); err != nil && err != io.EOF {
			return err
		}
		if _, err := cryptomator.DiscoverVaultID(vaultPath); err == nil {
			return nil
		}
		return fmt.Errorf("encrypted workspace is still not registered with Cryptomator")
	}

	if err := os.MkdirAll(vaultPath, 0700); err != nil {
		return fmt.Errorf("create encrypted storage folder: %w", err)
	}
	if !isDir("/Applications/Cryptomator.app") {
		ok, err := askYesNo("Cryptomator is missing. Install it with Homebrew? [y/N] ", false)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Cryptomator is required")
		}
		if err := brewInstallCask("cryptomator"); err != nil {
			return err
		}
	}

	_ = ui.OpenApplication("Cryptomator")
	fmt.Println("Creating the encrypted workspace...")
	fmt.Println("Use the edrive folder inside your Google Drive storage.")
	fmt.Print("Press Enter after the encrypted workspace has been created: ")
	if _, err := input.ReadString('
'); err != nil && err != io.EOF {
		return err
	}

	if !fileExists(marker) {
		return fmt.Errorf("encrypted workspace was not created")
	}
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
		return fmt.Errorf("encrypted workspace was created but not registered")
	}
	return nil
}

func ensureIdentities(cfg *config.Config) error {
	keygenPath, err := ageKeygenPath(cfg.AgePath)
	if err != nil {
		return err
	}

	fmt.Println("macOS may ask for Keychain permission.")
	device, err := ensureIdentity(keychain.DeviceIdentity, legacyValue("EDRIVE_MAC_IDENTITY"), keygenPath)
	if err != nil {
		return err
	}
	recovery, err := ensureIdentity(keychain.RecoveryIdentity, legacyValue("EDRIVE_RECOVERY_IDENTITY"), keygenPath)
	if err != nil {
		return err
	}

	deviceRecipient, err := recipientFromIdentity(device, keygenPath)
	if err != nil {
		return err
	}
	recoveryRecipient, err := recipientFromIdentity(recovery, keygenPath)
	if err != nil {
		return err
	}

	if err := writeRecipients(cfg.Recipients, deviceRecipient, recoveryRecipient); err != nil {
		return err
	}
	return nil
}

func ensureIdentity(account, legacyPath, keygenPath string) (string, error) {
	if value, err := keychain.Get(account); err == nil {
		return value, nil
	}

	if legacyPath != "" && fileExists(legacyPath) {
		b, err := os.ReadFile(legacyPath)
		if err != nil {
			return "", err
		}
		value := strings.TrimSpace(string(b))
		if value != "" {
			if err := keychain.Set(account, value); err != nil {
				return "", err
			}
			return value, nil
		}
	}

	value, err := generateAgeIdentity(keygenPath)
	if err != nil {
		return "", err
	}
	if err := keychain.Set(account, value); err != nil {
		return "", err
	}
	return value, nil
}

func generateAgeIdentity(keygenPath string) (string, error) {
	tmpDir, err := os.MkdirTemp(config.DefaultTempDir(), ".identity-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "identity")
	cmd := exec.Command(keygenPath, "-pq", "-o", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("generate age identity: %w", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func ageKeygenPath(agePath string) (string, error) {
	if agePath == "" {
		return "", fmt.Errorf("age is not configured")
	}
	path := filepath.Join(filepath.Dir(agePath), "age-keygen")
	if !isExecutableFile(path) {
		return "", fmt.Errorf("age-keygen is unavailable")
	}
	return path, nil
}

func recipientFromIdentity(identity, keygenPath string) (string, error) {
	tmpDir, err := os.MkdirTemp(config.DefaultTempDir(), ".recipient-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "identity")
	if err := os.WriteFile(path, []byte(identity+"\n"), 0600); err != nil {
		return "", err
	}
	out, err := exec.Command(keygenPath, "-y", path).Output()
	if err != nil {
		return "", fmt.Errorf("derive age recipient: %w", err)
	}
	recipient := strings.TrimSpace(string(out))
	if !strings.HasPrefix(recipient, "age1") {
		return "", fmt.Errorf("invalid age recipient")
	}
	return recipient, nil
}

func writeRecipients(path string, recipients ...string) error {
	seen := make(map[string]struct{}, len(recipients))
	var lines []string
	for _, recipient := range recipients {
		recipient = strings.TrimSpace(recipient)
		if !strings.HasPrefix(recipient, "age1") {
			return fmt.Errorf("invalid age recipient")
		}
		if _, ok := seen[recipient]; ok {
			continue
		}
		seen[recipient] = struct{}{}
		lines = append(lines, recipient)
	}

	if len(lines) == 0 {
		return fmt.Errorf("no age recipients available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	return nil
}

func migrateLegacyState(cfg *config.Config) (bool, error) {
	found := false
	if value := legacyValue("EDRIVE_DATA_ROOT"); value != "" {
		cfg.DataRoot = value
		found = true
	}
	if value := legacyValue("EDRIVE_CRYPTOMATOR_CLI"); value != "" {
		cfg.CryptomatorCLI = value
		found = true
	}
	return found, nil
}

func legacyValue(key string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, "Library", "Application Support", "edrive", "config.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	prefix := key + "="
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return config.Expand(strings.Trim(value, "\""))
	}
	return ""
}

func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download dependency: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download dependency returned HTTP %d", resp.StatusCode)
	}

	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("save dependency: %v %v", copyErr, closeErr)
	}
	return nil
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha256 mismatch: got %s", got)
	}
	return nil
}

func extractAge(archive, destination string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe age archive path")
		}
		base := filepath.Base(name)
		if base != "age" && base != "age-keygen" {
			continue
		}

		target := filepath.Join(destination, base)
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf("extract age executable: %v %v", copyErr, closeErr)
		}
	}
}

func unzipSafe(zipPath, destination string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	root := filepath.Clean(destination)
	for _, file := range zr.File {
		name := filepath.Clean(file.Name)
		if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe zip path")
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in release archive")
		}

		target := filepath.Join(root, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}

		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		_ = in.Close()
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf("extract release file: %v %v", copyErr, closeErr)
		}
	}
	return nil
}

func brewInstall(name string) error {
	cmd := exec.Command("brew", "install", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	return nil
}

func brewInstallCask(name string) error {
	cmd := exec.Command("brew", "install", "--cask", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	return nil
}

func brewCaskInstalled(name string) bool {
	return exec.Command("brew", "list", "--cask", name).Run() == nil
}

func versionMatches(path, version string) bool {
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

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func mountedByOS(path string) bool {
	out, err := exec.Command("mount").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), " on "+filepath.Clean(path)+" (")
}

func unique(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func askPath(prompt, defaultValue string) (string, error) {
	fmt.Print(prompt)
	line, err := input.ReadString('
')
	if err != nil && err != io.EOF {
		return "", err
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		answer = defaultValue
	}
	if answer == "" {
		return "", fmt.Errorf("path is required")
	}
	return config.Expand(answer), nil
}

func askYesNo(prompt string, defaultYes bool) (bool, error) {
	fmt.Print(prompt)
	line, err := input.ReadString('
')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	if answer == "" {
		return defaultYes, nil
	}
	switch answer {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("please answer yes or no")
	}
}

func Remove() error {
	if _, err := config.Load(config.DefaultPath()); err != nil {
		return err
	}
	fmt.Println("This removes edrive's private control state.")
	fmt.Println("It does not remove your working folder, Google Drive data, or Keychain identities.")
	ok, err := askYesNo("Continue? [y/N] ", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}
	return os.RemoveAll(config.DefaultHome())
}

func Purge() error {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}
	if mountedByOS(cfg.DataRoot) {
		return fmt.Errorf("edrive is currently open; close it before purge")
	}

	fmt.Println("WARNING: purge removes edrive's private control state and Keychain identities.")
	fmt.Println("It does not remove your working folder or Google Drive data.")
	fmt.Print("Type PURGE to continue: ")
	line, err := input.ReadString('
')
	if err != nil && err != io.EOF {
		return err
	}
	if strings.TrimSpace(line) != "PURGE" {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := os.RemoveAll(config.DefaultHome()); err != nil {
		return fmt.Errorf("purge edrive state: %w", err)
	}
	if err := keychain.Delete(keychain.DeviceIdentity); err != nil {
		return err
	}
	if err := keychain.Delete(keychain.RecoveryIdentity); err != nil {
		return err
	}
	return nil
}
