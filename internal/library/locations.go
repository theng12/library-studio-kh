package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Owner-chosen locations live beside the import ledger: when a library app's
// own path is wrong or absent, the owner can point Library at the real folder.
const locationsFile = "locations.json"

// LocationOverride is one owner-chosen folder for a library title.
type LocationOverride struct {
	App  string `json:"app"`
	ID   int    `json:"id"`
	Path string `json:"path"`
}

var locationsMu sync.Mutex

func locationKey(app string, id int) string { return proofKey(app, id) }

// savedLocations reads the owner's chosen folders, keyed by app and media ID.
// A missing or damaged file yields no overrides rather than a wrong path.
func savedLocations(home string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(home, locationsFile))
	if err != nil {
		return out
	}
	var file struct {
		Locations []LocationOverride `json:"locations"`
	}
	if json.Unmarshal(b, &file) != nil {
		return out
	}
	for _, loc := range file.Locations {
		if strings.TrimSpace(loc.Path) != "" {
			out[locationKey(loc.App, loc.ID)] = strings.TrimSpace(loc.Path)
		}
	}
	return out
}

// SaveLocation records the owner's folder for one title and writes it at once.
func SaveLocation(home, app string, id int, path string) error {
	app, path = strings.TrimSpace(app), strings.TrimSpace(path)
	if app != "Sonarr" && app != "Radarr" && app != "Lidarr" {
		return errors.New("choose a show, movie or artist")
	}
	if id <= 0 || !filepath.IsAbs(path) {
		return errors.New("choose a full folder path")
	}
	locationsMu.Lock()
	defer locationsMu.Unlock()

	var file struct {
		Locations []LocationOverride `json:"locations"`
	}
	if b, err := os.ReadFile(filepath.Join(home, locationsFile)); err == nil {
		_ = json.Unmarshal(b, &file)
	}
	replaced := false
	for i := range file.Locations {
		if file.Locations[i].App == app && file.Locations[i].ID == id {
			file.Locations[i].Path = path
			replaced = true
			break
		}
	}
	if !replaced {
		file.Locations = append(file.Locations, LocationOverride{App: app, ID: id, Path: path})
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(home, locationsFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(home, locationsFile))
}
