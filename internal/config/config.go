package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Home                       string
	Mount                      string
	RecoveryDir                string
	Recipients                 string
	SnapshotKeep               int
	CryptomatorVault           string
	CryptomatorVaultID         string
	CryptomatorCLI             string
	CryptomatorMounter         string
	CryptomatorKeychainService string
	RuntimeDir                 string
}

const defaultConfigRel = "Desktop/local-infra/edrive/config.sh"

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultConfigRel)
}

func Expand(path string) string {
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

// Load accepts intentionally simple shell-style KEY=value lines. It does not
// execute the config file or expand environment variables. Setup writes
// absolute paths so the config remains machine-specific and unambiguous.
func Load(path string) (Config, error) {
	path = Expand(path)
	if path == "" {
		path = DefaultPath()
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config not found: %s: %w", path, err)
	}
	defer f.Close()

	values := map[string]string{}
	allowed := map[string]bool{
		"EDRIVE_HOME":                         true,
		"EDRIVE_MOUNT":                        true,
		"EDRIVE_RECOVERY_DIR":                 true,
		"EDRIVE_RECIPIENTS":                   true,
		"EDRIVE_SNAPSHOT_KEEP":                true,
		"EDRIVE_CRYPTOMATOR_VAULT":            true,
		"EDRIVE_CRYPTOMATOR_VAULT_ID":         true,
		"EDRIVE_CRYPTOMATOR_CLI":              true,
		"EDRIVE_CRYPTOMATOR_MOUNTER":          true,
		"EDRIVE_CRYPTOMATOR_KEYCHAIN_SERVICE": true,
		"EDRIVE_RUNTIME_DIR":                  true,
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			return Config{}, fmt.Errorf("invalid config line (expected KEY=value): %q", line)
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := unquote(strings.TrimSpace(parts[1]))
		if !allowed[key] {
			return Config{}, fmt.Errorf("unsupported config key %q", key)
		}
		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{
		Home:                       Expand(values["EDRIVE_HOME"]),
		Mount:                      Expand(values["EDRIVE_MOUNT"]),
		RecoveryDir:                Expand(values["EDRIVE_RECOVERY_DIR"]),
		Recipients:                 Expand(values["EDRIVE_RECIPIENTS"]),
		CryptomatorVault:           Expand(values["EDRIVE_CRYPTOMATOR_VAULT"]),
		CryptomatorVaultID:         strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_VAULT_ID"]),
		CryptomatorCLI:             Expand(values["EDRIVE_CRYPTOMATOR_CLI"]),
		CryptomatorMounter:         strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_MOUNTER"]),
		CryptomatorKeychainService: strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_KEYCHAIN_SERVICE"]),
		RuntimeDir:                 Expand(values["EDRIVE_RUNTIME_DIR"]),
		SnapshotKeep:               20,
	}

	if cfg.CryptomatorKeychainService == "" {
		cfg.CryptomatorKeychainService = "Cryptomator"
	}
	if cfg.RuntimeDir == "" {
		home, _ := os.UserHomeDir()
		cfg.RuntimeDir = filepath.Join(home, "Library/Application Support/edrive")
	}
	if cfg.Home == "" {
		cfg.Home = filepath.Dir(path)
	}

	if cfg.CryptomatorVault == "" ||
		cfg.CryptomatorVaultID == "" ||
		cfg.CryptomatorCLI == "" ||
		cfg.CryptomatorMounter == "" {
		return Config{}, fmt.Errorf(
			"config must define EDRIVE_CRYPTOMATOR_VAULT, EDRIVE_CRYPTOMATOR_VAULT_ID, EDRIVE_CRYPTOMATOR_CLI and EDRIVE_CRYPTOMATOR_MOUNTER",
		)
	}
	if cfg.Mount == "" || cfg.RecoveryDir == "" || cfg.Recipients == "" {
		return Config{}, fmt.Errorf("config must define EDRIVE_MOUNT, EDRIVE_RECOVERY_DIR and EDRIVE_RECIPIENTS")
	}

	if v := strings.TrimSpace(values["EDRIVE_SNAPSHOT_KEEP"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("invalid EDRIVE_SNAPSHOT_KEEP=%q", v)
		}
		cfg.SnapshotKeep = n
	}

	return cfg, nil
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			if v[0] == '"' {
				if s, err := strconv.Unquote(v); err == nil {
					return s
				}
			}
			return v[1 : len(v)-1]
		}
	}
	return v
}
