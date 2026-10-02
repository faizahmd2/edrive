package cryptomator

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	KeychainService = "Cryptomator"
	Mounter         = "org.cryptomator.frontend.fuse.mount.FuseTMountProvider"
)

type Config struct {
	VaultPath  string
	VaultID    string
	MountPoint string
	CLIPath    string
	RuntimeDir string
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
	if c.cfg.VaultPath == "" || c.cfg.MountPoint == "" || c.cfg.CLIPath == "" || c.cfg.RuntimeDir == "" {
		return fmt.Errorf("Cryptomator is not configured")
	}

	info, err := os.Stat(c.cfg.VaultPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("encrypted workspace is unavailable")
	}
	if !isExecutableFile(c.cfg.CLIPath) {
		return fmt.Errorf("Cryptomator CLI is unavailable")
	}

	vaultID := c.cfg.VaultID
	if vaultID == "" {
		vaultID, err = DiscoverVaultID(c.cfg.VaultPath)
		if err != nil {
			return fmt.Errorf("the Cryptomator vault is not registered on this Mac")
		}
	}
	credential, err := cryptomatorCredential(vaultID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(c.cfg.RuntimeDir, 0700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}

	cmd := exec.Command(
		c.cfg.CLIPath,
		"unlock",
		"--password:stdin",
		"--mounter="+Mounter,
		"--mountPoint="+c.cfg.MountPoint,
		c.cfg.VaultPath,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cryptStdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create Cryptomator stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = cryptStdin.Close()
		return fmt.Errorf("start Cryptomator CLI")
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	if _, err := io.WriteString(cryptStdin, credential+"\n"); err != nil {
		credential = ""
		_ = cryptStdin.Close()
		terminate(cmd.Process.Pid)
		<-waitDone
		return fmt.Errorf("send the Cryptomator password to its CLI: %w", err)
	}
	credential = ""

	if err := cryptStdin.Close(); err != nil {
		terminate(cmd.Process.Pid)
		<-waitDone
		return fmt.Errorf("close Cryptomator stdin")
	}

	pidPath := filepath.Join(c.cfg.RuntimeDir, "cryptomator.pid")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if mounted(c.cfg.MountPoint) {
			if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0600); err != nil {
				terminate(cmd.Process.Pid)
				<-waitDone
				return fmt.Errorf("write Cryptomator state: %w", err)
			}
			return nil
		}

		select {
		case <-waitDone:
			return fmt.Errorf("Cryptomator CLI exited before mounting; check that the vault is available locally and its password is stored in Keychain")
		default:
		}
		time.Sleep(250 * time.Millisecond)
	}

	terminate(cmd.Process.Pid)
	<-waitDone
	return fmt.Errorf("timed out waiting for Cryptomator mount")
}

func (c *Client) Lock() error {
	pidPath := filepath.Join(c.cfg.RuntimeDir, "cryptomator.pid")
	b, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("vault is not owned by edrive")
		}
		return err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid Cryptomator process state")
	}
	if !processMatches(pid, c.cfg.CLIPath) {
		return fmt.Errorf("edrive process state is stale")
	}

	terminate(pid)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mountedNow := mounted(c.cfg.MountPoint)
		aliveNow := processExists(pid)
		if !mountedNow && !aliveNow {
			_ = os.Remove(pidPath)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for Cryptomator to close")
}

func cryptomatorCredential(vaultID string) (string, error) {
	out, err := exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", KeychainService,
		"-a", vaultID,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("Cryptomator's password is not available from macOS Keychain for this vault; unlock it once in Cryptomator so its password is stored, then retry")
	}
	credential := strings.TrimSpace(string(out))
	if credential == "" {
		return "", fmt.Errorf("Cryptomator's Keychain password for this vault is empty; open Cryptomator and set the vault password again")
	}
	return credential, nil
}

func DiscoverVaultID(vaultPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	settingsPath := filepath.Join(home, "Library", "Application Support", "Cryptomator", "settings.json")
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", err
	}

	var settings struct {
		Directories []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"directories"`
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		return "", err
	}

	target := canonicalPath(vaultPath)
	for _, directory := range settings.Directories {
		if directory.ID == "" || directory.Path == "" {
			continue
		}
		path := directory.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(home, path)
		}
		if canonicalPath(path) == target {
			return directory.ID, nil
		}
	}

	return "", fmt.Errorf("vault is not registered in Cryptomator")
}

func mounted(mountPoint string) bool {
	if mountPoint == "" {
		return false
	}
	out, err := exec.Command("mount").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), " on "+filepath.Clean(mountPoint)+" (")
}

func processMatches(pid int, expectedCLI string) bool {
	if expectedCLI == "" {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(string(out)), expectedCLI)
}

func processExists(pid int) bool {
	return exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=").Run() == nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

func terminate(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
}
