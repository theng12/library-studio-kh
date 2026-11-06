//go:build !darwin

package setup

func setupVolumeMounted(path string) bool { return false }
