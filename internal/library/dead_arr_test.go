package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"librarystudio/internal/config"
)

type arrStub struct {
	mu      sync.Mutex
	calls   []string
	queueUp bool
	history bool
}

func (s *arrStub) server(t *testing.T, downloadID string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls = append(s.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		s.mu.Unlock()
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/history"):
			records := []map[string]any{}
			if s.history {
				records = []map[string]any{{"eventType": "downloadFolderImported", "downloadId": downloadID, "date": time.Now().Add(-48 * time.Hour), "seriesId": 1}}
			}
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": len(records), "records": records})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/queue"):
			if !s.queueUp {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 1, "records": []map[string]any{{"id": 7, "downloadId": downloadID, "seriesId": 1}}})
		case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/queue/7"):
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *arrStub) deletes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, c := range s.calls {
		if strings.HasPrefix(c, "DELETE ") {
			out = append(out, c)
		}
	}
	return out
}

// arrFixture: a dead torrent in qBittorrent plus a Sonarr queue row for it.
func arrFixture(t *testing.T, imported bool) (config.Config, string, *arrStub) {
	t.Helper()
	return arrFixtureState(t, imported, "missingFiles")
}

// arrFixtureState is arrFixture with the qBittorrent state chosen, for the
// stalled path.
func arrFixtureState(t *testing.T, imported bool, state string) (config.Config, string, *arrStub) {
	t.Helper()
	deletes := []url.Values{}
	qbit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			json.NewEncoder(w).Encode([]map[string]any{{"hash": strings.ToLower(deadHash), "name": "Dead Show S01E09", "progress": 0.3, "state": state, "save_path": "/downloads/tv"}})
		case "/api/v2/torrents/delete":
			r.ParseForm()
			deletes = append(deletes, r.PostForm)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(qbit.Close)
	stub := &arrStub{queueUp: true, history: imported}
	arr := stub.server(t, deadHash)
	c := config.Default()
	c.QBit = config.QBit{URL: qbit.URL}
	c.Sonarr = config.App{URL: arr.URL, APIKey: "k"}
	c.DeadCleanupOn = true
	c.DeadArrCleanupOn = true
	c.DeadCleanupHours = 24
	return c, t.TempDir(), stub
}

func osRead(home, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(home, name))
	return string(b), err
}

func TestDeadArrCleanupSwitchOffNeverCallsAnApp(t *testing.T) {
	c, home, stub := arrFixture(t, false)
	c.DeadArrCleanupOn = false
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	SweepDeadArr(c, home, now, true)
	SweepDeadArr(c, home, now.Add(48*time.Hour), true)
	if got := stub.deletes(); len(got) != 0 {
		t.Fatalf("switch off must never call an app queue: %v", got)
	}
}

func TestDeadArrCleanupNeverTouchesImportedItems(t *testing.T) {
	c, home, stub := arrFixture(t, true)
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	SweepDeadArr(c, home, now, true)
	SweepDeadArr(c, home, now.Add(48*time.Hour), true)
	if got := stub.deletes(); len(got) != 0 {
		t.Fatalf("an imported item must never lose its queue row: %v", got)
	}
}

func TestDeadArrCleanupRemovesOnlyTheQueueRowAfterGrace(t *testing.T) {
	c, home, stub := arrFixture(t, false)
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	if removed, _, err := SweepDeadArr(c, home, now, true); err != nil || len(removed) != 0 {
		t.Fatalf("first sighting starts the grace: %v %v", removed, err)
	}
	removed, problems, err := SweepDeadArr(c, home, now.Add(25*time.Hour), true)
	if err != nil || len(problems) != 0 || len(removed) != 1 {
		t.Fatalf("grace elapsed must remove one queue row: removed=%v problems=%v err=%v", removed, problems, err)
	}
	got := stub.deletes()
	if len(got) != 1 || !strings.Contains(got[0], "/queue/7?") ||
		!strings.Contains(got[0], "removeFromClient=false") || !strings.Contains(got[0], "blocklist=false") {
		t.Fatalf("exactly one queue DELETE with removeFromClient=false and blocklist=false, got %v", got)
	}
	events, err := osRead(home, "removed.json")
	if err != nil || !strings.Contains(events, "download-arr-removed") || !strings.Contains(events, "Dead Show S01E09") {
		t.Fatalf("the app-queue removal must be recorded in Activity: %v %s", err, events)
	}
}

func TestStalledDownloadIsBlocklistedAndSearchedAgain(t *testing.T) {
	c, home, stub := arrFixtureState(t, false, "stalledDL")
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	if removed, _, err := SweepDeadArr(c, home, now, true); err != nil || len(removed) != 0 {
		t.Fatalf("first sighting starts the grace: %v %v", removed, err)
	}
	if removed, _, err := SweepDeadArr(c, home, now.Add(23*time.Hour), true); err != nil || len(removed) != 0 {
		t.Fatalf("a stalled row inside the grace must not be touched: %v %v", removed, err)
	}
	removed, problems, err := SweepDeadArr(c, home, now.Add(25*time.Hour), true)
	if err != nil || len(problems) != 0 || len(removed) != 1 || removed[0].Reason != "stalled" {
		t.Fatalf("a stalled row past the grace must be swept: removed=%v problems=%v err=%v", removed, problems, err)
	}
	got := stub.deletes()
	if len(got) != 1 || !strings.Contains(got[0], "removeFromClient=true") ||
		!strings.Contains(got[0], "blocklist=true") || !strings.Contains(got[0], "skipRedownload=false") {
		t.Fatalf("a stalled release must leave the client, be blocklisted and be searched again, got %v", got)
	}
}

func TestDeadArrCleanupRefusesPartialActionWhenAnAppIsDown(t *testing.T) {
	c, home, stub := arrFixture(t, false)
	stub.queueUp = false
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	SweepDeadArr(c, home, now, true)
	removed, problems, err := SweepDeadArr(c, home, now.Add(48*time.Hour), true)
	if err == nil || len(removed) != 0 || len(problems) == 0 {
		t.Fatalf("an unreadable app queue must refuse with an honest error and no action: removed=%v problems=%v err=%v", removed, problems, err)
	}
	if got := stub.deletes(); len(got) != 0 {
		t.Fatalf("no partial action may be taken: %v", got)
	}
}
