package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Secrets live only in the app-data settings file: private mode, atomic write,
// no temporary file left behind.
func TestSettingsFileIsPrivateAndAtomic(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := Default()
	c.QBit.Username = "owner"
	c.QBit.Password = "s3cret"
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings.json mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("a temporary settings file was left behind: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "s3cret") {
		t.Fatal("the secret was not saved to the app-data file")
	}
}
