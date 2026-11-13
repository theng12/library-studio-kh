package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UI fixture names shared with cmd/library-studio's -seed-fixtures. The seeded
// cleanup record, download file and NAS copy use these exact values.
const (
	uiImportedFile = "Sample Series S02E06 1080p.mkv"
	uiImportedRel  = "Sample Series/Season 02/" + uiImportedFile
)

// uiHash is a 40-hex qBittorrent hash; history reports it upper-case like the real apps.
func uiHash(short string) string { return strings.Repeat(short, 20) }

// uiFixture adds the states the headless UI test needs: one verified import,
// a move in progress, a missing-files row and a completed download nobody imported.
type uiFixture struct {
	downloads, library string
	now                time.Time
	paused             map[string]bool // shared with the mock's stop/start handler
}

func (u uiFixture) unix(hours float64) int64 {
	return u.now.Add(-time.Duration(hours * float64(time.Hour))).Unix()
}

// importedSize is read from the seeded file so qBittorrent, the record and the disk agree.
func (u uiFixture) importedSize() int64 {
	if fi, err := os.Stat(filepath.Join(u.downloads, uiImportedFile)); err == nil {
		return fi.Size()
	}
	return 0
}

func (u uiFixture) torrents() []map[string]any {
	size := u.importedSize()
	// One active download so Pause/Resume and its Activity record are exercisable.
	activeState := "downloading"
	if u.paused[uiHash("c5")] {
		activeState = "stoppedDL"
	}
	return []map[string]any{
		{"hash": uiHash("c5"), "name": "Fresh Film (2021) 1080p", "progress": 0.42, "state": activeState, "size": 4e9, "downloaded": 1.68e9,
			"dlspeed": 8e6, "eta": 360, "added_on": u.unix(2), "num_seeds": 9, "num_leechs": 2, "save_path": u.downloads, "category": "radarr"},
		{"hash": uiHash("c1"), "name": uiImportedFile, "progress": 1, "state": "stalledUP", "size": size, "downloaded": size, "uploaded": size / 2,
			"ratio": 0.5, "seeding_time": 3600, "added_on": u.unix(30), "completion_on": u.unix(26), "num_seeds": 0, "num_leechs": 0, "save_path": u.downloads, "category": "tv-sonarr"},
		{"hash": uiHash("c2"), "name": "A Quiet Film (1998) 1080p", "progress": 1, "state": "moving", "size": 6e9, "downloaded": 6e9,
			"added_on": u.unix(3), "completion_on": u.unix(1), "save_path": u.downloads, "category": "radarr"},
		{"hash": uiHash("c3"), "name": "Another Show S01E02 720p", "progress": 0.9, "state": "missingFiles", "size": 1.1e9, "downloaded": 0.99e9,
			"added_on": u.unix(200), "save_path": u.downloads, "category": "tv-sonarr"},
		{"hash": uiHash("c4"), "name": "Old Download Without Import", "progress": 1, "state": "stalledUP", "size": 3.2e9, "downloaded": 3.2e9, "uploaded": 6.4e9,
			"ratio": 2, "seeding_time": 1_700_000, "added_on": u.unix(500), "completion_on": u.unix(480), "save_path": u.downloads, "category": "radarr"},
	}
}

// sonarrHistory is the whole-download import proof for c1, joined to series 1 by seriesId.
func (u uiFixture) sonarrHistory() map[string]any {
	return map[string]any{"eventType": "downloadFolderImported", "downloadId": strings.ToUpper(uiHash("c1")), "seriesId": 1,
		"date": u.now.Add(-24 * time.Hour).Format(time.RFC3339), "data": map[string]any{"importedPath": filepath.Join(u.library, uiImportedRel)}}
}

// files answers /api/v2/torrents/files for the UI torrents; ok is false for any other hash.
func (u uiFixture) files(hash string) ([]map[string]any, bool) {
	for _, t := range u.torrents() {
		if strings.EqualFold(t["hash"].(string), hash) {
			return []map[string]any{{"name": t["name"], "size": t["size"], "progress": t["progress"]}}, true
		}
	}
	return nil, false
}
