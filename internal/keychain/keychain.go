package keychain

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Get(service, account string) ([]byte, error) {
	if service == "" || account == "" {
		return nil, fmt.Errorf("keychain service and account are required")
	}

	if _, err := os.Stat("/usr/bin/security"); err != nil {
		return nil, fmt.Errorf("macOS security tool not found: %w", err)
	}

	cmd := exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf(
			"keychain item not found for account %s: %w",
			account,
			err,
		)
	}

	return []byte(strings.TrimRight(string(out), "\r\n")), nil
}
