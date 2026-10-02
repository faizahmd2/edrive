package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/faiz/edrive/internal/ageutil"
	"github.com/faiz/edrive/internal/config"
	"github.com/faiz/edrive/internal/cryptomator"
	"github.com/faiz/edrive/internal/deps"
	"github.com/faiz/edrive/internal/device"
	"github.com/faiz/edrive/internal/keychain"
	"github.com/faiz/edrive/internal/rclone"
	"github.com/faiz/edrive/internal/ui"
	"github.com/faiz/edrive/internal/vault"
)

func Run() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("setup currently supports macOS")
	}

	cfg := config.Defaults(config.DefaultPath())
	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return fmt.Errorf("create edrive state: %w", err)
	}
	if err := os.MkdirAll(config.TempDir(), 0700); err != nil {
		return fmt.Errorf("create edrive temporary state: %w", err)
	}

	fmt.Println("edrive setup")
	fmt.Println()
	fmt.Println("Workspace:       ", config.WorkspacePath())
	fmt.Println("Encrypted vault:", config.LocalVaultPath())
	fmt.Println("Remote vault:   ", cfg.RcloneRemote+":"+cfg.RclonePath)
	fmt.Println()

	paths, err := deps.Ensure()
	if err != nil {
		return err
	}
	cfg.AgePath = paths.Age
	cfg.ZstdPath = paths.Zstd
	cfg.CryptomatorCLI = paths.CryptomatorCLI

	rc, err := rclone.New(paths.Rclone, cfg.RcloneRemote, cfg.RclonePath)
	if err != nil {
		return err
	}
	if err := rc.EnsureConfigured(); err != nil {
		return err
	}
	if err := rc.EnsureRemoteDir(); err != nil {
		return err
	}

	if err := ensureWorkspace(); err != nil {
		return err
	}
	if err := ensureVault(rc); err != nil {
		return err
	}

	keygenPath, err := ageutil.KeygenPath(cfg.AgePath)
	if err != nil {
		return err
	}
	if err := ensureDefaultDevice(keygenPath); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Setup complete.")
	fmt.Println("Workspace:", config.WorkspacePath())
	fmt.Println("Remote:", rc.Remote())
	fmt.Println()
	fmt.Println("Use:")
	fmt.Println("  edrive open")
	fmt.Println("  edrive lock")
	fmt.Println("  edrive push")
	fmt.Println("  edrive pull")
	fmt.Println("  edrive backup")
	return nil
}

func ensureWorkspace() error {
	path := config.WorkspacePath()
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("workspace path is not a directory: %s", path)
		}
		if mounted(path) {
			return fmt.Errorf("the edrive workspace is already mounted; run 'edrive lock' before setup")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("workspace path is not empty: %s", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(path, 0700)
}

func ensureVault(rc *rclone.Client) error {
	state, err := vault.Inspect(config.LocalVaultPath())
	if err != nil {
		return err
	}

	remoteHasVault, remoteHasOther, err := rc.RemoteVaultState()
	if err != nil {
		return err
	}
	if remoteHasOther && !remoteHasVault {
		return fmt.Errorf("remote path %s already contains files but is not a Cryptomator vault", rc.Remote())
	}

	switch {
	case remoteHasVault:
		switch {
		case !state.Exists || state.Empty:
			fmt.Println("A remote edrive vault already exists. Pulling it to this Mac...")
			if err := rc.SyncRemoteToLocal(config.LocalVaultPath()); err != nil {
				return err
			}
			state, err = vault.Inspect(config.LocalVaultPath())
			if err != nil {
				return err
			}
		case state.Complete:
			fmt.Println("Local edrive vault already exists. Remote vault found too.")
		default:
			return fmt.Errorf("local vault is incomplete while the remote vault already exists: %s", config.LocalVaultPath())
		}
	case state.Complete:
		fmt.Println("No remote edrive vault exists. Publishing the existing local vault...")
		if err := rc.SyncLocalToRemote(config.LocalVaultPath()); err != nil {
			return err
		}
	case !state.Exists || state.Empty:
		if err := createNewVault(config.LocalVaultPath()); err != nil {
			return err
		}
		if err := rc.SyncLocalToRemote(config.LocalVaultPath()); err != nil {
			return err
		default:
		return fmt.Errorf("local vault contains files but is not a complete Cryptomator vault: %s", config.LocalVaultPath())
		}
	}

	state, err = vault.Inspect(config.LocalVaultPath())
	if err != nil {
		return err
	}
	if !state.Complete {
		return fmt.Errorf("edrive local vault is incomplete: %s", config.LocalVaultPath())
	}
	if _, err := cryptomator.DiscoverVaultID(config.LocalVaultPath()); err != nil {
		if err := ensureRegistered(config.LocalVaultPath()); err != nil {
			return err
		}
	}
	return ensurePasswordStored(config.LocalVaultPath())
}

func createNewVault(vaultPath string) error {
	parent := filepath.Dir(vaultPath)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("One Cryptomator GUI step is required to create the vault.")
	fmt.Println("Create a new vault with:")
	fmt.Println("  Vault name: edrive")
	fmt.Println("  Storage location:", parent)
	fmt.Println()
	fmt.Println("Keep 'Remember password' enabled for this vault.")
	fmt.Println()

	if err := ui.OpenApplication("Cryptomator"); err != nil {
		return fmt.Errorf("open Cryptomator: %w", err)
	}
	if err := ui.Pause("Press Enter after the edrive vault is created: "); err != nil {
		return err
	}
	state, err := vault.Inspect(vaultPath)
	if err != nil {
		return err
	}
	if !state.Complete {
		return fmt.Errorf("the new Cryptomator vault was not created at %s", vaultPath)
	}
	if err := ensureRegistered(vaultPath); err != nil {
		return err
	}
	return ensurePasswordStored(vaultPath)
}

func ensureRegistered(vaultPath string) error {
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err == nil {
		return nil
	}

	fmt.Println()
	fmt.Println("Add the existing edrive vault in Cryptomator.")
	fmt.Println("Choose Add -> Open Existing Vault and select:")
	fmt.Println(" ", filepath.Join(vaultPath, "masterkey.cryptomator"))
	fmt.Println()

	if err := ui.OpenApplication("Cryptomator"); err != nil {
		return fmt.Errorf("open Cryptomator: %w", err)
	}
	if err := ui.Pause("Press Enter after the edrive vault appears in Cryptomator: "); err != nil {
		return err
	}
	if _, err := cryptomator.DiscoverVaultID(vaultPath); err != nil {
		return fmt.Errorf("Cryptomator still has not registered %s", vaultPath)
	}
	return nil
}

func ensurePasswordStored(vaultPath string) error {
	vaultID, err := cryptomator.DiscoverVaultID(vaultPath)
	if err != nil {
		return err
	}
	if cryptomator.CredentialAvailable(vaultID) {
		return nil
	}

	fmt.Println()
	fmt.Println("Cryptomator needs to remember the vault password for CLI unlocks.")
	fmt.Println("Unlock the edrive vault once in Cryptomator and enable")
	fmt.Println("'Remember password' / Keychain storage.")
	fmt.Println()
	if err := ui.Pause("Press Enter after the password is stored: "); err != nil {
		return err
	}
	if !cryptomator.CredentialAvailable(vaultID) {
		return fmt.Errorf("Cryptomator still does not have the vault password in macOS Keychain")
	}
	return nil
}

func ensureDefaultDevice(ageKeygen string) error {
	devices, err := device.List()
	if err != nil {
		return err
	}
	for _, d := range devices {
		if d.Label == "mac-1" {
			return nil
		}
	}
	if keychain.IdentityExists("mac-1") {
		identity, err := keychain.GetIdentity("mac-1")
		if err != nil {
			return fmt.Errorf("edrive needs access to its existing mac-1 device identity")
		}
		_, err = device.Import("mac-1", identity, ageKeygen)
		return err
	}
	_, err = device.AddGenerated("mac-1", ageKeygen)
	return err
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
