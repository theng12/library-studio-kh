package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestTrackingRoutes(t *testing.T) {
	var monitored, releaseSearched bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series/7":
			w.Write([]byte(`{"id":7,"title":"Show","monitored":true,"seasons":[]}`))
		case "/api/v3/episode":
			w.Write([]byte(`[{"id":11,"seriesId":7,"seasonNumber":1,"episodeNumber":1,"title":"E","monitored":false,"airDate":"2024-02-02"}]`))
		case "/api/v3/episode/monitor":
			var put map[string]any
			json.NewDecoder(r.Body).Decode(&put)
			monitored = put["monitored"] == true
		case "/api/v3/release":
			releaseSearched = true
			w.Write([]byte(`[{"guid":"g1","indexerId":1,"indexer":"Idx","title":"Rel","protocol":1,"size":10,"seeders":2,"downloadAllowed":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Sonarr = config.App{URL: srv.URL, APIKey: "k"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerTracking(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}

	if w := do("GET", "/api/media/details?app=Sonarr&id=7", ""); w.Code != 200 || releaseSearched {
		t.Fatalf("details must not search releases: %d %v", w.Code, releaseSearched)
	} else if !strings.Contains(w.Body.String(), `"episodes"`) {
		t.Fatalf("details: %s", w.Body.String())
	}
	if w := do("GET", "/api/media/details?app=Prowlarr&id=7", ""); w.Code != 400 {
		t.Fatalf("unknown app must be refused: %d", w.Code)
	}
	if w := do("POST", "/api/media/tracking", `{"app":"Sonarr","id":7,"kind":"episode","childId":11}`); w.Code != 400 || monitored {
		t.Fatalf("missing monitored must be refused: %d %v", w.Code, monitored)
	}
	if w := do("POST", "/api/media/tracking", `{"app":"Sonarr","id":7,"kind":"episode","childId":11,"monitored":true}`); w.Code != 200 || !monitored {
		t.Fatalf("episode tracking: %d %v", w.Code, monitored)
	}
	if w := do("GET", "/api/media/releases?app=Sonarr&id=7", ""); w.Code != 400 || releaseSearched {
		t.Fatalf("missing episodeId must be refused without searching: %d %v", w.Code, releaseSearched)
	}
	if w := do("GET", "/api/media/releases?app=Sonarr&id=7&episodeId=11", ""); w.Code != 200 || !releaseSearched || strings.Contains(w.Body.String(), `"g1"`) {
		t.Fatalf("releases must be searched and sanitized: %d %s %v", w.Code, w.Body.String(), releaseSearched)
	}
	if w := do("POST", "/api/media/grab", `{"app":"Sonarr","id":7,"episodeId":11,"key":"bad"}`); w.Code != 400 {
		t.Fatalf("unknown grab key must be refused: %d", w.Code)
	}
}
