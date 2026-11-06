//go:build !darwin

package platform

// TrashDir is explicit: no native-Trash helper has been verified on this
// system, so the engine reports unsupported rather than pretending.
func TrashDir() (string, error) { return "", ErrTrashUnsupported }
