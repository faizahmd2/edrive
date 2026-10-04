package cryptomator

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const Mounter = "org.cryptomator.frontend.fuse.mount.FuseTMountProvider"

// ErrBusy means the workspace could not be unmounted because something is
// still using files inside it.
var ErrBusy = errors.New("workspace is busy")

type Client struct {
	VaultPath  string
	MountPoint string
	CLIPath    string
	RuntimeDir string
}

func (c *Client) pidPath() string { return filepath.Join(c.RuntimeDir, "cryptomator.pid") }
func (c *Client) logPath() string { return filepath.Join(c.RuntimeDir, "cryptomator.log") }

// Unlock mounts the vault. The password goes to the CLI over stdin, never argv.
func (c *Client) Unlock(password []byte) error {
	if Mounted(c.MountPoint) {
		return nil
	}
	if !isExecutable(c.CLIPath) {
		return fmt.Errorf("Cryptomator CLI is missing; run 'edrive setup'")
	}
	if err := os.MkdirAll(c.RuntimeDir, 0700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(c.logPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(c.CLIPath,
		"unlock",
		"--password:stdin",
		"--mounter="+Mounter,
		"--mountPoint="+c.MountPoint,
		c.VaultPath,
	)
	// The CLI outlives edrive, so its output goes to a file rather than a pipe.
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Cryptomator CLI: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	_, writeErr := stdin.Write(append(password, '\n'))
	closeErr := stdin.Close()
	if writeErr != nil || closeErr != nil {
		terminate(cmd.Process.Pid)
		<-exited
		return fmt.Errorf("Cryptomator CLI did not accept the password: %s", c.lastLog())
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if Mounted(c.MountPoint) {
			return os.WriteFile(c.pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0600)
		}
		select {
		case <-exited:
			detail := c.lastLog()
			if strings.Contains(strings.ToLower(detail), "invalid passphrase") || strings.Contains(strings.ToLower(detail), "invalidpassphrase") {
				return fmt.Errorf("the stored vault password no longer works (was it changed?); run 'edrive setup --password'")
			}
			return fmt.Errorf("Cryptomator could not mount the vault: %s", detail)
		case <-time.After(100 * time.Millisecond):
		}
	}
	terminate(cmd.Process.Pid)
	<-exited
	return fmt.Errorf("timed out waiting for the vault to mount: %s", c.lastLog())
}

// Lock unmounts the workspace and stops the Cryptomator CLI. It asks
// nicely first, then force-unmounts: no app, open file or terminal sitting in
// the folder can keep the workspace unlocked. Whatever was already saved is
// already encrypted; only unsaved edits inside other apps are left behind.
func (c *Client) Lock() error {
	if !Mounted(c.MountPoint) {
		c.stopStaleProcess()
		return nil
	}

	pid := c.ownedPID()
	if pid > 0 {
		terminate(pid)
	}
	if c.waitUnmounted(3 * time.Second) {
		c.finish(pid)
		return nil
	}

	// Something is holding it open. Force it.
	_ = exec.Command("/usr/sbin/diskutil", "unmount", "force", c.MountPoint).Run()
	if !c.waitUnmounted(2 * time.Second) {
		_ = exec.Command("/sbin/umount", "-f", c.MountPoint).Run()
	}
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if !c.waitUnmounted(3 * time.Second) {
		return ErrBusy
	}
	c.finish(pid)
	return nil
}

func (c *Client) waitUnmounted(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !Mounted(c.MountPoint) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !Mounted(c.MountPoint)
}

// finish makes sure the CLI (and its file server) are gone.
func (c *Client) finish(pid int) {
	if pid > 0 {
		deadline := time.Now().Add(2 * time.Second)
		for processExists(pid) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if processExists(pid) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	_ = os.Remove(c.pidPath())
}

func (c *Client) stopStaleProcess() {
	if pid := c.ownedPID(); pid > 0 {
		terminate(pid)
	}
	_ = os.Remove(c.pidPath())
}

// ownedPID returns the recorded CLI pid if that process is still the CLI.
func (c *Client) ownedPID() int {
	b, err := os.ReadFile(c.pidPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	// Match by name rather than full path so a CLI started by an older edrive
	// (or from another install location) is still recognised and stopped.
	out, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil || !strings.Contains(string(out), "cryptomator-cli") {
		return 0
	}
	return pid
}

func (c *Client) lastLog() string {
	data, err := os.ReadFile(c.logPath())
	if err != nil {
		return "no details available"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	text := strings.TrimSpace(strings.Join(lines, " "))
	if text == "" {
		return "no details available"
	}
	return text
}

func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func terminate(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}
