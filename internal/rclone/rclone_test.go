package rclone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffReadsCombinedReportWithoutWriting(t *testing.T) {
	bin := t.TempDir()
	fake := filepath.Join(bin, "rclone")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '= same.txt' '+ local-only.txt' '- remote-only.txt' '* changed.txt'\n" +
		"exit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	local := t.TempDir()
	client, err := New(fake, "edrive-cloud", "edrive")
	if err != nil {
		t.Fatal(err)
	}

	report, err := client.Diff(local)
	if err != nil {
		t.Fatal(err)
	}

	want := "Differences:\n+ local-only.txt\n- remote-only.txt\n* changed.txt\n\n3 difference(s).\n"
	if report != want {
		t.Fatalf("report=%q, want %q", report, want)
	}
}

func TestDiffReturnsComparisonErrors(t *testing.T) {
	bin := t.TempDir()
	fake := filepath.Join(bin, "rclone")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '! broken.txt'\n" +
		"exit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	client, err := New(fake, "edrive-cloud", "edrive")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Diff(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "error(s)") {
		t.Fatalf("expected comparison error, got %v", err)
	}
}
