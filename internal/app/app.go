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

func (a App) Doctor() error {
	fmt.Println("EDRIVE DOCTOR")
	fmt.Println()

	type check struct {
		name   string
		ok     bool
		detail string
	}
	var checks []check

	configOK := a.Config.ConfigFound && fileMode0600(a.Config.ConfigPath)
	configDetail := a.Config.ConfigPath
	if !a.Config.ConfigFound {
		configDetail = "not configured"
	} else if !fileMode0600(a.Config.ConfigPath) {
		configDetail = "permissions should be 0600"
	}
	checks = append(checks, check{"Config", configOK, configDetail})

	dataOK := isDir(a.Config.DataRoot)
	checks = append(checks, check{"Workspace", dataOK, a.Config.DataRoot})

	if runtime.GOOS == "darwin" {
		driveRoot, err := (provider.GoogleDrive{StorageName: a.Config.StorageName}).Root()
		if err != nil {
			checks = append(checks, check{"Google Drive", false, "local storage unavailable"})
		} else {
			checks = append(checks, check{"Google Drive", true, driveRoot})
			vault := filepath.Join(driveRoot, a.Config.StorageName)
			checks = append(checks, check{"Encrypted vault", isFile(filepath.Join(vault, "vault.cryptomator")), vault})
			if _, err := cryptomator.DiscoverVaultID(vault); err != nil {
				checks = append(checks, check{"Cryptomator registration", false, "vault is not registered"})
			} else {
				checks = append(checks, check{"Cryptomator registration", true, "ready"})
			}
		}
		checks = append(checks, check{"Google Drive app", isDir("/Applications/Google Drive.app"), "/Applications/Google Drive.app"})
		checks = append(checks, check{"FUSE-T", brewCaskInstalled("fuse-t"), "installed"})
	}

	checks = append(checks, toolCheck("age", a.Config.AgePath, ageVersion))
	checks = append(checks, toolCheck("zstd", a.Config.ZstdPath, zstdVersion))
	checks = append(checks, check{"Cryptomator CLI", isExecutableFile(a.Config.CryptomatorCLI), a.Config.CryptomatorCLI})

	recipientCount, recErr := loadRecipients(a.Config.Recipients)
	checks = append(checks, check{"Recipients", recErr == nil, fmt.Sprintf("%d recipient(s)", recipientCount)})
	checks = append(checks, check{"Device identity", keychain.Exists(keychain.DeviceIdentity), "macOS Keychain"})
	checks = append(checks, check{"Recovery identity", keychain.Exists(keychain.RecoveryIdentity), "macOS Keychain"})

	all := true
	for _, c := range checks {
		mark := "✓"
		if !c.ok {
			mark = "✗"
			all = false
		}
		fmt.Printf("%-23s %s  %s
", c.name, mark, c.detail)
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
	fmt.Printf("Workspace: %s
", a.Config.DataRoot)
	fmt.Printf("State:     %s
", workspaceState(a.Config.DataRoot))

	storageRoot, err := (provider.GoogleDrive{StorageName: a.Config.StorageName}).Root()
	if err != nil {
		fmt.Println("Storage:   unavailable")
	} else {
		fmt.Println("Storage:   Google Drive")
		fmt.Printf("Vault:     %s
", filepath.Join(storageRoot, a.Config.StorageName))
	}

	if a.Config.LastBackupDir == "" {
		fmt.Println("Backups:   no previous destination")
	} else {
		fmt.Printf("Backups:   %s
", a.Config.LastBackupDir)
	}

	fmt.Println()
	return nil
}

func (a App) Open() error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	if err := ui.Open(a.Config.DataRoot); err != nil {
		return err
	}
	return nil
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
	fmt.Printf("Workspace opened: %s
", a.Config.DataRoot)
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
	if _, err := os.Stat(filepath.Join(vaultPath, "vault.cryptomator")); err != nil {
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

	storageRoot, err := (provider.GoogleDrive{StorageName: a.Config.StorageName}).Root()
	if err != nil {
		return err
	}
	vaultPath := filepath.Join(storageRoot, a.Config.StorageName)

	client := cryptomator.New(cryptomator.Config{
		VaultPath:  vaultPath,
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

	fmt.Println("Requesting secure recovery access...")
	recoveryIdentity, err := keychain.Get(keychain.RecoveryIdentity)
	if err != nil {
		return err
	}

	if err := a.ensureUnlocked(); err != nil {
		return err
	}

	defaultDir := a.Config.LastBackupDir
	if !isDir(defaultDir) {
		defaultDir = a.Config.DataRoot
	}
	destination, selected, err := ui.ChooseFolder("Choose where to save this edrive backup", defaultDir)
	if err != nil {
		return err
	}
	if !selected {
		fmt.Println("Backup cancelled.")
		return nil
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
	syncErr := error(nil)
	closeErr := archive.Close()
	if createErr == nil {
		syncErr = syncFile(archivePath)
	}
	if createErr != nil {
		return createErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
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
	if err := copyFile(archivePath, finalArchive, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageDir, "recovery-key.txt"), []byte(recoveryIdentity+"
"), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageDir, "README.txt"), []byte(backupReadme), 0600); err != nil {
		return err
	}
	if err := syncDir(packageDir); err != nil {
		return err
	}

	a.Config.LastBackupDir = destination
	if err := a.Config.Save(); err != nil {
		return err
	}

	committed = true
	_ = ui.Open(packageDir)

	info, err := os.Stat(finalArchive)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("Backup saved: %s
", packageDir)
	fmt.Printf("Files:        %d
", len(manifest.Files))
	fmt.Printf("Encrypted:    %s
", formatBytes(info.Size()))
	return nil
}

func (a App) Restore() error {
	if err := a.requireTools(); err != nil {
		return err
	}

	defaultDir := a.Config.LastBackupDir
	if !isDir(defaultDir) {
		defaultDir = config.DefaultDataRoot()
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
	recoveryKeyPath := filepath.Join(backupDir, "recovery-key.txt")
	if !isFile(archivePath) {
		return fmt.Errorf("selected folder does not contain an edrive backup")
	}

	recoveryIdentity, err := keychain.Get(keychain.RecoveryIdentity)
	if err != nil {
		if !isFile(recoveryKeyPath) {
			return err
		}
		b, readErr := os.ReadFile(recoveryKeyPath)
		if readErr != nil {
			return readErr
		}
		recoveryIdentity = strings.TrimSpace(string(b))
		if recoveryIdentity == "" {
			return fmt.Errorf("backup recovery key is empty")
		}
		if setErr := keychain.Set(keychain.RecoveryIdentity, recoveryIdentity); setErr != nil {
			return setErr
		}
	}

	restoreRoot, selected, err := ui.ChooseFolder(
		"Choose an empty folder to restore the backup into",
		filepath.Join(config.DefaultDataRoot(), "restore"),
	)
	if err != nil {
		return err
	}
	if !selected {
		fmt.Println("Restore cancelled.")
		return nil
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
			restoreRoot,
			true,
		)
		if err != nil {
			return err
		}
		fmt.Printf("Restored: %s
", restoreRoot)
		fmt.Printf("Files:    %d
", len(manifest.Files))
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
	if _, err := loadRecipients(a.Config.Recipients); err != nil {
		return err
	}
	return nil
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
	marker := " on " + filepath.Clean(path) + " ("
	return strings.Contains(string(out), marker)
}

func loadRecipients(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(b), "
") {
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

func toolCheck(name, path, version string) struct {
	name   string
	ok     bool
	detail string
} {
	return struct {
		name   string
		ok     bool
		detail string
	}{name, toolVersionMatches(path, version), path}
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
		} else if !os.IsExist(err) {
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
	if err := os.WriteFile(path, []byte(identity+"
"), 0600); err != nil {
		return err
	}
	return fn(path)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := error(nil)
	if copyErr == nil {
		syncErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

const backupReadme = "edrive backup set

This folder contains:
- backup.tar.zst.age: the encrypted backup
- recovery-key.txt: the private recovery identity required to decrypt it

Keep both files together and protect this folder like a private secret.

The recovery key is never printed by edrive during normal operation.
"
