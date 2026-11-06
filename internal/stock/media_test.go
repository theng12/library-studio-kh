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
	if got[1].ID != 4 || !got[1].Monitored || got[1].QualityProfileID != 6 {
		t.Fatalf("library fields missing: %+v", got[1])
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
