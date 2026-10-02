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

	tarDone := make(chan error, 1)
	go func() {
		err := writeTar(root, zstdIn)
		closeErr := zstdIn.Close()
		if err == nil {
			err = closeErr
		}
		tarDone <- err
	}()

	agePipeDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(ageIn, zstdOut)
		closeErr := ageIn.Close()
		if err == nil {
			err = closeErr
		}
		agePipeDone <- err
	}()

	tarErr := <-tarDone
	if tarErr != nil {
		_ = zstdCmd.Process.Kill()
		_ = ageCmd.Process.Kill()
		_ = zstdIn.Close()
		_ = ageIn.Close()
		<-agePipeDone
		_ = zstdCmd.Wait()
		_ = ageCmd.Wait()
		return tarErr
	}

	agePipeErr := <-agePipeDone
	if agePipeErr != nil {
		_ = zstdCmd.Process.Kill()
		_ = ageCmd.Process.Kill()
		_ = zstdCmd.Wait()
		_ = ageCmd.Wait()
		return fmt.Errorf("compress backup: %w", agePipeErr)
	}

	if err := zstdCmd.Wait(); err != nil {
		_ = ageCmd.Process.Kill()
		_ = ageCmd.Wait()
		return fmt.Errorf("compress backup: %w", err)
	}
	if err := ageCmd.Wait(); err != nil {
		return fmt.Errorf("encrypt backup: %w", err)
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
	defer tw.Close()

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
}

func shouldSkip(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}
