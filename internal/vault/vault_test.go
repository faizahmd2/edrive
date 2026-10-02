package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectCompleteUnmanagedVault(t *testing.T) {
	root := t.TempDir()
	vaultPath := filepath.Join(root, "edrive")
	if err := os.MkdirAll(vaultPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultPath, "vault.cryptomator"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultPath, "masterkey.cryptomator"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	state, err := Inspect(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists || !state.HasCryptomatorFiles || !state.Complete {
		t.Fatalf("expected complete vault: %+v", state)
	}
	if state.Managed {
		t.Fatalf("test vault must not be managed: %+v", state)
	}
}
