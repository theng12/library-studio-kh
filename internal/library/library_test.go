package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
)

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// mockStock stands in for qBittorrent and Sonarr on one test server and records deletions.
func mockStock(t *testing.T, sonarrUp bool) (*httptest.Server, *[]string) {
	deleted := &[]string{}
	days := func(n int) string { return now.Add(-time.Duration(n) * 24 * time.Hour).Format(time.RFC3339) }
	routes := map[string]any{
		"/api/v2/torrents/info": []map[string]any{
			{"hash": "aaa", "name": "Old Imported", "progress": 1, "completion_on": 1},
			{"hash": "bbb", "name": "New Imported", "progress": 1, "completion_on": 1},
			{"hash": "ccc", "name": "Never Imported", "progress": 1, "completion_on": 1},
			{"hash": "ddd", "name": "Part Imported", "progress": 1, "completion_on": 1},
			{"hash": "eee", "name": "Still Downloading", "progress": 0.4},
			{"hash": "fff", "name": "Import Failed", "progress": 1, "completion_on": 1},
		},
		"/api/v2/app/preferences": map[string]any{"max_seeding_time_enabled": false, "max_ratio_act": 3},
		"/api/v2/sync/maindata":   map[string]any{"server_state": map[string]any{"free_space_on_disk": 5e11}},
		"/api/v3/history": map[string]any{"records": []map[string]any{
			{"eventType": "downloadFolderImported", "downloadId": "AAA", "date": days(8)},
			{"eventType": "downloadFolderImported", "downloadId": "BBB", "date": days(2)},
			{"eventType": "downloadFolderImported", "downloadId": "DDD", "date": days(9)},
			{"eventType": "downloadFailed", "downloadId": "FFF", "date": days(9)},
			{"eventType": "grabbed", "downloadId": "CCC", "date": days(9)},
		}},
		"/api/v3/queue":                 map[string]any{"records": []map[string]any{{"downloadId": "DDD"}, {"downloadId": "FFF"}}},
		"/api/v3/calendar":              []map[string]any{{"title": "Pilot", "seasonNumber": 1, "episodeNumber": 1, "airDateUtc": now.Add(48 * time.Hour).Format(time.RFC3339), "series": map[string]any{"title": "Show"}}},
		"/api/v3/config/downloadclient": map[string]any{"id": 1, "enableCompletedDownloadHandling": true},
		"/api/v3/downloadclient":        []map[string]any{{"id": 1, "implementation": "QBittorrent", "removeCompletedDownloads": false}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/torrents/delete" {
			r.ParseForm()
			if r.Form.Get("deleteFiles") != "true" {
				t.Errorf("delete without deleteFiles")
			}
			*deleted = append(*deleted, r.Form.Get("hashes"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v3/") && (!sonarrUp || r.Header.Get("X-Api-Key") != "key") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, deleted
}

func cfg(url string) config.Config {
	c := config.Default()
	c.QBit.URL = url
	c.Sonarr = config.App{URL: url, APIKey: "key"}
	return c
}

func TestRetiredCleanupKeepsEveryLocalCopy(t *testing.T) {
	srv, deleted := mockStock(t, true)
	home := t.TempDir()
	s := Gather(cfg(srv.URL), home, now)
	if s.Blocked != "" {
		t.Fatalf("unexpected block: %s", s.Blocked)
	}
	legacy := cfg(srv.URL)
	legacy.CleanupOn = true
	n, errs := Cleanup(legacy, home, s, now)
	if n != 0 || len(errs) != 1 || len(*deleted) != 0 {
		t.Fatalf("retired cleanup must never delete, got %d %v %v", n, *deleted, errs)
	}
	kinds := map[string]string{}
	for _, it := range Gather(cfg(srv.URL), home, now).Items {
		kinds[it.Title] = it.Kind
	}
	want := map[string]string{"New Imported": "imported", "Never Imported": "unmanaged", "Part Imported": "waiting",
		"Still Downloading": "downloading", "Import Failed": "waiting", "Old Imported": "imported", "Show · S01E01 Pilot": "upcoming"}
	for k, v := range want {
		if kinds[k] != v {
			t.Errorf("%s: want %s, got %q", k, v, kinds[k])
		}
	}
	if s.FreeBytes == nil || *s.FreeBytes != 5e11 {
		t.Errorf("free space not read from qBittorrent")
	}
	for _, st := range s.Settings {
		if st.Name == "When the limit is reached" && st.OK {
			t.Errorf("remove-with-files in qBittorrent must be flagged")
		}
	}
}

func TestCleanupDoesNothingWhenAnAppIsDownOrSwitchedOff(t *testing.T) {
	srv, deleted := mockStock(t, false)
	home := t.TempDir()
	s := Gather(cfg(srv.URL), home, now)
	if s.Blocked == "" {
		t.Fatal("Sonarr down must block cleanup")
	}
	Cleanup(cfg(srv.URL), home, s, now)

	up, deleted2 := mockStock(t, true)
	off := cfg(up.URL)
	off.CleanupOn = false
	Cleanup(off, home, Gather(off, home, now), now)
	if len(*deleted)+len(*deleted2) != 0 {
		t.Fatalf("nothing may be removed, got %v %v", *deleted, *deleted2)
	}
}
