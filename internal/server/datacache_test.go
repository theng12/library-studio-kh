package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/library"
)

func TestPageDataUsesTheWarmCacheFile(t *testing.T) {
	s := folderTestServer(t)
	warm := dataSnapshot{
		Items:    []library.Item{{Title: "Cached Show", App: "Sonarr", Kind: "library"}},
		Problems: []string{"one source did not answer"},
		Snapshot: &library.Snapshot{Items: []library.Item{}, Library: "not set"},
		BuiltAt:  time.Now(),
	}
	if err := writeDataCache(filepath.Join(s.Home, dataCacheFile), warm); err != nil {
		t.Fatal(err)
	}
	got := s.pageData()
	if len(got.Items) != 1 || got.Items[0].Title != "Cached Show" {
		t.Fatalf("warm cache items = %+v", got.Items)
	}
	if len(got.Problems) != 1 || got.Snapshot == nil {
		t.Fatalf("warm cache lost its problems or snapshot: %+v", got)
	}
	info, err := os.Stat(filepath.Join(s.Home, dataCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 600", perm)
	}
}

func TestPageDataRebuildsWhenTheCacheIsDamaged(t *testing.T) {
	s := folderTestServer(t)
	if err := os.WriteFile(filepath.Join(s.Home, dataCacheFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := s.pageData()
	if got.Snapshot == nil {
		t.Fatal("a damaged cache produced no snapshot")
	}
	if got.BuiltAt.IsZero() {
		t.Error("a rebuilt snapshot must carry its build time")
	}
}

func TestInvalidateMakesTheNextReadRebuild(t *testing.T) {
	s := folderTestServer(t)
	first := s.pageData()
	s.invalidateData()
	second := s.pageData()
	if !second.BuiltAt.After(first.BuiltAt) && !second.BuiltAt.Equal(first.BuiltAt) {
		t.Errorf("rebuild time went backwards: %v then %v", first.BuiltAt, second.BuiltAt)
	}
	if second.Snapshot == nil {
		t.Error("invalidation produced no snapshot")
	}
}

func TestPageDataServesTheWarmCacheWithoutAskingAnApp(t *testing.T) {
	s := folderTestServer(t)
	hits := 0
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	t.Cleanup(app.Close)
	c := s.Store.Get()
	c.Sonarr = config.App{URL: app.URL, APIKey: "k"}
	if err := s.Store.Save(c); err != nil {
		t.Fatal(err)
	}
	warm := dataSnapshot{
		Items:    []library.Item{{Title: "Cached Show", App: "Sonarr"}},
		Snapshot: &library.Snapshot{Items: []library.Item{}, Library: "reachable"},
		BuiltAt:  time.Now(),
	}
	if err := writeDataCache(filepath.Join(s.Home, dataCacheFile), warm); err != nil {
		t.Fatal(err)
	}
	got := s.pageData()
	if hits != 0 {
		t.Fatalf("the render path asked the app %d time(s)", hits)
	}
	if len(got.Items) != 1 || got.Items[0].Title != "Cached Show" {
		t.Fatalf("warm rows = %+v", got.Items)
	}
}
