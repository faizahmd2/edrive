package ageutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func KeygenPath(agePath string) (string, error) {
	if agePath == "" {
		return "", fmt.Errorf("age is not configured")
	}
	path := filepath.Join(filepath.Dir(agePath), "age-keygen")
	if info, err := os.Stat(path); err != nil || info.IsDir() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("age-keygen is unavailable")
	}
	return path, nil
}

func GenerateIdentity(keygenPath, tempDir string) (string, error) {
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return "", err
	}

	workspace, err := os.MkdirTemp(tempDir, ".identity-*")
	if err != nil {
		return "", fmt.Errorf("create age identity workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	path := filepath.Join(workspace, "identity.txt")
	cmd := exec.Command(keygenPath, "-pq", "-o", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return "", fmt.Errorf("generate age identity: %w", err)
		}
		return "", fmt.Errorf("generate age identity: %w: %s", err, detail)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read generated age identity: %w", err)
	}
	identity := strings.TrimSpace(string(b))
	if identity == "" {
		return "", fmt.Errorf("generated age identity is empty")
	}
	return identity, nil
}

func Recipient(identity, keygenPath, tempDir string) (string, error) {
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(tempDir, ".recipient-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer os.Remove(path)

	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return "", err
	}
	if _, err := f.WriteString(strings.TrimSpace(identity) + "\n"); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	cmd := exec.Command(keygenPath, "-y", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return "", fmt.Errorf("derive age recipient: %w", err)
		}
		return "", fmt.Errorf("derive age recipient: %w: %s", err, detail)
	}
	recipient := strings.TrimSpace(string(out))
	if !strings.HasPrefix(recipient, "age1") {
		return "", fmt.Errorf("invalid age recipient")
	}
	return recipient, nil
}
