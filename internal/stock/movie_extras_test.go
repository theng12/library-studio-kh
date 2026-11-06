package stock

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const movieExtrasMovie = `{
	"id":13,"title":"Aquaman","tmdbId":297802,"imdbId":"tt1477834","studio":"Warner Bros.",
	"certification":"PG-13","runtime":143,"status":"released",
	"images":[{"coverType":"poster","url":"/MediaCover/13/poster.jpg?lastWrite=1","remoteUrl":"https://image.tmdb.org/x.jpg"}],
	"ratings":{"imdb":{"value":6.9,"votes":450000},"tmdb":{"value":7.1,"votes":1200},"rottenTomatoes":{"value":6.5,"votes":0}},
	"collection":{"title":"Aquaman Collection","tmdbId":573693}
}`

const movieExtrasCollection = `[{
	"id":10,"tmdbId":573693,"title":"Aquaman Collection","overview":"The Aquaman films.","monitored":true,"missingMovies":1,
	"movies":[
		{"tmdbId":297802,"title":"Aquaman","year":2018,"status":"released","runtime":143,"isExisting":true,"isExcluded":false,"digitalRelease":"2018-12-21T00:00:00Z"},
		{"tmdbId":572802,"title":"Aquaman and the Lost Kingdom","year":2023,"status":"released","runtime":124,"isExisting":true,"isExcluded":false,"digitalRelease":"2023-12-22T00:00:00Z"},
		{"tmdbId":999999,"title":"Aquaman 3","year":2027,"status":"announced","runtime":0,"isExisting":false,"isExcluded":true}
	]
}]`

const movieExtrasCatalog = `[
	{"id":13,"tmdbId":297802,"hasFile":true},
	{"id":14,"tmdbId":572802,"hasFile":false}
]`

func movieExtrasServer(t *testing.T, catalog string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie/13":
			w.Write([]byte(movieExtrasMovie))
		case "/api/v3/collection":
			w.Write([]byte(movieExtrasCollection))
		case "/api/v3/movie":
			w.Write([]byte(catalog))
		default:
			t.Errorf("movie extras must not call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
}

func TestMovieExtrasJoinsCollectionToOwnMovieList(t *testing.T) {
	srv := movieExtrasServer(t, movieExtrasCatalog)
	defer srv.Close()

	x, err := NewArr("Radarr", srv.URL, "k").MovieExtras(13)
	if err != nil {
		t.Fatal(err)
	}
	if x.Studio != "Warner Bros." || x.Certification != "PG-13" || x.RuntimeMinutes == nil || *x.RuntimeMinutes != 143 {
		t.Fatalf("reported facts wrong: %+v", x)
	}
	if !x.Poster || x.TMDBID != 297802 || x.IMDbID != "tt1477834" {
		t.Fatalf("artwork identity wrong: %+v", x)
	}
	if len(x.Ratings) != 3 || x.Ratings[0].Source != "IMDb" || x.Ratings[0].Votes == nil || *x.Ratings[0].Votes != 450000 {
		t.Fatalf("ratings wrong: %+v", x.Ratings)
	}
	c := x.Collection
	if c == nil || c.Title != "Aquaman Collection" || len(c.Members) != 3 || !x.CatalogKnown {
		t.Fatalf("collection wrong: %+v", c)
	}
	if c.OwnedCount != 1 || c.NoFileCount != 1 || c.Missing != 1 {
		t.Fatalf("counts wrong: owned=%d nofile=%d missing=%d", c.OwnedCount, c.NoFileCount, c.Missing)
	}
	own, nofile, missing := c.Members[0], c.Members[1], c.Members[2]
	if own.HaveFile == nil || !*own.HaveFile || !own.InLibrary || own.LibraryID != 13 {
		t.Fatalf("owned member wrong: %+v", own)
	}
	// Radarr's isExisting is true, but the catalog says no file: not owned.
	if nofile.HaveFile == nil || *nofile.HaveFile || !nofile.InLibrary || nofile.LibraryID != 14 {
		t.Fatalf("isExisting must not be read as file available: %+v", nofile)
	}
	if missing.HaveFile == nil || *missing.HaveFile || missing.InLibrary {
		t.Fatalf("missing member wrong: %+v", missing)
	}
	if missing.Status != "announced" || missing.Year == nil || *missing.Year != 2027 || !missing.IsExcluded {
		t.Fatalf("announced member must keep its native facts: %+v", missing)
	}
	if own.ReleaseDate != "2018-12-21" || !own.IsCurrent {
		t.Fatalf("release date and current film wrong: %+v", own)
	}
}

func TestMovieExtrasCatalogFailureStaysUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie/13":
			w.Write([]byte(movieExtrasMovie))
		case "/api/v3/collection":
			w.Write([]byte(movieExtrasCollection))
		case "/api/v3/movie":
			http.Error(w, "down", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	x, err := NewArr("Radarr", srv.URL, "k").MovieExtras(13)
	if err != nil {
		t.Fatal(err)
	}
	if x.CatalogKnown || len(x.Problems) != 1 || !strings.Contains(x.Problems[0], "unknown") {
		t.Fatalf("catalog failure must be explicit: %+v", x)
	}
	for _, m := range x.Collection.Members {
		if m.HaveFile != nil || m.InLibrary {
			t.Fatalf("member state must stay unknown: %+v", m)
		}
	}
	if x.Collection.OwnedCount+x.Collection.NoFileCount+x.Collection.Missing != 0 {
		t.Fatalf("unknown members must not be counted: %+v", x.Collection)
	}
}

func TestMovieExtrasWithoutCollectionKeepsFacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/movie/13" {
			w.Write([]byte(`{"id":13,"title":"Solo Film","overview":"Still here","year":2020,"genres":["Drama"]}`))
			return
		}
		t.Errorf("a film without a collection must not fetch %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	x, err := NewArr("Radarr", srv.URL, "k").MovieExtras(13)
	if err != nil {
		t.Fatal(err)
	}
	if x.Collection != nil || x.Title != "Solo Film" || x.Poster {
		t.Fatalf("absent collection and poster must stay explicit: %+v", x)
	}
}

func TestArtworkUsesTrustedPathAndAPIKey(t *testing.T) {
	var path, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, key = r.URL.Path, r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "text/html") // upstream type is ignored; bytes decide
		w.Write([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))
	}))
	defer srv.Close()

	b, ct, err := NewArr("Radarr", srv.URL, "k").Artwork(13, "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/MediaCover/13/poster.jpg" || key != "k" {
		t.Fatalf("trusted path or key wrong: path=%q key=%q", path, key)
	}
	if ct != "image/jpeg" || len(b) == 0 {
		t.Fatalf("image not returned: type=%q bytes=%d", ct, len(b))
	}
}

func TestArtworkRefusesUnlistedNamesWithoutRequest(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	for _, name := range []string{"", "../movie/13", "poster.png", "https://evil.example/p.jpg"} {
		if _, _, err := NewArr("Radarr", srv.URL, "k").Artwork(13, name); err == nil {
			t.Fatalf("%q must be refused", name)
		}
	}
	if calls != 0 {
		t.Fatalf("refused names must not reach Radarr: %d calls", calls)
	}
}

func TestArtworkDoesNotFollowRedirects(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls++
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer srv.Close()

	if _, _, err := NewArr("Radarr", srv.URL, "k").Artwork(13, "poster.jpg"); err == nil {
		t.Fatal("a redirect must be refused")
	}
	if targetCalls != 0 {
		t.Fatalf("the API key must never follow a redirect: %d target calls", targetCalls)
	}
}

func TestArtworkRefusesNonImageAndOversize(t *testing.T) {
	body := []byte("<html>not an image</html>")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()
	if _, _, err := NewArr("Radarr", srv.URL, "k").Artwork(13, "poster.jpg"); err == nil {
		t.Fatal("HTML must not be presented as artwork")
	}

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("\xff\xd8\xff"))
		w.Write(make([]byte, maxArtworkBytes))
	}))
	defer big.Close()
	if _, _, err := NewArr("Radarr", big.URL, "k").Artwork(13, "poster.jpg"); err == nil {
		t.Fatal("artwork over the bound must be refused")
	}
}

func movieExtrasWithCatalog(t *testing.T, catalog string) MovieExtras {
	srv := movieExtrasServer(t, catalog)
	defer srv.Close()
	x, err := NewArr("Radarr", srv.URL, "k").MovieExtras(13)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestMovieExtrasNullCatalogIsUnknown(t *testing.T) {
	x := movieExtrasWithCatalog(t, `null`)
	if x.CatalogKnown || !strings.Contains(strings.Join(x.Problems, " "), "unknown") {
		t.Fatalf("a null catalog must stay unknown: %+v", x)
	}
	for _, m := range x.Collection.Members {
		if m.HaveFile != nil || m.InLibrary {
			t.Fatalf("member state must stay unknown: %+v", m)
		}
	}
	if x.Collection.OwnedCount+x.Collection.NoFileCount+x.Collection.Missing != 0 {
		t.Fatalf("unknown members must not be counted: %+v", x.Collection)
	}
}

func TestMovieExtrasIncompleteCatalogFailsClosed(t *testing.T) {
	for _, catalog := range []string{
		`[{"id":13,"tmdbId":297802}]`,                  // hasFile missing
		`[{"id":13,"hasFile":true}]`,                   // tmdbId missing
		`[{"id":0,"tmdbId":297802,"hasFile":true}]`,    // id missing
		`[{"id":13,"tmdbId":297802,"hasFile":null}]`,   // hasFile null
		`[{"id":"13","tmdbId":297802,"hasFile":true}]`, // malformed row
	} {
		x := movieExtrasWithCatalog(t, catalog)
		if x.CatalogKnown || !strings.Contains(strings.Join(x.Problems, " "), "unknown") {
			t.Fatalf("incomplete catalog %s must stay unknown: %+v", catalog, x)
		}
		if x.Collection.OwnedCount+x.Collection.NoFileCount+x.Collection.Missing != 0 {
			t.Fatalf("incomplete catalog must not count members: %s %+v", catalog, x.Collection)
		}
	}
}

func TestMovieExtrasEmptyCatalogIsKnownEmpty(t *testing.T) {
	x := movieExtrasWithCatalog(t, `[]`)
	if !x.CatalogKnown || len(x.Problems) != 0 {
		t.Fatalf("an empty array is a known empty library: %+v", x)
	}
	if x.Collection.Missing != 3 || x.Collection.OwnedCount != 0 || x.Collection.NoFileCount != 0 {
		t.Fatalf("counts wrong for empty catalog: %+v", x.Collection)
	}
	for _, m := range x.Collection.Members {
		if m.HaveFile == nil || *m.HaveFile || m.InLibrary {
			t.Fatalf("known-empty catalog means definitely missing: %+v", m)
		}
	}
}
