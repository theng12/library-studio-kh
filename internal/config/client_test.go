package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientHostNeverPersistsBootstrapOrLoginShortcuts(t *testing.T) {
	for _, raw := range []string{"https://example.test/#token", "https://example.test/?token=secret", "https://user:secret@example.test", "https://example.test/settings", "file:///tmp/app", "http://0.0.0.0:7881", "http://[::]:7881"} {
		if _, err := SaveClientHost(t.TempDir(), raw); err == nil {
			t.Fatal("unsafe client URL accepted")
		}
	}
	home := t.TempDir()
	got, err := SaveClientHost(home, "https://library.example.test/")
	if err != nil || got != "https://library.example.test" {
		t.Fatal("normal webpage address was not saved")
	}
	if got, err := LoadClientHost(home); err != nil || got != "https://library.example.test" {
		t.Fatal("saved host did not round trip")
	}
	if _, err := SaveClientHost(home, "http://other.example.test:7882"); err != nil {
		t.Fatal(err)
	}
	if err := ClearClientHost(home); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadClientHost(home); err != nil || got != "" {
		t.Fatal("clear did not return to local mode")
	}
	old, _ := filepath.Glob(filepath.Join(home, "retired-client-settings", "*.json"))
	if len(old) != 2 {
		t.Fatal("previous client choices were not retained")
	}
	if err := ClearClientHost(home); err != nil {
		t.Fatal(err)
	}
}

func TestClientSettingsRejectSymlinksAndInvalidPrivateState(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "client.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClientHost(home); err == nil {
		t.Fatal("followed client settings symlink")
	}
	if _, err := SaveClientHost(home, "https://library.example.test"); err == nil {
		t.Fatal("replaced symlink")
	}
	if err := ClearClientHost(home); err == nil {
		t.Fatal("retired a foreign symlink")
	}
	data, _ := os.ReadFile(outside)
	if string(data) != "untouched" {
		t.Fatal("changed linked file")
	}
}

func TestSetupCompareAndSavePreservesConcurrentOwnerEdit(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before := s.Get()
	edited := before
	edited.DownloadPath = "/owner-choice"
	if err := s.Save(edited); err != nil {
		t.Fatal(err)
	}
	proposed := before
	proposed.DownloadPath = "/setup-choice"
	if err := s.SaveIfUnchanged(before, proposed); err == nil {
		t.Fatal("setup overwrote concurrent owner edit")
	}
	if s.Get() != edited {
		t.Fatal("owner settings were changed")
	}
	if err := s.SaveIfUnchanged(edited, proposed); err != nil {
		t.Fatal(err)
	}
}
