package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const CurrentVersion = 1

type Config struct {
	ConfigPath      string `json:"-"`
	ConfigFound     bool   `json:"-"`
	Version         int    `json:"version"`
	DataRoot        string `json:"data_root"`
	StorageProvider string `json:"storage_provider"`
	StorageName     string `json:"storage_name"`
	LastBackupDir   string `json:"last_backup_dir,omitempty"`
	Recipients      string `json:"recipients"`
	CryptomatorCLI  string `json:"cryptomator_cli,omitempty"`
}

func DefaultHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".edrive")
}

func DefaultPath() string {
	return filepath.Join(DefaultHome(), "config.json")
}

func DefaultDataRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "edrive")
}

func DefaultRecipients() string {
	return filepath.Join(DefaultHome(), "recipients.txt")
}

func DefaultRuntimeDir() string {
	return filepath.Join(DefaultHome(), "runtime")
}

func DefaultToolsDir() string {
	return filepath.Join(DefaultHome(), "tools")
}

func DefaultTempDir() string {
	return filepath.Join(DefaultHome(), "tmp")
}

func Expand(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
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

func Defaults(path string) Config {
	if path == "" {
		path = DefaultPath()
	}
	return Config{
		ConfigPath:      Expand(path),
		Version:         CurrentVersion,
		DataRoot:        DefaultDataRoot(),
		StorageProvider: "google-drive",
		StorageName:     "edrive",
		Recipients:      DefaultRecipients(),
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults(path)

	f, err := os.Open(cfg.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("open config %s: %w", cfg.ConfigPath, err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", cfg.ConfigPath, err)
	}
	cfg.ConfigPath = Defaults(cfg.ConfigPath).ConfigPath
	cfg.ConfigFound = true

	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	if cfg.Version != CurrentVersion {
		return Config{}, fmt.Errorf("unsupported edrive config version: %d", cfg.Version)
	}
	if cfg.DataRoot == "" {
		cfg.DataRoot = DefaultDataRoot()
	}
	cfg.DataRoot = Expand(cfg.DataRoot)
	if cfg.StorageProvider == "" {
		cfg.StorageProvider = "google-drive"
	}
	if cfg.StorageName == "" {
		cfg.StorageName = "edrive"
	}
	if cfg.Recipients == "" {
		cfg.Recipients = DefaultRecipients()
	} else {
		cfg.Recipients = Expand(cfg.Recipients)
	}
	if cfg.LastBackupDir != "" {
		cfg.LastBackupDir = Expand(cfg.LastBackupDir)
	}
	if cfg.CryptomatorCLI != "" {
		cfg.CryptomatorCLI = Expand(cfg.CryptomatorCLI)
	}
	return cfg, nil
}

func (c Config) Save() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0700); err != nil {
		return fmt.Errorf("create edrive home: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(c.ConfigPath), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	_ = tmp.Chmod(0600)
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
		return fmt.Errorf("finalize config: %w", err)
	}
	return os.Chmod(c.ConfigPath, 0600)
}
