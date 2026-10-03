package app

import (
	"fmt"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/vault"
)

func (a App) Diff() error {
	if err := a.requireConfigured(); err != nil {
		return err
	}

	if mounted(config.WorkspacePath()) {
		return fmt.Errorf("workspace is unlocked; run 'edrive lock' before diff")
	}

	state, err := vault.Inspect(config.LocalVaultPath())
	if err != nil {
		return err
	}
	if !state.Exists || !state.Complete {
		return fmt.Errorf("local Cryptomator vault is missing or incomplete; run 'edrive setup'")
	}

	rc, err := a.rcloneClient()
	if err != nil {
		return err
	}
	if err := rc.CheckConfigured(); err != nil {
		return err
	}

	fmt.Println("Comparing encrypted vault:")
	fmt.Println("  local:  " + config.LocalVaultPath())
	fmt.Println("  remote: " + rc.Remote())
	fmt.Println()

	report, err := rc.Diff(config.LocalVaultPath())
	if err != nil {
		return err
	}
	fmt.Print(report)
	return nil
}
