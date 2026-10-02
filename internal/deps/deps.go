package deps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/faizahmd2/edrive/internal/config"
	"github.com/faizahmd2/edrive/internal/toolchain"
)

type Paths struct {
	Age            string
	Zstd           string
	Rclone         string
	CryptomatorCLI string
}

func Ensure() (Paths, error) {
	if runtime.GOOS != "darwin" {
		return Paths{}, fmt.Errorf("dependency installation currently supports macOS")
	}
	if _, err := exec.LookPath("brew"); err != nil {
		return Paths{}, fmt.Errorf("Homebrew is required to install edrive dependencies")
	}

	for _, name := range []string{"rclone", "age", "zstd"} {
		if err := ensureFormula(name); err != nil {
			return Paths{}, err
		}
	}
	if !FUSEInstalled() {
		fmt.Println("Installing FUSE-T...")
		if err := runBrew("install", "--cask", "fuse-t"); err != nil {
			return Paths{}, err
		}
	}
	if !CryptomatorInstalled() {
		fmt.Println("Installing Cryptomator...")
		if err := runBrew("install", "--cask", "cryptomator"); err != nil {
			return Paths{}, err
		}
	}

	cli, err := EnsureCryptomatorCLI("")
	if err != nil {
		return Paths{}, err
	}

	return Paths{
		Age:            mustLookPath("age"),
		Zstd:           mustLookPath("zstd"),
		Rclone:         mustLookPath("rclone"),
		CryptomatorCLI: cli,
	}, nil
}

func EnsureCryptomatorCLI(configured string) (string, error) {
	for _, candidate := range unique([]string{
		configured,
		filepath.Join(config.ToolsDir(), "cryptomator-cli", toolchain.CryptomatorCLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
		homePath("Desktop/local-infra/tools/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
	}) {
		if isExecutable(candidate) && versionMatches(candidate, toolchain.CryptomatorCLIVersion) {
			return filepath.Clean(candidate), nil
		}
	}
	if path, err := exec.LookPath("cryptomator-cli"); err == nil && versionMatches(path, toolchain.CryptomatorCLIVersion) {
		return filepath.Clean(path), nil
	}
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("Cryptomator CLI %s is required", toolchain.CryptomatorCLIVersion)
	}
	return downloadCryptomatorCLI()
}

func FUSEInstalled() bool {
	return exec.Command("brew", "list", "--cask", "fuse-t").Run() == nil
}

func CryptomatorInstalled() bool {
	return isDir("/Applications/Cryptomator.app")
}

func UninstallFormula(name string) error {
	if !formulaInstalled(name) {
		fmt.Printf("%s is not installed through Homebrew.\n", name)
		return nil
	}
	return runBrew("uninstall", name)
}

func UninstallCask(name string) error {
	if !caskInstalled(name) {
		fmt.Printf("%s is not installed through Homebrew.\n", name)
		return nil
	}
	return runBrew("uninstall", "--cask", name)
}

func RemoveManagedCryptomatorCLI() error {
	root := filepath.Join(config.ToolsDir(), "cryptomator-cli")
	if _, err := os.Stat(root); err == nil {
		return os.RemoveAll(root)
	}
	return nil
}

func FindCryptomatorCLI(configured string) string {
	for _, candidate := range unique([]string{
		configured,
		filepath.Join(config.ToolsDir(), "cryptomator-cli", toolchain.CryptomatorCLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli"),
		"/Applications/cryptomator-cli.app/Contents/MacOS/cryptomator-cli",
		homePath("Desktop/local-infra/tools/cryptomator-cli.app/Contents/MacOS/cryptomator-cli"),
	}) {
		if isExecutable(candidate) && versionMatches(candidate, toolchain.CryptomatorCLIVersion) {
			return filepath.Clean(candidate)
		}
	}
	if path, err := exec.LookPath("cryptomator-cli"); err == nil && versionMatches(path, toolchain.CryptomatorCLIVersion) {
		return filepath.Clean(path)
	}
	return ""
}

func FormulaInstalled(name string) bool {
	return formulaInstalled(name)
}

func CaskInstalled(name string) bool {
	return caskInstalled(name)
}

func ensureFormula(name string) error {
	if formulaInstalled(name) {
		return nil
	}
	fmt.Printf("Installing %s...\n", name)
	return runBrew("install", name)
}

func formulaInstalled(name string) bool {
	return exec.Command("brew", "list", "--formula", name).Run() == nil
}

func caskInstalled(name string) bool {
	return exec.Command("brew", "list", "--cask", name).Run() == nil
}

func runBrew(args ...string) error {
	cmd := exec.Command("brew", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew %s failed", strings.Join(args, " "))
	}
	return nil
}

func downloadCryptomatorCLI() (string, error) {
	if runtime.GOARCH != "arm64" {
		return "", fmt.Errorf("automatic Cryptomator CLI installation currently supports Apple Silicon macOS")
	}

	root := filepath.Join(config.ToolsDir(), "cryptomator-cli", toolchain.CryptomatorCLIVersion)
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", fmt.Errorf("create Cryptomator CLI directory: %w", err)
	}

	zipPath := filepath.Join(root, "cryptomator-cli.zip")
	url := "https://github.com/cryptomator/cli/releases/download/" + toolchain.CryptomatorCLIVersion + "/cryptomator-cli-" + toolchain.CryptomatorCLIVersion + "-mac-arm64.zip"

	fmt.Println("Downloading Cryptomator CLI", toolchain.CryptomatorCLIVersion, "...")
	if err := exec.Command("curl", "-fL", "--retry", "3", "--output", zipPath, url).Run(); err != nil {
		_ = os.Remove(zipPath)
		return "", fmt.Errorf("download Cryptomator CLI %s", toolchain.CryptomatorCLIVersion)
	}
	if err := exec.Command("unzip", "-q", "-o", zipPath, "-d", root).Run(); err != nil {
		_ = os.Remove(zipPath)
		return "", fmt.Errorf("extract Cryptomator CLI %s", toolchain.CryptomatorCLIVersion)
	}
	_ = os.Remove(zipPath)

	appPath := filepath.Join(root, "cryptomator-cli.app")
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", appPath).Run()

	path := filepath.Join(appPath, "Contents", "MacOS", "cryptomator-cli")
	if !isExecutable(path) || !versionMatches(path, toolchain.CryptomatorCLIVersion) {
		return "", fmt.Errorf("downloaded Cryptomator CLI %s is invalid", toolchain.CryptomatorCLIVersion)
	}
	return path, nil
}

func versionMatches(path, version string) bool {
	out, err := exec.Command(path, "--version").CombinedOutput()
	return err == nil && strings.Contains(string(out), version)
}

func mustLookPath(name string) string {
	path, _ := exec.LookPath(name)
	return path
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func homePath(relative string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, relative)
}

func unique(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		path = filepath.Clean(path)
		if path == "." || path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}
