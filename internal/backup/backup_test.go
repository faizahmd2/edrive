package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"filippo.io/age"
)

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pass", "github.txt"), "token-123")
	mustWrite(t, filepath.Join(root, "notes.md"), "hello")
	mustWrite(t, filepath.Join(root, ".DS_Store"), "skip")
	mustWrite(t, filepath.Join(root, "._notes.md"), "skip")

	out := filepath.Join(t.TempDir(), "edrive-backup"+Suffix)
	var buf bytes.Buffer
	if err := Create(root, "correct-horse", &buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	wrong, _ := Identities("nope", "")
	if _, err := Decode(out, wrong); err == nil {
		t.Fatal("wrong passphrase must fail")
	}
	if _, err := os.Stat(DecodedPath(out)); !os.IsNotExist(err) {
		t.Fatal("failed decode must not leave output behind")
	}

	ids, _ := Identities("correct-horse", "")
	dir, err := Decode(out, ids)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "pass", "github.txt")); string(b) != "token-123" {
		t.Fatalf("restored content = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatal(".DS_Store should be skipped")
	}
	if _, err := Decode(out, ids); err == nil {
		t.Fatal("decode must refuse to overwrite an existing folder")
	}
}

// Backups made before 0.5 were encrypted to age key files.
func TestDecodeWithKeyFile(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "key.txt")
	mustWrite(t, keyFile, id.String()+"\n")

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "legacy")

	out := filepath.Join(t.TempDir(), "old"+Suffix)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := age.Encrypt(f, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchive(root, enc); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	ids, err := Identities("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := Decode(out, ids)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "legacy" {
		t.Fatalf("restored = %q", b)
	}
}

func TestGeneratePassphrase(t *testing.T) {
	p, err := GeneratePassphrase()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^([a-z2-9]{5}-){5}[a-z2-9]{5}$`).MatchString(p) {
		t.Fatalf("unexpected passphrase format %q", p)
	}
}

func TestSafeTarPath(t *testing.T) {
	for _, bad := range []string{"../x", "/etc/passwd", "a/../../x"} {
		if _, err := safeTarPath(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
