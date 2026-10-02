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
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/provider"
	"github.com/faiz/edrive/internal/snapshot"
	"github.com/faiz/edrive/internal/ui"
)

const (
	ageVersion  = "1.3.2"
	zstdVersion = "1.5.7"
)

type App struct {
	Config config.Config
}

type check struct {
	name   string
	ok     bool
	detail string
}

func (a App) Doctor() error {
	fmt.Println("EDRIVE DOCTOR")
	fmt.Println()

	var checks []check

	configOK := a.Config.ConfigFound && fileMode0600(a.Config.ConfigPath)
	configDetail := a.Config.ConfigPath
	if !a.Config.ConfigFound {
		configDetail = "not configured"
	} else if !fileMode0600(a.Config.ConfigPath) {
		configDetail = "permissions should be 0600"
	}
	checks = append(checks, check{name: "Config", ok: configOK, detail: configDetail})

	workspaceOK := isDir(a.Config.DataRoot)
	checks = append(checks, check{name: "Workspace", ok: workspaceOK, detail: a.Config.DataRoot})

	if runtime.GOOS == "darwin" {
		driveRoot, err := (provider.GoogleDrive{StorageName: a.Config.StorageName}).Root()
		if err != nil {
			checks = append(checks, check{name: "Google Drive", ok: false, detail: "local storage unavailable"})
		} else {
			checks = append(checks, check{name: "Google Drive", ok: true, detail: driveRoot})
			vault := filepath.Join(driveRoot, a.Config.StorageName)
			checks = append(checks, check{
				name:   "Encrypted vault",
				ok:     isFile(filepath.Join(vault, "vault.cryptomator")),
				detail: vault,
			})
			_, regErr := cryptomator.DiscoverVaultID(vault)
			checks = append(checks, check{
				name:   "Cryptomator registration",
				ok:     regErr == nil,
				detail: "ready",
			})
			if regErr != nil {
				checks[len(checks)-1].detail = "not registered"
			}
		}

		checks = append(checks, check{
			name:   "Google Drive app",
			ok:     isDir("/Applications/Google Drive.app"),
			detail: "/Applications/Google Drive.app",
		})
		checks = append(checks, check{
			name:   "FUSE-T",
			ok:     brewCaskInstalled("fuse-t"),
			detail: "installed",
		})
	}

	checks = append(checks, toolCheck("age", a.Config.AgePath, ageVersion))
	checks = append(checks, toolCheck("zstd", a.Config.ZstdPath, zstdVersion))
	checks = append(checks, check{
		name:   "Cryptomator CLI",
		ok:     isExecutableFile(a.Config.CryptomatorCLI),
		detail: a.Config.CryptomatorCLI,
	})

	recipients, recErr := loadRecipients(a.Config.Recipients)
	recDetail := fmt.Sprintf("%d recipient(s)", recipients)
	if recErr != nil {
		recDetail = recErr.Error()
	}
	checks = append(checks, check{name: "Recipients", ok: recErr == nil, detail: recDetail})
	checks = append(checks, check{
		name:   "Device identity",
		ok:     keychain.Exists(keychain.DeviceIdentity),
		detail: "macOS Keychain",
	})
	checks = append(checks, check{
		name:   "Recovery identity",
		ok:     keychain.Exists(keychain.RecoveryIdentity),
		detail: "macOS Keychain",
	})

	all := true
	for _, c := range checks {
		mark := "✓"
		if !c.ok {
			mark = "✗"
			all = false
		}
		fmt.Printf("%-23s %s  %s\n", c.name, mark, c.detail)
	}

	fmt.Println()
	if all {
		fmt.Println("Status: HEALTHY")
		return nil
	}
	return fmt.Errorf("doctor found one or more problems")
}

func (a App) Status() error {
	fmt.Println("EDRIVE STATUS")
	fmt.Println()
	fmt.Printf("Workspace: %s\n", a.Config.DataRoot)
	fmt.Printf("State:     %s\n", workspaceState(a.Config.DataRoot))

	if _, err := (provider.GoogleDrive{StorageName: a.Config.StorageName}).Root(); err == nil {
		fmt.Println("Storage:   Google Drive")
	} else {
		fmt.Println("Storage:   unavailable")
	}

	if a.Config.LastBackupDir == "" {
		fmt.Println("Backups:   no previous destination")
	} else {
		fmt.Printf("Backups:   %s\n", a.Config.LastBackupDir)
	}
	return nil
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
	fmt.Printf("Workspace opened: %s\n", a.Config.DataRoot)
	return nil
}

func (a App) ensureUnlocked() error {
	if mounted(a.Config.DataRoot) {
		return nil
	}

	storageRoot, err := (provider.GoogleDrive{StorageName: a.Config.StorageName}).Root()
	if err != nil {
		return fmt.Errorf("Google Drive storage is unavailable")
	}

	vaultPath := filepath.Join(storageRoot, a.Config.StorageName)
	if !isFile(filepath.Join(vaultPath, "vault.cryptomator")) {
		return fmt.Errorf("encrypted workspace is unavailable")
	}
	if !isExecutableFile(a.Config.CryptomatorCLI) {
		return fmt.Errorf("Cryptomator CLI is unavailable")
	}

	client := cryptomator.New(cryptomator.Config{
		VaultPath:  vaultPath,
		MountPoint: a.Config.DataRoot,
		CLIPath:    a.Config.CryptomatorCLI,
		RuntimeDir: config.DefaultRuntimeDir(),
	})
	if err := client.Unlock(); err != nil {
		return err
	}
	return nil
}

func (a App) Close() error {
	if !mounted(a.Config.DataRoot) {
		fmt.Println("Workspace already closed.")
		return nil
	}

	client := cryptomator.New(cryptomator.Config{
		MountPoint: a.Config.DataRoot,
		CLIPath:    a.Config.CryptomatorCLI,
		RuntimeDir: config.DefaultRuntimeDir(),
	})
	if err := client.Lock(); err != nil {
		return err
	}
	fmt.Println("Workspace closed.")
	return nil
}

func (a App) Backup() error {
	if err := a.requireTools(); err != nil {
		return err
	}

	// Accessing the recovery identity is intentionally the first sensitive
	// operation. macOS may ask for Keychain permission here.
	recoveryIdentity, err := keychain.Get(keychain.RecoveryIdentity)
	if err != nil {
		return fmt.Errorf("recovery access was not granted")
	}

	if err := a.ensureUnlocked(); err != nil {
		return err
	}

	defaultDir := a.Config.LastBackupDir
	if !isDir(defaultDir) {
		defaultDir, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	if pathInside(a.Config.DataRoot, defaultDir) {
		defaultDir, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}

	destination, selected, err := ui.ChooseFolder(
		"Choose where to save this edrive backup",
		defaultDir,
	)
	if err != nil {
		return err
	}
	if !selected {
		fmt.Println("Backup cancelled.")
		return nil
	}
	if pathInside(a.Config.DataRoot, destination) {
		return fmt.Errorf("backup destination cannot be inside the edrive workspace")
	}

	if err := os.MkdirAll(config.DefaultTempDir(), 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(config.DefaultTempDir(), ".backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	manifest, err := snapshot.BuildManifest(a.Config.DataRoot)
	if err != nil {
		return err
	}

	archivePath := filepath.Join(staging, "backup.tar.zst.age")
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	createErr := snapshot.CreateEncryptedSnapshot(
		a.Config.DataRoot,
		a.Config.Recipients,
		a.Config.AgePath,
		a.Config.ZstdPath,
		manifest,
		archive,
	)
	if createErr != nil {
		_ = archive.Close()
		return createErr
	}
	if err := archive.Sync(); err != nil {
		_ = archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}

	if err := withIdentityFile(recoveryIdentity, func(identityPath string) error {
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = snapshot.ReadAndVerifyArchive(
			f,
			identityPath,
			a.Config.AgePath,
			a.Config.ZstdPath,
			"",
			false,
		)
		return err
	}); err != nil {
		return fmt.Errorf("verify backup: %w", err)
	}

	packageDir, err := createBackupPackage(destination)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(packageDir)
		}
	}()

	finalArchive := filepath.Join(packageDir, "backup.tar.zst.age")
	if err := copyFileAtomic(archivePath, finalArchive, 0600); err != nil {
		return err
	}
	if err := writePrivateFile(filepath.Join(packageDir, "recovery-key.txt"), recoveryIdentity+"\n"); err != nil {
		return err
	}
	if err := writePrivateFile(filepath.Join(packageDir, "README.txt"), backupReadme); err != nil {
		return err
	}

	committed = true
	a.Config.LastBackupDir = destination
	configErr := a.Config.Save()

	if err := ui.Open(packageDir); err != nil {
		fmt.Println("Backup completed, but Finder could not be opened.")
	}

	info, statErr := os.Stat(finalArchive)
	if statErr != nil {
		return statErr
	}

	fmt.Println()
	fmt.Printf("Backup saved: %s\n", packageDir)
	fmt.Printf("Files:        %d\n", len(manifest.Files))
	fmt.Printf("Encrypted:    %s\n", formatBytes(info.Size()))

	return configErr
}

func (a App) Restore() error {
	if err := a.requireTools(); err != nil {
		return err
	}

	defaultDir := a.Config.LastBackupDir
	if !isDir(defaultDir) {
		defaultDir, _ = os.UserHomeDir()
	}

	backupDir, selected, err := ui.ChooseFolder("Choose an edrive backup folder", defaultDir)
	if err != nil {
		return err
	}
	if !selected {
		fmt.Println("Restore cancelled.")
		return nil
	}

	archivePath := filepath.Join(backupDir, "backup.tar.zst.age")
	keyPath := filepath.Join(backupDir, "recovery-key.txt")
	if !isFile(archivePath) {
		return fmt.Errorf("selected folder does not contain an edrive backup")
	}

	recoveryIdentity, keyErr := keychain.Get(keychain.RecoveryIdentity)
	if keyErr != nil {
		if !isFile(keyPath) {
			return fmt.Errorf("recovery identity is unavailable")
		}
		b, err := os.ReadFile(keyPath)
		if err != nil {
			return err
		}
		recoveryIdentity = strings.TrimSpace(string(b))
		if recoveryIdentity == "" {
			return fmt.Errorf("backup recovery key is empty")
		}
		if err := keychain.Set(keychain.RecoveryIdentity, recoveryIdentity); err != nil {
			return err
		}
	}

	restoreDir, selected, err := ui.ChooseFolder("Choose an empty folder for the restored workspace", func() string {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "edrive-restore")
	}())
	if err != nil {
		return err
	}
	if !selected {
		fmt.Println("Restore cancelled.")
		return nil
	}
	if pathInside(a.Config.DataRoot, restoreDir) {
		return fmt.Errorf("restore destination cannot be inside the edrive workspace")
	}

	return withIdentityFile(recoveryIdentity, func(identityPath string) error {
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()

		manifest, err := snapshot.ReadAndVerifyArchive(
			f,
			identityPath,
			a.Config.AgePath,
			a.Config.ZstdPath,
			restoreDir,
			true,
		)
		if err != nil {
			return err
		}
		fmt.Printf("Restored: %s\n", restoreDir)
		fmt.Printf("Files:    %d\n", len(manifest.Files))
		fmt.Println("Integrity: OK")
		return nil
	})
}

func (a App) requireTools() error {
	if !toolVersionMatches(a.Config.AgePath, ageVersion) {
		return fmt.Errorf("edrive requires age %s", ageVersion)
	}
	if !toolVersionMatches(a.Config.ZstdPath, zstdVersion) {
		return fmt.Errorf("edrive requires zstd %s", zstdVersion)
	}
	if !isExecutableFile(a.Config.CryptomatorCLI) {
		return fmt.Errorf("Cryptomator CLI is unavailable")
	}
	_, err := loadRecipients(a.Config.Recipients)
	return err
}

func workspaceState(path string) string {
	if mounted(path) {
		return "OPEN"
	}
	return "CLOSED"
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
			return 0, fmt.Errorf("invalid age recipient")
		}
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("no age recipients configured")
	}
	return count, nil
}

func toolCheck(name, path, version string) check {
	detail := path
	if detail == "" {
		detail = "not configured"
	}
	return check{name: name, ok: toolVersionMatches(path, version), detail: detail}
}

func toolVersionMatches(path, version string) bool {
	if !isExecutableFile(path) {
		return false
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	return err == nil && strings.Contains(string(out), version)
}

func fileMode0600(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
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

func brewCaskInstalled(name string) bool {
	return exec.Command("brew", "list", "--cask", name).Run() == nil
}

func pathInside(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func createBackupPackage(parent string) (string, error) {
	base := time.Now().UTC().Format("20060102-150405")
	for i := 0; i < 100; i++ {
		name := "edrive-backup-" + base
		if i > 0 {
			name = fmt.Sprintf("edrive-backup-%s-%02d", base, i)
		}
		path := filepath.Join(parent, name)
		if err := os.Mkdir(path, 0700); err == nil {
			return path, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("unable to create backup destination")
}

func withIdentityFile(identity, fn func(string) error) error {
	tmpDir, err := os.MkdirTemp(config.DefaultTempDir(), ".identity-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "identity")
	if err := writePrivateFile(path, identity+"\n"); err != nil {
		return err
	}
	return fn(path)
}

func writePrivateFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(f, content)
	syncErr := error(nil)
	if writeErr == nil {
		syncErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func copyFileAtomic(src, dst string, mode os.FileMode) error {
	tempDir := filepath.Dir(dst)
	temp, err := os.CreateTemp(tempDir, ".copy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		_ = temp.Close()
		return err
	}
	_, copyErr := io.Copy(temp, in)
	inCloseErr := in.Close()
	if copyErr != nil {
		_ = temp.Close()
		return copyErr
	}
	if inCloseErr != nil {
		_ = temp.Close()
		return inCloseErr
	}

	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, dst)
}

const backupReadme = "edrive backup set\n\nThis folder contains:\n- backup.tar.zst.age: the encrypted workspace backup\n- recovery-key.txt: the private recovery identity needed to decrypt it\n\nKeep this folder private. The recovery key is never displayed by edrive during normal operation.\n"
