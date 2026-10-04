// Package backup writes and reads standalone recovery archives:
//
//	workspace -> tar -> zstd -> age (passphrase)
//
// The format is plain age + zstd + tar, so a backup can also be recovered
// without edrive:  age -d backup.tar.zst.age | zstd -d | tar x
package backup

import (
	"archive/tar"
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
)

const Suffix = ".tar.zst.age"

// Create writes an encrypted archive of root to w.
func Create(root string, passphrase string, w io.Writer) error {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	encrypted, err := age.Encrypt(w, recipient)
	if err != nil {
		return err
	}
	if err := writeArchive(root, encrypted); err != nil {
		return err
	}
	return encrypted.Close()
}

// writeArchive writes root as a zstd-compressed tar stream.
func writeArchive(root string, w io.Writer) error {
	compressed, err := zstd.NewWriter(w)
	if err != nil {
		return err
	}
	if err := writeTar(root, compressed); err != nil {
		compressed.Close()
		return err
	}
	return compressed.Close()
}

// GeneratePassphrase returns six groups of five characters (~150 bits) from
// an alphabet without look-alike characters, easy to copy or write down.
func GeneratePassphrase() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	groups := make([]string, 6)
	for g := range groups {
		var b strings.Builder
		for i := 0; i < 5; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", err
			}
			b.WriteByte(alphabet[n.Int64()])
		}
		groups[g] = b.String()
	}
	return strings.Join(groups, "-"), nil
}

// Identities builds decryption identities from either a passphrase or a key
// file (backups made by edrive before 0.5 used age key files).
func Identities(passphrase, keyFile string) ([]age.Identity, error) {
	if keyFile != "" {
		f, err := os.Open(keyFile)
		if err != nil {
			return nil, fmt.Errorf("open recovery key: %w", err)
		}
		defer f.Close()
		ids, err := age.ParseIdentities(f)
		if err != nil {
			return nil, fmt.Errorf("recovery key file is not an age key: %w", err)
		}
		return ids, nil
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	return []age.Identity{id}, nil
}

// Decode extracts the archive at input into a new folder next to it and
// returns that folder's path. Nothing is written if decryption fails.
func Decode(input string, ids []age.Identity) (string, error) {
	input = filepath.Clean(input)
	info, err := os.Stat(input)
	if err != nil {
		return "", fmt.Errorf("backup file is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("backup is not a file")
	}
	output := DecodedPath(input)
	if _, err := os.Stat(output); err == nil {
		return "", fmt.Errorf("output folder already exists: %s", output)
	}

	in, err := os.Open(input)
	if err != nil {
		return "", err
	}
	defer in.Close()

	plain, err := age.Decrypt(bufio.NewReader(in), ids...)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return "", fmt.Errorf("wrong passphrase or recovery key")
		}
		return "", fmt.Errorf("decrypt backup: %w", err)
	}
	decompressed, err := zstd.NewReader(plain)
	if err != nil {
		return "", fmt.Errorf("decompress backup: %w", err)
	}
	defer decompressed.Close()

	stage, err := os.MkdirTemp(filepath.Dir(output), "."+filepath.Base(output)+"-*")
	if err != nil {
		return "", err
	}
	if err := extractTar(decompressed, stage); err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	if err := os.Rename(stage, output); err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	return output, nil
}

func DecodedPath(input string) string {
	if strings.HasSuffix(input, Suffix) {
		return strings.TrimSuffix(input, Suffix)
	}
	return input + ".decoded"
}

func writeTar(root string, output io.Writer) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("read workspace: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace is not a directory")
	}

	tw := tar.NewWriter(output)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if shouldSkip(entry.Name()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)

		switch {
		case entry.IsDir():
			return tw.WriteHeader(&tar.Header{Name: name + "/", Mode: 0700, Typeflag: tar.TypeDir})
		case entry.Type().IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{
				Name:     name,
				Mode:     int64(info.Mode().Perm()),
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				Typeflag: tar.TypeReg,
			}); err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, f)
			closeErr := f.Close()
			if copyErr != nil {
				return fmt.Errorf("read %s: %w", name, copyErr)
			}
			return closeErr
		default:
			// Symlinks and special files are not part of a backup.
			return nil
		}
	})
	closeErr := tw.Close()
	if walkErr != nil {
		return walkErr
	}
	return closeErr
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	seen := make(map[string]struct{})
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read backup archive: %w", err)
		}
		name, err := safeTarPath(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate archive entry: %s", header.Name)
		}
		seen[name] = struct{}{}

		target := filepath.Join(dest, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, archiveMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported archive entry %q", header.Name)
		}
	}
}

func safeTarPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "./" {
		return "", nil
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return clean, nil
}

func archiveMode(mode int64) os.FileMode {
	perm := os.FileMode(mode) & 0o777
	if perm == 0 {
		return 0600
	}
	return perm & 0o700 // files restored from a backup stay private to you
}

func shouldSkip(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}
