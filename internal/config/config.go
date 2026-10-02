package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const CurrentVersion = 3

const (
	RcloneRemote = "edrive-cloud"
	RemoteVault  = "edrive"
)

type Config struct {
	ConfigPath     string `json:"-"`
	ConfigFound    bool   `json:"-"`
	Version        int    `json:"version"`
	RcloneRemote   string `json:"rclone_remote"`
	RclonePath     string `json:"rclone_path"`
	AgePath        string `json:"age_path,omitempty"`
	ZstdPath       string `json:"zstd_path,omitempty"`
	CryptomatorCLI string `json:"cryptomator_cli,omitempty"`
}

func Home() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".edrive")
}

func DefaultPath() string {
	return filepath.Join(Home(), "config.json")
}

func WorkspacePath() string {
	return filepath.Join(Home(), "workspace")
}

func LocalVaultPath() string {
	return filepath.Join(Home(), "vault")
}

func BackupDir() string {
	return filepath.Join(Home(), "backups")
}

func DevicesPath() string {
	return filepath.Join(Home(), "devices.json")
}

func RuntimeDir() string {
	return filepath.Join(Home(), "runtime")
}

func ToolsDir() string {
	return filepath.Join(Home(), "tools")
}

func TempDir() string {
	return filepath.Join(Home(), "tmp")
}

func Defaults(path string) Config {
	if path == "" {
		path = DefaultPath()
	}
	return Config{
		ConfigPath:   filepath.Clean(path),
		Version:      CurrentVersion,
		RcloneRemote: RcloneRemote,
		RclonePath:   RemoteVault,
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults(path)

	f, err := os.Open(cfg.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		cfg.ConfigFound = true
		return cfg, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	cfg.ConfigFound = true
	var stored Config
	if err := json.NewDecoder(f).Decode(&stored); err != nil {
		return cfg, fmt.Errorf("decode config: %w", err)
	}

	cfg.Version = stored.Version
	if cfg.Version != CurrentVersion {
		return cfg, fmt.Errorf("edrive config version %d must be rebuilt with 'edrive setup'", cfg.Version)
	}
	cfg.RcloneRemote = stored.RcloneRemote
	cfg.RclonePath = stored.RclonePath
	cfg.AgePath = Expand(stored.AgePath)
	cfg.ZstdPath = Expand(stored.ZstdPath)
	cfg.CryptomatorCLI = Expand(stored.CryptomatorCLI)

	if strings.TrimSpace(cfg.RcloneRemote) == "" {
		cfg.RcloneRemote = RcloneRemote
	}
	if strings.TrimSpace(cfg.RclonePath) == "" {
		cfg.RclonePath = RemoteVault
	}
	cfg.ConfigPath = Defaults(cfg.ConfigPath).ConfigPath
	return cfg, nil
}

func (c Config) Save() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0700); err != nil {
		return fmt.Errorf("create edrive state: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(c.ConfigPath), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create config temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tmpPath, c.ConfigPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return os.Chmod(c.ConfigPath, 0600)
}

func Expand(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
