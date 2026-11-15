package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedNonDefaults writes settings clearly away from every default a release
// checks, then returns exactly what was written.
func seedNonDefaults(t *testing.T, s *Store) Config {
	t.Helper()
	c := s.Get()
	c.Appearance = "dark"
	c.TailscaleOn, c.TailscalePort = true, 7999
	c.StagedCleanupOn, c.StagedDays = true, 7
	c.ManagedTrashDays, c.NativeTrashDays = 9, 45
	c.PressureFreeGB = 120
	c.SeedRatio, c.SeedMinutes = 1.5, 900
	c.DeadCleanupOn, c.DeadCleanupHours = true, 48
	c.DownloadPath, c.LibraryPath = "/survive-update/downloads", "/survive-update/library"
	c.Sonarr = App{URL: "http://127.0.0.1:8989", APIKey: "settings-survive-key"}
	if err := s.Save(c); err != nil {
		t.Fatalf("save non-default settings: %v", err)
	}
	return c
}

// openHome opens settings the way the app does on startup: config.Dir() (with
// LIBRARY_STUDIO_HOME pointing at the temp home) then Open.
func openHome(t *testing.T) (*Store, string) {
	t.Helper()
	t.Setenv("LIBRARY_STUDIO_HOME", t.TempDir())
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// Owner settings must read back exactly as written after the app reopens its
// file — an update installs a new build over the same home.
func TestSettingsSurviveReopen(t *testing.T) {
	s, dir := openHome(t)
	want := seedNonDefaults(t, s)

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get(); got != want {
		t.Errorf("settings after reopen =\n  %+v\nwant\n  %+v", got, want)
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Opening reads the file; it must never rewrite or reset it.
func TestOpenDoesNotRewriteSettingsFile(t *testing.T) {
	s, dir := openHome(t)
	seedNonDefaults(t, s)
	path := filepath.Join(dir, "settings.json")
	want := hashFile(t, path)
	for i := 1; i <= 2; i++ {
		if _, err := Open(dir); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if got := hashFile(t, path); got != want {
			t.Fatalf("open %d rewrote settings.json: hash %s, want %s", i, got, want)
		}
	}
}

// The Settings form posts a partial payload and saveSettings merges it over the
// saved config (c := old); every field the payload omits must stay put.
func TestPartialSaveKeepsOmittedFields(t *testing.T) {
	s, dir := openHome(t)
	want := seedNonDefaults(t, s)

	// The shape server.saveSettings decodes: top-level scalars plus a nested app.
	payload := `{"appearance":"light","stagedDays":14,"sonarr":{"url":"http://127.0.0.1:18989"}}`
	c := s.Get()
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(c); err != nil {
		t.Fatalf("save partial settings: %v", err)
	}
	want.Appearance, want.StagedDays = "light", 14
	want.Sonarr.URL = "http://127.0.0.1:18989"

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get(); got != want {
		t.Errorf("settings after partial save =\n  %+v\nwant\n  %+v", got, want)
	}
}
