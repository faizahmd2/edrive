package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/ui"
)

// Open unlocks the workspace and shows it in Finder. It stays open for 30
// minutes (or --for), and locks early when its Finder window closes, the
// screen locks or the Mac sleeps.
func (a App) Open(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	duration := fs.Duration("for", config.OpenDuration, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: edrive open [--for 1h]")
	}

	if !mounted() {
		err := withLock(func() error {
			_, err := a.unlock("open your edrive workspace")
			return err
		})
		if err != nil {
			return err
		}
		startBackgroundSync(syncAfterUnlock) // bring in phone edits without making you wait
	}

	s := session{Deadline: time.Now().Add(*duration), Finder: true}
	if prev, ok := readSession(); ok && prev.Deadline.After(s.Deadline) {
		s.Deadline = prev.Deadline
	}
	if err := ui.Open(config.WorkspacePath()); err != nil {
		return err
	}
	if err := checkFinderAccess(); err != nil {
		s.Finder = false
		if errors.Is(err, errNoAutomation) {
			fmt.Println("Tip: allow your terminal to control Finder (System Settings > Privacy & Security > Automation)")
			fmt.Println("     so edrive can lock as soon as you close the Finder window.")
		}
	}
	if err := writeSession(s); err != nil {
		return err
	}
	if err := startGuard(); err != nil {
		return err
	}

	if s.Finder {
		fmt.Printf("Workspace open. Locks when you close its Finder window, or in %s.\n", roundDuration(time.Until(s.Deadline)))
	} else {
		fmt.Printf("Workspace open. Locks in %s.\n", roundDuration(time.Until(s.Deadline)))
	}
	return nil
}

// Pwd prints the workspace path, unlocking it for 30 minutes if needed.
// Use it to open the workspace in other tools: code "$(edrive pwd)".
func (a App) Pwd() error {
	if !mounted() {
		err := withLock(func() error {
			_, err := a.unlock("open your edrive workspace")
			return err
		})
		if err != nil {
			return err
		}
		startBackgroundSync(syncAfterUnlock)
		if err := writeSession(session{Deadline: time.Now().Add(config.OpenDuration)}); err != nil {
			return err
		}
		if err := startGuard(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Workspace open for %s (or until you run 'edrive lock').\n", roundDuration(config.OpenDuration))
	}
	fmt.Println(config.WorkspacePath())
	return nil
}

func (a App) Lock() error {
	if !mounted() {
		clearSession()
		fmt.Println("Workspace is locked.")
		return nil
	}
	if err := withLock(a.lock); err != nil {
		return err
	}
	fmt.Println("Workspace locked.")
	return nil
}

// Sync brings the local vault and the cloud together. push and pull are
// aliases: changes always flow both ways and nothing is ever deleted.
func (a App) Sync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	allowDeletes := fs.Bool("allow-deletes", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: edrive sync [--allow-deletes]")
	}
	if err := a.requireSetup(); err != nil {
		return err
	}
	return a.runForegroundSync(*allowDeletes)
}

// Diff shows what a sync would do, without changing anything.
func (a App) Diff() error {
	s := a.syncer()
	if s == nil {
		return fmt.Errorf("no cloud is configured; run 'edrive cloud add'")
	}
	plan, err := s.Preview()
	if err != nil {
		return err
	}
	if plan.Empty() {
		fmt.Println("Local vault and cloud are in sync.")
		return nil
	}
	fmt.Printf("To upload:   %d file(s)\n", len(plan.Upload)+len(plan.MkdirRemote))
	fmt.Printf("To download: %d file(s)\n", len(plan.Download)+len(plan.MkdirLocal))
	fmt.Printf("To trash:    %d file(s) (kept in trash, never deleted)\n", len(plan.TrashLocal)+len(plan.TrashRemote))
	if plan.Conflicts > 0 {
		fmt.Printf("Changed on both sides: %d (newest wins, older kept in trash)\n", plan.Conflicts)
	}
	fmt.Println("Run 'edrive sync' to apply.")
	return nil
}

func (a App) Status() error {
	if mounted() {
		s, ok := readSession()
		switch {
		case !ok:
			fmt.Println("Workspace: open")
		case s.Finder:
			fmt.Printf("Workspace: open (locks when its Finder window closes, or in %s)\n", roundDuration(time.Until(s.Deadline)))
		default:
			fmt.Printf("Workspace: open (locks in %s)\n", roundDuration(time.Until(s.Deadline)))
		}
	} else {
		fmt.Println("Workspace: locked")
	}

	lines, suggest := a.syncSummary()
	for _, l := range lines {
		fmt.Println(l)
	}
	if suggest {
		fmt.Println()
		fmt.Println("Run 'edrive sync' to sync now.")
	}
	return nil
}

func roundDuration(d time.Duration) string {
	if d < time.Minute {
		return "under a minute"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()+0.5))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%.1f h", d.Hours())
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}
