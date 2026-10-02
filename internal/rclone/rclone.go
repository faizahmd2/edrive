package rclone

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Client struct {
	Path        string
	RemoteName string
	RemotePath string
}

func New(path, remoteName, remotePath string) (*Client, error) {
	if path == "" {
		path, _ = exec.LookPath("rclone")
	}
	if path == "" {
		return nil, fmt.Errorf("rclone is not installed")
	}
	if remoteName == "" || remotePath == "" {
		return nil, fmt.Errorf("rclone remote is not configured")
	}
	return &Client{Path: path, RemoteName: remoteName, RemotePath: remotePath}, nil
}

func (c *Client) Remote() string {
	return c.RemoteName + ":" + c.RemotePath
}

func (c *Client) EnsureConfigured() error {
	if !c.remoteExists() {
		fmt.Println()
		fmt.Printf("rclone has no remote named %q.\n", c.RemoteName)
		fmt.Printf("edrive will open rclone setup now. Create a remote named %q.\n", c.RemoteName)
		fmt.Println("For Google Drive, choose Google Drive and complete the browser login.")
		fmt.Println()

		if err := c.interactive("config"); err != nil {
			return fmt.Errorf("configure rclone: %w", err)
		}
		if !c.remoteExists() {
			return fmt.Errorf("rclone remote %q was not created", c.RemoteName)
		}
	}

	if err := c.probe(); err == nil {
		return nil
	}

	fmt.Println()
	fmt.Printf("The rclone remote %q is configured but needs authentication.\n", c.RemoteName)
	fmt.Println("edrive will open rclone reconnect now.")
	fmt.Println()

	if err := c.interactive("config", "reconnect", c.RemoteName+":"); err != nil {
		return fmt.Errorf("reconnect rclone remote %q: %w", c.RemoteName, err)
	}
	if err := c.probe(); err != nil {
		return fmt.Errorf("rclone remote %q is still unavailable: %w", c.RemoteName, err)
	}
	return nil
}

func (c *Client) EnsureRemoteDir() error {
	cmd := exec.Command(c.Path, "mkdir", c.Remote())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create remote vault directory: %s", compactOutput(out, err))
	}
	return nil
}

func (c *Client) RemoteVaultState() (bool, bool, error) {
	out, err := c.output("lsf", "--files-only", c.Remote())
	if err != nil {
		return false, false, fmt.Errorf("inspect remote vault: %s", compactOutput(out, err))
	}

	hasVault := false
	hasOther := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if name == "vault.cryptomator" {
			hasVault = true
		} else {
			hasOther = true
		}
	}
	return hasVault, hasOther, nil
}

func (c *Client) SyncLocalToRemote(localVault string) error {
	if err := requireDirectory(localVault); err != nil {
		return err
	}
	fmt.Printf("Publishing encrypted vault to %s\n", c.Remote())
	return c.run("sync", localVault, c.Remote())
}

func (c *Client) SyncRemoteToLocal(localVault string) error {
	if err := os.MkdirAll(localVault, 0700); err != nil {
		return fmt.Errorf("create local vault: %w", err)
	}
	fmt.Printf("Pulling encrypted vault from %s\n", c.Remote())
	return c.run("sync", c.Remote(), localVault)
}

func (c *Client) remoteExists() bool {
	out, err := c.output("listremotes")
	if err != nil {
		return false
	}
	want := c.RemoteName + ":"
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func (c *Client) probe() error {
	return c.runQuiet("lsd", c.RemoteName+":")
}

func (c *Client) interactive(args ...string) error {
	cmd := exec.Command(c.Path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) run(args ...string) error {
	cmd := exec.Command(c.Path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone %s failed", args[0])
	}
	return nil
}

func (c *Client) runQuiet(args ...string) error {
	cmd := exec.Command(c.Path, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func (c *Client) output(args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(c.Path, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return append(out, stderr.Bytes()...), err
	}
	return out, err
}

func requireDirectory(path string) error {
	info, err := os.Stat(filepath.Clean(path))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("local encrypted vault is unavailable at %s", path)
	}
	return nil
}

func compactOutput(out []byte, err error) string {
	message := strings.TrimSpace(string(out))
	if message == "" {
		return err.Error()
	}
	lines := strings.Split(message, "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " ")
}
