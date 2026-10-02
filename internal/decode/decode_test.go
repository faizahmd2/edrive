package decode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodedPath(t *testing.T) {
	cases := map[string]string{
		"/tmp/a.tar.zst.age": "/tmp/a.tar.zst",
		"/tmp/a.age":         "/tmp/a",
		"/tmp/a.enc":         "/tmp/a.enc.decoded",
	}
	for input, want := range cases {
		if got := decodedPath(input); got != want {
			t.Fatalf("decodedPath(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestFileWithFakeAge(t *testing.T) {
	bin := t.TempDir()
	age := filepath.Join(bin, "age")
	script := "#!/bin/sh\nset -eu\ncat\n"
	if err := os.WriteFile(age, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	input := filepath.Join(t.TempDir(), "backup.age")
	if err := os.WriteFile(input, []byte("encrypted-ish"), 0600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(key, []byte("identity"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := File(input, key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "encrypted-ish" {
		t.Fatalf("output=%q", data)
	}
}
