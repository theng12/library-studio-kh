//go:build darwin

package platform

import "os/exec"

// Reveal shows a file or folder in Finder. The owner clicked Open location, so
// bringing Finder forward is the requested effect, not a surprise.
func Reveal(path string) error {
	return exec.Command("open", "-R", path).Start()
}
