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
	App              string          `json:"app"`
	ID               int             `json:"id"`
	Title            string          `json:"title"`
	Year             int             `json:"year,omitempty"`
	Added            *time.Time      `json:"added,omitempty"`
	Status           string          `json:"status,omitempty"`
	Overview         string          `json:"overview,omitempty"`
	HaveFiles        bool            `json:"haveFiles"`
	InLibrary        bool            `json:"inLibrary"`
	Monitored        bool            `json:"monitored"`
	QualityProfileID int             `json:"qualityProfileId"`
	Raw              json.RawMessage `json:"raw,omitempty"` // lookup result, sent back unchanged to add it
}

// Each app keeps its own kind of item: Sonarr series, Radarr movies, Lidarr artists.
var kinds = map[string]string{"Sonarr": "series", "Radarr": "movie", "Lidarr": "artist"}

type rawMedia struct {
	ID               int
	Title            string
	ArtistName       string
	Year             int
	Added            *time.Time
	Status           string
	Overview         string
	HasFile          bool
	Monitored        bool
	QualityProfileID int `json:"qualityProfileId"`
	Statistics       *struct{ EpisodeFileCount, TrackFileCount int }
}

func (r rawMedia) media(app string) Media {
	m := Media{App: app, ID: r.ID, Title: r.Title, Year: r.Year, Status: r.Status, Overview: r.Overview, InLibrary: r.ID > 0, HaveFiles: r.HasFile, Monitored: r.Monitored, QualityProfileID: r.QualityProfileID}
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
// Kept for existing callers; new adds use AddSelected so the owner chooses.
func (a *Arr) Add(raw json.RawMessage) (string, error) {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil || item == nil {
		return "", errors.New("that result could not be read")
	}
	o, err := a.Options()
	if err != nil {
		return "", err
	}
	if len(o.QualityProfiles) == 0 {
		return "", fmt.Errorf("%s has no quality profile set up yet", a.Name)
	}
	if len(o.RootFolders) == 0 {
		return "", fmt.Errorf("%s has no root folder set up yet", a.Name)
	}
	sel := AddSelection{QualityProfileID: o.QualityProfiles[0].ID, RootFolderPath: o.RootFolders[0].Path}
	if a.Name == "Lidarr" {
		if len(o.MetadataProfiles) == 0 {
			return "", fmt.Errorf("%s has no metadata profile set up yet", a.Name)
		}
		sel.MetadataProfileID = o.MetadataProfiles[0].ID
	}
	return a.add(item, sel)
}
