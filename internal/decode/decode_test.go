package decode

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodedPath(t *testing.T) {
	cases := map[string]string{
		"/tmp/a.tar.zst.age": "/tmp/a",
		"/tmp/a.age":         "/tmp/a.age.decoded",
		"/tmp/a.enc":         "/tmp/a.enc.decoded",
	}
	for input, want := range cases {
		if got := decodedPath(input); got != want {
			t.Fatalf("decodedPath(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestFileExtractsBackup(t *testing.T) {
	bin := t.TempDir()

	age := filepath.Join(bin, "age")
	if err := os.WriteFile(age, []byte("#!/bin/sh\nset -eu\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}
	zstd := filepath.Join(bin, "zstd")
	if err := os.WriteFile(zstd, []byte("#!/bin/sh\nset -eu\ncat\n"), 0700); err != nil {
		t.Fatal(err)
	}

	input := filepath.Join(t.TempDir(), "backup.tar.zst.age")
	key := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(input, makeTar(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("identity"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := File(input, key)
	if err != nil {
		t.Fatal(err)
	}
	if output != filepath.Join(filepath.Dir(input), "backup") {
		t.Fatalf("output=%q", output)
	}

	data, err := os.ReadFile(filepath.Join(output, "notes", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\n" {
		t.Fatalf("output=%q", data)
	}

	if _, err := os.Stat(input[:len(input)-len(".age")]); !os.IsNotExist(err) {
		t.Fatalf("intermediate archive should not remain")
	}

	if err := os.RemoveAll(output); err != nil {
		t.Fatal(err)
	}
}

func TestFileRejectsExistingOutput(t *testing.T) {
	bin := t.TempDir()
	age := filepath.Join(bin, "age")
	zstd := filepath.Join(bin, "zstd")
	for _, path := range []string{age, zstd} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\ncat\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	input := filepath.Join(t.TempDir(), "backup.tar.zst.age")
	if err := os.WriteFile(input, makeTar(t), 0600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(key, []byte("identity"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(input), "backup")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := File(input, key); err == nil {
		t.Fatal("expected existing output error")
	}
}

func makeTar(t *testing.T) []byte {
	t.Helper()
	var buf testBuffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "notes/", Typeflag: tar.TypeDir, Mode: 0700}); err != nil {
		t.Fatal(err)
	}
	data := []byte("hello world\n")
	if err := tw.WriteHeader(&tar.Header{Name: "notes/hello.txt", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type testBuffer struct {
	data []byte
}

func (b *testBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *testBuffer) Bytes() []byte {
	return b.data
}
