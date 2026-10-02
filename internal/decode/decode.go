package decode

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

	agePath, err := exec.LookPath("age")
	if err != nil {
		return "", fmt.Errorf("age is required for decode")
	}

	outputPath := decodedPath(inputPath)
	if _, err := os.Stat(outputPath); err == nil {
		return "", fmt.Errorf("decoded file already exists: %s", outputPath)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("create decoded file: %w", err)
	}

	in, err := os.Open(inputPath)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(outputPath)
		return "", err
	}

	cmd := exec.Command(
		agePath,
		"--decrypt",
		"--identity", identityPath,
	)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = io.Discard

	runErr := cmd.Run()
	inCloseErr := in.Close()
	syncErr := error(nil)
	if runErr == nil {
		syncErr = out.Sync()
	}
	closeErr := out.Close()

	if runErr != nil || inCloseErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(outputPath)
		if runErr != nil {
			return "", fmt.Errorf("decode failed: %w", runErr)
		}
		if inCloseErr != nil {
			return "", inCloseErr
		}
		if syncErr != nil {
			return "", syncErr
		}
		return "", closeErr
	}

	return outputPath, nil
}

func decodedPath(input string) string {
	if strings.HasSuffix(input, ".age") {
		return strings.TrimSuffix(input, ".age")
	}
	return input + ".decoded"
}
