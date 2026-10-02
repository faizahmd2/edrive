package keychain

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	Service          = "edrive"
	DeviceIdentity   = "device-identity"
	RecoveryIdentity = "recovery-identity"
)

func Get(account string) (string, error) {
	if !validAccount(account) {
		return "", fmt.Errorf("invalid edrive Keychain account")
	}

	out, err := exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", Service,
		"-a", account,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("read %s from macOS Keychain: %w", account, err)
	}

	secret := strings.TrimSpace(string(out))
	if secret == "" {
		return "", fmt.Errorf("empty %s in macOS Keychain", account)
	}
	return secret, nil
}

func Set(account, secret string) error {
	if !validAccount(account) {
		return fmt.Errorf("invalid edrive Keychain account")
	}

	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("cannot store empty %s", account)
	}

	cmd := exec.Command(
		"/usr/bin/security",
		"add-generic-password",
		"-U",
		"-s", Service,
		"-a", account,
		"-w", secret,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("store %s in macOS Keychain: %w", account, err)
	}
	return nil
}

func Exists(account string) bool {
	if !validAccount(account) {
		return false
	}
	return exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", Service,
		"-a", account,
	).Run() == nil
}

func Delete(account string) error {
	if !validAccount(account) {
		return fmt.Errorf("invalid edrive Keychain account")
	}

	cmd := exec.Command(
		"/usr/bin/security",
		"delete-generic-password",
		"-s", Service,
		"-a", account,
	)
	if err := cmd.Run(); err != nil && !strings.Contains(err.Error(), "could not be found in the keychain") {
		return fmt.Errorf("delete %s from macOS Keychain: %w", account, err)
	}
	return nil
}

func validAccount(account string) bool {
	return account == DeviceIdentity || account == RecoveryIdentity
}
