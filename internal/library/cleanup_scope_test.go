package library

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

type scopedHistorySource struct {
	*fakeSource
	native     liveCleanupSource
	queueError bool
}

func (s scopedHistorySource) Proofs(app string) (map[string][]stock.ImportProof, []string, error) {
	return s.native.Proofs(app)
}
func (s scopedHistorySource) Queued() (map[string]string, []string, error) {
	if s.queueError {
		return nil, []string{"Lidarr queue could not be read."}, nil
	}
	return s.fakeSource.Queued()
}

func TestScopedCleanupIsIndependentButKeepsRequiredProofs(t *testing.T) {
	for _, mode := range []string{"independent", "target-history-failed", "queue-failed", "queued", "incomplete", "seeding", "NAS-mismatch", "ambiguous-owner"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			tv := h.add("TV", h.clock.Add(-10*24*time.Hour))
			music := h.add("Music", h.clock.Add(-10*24*time.Hour))
			h.src.proofs[strings.ToUpper(music.hash)][0].App = "Lidarr"
			h.cfg.PressureFreeGB = 200
			h.cfg.PressureBypassRetention = true
			h.freeFn = func() int64 { return 1 }
			if mode == "incomplete" {
				h.src.torrents[0].Progress = .5
			}
			if mode == "seeding" {
				h.src.torrents[0].Ratio = 0
				h.src.torrents[0].SeedingTime = 0
			}
			if mode == "queued" {
				h.src.queued[strings.ToUpper(tv.hash)] = "Lidarr"
			}
			if mode == "NAS-mismatch" {
				if err := os.WriteFile(tv.lib+"/renamed-by-app.mkv", []byte("different"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			nativeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				isSonarr := strings.HasPrefix(r.URL.Path, "/sonarr/")
				if (!isSonarr && mode != "ambiguous-owner") || (isSonarr && mode == "target-history-failed") {
					w.WriteHeader(503)
					w.Write([]byte("private body"))
					return
				}
				proof := h.src.proofs[strings.ToUpper(tv.hash)][0]
				json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 1, "records": []any{map[string]any{"eventType": "downloadFolderImported", "downloadId": tv.hash, "date": proof.ImportedAt, "data": map[string]any{"importedPath": proof.ImportedPath}}}})
			}))
			defer nativeServer.Close()
			c := config.Config{Sonarr: config.App{URL: nativeServer.URL + "/sonarr", APIKey: "private"}, Lidarr: config.App{URL: nativeServer.URL + "/lidarr", APIKey: "private"}}
			native := liveCleanupSource{get: func() config.Config { return c }}
			source := scopedHistorySource{fakeSource: h.src, native: native, queueError: mode == "queue-failed"}
			engine := h.engine()
			engine.Source = source
			preview, err := engine.PreviewApp(h.cfg, "Sonarr")
			if err != nil {
				t.Fatal(err)
			}
			if preview.App != "Sonarr" || h.src.stopCalls != 0 || len(h.src.metadataCalls) != 0 || !exists(tv.src) {
				t.Fatal("preview mutated state or lost scope")
			}
			result, err := engine.RunApp(h.cfg, "Sonarr")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "independent" {
				if result.Purged != 1 || exists(tv.src) || len(h.src.metadataCalls) != 1 || !strings.EqualFold(h.src.metadataCalls[0], tv.hash) {
					t.Fatalf("independent TV cleanup did not complete: %+v", result)
				}
				if !exists(tv.lib + "/renamed-by-app.mkv") {
					t.Fatal("NAS fixture removed")
				}
			} else if result.Purged != 0 || !exists(tv.src) || h.src.stopCalls != 0 || len(h.src.metadataCalls) != 0 {
				t.Fatalf("unsafe cleanup in %s: %+v", mode, result)
			}
			if !exists(music.src) || !exists(music.lib+"/renamed-by-app.mkv") {
				t.Fatal("out-of-scope music changed")
			}
		})
	}
}

func TestScopedCleanupPreservesOtherSavedRecords(t *testing.T) {
	h := newHarness(t)
	tv := h.add("TV", h.clock.Add(-10*24*time.Hour))
	music := h.add("Music", h.clock.Add(-10*24*time.Hour))
	h.src.proofs[strings.ToUpper(tv.hash)][0].App = "Sonarr"
	h.src.proofs[strings.ToUpper(music.hash)][0].App = "Lidarr"
	h.cfg.StagedDays = 365
	h.run()
	engine := h.engine()
	records, err := engine.load()
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(records[1])
	if err != nil {
		t.Fatal(err)
	}
	h.cfg.PressureFreeGB = 200
	h.cfg.PressureBypassRetention = true
	h.freeFn = func() int64 { return 1 }
	result, err := engine.RunApp(h.cfg, "Sonarr")
	if err != nil || result.Purged != 1 || exists(tv.src) {
		t.Fatalf("scoped saved-record cleanup failed: %+v %v", result, err)
	}
	records, err = engine.load()
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(records[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || !exists(music.src) {
		t.Fatal("out-of-scope saved record or files changed")
	}
}

func TestScopedCleanupRejectsUnknownOrUnconfiguredApp(t *testing.T) {
	h := newHarness(t)
	for _, app := range []string{"anything", "http://private-host", "sonarr"} {
		if _, err := h.engine().PreviewApp(h.cfg, app); !errors.Is(err, ErrCleanupApp) {
			t.Fatalf("accepted %q", app)
		}
		if _, err := h.engine().RunApp(h.cfg, app); !errors.Is(err, ErrCleanupApp) {
			t.Fatalf("accepted run %q", app)
		}
	}
	native := liveCleanupSource{get: func() config.Config { return config.Config{} }}
	if _, _, err := native.Proofs("Sonarr"); err == nil {
		t.Fatal("missing selected app became valid history")
	}
}
