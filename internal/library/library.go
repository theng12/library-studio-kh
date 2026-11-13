// Package library gathers the dashboard list from the stock apps and decides which local copies may go.
package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/metadata"
	"librarystudio/internal/platform"
	"librarystudio/internal/stock"
)

type Item struct {
	ID       string    `json:"id,omitempty"`      // torrent hash (lowercase), or the app's media ID for library items
	MediaID  int       `json:"mediaId,omitempty"` // Sonarr series, Radarr movie or Lidarr artist ID
	Title    string    `json:"title"`
	Kind     string    `json:"kind"`             // downloading | waiting | imported | unmanaged | upcoming | removed | library
	State    string    `json:"state,omitempty"`  // qBittorrent's own state, e.g. pausedDL or queuedDL
	Status   string    `json:"status,omitempty"` // queued | downloading | importing | copied | moved | missing | on-disk | upcoming
	App      string    `json:"app,omitempty"`
	When     time.Time `json:"when,omitempty"` // upcoming date, planned removal, or removal done
	Progress float64   `json:"progress"`
	Size     int64     `json:"size,omitempty"`
	ETA      int64     `json:"eta,omitempty"`
	Speed    int64     `json:"speed,omitempty"`
	SavePath string    `json:"savePath,omitempty"`
	Category string    `json:"category,omitempty"`
	// Proof and destination: filled from the import/cleanup record, never guessed.
	StatusAt       time.Time `json:"statusAt,omitempty"`
	SourcePath     string    `json:"sourcePath,omitempty"`
	TargetVolume   string    `json:"targetVolume,omitempty"`
	TargetPath     string    `json:"targetPath,omitempty"`
	Verified       string    `json:"verified,omitempty"` // hash | size | none
	VerifiedAt     time.Time `json:"verifiedAt,omitempty"`
	PosterPath     string    `json:"posterPath,omitempty"`
	Overview       string    `json:"overview,omitempty"`
	Year           int       `json:"year,omitempty"`
	Genres         []string  `json:"genres,omitempty"`
	RuntimeMinutes int       `json:"runtimeMinutes,omitempty"`
	Rating         float64   `json:"rating,omitempty"`
	Stats          *Stats    `json:"stats,omitempty"` // downloads in qBittorrent only
}

// itemStatus is the owner-facing state behind the kind; it never claims a copy is
// safely in the library without the import proof that produced "imported".
func itemStatus(kind, state string) string {
	switch kind {
	case "downloading":
		switch {
		case state == "queuedDL" || state == "queuedUP":
			return "queued"
		case state == "stalledDL":
			return "stalled"
		case state == "missingFiles":
			return "missing"
		}
		return "downloading"
	case "waiting":
		return "importing"
	case "imported":
		return "copied"
	case "removed":
		return "moved"
	case "unmanaged":
		return "on-disk"
	case "upcoming":
		return "upcoming"
	}
	return kind
}

type AppState struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type Setting struct {
	App   string `json:"app"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Want  string `json:"want"`
	OK    bool   `json:"ok"`
}

type Snapshot struct {
	Items     []Item     `json:"items"`
	Apps      []AppState `json:"apps"`
	Settings  []Setting  `json:"settings"`
	FreeBytes *int64     `json:"freeBytes"` // nil = not known, never shown as 0
	FreeFrom  string     `json:"freeFrom,omitempty"`
	Library   string     `json:"library"` // reachable | missing | not set
	Blocked   string     `json:"blocked,omitempty"`
	remove    []stock.Torrent
}

type arrApp struct {
	name string
	cfg  config.App
}

func arrs(c config.Config) []*stock.Arr {
	var out []*stock.Arr
	for _, a := range []arrApp{{"Sonarr", c.Sonarr}, {"Radarr", c.Radarr}, {"Lidarr", c.Lidarr}} {
		if a.cfg.URL != "" && a.cfg.APIKey != "" {
			out = append(out, stock.NewArr(a.name, a.cfg.URL, a.cfg.APIKey))
		}
	}
	return out
}

// Decide is the safety rule: remove only a finished torrent that a library app reports as fully
// imported, that no library app is still handling, and whose import is at least days old.
func Decide(t stock.Torrent, imported map[string]time.Time, queued map[string]bool, now time.Time, days int) (kind string, removeAt time.Time, remove bool) {
	h := strings.ToUpper(t.Hash)
	at, ok := imported[h]
	switch {
	case t.Progress < 1:
		return "downloading", time.Time{}, false
	case !ok && queued[h]:
		return "waiting", time.Time{}, false
	case !ok:
		return "unmanaged", time.Time{}, false
	case queued[h]:
		return "waiting", time.Time{}, false // partly imported: the app still lists it
	}
	removeAt = at.Add(time.Duration(days) * 24 * time.Hour)
	return "imported", removeAt, !now.Before(removeAt)
}

// Gather builds the dashboard snapshot. meta may be nil; Gather never fills
// metadata itself because its titles are release names, not library titles
// (the inventory does that, cache-only), so nothing here waits on the network.
func Gather(c config.Config, home string, now time.Time, meta *metadata.Client) Snapshot {
	s := Snapshot{Items: []Item{}, Apps: []AppState{}, Library: "not set"}
	imported, queued := map[string]time.Time{}, map[string]bool{}
	appOf := map[string]string{}
	for _, a := range arrs(c) {
		st := AppState{Name: a.Name, OK: true}
		im, err := a.Imports()
		var q map[string]bool
		if err == nil {
			q, err = a.Queue()
		}
		if err != nil {
			st = AppState{Name: a.Name, Error: err.Error()}
			s.Blocked = fmt.Sprintf("%s didn't answer, so nothing is removed until it does.", a.Name)
		}
		for h, t := range im {
			if t.After(imported[h]) {
				imported[h], appOf[h] = t, a.Name
			}
		}
		for h := range q {
			queued[h], appOf[h] = true, a.Name
		}
		if err == nil {
			up, uerr := a.Upcoming(now, now.Add(14*24*time.Hour))
			if uerr != nil {
				st = AppState{Name: a.Name, Error: uerr.Error()}
			}
			for _, u := range up {
				s.Items = append(s.Items, Item{Title: u.Title, Kind: "upcoming", App: a.Name, When: u.When})
			}
			handling, removesNow, herr := a.Handling()
			h, r := onOff(handling), onOff(removesNow)
			if herr != nil {
				h, r = "Couldn't read", "Couldn't read"
			}
			s.Settings = append(s.Settings,
				Setting{a.Name, "Completed download handling", h, "On", herr == nil && handling},
				Setting{a.Name, "Remove downloads right after import", r, "Off", herr == nil && !removesNow})
		}
		s.Apps = append(s.Apps, st)
	}
	if c.Prowlarr.URL != "" && c.Prowlarr.APIKey != "" {
		st := AppState{Name: "Prowlarr", OK: true}
		if err := stock.NewArr("Prowlarr", c.Prowlarr.URL, c.Prowlarr.APIKey).Ping(); err != nil {
			st = AppState{Name: "Prowlarr", Error: err.Error()}
		}
		s.Apps = append(s.Apps, st)
	}

	if c.QBit.URL != "" {
		qb := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password)
		ts, err := qb.Torrents()
		if err != nil {
			s.Apps = append([]AppState{{Name: "qBittorrent", Error: err.Error()}}, s.Apps...)
			s.Blocked = "qBittorrent didn't answer, so nothing is removed until it does."
		} else {
			s.Apps = append([]AppState{{Name: "qBittorrent", OK: true}}, s.Apps...)
			speedMu.Lock()
			speeds := readSpeeds(home)
			speedMu.Unlock()
			recs := savedRecords(home)
			for _, t := range ts {
				kind, at, rm := Decide(t, imported, queued, now, c.CleanupDays)
				it := Item{ID: strings.ToLower(t.Hash), Title: t.Name, Kind: kind, State: t.State, Status: itemStatus(kind, t.State), App: appOf[strings.ToUpper(t.Hash)], Progress: t.Progress, Size: t.Size, ETA: t.ETA, Speed: t.DlSpeed, SavePath: t.SavePath, SourcePath: t.SavePath, Category: t.Category, Stats: statsOf(t, speeds[t.Hash])}
				if t.CompletionOn > 0 {
					it.StatusAt = time.Unix(t.CompletionOn, 0)
				}
				if rec := recs[strings.ToUpper(t.Hash)]; rec != nil {
					applyRecord(&it, rec)
				}
				if kind == "imported" {
					it.When = at
				}
				if rm {
					s.remove = append(s.remove, t)
				}
				s.Items = append(s.Items, it)
			}
			if p, err := qb.Prefs(); err == nil {
				s.Settings = append(s.Settings, qbitSettings(p)...)
			}
			if c.DownloadPath == "" {
				if f, err := qb.FreeSpace(); err == nil {
					s.FreeBytes, s.FreeFrom = &f, "qBittorrent's download disk"
				}
			}
		}
	}
	if c.DownloadPath != "" {
		if f, err := platform.FreeBytes(c.DownloadPath); err == nil {
			s.FreeBytes, s.FreeFrom = &f, c.DownloadPath
		}
	}
	if c.LibraryPath != "" {
		s.Library = "missing"
		if fi, err := os.Stat(c.LibraryPath); err == nil && fi.IsDir() {
			s.Library = "reachable"
		}
	}
	for _, r := range readLog(home) {
		s.Items = append(s.Items, r)
	}
	order := map[string]int{"downloading": 0, "waiting": 1, "imported": 2, "unmanaged": 3, "upcoming": 4, "removed": 5}
	sort.SliceStable(s.Items, func(i, j int) bool {
		a, b := s.Items[i], s.Items[j]
		if order[a.Kind] != order[b.Kind] {
			return order[a.Kind] < order[b.Kind]
		}
		if a.Kind == "removed" {
			return a.When.After(b.When)
		}
		return a.When.Before(b.When)
	})
	return s
}

// Browse lists what Sonarr, Radarr and Lidarr already look after. errs names any app that didn't answer.
func Browse(c config.Config) (items []stock.Media, errs []string) {
	items = []stock.Media{}
	for _, a := range arrs(c) {
		m, err := a.Library()
		if err != nil {
			errs = append(errs, err.Error())
		}
		items = append(items, m...)
	}
	return
}

// Find asks each library app's own search for new shows, films or artists.
func Find(c config.Config, term string) (items []stock.Media, errs []string) {
	items = []stock.Media{}
	for _, a := range arrs(c) {
		m, err := a.Lookup(term)
		if err != nil {
			errs = append(errs, err.Error())
		}
		items = append(items, m...)
	}
	return
}

// Add hands a Find result back to the app it came from.
func Add(c config.Config, app string, raw json.RawMessage) (string, error) {
	for _, a := range arrs(c) {
		if a.Name == app {
			return a.Add(raw)
		}
	}
	return "", fmt.Errorf("%s isn't connected", app)
}

// AddTorrents hands magnet/web links and .torrent files to qBittorrent.
func AddTorrents(c config.Config, links []string, files map[string][]byte) error {
	if c.QBit.URL == "" {
		return fmt.Errorf("qBittorrent isn't connected. Add it in Settings first.")
	}
	qb := stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password)
	if len(links) > 0 {
		if err := qb.AddLinks(links); err != nil {
			return err
		}
	}
	if len(files) > 0 {
		return qb.AddFiles(files)
	}
	return nil
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// qBittorrent's limit action: 0 stop, 1 remove torrent, 2 super seeding, 3 remove torrent and files.
var limitActions = map[float64]string{0: "Stop torrent", 1: "Remove torrent", 2: "Super seeding", 3: "Remove torrent and files"}

func qbitSettings(p map[string]any) []Setting {
	enabled, hasEnabled := p["max_seeding_time_enabled"].(bool)
	mins, hasMinutes := p["max_seeding_time"].(float64)
	act, hasAction := p["max_ratio_act"].(float64)
	limit := "Not reported"
	if hasEnabled && !enabled {
		limit = "No limit"
	} else if hasEnabled && hasMinutes {
		limit = fmt.Sprintf("%g days", mins/1440)
	}
	action := "Not reported"
	if hasAction {
		action = limitActions[act]
	}
	return []Setting{
		{"qBittorrent", "Stop seeding after", limit, "Controlled in qBittorrent", hasEnabled && (!enabled || hasMinutes)},
		{"qBittorrent", "When the limit is reached", action, "Stop torrent", hasAction && act == 0},
	}
}

// Apply is retained for old callers, but no longer overwrites the stock seeding policy.
func Apply(c config.Config) []string {
	if c.CleanupOn {
		return []string{config.StagedCleanupNotice}
	}
	return nil
}

// Cleanup never calls the retired delete-with-files path. The staged Trash engine is separate work.
func Cleanup(c config.Config, home string, s Snapshot, now time.Time) (removed int, errs []string) {
	if c.CleanupOn {
		return 0, []string{config.StagedCleanupNotice}
	}
	return 0, nil
}

const logKeep = 200

func logPath(home string) string { return filepath.Join(home, "removed.json") }

func readLog(home string) []Item {
	var items []Item
	if b, err := os.ReadFile(logPath(home)); err == nil {
		_ = json.Unmarshal(b, &items)
	}
	return items
}

func appendLog(home string, it Item) {
	items := append(readLog(home), it)
	if len(items) > logKeep {
		items = items[len(items)-logKeep:]
	}
	b, _ := json.MarshalIndent(items, "", "  ")
	_ = os.MkdirAll(home, 0o700)
	_ = os.WriteFile(logPath(home), b, 0o600)
}
