//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrashDirIsTheUsersOwnFolder(t *testing.T) {
	dir, err := TrashDir()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(home, ".Trash") {
		t.Fatalf("TrashDir returned %q, want this user's .Trash", dir)
	}
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("TrashDir must be a real folder: %v", err)
	}
}

func TestLocalVolumeOnATempFolder(t *testing.T) {
	ok, err := LocalVolume(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("a temporary folder should be on a local volume")
	}
}
