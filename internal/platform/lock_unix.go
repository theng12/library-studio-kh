//go:build unix

package platform

import (
	"fmt"
	"os"
	"syscall"
)

func acquireFileLock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("lock is held: %w", err)
	}
	return nil
}

func releaseFileLock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
