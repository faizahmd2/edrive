package pass

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Get(workspace, name string) (string, error) {
	value, err := ReadText(workspace, name)
	if err != nil {
		return "", err
	}
	if len(value) == 0 {
		return "", fmt.Errorf("pass entry %q is empty", name)
	}
	if value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
	}
	if len(value) == 0 {
		return "", fmt.Errorf("pass entry %q is empty", name)
	}
	return value, nil
}

func Exists(workspace, name string) bool {
	path, err := entryPath(workspace, name)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func ReadText(workspace, name string) (string, error) {
	path, err := entryPath(workspace, name)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("pass entry %q does not exist", name)
		}
		return "", fmt.Errorf("read pass entry %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("pass entry %q is not a regular file", name)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read pass entry %q: %w", name, err)
	}
	return string(value), nil
}

func Set(workspace, name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("pass value cannot be empty")
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("pass value must be a single line")
	}
	return setText(workspace, name, value)
}

func SetText(workspace, name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("pass value cannot be empty")
	}
	return setText(workspace, name, value)
}

func setText(workspace, name, value string) error {
	path, err := entryPath(workspace, name)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create pass directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".pass-*.tmp")
	if err != nil {
		return fmt.Errorf("create pass entry temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0600); err != nil {
		return fmt.Errorf("protect pass entry temp file: %w", err)
	}
	if _, err := tmp.WriteString(value); err != nil {
		return fmt.Errorf("write pass entry: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync pass entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close pass entry: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("save pass entry %q: %w", name, err)
	}
	return nil
}

func List(workspace string) ([]string, error) {
	dir := filepath.Join(workspace, "pass")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read pass directory: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func entryPath(workspace, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("pass name is required")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("invalid pass name %q", name)
	}

	dir := filepath.Join(workspace, "pass")
	legacy := filepath.Join(dir, name)
	text := filepath.Join(dir, name+".txt")
	if info, err := os.Stat(legacy); err == nil && info.Mode().IsRegular() {
		return legacy, nil
	}
	if info, err := os.Stat(text); err == nil && info.Mode().IsRegular() {
		return text, nil
	}
	return text, nil
}
