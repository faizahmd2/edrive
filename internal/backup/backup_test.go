package backup

import (
	"archive/tar"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCreate(t *testing.T) {
	zstd, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("zstd not installed")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "note.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "._note"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}

	age := filepath.Join(t.TempDir(), "age")
	if err := os.WriteFile(age, []byte("#!/bin/sh\nset -eu\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "backup.age")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := Create(root, []string{"age1testrecipient"}, age, zstd, f); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(zstd, "-d", "-c", out)
	plain, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(plain)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "nested/note.txt" {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "hello" {
				t.Fatalf("content=%q", data)
			}
			found = true
		}
		if h.Name == ".DS_Store" || h.Name == "._note" {
			t.Fatalf("excluded file was archived: %s", h.Name)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected file not found in archive")
	}
}
