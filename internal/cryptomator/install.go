package cryptomator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Pinned Cryptomator CLI release. 0.6.1 is the newest release with a native
// Apple Silicon build (0.6.2 ships Intel-only, which runs slowly under Rosetta).
const (
	CLIVersion = "0.6.1"
	cliSHA256  = "9252f8765db5068931d78f1ee74f7fe4d653605bf691d30e05c119eb5fa5d0d3"
	cliURL     = "https://github.com/cryptomator/cli/releases/download/" + CLIVersion + "/cryptomator-cli-" + CLIVersion + "-mac-arm64.zip"
	// Skymatic GmbH, the company behind Cryptomator.
	cliTeamID = "YZQJQUHA3L"
)

// CLIPath returns the Cryptomator CLI edrive uses. EDRIVE_CRYPTOMATOR_CLI
// overrides the managed copy for people who install it themselves.
func CLIPath(toolsDir string) string {
	if p := strings.TrimSpace(os.Getenv("EDRIVE_CRYPTOMATOR_CLI")); p != "" {
		return p
	}
	return filepath.Join(toolsDir, "cryptomator-cli", CLIVersion, "cryptomator-cli.app", "Contents", "MacOS", "cryptomator-cli")
}

func CLIInstalled(toolsDir string) bool {
	return isExecutable(CLIPath(toolsDir))
}

// InstallCLI downloads the pinned CLI, checks its SHA-256 and code signature,
// and unpacks it into toolsDir. It does nothing if the CLI is already there.
func InstallCLI(toolsDir string) error {
	if CLIInstalled(toolsDir) {
		return nil
	}
	if runtime.GOARCH != "arm64" {
		return fmt.Errorf("automatic Cryptomator CLI install supports Apple Silicon; set EDRIVE_CRYPTOMATOR_CLI to your own cryptomator-cli")
	}

	root := filepath.Join(toolsDir, "cryptomator-cli", CLIVersion)
	if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(root), ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	fmt.Printf("Downloading Cryptomator CLI %s...\n", CLIVersion)
	zipPath := filepath.Join(staging, "cli.zip")
	sum, err := download(cliURL, zipPath)
	if err != nil {
		return fmt.Errorf("download Cryptomator CLI: %w", err)
	}
	if sum != cliSHA256 {
		return fmt.Errorf("Cryptomator CLI download failed its checksum (got %s); not installing it", sum)
	}

	extract := filepath.Join(staging, "out")
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", zipPath, extract).CombinedOutput(); err != nil {
		return fmt.Errorf("extract Cryptomator CLI: %s", strings.TrimSpace(string(out)))
	}
	app := filepath.Join(extract, "cryptomator-cli.app")
	if err := verifySignature(app); err != nil {
		return err
	}

	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err := os.Rename(app, filepath.Join(root, "cryptomator-cli.app")); err != nil {
		return err
	}
	if !CLIInstalled(toolsDir) {
		return fmt.Errorf("Cryptomator CLI was installed but is not executable")
	}
	return nil
}

func download(url, dest string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %s", resp.Status)
			continue
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			resp.Body.Close()
			return "", err
		}
		h := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
		resp.Body.Close()
		closeErr := f.Close()
		if copyErr != nil {
			lastErr = copyErr
			continue
		}
		if closeErr != nil {
			return "", closeErr
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	return "", lastErr
}

func verifySignature(app string) error {
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		return fmt.Errorf("Cryptomator CLI signature is invalid: %s", strings.TrimSpace(string(out)))
	}
	out, _ := exec.Command("/usr/bin/codesign", "-dv", app).CombinedOutput()
	if !strings.Contains(string(out), "TeamIdentifier="+cliTeamID) {
		return fmt.Errorf("Cryptomator CLI is not signed by Cryptomator (team %s)", cliTeamID)
	}
	return nil
}
