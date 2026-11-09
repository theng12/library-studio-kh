// Package stock talks to the stock apps (qBittorrent, Sonarr, Radarr, Lidarr, Prowlarr) through their official web APIs.
package stock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const timeout = 15 * time.Second

// ---- qBittorrent (WebUI API v2) ----

type Torrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	Progress     float64 `json:"progress"`
	State        string  `json:"state"`
	AddedOn      int64   `json:"added_on"`
	CompletionOn int64   `json:"completion_on"`
	ETA          int64   `json:"eta"`
	Size         int64   `json:"size"`
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
	DlSpeed      int64   `json:"dlspeed"`
	UpSpeed      int64   `json:"upspeed"`
	Ratio        float64 `json:"ratio"`
	Seeds        int     `json:"num_seeds"`
	Peers        int     `json:"num_leechs"`
	SavePath     string  `json:"save_path"`
	Category     string  `json:"category"`
	// Priority is qBittorrent's queue position: nil when the app does not report it,
	// 0 for completed/non-queued torrents, >0 for queued ones.
	Priority *int `json:"priority"`
}

type QBit struct {
	base, user, pass string
	c                *http.Client
	loggedIn         bool
}

func NewQBit(base, user, pass string) *QBit {
	jar, _ := cookiejar.New(nil)
	return &QBit{base: strings.TrimRight(base, "/"), user: user, pass: pass, c: &http.Client{Jar: jar, Timeout: timeout}}
}

func (q *QBit) login() error {
	if q.loggedIn || q.user == "" {
		return nil // no username: qBittorrent's "bypass authentication for localhost" is in use
	}
	resp, err := q.c.PostForm(q.base+"/api/v2/auth/login", url.Values{"username": {q.user}, "password": {q.pass}})
	if err != nil {
		return fmt.Errorf("qBittorrent is not reachable: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent && (resp.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "Ok.") {
		return fmt.Errorf("qBittorrent refused the username or password")
	}
	q.loggedIn = true
	return nil
}

func (q *QBit) call(path string, form url.Values, out any) error {
	if err := q.login(); err != nil {
		return err
	}
	var resp *http.Response
	var err error
	if form == nil {
		resp, err = q.c.Get(q.base + path)
	} else {
		resp, err = q.c.PostForm(q.base+path, form)
	}
	if err != nil {
		return fmt.Errorf("qBittorrent is not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("qBittorrent asked for a login: add its username and password in Settings")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qBittorrent answered %s for %s", resp.Status, path)
	}
	if out == nil {
		if path == "/api/v2/torrents/add" {
			b, err := io.ReadAll(io.LimitReader(resp.Body, 512))
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(b)) == "Fails." {
				return fmt.Errorf("qBittorrent didn't accept those links")
			}
		}
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (q *QBit) Torrents() ([]Torrent, error) {
	var ts []Torrent
	return ts, q.call("/api/v2/torrents/info", nil, &ts)
}

// Delete removes the torrent and its local files. Callers must have checked the library import first.
func (q *QBit) Delete(hash string) error {
	return q.call("/api/v2/torrents/delete", url.Values{"hashes": {hash}, "deleteFiles": {"true"}}, nil)
}

// AddLinks hands magnet or web links to qBittorrent, which saves them in its default folder.
func (q *QBit) AddLinks(links []string) error {
	return q.call("/api/v2/torrents/add", url.Values{"urls": {strings.Join(links, "\n")}}, nil)
}

// AddFiles uploads .torrent files to qBittorrent in one request.
func (q *QBit) AddFiles(files map[string][]byte) error {
	if err := q.login(); err != nil {
		return err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, b := range files {
		w, err := mw.CreateFormFile("torrents", name)
		if err != nil {
			return err
		}
		w.Write(b)
	}
	mw.Close()
	resp, err := q.c.Post(q.base+"/api/v2/torrents/add", mw.FormDataContentType(), &body)
	if err != nil {
		return fmt.Errorf("qBittorrent is not reachable: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || strings.TrimSpace(string(b)) == "Fails." {
		return fmt.Errorf("qBittorrent didn't accept the torrent files (%s)", strings.TrimSpace(resp.Status+" "+string(b)))
	}
	return nil
}

// FreeSpace is qBittorrent's own reading of its download disk.
func (q *QBit) FreeSpace() (int64, error) {
	var m struct {
		ServerState struct {
			Free int64 `json:"free_space_on_disk"`
		} `json:"server_state"`
	}
	return m.ServerState.Free, q.call("/api/v2/sync/maindata", nil, &m)
}

func (q *QBit) Prefs() (map[string]any, error) {
	var p map[string]any
	return p, q.call("/api/v2/app/preferences", nil, &p)
}

func (q *QBit) SetPrefs(p map[string]any) error {
	b, _ := json.Marshal(p)
	return q.call("/api/v2/app/setPreferences", url.Values{"json": {string(b)}}, nil)
}

// ---- Sonarr / Radarr / Lidarr / Prowlarr ----

type Arr struct {
	Name, base, key, api string
	c                    *http.Client
}

// NewArr: Sonarr and Radarr use API v3, Lidarr and Prowlarr use v1.
func NewArr(name, base, key string) *Arr {
	api := "v3"
	if name == "Lidarr" || name == "Prowlarr" {
		api = "v1"
	}
	return &Arr{Name: name, base: strings.TrimRight(base, "/"), key: key, api: api, c: &http.Client{Timeout: timeout}}
}

func (a *Arr) do(method, path string, body, out any) (err error) {
	status := 0
	defer func() {
		if strings.HasPrefix(path, "/history?") {
			err = historyFailure(err, status, false)
		}
	}()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+"/api/"+a.api+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", a.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.c.Do(req)
	if err != nil {
		return fmt.Errorf("%s is not reachable: %w", a.Name, err)
	}
	defer resp.Body.Close()
	status = resp.StatusCode
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%s refused the API key", a.Name)
	}
	if resp.StatusCode/100 != 2 {
		// The apps explain refusals (e.g. "already added") as [{"errorMessage": ...}].
		var why []struct{ ErrorMessage string }
		if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&why) == nil && len(why) > 0 && why[0].ErrorMessage != "" {
			return fmt.Errorf("%s: %s", a.Name, why[0].ErrorMessage)
		}
		return fmt.Errorf("%s answered %s for %s", a.Name, resp.Status, path)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *Arr) Ping() error { return a.do("GET", "/system/status", nil, nil) }

// Only these history events mean a whole download reached the library (Lidarr's trackFileImported is one file).
var importEvents = map[string]bool{"downloadFolderImported": true, "downloadImported": true}

// Imports maps an upper-case torrent hash to its latest successful library import.
// shortcut: reads the newest 1000 history rows; a missing row only delays removal, never causes one.
func (a *Arr) Imports() (map[string]time.Time, error) {
	var h struct {
		Records []struct {
			EventType  string    `json:"eventType"`
			DownloadID string    `json:"downloadId"`
			Date       time.Time `json:"date"`
		} `json:"records"`
	}
	if err := a.do("GET", "/history?page=1&pageSize=1000&sortKey=date&sortDirection=descending", nil, &h); err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for _, r := range h.Records {
		id := strings.ToUpper(r.DownloadID)
		if importEvents[r.EventType] && id != "" && r.Date.After(out[id]) {
			out[id] = r.Date
		}
	}
	return out, nil
}

// Queue is the set of hashes the app is still handling (downloading, or import not finished).
func (a *Arr) Queue() (map[string]bool, error) {
	var q struct {
		Records []struct {
			DownloadID string `json:"downloadId"`
		} `json:"records"`
	}
	if err := a.do("GET", "/queue?page=1&pageSize=1000", nil, &q); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range q.Records {
		out[strings.ToUpper(r.DownloadID)] = true
	}
	return out, nil
}

type Upcoming struct {
	Title string    `json:"title"`
	When  time.Time `json:"when"`
}

func (a *Arr) Upcoming(from, to time.Time) ([]Upcoming, error) {
	var raw []struct {
		Title                                   string
		HasFile                                 bool
		SeasonNumber, EpisodeNumber             int
		AirDateUtc, ReleaseDate, DigitalRelease *time.Time
		PhysicalRelease, InCinemas              *time.Time
		Series                                  *struct{ Title string }
		Artist                                  *struct{ ArtistName string }
	}
	q := "/calendar?start=" + from.UTC().Format(time.RFC3339) + "&end=" + to.UTC().Format(time.RFC3339) + "&includeSeries=true&includeArtist=true"
	if err := a.do("GET", q, nil, &raw); err != nil {
		return nil, err
	}
	var out []Upcoming
	for _, r := range raw {
		if r.HasFile {
			continue
		}
		var when time.Time
		for _, t := range []*time.Time{r.AirDateUtc, r.ReleaseDate, r.DigitalRelease, r.PhysicalRelease, r.InCinemas} {
			if t != nil && !t.Before(from) && (when.IsZero() || t.Before(when)) {
				when = *t
			}
		}
		if when.IsZero() {
			continue
		}
		title := r.Title
		switch {
		case r.Series != nil:
			title = fmt.Sprintf("%s · S%02dE%02d %s", r.Series.Title, r.SeasonNumber, r.EpisodeNumber, r.Title)
		case r.Artist != nil:
			title = r.Artist.ArtistName + " · " + r.Title
		}
		out = append(out, Upcoming{Title: title, When: when})
	}
	return out, nil
}

// Handling reads completed download handling (must be on) and, per qBittorrent client,
// whether the app removes the download right after import (must be off so the N days apply).
func (a *Arr) Handling() (handling bool, removesAtImport bool, err error) {
	var cfg map[string]any
	if err = a.do("GET", "/config/downloadclient", nil, &cfg); err != nil {
		return
	}
	handling, _ = cfg["enableCompletedDownloadHandling"].(bool)
	var clients []map[string]any
	if err = a.do("GET", "/downloadclient", nil, &clients); err != nil {
		return
	}
	for _, c := range clients {
		if c["implementation"] == "QBittorrent" && c["removeCompletedDownloads"] == true {
			removesAtImport = true
		}
	}
	return
}

// ApplyHandling turns completed download handling on and immediate removal off.
func (a *Arr) ApplyHandling() error {
	var cfg map[string]any
	if err := a.do("GET", "/config/downloadclient", nil, &cfg); err != nil {
		return err
	}
	if cfg["enableCompletedDownloadHandling"] != true {
		cfg["enableCompletedDownloadHandling"] = true
		if err := a.do("PUT", fmt.Sprintf("/config/downloadclient/%v", cfg["id"]), cfg, nil); err != nil {
			return err
		}
	}
	var clients []map[string]any
	if err := a.do("GET", "/downloadclient", nil, &clients); err != nil {
		return err
	}
	for _, c := range clients {
		if c["implementation"] == "QBittorrent" && c["removeCompletedDownloads"] == true {
			c["removeCompletedDownloads"] = false
			if err := a.do("PUT", fmt.Sprintf("/downloadclient/%v", c["id"]), c, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
