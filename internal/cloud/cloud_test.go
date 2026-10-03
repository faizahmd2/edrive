package cloud

import "testing"

func TestProviderAliases(t *testing.T) {
	cases := map[string]string{
		"": "",
		"google": "1",
		"gdrive": "1",
		"s3": "2",
		"r2": "e",
		"b2": "3",
		"dropbox": "4",
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
