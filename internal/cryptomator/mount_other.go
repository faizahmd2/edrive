//go:build !darwin

package cryptomator

func Mounted(string) bool { return false }
