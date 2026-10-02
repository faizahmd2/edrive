package setup

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"time"

	"github.com/faiz/edrive/internal/config"
)

const (
	cryptomatorMounter = "org.cryptomator.frontend.fuse.mount.FuseTMountProvider"
	keychainService    = "Cryptomator"
)

type cryptomatorSettings struct {
	Directories []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"directories"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

var input = bufio.NewReader(os.Stdin)

func Run() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("setup currently supports macOS")
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}

	dataRoot, err := chooseDataRoot(cfg)
	if err != nil {
		return err
	}
	applyDataLayout(&cfg, dataRoot)

	if err := writeConfig(cfg); err != nil {
		return err
	}

	fmt.Println("✓ data root:", cfg.DataRoot)
	fmt.Println("  Google Drive mirror:", cfg.GoogleDriveRoot)
	fmt.Println("  Mounted vault:", cfg.Mount)
	fmt.Println("  Recovery:", cfg.RecoveryDir)

	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("Homebrew is required before setup can install dependencies")
	}

	fmt.Println()
	fmt.Println("STEP 1/6  Dependencies")
	for _, name := range []string{"age", "zstd"} {
		if err := ensureFormula(name); err != nil {
			return err
		}
	}
	if err := ensureCask("fuse-t", nil); err != nil {
		return err
	}
	if err := ensureCask("google-drive", []string{"/Applications/Google Drive.app"}); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("STEP 2/6  Google Drive")
	if !cfg.GoogleDriveReady {
		if err := configureGoogleDrive(cfg.GoogleDriveRoot); err != nil {
			return err
		}
		cfg.GoogleDriveReady = true
		if err := writeConfig(cfg); err != nil {
			return err
		}
	} else {
		if !isDir(cfg.GoogleDriveRoot) {
			cfg.GoogleDriveReady = false
			_ = writeConfig(cfg)
			return fmt.Errorf("Google Drive mirror directory is unavailable: %s", cfg.GoogleDriveRoot)
		}
		fmt.Println("✓ Google Drive setup already recorded")
	}

	fmt.Println()
	fmt.Println("STEP 3/6  Cryptomator CLI")
	cliPath := cfg.CryptomatorCLI
	if !isExecutableFile(cliPath) {
		found, _ := findCryptomatorCLI(runtimeToolsDir())
		if found != "" {
			cliPath = found
			cfg.CryptomatorCLI = found
			if err := writeConfig(cfg); err != nil {
				return err
			}
			fmt.Println("✓ existing Cryptomator CLI:", found)
		} else {
			ok, err := askYesNo("Cryptomator CLI is missing. Download the official CLI now? [y/N] ", false)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("Cryptomator CLI is required. Install it, then run edrive setup again")
			}
			found, err = downloadCryptomatorCLI(runtimeToolsDir())
			if err != nil {
				return err
			}
			cfg.CryptomatorCLI = found
			cliPath = found
			if err := writeConfig(cfg); err != nil {
				return err
			}
		}
	}
	fmt.Println("✓ Cryptomator CLI:", cliPath)

	fmt.Println()
	fmt.Println("STEP 4/6  Identities")
	if err := ensureMacIdentity(&cfg); err != nil {
		return err
	}
	if err := ensureRecoveryIdentity(&cfg); err != nil {
		return err
	}

	macRecipient, err := ageRecipient(cfg.MacIdentity)
	if err != nil {
		return fmt.Errorf("read Mac recipient: %w", err)
	}
	recoveryRecipient, err := ageRecipient(cfg.RecoveryIdentity)
	if err != nil {
		return fmt.Errorf("read Recovery recipient: %w", err)
	}
	if err := ensureRecipients(cfg.Recipients, macRecipient, recoveryRecipient); err != nil {
		return err
	}
	if err := writeConfig(cfg); err != nil {
		return err
	}
	fmt.Println("✓ recipients:", cfg.Recipients)

	fmt.Println()
	fmt.Println("STEP 5/6  Cryptomator vault")
	complete, err := ensureVault(&cfg)
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("vault setup is incomplete; run edrive setup again after the requested Cryptomator step")
	}
	if err := writeConfig(cfg); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("STEP 6/6  Final verification")
	fmt.Println("✓ configuration written:", cfg.ConfigPath)
	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  edrive doctor")
	fmt.Println("  edrive unlock")
	return nil
}

func chooseDataRoot(cfg config.Config) (string, error) {
	if cfg.ConfigFound && strings.TrimSpace(cfg.DataRoot) != "" {
		fmt.Println("✓ reusing data root:", cfg.DataRoot)
		return cfg.DataRoot, nil
	}
	return askPath("Where should edrive keep its local data? ["+cfg.DataRoot+"]: ", cfg.DataRoot)
}

func applyDataLayout(cfg *config.Config, dataRoot string) {
	cfg.DataRoot = config.Expand(filepath.Clean(dataRoot))
	cfg.GoogleDriveRoot = filepath.Join(cfg.DataRoot, "google-drive-remote")
	cfg.Mount = filepath.Join(cfg.DataRoot, "edrive")
	cfg.RecoveryDir = filepath.Join(cfg.DataRoot, "recovery")
	cfg.Recipients = filepath.Join(filepath.Dir(cfg.ConfigPath), "recipients.txt")
	cfg.MacIdentity = filepath.Join(filepath.Dir(cfg.ConfigPath), "identities", "mac.identity")
	cfg.RuntimeDir = filepath.Join(filepath.Dir(cfg.ConfigPath), "runtime")
	if cfg.RecoveryIdentity == "" {
		cfg.RecoveryIdentity = config.DefaultRecoveryIdentity()
	}
	if cfg.CryptomatorKeychainService == "" {
		cfg.CryptomatorKeychainService = keychainService
	}
	cfg.CryptomatorMounter = cryptomatorMounter
	cfg.CryptomatorVault = filepath.Join(cfg.GoogleDriveRoot, "edrive")
}

func writeConfig(cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0700); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("EDRIVE_DATA_ROOT=%q", cfg.DataRoot),
		fmt.Sprintf("EDRIVE_GOOGLE_DRIVE_ROOT=%q", cfg.GoogleDriveRoot),
		fmt.Sprintf("EDRIVE_GOOGLE_DRIVE_READY=%t", cfg.GoogleDriveReady),
		fmt.Sprintf("EDRIVE_MOUNT=%q", cfg.Mount),
		fmt.Sprintf("EDRIVE_RECOVERY_DIR=%q", cfg.RecoveryDir),
		fmt.Sprintf("EDRIVE_RECIPIENTS=%q", cfg.Recipients),
		fmt.Sprintf("EDRIVE_MAC_IDENTITY=%q", cfg.MacIdentity),
		fmt.Sprintf("EDRIVE_RECOVERY_IDENTITY=%q", cfg.RecoveryIdentity),
		fmt.Sprintf("EDRIVE_SNAPSHOT_KEEP=%d", cfg.SnapshotKeep),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_VAULT=%q", cfg.CryptomatorVault),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_VAULT_ID=%q", cfg.CryptomatorVaultID),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_CLI=%q", cfg.CryptomatorCLI),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_MOUNTER=%q", cfg.CryptomatorMounter),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_KEYCHAIN_SERVICE=%q", cfg.CryptomatorKeychainService),
		fmt.Sprintf("EDRIVE_RUNTIME_DIR=%q", cfg.RuntimeDir),
	}
	if err := os.WriteFile(cfg.ConfigPath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(cfg.ConfigPath, 0600)
}

func configureGoogleDrive(mirrorRoot string) error {
	if err := os.MkdirAll(mirrorRoot, 0700); err != nil {
		return fmt.Errorf("create Google Drive mirror directory: %w", err)
	}

	fmt.Println("edrive does not manage your Google account or Google Drive permissions.")
	fmt.Println("Complete these steps in Google Drive for desktop:")
	fmt.Println("  1. Sign in with your Google account. Google may use your browser for authentication.")
	fmt.Println("  2. Open Preferences and select Folders from Drive.")
	fmt.Println("  3. Under My Drive syncing options, select Mirror files.")
	fmt.Println("  4. Set the local My Drive folder to:")
	fmt.Println("     " + mirrorRoot)
	fmt.Println("  5. Leave Google Drive running and return here.")
	fmt.Println()
	if err := exec.Command("open", "-a", "Google Drive").Run(); err != nil {
		fmt.Println("Could not open Google Drive automatically. Open it manually.")
	}
	_ = exec.Command("open", "https://drive.google.com").Run()
	fmt.Print("Press Enter after Google Drive is signed in and configured: ")
	if _, err := input.ReadString('\n'); err != nil && err != io.EOF {
		return err
	}

	if !isDir(mirrorRoot) {
		return fmt.Errorf("Google Drive mirror directory is not available: %s", mirrorRoot)
	}
	fmt.Println("✓ Google Drive local folder:", mirrorRoot)
	fmt.Println("  edrive does not claim remote sync is complete; Google Drive owns sync state.")
	return nil
}

func ensureFormula(name string) error {
	if _, err := exec.LookPath(name); err == nil {
		fmt.Printf("✓ %s\n", name)
		return nil
	}
	ok, err := askYesNo(fmt.Sprintf("%s is not installed. Install it with Homebrew? [y/N] ", name), false)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required. Install it, then run edrive setup again", name)
	}
	cmd := exec.Command("brew", "install", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	fmt.Printf("✓ %s\n", name)
	return nil
}

func ensureCask(name string, appPaths []string) error {
	if len(appPaths) > 0 {
		for _, appPath := range appPaths {
			if isDir(appPath) {
				fmt.Printf("✓ %s (%s)\n", name, appPath)
				return nil
			}
		}
	} else if err := exec.Command("brew", "list", "--cask", name).Run(); err == nil {
		fmt.Printf("✓ %s\n", name)
		return nil
	}

	ok, err := askYesNo(fmt.Sprintf("%s is not installed. Install it with Homebrew? [y/N] ", name), false)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s is required. Install it, then run edrive setup again", name)
	}

	cmd := exec.Command("brew", "install", "--cask", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	fmt.Printf("✓ %s\n", name)
	return nil
}

func ensureMacIdentity(cfg *config.Config) error {
	if isReadableIdentity(cfg.MacIdentity) {
		_ = os.Chmod(cfg.MacIdentity, 0600)
		fmt.Println("✓ Mac identity:", cfg.MacIdentity)
		return nil
	}
	ok, err := askYesNo(fmt.Sprintf("Mac identity is missing at %s. Generate a new identity? [y/N] ", cfg.MacIdentity), false)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("Mac identity is required. Generate/install it, then run edrive setup again")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.MacIdentity), 0700); err != nil {
		return err
	}
	cmd := exec.Command("age-keygen", "-pq", "-o", cfg.MacIdentity)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate Mac identity: %w", err)
	}
	if err := os.Chmod(cfg.MacIdentity, 0600); err != nil {
		return err
	}
	fmt.Println("✓ Mac identity generated:", cfg.MacIdentity)
	return nil
}

func ensureRecoveryIdentity(cfg *config.Config) error {
	if isReadableIdentity(cfg.RecoveryIdentity) {
		_ = os.Chmod(cfg.RecoveryIdentity, 0600)
		fmt.Println("✓ Recovery identity:", cfg.RecoveryIdentity)
		return nil
	}

	fmt.Println("Recovery identity is not found.")
	fmt.Println("This identity is intentionally kept outside edrive's normal data root.")
	path, err := askPath("Path to the existing recovery identity ["+cfg.RecoveryIdentity+"]: ", cfg.RecoveryIdentity)
	if err != nil {
		return err
	}
	if !isReadableIdentity(path) {
		return fmt.Errorf("recovery identity is required and must be a readable age identity")
	}
	cfg.RecoveryIdentity = path
	_ = os.Chmod(cfg.RecoveryIdentity, 0600)
	fmt.Println("✓ Recovery identity:", cfg.RecoveryIdentity)
	return nil
}

func ensureRecipients(path string, recipients ...string) error {
	var lines []string
	seen := make(map[string]bool)

	if b, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "#") {
				lines = append(lines, line)
				continue
			}
			if !strings.HasPrefix(line, "age1") {
				return fmt.Errorf("invalid recipient in %s: %q", path, line)
			}
			if !seen[line] {
				seen[line] = true
				lines = append(lines, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, recipient := range recipients {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" || seen[recipient] {
			continue
		}
		if !strings.HasPrefix(recipient, "age1") {
			return fmt.Errorf("invalid age recipient: %q", recipient)
		}
		seen[recipient] = true
		lines = append(lines, recipient)
	}

	if len(lines) == 0 {
		return fmt.Errorf("no age recipients available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return fmt.Errorf("write recipients file: %w", err)
	}
	return os.Chmod(path, 0600)
}

func ensureVault(cfg *config.Config) (bool, error) {
	vault := cfg.CryptomatorVault
	marker := filepath.Join(vault, "vault.cryptomator")

	if !isDir(vault) || !fileExists(marker) {
		fmt.Println("Cryptomator vault is not available at:")
		fmt.Println("  " + vault)
		fmt.Println()
		fmt.Println("edrive does not create or configure a vault password.")
		fmt.Println("The one-time vault creation/registration step belongs to Cryptomator.")
		fmt.Println()

		if !isDir("/Applications/Cryptomator.app") {
			ok, err := askYesNo("Cryptomator desktop app is needed to create this vault. Install it with Homebrew? [y/N] ", false)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, fmt.Errorf("create the vault with Cryptomator, then run edrive setup again")
			}
			if err := installCask("cryptomator"); err != nil {
				return false, err
			}
		}

		_ = exec.Command("open", "-a", "Cryptomator").Run()
		fmt.Println("Create or add the vault exactly at:")
		fmt.Println("  " + vault)
		fmt.Println("Store the vault password in the macOS Keychain.")
		fmt.Println()
		fmt.Print("Press Enter after the vault has been created/added: ")
		if _, err := input.ReadString('\n'); err != nil && err != io.EOF {
			return false, err
		}
		if !isDir(vault) || !fileExists(marker) {
			return false, fmt.Errorf("Cryptomator vault is still missing at %s", vault)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	settingsPath := filepath.Join(home, "Library/Application Support/Cryptomator/settings.json")
	vaultID, err := findVaultID(settingsPath, vault, home)
	if err != nil {
		fmt.Println()
		fmt.Println("The vault exists, but Cryptomator has not registered it yet.")
		fmt.Println("Add this vault to Cryptomator:")
		fmt.Println("  " + vault)
		fmt.Println("Then run edrive setup again.")
		return false, nil
	}

	cfg.CryptomatorVaultID = vaultID
	cfg.CryptomatorMounter = cryptomatorMounter
	cfg.CryptomatorKeychainService = keychainService
	fmt.Println("✓ Cryptomator vault:", vault)
	fmt.Println("✓ Vault ID:", vaultID)
	return true, nil
}

func findVaultID(settingsPath, vaultPath, home string) (string, error) {
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", err
	}
	var settings cryptomatorSettings
	if err := json.Unmarshal(b, &settings); err != nil {
		return "", err
	}
	target := canonicalPath(vaultPath)
	for _, entry := range settings.Directories {
		if entry.ID == "" || entry.Path == "" {
			continue
		}
		path := entry.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(home, path)
		}
		if canonicalPath(path) == target {
			return entry.ID, nil
		}
	}
	return "", fmt.Errorf("vault not registered in Cryptomator settings")
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

func findCryptomatorCLI(toolsDir string) (string, error) {
	candidates := []string{
		filepath.Join(toolsDir, "cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
		filepath.Join(toolsDir, "cryptomator-cli", "cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
	}
	if path, err := exec.LookPath("cryptomator-cli"); err == nil {
		candidates = append([]string{path}, candidates...)
	}
	for _, candidate := range candidates {
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Cryptomator CLI not found")
}

func downloadCryptomatorCLI(toolsDir string) (string, error) {
	if err := os.MkdirAll(toolsDir, 0700); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/cryptomator/cli/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "edrive-setup")

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch Cryptomator CLI release metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Cryptomator CLI release lookup returned HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode Cryptomator CLI release metadata: %w", err)
	}

	suffix := "-mac-arm64.zip"
	if runtime.GOARCH == "amd64" {
		suffix = "-mac-x86_64.zip"
	}

	var assetURL string
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, suffix) {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("no macOS Cryptomator CLI asset found for %s", suffix)
	}

	resp, err = client.Get(assetURL)
	if err != nil {
		return "", fmt.Errorf("download Cryptomator CLI: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Cryptomator CLI download returned HTTP %d", resp.StatusCode)
	}

	tempDir, err := os.MkdirTemp(toolsDir, ".cryptomator-cli-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	zipPath := filepath.Join(tempDir, "cryptomator-cli.zip")
	out, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}

	targetDir := filepath.Join(toolsDir, "cryptomator-cli", release.TagName)
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", err
	}
	if err := unzipSafe(zipPath, targetDir); err != nil {
		return "", fmt.Errorf("extract Cryptomator CLI: %w", err)
	}

	cliPath := filepath.Join(targetDir, "cryptomator-cli.app/Contents/MacOS/cryptomator-cli")
	if !isExecutableFile(cliPath) {
		return "", fmt.Errorf("Cryptomator CLI executable not found after extraction")
	}
	if err := os.Chmod(cliPath, 0700); err != nil {
		return "", err
	}
	fmt.Printf("✓ Cryptomator CLI downloaded: %s\n", release.TagName)
	return cliPath, nil
}

func installCask(name string) error {
	cmd := exec.Command("brew", "install", "--cask", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	return nil
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
			return fmt.Errorf("unsafe zip path: %q", file.Name)
		}
		target := filepath.Join(root, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in release archive: %s", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}

		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		_ = in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func askPath(prompt, defaultValue string) (string, error) {
	fmt.Print(prompt)
	line, err := input.ReadString('\n')
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
	line, err := input.ReadString('\n')
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
	fmt.Println("This removes edrive's local configuration and runtime state.")
	fmt.Println("It does NOT remove the Google Drive mirror, recovery data, or identities.")
	ok, err := askYesNo("Continue? [y/N] ", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}
	if err := os.Remove(cfg.ConfigPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(cfg.RuntimeDir); err != nil {
		return err
	}
	fmt.Println("edrive configuration/runtime removed.")
	return nil
}

func Purge() error {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}
	if isMounted(cfg.Mount) {
		return fmt.Errorf("vault is currently mounted; run edrive lock before purge")
	}

	fmt.Println("WARNING: purge removes local edrive-owned data.")
	fmt.Println("It preserves the Google Drive mirror and the external recovery identity.")
	fmt.Println()
	fmt.Println("Will remove:")
	fmt.Println("  Config:    " + cfg.ConfigPath)
	fmt.Println("  Runtime:   " + cfg.RuntimeDir)
	fmt.Println("  Mount dir: " + cfg.Mount)
	fmt.Println("  Recovery:  " + cfg.RecoveryDir)
	fmt.Println("  Mac identity and recipients")
	fmt.Println()
	fmt.Print("Type PURGE to continue: ")
	line, err := input.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	if strings.TrimSpace(line) != "PURGE" {
		fmt.Println("Cancelled.")
		return nil
	}

	for _, path := range []string{
		cfg.Mount,
		cfg.RecoveryDir,
		cfg.RuntimeDir,
		cfg.Recipients,
		cfg.MacIdentity,
		cfg.ConfigPath,
	} {
		if path == "" {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	fmt.Println("Local edrive state purged.")
	return nil
}

func isMounted(path string) bool {
	if path == "" {
		return false
	}
	out, err := exec.Command("mount").Output()
	if err != nil {
		return false
	}
	marker := " on " + filepath.Clean(path) + " ("
	return strings.Contains(string(out), marker)
}

func isExecutableFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func runtimeToolsDir() string {
	return filepath.Join(filepath.Dir(config.DefaultRuntimeDir()), "tools")
}

func ageRecipient(path string) (string, error) {
	out, err := exec.Command("age-keygen", "-y", path).Output()
	if err != nil {
		return "", fmt.Errorf("read age recipient: %w", err)
	}

	recipient := strings.TrimSpace(string(out))
	if !strings.HasPrefix(recipient, "age1") {
		return "", fmt.Errorf("unexpected age recipient output")
	}

	return recipient, nil
}

func isReadableIdentity(path string) bool {
	if path == "" {
		return false
	}

	if _, err := os.Stat(path); err != nil {
		return false
	}

	return exec.Command("age-keygen", "-y", path).Run() == nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
