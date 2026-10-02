package util

import (
	"fmt"
	"os/exec"
	"strings"
)

func RequireBinary(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command %q not found in PATH", name)
	}
	return nil
}

func Version(name string) string {
	cmd := exec.Command(name, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(out))
}
