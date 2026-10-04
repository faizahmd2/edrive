package rclone

import (
	"encoding/base64"
	"testing"
)

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
