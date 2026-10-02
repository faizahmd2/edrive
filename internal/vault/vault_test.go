package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectAndMarkManaged(t *testing.T) {
	root := t.TempDir()
	state, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists || !state.Empty || state.Managed {
		t.Fatalf("unexpected empty state: %+v", state)
	}

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

	state, err = Inspect(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Managed || !state.Complete {
		t.Fatalf("expected complete unmanaged vault: %+v", state)
	}

	if err := MarkManaged(vaultPath); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Managed || !state.Complete {
		t.Fatalf("expected complete managed vault: %+v", state)
	}
}
