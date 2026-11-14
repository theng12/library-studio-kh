//go:build windows

package platform

import "os/exec"

// Reveal shows a file or folder in Explorer.
func Reveal(path string) error {
	return exec.Command("explorer", "/select,"+path).Start()
}
