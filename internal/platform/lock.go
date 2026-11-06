package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockConfigHome keeps one real server per configuration home. The OS releases
// the advisory lock when the process exits, so a crash cannot leave it held.
func LockConfigHome(home string) (func(), error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, "host.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := acquireFileLock(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Library Studio KH host is already using %s; quit it first", home)
	}
	return func() {
		releaseFileLock(f)
		f.Close()
	}, nil
}
