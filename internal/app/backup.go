package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/faiz/edrive/internal/ageutil"
	"github.com/faiz/edrive/internal/backup"
	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/device"
	"github.com/faiz/edrive/internal/keychain"
)

const recoveryFileName = "edrive-recovery-key.txt"

func (a App) Backup() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}

	agePath, zstdPath, err := a.BackupToolPaths()
	if err != nil {
		return err
	}

	recoveryIdentity, err := ensureRecoveryIdentity(agePath)
	if err != nil {
		return err
	}
	if err := a.ensureUnlocked(); err != nil {
		return err
	}

	destination := config.BackupDir()
	if err := os.MkdirAll(destination, 0700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}

	deviceRecipients, err := device.Recipients()
	if err != nil {
		return err
	}
	keygenPath, err := ageutil.KeygenPath(agePath)
	if err != nil {
		return err
	}
	recoveryRecipient, err := ageutil.Recipient(recoveryIdentity, keygenPath, config.TempDir())
	if err != nil {
		return err
	}
	recipients := append(append([]string{}, deviceRecipients...), recoveryRecipient)

	recoveryPath := filepath.Join(destination, recoveryFileName)
	exported, err := ensureExportedRecoveryKey(recoveryPath, recoveryIdentity, keygenPath)
	if err != nil {
		return err
	}

	finalPath := filepath.Join(destination, timestampedBackupName())
	partialPath := finalPath + ".partial"
	out, err := os.OpenFile(partialPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}

	if err := backup.Create(config.WorkspacePath(), recipients, agePath, zstdPath, out); err != nil {
		_ = out.Close()
		_ = os.Remove(partialPath)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(partialPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(partialPath)
		return err
	}
	if err := os.Rename(partialPath, finalPath); err != nil {
		_ = os.Remove(partialPath)
		return fmt.Errorf("finalize backup: %w", err)
	}

	fmt.Println()
	fmt.Println("Backup saved:", finalPath)
	if exported {
		fmt.Println("Recovery key saved:", recoveryPath)
	} else {
		fmt.Println("Recovery key already exists:", recoveryPath)
	}
	return nil
}

func ensureRecoveryIdentity(agePath string) (string, error) {
	if keychain.RecoveryExists() {
		value, err := keychain.GetRecovery()
		if err != nil {
			return "", fmt.Errorf("edrive needs access to its recovery key in the macOS Keychain")
		}
		return value, nil
	}

	fmt.Println("Creating your edrive recovery key in macOS Keychain...")
	keygenPath, err := ageutil.KeygenPath(agePath)
	if err != nil {
		return "", err
	}
	value, err := ageutil.GenerateIdentity(keygenPath, config.TempDir())
	if err != nil {
		return "", err
	}
	if err := keychain.SetRecovery(value); err != nil {
		return "", fmt.Errorf("store the edrive recovery key in macOS Keychain: %w", err)
	}
	return value, nil
}

func ensureExportedRecoveryKey(path, identity, keygenPath string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil {
		value := strings.TrimSpace(string(existing))
		if value == "" {
			return false, fmt.Errorf("recovery key file is empty: %s", path)
		}
		existingRecipient, err := ageutil.Recipient(value, keygenPath, config.TempDir())
		if err != nil {
			return false, fmt.Errorf("recovery key file is invalid: %s", path)
		}
		currentRecipient, err := ageutil.Recipient(identity, keygenPath, config.TempDir())
		if err != nil {
			return false, err
		}
		if existingRecipient != currentRecipient {
			return false, fmt.Errorf("recovery key file belongs to a different edrive recovery key: %s", path)
		}
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	_, writeErr := f.WriteString(identity + "\n")
	syncErr := error(nil)
	if writeErr == nil {
		syncErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return false, writeErr
		}
		if syncErr != nil {
			return false, syncErr
		}
		return false, closeErr
	}
	return true, nil
}
