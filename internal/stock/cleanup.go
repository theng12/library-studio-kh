package stock

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CleanupTorrent is the qBittorrent state the staged cleanup engine needs.
// It is deliberately separate from Torrent so the existing dashboard shapes
// do not grow or change.
type CleanupTorrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	SavePath     string  `json:"save_path"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	Ratio        float64 `json:"ratio"`
	SeedingTime  int64   `json:"seeding_time"` // seconds qBittorrent reports seeding
	CompletionOn int64   `json:"completion_on"`
	AddedOn      int64   `json:"added_on"`
	Size         int64   `json:"size"`
}

// CleanupTorrents lists qBittorrent's live torrents with the fields the staged
// policy uses. A read failure is an error; callers must not guess.
func (q *QBit) CleanupTorrents() ([]CleanupTorrent, error) {
	var ts []CleanupTorrent
	return ts, q.call("/api/v2/torrents/info", nil, &ts)
}

// TorrentFile is one file qBittorrent reports for a torrent. Name is relative
// to the torrent's save path.
type TorrentFile struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

// TorrentFiles lists every file in one torrent. Progress below 1 means at least
// one file is unfinished and the torrent is not eligible.
func (q *QBit) TorrentFiles(hash string) ([]TorrentFile, error) {
	if !validHash(hash) {
		return nil, fmt.Errorf("%q isn't a torrent hash", hash)
	}
	var fs []TorrentFile
	return fs, q.call("/api/v2/torrents/files?hash="+url.QueryEscape(hash), nil, &fs)
}

// RemoveMetadata removes only qBittorrent's bookkeeping for one torrent. It can
// never ask qBittorrent to delete files: deleteFiles is always false.
func (q *QBit) RemoveMetadata(hash string) error {
	if !validHash(hash) {
		return fmt.Errorf("%q isn't a torrent hash", hash)
	}
	return q.call("/api/v2/torrents/delete", url.Values{"hashes": {hash}, "deleteFiles": {"false"}}, nil)
}

// ErrNoWholeImport means no completed whole-download import record exists.
var ErrNoWholeImport = errors.New("no completed whole-download import in the app's history")

// ImportProof is one app's evidence that a whole download reached its library.
// The engine verifies the actual files itself; this record supplies the real
// download ID, import time and library target.
type ImportProof struct {
	App          string
	DownloadID   string
	ImportedAt   time.Time
	ImportedPath string
}

// WholeImports reads every history page the app reports, never a single newest
// page, and returns the latest whole-download import per download ID. Missing
// pagination fields, a short page run or a matching record with no library path
// are errors, so an incomplete history can never read as a verified import.
func (a *Arr) WholeImports() (result map[string]ImportProof, err error) {
	defer func() { err = historyFailure(err, http.StatusOK, true) }()
	out := map[string]ImportProof{}
	read := 0
	now := time.Now()
	for page := 1; page <= maxQueuePages; page++ {
		var p struct {
			Page         int  `json:"page"`
			TotalRecords *int `json:"totalRecords"`
			Records      []struct {
				EventType  string         `json:"eventType"`
				DownloadID string         `json:"downloadId"`
				Date       time.Time      `json:"date"`
				Data       map[string]any `json:"data"`
			} `json:"records"`
		}
		if err := a.do("GET", fmt.Sprintf("/history?page=%d&pageSize=1000&sortKey=date&sortDirection=descending", page), nil, &p); err != nil {
			return nil, err
		}
		if p.TotalRecords == nil || *p.TotalRecords < 0 || p.Records == nil || (p.Page != 0 && p.Page != page) {
			return nil, fmt.Errorf("%s returned a malformed history page", a.Name)
		}
		for _, r := range p.Records {
			if !importEvents[r.EventType] {
				continue
			}
			id := strings.ToUpper(strings.TrimSpace(r.DownloadID))
			if id == "" {
				continue
			}
			path, _ := r.Data["importedPath"].(string)
			if strings.TrimSpace(path) == "" {
				return nil, fmt.Errorf("%s reported an import for download %s without its library path", a.Name, id)
			}
			// A missing/zero date would make the item immediately eligible; a
			// date beyond a small clock-skew allowance is not a real import.
			if r.Date.IsZero() || r.Date.After(now.Add(5*time.Minute)) {
				return nil, fmt.Errorf("%s reported an import for download %s without a valid import date", a.Name, id)
			}
			if prev, ok := out[id]; !ok || r.Date.After(prev.ImportedAt) {
				out[id] = ImportProof{App: a.Name, DownloadID: id, ImportedAt: r.Date, ImportedPath: strings.TrimSpace(path)}
			}
		}
		read += len(p.Records)
		if read >= *p.TotalRecords {
			return out, nil
		}
		if len(p.Records) == 0 {
			return nil, fmt.Errorf("%s history pagination stopped early", a.Name)
		}
	}
	return nil, fmt.Errorf("%s history pagination did not finish within the %d-page safety limit", a.Name, maxQueuePages)
}
