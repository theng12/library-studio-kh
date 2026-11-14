//go:build !darwin && !windows

package platform

import (
	"os/exec"
	"path/filepath"
)

// Reveal opens the file's folder with the desktop's own handler.
func Reveal(path string) error {
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}
