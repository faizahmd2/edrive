package app

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/faizahmd2/edrive/internal/backup"
	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/ui"
)

// Backup writes one standalone, passphrase-encrypted copy of the workspace.
// It replaces the previous backup; the cloud vault is the live copy.
func (a App) Backup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := fs.String("o", filepath.Join(config.BackupDir(), "edrive-backup"+backup.Suffix), "")
	ownPassphrase := fs.Bool("passphrase", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: edrive backup [-o file] [--passphrase]")
	}
	out := config.Expand(*output)
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		out = filepath.Join(out, "edrive-backup"+backup.Suffix)
	}

	passphrase, err := choosePassphrase(*ownPassphrase)
	if err != nil {
		return err
	}

	err = a.withVault("create an edrive backup", false, func() error {
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		partial := out + ".partial"
		f, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		createErr := backup.Create(config.WorkspacePath(), passphrase, f)
		if createErr == nil {
			createErr = f.Sync()
		}
		closeErr := f.Close()
		if createErr == nil {
			createErr = closeErr
		}
		if createErr != nil {
			_ = os.Remove(partial)
			return fmt.Errorf("create backup: %w", createErr)
		}
		return os.Rename(partial, out)
	})
	if err != nil {
		return err
	}

	size := ""
	if info, err := os.Stat(out); err == nil {
		size = fmt.Sprintf(" (%.1f MB)", float64(info.Size())/1e6)
	}
	fmt.Println("Backup saved:", out+size)
	if !*ownPassphrase {
		fmt.Println()
		fmt.Println("Passphrase (shown once, not stored anywhere; save it in your password manager):")
		fmt.Println()
		fmt.Println("    " + passphrase)
		fmt.Println()
	}
	fmt.Println("Restore on any Mac:  edrive decode", filepath.Base(out))
	fmt.Println("Or without edrive:   age -d", filepath.Base(out), "| zstd -d | tar x")

	if _, err := os.Stat(config.LegacyRecoveryKeyPath()); err == nil {
		fmt.Println()
		fmt.Println("Note: an old unencrypted recovery key from a previous edrive version is at")
		fmt.Println("  " + config.LegacyRecoveryKeyPath())
		fmt.Println("It only opens backups made before this version. Move it into your password manager and delete the file.")
	}
	return nil
}

func choosePassphrase(own bool) (string, error) {
	if !own {
		return backup.GeneratePassphrase()
	}
	first, err := ui.ReadSecret("Backup passphrase (12+ characters): ")
	if err != nil {
		return "", err
	}
	defer ui.Wipe(first)
	if len(first) < 12 {
		return "", fmt.Errorf("passphrase must be at least 12 characters")
	}
	second, err := ui.ReadSecret("Repeat passphrase: ")
	if err != nil {
		return "", err
	}
	defer ui.Wipe(second)
	if string(first) != string(second) {
		return "", fmt.Errorf("passphrases do not match")
	}
	return string(first), nil
}

// Decode restores a backup. It needs no setup, cloud or Cryptomator.
func Decode(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: edrive decode <backup-file> [old-recovery-key-file]")
	}
	keyFile := ""
	passphrase := ""
	if len(args) == 2 {
		keyFile = args[1]
	} else {
		secret, err := ui.ReadSecret("Backup passphrase: ")
		if err != nil {
			return err
		}
		passphrase = string(secret)
		ui.Wipe(secret)
	}
	ids, err := backup.Identities(passphrase, keyFile)
	if err != nil {
		return err
	}
	out, err := backup.Decode(args[0], ids)
	if err != nil {
		return err
	}
	fmt.Println("Restored to:", out)
	return nil
}
