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
	ConfigPath                 string
	ConfigFound                 bool
	DataRoot                    string
	GoogleDriveRoot             string
	Mount                       string
	RecoveryDir                 string
	Recipients                  string
	MacIdentity                 string
	RecoveryIdentity            string
	SnapshotKeep                int
	CryptomatorVault            string
	CryptomatorVaultID          string
	CryptomatorCLI              string
	CryptomatorMounter          string
	CryptomatorKeychainService  string
	RuntimeDir                  string
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Application Support/edrive/config.sh")
}

func DefaultDataRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "edrive")
}

func DefaultRuntimeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Application Support/edrive/runtime")
}

func DefaultIdentityDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/Application Support/edrive/identities")
}

func DefaultRecoveryIdentity() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "edrive-recovery-identity.txt")
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

// Load reads simple KEY=value configuration. It does not execute shell code
// and does not expand environment variables. Missing configuration is allowed
// so that "edrive doctor" can diagnose a fresh machine.
func Load(path string) (Config, error) {
	path = Expand(path)
	if path == "" {
		path = DefaultPath()
	}

	cfg := Config{
		ConfigPath:                path,
		DataRoot:                  DefaultDataRoot(),
		RecoveryDir:               filepath.Join(DefaultDataRoot(), "recovery"),
		GoogleDriveRoot:           filepath.Join(DefaultDataRoot(), "google-drive-remote"),
		Mount:                     filepath.Join(DefaultDataRoot(), "edrive"),
		Recipients:                filepath.Join(DefaultIdentityDir(), "recipients.txt"),
		MacIdentity:               filepath.Join(DefaultIdentityDir(), "mac.identity"),
		RecoveryIdentity:          DefaultRecoveryIdentity(),
		SnapshotKeep:              20,
		CryptomatorKeychainService: "Cryptomator",
		RuntimeDir:                DefaultRuntimeDir(),
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.ConfigFound = false
			cfg.CryptomatorVault = filepath.Join(cfg.GoogleDriveRoot, "edrive")
			return cfg, nil
		}
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	cfg.ConfigFound = true

	values := map[string]string{}
	allowed := map[string]bool{
		"EDRIVE_DATA_ROOT":                  true,
		"EDRIVE_HOME":                       true,
		"EDRIVE_GOOGLE_DRIVE_ROOT":          true,
		"EDRIVE_MOUNT":                      true,
		"EDRIVE_RECOVERY_DIR":               true,
		"EDRIVE_RECIPIENTS":                 true,
		"EDRIVE_MAC_IDENTITY":               true,
		"EDRIVE_RECOVERY_IDENTITY":          true,
		"EDRIVE_SNAPSHOT_KEEP":              true,
		"EDRIVE_CRYPTOMATOR_VAULT":          true,
		"EDRIVE_CRYPTOMATOR_VAULT_ID":       true,
		"EDRIVE_CRYPTOMATOR_CLI":            true,
		"EDRIVE_CRYPTOMATOR_MOUNTER":        true,
		"EDRIVE_CRYPTOMATOR_KEYCHAIN_SERVICE": true,
		"EDRIVE_RUNTIME_DIR":                true,
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

	if values["EDRIVE_DATA_ROOT"] == "" {
		values["EDRIVE_DATA_ROOT"] = values["EDRIVE_HOME"]
	}

	if v := strings.TrimSpace(values["EDRIVE_SNAPSHOT_KEEP"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("invalid EDRIVE_SNAPSHOT_KEEP=%q", v)
		}
		cfg.SnapshotKeep = n
	}

	if v := strings.TrimSpace(values["EDRIVE_DATA_ROOT"]); v != "" {
		cfg.DataRoot = Expand(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_GOOGLE_DRIVE_ROOT"]); v != "" {
		cfg.GoogleDriveRoot = Expand(v)
	} else {
		cfg.GoogleDriveRoot = filepath.Join(cfg.DataRoot, "google-drive-remote")
	}
	if v := strings.TrimSpace(values["EDRIVE_MOUNT"]); v != "" {
		cfg.Mount = Expand(v)
	} else {
		cfg.Mount = filepath.Join(cfg.DataRoot, "edrive")
	}
	if v := strings.TrimSpace(values["EDRIVE_RECOVERY_DIR"]); v != "" {
		cfg.RecoveryDir = Expand(v)
	} else {
		cfg.RecoveryDir = filepath.Join(cfg.DataRoot, "recovery")
	}
	if v := strings.TrimSpace(values["EDRIVE_RECIPIENTS"]); v != "" {
		cfg.Recipients = Expand(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_MAC_IDENTITY"]); v != "" {
		cfg.MacIdentity = Expand(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_RECOVERY_IDENTITY"]); v != "" {
		cfg.RecoveryIdentity = Expand(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_VAULT"]); v != "" {
		cfg.CryptomatorVault = Expand(v)
	} else {
		cfg.CryptomatorVault = filepath.Join(cfg.GoogleDriveRoot, "edrive")
	}
	if v := strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_VAULT_ID"]); v != "" {
		cfg.CryptomatorVaultID = strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_CLI"]); v != "" {
		cfg.CryptomatorCLI = Expand(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_MOUNTER"]); v != "" {
		cfg.CryptomatorMounter = strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_CRYPTOMATOR_KEYCHAIN_SERVICE"]); v != "" {
		cfg.CryptomatorKeychainService = strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(values["EDRIVE_RUNTIME_DIR"]); v != "" {
		cfg.RuntimeDir = Expand(v)
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
