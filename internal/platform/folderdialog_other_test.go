//go:build !darwin

package platform

import (
	"errors"
	"testing"
)

func TestPickFolderUnsupported(t *testing.T) {
	StartFolderUI()
	StartFolderUI() // idempotent
	path, err := PickFolder("Choose a folder")
	if path != "" {
		t.Fatalf("unsupported platform returned a path: %q", path)
	}
	if !errors.Is(err, ErrFolderDialogUnsupported) {
		t.Fatalf("want ErrFolderDialogUnsupported, got %v", err)
	}
}
