package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/faiz/edrive/internal/config"
)

const bindingVersion = 1

type Binding struct {
	Version   int       \`json:"version"\`
	Path      string    \`json:"path"\`
	CreatedAt time.Time \`json:"created_at"\`
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
		return State{}, err
	}
	if !info.IsDir() {
		return State{}, os.ErrInvalid
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return State{}, err
	}
	masterkey := isFile(filepath.Join(path, "masterkey.cryptomator"))
	vaultFile := isFile(filepath.Join(path, "vault.cryptomator"))
	return State{
		Exists:              true,
		Empty:               len(entries) == 0,
		Managed:             IsManaged(path),
		HasCryptomatorFiles: masterkey || vaultFile,
		Complete:            masterkey && vaultFile,
	}, nil
}

func IsManaged(path string) bool {
	binding, ok, err := ReadBinding()
	if err != nil || !ok {
		return false
	}
	return binding.Path == canonicalPath(path)
}

func ReadBinding() (Binding, bool, error) {
	f, err := os.Open(filepath.Join(config.Home(), "vault.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Binding{}, false, nil
		}
		return Binding{}, false, err
	}
	defer f.Close()

	var binding Binding
	if err := json.NewDecoder(f).Decode(&binding); err != nil {
		return Binding{}, false, err
	}
	if binding.Version != bindingVersion || binding.Path == "" {
		return Binding{}, false, os.ErrInvalid
	}
	binding.Path = canonicalPath(binding.Path)
	return binding, true, nil
}

func MarkManaged(path string) error {
	path = canonicalPath(path)
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(config.Home(), 0700); err != nil {
		return err
	}

	binding := Binding{
		Version:   bindingVersion,
		Path:      path,
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.CreateTemp(config.Home(), ".vault-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(config.Home(), "vault.json"))
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
