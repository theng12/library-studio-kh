//go:build darwin

package setup

import (
	"path/filepath"
	"strings"
	"syscall"
)

func setupVolumeMounted(path string) bool {
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) != nil {
		return false
	}
	var name []byte
	for _, ch := range fs.Mntonname {
		if ch == 0 {
			break
		}
		name = append(name, byte(ch))
	}
	return strings.EqualFold(filepath.Clean(string(name)), filepath.Clean(path))
}
