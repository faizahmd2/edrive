package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/cryptomator"
	"github.com/faizahmd2/edrive/internal/macos"
	"github.com/faizahmd2/edrive/internal/ui"
)

// DecodeVault writes a whole Cryptomator vault as plain files, with nothing
// but its password: no setup, no Cryptomator, no cloud.
func DecodeVault(vaultDir, outDir string) error {
	password, err := ui.ReadSecret("Vault password: ")
	if err != nil {
		return err
	}
	defer ui.Wipe(password)
	v, err := cryptomator.OpenVault(config.Expand(vaultDir), password)
	if err != nil {
		return err
	}
	defer v.Close()
	out := config.Expand(outDir)
	st, err := v.DecodeTo(out)
	if err != nil {
		return fmt.Errorf("stopped after %d file(s): %w", st.Files, err)
	}
	fmt.Printf("Decoded %d file(s) and %d folder(s), %.1f MB, into %s\n", st.Files, st.Dirs, float64(st.Bytes)/1e6, out)
	return nil
}

type verifyRecord struct {
	At    time.Time `json:"at"`
	Files int       `json:"files"`
}

func verifyPath() string { return filepath.Join(config.StateDir(), "verify.json") }

func lastVerify() (verifyRecord, bool) {
	data, err := os.ReadFile(verifyPath())
	if err != nil {
		return verifyRecord{}, false
	}
	var r verifyRecord
	return r, json.Unmarshal(data, &r) == nil
}

// Verify proves the vault can be read without Cryptomator: it decodes the
// local vault in memory with edrive's own decoder and compares every file
// with what Cryptomator shows in the workspace.
func (a App) Verify() error {
	if err := a.requireSetup(); err != nil {
		return err
	}
	return withLock(func() error {
		if err := macos.Authenticate("check that your vault opens without Cryptomator"); err != nil {
			if errors.Is(err, macos.ErrCancelled) {
				return fmt.Errorf("cancelled")
			}
			return err
		}
		password, err := macos.KeychainGet(config.KeychainService, config.VaultPasswordAccount)
		if err != nil {
			return fmt.Errorf("could not read the vault password from Keychain: %v", err)
		}
		defer ui.Wipe(password)

		v, err := cryptomator.OpenVault(config.LocalVaultPath(), password)
		if err != nil {
			return err
		}
		decoded, err := hashVault(v)
		v.Close()
		if err != nil {
			return fmt.Errorf("edrive's decoder could not read the vault: %w", err)
		}

		opened := false
		if !mounted() {
			if err := prepareMountPoint(); err != nil {
				return err
			}
			if err := a.crypto().Unlock(password); err != nil {
				return err
			}
			opened = true
		}
		shown, err := hashTree(config.WorkspacePath())
		if opened {
			if lockErr := a.crypto().Lock(); lockErr != nil && err == nil {
				err = lockErr
			}
		}
		if err != nil {
			return err
		}

		diffs := compareTrees(decoded, shown)
		if len(diffs) > 0 {
			for i, d := range diffs {
				if i == 5 {
					fmt.Printf("  … and %d more\n", len(diffs)-5)
					break
				}
				fmt.Println("  " + d)
			}
			return fmt.Errorf("%d difference(s) between edrive's decoder and Cryptomator; if you were editing files, run 'edrive verify' again when nothing is open", len(diffs))
		}
		data, _ := json.Marshal(verifyRecord{At: time.Now(), Files: len(decoded)})
		if err := config.WriteFileAtomic(verifyPath(), data, 0600); err != nil {
			return err
		}
		fmt.Printf("Verified %d file(s): your vault reads the same without Cryptomator.\n", len(decoded))
		return nil
	})
}

// A tree is path → what it holds: "dir", "link:<target>" or a file's SHA-256.
// macOS's "._" companions (extended attributes over FUSE-T) are left out on
// both sides.
func hashVault(v *cryptomator.Vault) (map[string]string, error) {
	tree := map[string]string{}
	err := v.Walk(func(n cryptomator.Node) error {
		if skipName(n.Path) {
			if n.Kind == cryptomator.Dir {
				return fs.SkipDir
			}
			return nil
		}
		switch n.Kind {
		case cryptomator.Dir:
			tree[n.Path] = "dir"
		case cryptomator.Symlink:
			tree[n.Path] = "link:" + n.Target
		default:
			r, err := v.Open(n)
			if err != nil {
				return err
			}
			defer r.Close()
			sum, err := sha(r)
			if err != nil {
				return fmt.Errorf("%q: %w", n.Path, err)
			}
			tree[n.Path] = sum
		}
		return nil
	})
	return tree, err
}

func hashTree(root string) (map[string]string, error) {
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if skipName(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			tree[rel] = "link:" + target
		case d.IsDir():
			tree[rel] = "dir"
		default:
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			sum, err := sha(f)
			if err != nil {
				return err
			}
			tree[rel] = sum
		}
		return nil
	})
	return tree, err
}

func skipName(p string) bool { return strings.HasPrefix(filepath.Base(p), "._") }

func sha(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// compareTrees lists every path the two trees disagree on, in order.
func compareTrees(decoded, shown map[string]string) []string {
	var diffs []string
	for p, want := range shown {
		got, ok := decoded[p]
		switch {
		case !ok:
			diffs = append(diffs, "missing from the decoder: "+p)
		case got != want:
			diffs = append(diffs, "differs: "+p)
		}
	}
	for p := range decoded {
		if _, ok := shown[p]; !ok {
			diffs = append(diffs, "only in the decoder: "+p)
		}
	}
	sort.Strings(diffs)
	return diffs
}
