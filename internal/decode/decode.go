package decode

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const encryptedSuffix = ".tar.zst.age"

type stageResult struct {
	name string
	err  error
}

func File(inputPath, identityPath string) (string, error) {
	if inputPath == "" || identityPath == "" {
		return "", fmt.Errorf("encrypted file and recovery key are required")
	}

	inputPath = filepath.Clean(inputPath)
	identityPath = filepath.Clean(identityPath)

	if info, err := os.Stat(inputPath); err != nil {
		return "", fmt.Errorf("encrypted file is unavailable: %w", err)
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("encrypted input is not a file")
	}
	if info, err := os.Stat(identityPath); err != nil {
		return "", fmt.Errorf("recovery key is unavailable: %w", err)
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("recovery key is not a file")
	}

	if !strings.HasSuffix(inputPath, encryptedSuffix) {
		return "", fmt.Errorf("encrypted backup must end with %s", encryptedSuffix)
	}

	agePath, err := exec.LookPath("age")
	if err != nil {
		return "", fmt.Errorf("age is required for decode")
	}
	zstdPath, err := exec.LookPath("zstd")
	if err != nil {
		return "", fmt.Errorf("zstd is required for decode")
	}

	outputPath := decodedPath(inputPath)
	if _, err := os.Stat(outputPath); err == nil {
		return "", fmt.Errorf("output folder already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	stageDir, err := os.MkdirTemp(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+"-decode-*")
	if err != nil {
		return "", fmt.Errorf("create recovery workspace: %w", err)
	}
	stagePath := stageDir
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagePath)
		}
	}()

	in, err := os.Open(inputPath)
	if err != nil {
		return "", fmt.Errorf("open encrypted backup: %w", err)
	}
	defer in.Close()

	ageCmd := exec.Command(agePath, "--decrypt", "--identity", identityPath)
	var ageErr bytes.Buffer
	ageCmd.Stdin = in
	ageCmd.Stderr = &ageErr

	ageOut, err := ageCmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create age output pipe: %w", err)
	}

	zstdCmd := exec.Command(zstdPath, "-d", "-c")
	var zstdErr bytes.Buffer
	zstdCmd.Stderr = &zstdErr
	zstdIn, err := zstdCmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("create zstd input pipe: %w", err)
	}
	zstdOut, err := zstdCmd.StdoutPipe()
	if err != nil {
		_ = zstdIn.Close()
		return "", fmt.Errorf("create zstd output pipe: %w", err)
	}

	if err := ageCmd.Start(); err != nil {
		_ = zstdIn.Close()
		return "", fmt.Errorf("start age: %w", err)
	}
	if err := zstdCmd.Start(); err != nil {
		_ = zstdIn.Close()
		_ = ageCmd.Process.Kill()
		_ = ageCmd.Wait()
		return "", fmt.Errorf("start zstd: %w", err)
	}

	results := make(chan stageResult, 2)

	go func() {
		_, copyErr := io.Copy(zstdIn, ageOut)
		closeErr := zstdIn.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		results <- stageResult{name: "decrypt stream", err: copyErr}
	}()

	go func() {
		results <- stageResult{name: "extract backup", err: extractTar(zstdOut, stagePath)}
	}()

	var firstErr error
	for remaining := 2; remaining > 0; remaining-- {
		result := <-results
		if result.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", result.name, result.err)
			_ = zstdIn.Close()
			_ = ageOut.Close()
			_ = zstdOut.Close()
			_ = ageCmd.Process.Kill()
			_ = zstdCmd.Process.Kill()
		}
	}

	ageWaitErr := ageCmd.Wait()
	zstdWaitErr := zstdCmd.Wait()

	if firstErr != nil {
		_ = os.RemoveAll(stagePath)
		return "", firstErr
	}
	if zstdWaitErr != nil {
		_ = os.RemoveAll(stagePath)
		return "", commandError("decompress backup", zstdWaitErr, zstdErr.String())
	}
	if ageWaitErr != nil {
		_ = os.RemoveAll(stagePath)
		return "", commandError("decrypt backup", ageWaitErr, ageErr.String())
	}

	if err := os.Rename(stagePath, outputPath); err != nil {
		_ = os.RemoveAll(stagePath)
		return "", fmt.Errorf("finalize recovery folder: %w", err)
	}
	cleanup = false

	return outputPath, nil
}

func decodedPath(input string) string {
	if strings.HasSuffix(input, encryptedSuffix) {
		return strings.TrimSuffix(input, encryptedSuffix)
	}
	return input + ".decoded"
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
			return err
		}

		name, err := safeTarPath(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate archive entry: %s", header.Name)
		}
		seen[name] = struct{}{}

		target := filepath.Join(dest, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			if err := os.Chmod(target, archiveMode(header.Mode, 0700)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
			if err := os.Chmod(target, archiveMode(header.Mode, 0600)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry %q", header.Name)
		}
	}
}

func safeTarPath(name string) (string, error) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	if name == "" || name == "." {
		return "", nil
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return clean, nil
}

func archiveMode(mode int64, fallback os.FileMode) os.FileMode {
	const permissionMask = 0o777
	permissions := os.FileMode(mode) & permissionMask
	if permissions == 0 {
		return fallback
	}
	return permissions
}

func commandError(prefix string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %w: %s", prefix, err, stderr)
}
