package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyCleanupCannotBeReenabled(t *testing.T) {
	if Default().CleanupOn {
		t.Fatal("cleanup must start off")
	}
	c := Default()
	c.CleanupOn = true
	if c.Validate() == nil {
		t.Fatal("legacy cleanup was accepted")
	}
	home := t.TempDir()
	original := []byte(`{"cleanupOn":true,"cleanupDays":7,"tailscalePort":7882}`)
	if err := os.WriteFile(filepath.Join(home, "settings.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().CleanupOn {
		t.Fatal("legacy true flag stayed active on load")
	}
	if err := store.Save(c); err == nil {
		t.Fatal("legacy true flag persisted")
	}
}
