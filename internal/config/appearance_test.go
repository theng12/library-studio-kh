package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppearanceChoices(t *testing.T) {
	if got := Default().Appearance; got != "system" {
		t.Fatalf("default appearance = %q, want system", got)
	}
	for _, choice := range []string{"", "system", "light", "dark"} {
		c := Default()
		c.Appearance = choice
		if err := c.Validate(); err != nil {
			t.Fatalf("appearance %q rejected: %v", choice, err)
		}
	}
	for _, choice := range []string{"sepia", "default"} {
		c := Default()
		c.Appearance = choice
		if err := c.Validate(); err == nil {
			t.Fatalf("appearance %q was accepted", choice)
		}
	}
}

// A settings file saved when "Default" existed must read back as System.
func TestStoredDefaultAppearanceMigratesToSystem(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"appearance":"default"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Get().Appearance; got != "system" {
		t.Fatalf("stored default read back as %q, want system", got)
	}
}
