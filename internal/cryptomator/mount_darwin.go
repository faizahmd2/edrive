//go:build darwin

package cryptomator

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Mounted reads the kernel mount table. It never touches the mounted
// filesystem itself, so it cannot hang on a stuck FUSE mount.
func Mounted(path string) bool {
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return false
	}
	buf := make([]unix.Statfs_t, n+4)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return false
	}
	for _, fs := range buf[:n] {
		if unix.ByteSliceToString(fs.Mntonname[:]) == path {
			return true
		}
	}
	return false
}
