package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func cleanFolder(path string) string { return filepath.Clean(path) }

// Selected local folders may be created; a missing NAS destination needs mounting,
// never a replacement local folder under /Volumes.
func ensureSetupFolder(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("choose an absolute destination folder")
	}
	path = filepath.Clean(path)
	parent := path
	var suffix []string
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		suffix = append([]string{filepath.Base(parent)}, suffix...)
		parent = filepath.Dir(parent)
	}
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	real = filepath.Join(append([]string{real}, suffix...)...)
	parts := strings.Split(strings.TrimPrefix(real, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) >= 2 && strings.EqualFold(parts[0], "Volumes") {
		volume := filepath.Join(string(filepath.Separator), parts[0], parts[1])
		if !setupVolumeMounted(volume) {
			return errors.New("the selected volume is not mounted; reconnect it before setup")
		}
		fi, err := os.Stat(real)
		if err != nil || !fi.IsDir() {
			return errors.New("choose an existing folder on the mounted volume; no replacement folder was created")
		}
		return nil
	}
	if err := os.MkdirAll(real, 0o755); err != nil {
		return err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errors.New("the selected destination is not a folder")
	}
	return nil
}
