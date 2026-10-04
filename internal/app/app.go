package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/cryptomator"
	"github.com/faizahmd2/edrive/internal/macos"
	"github.com/faizahmd2/edrive/internal/rclone"
	"github.com/faizahmd2/edrive/internal/ui"
	"github.com/faizahmd2/edrive/internal/vaultsync"
)

type App struct {
	Config config.Config
}

func (a App) crypto() *cryptomator.Client {
	return &cryptomator.Client{
		VaultPath:  config.LocalVaultPath(),
		MountPoint: config.WorkspacePath(),
		CLIPath:    cryptomator.CLIPath(config.ToolsDir()),
		RuntimeDir: config.RuntimeDir(),
	}
}

func (a App) rclone() (*rclone.Client, error) {
	path, err := exec.LookPath("rclone")
	if err != nil {
		return nil, fmt.Errorf("rclone is not installed; run 'edrive setup'")
	}
	return rclone.New(path, a.Config.RcloneRemote, a.Config.RclonePath)
}

// syncer returns nil when no cloud is configured (local-only use).
func (a App) syncer() *vaultsync.Syncer {
	s := a.quietSyncer()
	if s != nil {
		s.Progress = func(label string, done, total int) {
			// \r redraws the same line, so the counter ticks up in place.
			fmt.Fprintf(os.Stderr, "\r\033[K  %s %d/%d", label, done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		}
	}
	return s
}

// quietSyncer is the same, without progress output (for the background guard).
func (a App) quietSyncer() *vaultsync.Syncer {
	rc, err := a.rclone()
	if err != nil || !rc.RemoteExists() {
		return nil
	}
	return &vaultsync.Syncer{
		RC:         rc,
		LocalVault: config.LocalVaultPath(),
		Remote:     a.Config.Remote(),
		StatePath:  filepath.Join(config.StateDir(), "sync.json"),
		LocalTrash: config.LocalTrashDir(),
	}
}

func (a App) requireSetup() error {
	if !a.Config.ConfigFound || !macos.KeychainExists(config.KeychainService, config.VaultPasswordAccount) {
		return fmt.Errorf("edrive is not set up yet; run 'edrive setup'")
	}
	return nil
}

func mounted() bool {
	return cryptomator.Mounted(config.WorkspacePath())
}

// withLock serialises commands that mount, unmount or sync, so a background
// auto-lock never races a command you are running.
func withLock(fn func() error) error {
	if err := os.MkdirAll(config.RuntimeDir(), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(config.RuntimeDir(), "edrive.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "Waiting for another edrive command to finish...")
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			return err
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// unlock mounts the workspace after Touch ID. It reports whether this call
// did the mounting, so quick commands know to lock again afterwards.
func (a App) unlock(reason string) (bool, error) {
	if mounted() {
		return false, nil
	}
	if err := a.requireSetup(); err != nil {
		return false, err
	}
	if err := prepareMountPoint(); err != nil {
		return false, err
	}
	if err := macos.Authenticate(reason); err != nil {
		if errors.Is(err, macos.ErrCancelled) {
			return false, fmt.Errorf("cancelled")
		}
		return false, err
	}
	password, err := macos.KeychainGet(config.KeychainService, config.VaultPasswordAccount)
	if err != nil {
		if errors.Is(err, macos.ErrNotFound) {
			return false, fmt.Errorf("the vault password is not stored yet; run 'edrive setup'")
		}
		return false, fmt.Errorf("could not read the vault password from Keychain (%v); if you just updated edrive, choose 'Always Allow' when macOS asks", err)
	}
	defer ui.Wipe(password)
	if err := a.crypto().Unlock(password); err != nil {
		return false, err
	}
	return true, nil
}

// lock unmounts the workspace. If anything changed, a background sync
// uploads it; the lock itself never waits for the network.
func (a App) lock() error {
	if mounted() {
		if err := a.crypto().Lock(); err != nil {
			if errors.Is(err, cryptomator.ErrBusy) {
				return fmt.Errorf("macOS refused to unmount the workspace even when forced; run 'edrive lock' again")
			}
			return err
		}
	}
	clearSession()
	startBackgroundSync(syncAfterLock)
	return nil
}

func describeSync(res vaultsync.Result, err error) string {
	if err != nil {
		if errors.Is(err, vaultsync.ErrIncomplete) {
			return "skipped: " + err.Error() + ". Run 'edrive doctor'."
		}
		return "offline or unreachable; your files are safe locally and will sync next time."
	}
	var parts []string
	if res.Uploaded > 0 {
		parts = append(parts, fmt.Sprintf("%d up", res.Uploaded))
	}
	if res.Fetched > 0 {
		parts = append(parts, fmt.Sprintf("%d down", res.Fetched))
	}
	if res.Trashed > 0 {
		parts = append(parts, fmt.Sprintf("%d moved to trash", res.Trashed))
	}
	msg := "up to date."
	if len(parts) > 0 {
		msg = "done (" + strings.Join(parts, ", ") + ")."
	}
	if res.Plan.Conflicts > 0 {
		msg += fmt.Sprintf(" %d file(s) changed on two devices; kept the newest, older copy is in trash.", res.Plan.Conflicts)
	}
	if res.Plan.HeldDeletions > 0 {
		msg += fmt.Sprintf(" %d removed file(s) were kept on the other side because a lot disappeared at once; run 'edrive sync --allow-deletes' if that was intended.", res.Plan.HeldDeletions)
	}
	return msg
}

// prepareMountPoint makes sure the workspace folder exists and is empty
// before mounting. Anything left behind (e.g. a file saved after the vault
// locked) is moved aside, never deleted.
func prepareMountPoint() error {
	ws := config.WorkspacePath()
	if err := os.MkdirAll(ws, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(ws)
	if err != nil {
		return err
	}
	var leftovers []os.DirEntry
	for _, e := range entries {
		if e.Name() == ".DS_Store" {
			_ = os.Remove(filepath.Join(ws, e.Name()))
			continue
		}
		leftovers = append(leftovers, e)
	}
	if len(leftovers) == 0 {
		return nil
	}
	aside := filepath.Join(config.Home(), "unencrypted-leftovers-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(aside, 0700); err != nil {
		return err
	}
	for _, e := range leftovers {
		if err := os.Rename(filepath.Join(ws, e.Name()), filepath.Join(aside, e.Name())); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Note: found unencrypted files in the workspace folder while it was locked.\nMoved them to %s. Copy them back in after the workspace opens.\n", aside)
	return nil
}
