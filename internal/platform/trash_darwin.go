//go:build darwin

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// TrashDir is the current user's own Trash folder. It never returns the
// system-wide or another user's Trash.
func TrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".Trash")
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("the system Trash folder %s could not be read: %w", dir, err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s is not a real folder", dir)
	}
	return dir, nil
}

// LocalVolume reports whether path sits on a locally attached filesystem.
// Unknown filesystem types return false: the staged policy never treats an
// unproven mount as local.
func LocalVolume(path string) (bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, err
	}
	name := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	switch strings.ToLower(string(name)) {
	case "apfs", "hfs", "exfat", "msdos", "ntfs", "ufs", "lifs":
		return true, nil
	default:
		return false, nil
	}
}
