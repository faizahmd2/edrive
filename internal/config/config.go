package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const CurrentVersion = 2

type Config struct {
	ConfigPath      string `json:"-"`
	ConfigFound     bool   `json:"-"`
	Version         int    `json:"version"`
	DataRoot        string `json:"data_root"`
	StorageProvider string `json:"storage_provider"`
	StorageName     string `json:"storage_name"`
	StorageRoot     string `json:"storage_root,omitempty"`
	LastBackupDir   string `json:"last_backup_dir,omitempty"`
	AgePath         string `json:"age_path,omitempty"`
	ZstdPath        string `json:"zstd_path,omitempty"`
	CryptomatorCLI  string `json:"cryptomator_cli,omitempty"`
}

func Home() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".edrive")
}

func DefaultPath() string {
	return filepath.Join(Home(), "config.json")
}

func DefaultDataRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "edrive")
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
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults(path)

	f, err := os.Open(cfg.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.ConfigPath = Defaults(cfg.ConfigPath).ConfigPath
	cfg.ConfigFound = true

	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	if cfg.Version > CurrentVersion {
		return Config{}, fmt.Errorf("unsupported edrive config version: %d", cfg.Version)
	}
	if cfg.Version < CurrentVersion {
		cfg.Version = CurrentVersion
	}
	cfg.DataRoot = ExpandOrDefault(cfg.DataRoot, DefaultDataRoot())
	cfg.StorageProvider = ExpandOrDefault(cfg.StorageProvider, "google-drive")
	cfg.StorageName = ExpandOrDefault(cfg.StorageName, "edrive")
	cfg.StorageRoot = Expand(cfg.StorageRoot)
	cfg.LastBackupDir = Expand(cfg.LastBackupDir)
	cfg.AgePath = Expand(cfg.AgePath)
	cfg.ZstdPath = Expand(cfg.ZstdPath)
	cfg.CryptomatorCLI = Expand(cfg.CryptomatorCLI)

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

func ExpandOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return Expand(value)
}
