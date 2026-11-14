package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestSaveLocationRoundTripsAndReplaces(t *testing.T) {
	home := t.TempDir()
	first := filepath.Join(t.TempDir(), "First")
	if err := SaveLocation(home, "Sonarr", 7, first); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "Second")
	if err := SaveLocation(home, "Sonarr", 7, second); err != nil {
		t.Fatal(err)
	}
	if err := SaveLocation(home, "Radarr", 7, first); err != nil {
		t.Fatal(err)
	}
	got := savedLocations(home)
	if got[locationKey("Sonarr", 7)] != second {
		t.Errorf("Sonarr location = %q, want %q", got[locationKey("Sonarr", 7)], second)
	}
	if got[locationKey("Radarr", 7)] != first {
		t.Errorf("Radarr location = %q, want %q", got[locationKey("Radarr", 7)], first)
	}
	info, err := os.Stat(filepath.Join(home, locationsFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("locations mode = %o, want 600", perm)
	}
}

func TestSaveLocationRefusesUnusableChoices(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		app  string
		id   int
		path string
	}{
		{"Prowlarr", 1, "/tmp"},
		{"Sonarr", 0, "/tmp"},
		{"Sonarr", 1, "relative/folder"},
		{"Sonarr", 1, "  "},
	} {
		if err := SaveLocation(home, tc.app, tc.id, tc.path); err == nil {
			t.Errorf("SaveLocation(%q, %d, %q) was accepted", tc.app, tc.id, tc.path)
		}
	}
}

func TestSavedLocationsIgnoresADamagedFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, locationsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := savedLocations(home); len(got) != 0 {
		t.Errorf("damaged file yielded %v", got)
	}
	// A damaged file is replaced cleanly by the next owner choice.
	path := t.TempDir()
	if err := SaveLocation(home, "Sonarr", 3, path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, locationsFile))
	if err != nil || !json.Valid(b) {
		t.Fatalf("locations file is not valid JSON: %s %v", b, err)
	}
}

func TestInventoryUsesTheOwnersChosenFolder(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "Real Folder")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	reported := filepath.Join(root, "App Folder")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 4, "title": "Chosen Show", "path": reported, "statistics": map[string]any{"episodeFileCount": 2}},
			})
		case "/api/v3/history":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 0, "records": []map[string]any{}})
		case "/api/v3/queue":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 0, "records": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	if err := SaveLocation(home, "Sonarr", 4, real); err != nil {
		t.Fatal(err)
	}
	oldExists := pathExists
	pathExists = func(path string) bool { return !strings.Contains(path, "App Folder") }
	t.Cleanup(func() { pathExists = oldExists })

	items, problems := Inventory(config.Config{Sonarr: config.App{URL: srv.URL, APIKey: "k"}}, home, nil)
	if len(problems) != 0 || len(items) != 1 {
		t.Fatalf("items %d, problems %v", len(items), problems)
	}
	item := items[0]
	if !item.LocationManual {
		t.Error("the owner-chosen folder is not marked as such")
	}
	if item.TargetPath != real || item.TargetVolume == "" {
		t.Errorf("location = %q / %q, want the chosen folder", item.TargetPath, item.TargetVolume)
	}
	if item.Status != "on-disk" {
		t.Errorf("status = %q, want on-disk for the chosen folder", item.Status)
	}
}
