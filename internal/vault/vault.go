package vault

import (
	"os"
	"path/filepath"
)

type State struct {
	Exists              bool
	Empty               bool
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
		HasCryptomatorFiles: masterkey || vaultFile,
		Complete:            masterkey && vaultFile,
	}, nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
