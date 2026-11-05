package stock

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Media is one show, film or artist as the library apps describe it.
type Media struct {
	App       string          `json:"app"`
	Title     string          `json:"title"`
	Year      int             `json:"year,omitempty"`
	Added     *time.Time      `json:"added,omitempty"`
	Status    string          `json:"status,omitempty"`
	Overview  string          `json:"overview,omitempty"`
	HaveFiles bool            `json:"haveFiles"`
	InLibrary bool            `json:"inLibrary"`
	Raw       json.RawMessage `json:"raw,omitempty"` // lookup result, sent back unchanged to add it
}

// Each app keeps its own kind of item: Sonarr series, Radarr movies, Lidarr artists.
var kinds = map[string]string{"Sonarr": "series", "Radarr": "movie", "Lidarr": "artist"}

type rawMedia struct {
	ID         int
	Title      string
	ArtistName string
	Year       int
	Added      *time.Time
	Status     string
	Overview   string
	HasFile    bool
	Statistics *struct{ EpisodeFileCount, TrackFileCount int }
}

func (r rawMedia) media(app string) Media {
	m := Media{App: app, Title: r.Title, Year: r.Year, Status: r.Status, Overview: r.Overview, InLibrary: r.ID > 0, HaveFiles: r.HasFile}
	if r.ArtistName != "" {
		m.Title = r.ArtistName
	}
	if r.Added != nil && r.Added.Year() > 1 { // lookups report 0001-01-01
		m.Added = r.Added
	}
	if s := r.Statistics; s != nil && s.EpisodeFileCount+s.TrackFileCount > 0 {
		m.HaveFiles = true
	}
	return m
}

func (a *Arr) list(path string, keepRaw bool) ([]Media, error) {
	var raws []json.RawMessage
	if err := a.do("GET", path, nil, &raws); err != nil {
		return nil, err
	}
	out := make([]Media, 0, len(raws))
	for _, b := range raws {
		var r rawMedia
		if json.Unmarshal(b, &r) != nil {
			continue
		}
		m := r.media(a.Name)
		if keepRaw && !m.InLibrary {
			m.Raw = b
		}
		out = append(out, m)
	}
	return out, nil
}

// Library is everything the app already looks after.
func (a *Arr) Library() ([]Media, error) { return a.list("/"+kinds[a.Name], false) }

// Lookup searches the app's own metadata source for new things to add.
func (a *Arr) Lookup(term string) ([]Media, error) {
	return a.list("/"+kinds[a.Name]+"/lookup?term="+url.QueryEscape(term), true)
}

// Add puts a lookup result into the library with the app's first quality profile and root folder,
// watched for new releases, and asks the app to search for it now.
func (a *Arr) Add(raw json.RawMessage) (string, error) {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", errors.New("that result could not be read")
	}
	first := func(path string) (map[string]any, error) {
		var xs []map[string]any
		if err := a.do("GET", path, nil, &xs); err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fmt.Errorf("%s has no %s set up yet", a.Name, path[1:])
		}
		return xs[0], nil
	}
	q, err := first("/qualityprofile")
	if err != nil {
		return "", err
	}
	root, err := first("/rootfolder")
	if err != nil {
		return "", err
	}
	item["qualityProfileId"], item["rootFolderPath"], item["monitored"] = q["id"], root["path"], true
	switch a.Name {
	case "Sonarr":
		item["seasonFolder"] = true
		item["addOptions"] = map[string]any{"searchForMissingEpisodes": true, "monitor": "all"}
	case "Radarr":
		item["minimumAvailability"] = "released"
		item["addOptions"] = map[string]any{"searchForMovie": true}
	case "Lidarr":
		m, err := first("/metadataprofile")
		if err != nil {
			return "", err
		}
		item["metadataProfileId"] = m["id"]
		item["addOptions"] = map[string]any{"searchForMissingAlbums": true, "monitor": "all"}
	}
	if err := a.do("POST", "/"+kinds[a.Name], item, nil); err != nil {
		return "", err
	}
	return fmt.Sprint(root["path"]), nil
}
