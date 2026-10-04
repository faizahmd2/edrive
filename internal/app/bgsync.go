package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
)

// Background sync runs only right after a lock or an unlock, in a detached
// process, so commands never wait for the network. Nothing is scheduled and
// nothing retries on its own; 'edrive status' says when a manual sync helps.
const (
	syncAfterLock   = "lock"   // upload, but only if something changed
	syncAfterUnlock = "unlock" // always check, to bring in edits from other devices
)

// staleAfter is when status/doctor start suggesting 'edrive sync'.
const staleAfter = 24 * time.Hour

func syncLockPath() string  { return filepath.Join(config.RuntimeDir(), "sync.lock") }
func syncAgainPath() string { return filepath.Join(config.RuntimeDir(), "sync.again") }
func syncStatusPath() string {
	return filepath.Join(config.StateDir(), "last-attempt.json")
}

// syncAttempt is the outcome of the most recent sync, kept on this Mac so
// status and doctor never need the network.
type syncAttempt struct {
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
}

func startBackgroundSync(trigger string) {
	if !cloudConfigured() {
		return
	}
	_ = spawnDetached("__sync", trigger)
}

// cloudConfigured is a cheap local check (no rclone call, no network).
func cloudConfigured() bool {
	_, err := os.Stat(config.DefaultPath())
	return err == nil
}

// BackgroundSync is the hidden '__sync' command.
func (a App) BackgroundSync(args []string) error {
	trigger := syncAfterUnlock
	if len(args) == 1 {
		trigger = args[0]
	}
	s := a.quietSyncer()
	if s == nil {
		return nil
	}

	release, ok := trySyncLock()
	if !ok {
		// A sync is already running; ask it to go once more when it finishes,
		// so changes made in the meantime are not left behind.
		_ = os.WriteFile(syncAgainPath(), nil, 0600)
		return nil
	}
	defer release()

	for {
		_ = os.Remove(syncAgainPath())
		if trigger == syncAfterLock && !s.LocalChanged() {
			return nil
		}
		res, err := s.Run(false)
		recordAttempt(err == nil, describeSync(res, err))
		logGuard("sync after " + trigger + ": " + describeSync(res, err))
		if _, again := os.Stat(syncAgainPath()); again != nil {
			return nil
		}
		trigger = syncAfterLock
	}
}

// runForegroundSync is 'edrive sync': same work, shown to you, waiting for
// a background sync to finish first instead of running two at once.
func (a App) runForegroundSync(allowDeletes bool) error {
	s := a.syncer()
	if s == nil {
		return fmt.Errorf("no cloud is configured; run 'edrive cloud add'")
	}
	release, ok := trySyncLock()
	if !ok {
		fmt.Println("A background sync is running; waiting for it...")
		release = waitSyncLock()
	}
	defer release()

	fmt.Println("Syncing...")
	res, err := s.Run(allowDeletes)
	recordAttempt(err == nil, describeSync(res, err))
	if err != nil {
		return fmt.Errorf("sync did not finish: %w (nothing was lost; run it again when the cloud is reachable)", err)
	}
	fmt.Println("  " + describeSync(res, nil))
	return nil
}

func trySyncLock() (func(), bool) {
	f, err := openSyncLock()
	if err != nil {
		return nil, false
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, false
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true
}

func waitSyncLock() func() {
	f, err := openSyncLock()
	if err != nil {
		return func() {}
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
}

func openSyncLock() (*os.File, error) {
	if err := os.MkdirAll(config.RuntimeDir(), 0700); err != nil {
		return nil, err
	}
	return os.OpenFile(syncLockPath(), os.O_CREATE|os.O_RDWR, 0600)
}

func syncRunning() bool {
	release, ok := trySyncLock()
	if ok {
		release()
	}
	return !ok
}

func recordAttempt(ok bool, msg string) {
	data, err := json.Marshal(syncAttempt{At: time.Now().UTC(), OK: ok, Message: msg})
	if err == nil {
		_ = config.WriteFileAtomic(syncStatusPath(), data, 0600)
	}
}

func lastAttempt() (syncAttempt, bool) {
	data, err := os.ReadFile(syncStatusPath())
	if err != nil {
		return syncAttempt{}, false
	}
	var at syncAttempt
	return at, json.Unmarshal(data, &at) == nil
}

// syncSummary describes the sync state from local records only.
// It returns the lines to print and whether a manual sync is worth suggesting.
func (a App) syncSummary() (lines []string, suggest bool) {
	s := a.quietSyncer()
	if s == nil {
		return []string{"Cloud:     not configured ('edrive cloud add')"}, false
	}
	if syncRunning() {
		lines = append(lines, "Sync:      running in the background")
	}
	last := s.LastSync()
	switch {
	case last.IsZero():
		lines = append(lines, "Last sync: never")
		suggest = true
	default:
		lines = append(lines, fmt.Sprintf("Last sync: %s ago", roundDuration(time.Since(last))))
		if time.Since(last) > staleAfter {
			suggest = true
		}
	}
	if at, ok := lastAttempt(); ok && !at.OK && at.At.After(last) {
		lines = append(lines, "Last try:  "+at.Message)
		suggest = true
	}
	if s.LocalChanged() {
		lines = append(lines, "Changes:   waiting to upload")
		suggest = true
	} else {
		lines = append(lines, "Changes:   none on this Mac")
	}
	return lines, suggest
}
