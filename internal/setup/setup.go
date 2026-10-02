package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

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

func Run() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("setup currently supports macOS")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("Homebrew is required; install it and run edrive setup again")
	}

	fmt.Println("EDRIVE SETUP")
	fmt.Println()
	fmt.Println("Checking dependencies...")

	for _, name := range []string{"age", "zstd"} {
		if err := ensureFormula(name); err != nil {
			return err
		}
	}
	for _, name := range []string{"fuse-t", "google-drive", "cryptomator"} {
		if err := ensureCask(name); err != nil {
			return err
		}
	}

	repoRoot := filepath.Dir(config.DefaultPath())
	configDir := filepath.Join(repoRoot, "config")
	runtimeDir := filepath.Join(home, "Library/Application Support/edrive")
	recoveryDir := filepath.Join(home, "Desktop/local-infra/edrive-recovery")
	mountDir := filepath.Join(home, "Library/Application Support/Cryptomator/mnt/edrive")
	toolsDir := filepath.Join(home, "Desktop/local-infra/tools")
	identityDir := filepath.Join(runtimeDir, "identities")
	macIdentity := filepath.Join(identityDir, "mac.identity")
	recoveryIdentity := filepath.Join(home, "Desktop/local-infra/configs/edrive/edrive-recovery-identity.txt")
	recipientsPath := filepath.Join(configDir, "recipients.txt")

	for _, dir := range []string{
		configDir,
		runtimeDir,
		recoveryDir,
		mountDir,
		identityDir,
		toolsDir,
	} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	fmt.Println("✓ local directories ready")

	driveRoot, err := detectGoogleDriveRoot(home)
	if err != nil {
		_ = exec.Command("open", "-a", "Google Drive").Run()
		return err
	}
	fmt.Printf("✓ Google Drive: %s\n", driveRoot)

	cliPath, err := findCryptomatorCLI(home, toolsDir)
	if err != nil {
		fmt.Println()
		fmt.Println("Cryptomator CLI is not installed.")
		fmt.Println("Install the official Cryptomator CLI, then run:")
		fmt.Println("  edrive setup")
		return err
	}
	fmt.Printf("✓ Cryptomator CLI: %s\n", cliPath)

	if err := validateIdentity(macIdentity); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"Mac identity not found at %s; run edrive identity generate --output %s first",
				macIdentity,
				macIdentity,
			)
		}
		return fmt.Errorf("Mac identity is invalid: %w", err)
	}
	if err := os.Chmod(macIdentity, 0600); err != nil {
		return fmt.Errorf("protect Mac identity: %w", err)
	}
	fmt.Printf("✓ Mac identity: %s\n", macIdentity)

	macRecipient, err := ageRecipient(macIdentity)
	if err != nil {
		return fmt.Errorf("read Mac recipient: %w", err)
	}

	recoveryRecipient := ""
	if _, err := os.Stat(recoveryIdentity); err == nil {
		recoveryRecipient, err = ageRecipient(recoveryIdentity)
		if err != nil {
			return fmt.Errorf("validate recovery identity: %w", err)
		}
		fmt.Printf("✓ Recovery identity: %s\n", recoveryIdentity)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check recovery identity: %w", err)
	} else {
		fmt.Printf("! Recovery identity not found: %s\n", recoveryIdentity)
	}

	if err := ensureRecipients(recipientsPath, macRecipient, recoveryRecipient); err != nil {
		return err
	}
	fmt.Printf("✓ Recipients: %s\n", recipientsPath)

	vaultPath := filepath.Join(driveRoot, "edrive")
	vaultMarker := filepath.Join(vaultPath, "vault.cryptomator")
	if info, err := os.Stat(vaultMarker); err != nil || info.IsDir() {
		fmt.Println()
		fmt.Printf("Cryptomator vault is not initialized at:\n%s\n\n", vaultPath)
		fmt.Println("Open Cryptomator and create/add the vault at that exact location.")
		fmt.Println("Store the password in the macOS Keychain.")
		_ = exec.Command("open", "-a", "Cryptomator").Run()
		fmt.Println()
		fmt.Println("After the vault exists, run: edrive setup")
		return nil
	}

	settingsPath := filepath.Join(home, "Library/Application Support/Cryptomator/settings.json")
	vaultID, err := findVaultID(settingsPath, vaultPath, home)
	if err != nil {
		fmt.Println()
		fmt.Println("The vault exists, but Cryptomator has not registered it yet.")
		fmt.Printf("Vault: %s\n", vaultPath)
		fmt.Println("Open Cryptomator, add the vault, then run: edrive setup")
		return nil
	}

	if err := writeConfig(
		config.DefaultPath(),
		repoRoot,
		mountDir,
		recoveryDir,
		recipientsPath,
		cliPath,
		vaultPath,
		vaultID,
		runtimeDir,
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("✓ configuration written")
	fmt.Println("Setup complete.")
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  edrive doctor")
	fmt.Println("  edrive unlock")
	return nil
}

func ensureFormula(name string) error {
	if err := exec.Command("brew", "list", "--formula", name).Run(); err == nil {
		fmt.Printf("✓ %s\n", name)
		return nil
	}
	fmt.Printf("→ installing %s\n", name)
	cmd := exec.Command("brew", "install", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	fmt.Printf("✓ %s\n", name)
	return nil
}

func ensureCask(name string) error {
	if err := exec.Command("brew", "list", "--cask", name).Run(); err == nil {
		fmt.Printf("✓ %s\n", name)
		return nil
	}
	fmt.Printf("→ installing %s\n", name)
	cmd := exec.Command("brew", "install", "--cask", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	fmt.Printf("✓ %s\n", name)
	return nil
}

func detectGoogleDriveRoot(home string) (string, error) {
	candidates := []string{
		filepath.Join(home, "Google Drive", "My Drive"),
		"/Volumes/GoogleDrive/My Drive",
	}

	cloudStorage := filepath.Join(home, "Library/CloudStorage")
	entries, err := os.ReadDir(cloudStorage)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "GoogleDrive-") {
				candidates = append(candidates, filepath.Join(cloudStorage, entry.Name(), "My Drive"))
			}
		}
	}

	var valid []string
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			valid = append(valid, candidate)
		}
	}

	sort.Strings(valid)
	switch len(valid) {
	case 0:
		return "", fmt.Errorf("Google Drive My Drive was not found; sign in to Google Drive and run edrive setup again")
	case 1:
		return valid[0], nil
	default:
		fmt.Println("Multiple Google Drive locations found:")
		for i, candidate := range valid {
			fmt.Printf("  %d) %s\n", i+1, candidate)
		}
		fmt.Printf("Choose one [1-%d]: ", len(valid))
		reader := bufio.NewReader(os.Stdin)
		var choice int
		if _, err := fmt.Fscan(reader, &choice); err != nil {
			return "", fmt.Errorf("invalid selection: %w", err)
		}
		if choice < 1 || choice > len(valid) {
			return "", fmt.Errorf("invalid Google Drive selection: %d", choice)
		}
		return valid[choice-1], nil
	}
}

func findCryptomatorCLI(home, toolsDir string) (string, error) {
	candidates := []string{
		filepath.Join(toolsDir, "cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
		filepath.Join(toolsDir, "cryptomator-cli/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
		filepath.Join(home, "Downloads/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
		filepath.Join(home, "Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
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

	var found string
	_ = filepath.WalkDir(toolsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return err
		}
		if !d.IsDir() && d.Name() == "cryptomator-cli" && isExecutableFile(path) {
			found = path
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("Cryptomator CLI not found")
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func validateIdentity(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if err := exec.Command("age-keygen", "-y", path).Run(); err != nil {
		return fmt.Errorf("age identity cannot be read: %w", err)
	}
	return nil
}

func ageRecipient(path string) (string, error) {
	out, err := exec.Command("age-keygen", "-y", path).Output()
	if err != nil {
		return "", err
	}
	recipient := strings.TrimSpace(string(out))
	if !strings.HasPrefix(recipient, "age1") {
		return "", fmt.Errorf("unexpected age recipient output")
	}
	return recipient, nil
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
		if recipient == "" || seen[recipient] {
			continue
		}
		seen[recipient] = true
		lines = append(lines, recipient)
	}

	if len(lines) == 0 {
		return fmt.Errorf("no age recipients available")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return fmt.Errorf("write recipients file: %w", err)
	}
	return os.Chmod(path, 0600)
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

func writeConfig(
	path,
	repoRoot,
	mount,
	recoveryDir,
	recipients,
	cli,
	vault,
	vaultID,
	runtimeDir string,
) error {
	keep := 20
	if existing, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) != "EDRIVE_SNAPSHOT_KEEP" {
				continue
			}
			value := strings.Trim(strings.TrimSpace(parts[1]), ""'")
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				keep = n
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	lines := []string{
		fmt.Sprintf("EDRIVE_HOME=%q", repoRoot),
		fmt.Sprintf("EDRIVE_MOUNT=%q", mount),
		fmt.Sprintf("EDRIVE_RECOVERY_DIR=%q", recoveryDir),
		fmt.Sprintf("EDRIVE_RECIPIENTS=%q", recipients),
		fmt.Sprintf("EDRIVE_SNAPSHOT_KEEP=%d", keep),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_VAULT=%q", vault),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_VAULT_ID=%q", vaultID),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_CLI=%q", cli),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_MOUNTER=%q", cryptomatorMounter),
		fmt.Sprintf("EDRIVE_CRYPTOMATOR_KEYCHAIN_SERVICE=%q", keychainService),
		fmt.Sprintf("EDRIVE_RUNTIME_DIR=%q", runtimeDir),
	}
	content := strings.Join(lines, "\n") + "\n"

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(path, 0600)
}

