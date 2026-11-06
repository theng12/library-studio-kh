package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestMovieExtrasRoutes(t *testing.T) {
	artworkCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie/13":
			w.Write([]byte(`{"id":13,"title":"Aquaman","tmdbId":297802,"studio":"Warner Bros.","genres":["Action"],"images":[{"coverType":"poster"}],"collection":{"title":"Aquaman Collection","tmdbId":573693}}`))
		case "/api/v3/collection":
			w.Write([]byte(`[{"tmdbId":573693,"title":"Aquaman Collection","movies":[{"tmdbId":297802,"title":"Aquaman","year":2018,"isExisting":true}]}]`))
		case "/api/v3/movie":
			w.Write([]byte(`[{"id":13,"tmdbId":297802,"hasFile":true}]`))
		case "/MediaCover/13/poster.jpg":
			artworkCalls++
			if r.Header.Get("X-Api-Key") != "k" {
				t.Error("artwork must carry Radarr's API key server-side")
			}
			w.Write([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"))
		default:
			t.Errorf("unexpected upstream call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Radarr = config.App{URL: srv.URL, APIKey: "k"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerTracking(mux)
	do := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}

	w := do("/api/media/movie-extras?app=Radarr&id=13")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"title":"Aquaman Collection"`) || !strings.Contains(w.Body.String(), `"ownedCount":1`) || !strings.Contains(w.Body.String(), `"genres":["Action"]`) {
		t.Fatalf("extras route: %d %s", w.Code, w.Body.String())
	}
	if w := do("/api/media/movie-extras?app=Sonarr&id=13"); w.Code != 400 {
		t.Fatalf("non-Radarr extras must be refused: %d", w.Code)
	}
	if w := do("/api/media/movie-extras?app=Radarr&id=0"); w.Code != 400 {
		t.Fatalf("invalid id must be refused: %d", w.Code)
	}

	w = do("/api/media/artwork?app=Radarr&id=13&name=poster.jpg")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") || w.Body.Len() == 0 {
		t.Fatalf("artwork route: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if artworkCalls != 1 {
		t.Fatalf("expected one artwork call, got %d", artworkCalls)
	}
	if w := do("/api/media/artwork?app=Radarr&id=13&name=../movie/13"); w.Code != 400 || artworkCalls != 1 {
		t.Fatalf("unlisted artwork must be refused before Radarr: %d calls=%d", w.Code, artworkCalls)
	}
}
