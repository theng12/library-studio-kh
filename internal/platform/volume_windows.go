//go:build windows

package platform

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var getDriveType = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW")

// LocalVolume reports whether path sits on a fixed or removable drive. Network
// and unknown drives return false.
func LocalVolume(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return false, nil
	}
	p, err := syscall.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false, err
	}
	r, _, e := getDriveType.Call(uintptr(unsafe.Pointer(p)))
	if r == 0 {
		return false, e
	}
	return r == 2 || r == 3, nil // DRIVE_REMOVABLE, DRIVE_FIXED
}
