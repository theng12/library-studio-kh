//go:build !windows

// Package platform is the only place with operating-system specific code.
package platform

import "syscall"

// FreeBytes is the space the current user can still write at path.
func FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
