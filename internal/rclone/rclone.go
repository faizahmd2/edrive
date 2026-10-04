package rclone

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type Client struct {
	Path       string
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

func (c *Client) RemoteExists() bool {
	return c.remoteExists()
}

func (c *Client) CreateRemote(remoteType string, options map[string]string) error {
	if remoteType == "" {
		return fmt.Errorf("rclone remote type is required")
	}
	args := []string{"config", "create", c.RemoteName, remoteType}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, key, options[key])
	}
	args = append(args, "--non-interactive")

	cmd := exec.Command(c.Path, args...)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create rclone remote %q: %s", c.RemoteName, compactOutput(stderr.Bytes(), err))
	}
	return nil
}

func (c *Client) Authorize(backend string, credentials ...string) (string, error) {
	if strings.TrimSpace(backend) == "" {
		return "", fmt.Errorf("rclone authorization backend is required")
	}
	args := []string{"authorize", backend}
	args = append(args, credentials...)

	var stdout bytes.Buffer
	cmd := exec.Command(c.Path, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("authorize rclone %q: %w", backend, err)
	}

	token, err := extractAuthorizeToken(stdout.String())
	if err != nil {
		return "", err
	}
	return token, nil
}

func extractAuthorizeToken(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("rclone authorization completed without returning a token")
	}

	if token := extractTokenJSON(output); token != "" {
		return token, nil
	}

	if start := strings.Index(output, "--->"); start >= 0 {
		if end := strings.Index(output[start+4:], "<---"); end >= 0 {
			candidate := strings.Join(strings.Fields(output[start+4:start+4+end]), "")
			if token := decodeAuthorizeBlob(candidate); token != "" {
				return token, nil
			}
		}
	}

	if token := decodeAuthorizeBlob(strings.Join(strings.Fields(output), "")); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("rclone authorization completed but did not return a usable OAuth token")
}

func extractTokenJSON(output string) string {
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return ""
	}

	candidate := strings.TrimSpace(output[start : end+1])
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &fields); err != nil {
		return ""
	}
	if _, ok := fields["access_token"]; ok {
		return candidate
	}
	if _, ok := fields["refresh_token"]; ok {
		return candidate
	}
	if raw, ok := fields["token"]; ok {
		var nested string
		if json.Unmarshal(raw, &nested) == nil {
			if token := extractTokenJSON(nested); token != "" {
				return token
			}
		}
	}
	return ""
}

func decodeAuthorizeBlob(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err != nil {
			continue
		}
		if token := extractTokenJSON(string(decoded)); token != "" {
			return token
		}
	}
	return ""
}

func (c *Client) DeleteRemote() error {
	cmd := exec.Command(c.Path, "config", "delete", c.RemoteName)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("delete rclone remote %q: %s", c.RemoteName, compactOutput(stderr.Bytes(), err))
	}
	return nil
}

func (c *Client) Reconnect() error {
	return c.interactive("config", "reconnect", c.RemoteName+":")
}

func (c *Client) ConfigFile() (string, error) {
	out, err := c.output("config", "file")
	if err != nil {
		return "", fmt.Errorf("find rclone configuration file: %s", compactOutput(out, err))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" && !strings.HasPrefix(line, "Configuration file") {
			return line, nil
		}
	}
	return "", fmt.Errorf("rclone did not report its configuration file")
}

func (c *Client) RemoteIdentity() (string, string, error) {
	out, err := c.output("config", "redacted", c.RemoteName)
	if err != nil {
		return "", "", fmt.Errorf("inspect rclone remote: %s", compactOutput(out, err))
	}
	var remoteType, backend string
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := splitConfigLine(line)
		if !ok {
			continue
		}
		switch key {
		case "type":
			remoteType = value
		case "provider":
			backend = value
		}
	}
	if remoteType == "" {
		return "", "", fmt.Errorf("rclone remote %q has no type", c.RemoteName)
	}
	return remoteType, backend, nil
}

func splitConfigLine(line string) (string, string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func (c *Client) CheckConfigured() error {
	if !c.remoteExists() {
		return fmt.Errorf("rclone remote %q is not configured; run 'edrive setup'", c.RemoteName)
	}
	if err := c.probe(); err != nil {
		return fmt.Errorf("rclone remote %q is unavailable; run 'edrive setup' to authenticate it", c.RemoteName)
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

// netFlags keep rclone from hanging for minutes when the network is down.
var netFlags = []string{"--contimeout", "15s", "--timeout", "60s", "--retries", "2", "--low-level-retries", "3"}

func (c *Client) probe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Path, append([]string{"lsd", "--max-depth", "1", c.RemoteName + ":"}, netFlags...)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// Item is one entry from 'rclone lsjson'.
type Item struct {
	Path    string    `json:"Path"`
	Size    int64     `json:"Size"`
	ModTime time.Time `json:"ModTime"`
	IsDir   bool      `json:"IsDir"`
}

// List returns every file and directory under path (recursive). A missing
// directory is reported as an empty listing.
func (c *Client) List(path string, recursive bool) ([]Item, error) {
	args := []string{"lsjson", "--no-mimetype", "--fast-list"}
	if recursive {
		args = append(args, "-R")
	}
	args = append(args, path)
	out, err := c.output(append(args, netFlags...)...)
	if err != nil {
		if strings.Contains(string(out), "directory not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %s", path, compactOutput(out, err))
	}
	var items []Item
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("list %s: unexpected rclone output", path)
	}
	return items, nil
}

// Copy copies exactly the listed relative paths from src to dst. Files that
// would be overwritten in dst are moved into backupDir instead of being lost.
// progress, if set, is called with the number of files done so far.
func (c *Client) Copy(src, dst string, files []string, backupDir string, progress func(int)) error {
	if len(files) == 0 {
		return nil
	}
	list, cleanup, err := writeList(files)
	if err != nil {
		return err
	}
	defer cleanup()
	args := append([]string{"copy", src, dst, "--files-from-raw", list}, batchFlags(len(files))...)
	if backupDir != "" {
		args = append(args, "--backup-dir", backupDir)
	}
	return c.runCounting(progress, append(args, netFlags...)...)
}

// Move moves exactly the listed relative paths from src to dst.
func (c *Client) Move(src, dst string, files []string, progress func(int)) error {
	if len(files) == 0 {
		return nil
	}
	list, cleanup, err := writeList(files)
	if err != nil {
		return err
	}
	defer cleanup()
	args := append([]string{"move", src, dst, "--files-from-raw", list}, batchFlags(len(files))...)
	return c.runCounting(progress, append(args, netFlags...)...)
}

// batchFlags: for a few files, look each one up directly; for many, one
// listing of the destination is far quicker than hundreds of lookups.
// Several parallel transfers help with clouds that are slow per file.
func batchFlags(n int) []string {
	flags := []string{"--transfers", "8", "--checkers", "16"}
	if n <= 50 {
		return append(flags, "--no-traverse")
	}
	return append(flags, "--fast-list")
}

// runCounting runs rclone with per-file logging and reports each finished
// file, so long transfers show live progress instead of looking frozen.
func (c *Client) runCounting(progress func(int), args ...string) error {
	cmd := exec.Command(c.Path, append(args, "-v", "--stats", "0")...)
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := 0
	var tail []string
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, ": Copied (") || strings.Contains(line, ": Moved (") {
			done++
			if progress != nil {
				progress(done)
			}
			continue
		}
		if strings.Contains(line, "ERROR") || strings.Contains(line, "NOTICE") || strings.Contains(line, "Failed") {
			tail = append(tail, line)
			if len(tail) > 3 {
				tail = tail[1:]
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rclone %s: %s", args[0], compactOutput([]byte(strings.Join(tail, "\n")), err))
	}
	return nil
}

func (c *Client) Mkdir(path string) error {
	return c.run(append([]string{"mkdir", path}, netFlags...)...)
}

func writeList(files []string) (string, func(), error) {
	f, err := os.CreateTemp("", "edrive-rclone-list-*")
	if err != nil {
		return "", nil, err
	}
	_, werr := f.WriteString(strings.Join(files, "\n") + "\n")
	cerr := f.Close()
	cleanup := func() { _ = os.Remove(f.Name()) }
	if werr != nil || cerr != nil {
		cleanup()
		return "", nil, fmt.Errorf("write rclone file list")
	}
	return f.Name(), cleanup, nil
}

func (c *Client) interactive(args ...string) error {
	cmd := exec.Command(c.Path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) run(args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(c.Path, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rclone %s: %s", args[0], compactOutput(stderr.Bytes(), err))
	}
	return nil
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
