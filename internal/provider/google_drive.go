package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type Provider interface {
	Name() string
	Root() (string, error)
}

type GoogleDrive struct {
	StorageName string
}

func (p GoogleDrive) Name() string {
	return "Google Drive"
}

func (p GoogleDrive) Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var candidates []string

	legacy := filepath.Join(home, "Google Drive", "My Drive")
	if isDir(legacy) {
		candidates = append(candidates, legacy)
	}

	mirrored := filepath.Join(home, "Library", "CloudStorage")
	matches, err := filepath.Glob(filepath.Join(mirrored, "GoogleDrive-*", "My Drive"))
	if err != nil {
		return "", fmt.Errorf("find Google Drive storage: %w", err)
	}
	for _, match := range matches {
		if isDir(match) {
			candidates = append(candidates, match)
		}
	}

	sort.Strings(candidates)
	candidates = unique(candidates)

	if len(candidates) == 0 {
		return "", fmt.Errorf("Google Drive local storage is not available")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	name := p.StorageName
	if name == "" {
		name = "edrive"
	}

	var existing []string
	for _, root := range candidates {
		vault := filepath.Join(root, name, "vault.cryptomator")
		if isFile(vault) {
			existing = append(existing, root)
		}
	}
	if len(existing) == 1 {
		return existing[0], nil
	}

	return "", fmt.Errorf("multiple Google Drive local storage locations found; edrive cannot choose safely")
}

func (p GoogleDrive) VaultPath() (string, error) {
	root, err := p.Root()
	if err != nil {
		return "", err
	}
	name := p.StorageName
	if name == "" {
		name = "edrive"
	}
	return filepath.Join(root, name), nil
}

func EnsureRunning() error {
	if _, err := exec.LookPath("open"); err != nil {
		return fmt.Errorf("macOS open command is unavailable")
	}
	_ = exec.Command("open", "-a", "Google Drive").Run()
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func unique(paths []string) []string {
	out := paths[:0]
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

