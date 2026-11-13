//go:build !darwin

package platform

// LoadVolumeLabel exists on every platform so startup code is unchanged.
func LoadVolumeLabel() {}

// VolumeLabel has no label source outside macOS.
func VolumeLabel(path string) string { return "" }
