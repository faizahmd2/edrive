//go:build !darwin || !cgo

package macos

import "errors"

var ErrNotFound = errors.New("keychain item not found")

var ErrCancelled = errors.New("authentication cancelled")

var errUnsupported = errors.New("edrive requires macOS (built with cgo)")

func Authenticate(string) error { return errUnsupported }

func CanAuthenticate() bool { return false }

func KeychainGet(string, string) ([]byte, error) { return nil, errUnsupported }

func KeychainExists(string, string) bool { return false }

func KeychainSet(string, string, string, []byte) error { return errUnsupported }

func KeychainDelete(string, string) error { return errUnsupported }

func ScreenLocked() bool { return false }

func ClipboardSet(string) int64 { return 0 }

func ClipboardClearIf(int64) {}
