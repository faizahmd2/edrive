package app

import (
	"fmt"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/keychain"
	"github.com/faizahmd2/edrive/internal/pass"
)

func (a App) Pass(args []string) error {
	if err := a.requireConfigured(); err != nil {
		return err
	}

	switch len(args) {
	case 1:
		if err := a.ensureUnlocked(); err != nil {
			return err
		}
		value, err := pass.Get(config.WorkspacePath(), args[0])
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil

	case 2:
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
		return fmt.Errorf("usage: edrive pass <name> [value]")
	}
}
