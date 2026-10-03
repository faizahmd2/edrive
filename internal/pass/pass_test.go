package pass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetAndGet(t *testing.T) {
	workspace := t.TempDir()

	if err := Set(workspace, "insta", "s3cret value"); err != nil {
		t.Fatal(err)
	}

	value, err := Get(workspace, "insta")
	if err != nil {
		t.Fatal(err)
	}
	if value != "s3cret value" {
		t.Fatalf("value=%q", value)
	}

	info, err := os.Stat(filepath.Join(workspace, "pass", "insta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode=%#o, want 0600", got)
	}
}

func TestSetRejectsEmptyAndMultiline(t *testing.T) {
	workspace := t.TempDir()

	if err := Set(workspace, "insta", ""); err == nil {
		t.Fatal("expected empty value error")
	}
	if err := Set(workspace, "insta", "one\ntwo"); err == nil {
		t.Fatal("expected multiline value error")
	}
}

func TestList(t *testing.T) {
	workspace := t.TempDir()
	if err := Set(workspace, "github", "g"); err != nil {
		t.Fatal(err)
	}
	if err := Set(workspace, "insta", "i"); err != nil {
		t.Fatal(err)
	}
	names, err := List(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "github" || names[1] != "insta" {
		t.Fatalf("names=%v", names)
	}
}

func TestSetTextAllowsMultiline(t *testing.T) {
	workspace := t.TempDir()
	value := "line one\nline two\nline three"
	if err := SetText(workspace, "certificate", value); err != nil {
		t.Fatal(err)
	}
	got, err := Get(workspace, "certificate")
	if err != nil {
		t.Fatal(err)
	}
	if got != value {
		t.Fatalf("value=%q", got)
	}
}

func TestReadTextPreservesNewline(t *testing.T) {
	workspace := t.TempDir()
	if err := SetText(workspace, "note", "line one\nline two\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadText(workspace, "note")
	if err != nil {
		t.Fatal(err)
	}
	if got != "line one\nline two\n" {
		t.Fatalf("value=%q", got)
	}
}

func TestLegacyExtensionlessEntryStillWorks(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "pass")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy"), []byte("old-value"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := Get(workspace, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got != "old-value" {
		t.Fatalf("value=%q", got)
	}
}
