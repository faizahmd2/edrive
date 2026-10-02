package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	MarkerFile    = ".edrive-vault.json"
	MarkerVersion = 1
)

type Marker struct {
	Product   string    `json:"product"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

type State struct {
	Exists              bool
	Empty               bool
	Managed             bool
	HasCryptomatorFiles bool
	Complete            bool
}

func Inspect(path string) (State, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{Empty: true}, nil
		}
		return State{}, fmt.Errorf("inspect vault location: %w", err)
	}
	if !info.IsDir() {
		return State{}, fmt.Errorf("vault location is not a directory: %s", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return State{}, fmt.Errorf("read vault location: %w", err)
	}

	managed, err := IsManaged(path)
	if err != nil {
		return State{}, err
	}

	hasMasterkey := isFile(filepath.Join(path, "masterkey.cryptomator"))
	hasVault := isFile(filepath.Join(path, "vault.cryptomator"))
	return State{
		Exists:              true,
		Empty:               len(entries) == 0,
		Managed:             managed,
		HasCryptomatorFiles: hasMasterkey || hasVault,
		Complete:            hasMasterkey && hasVault,
	}, nil
}

func IsManaged(path string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(path, MarkerFile))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read edrive vault marker: %w", err)
	}
	var marker Marker
	if err := json.Unmarshal(b, &marker); err != nil {
		return false, fmt.Errorf("edrive vault marker is invalid: %w", err)
	}
	if marker.Product != "edrive" || marker.Version != MarkerVersion {
		return false, fmt.Errorf("edrive vault marker belongs to an unsupported format")
	}
	return true, nil
}

func MarkManaged(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create edrive vault location: %w", err)
	}
	managed, err := IsManaged(path)
	if err != nil {
		return err
	}
	if managed {
		return nil
	}

	data, err := json.MarshalIndent(Marker{
		Product:   "edrive",
		Version:   MarkerVersion,
		CreatedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(path, ".edrive-vault-*.tmp")
	if err != nil {
		return fmt.Errorf("create vault marker: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0600); err != nil {
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
	if err := os.Rename(tmpPath, filepath.Join(path, MarkerFile)); err != nil {
		return fmt.Errorf("save edrive vault marker: %w", err)
	}
	return nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
