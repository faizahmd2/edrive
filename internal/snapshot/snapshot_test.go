package snapshot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBuildManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "active", "faiz", "github"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "active", "faiz", "github", "key.txt"), []byte("secret-ish test"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 {
		t.Fatalf("files=%d, want 1", len(m.Files))
	}
	if m.Files[0].Path != "active/faiz/github/key.txt" {
		t.Fatalf("path=%q", m.Files[0].Path)
	}
	if m.Files[0].SHA256 == "" {
		t.Fatal("sha256 empty")
	}
}

func TestSafeTarPath(t *testing.T) {
	good := []string{"active/faiz/github/key.txt", "archive/2026/10/"}
	for _, p := range good {
		if _, err := safeTarPath(p); err != nil {
			t.Fatalf("good path %q rejected: %v", p, err)
		}
	}
	bad := []string{"/etc/passwd", "../escape", "../../escape", "./../escape", ""}
	if runtime.GOOS == "windows" {
		bad = append(bad, `C:\\escape`)
	}
	for _, p := range bad {
		if _, err := safeTarPath(p); err == nil {
			t.Fatalf("bad path %q accepted", p)
		}
	}
}

func TestSnapshotPipelineWithFakeAge(t *testing.T) {
	if _, err := os.Stat("/usr/bin/zstd"); err != nil {
		t.Skip("zstd not installed in test environment")
	}
	binDir := t.TempDir()
	fakeAge := filepath.Join(binDir, "age")
	script := `#!/bin/sh
set -eu
if [ "$1" = "--decrypt" ]; then
  cat
  exit 0
fi
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--output" ]; then out="$arg"; fi
  prev="$arg"
done
if [ -n "$out" ]; then
  cat > "$out"
else
  cat
fi
`
	if err := os.WriteFile(fakeAge, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "active", "faiz"), 0700); err != nil {
		t.Fatal(err)
	}
	want := []byte("hello encrypted world\n")
	if err := os.WriteFile(filepath.Join(root, "active", "faiz", "note.txt"), want, 0600); err != nil {
		t.Fatal(err)
	}
	recipients := filepath.Join(t.TempDir(), "recipients.txt")
	if err := os.WriteFile(recipients, []byte("age1pq1test-recipient\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "snapshot.tar.zst.age")
	manifest, err := BuildManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	fout, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateEncryptedSnapshot(root, recipients, manifest, fout); err != nil {
		_ = fout.Close()
		t.Fatal(err)
	}
	if err := fout.Close(); err != nil {
		t.Fatal(err)
	}

	identity := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(identity, []byte("fake identity\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadAndVerifyArchive(f, identity, "", false)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "active/faiz/note.txt" {
		t.Fatalf("unexpected manifest: %+v", got)
	}

	restore := filepath.Join(t.TempDir(), "restore")
	f, err = os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndVerifyArchive(f, identity, restore, true); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	data, err := os.ReadFile(filepath.Join(restore, "active", "faiz", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("restored content=%q", string(data))
	}
}
