package library

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
)

func TestHistoryDiagnosticKeepsCleanupClosed(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body, detail string
	}{
		{"http", 503, `[{"errorMessage":"private-key-private-host"}]`, "http_error; HTTP 503"},
		{"decode", 200, `private-key-private-host`, "invalid_response; HTTP 200"},
		{"history", 200, `{"records":[]}`, "invalid_history; HTTP 200"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			c := config.Config{Lidarr: config.App{URL: server.URL, APIKey: "private-key-private-host"}}
			native := liveCleanupSource{get: func() config.Config { return c }}
			proofs, problems, err := native.Proofs()
			want := "Lidarr history could not be read: " + tc.detail + "."
			if err != nil || len(proofs) != 0 || len(problems) != 1 || problems[0] != want {
				t.Fatalf("unexpected proof result: %v %v %v", proofs, problems, err)
			}
			if strings.Contains(problems[0], server.URL) || strings.Contains(problems[0], "private-key-private-host") {
				t.Fatal("leaked native data")
			}
			h := newHarness(t)
			item := h.add("safe-import", h.clock.Add(-10*24*time.Hour))
			h.src.proofProblems = problems
			h.cfg.PressureFreeGB = 200
			h.cfg.PressureBypassRetention = true
			h.freeFn = func() int64 { return 1 }
			for _, res := range []CleanupResult{h.preview(), h.run()} {
				if len(res.Status.Blocked) != 1 || res.Status.Blocked[0] != want || len(res.Actions) != 0 || res.Purged != 0 {
					t.Fatalf("history failure allowed cleanup: %+v", res)
				}
			}
			if h.src.stopCalls != 0 || len(h.src.metadataCalls) != 0 {
				t.Fatal("history failure changed torrent")
			}
			if _, err := os.Stat(item.src); err != nil {
				t.Fatalf("history failure moved local copy: %v", err)
			}
			if _, err := os.Stat(item.lib); err != nil {
				t.Fatalf("history failure touched library: %v", err)
			}
		})
	}
}
