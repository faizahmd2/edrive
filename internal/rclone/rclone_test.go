package rclone

import (
	"encoding/base64"
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

func TestExtractAuthorizeTokenJSON(t *testing.T) {
	input := "notice\n{\"access_token\":\"access\",\"refresh_token\":\"refresh\"}\n"
	got, err := extractAuthorizeToken(input)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"access_token\":\"access\",\"refresh_token\":\"refresh\"}"
	if got != want {
		t.Fatalf("token=%q, want %q", got, want)
	}
}

func TestExtractAuthorizeTokenBase64Blob(t *testing.T) {
	jsonToken := "{\"access_token\":\"access\",\"refresh_token\":\"refresh\"}"
	encoded := base64.StdEncoding.EncodeToString([]byte(jsonToken))
	got, err := extractAuthorizeToken(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != jsonToken {
		t.Fatalf("token=%q", got)
	}
}

func TestExtractAuthorizeTokenWrappedBlob(t *testing.T) {
	jsonToken := "{\"access_token\":\"access\",\"refresh_token\":\"refresh\"}"
	encoded := base64.RawStdEncoding.EncodeToString([]byte(jsonToken))
	input := "Paste the following into your remote machine --->\n" + encoded + "\n<---End paste\n"
	got, err := extractAuthorizeToken(input)
	if err != nil {
		t.Fatal(err)
	}
	if got != jsonToken {
		t.Fatalf("token=%q", got)
	}
}
