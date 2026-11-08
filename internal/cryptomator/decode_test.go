package cryptomator

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/crypto/scrypt"
)

// The fixture in testdata/vault8 was written by the real Cryptomator CLI:
// edrive only made the empty vault (which the CLI accepted), then every file
// name and file body below went in through the CLI's mount. Make it again with
//
//	EDRIVE_MAKE_FIXTURE=1 go test ./internal/cryptomator -run TestMakeFixture
const fixturePassword = "edrive-test-only"

var longName = strings.Repeat("बहुत लंबा नाम ", 8) + "and a long English tail so the encrypted name passes 220 characters.txt"

// fixtureFiles is what the vault holds: path → contents.
func fixtureFiles() map[string][]byte {
	pattern := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i*7 + i/251)
		}
		return b
	}
	return map[string][]byte{
		"notes.txt":                                    []byte("hello from the vault\n"),
		"empty.txt":                                    {},
		"chunks/exactly-two.bin":                       pattern(2 * chunkPlain),
		"chunks/seventy-kb.bin":                        pattern(70 * 1024),
		"folder/nested/deep.txt":                       []byte("three levels down\n"),
		"हिंदी नोट.txt":                                []byte("नमस्ते, यह एक परीक्षण है\n"),
		"long/" + longName:                             []byte("a long name lives in a .c9s folder\n"),
		strings.Repeat("folder-", 30) + "x/inside.txt": []byte("inside a long-named folder\n"),
	}
}

func TestDecodeFixture(t *testing.T) {
	v, err := OpenVault(filepath.Join("testdata", "vault8"), []byte(fixturePassword))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	got := map[string][]byte{}
	dirs := map[string]bool{}
	err = v.Walk(func(n Node) error {
		switch n.Kind {
		case Dir:
			dirs[n.Path] = true
		case File:
			r, err := v.Open(n)
			if err != nil {
				return err
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			got[n.Path] = b
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := fixtureFiles()
	for p, body := range want {
		if !bytes.Equal(got[p], body) {
			t.Errorf("%s: got %d bytes, want %d", p, len(got[p]), len(body))
		}
		if !dirs[filepath.Dir(p)] && filepath.Dir(p) != "." {
			t.Errorf("%s: its folder was not walked", p)
		}
	}
	for p := range got {
		if isAppleDouble(p) { // macOS keeps extended attributes beside a file over FUSE-T
			continue
		}
		if _, ok := want[p]; !ok {
			t.Errorf("unexpected file %q", p)
		}
	}

	out := filepath.Join(t.TempDir(), "plain")
	st, err := v.DecodeTo(out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != len(got) {
		t.Errorf("DecodeTo wrote %d files, the walk saw %d", st.Files, len(got))
	}
	for p, body := range want {
		b, err := os.ReadFile(filepath.Join(out, p))
		if err != nil || !bytes.Equal(b, body) {
			t.Errorf("decoded %s differs: %v", p, err)
		}
	}
	if _, err := v.DecodeTo(out); err == nil {
		t.Error("DecodeTo must refuse a folder that is not empty")
	}
}

func isAppleDouble(p string) bool { return strings.HasPrefix(filepath.Base(p), "._") }

func TestDecodeRefusesWrongPassword(t *testing.T) {
	if _, err := OpenVault(filepath.Join("testdata", "vault8"), []byte("not it")); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("got %v", err)
	}
}

// A copy of the fixture with one byte changed in each place that matters
// must fail loudly, never hand back wrong data.
func TestDecodeRefusesTampering(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"a file body":   func(t *testing.T, root string) { flipInLargest(t, root, 100) },
		"a file header": func(t *testing.T, root string) { flipInLargest(t, root, 20) },
		"the vault config": func(t *testing.T, root string) {
			p := filepath.Join(root, "vault.cryptomator")
			b, _ := os.ReadFile(p)
			parts := strings.Split(string(b), ".")
			payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
			payload = bytes.Replace(payload, []byte(`"format":8`), []byte(`"format":9`), 1)
			parts[1] = base64.RawURLEncoding.EncodeToString(payload)
			_ = os.WriteFile(p, []byte(strings.Join(parts, ".")), 0600)
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			root := copyTree(t, filepath.Join("testdata", "vault8"))
			spoil(t, root)
			v, err := OpenVault(root, []byte(fixturePassword))
			if err == nil {
				defer v.Close()
				_, err = v.DecodeTo(filepath.Join(t.TempDir(), "out"))
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("got %v, want ErrCorrupt", err)
			}
		})
	}
}

func flipInLargest(t *testing.T, root string, at int64) {
	t.Helper()
	var largest string
	var size int64
	_ = filepath.WalkDir(filepath.Join(root, "d"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, _ := d.Info(); info.Size() > size {
				largest, size = p, info.Size()
			}
		}
		return nil
	})
	f, err := os.OpenFile(largest, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := make([]byte, 1)
	_, _ = f.ReadAt(b, at)
	b[0] ^= 1
	_, _ = f.WriteAt(b, at)
}

func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "vault")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// TestMakeFixture builds testdata/vault8 with the real Cryptomator CLI. It
// needs FUSE-T and the CLI that 'edrive setup' installs.
func TestMakeFixture(t *testing.T) {
	if os.Getenv("EDRIVE_MAKE_FIXTURE") != "1" {
		t.Skip("set EDRIVE_MAKE_FIXTURE=1 to rebuild testdata/vault8")
	}
	home, _ := os.UserHomeDir()
	cli := CLIPath(filepath.Join(home, ".edrive", "tools"))
	if _, err := exec.LookPath(cli); err != nil {
		t.Fatalf("Cryptomator CLI not found at %s", cli)
	}
	work, err := filepath.EvalSymlinks(t.TempDir()) // /var is /private/var in the mount table
	if err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(work, "vault")
	mount := filepath.Join(work, "mnt")
	if err := os.MkdirAll(mount, 0700); err != nil {
		t.Fatal(err)
	}
	createEmptyVault(t, vault, []byte(fixturePassword))

	c := &Client{VaultPath: vault, MountPoint: mount, CLIPath: cli, RuntimeDir: filepath.Join(work, "run")}
	if err := c.Unlock([]byte(fixturePassword)); err != nil {
		t.Fatal(err)
	}
	files := fixtureFiles()
	var names []string
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		at := filepath.Join(mount, p)
		if err := os.MkdirAll(filepath.Dir(at), 0700); err != nil {
			_ = c.Lock()
			t.Fatal(err)
		}
		if err := os.WriteFile(at, files[p], 0600); err != nil {
			_ = c.Lock()
			t.Fatal(err)
		}
	}
	// Lock itself must hand every write to Cryptomator before it stops.
	if err := c.Lock(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join("testdata", "vault8")
	_ = os.RemoveAll(dst)
	err = filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(vault, p)
		if d.Name() == ".DS_Store" || strings.HasPrefix(d.Name(), "._") {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// createEmptyVault writes a new format 8, SIV_GCM vault the way Cryptomator
// does: a scrypt-wrapped masterkey, a signed config and the root folder.
func createEmptyVault(t *testing.T, root string, password []byte) {
	t.Helper()
	enc, mac, salt := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	for _, b := range [][]byte{enc, mac, salt} {
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
	}
	kek, err := scrypt.Key(password, salt, 32768, 8, 1, 32)
	if err != nil {
		t.Fatal(err)
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, 999)
	versionMac := hmac.New(sha256.New, mac)
	versionMac.Write(version)
	mk, _ := json.Marshal(map[string]any{
		"version":          999,
		"scryptSalt":       base64.StdEncoding.EncodeToString(salt),
		"scryptCostParam":  32768,
		"scryptBlockSize":  8,
		"primaryMasterKey": base64.StdEncoding.EncodeToString(aesKeyWrap(t, kek, enc)),
		"hmacMasterKey":    base64.StdEncoding.EncodeToString(aesKeyWrap(t, kek, mac)),
		"versionMac":       base64.StdEncoding.EncodeToString(versionMac.Sum(nil)),
	})
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "masterkey.cryptomator"), mk, 0600); err != nil {
		t.Fatal(err)
	}

	part := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	jti := make([]byte, 16)
	_, _ = rand.Read(jti)
	head := part(map[string]string{"kid": "masterkeyfile:masterkey.cryptomator", "typ": "JWT", "alg": "HS256"})
	body := part(map[string]any{
		"jti":                 fmt.Sprintf("%x-%x-%x-%x-%x", jti[:4], jti[4:6], jti[6:8], jti[8:10], jti[10:]),
		"format":              8,
		"cipherCombo":         "SIV_GCM",
		"shorteningThreshold": 220,
	})
	sign := hmac.New(sha256.New, append(append([]byte(nil), enc...), mac...))
	sign.Write([]byte(head + "." + body))
	token := head + "." + body + "." + base64.RawURLEncoding.EncodeToString(sign.Sum(nil))
	if err := os.WriteFile(filepath.Join(root, "vault.cryptomator"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}

	v := &Vault{root: root, enc: enc, mac: mac}
	rootDir, err := v.dirPath("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootDir, 0700); err != nil {
		t.Fatal(err)
	}
}
