package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type stageResult struct {
	name string
	err  error
}

func Create(root string, recipients []string, agePath, zstdPath string, output io.Writer) error {
	if root == "" {
		return fmt.Errorf("workspace path is empty")
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no backup recipients configured")
	}
	if agePath == "" || zstdPath == "" {
		return fmt.Errorf("backup tools are not configured")
	}

	ageArgs := []string{"--encrypt"}
	for _, recipient := range recipients {
		recipient = strings.TrimSpace(recipient)
		if !strings.HasPrefix(recipient, "age1") {
			return fmt.Errorf("invalid backup recipient")
		}
		ageArgs = append(ageArgs, "--recipient", recipient)
	}

	ageCmd := exec.Command(agePath, ageArgs...)
	ageIn, err := ageCmd.StdinPipe()
	if err != nil {
		return err
	}
	ageCmd.Stdout = output
	ageCmd.Stderr = io.Discard

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
	zstdCmd.Stderr = io.Discard

	if err := ageCmd.Start(); err != nil {
		_ = ageIn.Close()
		_ = zstdIn.Close()
		return fmt.Errorf("start age: %w", err)
	}
	if err := zstdCmd.Start(); err != nil {
		_ = ageIn.Close()
		_ = zstdIn.Close()
		_ = ageCmd.Process.Kill()
		_ = ageCmd.Wait()
		return fmt.Errorf("start zstd: %w", err)
	}

	done := make(chan stageResult, 2)

	go func() {
		err := writeTar(root, zstdIn)
		closeErr := zstdIn.Close()
		if err == nil {
			err = closeErr
		}
		done <- stageResult{name: "tar", err: err}
	}()

	go func() {
		_, err := io.Copy(ageIn, zstdOut)
		closeErr := ageIn.Close()
		if err == nil {
			err = closeErr
		}
		done <- stageResult{name: "pipe", err: err}
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		result := <-done
		if result.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", result.name, result.err)
			_ = zstdCmd.Process.Kill()
			_ = ageCmd.Process.Kill()
			_ = zstdIn.Close()
			_ = ageIn.Close()
		}
	}

	zstdErr := zstdCmd.Wait()
	ageErr := ageCmd.Wait()

	if firstErr != nil {
		return firstErr
	}
	if zstdErr != nil {
		return fmt.Errorf("compress backup: %w", zstdErr)
	}
	if ageErr != nil {
		return fmt.Errorf("encrypt backup: %w", ageErr)
	}
	return nil
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
		if path != root && shouldSkip(entry.Name()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)

		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in workspace: %s", name)
		}

		switch {
		case entry.IsDir():
			return tw.WriteHeader(&tar.Header{
				Name:     name + "/",
				Mode:     0700,
				Typeflag: tar.TypeDir,
			})
		case entry.Type().IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{
				Name:     name,
				Mode:     int64(info.Mode().Perm()),
				Size:     info.Size(),
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
			return fmt.Errorf("unsupported file in workspace: %s", name)
		}
	})
	closeErr := tw.Close()
	if walkErr != nil {
		return walkErr
	}
	return closeErr
}

func shouldSkip(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}
