package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
)

const deadHash = "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD"

// deadFixture serves one qBittorrent torrent in the given state and records
// every metadata-only delete request.
func deadFixture(t *testing.T, state string) (config.Config, string, *[]url.Values) {
	t.Helper()
	deletes := []url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			json.NewEncoder(w).Encode([]map[string]any{{"hash": strings.ToLower(deadHash), "name": "Dead Show S01E01", "progress": 0.4, "state": state, "save_path": "/downloads/tv"}})
		case "/api/v2/torrents/delete":
			r.ParseForm()
			deletes = append(deletes, r.PostForm)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := config.Default()
	c.QBit = config.QBit{URL: srv.URL}
	c.DeadCleanupOn = true
	c.DeadCleanupHours = 24
	return c, t.TempDir(), &deletes
}

func TestDeadCleanupGraceAndMetadataOnlyRemoval(t *testing.T) {
	c, home, deletes := deadFixture(t, "missingFiles")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	c.DeadCleanupOn = false
	if removed, _, err := SweepDead(c, home, now, true); err != nil || len(removed) != 0 || len(*deletes) != 0 {
		t.Fatalf("switch off must never act: %v %v %v", removed, err, *deletes)
	}
	c.DeadCleanupOn = true
	// First sighting starts the grace; nothing is removed yet.
	if removed, _, err := SweepDead(c, home, now, true); err != nil || len(removed) != 0 || len(*deletes) != 0 {
		t.Fatalf("grace must start on the first sighting: %v %v %v", removed, err, *deletes)
	}
	if removed, _, err := SweepDead(c, home, now.Add(23*time.Hour), true); err != nil || len(removed) != 0 || len(*deletes) != 0 {
		t.Fatalf("grace not elapsed must not act: %v %v %v", removed, err, *deletes)
	}
	removed, problems, err := SweepDead(c, home, now.Add(25*time.Hour), true)
	if err != nil || len(problems) != 0 || len(removed) != 1 || len(*deletes) != 1 {
		t.Fatalf("grace elapsed must remove: removed=%v problems=%v err=%v deletes=%v", removed, problems, err, *deletes)
	}
	if got := (*deletes)[0].Get("deleteFiles"); got != "false" {
		t.Fatalf("removal must never delete files, got deleteFiles=%q", got)
	}
	events, err := os.ReadFile(filepath.Join(home, "removed.json"))
	if err != nil || !strings.Contains(string(events), "download-dead-removed") || !strings.Contains(string(events), "Dead Show S01E01") {
		t.Fatalf("the automatic removal must be recorded in Activity: %v %s", err, events)
	}
}

func TestDeadCleanupNeverTouchesProofOrPausedRows(t *testing.T) {
	c, home, deletes := deadFixture(t, "missingFiles")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// A proof for the same hash: the row is not dead.
	history := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 1, "records": []map[string]any{
			{"eventType": "downloadFolderImported", "downloadId": deadHash, "date": now.Add(-48 * time.Hour), "seriesId": 1},
		}})
	}))
	t.Cleanup(history.Close)
	c.Sonarr = config.App{URL: history.URL, APIKey: "k"}
	if removed, _, err := SweepDead(c, home, now, true); err != nil || len(removed) != 0 || len(*deletes) != 0 {
		t.Fatalf("proof-present rows must never be touched: %v %v %v", removed, err, *deletes)
	}
	// Owner-paused rows are not dead even after the grace.
	c.Sonarr = config.App{}
	c2, home2, deletes2 := deadFixture(t, "pausedDL")
	SweepDead(c2, home2, now, true)
	if removed, _, err := SweepDead(c2, home2, now.Add(48*time.Hour), true); err != nil || len(removed) != 0 || len(*deletes2) != 0 {
		t.Fatalf("owner-paused rows must never be touched: %v %v %v", removed, err, *deletes2)
	}
}
