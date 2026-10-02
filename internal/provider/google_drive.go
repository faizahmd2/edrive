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
	StorageName  string
}

func (p GoogleDrive) Name() string {
	return "Google Drive"
}

func (p GoogleDrive) Root() (string, error) {
	if root := filepath.Clean(p.PreferredRoot); root != "." && isDir(root) {
		return root, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	candidates := make([]string, 0, 8)

	// Modern Google Drive File Provider locations.
	matches, err := filepath.Glob(filepath.Join(home, "Library", "CloudStorage", "GoogleDrive-*", "My Drive"))
	if err != nil {
		return "", fmt.Errorf("find Google Drive storage: %w", err)
	}
	for _, match := range matches {
		if isDir(match) {
			candidates = append(candidates, match)
		}
	}

	// Legacy streaming location.
	legacy := filepath.Join("/Volumes", "GoogleDrive", "My Drive")
	if isDir(legacy) {
		candidates = append(candidates, legacy)
	}

	// A user/admin configured legacy streaming mount point.
	if mount := googleDriveMountPoint(); mount != "" {
		for _, candidate := range []string{
			mount,
			filepath.Join(mount, "My Drive"),
		} {
			if isDir(candidate) {
				candidates = append(candidates, candidate)
			}
		}
	}

	// Very old local mirror layout.
	legacyMirror := filepath.Join(home, "Google Drive", "My Drive")
	if isDir(legacyMirror) {
		candidates = append(candidates, legacyMirror)
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

	var matching []string
	for _, root := range candidates {
		if isFile(filepath.Join(root, name, "vault.cryptomator")) {
			matching = append(matching, root)
		}
	}
	if len(matching) == 1 {
		return matching[0], nil
	}

	return "", fmt.Errorf("multiple Google Drive storage locations found; edrive cannot choose safely")
}

func (p GoogleDrive) VaultPath(name string) (string, error) {
	root, err := p.Root()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		name = p.StorageName
	}
	if strings.TrimSpace(name) == "" {
		name = "edrive"
	}
	return filepath.Join(root, name), nil
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

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func unique(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
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
