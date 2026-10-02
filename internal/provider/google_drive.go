package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Provider interface {
	Name() string
	Root() (string, error)
	VaultPath(name string) (string, error)
}

type GoogleDrive struct {
	PreferredRoot string
	StorageName   string
}

func (p GoogleDrive) Name() string {
	return "Google Drive"
}

func (p GoogleDrive) Candidates() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	candidates := make([]string, 0, 8)

	matches, err := filepath.Glob(filepath.Join(home, "Library", "CloudStorage", "GoogleDrive-*", "My Drive"))
	if err != nil {
		return nil, fmt.Errorf("find Google Drive storage: %w", err)
	}
	for _, match := range matches {
		if isDir(match) {
			candidates = append(candidates, match)
		}
	}

	legacy := filepath.Join("/Volumes", "GoogleDrive", "My Drive")
	if isDir(legacy) {
		candidates = append(candidates, legacy)
	}

	if mount := googleDriveMountPoint(); mount != "" {
		for _, candidate := range []string{mount, filepath.Join(mount, "My Drive")} {
			if isDir(candidate) {
				candidates = append(candidates, candidate)
			}
		}
	}

	legacyMirror := filepath.Join(home, "Google Drive", "My Drive")
	if isDir(legacyMirror) {
		candidates = append(candidates, legacyMirror)
	}

	sort.Strings(candidates)
	return unique(candidates), nil
}

func (p GoogleDrive) Root() (string, error) {
	if root := cleanExisting(p.PreferredRoot); root != "" {
		return root, nil
	}

	candidates, err := p.Candidates()
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("Google Drive's local My Drive is not available")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("Google Drive has multiple local My Drive locations")
}

func (p GoogleDrive) VaultPath(name string) (string, error) {
	root, err := p.Root()
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "edrive"
	}
	relative, err := safeRelative(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, relative), nil
}

func NormalizeRoot(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return ""
	}
	if strings.EqualFold(filepath.Base(path), "My Drive") {
		return path
	}
	child := filepath.Join(path, "My Drive")
	if isDir(child) {
		return child
	}
	return path
}

func EnsureRunning() error {
	return exec.Command("/usr/bin/open", "-a", "Google Drive").Run()
}

func googleDriveMountPoint() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "com.google.drivefs.settings", "DefaultMountPoint").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cleanExisting(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" || !isDir(path) {
		return ""
	}
	return path
}

func safeRelative(path string) (string, error) {
	clean := filepath.Clean(path)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid edrive vault location")
	}
	return clean, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func unique(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		clean := filepath.Clean(path)
		if clean == "." {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}
