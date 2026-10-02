package setup

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/config"
)

const (
	cryptomatorMounter = "org.cryptomator.frontend.fuse.mount.FuseTMountProvider"
	keychainService    = "Cryptomator"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

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

	if err := ensureFormula("age"); err != nil {
		return err
	}
	if err := ensureFormula("zstd"); err != nil {
		return err
	}
	if err := ensureCask("fuse-t"); err != nil {
		return err
	}
	if err := ensureCask("google-drive"); err != nil {
		return err
	}
	if err := ensureCask("cryptomator"); err != nil {
		return err
	}

	repoRoot := filepath.Dir(config.DefaultPath())
	configDir := filepath.Join(repoRoot, "config")
	runtimeDir := filepath.Join(home, "Library/Application Support/edrive")
	recoveryDir := filepath.Join(home, "Desktop/local-infra/edrive-recovery")
	mountDir := filepath.Join(home, "Library/Application Support/Cryptomator/mnt/edrive")
	toolsDir := filepath.Join(home, "Desktop/local-infra/tools")
	identityDir := filepath.Join(runtimeDir, "identities")
	macIdentity := filepath.Join(identityDir, "mac.identity")
	recoveryIdentity := filepath.Join(
		home,
		"Desktop/local-infra/configs/edrive/edrive-recovery-identity.txt",
	)
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

	fmt.Println("✓ base directories ready")

	driveRoot, err := detectGoogleDriveRoot(home)
	if err != nil {
		_ = exec.Command("open", "-a", "Google Drive").Run()
		return err
	}
	fmt.Printf("✓ Google Drive: %s\n", driveRoot)

	cliPath, err := findCryptomatorCLI(home, toolsDir)
	if err != nil {
		fmt.Println()
		fmt.Println("Cryptomator CLI was not found.")
		ok, promptErr := askYesNo(
			"Download the latest official Cryptomator CLI from GitHub? [Y/n] ",
			true,
		)
		if promptErr != nil {
			return promptErr
		}
		if !ok {
			return fmt.Errorf("Cryptomator CLI is required; run edrive setup again after installing it")
		}
		cliPath, err = downloadCryptomatorCLI(toolsDir)
		if err != nil {
			return err
		}
	}
	fmt.Printf("✓ Cryptomator CLI: %s\n", cliPath)

	if err := ensureMacIdentity(macIdentity); err != nil {
		return err
	}
	fmt.Printf("✓ Mac identity: %s\n", macIdentity)

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

	macRecipient, err := ageRecipient(macIdentity)
	if err != nil {
		return fmt.Errorf("read Mac recipient: %w", err)
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
		fmt.Println("The setup cannot create a Cryptomator vault itself.")
		fmt.Println("Open Cryptomator and create/add the vault at that exact location.")
		fmt.Println("Store the vault password in the macOS Keychain.")
		fmt.Println()
		_ = exec.Command("open", "-a", "Cryptomator").Run()
		fmt.Println("After the vault exists, run: edrive setup")
		return nil
	}

	settingsPath := filepath.Join(
		home,
		"Library/Application Support/Cryptomator/settings.json",
	)
	vaultID, err := findVaultID(settingsPath, vaultPath, home)
	if err != nil {
		fmt.Println()
		fmt.Println("Vault exists, but Cryptomator has not registered it yet.")
		fmt.Printf("Vault: %s\n", vaultPath)
		fmt.Println("Open Cryptomator, add the vault, then run:")
		fmt.Println("  edrive setup")
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
	fmt.Printf("✓ Cryptomator vault: %s\n", vaultPath)
	fmt.Printf("✓ Vault ID: %s\n", vaultID)
	fmt.Println()
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
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "GoogleDrive-") {
				continue
			}
			candidates = append(candidates, filepath.Join(cloudStorage, entry.Name(), "My Drive"))
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
		return "", fmt.Errorf(
			"Google Drive My Drive was not found; sign in to Google Drive and run edrive setup again",
		)
	case 1:
		return valid[0], nil
	default:
		fmt.Println("Multiple Google Drive locations were found:")
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
		if d.IsDir() || d.Name() != "cryptomator-cli" {
			return nil
		}
		if isExecutableFile(path) {
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
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0111 != 0
}

func downloadCryptomatorCLI(toolsDir string) (string, error) {
	req, err := http.NewRequest(
		http.MethodGet,
		"https://api.github.com/repos/cryptomator/cli/releases/latest",
		nil,
	)
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
		return "", fmt.Errorf("no macOS %s Cryptomator CLI release asset found", suffix)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("Cryptomator CLI release has no version tag")
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
	zipFile, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(zipFile, resp.Body)
	closeErr := zipFile.Close()
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
		return "", fmt.Errorf("Cryptomator CLI executable not found after extraction: %s", cliPath)
	}
	if err := os.Chmod(cliPath, 0700); err != nil {
		return "", fmt.Errorf("make Cryptomator CLI executable: %w", err)
	}

	fmt.Printf("✓ Cryptomator CLI downloaded: %s\n", release.TagName)
	return cliPath, nil
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

func ensureMacIdentity(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("Mac identity path is a directory: %s", path)
		}
		if err := validateIdentity(path); err != nil {
			return fmt.Errorf("existing Mac identity is invalid: %w", err)
		}
		return os.Chmod(path, 0600)
	} else if !os.IsNotExist(err) {
		return err
	}

	ok, err := askYesNo(
		fmt.Sprintf("Mac identity not found at %s. Generate a new one? [Y/n] ", path),
		true,
	)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("Mac identity is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	cmd := exec.Command("age-keygen", "-pq", "-o", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate Mac identity: %w", err)
	}
	return os.Chmod(path, 0600)
}

func validateIdentity(path string) error {
	if err := exec.Command("age-keygen", "-y", path).Run(); err != nil {
		return fmt.Errorf("age identity cannot be read: %w", err)
	}
	return nil
}

func ageRecipient(identityPath string) (string, error) {
	cmd := exec.Command("age-keygen", "-y", identityPath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	recipient := strings.TrimSpace(string(out))
	if !strings.HasPrefix(recipient, "age1") {
		return "", fmt.Errorf("unexpected age recipient: %q", recipient)
	}
	return recipient, nil
}

func ensureRecipients(path string, recipients ...string) error {
	existing := []string{}
	if b, err := os.ReadFile(path); err == nil {
		existing = strings.Split(string(b), "\n")
	} else if !os.IsNotExist(err) {
		return err
	}

	seen := make(map[string]bool)
	var lines []string
	for _, line := range existing {
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

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write recipients file: %w", err)
	}
	return os.Chmod(path, 0600)
}

func findVaultID(settingsPath, vaultPath, home string) (string, error) {
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", fmt.Errorf("Cryptomator settings not found: %w", err)
	}

	var settings cryptomatorSettings
	if err := json.Unmarshal(b, &settings); err != nil {
		return "", fmt.Errorf("parse Cryptomator settings: %w", err)
	}

	target := canonicalPath(vaultPath)
	for _, dir := range settings.Directories {
		if dir.ID == "" || dir.Path == "" {
			continue
		}
		p := dir.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(home, p)
		}
		if canonicalPath(p) == target {
			return dir.ID, nil
		}
	}

	return "", fmt.Errorf("vault is not registered in Cryptomator settings")
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

	content := strings.Join([]string{
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
	}, "\n") + "\n"

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(path, 0600)
}

func askYesNo(prompt string, defaultYes bool) (bool, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
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
