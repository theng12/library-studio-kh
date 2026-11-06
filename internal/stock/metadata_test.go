package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDetailMetadataForEachApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series/7":
			w.Write([]byte(`{"title":"Show","overview":"Series overview","year":2020,"genres":["Drama","Sci-Fi"],"runtime":60,"network":"HBO","certification":"TV-MA","originalLanguage":{"id":1,"name":"English"},"seasons":[]}`))
		case "/api/v3/episode":
			w.Write([]byte(`[]`))
		case "/api/v3/movie/9":
			w.Write([]byte(`{"title":"Film","overview":"Film overview","year":2003,"genres":["Drama"],"runtime":125,"studio":"Big Studio","certification":"PG-13","originalLanguage":{"id":2,"name":"French"}}`))
		case "/api/v1/artist/5":
			w.Write([]byte(`{"artistName":"Band","biography":"Band story","genres":["Rock"]}`))
		case "/api/v1/album":
			w.Write([]byte(`[]`))
		default:
			t.Errorf("detail must not call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	series, err := NewArr("Sonarr", srv.URL, "k").Detail(7)
	if err != nil {
		t.Fatal(err)
	}
	if series.Overview != "Series overview" || series.Year == nil || *series.Year != 2020 ||
		len(series.Genres) != 2 || series.RuntimeMinutes == nil || *series.RuntimeMinutes != 60 ||
		series.Network != "HBO" || series.Certification != "TV-MA" || series.OriginalLanguage != "English" {
		t.Fatalf("series metadata wrong: %+v", series)
	}

	movie, err := NewArr("Radarr", srv.URL, "k").Detail(9)
	if err != nil {
		t.Fatal(err)
	}
	if movie.Overview != "Film overview" || movie.Year == nil || *movie.Year != 2003 ||
		len(movie.Genres) != 1 || movie.RuntimeMinutes == nil || *movie.RuntimeMinutes != 125 ||
		movie.Certification != "PG-13" || movie.OriginalLanguage != "French" {
		t.Fatalf("movie metadata wrong: %+v", movie)
	}
	if movie.Network != "" {
		t.Fatalf("Radarr has no network; studio must not be guessed: %q", movie.Network)
	}

	artist, err := NewArr("Lidarr", srv.URL, "k").Detail(5)
	if err != nil {
		t.Fatal(err)
	}
	if artist.Overview != "Band story" || len(artist.Genres) != 1 || artist.Genres[0] != "Rock" {
		t.Fatalf("artist metadata wrong: %+v", artist)
	}
	if artist.Year != nil || artist.RuntimeMinutes != nil || artist.Network != "" || artist.Certification != "" || artist.OriginalLanguage != "" {
		t.Fatalf("artist film fields must stay unknown: %+v", artist)
	}

	// The normalized JSON carries the agreed names, and never the raw resource.
	var fields map[string]any
	if err := json.Unmarshal(mustJSON(t, movie), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["runtimeMinutes"] != 125.0 || fields["year"] != 2003.0 || fields["originalLanguage"] != "French" {
		t.Fatalf("normalized JSON wrong: %v", fields)
	}
	if _, ok := fields["studio"]; ok {
		t.Fatalf("raw fields must not leak: %v", fields)
	}
}

func TestDetailMetadataAbsentStaysUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series/7":
			w.Write([]byte(`{"title":"Show","seasons":[]}`))
		case "/api/v3/episode":
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d, err := NewArr("Sonarr", srv.URL, "k").Detail(7)
	if err != nil {
		t.Fatal(err)
	}
	if d.Year != nil || d.RuntimeMinutes != nil || d.Network != "" || d.Certification != "" || d.OriginalLanguage != "" || d.Overview != "" {
		t.Fatalf("absent fields must stay unknown: %+v", d)
	}
	if d.Genres == nil || len(d.Genres) != 0 {
		t.Fatalf("genres must be an empty list, not null: %#v", d.Genres)
	}
	var fields map[string]any
	if err := json.Unmarshal(mustJSON(t, d), &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"year", "runtimeMinutes", "network", "certification", "originalLanguage"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("absent %s must be omitted: %v", key, fields)
		}
	}
	if genres, ok := fields["genres"].([]any); !ok || len(genres) != 0 {
		t.Fatalf("genres must serialize as []: %v", fields["genres"])
	}
}

func TestBrowseOriginalLanguageOnlyWhenReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/movie" {
			t.Errorf("library browse must not call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[
			{"id":1,"title":"Known","year":2001,"originalLanguage":{"id":1,"name":"English"}},
			{"id":2,"title":"Unknown","year":2002}
		]`))
	}))
	defer srv.Close()

	items, err := NewArr("Radarr", srv.URL, "k").Library()
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if items[0].OriginalLanguage != "English" || items[1].OriginalLanguage != "" {
		t.Fatalf("browse language wrong: %+v", items)
	}
	if fields := map[string]any{}; json.Unmarshal(mustJSON(t, items[1]), &fields) != nil {
		t.Fatal("browse item must marshal")
	} else if _, ok := fields["originalLanguage"]; ok {
		t.Fatalf("unreported language must be omitted: %v", fields)
	}
}
