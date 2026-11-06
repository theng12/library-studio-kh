package stock

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// errNotFound marks an endpoint this qBittorrent version doesn't have, so the old name can be tried.
var errNotFound = errors.New("not found")

// TorrentAction pauses or resumes torrents by hash. qBittorrent 5 renamed pause/resume to
// stop/start; a 404 on the new name means an older server, which is retried with the old one.
func (q *QBit) TorrentAction(hashes []string, action string) error {
	if action != "pause" && action != "resume" {
		return errors.New(`action must be "pause" or "resume"`)
	}
	if len(hashes) == 0 || len(hashes) > 1000 {
		return errors.New("choose between 1 and 1000 torrents")
	}
	for _, h := range hashes {
		if !validHash(h) {
			return fmt.Errorf("%q isn't a torrent hash", h)
		}
	}
	paths := []string{"/api/v2/torrents/stop", "/api/v2/torrents/pause"}
	if action == "resume" {
		paths = []string{"/api/v2/torrents/start", "/api/v2/torrents/resume"}
	}
	err := q.postHashes(paths[0], hashes)
	if errors.Is(err, errNotFound) {
		err = q.postHashes(paths[1], hashes)
	}
	return err
}

func validHash(h string) bool {
	if len(h) != 40 && len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

// postHashes sends one qBittorrent request for the whole selection.
// shortcut: qBittorrent doesn't report per-hash failures, so a bulk action is all or nothing.
func (q *QBit) postHashes(path string, hashes []string) error {
	if err := q.login(); err != nil {
		return err
	}
	resp, err := q.c.PostForm(q.base+path, url.Values{"hashes": {strings.Join(hashes, "|")}})
	if err != nil {
		return fmt.Errorf("qBittorrent is not reachable: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("qBittorrent answered %s for %s: %w", resp.Status, path, errNotFound)
	case http.StatusForbidden:
		return fmt.Errorf("qBittorrent asked for a login: add its username and password in Settings")
	case http.StatusOK, http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("qBittorrent answered %s for %s", resp.Status, path)
	}
}

// Option is one live choice a library app offers: a quality profile, root folder or metadata profile.
type Option struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type Options struct {
	QualityProfiles  []Option `json:"qualityProfiles"`
	RootFolders      []Option `json:"rootFolders"`
	MetadataProfiles []Option `json:"metadataProfiles"`
}

// Options reads the choices the app offers right now; nothing is cached.
func (a *Arr) Options() (Options, error) {
	o := Options{QualityProfiles: []Option{}, RootFolders: []Option{}, MetadataProfiles: []Option{}}
	if err := a.do("GET", "/qualityprofile", nil, &o.QualityProfiles); err != nil {
		return o, err
	}
	if err := a.do("GET", "/rootfolder", nil, &o.RootFolders); err != nil {
		return o, err
	}
	if a.Name == "Lidarr" {
		if err := a.do("GET", "/metadataprofile", nil, &o.MetadataProfiles); err != nil {
			return o, err
		}
	}
	return o, nil
}

type AddSelection struct {
	QualityProfileID  int
	RootFolderPath    string
	MetadataProfileID int // Lidarr only
}

// AddSelected adds a lookup result with the owner's choices, after checking them against the app.
func (a *Arr) AddSelected(raw json.RawMessage, sel AddSelection) (string, error) {
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil || item == nil {
		return "", errors.New("that result could not be read")
	}
	o, err := a.Options()
	if err != nil {
		return "", err
	}
	if !hasID(o.QualityProfiles, sel.QualityProfileID) {
		return "", fmt.Errorf("choose one of the quality profiles %s lists", a.Name)
	}
	if !hasPath(o.RootFolders, sel.RootFolderPath) {
		return "", fmt.Errorf("choose one of the folders %s lists", a.Name)
	}
	if a.Name == "Lidarr" && !hasID(o.MetadataProfiles, sel.MetadataProfileID) {
		return "", errors.New("choose one of the metadata profiles Lidarr lists")
	}
	return a.add(item, sel)
}

func hasID(xs []Option, id int) bool {
	for _, x := range xs {
		if x.ID == id {
			return true
		}
	}
	return false
}

func hasPath(xs []Option, path string) bool {
	for _, x := range xs {
		if x.Path == path {
			return true
		}
	}
	return false
}

func (a *Arr) add(item map[string]any, sel AddSelection) (string, error) {
	item["qualityProfileId"], item["rootFolderPath"], item["monitored"] = sel.QualityProfileID, sel.RootFolderPath, true
	switch a.Name {
	case "Sonarr":
		item["seasonFolder"] = true
		item["addOptions"] = map[string]any{"searchForMissingEpisodes": true, "monitor": "all"}
	case "Radarr":
		item["minimumAvailability"] = "released"
		item["addOptions"] = map[string]any{"searchForMovie": true}
	case "Lidarr":
		item["metadataProfileId"] = sel.MetadataProfileID
		item["addOptions"] = map[string]any{"searchForMissingAlbums": true, "monitor": "all"}
	}
	if err := a.do("POST", "/"+kinds[a.Name], item, nil); err != nil {
		return "", err
	}
	return sel.RootFolderPath, nil
}

// SetMonitored reads the item first so the PUT keeps every other field it already has.
func (a *Arr) SetMonitored(id int, monitored bool) error {
	if id <= 0 {
		return errors.New("choose a library item")
	}
	path := fmt.Sprintf("/%s/%d", kinds[a.Name], id)
	var item map[string]any
	if err := a.do("GET", path, nil, &item); err != nil {
		return err
	}
	item["monitored"] = monitored
	return a.do("PUT", path, item, nil)
}
