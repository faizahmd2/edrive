package cloud

import (
	"os"
	"testing"
)

func TestProviderAliases(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"google":   "1",
		"gdrive":   "1",
		"s3":       "2",
		"r2":       "e",
		"b2":       "3",
		"dropbox":  "4",
		"onedrive": "5",
	}
	for input, code := range cases {
		if input == "" {
			continue
		}
		p, err := providerFor(input)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if p.Code != code {
			t.Fatalf("%q: code=%q, want %q", input, p.Code, code)
		}
	}
}

func TestPatchValueUpdatesOnlyTargetSection(t *testing.T) {
	path := t.TempDir() + "/rclone.conf"
	initial := "[other]\ntype = local\n\n[edrive-cloud]\ntype = drive\nscope = drive\n\n[last]\ntype = sftp\n"
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PatchValue(path, "edrive-cloud", "client_id", "client-id"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[other]\ntype = local\n\n[edrive-cloud]\ntype = drive\nscope = drive\nclient_id = client-id\n\n[last]\ntype = sftp\n"
	if string(got) != want {
		t.Fatalf("config=%q, want %q", got, want)
	}
}
