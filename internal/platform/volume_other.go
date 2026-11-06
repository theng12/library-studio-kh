//go:build !darwin && !linux && !windows

package platform

// LocalVolume is explicit on unverified systems: nothing is assumed local.
func LocalVolume(path string) (bool, error) { return false, ErrTrashUnsupported }
