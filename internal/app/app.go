package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/cryptomator"
	"github.com/faiz/edrive/internal/snapshot"
	"github.com/faiz/edrive/internal/storage"
	"github.com/faiz/edrive/internal/util"
)

type App struct {
	Config config.Config
}

func vaultAvailable(mountPath string) bool {
	mountPath = filepath.Clean(mountPath)

	cmd := exec.Command("mount")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	marker := " on " + mountPath + " ("

	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, marker) {
			entries, err := os.ReadDir(mountPath)
			return err == nil && entries != nil
		}
	}

	return false
}

func loadRecipients(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "age1") {
			return 0, fmt.Errorf("unsupported recipient in %s: %q", path, line)
		}
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("no age recipients configured in %s", path)
	}
	return count, nil
}

func (a App) Doctor() error {
	fmt.Println("EDRIVE DOCTOR")
	fmt.Println()

	type check struct {
		name   string
		ok     bool
		detail string
	}
	var checks []check

	cfgModeOK := false
	if info, err := os.Stat(a.Config.ConfigPath); err == nil {
		cfgModeOK = info.Mode().Perm()&0077 == 0
	}
	configDetail := a.Config.ConfigPath
	if !a.Config.ConfigFound {
		configDetail = "not configured"
	} else if !cfgModeOK {
		configDetail = "permissions should be 0600"
	}
	checks = append(checks, check{"Config", a.Config.ConfigFound && cfgModeOK, configDetail})
	if a.Config.DataRoot != "" {
		checks = append(checks, check{"Data root", isDir(a.Config.DataRoot), a.Config.DataRoot})
	}
	if a.Config.GoogleDriveRoot != "" {
		checks = append(checks, check{"Google Drive root", isDir(a.Config.GoogleDriveRoot), a.Config.GoogleDriveRoot})
	}
	checks = append(checks, check{"Drive configuration", a.Config.GoogleDriveReady, "mirrored local folder recorded"})
	if a.Config.Mount != "" {
		checks = append(checks, check{"Mount path", isDir(a.Config.Mount), a.Config.Mount})
	}
	if a.Config.RecoveryDir != "" {
		checks = append(checks, check{"Recovery dir", isDir(a.Config.RecoveryDir), a.Config.RecoveryDir})
	}
	if a.Config.Recipients != "" {
		n, err := loadRecipients(a.Config.Recipients)
		if err != nil {
			checks = append(checks, check{"Recipients", false, err.Error()})
		} else {
			checks = append(checks, check{"Recipients", true, fmt.Sprintf("%d recipient(s)", n)})
		}
	}
	if a.Config.MacIdentity != "" {
		err := validateAgeIdentity(a.Config.MacIdentity)
		checks = append(checks, check{"Mac identity", err == nil, detailPathOrError(a.Config.MacIdentity, err)})
	}
	if a.Config.RecoveryIdentity != "" {
		err := validateAgeIdentity(a.Config.RecoveryIdentity)
		if err != nil {
			checks = append(checks, check{"Recovery identity", false, "not available: " + a.Config.RecoveryIdentity})
		} else {
			checks = append(checks, check{"Recovery identity", true, a.Config.RecoveryIdentity})
		}
	}

	checks = append(checks, check{"age", binaryAvailable("age"), binaryDetail("age")})
	checks = append(checks, check{"zstd", binaryAvailable("zstd"), binaryDetail("zstd")})
	checks = append(checks, check{"FUSE-T", caskOrAppAvailable("fuse-t", nil), binaryOrAppDetail("fuse-t", nil)})
	checks = append(checks, check{"Google Drive", caskOrAppAvailable("google-drive", []string{"/Applications/Google Drive.app"}), binaryOrAppDetail("google-drive", []string{"/Applications/Google Drive.app"})})
	if a.Config.GoogleDriveRoot != "" && isDir(a.Config.GoogleDriveRoot) {
		running := processRunning("Google Drive")
		checks = append(checks, check{"Drive process", running, "running"} )
		if !running {
			checks[len(checks)-1].detail = "not running"
		}
	}

	cliOK := isExecutableFile(a.Config.CryptomatorCLI)
	cliDetail := a.Config.CryptomatorCLI
	if a.Config.CryptomatorCLI == "" {
		cliDetail = "not configured"
	}
	checks = append(checks, check{"Cryptomator CLI", cliOK, cliDetail})

	vaultOK := isDir(a.Config.CryptomatorVault)
	vaultDetail := a.Config.CryptomatorVault
	if a.Config.CryptomatorVault == "" {
		vaultDetail = "not configured"
	}
	checks = append(checks, check{"Cryptomator vault", vaultOK, vaultDetail})

	vaultIDOK := strings.TrimSpace(a.Config.CryptomatorVaultID) != ""
	vaultIDDetail := a.Config.CryptomatorVaultID
	if !vaultIDOK {
		vaultIDDetail = "not registered/configured"
	}
	checks = append(checks, check{"Vault ID", vaultIDOK, vaultIDDetail})

	mounterOK := strings.TrimSpace(a.Config.CryptomatorMounter) != ""
	checks = append(checks, check{"FUSE mounter", mounterOK, a.Config.CryptomatorMounter})

	if vaultIDOK && runtime.GOOS == "darwin" {
		err := exec.Command(
			"/usr/bin/security",
			"find-generic-password",
			"-s", a.Config.CryptomatorKeychainService,
			"-a", a.Config.CryptomatorVaultID,
		).Run()
		checks = append(checks, check{"Keychain item", err == nil, "Cryptomator vault credential"})
	}

	all := true
	for _, c := range checks {
		mark := "✓"
		if !c.ok {
			mark = "✗"
			all = false
		}
		fmt.Printf("%-20s %s  %s\n", c.name, mark, c.detail)
	}

	fmt.Println()
	if all {
		fmt.Println("Status: HEALTHY")
		return nil
	}
	return fmt.Errorf("doctor found one or more problems")
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isExecutableFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func binaryAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func binaryDetail(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return "not installed"
}

func caskOrAppAvailable(cask string, appPaths []string) bool {
	if len(appPaths) > 0 {
		for _, path := range appPaths {
			if isDir(path) {
				return true
			}
		}
		return false
	}
	if _, err := exec.LookPath("brew"); err == nil {
		return exec.Command("brew", "list", "--cask", cask).Run() == nil
	}
	return false
}

func binaryOrAppDetail(name string, appPaths []string) string {
	if len(appPaths) > 0 {
		for _, path := range appPaths {
			if isDir(path) {
				return path
			}
		}
		return "not installed"
	}
	if _, err := exec.LookPath("brew"); err == nil {
		if err := exec.Command("brew", "list", "--cask", name).Run(); err == nil {
			return "installed"
		}
	}
	return "not installed"
}

func processRunning(name string) bool {
	return exec.Command("pgrep", "-f", name).Run() == nil
}

func validateAgeIdentity(path string) error {
	if path == "" {
		return fmt.Errorf("not configured")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return exec.Command("age-keygen", "-y", path).Run()
}

func detailPathOrError(path string, err error) string {
	if err == nil {
		return path
	}
	return err.Error()
}

func (a App) Status() error {
	fmt.Printf("data root:  %s\n", a.Config.DataRoot)
	fmt.Printf("drive root: %s\n", a.Config.GoogleDriveRoot)
	fmt.Printf("mount:      %s\n", a.Config.Mount)

	if runtime.GOOS == "darwin" {
		if caskOrAppAvailable("google-drive", []string{"/Applications/Google Drive.app"}) {
			fmt.Println("Google Drive: installed")
		} else {
			fmt.Println("Google Drive: missing")
		}
	}

	if !isDir(a.Config.Mount) || !vaultAvailable(a.Config.Mount) {
		fmt.Println("state:      LOCKED / unavailable")
	} else {
		fmt.Println("state:      UNLOCKED / available")
	}

	fmt.Printf("recovery:   %s\n", a.Config.RecoveryDir)
	fmt.Printf("recipients: %s\n", a.Config.Recipients)

	if !isDir(a.Config.RecoveryDir) {
		fmt.Println("snapshots:  0")
		fmt.Println("latest:     none")
		return nil
	}

	entries, err := os.ReadDir(a.Config.RecoveryDir)
	if err != nil {
		return err
	}
	count := 0
	var latest os.DirEntry
	var latestMod time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.zst.age") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		if latest == nil || info.ModTime().After(latestMod) {
			latest = entry
			latestMod = info.ModTime()
		}
	}
	fmt.Printf("snapshots:  %d\n", count)
	if latest != nil {
		fmt.Printf("latest:     %s\n", latest.Name())
	} else {
		fmt.Println("latest:     none")
	}
	return nil
}

func (a App) Backup() error {
	if err := util.RequireBinary("age"); err != nil {
		return err
	}
	if err := util.RequireBinary("zstd"); err != nil {
		return err
	}
	if !vaultAvailable(a.Config.Mount) {
		return fmt.Errorf("vault is not unlocked at %s", a.Config.Mount)
	}
	if _, err := loadRecipients(a.Config.Recipients); err != nil {
		return err
	}
	store, err := storage.NewLocal(a.Config.RecoveryDir)
	if err != nil {
		return err
	}

	m, err := snapshot.BuildManifest(a.Config.Mount)
	if err != nil {
		return err
	}
	ts := time.Now().UTC().Format("20060102-150405")
	name := "edrive-recovery-" + ts + ".tar.zst.age"
	fmt.Printf("Creating snapshot...\nFiles: %d\n", len(m.Files))
	if err := store.Write(name, func(w io.Writer) error {
		return snapshot.CreateEncryptedSnapshot(a.Config.Mount, a.Config.Recipients, m, w)
	}); err != nil {
		return err
	}
	obj := filepath.Join(a.Config.RecoveryDir, name)
	info, err := os.Stat(obj)
	if err != nil {
		return err
	}
	fmt.Printf("Snapshot:   %s\n", obj)
	fmt.Printf("Encrypted:  %s\n", formatBytes(info.Size()))
	fmt.Println("Status:     CREATED")
	return pruneSnapshots(store, a.Config.SnapshotKeep)
}

func (a App) Backups() error {
	store, err := storage.NewLocal(a.Config.RecoveryDir)
	if err != nil {
		return err
	}
	objects, err := store.List(".tar.zst.age")
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		fmt.Println("No snapshots.")
		return nil
	}
	for _, obj := range objects {
		fmt.Printf("%s  %s  %s\n", obj.ModTime.UTC().Format(time.RFC3339), formatBytes(obj.Size), filepath.Join(a.Config.RecoveryDir, obj.Name))
	}
	return nil
}

func (a App) Verify(path, identity string) error {
	if err := util.RequireBinary("age"); err != nil {
		return err
	}
	if err := util.RequireBinary("zstd"); err != nil {
		return err
	}
	store, err := storage.NewLocal(a.Config.RecoveryDir)
	if err != nil {
		return err
	}
	name, err := resolveSnapshot(store, path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(identity) == "" {
		return fmt.Errorf("recovery identity required: use --identity /path/to/identity")
	}
	if _, err := os.Stat(identity); err != nil {
		return fmt.Errorf("identity not found: %w", err)
	}
	plain, err := store.Open(name)
	if err != nil {
		return err
	}
	defer plain.Close()
	manifest, err := snapshot.ReadAndVerifyArchive(plain, identity, "", false)
	if err != nil {
		return err
	}
	fmt.Printf("Verified:   %s\n", filepath.Join(a.Config.RecoveryDir, name))
	fmt.Printf("Files:      %d\n", len(manifest.Files))
	fmt.Printf("Created:    %s\n", manifest.CreatedAt.Format(time.RFC3339))
	fmt.Println("Integrity:  OK")
	return nil
}

func (a App) Restore(path, identity, output string) error {
	if err := util.RequireBinary("age"); err != nil {
		return err
	}
	if err := util.RequireBinary("zstd"); err != nil {
		return err
	}
	store, err := storage.NewLocal(a.Config.RecoveryDir)
	if err != nil {
		return err
	}
	name, err := resolveSnapshot(store, path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(identity) == "" {
		return fmt.Errorf("identity required: use --identity /path/to/identity")
	}
	if _, err := os.Stat(identity); err != nil {
		return fmt.Errorf("identity not found: %w", err)
	}
	if output == "" {
		return fmt.Errorf("restore output directory is required: use --output DIR")
	}
	if filepath.Clean(output) == filepath.Clean(a.Config.Mount) {
		return fmt.Errorf("refusing to restore over live mount")
	}
	plain, err := store.Open(name)
	if err != nil {
		return err
	}
	defer plain.Close()
	manifest, err := snapshot.ReadAndVerifyArchive(plain, identity, output, true)
	if err != nil {
		return err
	}
	fmt.Printf("Restored:   %s\n", output)
	fmt.Printf("Files:      %d\n", len(manifest.Files))
	fmt.Println("Integrity:  OK")
	return nil
}

func (a App) Unlock() error {
	if runtime.GOOS == "darwin" && !caskOrAppAvailable("google-drive", []string{"/Applications/Google Drive.app"}) {
		return fmt.Errorf("Google Drive for desktop is not installed")
	}
	if vaultAvailable(a.Config.Mount) {
		fmt.Printf("Vault already unlocked: %s\n", a.Config.Mount)
		return nil
	}

	client := cryptomator.New(cryptomator.Config{
		VaultPath:       a.Config.CryptomatorVault,
		VaultID:         a.Config.CryptomatorVaultID,
		CLIPath:         a.Config.CryptomatorCLI,
		MountPoint:      a.Config.Mount,
		Mounter:         a.Config.CryptomatorMounter,
		KeychainService: a.Config.CryptomatorKeychainService,
		RuntimeDir:      a.Config.RuntimeDir,
	})

	if err := client.Unlock(); err != nil {
		return err
	}

	fmt.Printf("Vault unlocked: %s\n", a.Config.Mount)
	return nil
}

func (a App) Lock() error {
	if !vaultAvailable(a.Config.Mount) {
		fmt.Println("Vault already locked.")
		return nil
	}

	client := cryptomator.New(cryptomator.Config{
		VaultPath:       a.Config.CryptomatorVault,
		VaultID:         a.Config.CryptomatorVaultID,
		CLIPath:         a.Config.CryptomatorCLI,
		MountPoint:      a.Config.Mount,
		Mounter:         a.Config.CryptomatorMounter,
		KeychainService: a.Config.CryptomatorKeychainService,
		RuntimeDir:      a.Config.RuntimeDir,
	})

	if err := client.Lock(); err != nil {
		return err
	}

	fmt.Println("Vault locked.")
	return nil
}

func GenerateIdentity(path string) error {
	if path == "" {
		return fmt.Errorf("output path required")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing identity: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	cmd := exec.Command("age-keygen", "-pq", "-o", path)
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func PrintRecipient(path string) error {
	cmd := exec.Command("age-keygen", "-y", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func AddRecipient(identityPath, recipientsPath string) error {
	if _, err := os.Stat(identityPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(recipientsPath), 0700); err != nil {
		return err
	}
	cmd := exec.Command("age-keygen", "-y", identityPath)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	recipient := strings.TrimSpace(string(out))
	if !strings.HasPrefix(recipient, "age1") {
		return fmt.Errorf("unexpected age recipient output")
	}
	if existing, err := os.ReadFile(recipientsPath); err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			if strings.TrimSpace(line) == recipient {
				return fmt.Errorf("recipient already exists in %s", recipientsPath)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(recipientsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, recipient); err != nil {
		return err
	}
	return nil
}

func resolveSnapshot(store storage.Provider, path string) (string, error) {
	objects, err := store.List(".tar.zst.age")
	if err != nil {
		return "", err
	}
	if path == "" {
		if len(objects) == 0 {
			return "", fmt.Errorf("no recovery snapshots found")
		}
		return objects[0].Name, nil
	}
	path = config.Expand(path)
	name := filepath.Base(path)
	if name != path && !filepath.IsAbs(path) {
		name = filepath.Base(path)
	}
	if filepath.IsAbs(path) {
		name = filepath.Base(path)
	}
	for _, obj := range objects {
		if obj.Name == name {
			return obj.Name, nil
		}
	}
	return "", fmt.Errorf("snapshot not found: %s", path)
}

func pruneSnapshots(store storage.Provider, keep int) error {
	objects, err := store.List(".tar.zst.age")
	if err != nil {
		return err
	}
	for i := keep; i < len(objects); i++ {
		if err := store.Remove(objects[i].Name); err != nil {
			return fmt.Errorf("remove old snapshot %s: %w", objects[i].Name, err)
		}
	}
	return nil
}

func ensureDir(path string) error {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("not a directory: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(path, 0700)
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
