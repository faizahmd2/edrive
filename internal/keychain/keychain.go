package keychain

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const Service = "edrive"

const recoveryAccount = "recovery"

func IdentityAccount(label string) string {
	return "identity:" + label
}

func GetIdentity(label string) (string, error) {
	return get(IdentityAccount(label))
}

func SetIdentity(label, secret string) error {
	return set(IdentityAccount(label), secret)
}

func IdentityExists(label string) bool {
	return exists(IdentityAccount(label))
}

func DeleteIdentity(label string) error {
	return remove(IdentityAccount(label))
}

func GetRecovery() (string, error) {
	return get(recoveryAccount)
}

func SetRecovery(secret string) error {
	return set(recoveryAccount, secret)
}

func RecoveryExists() bool {
	return exists(recoveryAccount)
}

func DeleteRecovery() error {
	return remove(recoveryAccount)
}

func UnlockDefault() error {
	cmd := exec.Command("/usr/bin/security", "unlock-keychain")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unlock default macOS Keychain: %w", err)
	}
	return nil
}

func get(account string) (string, error) {
	out, err := exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", Service,
		"-a", account,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("read edrive Keychain item %q: %w", account, err)
	}
	secret := strings.TrimSpace(string(out))
	if secret == "" {
		return "", fmt.Errorf("edrive Keychain item %q is empty", account)
	}
	return secret, nil
}

func set(account, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("cannot store empty edrive Keychain item %q", account)
	}
	if err := exec.Command(
		"/usr/bin/security",
		"add-generic-password",
		"-U",
		"-s", Service,
		"-a", account,
		"-w", secret,
	).Run(); err != nil {
		return fmt.Errorf("store edrive Keychain item %q: %w", account, err)
	}
	return nil
}

func exists(account string) bool {
	return exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", Service,
		"-a", account,
	).Run() == nil
}

func remove(account string) error {
	if !exists(account) {
		return nil
	}
	if err := exec.Command(
		"/usr/bin/security",
		"delete-generic-password",
		"-s", Service,
		"-a", account,
	).Run(); err != nil {
		return fmt.Errorf("delete edrive Keychain item %q: %w", account, err)
	}
	return nil
}

func LegacyIdentityExists(account string) bool {
	return legacyExists(account)
}

func LegacyRecoveryExists() bool {
	return legacyExists("recovery-identity")
}

func GetLegacyIdentity(account string) (string, error) {
	return getLegacy(account)
}

func DeleteLegacyIdentity(account string) error {
	return removeLegacy(account)
}

func GetLegacyRecovery() (string, error) {
	return getLegacy("recovery-identity")
}

func DeleteLegacyRecovery() error {
	return removeLegacy("recovery-identity")
}

func getLegacy(account string) (string, error) {
	out, err := exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", Service,
		"-a", account,
		"-w",
	).Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("legacy Keychain item %q is empty", account)
	}
	return value, nil
}

func removeLegacy(account string) error {
	if err := exec.Command(
		"/usr/bin/security",
		"delete-generic-password",
		"-s", Service,
		"-a", account,
	).Run(); err != nil {
		return err
	}
	return nil
}

func legacyExists(account string) bool {
	return exec.Command(
		"/usr/bin/security",
		"find-generic-password",
		"-s", Service,
		"-a", account,
	).Run() == nil
}
