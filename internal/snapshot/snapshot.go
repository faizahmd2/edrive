package snapshot

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ManifestPath = "index/.edrive-manifest.json"
const ToolVersion = "0.1.0"

// BuildManifest walks the mounted, unlocked vault and hashes regular files.
// Symlinks and special files are rejected so the archive cannot escape the vault
// or contain device/socket entries.
func BuildManifest(root string) (Manifest, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("vault mount unavailable: %w", err)
	}
	if !info.IsDir() {
		return Manifest{}, fmt.Errorf("vault mount is not a directory: %s", root)
	}

	var files []File
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestPath {
			// The manifest is generated into the archive, not read from the vault.
			return nil
		}

		typ := d.Type()
		if typ&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in vault: %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		if !typ.IsRegular() {
			return fmt.Errorf("unsupported special file in vault: %s", rel)
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("hash %s: %w", rel, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", rel, closeErr)
		}
		fi, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if n != fi.Size() {
			return fmt.Errorf("file changed while hashing: %s", rel)
		}
		files = append(files, File{
			Path:   rel,
			Size:   n,
			SHA256: hex.EncodeToString(h.Sum(nil)),
			Mode:   uint32(fi.Mode().Perm()),
		})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Manifest{
		Format:      1,
		CreatedAt:   time.Now().UTC(),
		Tool:        "edrive",
		ToolVersion: ToolVersion,
		Source:      "logical-vault",
		Files:       files,
	}, nil
}

func encodeManifest(m Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

func writeTarHeader(tw *tar.Writer, name string, mode int64, size int64, typeflag byte) error {
	return tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     mode,
		Size:     size,
		Typeflag: typeflag,
	})
}

func CreateEncryptedSnapshot(root, recipientsFile, agePath, zstdPath string, manifest Manifest, output io.Writer) error {
	ageCmd := exec.Command(agePath, "--encrypt", "--recipients-file", recipientsFile)
	ageCmd.Stdout = output
	ageIn, err := ageCmd.StdinPipe()
	if err != nil {
		return err
	}

	zstdCmd := exec.Command(zstdPath, "-q", "-T0", "-c")
	zstdIn, err := zstdCmd.StdinPipe()
	if err != nil {
		_ = ageIn.Close()
		return err
	}
	zstdOut, err := zstdCmd.StdoutPipe()
	if err != nil {
		_ = ageIn.Close()
		_ = zstdIn.Close()
		return err
	}
	if err := ageCmd.Start(); err != nil {
		_ = ageIn.Close()
		_ = zstdIn.Close()
		return fmt.Errorf("start age: %w", err)
	}
	if err := zstdCmd.Start(); err != nil {
		_ = ageIn.Close()
		_ = zstdIn.Close()
		_ = ageCmd.Wait()
		return fmt.Errorf("start zstd: %w", err)
	}

	copyErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(ageIn, zstdOut)
		closeErr := ageIn.Close()
		if err == nil {
			err = closeErr
		}
		copyErr <- err
	}()

	tw := tar.NewWriter(zstdIn)
	manifestBytes, err := encodeManifest(manifest)
	if err != nil {
		_ = zstdIn.Close()
		_ = ageIn.Close()
		_ = zstdCmd.Wait()
		_ = ageCmd.Wait()
		return err
	}
	if err := writeTarHeader(tw, ManifestPath, 0600, int64(len(manifestBytes)), tar.TypeReg); err != nil {
		return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
	}

	// Directories are included so empty directories survive restore. File order
	// is stable and deterministic.
	var dirs []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestPath {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, rel+"/")
		}
		return nil
	})
	if err != nil {
		return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if err := writeTarHeader(tw, dir, 0700, 0, tar.TypeDir); err != nil {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
		}
	}

	for _, f := range manifest.Files {
		full := filepath.Join(root, filepath.FromSlash(f.Path))
		info, err := os.Stat(full)
		if err != nil {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, fmt.Errorf("stat %s: %w", f.Path, err))
		}
		if !info.Mode().IsRegular() {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, fmt.Errorf("file changed type during backup: %s", f.Path))
		}
		hdr := &tar.Header{Name: f.Path, Mode: int64(f.Mode), Size: info.Size(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
		}
		in, err := os.Open(full)
		if err != nil {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
		}
		h := sha256.New()
		mw := io.MultiWriter(tw, h)
		_, copyErr := io.Copy(mw, in)
		closeErr := in.Close()
		if copyErr != nil {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, fmt.Errorf("read %s: %w", f.Path, copyErr))
		}
		if closeErr != nil {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, closeErr)
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != f.SHA256 {
			return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, fmt.Errorf("file changed while archiving: %s", f.Path))
		}
	}

	if err := tw.Close(); err != nil {
		return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
	}
	if err := zstdIn.Close(); err != nil {
		return abortPipeline(zstdIn, ageIn, zstdCmd, ageCmd, err)
	}
	if err := <-copyErr; err != nil {
		_ = zstdCmd.Process.Kill()
		_ = zstdCmd.Wait()
		_ = ageCmd.Wait()
		return fmt.Errorf("age input: %w", err)
	}
	if err := zstdCmd.Wait(); err != nil {
		_ = ageCmd.Wait()
		return fmt.Errorf("zstd: %w", err)
	}
	if err := ageCmd.Wait(); err != nil {
		return fmt.Errorf("age: %w", err)
	}
	return nil
}

func abortPipeline(zstdIn, ageIn io.Closer, zstdCmd, ageCmd *exec.Cmd, cause error) error {
	_ = zstdIn.Close()
	_ = ageIn.Close()
	_ = zstdCmd.Wait()
	_ = ageCmd.Wait()
	return cause
}

type DecryptedArchive struct {
	Reader   io.ReadCloser
	ageCmd   *exec.Cmd
	zstdCmd  *exec.Cmd
	pipeDone chan error
}

func (a *DecryptedArchive) Wait() error {
	copyErr := <-a.pipeDone
	zstdErr := a.zstdCmd.Wait()
	ageErr := a.ageCmd.Wait()
	if copyErr != nil {
		return fmt.Errorf("age to zstd pipe: %w", copyErr)
	}
	if ageErr != nil {
		return fmt.Errorf("age: %w", ageErr)
	}
	if zstdErr != nil {
		return fmt.Errorf("zstd: %w", zstdErr)
	}
	return nil
}

func (a *DecryptedArchive) Abort() {
	_ = a.Reader.Close()
	if a.zstdCmd.Process != nil {
		_ = a.zstdCmd.Process.Kill()
	}
	if a.ageCmd.Process != nil {
		_ = a.ageCmd.Process.Kill()
	}
	_ = a.zstdCmd.Wait()
	_ = a.ageCmd.Wait()
}

func OpenDecryptedArchive(input io.Reader, identityPath, agePath, zstdPath string) (*DecryptedArchive, error) {
	ageCmd := exec.Command(agePath, "--decrypt", "--identity", identityPath)
	ageIn, err := ageCmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	ageOut, err := ageCmd.StdoutPipe()
	if err != nil {
		_ = ageIn.Close()
		return nil, err
	}
	if err := ageCmd.Start(); err != nil {
		_ = ageIn.Close()
		return nil, fmt.Errorf("start age: %w", err)
	}

	zstdCmd := exec.Command(zstdPath, "-q", "-d", "-c")
	zstdIn, err := zstdCmd.StdinPipe()
	if err != nil {
		_ = ageCmd.Process.Kill()
		_ = ageCmd.Wait()
		return nil, err
	}
	zstdOut, err := zstdCmd.StdoutPipe()
	if err != nil {
		_ = ageCmd.Process.Kill()
		_ = ageCmd.Wait()
		_ = zstdIn.Close()
		return nil, err
	}
	if err := zstdCmd.Start(); err != nil {
		_ = ageCmd.Process.Kill()
		_ = ageCmd.Wait()
		_ = zstdIn.Close()
		return nil, fmt.Errorf("start zstd: %w", err)
	}

	pipeDone := make(chan error, 1)
	go func() {
		_, copyToAgeErr := io.Copy(ageIn, input)
		_ = ageIn.Close()
		if copyToAgeErr != nil {
			_ = ageCmd.Process.Kill()
		}
		_, copyToZstdErr := io.Copy(zstdIn, ageOut)
		_ = zstdIn.Close()
		_ = ageOut.Close()
		if copyToAgeErr != nil {
			pipeDone <- copyToAgeErr
			return
		}
		pipeDone <- copyToZstdErr
	}()

	return &DecryptedArchive{
		Reader:   zstdOut,
		ageCmd:   ageCmd,
		zstdCmd:  zstdCmd,
		pipeDone: pipeDone,
	}, nil
}

func ReadAndVerifyArchive(input io.Reader, identityPath, agePath, zstdPath string, extractRoot string, extract bool) (Manifest, error) {
	archive, err := OpenDecryptedArchive(input, identityPath, agePath, zstdPath)
	if err != nil {
		return Manifest{}, err
	}
	plain := archive.Reader
	defer plain.Close()

	tr := tar.NewReader(bufio.NewReader(plain))
	hdr, err := tr.Next()
	if err != nil {
		archive.Abort()
		return Manifest{}, fmt.Errorf("read archive: %w", err)
	}
	if hdr.Name != ManifestPath || hdr.Typeflag != tar.TypeReg {
		archive.Abort()
		return Manifest{}, fmt.Errorf("archive missing first entry %s", ManifestPath)
	}
	var manifest Manifest
	manifestBytes, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
	if err != nil {
		archive.Abort()
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	if int64(len(manifestBytes)) != hdr.Size {
		archive.Abort()
		return Manifest{}, fmt.Errorf("manifest truncated")
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		archive.Abort()
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	expected := make(map[string]File, len(manifest.Files))
	for _, f := range manifest.Files {
		if _, exists := expected[f.Path]; exists {
			archive.Abort()
			return Manifest{}, fmt.Errorf("duplicate manifest file: %s", f.Path)
		}
		expected[f.Path] = f
	}

	if extract {
		if err := prepareRestoreRoot(extractRoot); err != nil {
			archive.Abort()
			return Manifest{}, err
		}
	}

	seen := make(map[string]bool, len(expected))
	for {
		hdr, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			archive.Abort()
			return Manifest{}, fmt.Errorf("read tar: %w", err)
		}
		safe, err := safeTarPath(hdr.Name)
		if err != nil {
			archive.Abort()
			return Manifest{}, err
		}
		if safe == ManifestPath {
			archive.Abort()
			return Manifest{}, fmt.Errorf("duplicate manifest entry")
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if extract {
				if err := os.MkdirAll(filepath.Join(extractRoot, filepath.FromSlash(strings.TrimSuffix(safe, "/"))), 0700); err != nil {
					archive.Abort()
					return Manifest{}, err
				}
			}
		case tar.TypeReg:
			exp, ok := expected[safe]
			if !ok {
				archive.Abort()
				return Manifest{}, fmt.Errorf("archive contains unexpected file: %s", safe)
			}
			if seen[safe] {
				archive.Abort()
				return Manifest{}, fmt.Errorf("duplicate file: %s", safe)
			}
			seen[safe] = true

			var dst *os.File
			if extract {
				dstPath := filepath.Join(extractRoot, filepath.FromSlash(safe))
				if err := os.MkdirAll(filepath.Dir(dstPath), 0700); err != nil {
					archive.Abort()
					return Manifest{}, err
				}
				dst, err = os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(exp.Mode))
				if err != nil {
					archive.Abort()
					return Manifest{}, fmt.Errorf("create %s: %w", safe, err)
				}
			}
			h := sha256.New()
			var sink io.Writer = io.Discard
			if dst != nil {
				sink = dst
			}
			mw := io.MultiWriter(sink, h)
			n, copyErr := io.Copy(mw, tr)
			closeErr := error(nil)
			if dst != nil {
				closeErr = dst.Close()
			}
			if copyErr != nil || closeErr != nil {
				archive.Abort()
				return Manifest{}, fmt.Errorf("extract %s: %v %v", safe, copyErr, closeErr)
			}
			if n != exp.Size {
				archive.Abort()
				return Manifest{}, fmt.Errorf("size mismatch: %s", safe)
			}
			if got := hex.EncodeToString(h.Sum(nil)); got != exp.SHA256 {
				archive.Abort()
				return Manifest{}, fmt.Errorf("sha256 mismatch: %s", safe)
			}
		default:
			archive.Abort()
			return Manifest{}, fmt.Errorf("unsupported tar entry type %d: %s", hdr.Typeflag, safe)
		}
	}

	if len(seen) != len(expected) {
		archive.Abort()
		return Manifest{}, fmt.Errorf("archive is missing %d expected file(s)", len(expected)-len(seen))
	}
	if err := archive.Wait(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func prepareRestoreRoot(root string) error {
	if root == "" {
		return fmt.Errorf("restore destination is required")
	}
	if info, err := os.Stat(root); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("restore destination is not a directory: %s", root)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("restore destination must be empty: %s", root)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(root, 0700)
}

func safeTarPath(name string) (string, error) {
	name = filepath.ToSlash(name)
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	if strings.ContainsRune(clean, '\x00') {
		return "", fmt.Errorf("unsafe archive path contains NUL")
	}
	return clean, nil
}
