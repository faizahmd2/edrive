package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	RcloneRemote = "edrive-cloud"
	RemoteVault  = "edrive"

	// KeychainService and VaultPasswordAccount name the Keychain item that
	// holds the Cryptomator vault password. Only the edrive binary can read it.
	KeychainService      = "edrive"
	VaultPasswordAccount = "vault-password"

	// OpenDuration is how long 'edrive open' keeps the workspace unlocked.
	OpenDuration = 30 * time.Minute
)

type Config struct {
	ConfigPath   string `json:"-"`
	ConfigFound  bool   `json:"-"`
	RcloneRemote string `json:"rclone_remote"`
	RclonePath   string `json:"rclone_path"`
}

func Home() string {
	if dir := strings.TrimSpace(os.Getenv("EDRIVE_HOME")); dir != "" {
		return filepath.Clean(dir)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".edrive")
}

func DefaultPath() string       { return filepath.Join(Home(), "config.json") }
func WorkspacePath() string     { return filepath.Join(Home(), "workspace") }
func LocalVaultPath() string    { return filepath.Join(Home(), "vault") }
func BackupDir() string         { return filepath.Join(Home(), "backups") }
func RuntimeDir() string        { return filepath.Join(Home(), "runtime") }
func ToolsDir() string          { return filepath.Join(Home(), "tools") }
func StateDir() string          { return filepath.Join(Home(), "state") }
func LocalTrashDir() string     { return filepath.Join(Home(), "trash") }
func LegacyDevicesPath() string { return filepath.Join(Home(), "devices.json") }
func LegacyRecoveryKeyPath() string {
	return filepath.Join(BackupDir(), "edrive-recovery-key.txt")
}

func Defaults(path string) Config {
	if path == "" {
		path = DefaultPath()
	}
	return Config{
		ConfigPath:   filepath.Clean(path),
		RcloneRemote: RcloneRemote,
		RclonePath:   RemoteVault,
	}
}

// Load reads the config. Unknown and legacy fields are ignored, so older
// installs keep working without a forced rebuild.
func Load(path string) (Config, error) {
	cfg := Defaults(path)
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	cfg.ConfigFound = true

	var stored Config
	if err := json.Unmarshal(data, &stored); err != nil {
		return cfg, fmt.Errorf("config %s is not valid JSON; run 'edrive setup' to rewrite it", cfg.ConfigPath)
	}
	if v := strings.TrimSpace(stored.RcloneRemote); v != "" {
		cfg.RcloneRemote = v
	}
	if v := strings.TrimSpace(stored.RclonePath); v != "" {
		cfg.RclonePath = v
	}
	return cfg, nil
}

func (c Config) Save() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(c.ConfigPath, append(data, '\n'), 0600)
}

// Remote is the rclone path of the encrypted vault, e.g. "edrive-cloud:edrive".
func (c Config) Remote() string {
	return c.RcloneRemote + ":" + c.RclonePath
}

func Expand(path string) string {
	path = strings.TrimSpace(path)
	home, _ := os.UserHomeDir()
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// WriteFileAtomic writes data to a temp file in the same directory and
// renames it into place, so readers never see a half-written file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
