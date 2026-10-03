package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/keychain"
	"github.com/faizahmd2/edrive/internal/pass"
	"github.com/faizahmd2/edrive/internal/ui"
)

func (a App) Pass(args []string) error {
	if err := a.requireConfigured(); err != nil {
		return err
	}

	switch {
	case len(args) == 1 && isPassListCommand(args[0]):
		if err := a.ensureUnlocked(); err != nil {
			return err
		}
		names, err := pass.List(config.WorkspacePath())
		if err != nil {
			return err
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return nil

	case len(args) == 1:
		if err := a.ensureUnlocked(); err != nil {
			return err
		}
		value, err := pass.Get(config.WorkspacePath(), args[0])
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil

	case len(args) == 2 && args[0] == "set":
		return a.passSetEditor(args[1])

	case len(args) == 2:
		if err := keychain.UnlockDefault(); err != nil {
			return fmt.Errorf("unlock macOS Keychain before changing pass entry: %w", err)
		}
		if err := a.ensureUnlocked(); err != nil {
			return err
		}
		if err := pass.Set(config.WorkspacePath(), args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("Pass entry %q updated.\n", args[0])
		return nil

	default:
		return fmt.Errorf("usage: edrive pass <key> | edrive pass ls | edrive pass list | edrive pass <key> <value> | edrive pass set <key>")
	}
}

func (a App) passSetEditor(name string) error {
	if err := keychain.UnlockDefault(); err != nil {
		return fmt.Errorf("unlock macOS Keychain before changing pass entry: %w", err)
	}
	if err := a.ensureUnlocked(); err != nil {
		return err
	}

	editDir := filepath.Join(config.WorkspacePath(), "pass")
	if err := os.MkdirAll(editDir, 0700); err != nil {
		return fmt.Errorf("create pass editor directory: %w", err)
	}
	tmp, err := os.CreateTemp(editDir, ".pass-edit-*")
	if err != nil {
		return fmt.Errorf("create pass editor file: %w", err)
	}
	path := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(path)
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect pass editor file: %w", err)
	}

	if pass.Exists(config.WorkspacePath(), name) {
		existing, err := pass.ReadText(config.WorkspacePath(), name)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if _, err := tmp.WriteString(existing); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("seed pass editor file: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close pass editor file: %w", err)
	}

	fmt.Println("Editing pass entry:", name)
	fmt.Println("Save and exit the editor to update the entry.")
	if err := ui.EditFile(path); err != nil {
		return fmt.Errorf("edit pass entry %q: %w", name, err)
	}

	value, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read edited pass entry: %w", err)
	}
	text := string(value)
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("pass entry cannot be empty")
	}
	if err := pass.SetText(config.WorkspacePath(), name, text); err != nil {
		return err
	}
	fmt.Printf("Pass entry %q updated.\n", name)
	return nil
}

func isPassListCommand(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "ls" || value == "list"
}
