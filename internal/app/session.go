package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/macos"
)

// session describes an open workspace. The guard process reads it on every
// tick, so re-running 'edrive open' simply extends the deadline.
type session struct {
	Deadline time.Time `json:"deadline"`
	Finder   bool      `json:"finder"` // lock when the workspace's Finder window closes
}

func sessionPath() string  { return filepath.Join(config.RuntimeDir(), "session.json") }
func guardPIDPath() string { return filepath.Join(config.RuntimeDir(), "guard.pid") }

func readSession() (session, bool) {
	data, err := os.ReadFile(sessionPath())
	if err != nil {
		return session{}, false
	}
	var s session
	if json.Unmarshal(data, &s) != nil {
		return session{}, false
	}
	return s, true
}

func writeSession(s session) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(sessionPath(), data, 0600)
}

func clearSession() {
	_ = os.Remove(sessionPath())
}

// startGuard launches the background auto-lock process unless it is running.
func startGuard() error {
	if guardRunning() {
		return nil
	}
	return spawnDetached("__guard")
}

func guardRunning() bool {
	b, err := os.ReadFile(guardPIDPath())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	return err == nil && strings.Contains(string(out), "__guard")
}

func spawnDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background helper: %w", err)
	}
	return cmd.Process.Release()
}

const guardTick = 2 * time.Second

// RunGuard is the hidden background process that locks the workspace when
// its time is up, its Finder window closes, the screen locks or the Mac
// sleeps. It exits once the workspace is locked.
func (a App) RunGuard() error {
	if err := os.WriteFile(guardPIDPath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		return err
	}
	defer os.Remove(guardPIDPath())

	ws := config.WorkspacePath()
	finderSeen := false
	finderFailures := 0
	fallback := time.Now().Add(config.OpenDuration)
	last := time.Now().Round(0) // wall clock, so time spent asleep is visible

	for {
		time.Sleep(guardTick)
		if !mounted() {
			clearSession()
			return nil
		}

		now := time.Now().Round(0)
		reason := ""
		if now.Sub(last) > 30*time.Second {
			reason = "Mac was asleep"
		}
		last = now

		s, ok := readSession()
		if !ok {
			s = session{Deadline: fallback}
		}
		switch {
		case reason != "":
		case now.After(s.Deadline):
			reason = "time is up"
		case macos.ScreenLocked():
			reason = "screen locked"
		case s.Finder && finderFailures < 3:
			showing, err := finderShowing(ws)
			switch {
			case err != nil:
				finderFailures++
			case showing:
				finderSeen = true
			case finderSeen:
				reason = "Finder window closed"
			}
		}
		if reason == "" {
			continue
		}

		err := withLock(a.lock)
		if err == nil {
			logGuard("locked: " + reason)
			return nil
		}
		// Something is still using the files; try again shortly.
		logGuard("could not lock yet (" + reason + "): " + err.Error())
		time.Sleep(10 * time.Second)
	}
}

// finderShowing reports whether any Finder window is showing the workspace
// or a folder inside it. Finder is asked for each window's URL; converting a
// window target to an alias fails for some volumes, including FUSE mounts.
func finderShowing(ws string) (bool, error) {
	const script = `set out to ""
tell application "Finder"
	repeat with i from 1 to (count of Finder windows)
		try
			set out to out & (URL of target of Finder window i) & linefeed
		end try
	end repeat
end tell
return out`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).Output()
	if err != nil {
		return false, err
	}
	return windowsShow(string(out), ws), nil
}

// windowsShow checks Finder window URLs (one per line) against the workspace.
func windowsShow(urls, ws string) bool {
	prefix := filepath.Clean(ws)
	for _, line := range strings.Split(urls, "\n") {
		u, err := url.Parse(strings.TrimSpace(line))
		if err != nil || u.Scheme != "file" {
			continue
		}
		p := filepath.Clean(u.Path)
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// errNoAutomation means macOS denied edrive permission to ask Finder about
// its windows (System Settings > Privacy & Security > Automation).
var errNoAutomation = errors.New("no Finder automation permission")

// checkFinderAccess triggers the one-time macOS permission prompt while the
// user is still at the terminal, instead of from the background later.
func checkFinderAccess() error {
	const script = `tell application "Finder" to count Finder windows`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "-1743") {
			return errNoAutomation
		}
		return err
	}
	return nil
}

func logGuard(line string) {
	f, err := os.OpenFile(filepath.Join(config.RuntimeDir(), "guard.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
}
