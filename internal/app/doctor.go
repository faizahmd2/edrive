package app

import (
	"debug/macho"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/cryptomator"
	"github.com/faizahmd2/edrive/internal/macos"
	"github.com/faizahmd2/edrive/internal/vault"
)

// Doctor checks that everything actually works, quickly, without prompting.
func (a App) Doctor() error {
	fmt.Println("edrive doctor")
	fmt.Println()
	problems := 0
	check := func(ok bool, name, detail string) {
		mark := "✓"
		if !ok {
			mark = "✗"
			problems++
		}
		fmt.Printf("  %s %-18s %s\n", mark, name, detail)
	}
	note := func(name, detail string) { fmt.Printf("  ! %-18s %s\n", name, detail) }

	check(runtime.GOOS == "darwin", "macOS", runtime.GOOS+"/"+runtime.GOARCH)

	_, err := exec.LookPath("rclone")
	check(err == nil, "rclone", orMissing(err == nil, "installed"))
	_, err = os.Stat(fuseTLibrary)
	check(err == nil, "FUSE-T", orMissing(err == nil, "installed"))

	cli := cryptomator.CLIPath(config.ToolsDir())
	switch {
	case !cryptomator.CLIInstalled(config.ToolsDir()):
		check(false, "Cryptomator CLI", "missing")
	case runtime.GOARCH == "arm64" && !nativeArm64(cli):
		check(false, "Cryptomator CLI", "Intel build (slow under Rosetta)")
	default:
		check(true, "Cryptomator CLI", cryptomator.CLIVersion)
	}

	state, err := vault.Inspect(config.LocalVaultPath())
	check(err == nil && state.Complete, "local vault", orMissing(err == nil && state.Complete, config.LocalVaultPath()))
	check(macos.KeychainExists(config.KeychainService, config.VaultPasswordAccount), "vault password",
		orMissing(macos.KeychainExists(config.KeychainService, config.VaultPasswordAccount), "in Keychain, edrive-only"))
	check(macos.CanAuthenticate(), "Touch ID", orMissing(macos.CanAuthenticate(), "available (password fallback)"))

	if mounted() {
		check(true, "workspace", "open")
	} else {
		check(true, "workspace", "locked")
	}

	// Cloud health comes from this Mac's own sync records; doctor never
	// waits on the network.
	rc, err := a.rclone()
	switch {
	case err != nil:
		check(false, "cloud", "rclone missing")
	case !rc.RemoteExists():
		check(false, "cloud", "not configured")
	default:
		check(true, "cloud", a.Config.Remote())
		lines, suggest := a.syncSummary()
		if !suggest {
			// Healthy: synced within 24h and nothing waiting.
			last := a.quietSyncer().LastSync()
			check(true, "sync", "synced "+roundDuration(time.Since(last))+" ago, nothing waiting")
		} else {
			for _, l := range lines {
				note("sync", l)
			}
			note("sync", "run 'edrive sync' to sync now")
		}
	}

	if _, err := os.Stat(config.LegacyRecoveryKeyPath()); err == nil {
		note("old recovery key", "unencrypted at "+config.LegacyRecoveryKeyPath()+" (move to a password manager, then delete)")
	}

	fmt.Println()
	if problems == 0 {
		fmt.Println("All good.")
		return nil
	}
	fmt.Println("Run 'edrive setup' to fix these. It only touches what is broken.")
	return fmt.Errorf("%d problem(s) found", problems)
}

func orMissing(ok bool, detail string) string {
	if ok {
		return detail
	}
	return "missing"
}

func nativeArm64(path string) bool {
	f, err := macho.Open(path)
	if err != nil {
		fat, ferr := macho.OpenFat(path)
		if ferr != nil {
			return false
		}
		defer fat.Close()
		for _, arch := range fat.Arches {
			if arch.Cpu == macho.CpuArm64 {
				return true
			}
		}
		return false
	}
	defer f.Close()
	return f.Cpu == macho.CpuArm64
}
