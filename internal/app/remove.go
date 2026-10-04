package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/macos"
	"github.com/faizahmd2/edrive/internal/ui"
)

// Remove deletes edrive's local state from this Mac. The local vault is only
// removed once a sync confirms the cloud has everything. Cloud data, trash
// and backups are never touched.
func (a App) Remove() error {
	fmt.Println("This removes edrive from this Mac. Your cloud vault is not touched.")
	ok, err := ui.Confirm("Continue?")
	if err != nil || !ok {
		fmt.Println("Cancelled.")
		return err
	}

	return withLock(func() error {
		if mounted() {
			if err := a.crypto().Lock(); err != nil {
				return fmt.Errorf("could not lock the workspace: %w", err)
			}
		}
		clearSession()

		removeVault := false
		if s := a.syncer(); s != nil {
			fmt.Println("Making sure the cloud has everything...")
			res, err := s.Run(false)
			if err == nil {
				if plan, perr := s.Preview(); perr == nil && plan.Empty() && res.Plan.HeldDeletions == 0 {
					removeVault = true
				}
			}
			fmt.Println("  " + describeSync(res, err))
		}

		paths := []string{config.WorkspacePath(), config.RuntimeDir(), config.StateDir(), config.ToolsDir(), config.DefaultPath(), config.LegacyDevicesPath()}
		if removeVault {
			paths = append(paths, config.LocalVaultPath())
		}
		for _, p := range paths {
			if p == config.WorkspacePath() && mounted() {
				continue
			}
			if err := os.RemoveAll(p); err != nil {
				return fmt.Errorf("remove %s: %w", p, err)
			}
		}
		if err := macos.KeychainDelete(config.KeychainService, config.VaultPasswordAccount); err != nil {
			return err
		}

		fmt.Println("edrive removed from this Mac.")
		if !removeVault {
			fmt.Println("Kept your local vault at", config.LocalVaultPath(), "because the cloud could not confirm it has everything.")
		}
		for _, keep := range []string{config.BackupDir(), config.LocalTrashDir()} {
			if _, err := os.Stat(keep); err == nil {
				fmt.Println("Kept:", keep)
			}
		}
		fmt.Println("Tools installed with Homebrew stay installed: brew uninstall rclone; brew uninstall --cask fuse-t")
		_ = os.Remove(filepath.Clean(config.Home())) // only succeeds if empty
		return nil
	})
}
