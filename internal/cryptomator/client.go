package cryptomator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	VaultPath       string
	VaultID         string
	MountPoint      string
	CLIPath         string
	Mounter         string
	KeychainService string
	RuntimeDir      string
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Unlock() error {
	if mounted(c.cfg.MountPoint) {
		return fmt.Errorf("vault already mounted at %s", c.cfg.MountPoint)
	}

	if c.cfg.VaultPath == "" {
		return fmt.Errorf("Cryptomator vault path is not configured")
	}
	if c.cfg.VaultID == "" {
		return fmt.Errorf("Cryptomator vault ID is not configured")
	}
	if c.cfg.CLIPath == "" {
		return fmt.Errorf("Cryptomator CLI path is not configured")
	}
	if c.cfg.Mounter == "" {
		return fmt.Errorf("Cryptomator mounter is not configured")
	}
	if c.cfg.MountPoint == "" {
		return fmt.Errorf("Cryptomator mount point is not configured")
	}
	if c.cfg.KeychainService == "" {
		return fmt.Errorf("Cryptomator Keychain service is not configured")
	}

	if info, err := os.Stat(c.cfg.VaultPath); err != nil || !info.IsDir() {
		return fmt.Errorf("Cryptomator vault path is unavailable: %s", c.cfg.VaultPath)
	}

	if _, err := os.Stat(c.cfg.CLIPath); err != nil {
		return fmt.Errorf("Cryptomator CLI unavailable: %w", err)
	}

	if err := os.MkdirAll(c.cfg.RuntimeDir, 0700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}

	logPath := filepath.Join(c.cfg.RuntimeDir, "cryptomator-cli.log")

	logFile, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
		0600,
	)
	if err != nil {
		return fmt.Errorf("create Cryptomator log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(
		c.cfg.CLIPath,
		"unlock",
		"--password:stdin",
		"--mounter="+c.cfg.Mounter,
		"--mountPoint="+c.cfg.MountPoint,
		c.cfg.VaultPath,
	)

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	cryptStdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create Cryptomator stdin: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = cryptStdin.Close()
		return fmt.Errorf("start Cryptomator CLI: %w", err)
	}

	// Reuse Cryptomator's existing macOS Keychain entry.
	security := exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", c.cfg.KeychainService,
		"-a", c.cfg.VaultID,
		"-w",
	)

	security.Stdout = cryptStdin
	security.Stderr = logFile

	if err := security.Run(); err != nil {
		_ = cryptStdin.Close()
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
		return fmt.Errorf(
			"read Cryptomator password from macOS Keychain: %w; see %s",
			err,
			logPath,
		)
	}

	if err := cryptStdin.Close(); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
		return fmt.Errorf("close Cryptomator stdin: %w", err)
	}

	pid := cmd.Process.Pid

	go func() {
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		if mounted(c.cfg.MountPoint) {
			pidPath := filepath.Join(c.cfg.RuntimeDir, "cryptomator.pid")

			if err := os.WriteFile(
				pidPath,
				[]byte(strconv.Itoa(pid)+"\n"),
				0600,
			); err != nil {
				_ = syscall.Kill(pid, syscall.SIGTERM)
				return fmt.Errorf("write Cryptomator PID: %w", err)
			}

			return nil
		}

		if !processAlive(pid) {
			return fmt.Errorf(
				"Cryptomator CLI exited before mounting; see %s",
				logPath,
			)
		}

		time.Sleep(250 * time.Millisecond)
	}

	_ = syscall.Kill(pid, syscall.SIGTERM)

	return fmt.Errorf(
		"timed out waiting for Cryptomator mount; see %s",
		logPath,
	)
}

func (c *Client) Lock() error {
	pidPath := filepath.Join(c.cfg.RuntimeDir, "cryptomator.pid")

	b, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"vault is not owned by edrive; lock it from Cryptomator",
			)
		}
		return err
	}

	pidText := strings.TrimSpace(string(b))

	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid Cryptomator PID: %q", pidText)
	}

	if !processMatches(pid, c.cfg.CLIPath) {
		return fmt.Errorf(
			"PID %d is not the expected Cryptomator CLI process",
			pid,
		)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil &&
		err != syscall.ESRCH {
		return fmt.Errorf("stop Cryptomator CLI: %w", err)
	}

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		if !mounted(c.cfg.MountPoint) {
			_ = os.Remove(pidPath)
			return nil
		}

		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for Cryptomator to unmount")
}

func mounted(mountPoint string) bool {
	out, err := exec.Command("mount").Output()
	if err != nil {
		return false
	}

	marker := " on " + filepath.Clean(mountPoint) + " ("

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, marker) {
			return true
		}
	}

	return false
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func processMatches(pid int, expectedCLI string) bool {
	out, err := exec.Command(
		"ps",
		"-p",
		strconv.Itoa(pid),
		"-o",
		"command=",
	).Output()
	if err != nil {
		return false
	}

	command := strings.TrimSpace(string(out))
	return strings.Contains(command, expectedCLI)
}
