package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileAcrossReadBuffers(t *testing.T) {
	data := append(bytes.Repeat([]byte("verified"), 1<<20), []byte("partial final buffer")...)
	path := filepath.Join(t.TempDir(), "large-copy")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	got, err := hashFile(path)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("full-file checksum changed across buffer boundaries: %s, %v", got, err)
	}
}

func trashRemovalFiles(t *testing.T, path string) []CleanupFile {
	t.Helper()
	tree, err := observedTree(path)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]CleanupFile, 0, len(tree))
	for _, file := range tree {
		files = append(files, file)
	}
	return files
}

func TestRemoveVerifiedCopyWithoutReadingTrashParent(t *testing.T) {
	trash := filepath.Join(t.TempDir(), "Trash")
	owned := filepath.Join(trash, "library-studio-owned")
	if err := os.MkdirAll(filepath.Join(owned, "season", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "season", "episode.mkv"), []byte("verified video"), 0600); err != nil {
		t.Fatal(err)
	}
	files := trashRemovalFiles(t, owned)
	if err := os.Chmod(trash, 0300); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(trash, 0700)
	if _, err := os.ReadDir(trash); err == nil {
		t.Skip("directory read permissions are not enforced for this user/platform")
	}
	if err := os.RemoveAll(owned); !os.IsPermission(err) {
		t.Fatalf("expected the legacy parent-read failure, got %v", err)
	}
	if err := removeVerifiedCopy(owned, files, func() error { return nil }); err != nil {
		t.Fatalf("owned folder removal required access to its parent: %v", err)
	}
	if _, err := os.Lstat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned folder remains: %v", err)
	}
}

func TestRemoveVerifiedCopyRequiresFreshImportProof(t *testing.T) {
	for _, directory := range []bool{false, true} {
		owned := filepath.Join(t.TempDir(), "owned")
		file := owned
		if directory {
			if err := os.Mkdir(owned, 0700); err != nil {
				t.Fatal(err)
			}
			file = filepath.Join(owned, "episode.mkv")
		}
		if err := os.WriteFile(file, []byte("verified"), 0600); err != nil {
			t.Fatal(err)
		}
		called := false
		missing := errors.New("NAS copy disappeared")
		err := removeVerifiedCopy(owned, trashRemovalFiles(t, owned), func() error {
			called = true
			return missing
		})
		if !called || !errors.Is(err, missing) {
			t.Fatalf("fresh proof was not required: called=%v, err=%v", called, err)
		}
		if data, err := os.ReadFile(file); err != nil || string(data) != "verified" {
			t.Fatalf("local copy changed after failed NAS proof: %q, %v", data, err)
		}
	}
}

func TestRemoveVerifiedCopyProtectsChangesDuringNASProof(t *testing.T) {
	for _, change := range []string{"single file", "nested file", "added file"} {
		t.Run(change, func(t *testing.T) {
			owned := filepath.Join(t.TempDir(), "owned")
			file := owned
			if change != "single file" {
				file = filepath.Join(owned, "season", "episode.mkv")
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(file, []byte("verified"), 0600); err != nil {
				t.Fatal(err)
			}
			files := trashRemovalFiles(t, owned)
			err := removeVerifiedCopy(owned, files, func() error {
				path := file
				if change == "added file" {
					path = filepath.Join(filepath.Dir(file), "untracked")
				}
				return os.WriteFile(path, []byte("new local data"), 0600)
			})
			if err == nil {
				t.Fatal("local changes during NAS verification were accepted")
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("local copy was removed: %v", err)
			}
		})
	}
}

func TestRemoveVerifiedCopyFilesAndNestedFolders(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "nested folder"
		}
		t.Run(name, func(t *testing.T) {
			owned := filepath.Join(t.TempDir(), "owned")
			file := owned
			if directory {
				file = filepath.Join(owned, "season", "episode.mkv")
				if err := os.MkdirAll(filepath.Join(owned, "empty"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(file, []byte("verified video"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := removeVerifiedCopy(owned, trashRemovalFiles(t, owned), func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(owned); !os.IsNotExist(err) {
				t.Fatalf("owned copy remains: %v", err)
			}
		})
	}
}

func TestRemoveVerifiedCopyProtectsChangesAndSymlinks(t *testing.T) {
	for _, change := range []string{"extra file", "changed content", "symlink", "traversal"} {
		t.Run(change, func(t *testing.T) {
			base := t.TempDir()
			owned := filepath.Join(base, "owned")
			if err := os.Mkdir(owned, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(owned, "episode.mkv")
			if err := os.WriteFile(file, []byte("verified"), 0600); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside")
			if err := os.WriteFile(outside, []byte("protected"), 0600); err != nil {
				t.Fatal(err)
			}
			files := trashRemovalFiles(t, owned)
			switch change {
			case "extra file":
				if err := os.WriteFile(filepath.Join(owned, "untracked"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed content":
				if err := os.WriteFile(file, []byte("modified"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := filepath.Join(base, "link")
				if err := os.Symlink(owned, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				owned = link
			case "traversal":
				files[0].Rel = "../outside"
			}
			if err := removeVerifiedCopy(owned, files, func() error { return nil }); err == nil {
				t.Fatal("unsafe or changed copy was accepted")
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("protected local file changed: %v", err)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "protected" {
				t.Fatalf("outside file changed: %q, %v", data, err)
			}
		})
	}
}
