package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupAndAdd(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie/lookup":
			if r.URL.Query().Get("term") != "big fish" {
				t.Errorf("term not passed: %q", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"title":"Big Fish","year":2003,"genres":["Drama","Mystery"],"tmdbId":587,"added":"0001-01-01T00:00:00Z"},{"id":4,"title":"Big Fish Too","year":2010,"hasFile":true,"monitored":true,"qualityProfileId":6}]`))
		case "/api/v3/qualityprofile":
			w.Write([]byte(`[{"id":6,"name":"HD"},{"id":7}]`))
		case "/api/v3/rootfolder":
			w.Write([]byte(`[{"id":1,"path":"/nas/films"}]`))
		case "/api/v3/movie":
			json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(201)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := NewArr("Radarr", srv.URL, "k")
	got, err := a.Lookup("big fish")
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].InLibrary || got[0].Added != nil || got[0].Raw == nil || !got[1].InLibrary || !got[1].HaveFiles || got[1].Raw != nil {
		t.Fatalf("lookup flags wrong: %+v", got)
	}
	if got[1].ID != 4 || got[1].Monitored == nil || !*got[1].Monitored || got[1].QualityProfileID != 6 {
		t.Fatalf("library fields missing: %+v", got[1])
	}
	if got[0].Monitored != nil {
		t.Fatalf("lookup without monitored should be nil: %v", *got[0].Monitored)
	}
	if len(got[0].Genres) != 2 || got[0].Genres[0] != "Drama" {
		t.Fatalf("reported genres missing: %+v", got[0].Genres)
	}
	if got[1].Genres == nil || len(got[1].Genres) != 0 {
		t.Fatalf("absent genres must stay an empty list, not null: %+v", got[1].Genres)
	}
	b, _ := json.Marshal(got[1])
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if genres, ok := fields["genres"].([]any); !ok || len(genres) != 0 {
		t.Fatalf("absent genres must serialize as []: %v", fields["genres"])
	}
	where, err := a.Add(got[0].Raw)
	if err != nil || where != "/nas/films" {
		t.Fatal(where, err)
	}
	if posted["tmdbId"] != 587.0 || posted["qualityProfileId"] != 6.0 || posted["rootFolderPath"] != "/nas/films" || posted["monitored"] != true {
		t.Fatalf("added item wrong: %v", posted)
	}
}

func TestMediaMonitoredDecodeAndJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"id":1,"title":"Show Absent"},
			{"id":2,"title":"Show False","monitored":false},
			{"id":3,"title":"Show True","monitored":true}
		]`))
	}))
	defer srv.Close()

	a := NewArr("Sonarr", srv.URL, "k")
	items, err := a.Library()
	if err != nil {
		t.Fatalf("Library: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Absent monitoring
	if items[0].Monitored != nil {
		t.Fatalf("items[0].Monitored must be nil for absent monitored, got: %v", *items[0].Monitored)
	}
	b0, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var m0 map[string]any
	if err := json.Unmarshal(b0, &m0); err != nil {
		t.Fatal(err)
	}
	if v, exists := m0["monitored"]; !exists || v != nil {
		t.Fatalf("absent monitored must serialize as null, got: %v (json: %s)", v, string(b0))
	}

	// Explicit false monitoring
	if items[1].Monitored == nil || *items[1].Monitored != false {
		t.Fatalf("items[1].Monitored must be false, got: %v", items[1].Monitored)
	}
	b1, err := json.Marshal(items[1])
	if err != nil {
		t.Fatal(err)
	}
	var m1 map[string]any
	if err := json.Unmarshal(b1, &m1); err != nil {
		t.Fatal(err)
	}
	if v, ok := m1["monitored"].(bool); !ok || v != false {
		t.Fatalf("monitored false must serialize as false, got: %v", m1["monitored"])
	}

	// Explicit true monitoring
	if items[2].Monitored == nil || *items[2].Monitored != true {
		t.Fatalf("items[2].Monitored must be true, got: %v", items[2].Monitored)
	}
	b2, err := json.Marshal(items[2])
	if err != nil {
		t.Fatal(err)
	}
	var m2 map[string]any
	if err := json.Unmarshal(b2, &m2); err != nil {
		t.Fatal(err)
	}
	if v, ok := m2["monitored"].(bool); !ok || v != true {
		t.Fatalf("monitored true must serialize as true, got: %v", m2["monitored"])
	}
}
