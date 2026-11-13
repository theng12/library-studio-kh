package library

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"librarystudio/internal/config"
	"librarystudio/internal/metadata"
)

const (
	hashC    = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	hashPrev = "0000000000000000000000000000000000000000"
	hashD    = "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"
	hashE    = "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE"
	sha      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

var (
	verifiedAt = time.Date(2026, 2, 2, 9, 0, 0, 0, time.UTC)
	importedAt = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
)

// rewrite sends every metadata request to a local stub, counting each one.
type rewrite struct {
	to    *url.URL
	calls *int32
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(r.calls, 1)
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return http.DefaultTransport.RoundTrip(req)
}

type failing struct{ calls *int32 }

func (f failing) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(f.calls, 1)
	return nil, errors.New("offline")
}

// metaStub answers TVmaze and iTunes searches and serves poster bytes.
func metaStub(t *testing.T) (*metadata.Client, *int32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Write([]byte("jpeg"))
			return
		}
		w.Write([]byte(`{"image":{"original":"https://posters.example/p.jpg"},"summary":"","genres":["Drama"],"averageRuntime":45,"rating":{"average":7.5}}`))
	}))
	t.Cleanup(srv.Close)
	to, _ := url.Parse(srv.URL)
	calls := new(int32)
	return &metadata.Client{Dir: filepath.Join(t.TempDir(), "metadata"), HTTP: &http.Client{Transport: rewrite{to, calls}}}, calls
}

func writeLedger(t *testing.T, home string, items ...map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"version": 1, "items": items})
	if err := os.WriteFile(filepath.Join(home, cleanupStateFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sonarrLibrary(t *testing.T, historyUp bool) *httptest.Server {
	t.Helper()
	rec := func(hash string, series int, at time.Time, path string) map[string]any {
		return map[string]any{"eventType": "downloadFolderImported", "downloadId": hash, "date": at, "seriesId": series, "data": map[string]any{"importedPath": path}}
	}
	history := map[string]any{"page": 1, "totalRecords": 4, "records": []map[string]any{
		rec(hashC, 3, importedAt, "/Volumes/ATLAS/TV/Copied Show"),
		rec(hashPrev, 3, importedAt.Add(-24*time.Hour), "/old/Copied Show"),
		rec(hashD, 4, importedAt, "/Volumes/ATLAS/TV/Moved Show"),
		rec(hashE, 5, importedAt, "/Volumes/ATLAS/TV/Missing Show"),
	}}
	files := func(n int) map[string]any { return map[string]any{"episodeFileCount": n} }
	series := []map[string]any{
		{"id": 1, "title": "Queued Show", "statistics": files(0)},
		{"id": 2, "title": "Disk Show", "statistics": files(2)},
		{"id": 3, "title": "Copied Show", "year": 2020, "overview": "App overview", "statistics": files(1)},
		{"id": 4, "title": "Moved Show", "statistics": files(1)},
		{"id": 5, "title": "Missing Show", "statistics": files(0)},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			json.NewEncoder(w).Encode(series)
		case "/api/v3/queue":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 0, "records": []map[string]any{}})
		case "/api/v3/history":
			if !historyUp {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			json.NewEncoder(w).Encode(history)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInventoryJoinsProofByMediaID(t *testing.T) {
	srv := sonarrLibrary(t, true)
	home := t.TempDir()
	writeLedger(t, home,
		map[string]any{"hash": hashC, "app": "Sonarr", "state": phaseStaging, "savePath": "/dl/Copied", "importedPath": "/Volumes/ATLAS/TV/Copied Show", "importedAt": importedAt, "verifiedAt": verifiedAt, "files": []map[string]any{{"rel": "", "size": 1, "sha256": sha}}},
		map[string]any{"hash": hashD, "app": "Sonarr", "state": phaseRemoved, "savePath": "/dl/Moved", "importedPath": "/Volumes/ATLAS/TV/Moved Show", "importedAt": importedAt, "files": []map[string]any{{"rel": "a", "size": 1, "sha256": ""}}},
	)
	meta, calls := metaStub(t)
	meta.Lookup(context.Background(), "series", "Copied Show", 2020) // fill the cache: empty summary, one genre, a poster
	before := atomic.LoadInt32(calls)

	items, problems := Inventory(config.Config{Sonarr: config.App{URL: srv.URL, APIKey: "k"}}, home, meta)
	if len(problems) != 0 || len(items) != 5 {
		t.Fatalf("items %d, problems %v", len(items), problems)
	}
	if atomic.LoadInt32(calls) != before {
		t.Fatal("Inventory must read metadata from the cache only")
	}
	by := map[string]Item{}
	for _, it := range items {
		by[it.Title] = it
	}
	for _, tc := range []struct {
		title, status, verified, target, volume, source string
		at                                              time.Time
	}{
		{"Queued Show", "queued", "", "", "", "", time.Time{}},
		{"Disk Show", "on-disk", "", "", "", "", time.Time{}},
		{"Copied Show", "copied", "hash", "/Volumes/ATLAS/TV/Copied Show", "ATLAS", "/dl/Copied", importedAt},
		{"Moved Show", "moved", "size", "/Volumes/ATLAS/TV/Moved Show", "ATLAS", "/dl/Moved", importedAt},
		{"Missing Show", "missing", "", "/Volumes/ATLAS/TV/Missing Show", "ATLAS", "", importedAt},
	} {
		it := by[tc.title]
		if tc.title == "Copied Show" && it.DownloadHash != hashC {
			t.Errorf("the proven item must carry the tracked download hash for verification, got %q", it.DownloadHash)
		}
		if it.Kind != "library" || it.App != "Sonarr" || it.MediaID == 0 || it.ID == "" {
			t.Errorf("%s: identity %+v", tc.title, it)
		}
		if it.Status != tc.status || it.Verified != tc.verified || it.TargetPath != tc.target || it.TargetVolume != tc.volume || it.SourcePath != tc.source || !it.StatusAt.Equal(tc.at) {
			t.Errorf("%s: got status=%q verified=%q target=%q volume=%q source=%q at=%v", tc.title, it.Status, it.Verified, it.TargetPath, it.TargetVolume, it.SourcePath, it.StatusAt)
		}
	}
	c := by["Copied Show"]
	if c.Overview != "App overview" {
		t.Errorf("an empty cached overview replaced the app's: %q", c.Overview)
	}
	if c.PosterPath == "" || c.PosterPath != filepath.Base(c.PosterPath) || !strings.HasSuffix(c.PosterPath, ".jpg") {
		t.Errorf("posterPath must be a bare file name, got %q", c.PosterPath)
	}
	if len(c.Genres) != 1 || c.Genres[0] != "Drama" || c.RuntimeMinutes != 45 || c.Year != 2020 {
		t.Errorf("cache gaps not filled: %+v", c)
	}
}

func TestInventoryUnreadHistoryNeverClaimsQueued(t *testing.T) {
	srv := sonarrLibrary(t, false)
	items, problems := Inventory(config.Config{Sonarr: config.App{URL: srv.URL, APIKey: "k"}}, t.TempDir(), nil)
	if len(items) != 5 || len(problems) == 0 {
		t.Fatalf("history failure must list items and report a problem: %d %v", len(items), problems)
	}
	for _, it := range items {
		want := ""
		if it.Title == "Disk Show" || it.Title == "Copied Show" || it.Title == "Moved Show" {
			want = "on-disk"
		}
		if it.Status != want || it.Verified != "" {
			t.Errorf("%s: status %q verified %q without readable history", it.Title, it.Status, it.Verified)
		}
	}
}

func TestPrefetchCachesOnceThenStaysCacheOnly(t *testing.T) {
	meta, calls := metaStub(t)
	items := []Item{
		{Kind: "library", App: "Sonarr", Title: "Some Show", Year: 2019},
		{Kind: "library", App: "Sonarr", Title: "some show", Year: 2019},
		{Kind: "downloading", App: "Sonarr", Title: "Some.Show.S01.1080p"},
		{Kind: "library", App: "Unknown", Title: "Nope"},
	}
	Prefetch(items, meta)
	if got := atomic.LoadInt32(calls); got != 2 { // one search + one poster
		t.Fatalf("first prefetch made %d requests, want 2", got)
	}
	if info, ok := meta.Cached("series", "Some Show", 2019); !ok || info.PosterPath == "" {
		t.Fatalf("prefetch didn't cache: %+v %v", info, ok)
	}
	if _, ok := meta.Cached("series", "Some.Show.S01.1080p", 0); ok {
		t.Fatal("a release-file name must never be cached")
	}
	Prefetch(items, meta)
	Prefetch(items, nil)
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("cached titles were looked up again: %d requests", got)
	}
}

func TestGatherJoinsSavedCleanupProofAndStaysOffline(t *testing.T) {
	srv, _ := mockStock(t, true)
	home := t.TempDir()
	writeLedger(t, home, map[string]any{"hash": "aaa", "app": "Sonarr", "state": phaseStaging, "savePath": "/dl/old", "importedPath": "/Volumes/ATLAS/TV/Old", "importedAt": importedAt, "verifiedAt": verifiedAt, "files": []map[string]any{{"rel": "", "size": 1, "sha256": sha}}})
	calls := new(int32)
	meta := &metadata.Client{Dir: t.TempDir(), HTTP: &http.Client{Transport: failing{calls}}}
	s := Gather(cfg(srv.URL), home, now, meta)
	by := map[string]Item{}
	for _, it := range s.Items {
		by[it.Title] = it
	}
	old := by["Old Imported"]
	if old.ID != "aaa" || old.Verified != "hash" || !old.VerifiedAt.Equal(importedAt) || old.TargetPath != "/Volumes/ATLAS/TV/Old" || old.TargetVolume != "ATLAS" || old.SourcePath != "/dl/old" {
		t.Fatalf("saved proof not joined: %+v", old)
	}
	fresh := by["New Imported"]
	if fresh.Verified != "" || !fresh.VerifiedAt.IsZero() || fresh.TargetPath != "" || fresh.TargetVolume != "" {
		t.Fatalf("proof invented without a record: %+v", fresh)
	}
	if len(s.Items) == 0 || atomic.LoadInt32(calls) != 0 {
		t.Fatalf("Gather must list items without the network: %d items, %d calls", len(s.Items), *calls)
	}
}

func TestVolumeName(t *testing.T) {
	for in, want := range map[string]string{"": "", "/Volumes/ATLAS/TV/x": "ATLAS", "/Users/me/TV": "Startup disk", "/Volumes": "Startup disk"} {
		if got := volumeName(in); got != want {
			t.Errorf("volumeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInventoryKeepsAVerifiedCopyCopiedWhenAnotherDownloadIsQueued(t *testing.T) {
	queueDownload := hashE
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "title": "Copied Show", "statistics": map[string]any{"episodeFileCount": 1}}})
		case "/api/v3/history":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 1, "records": []map[string]any{
				{"eventType": "downloadFolderImported", "downloadId": hashC, "date": importedAt, "seriesId": 1, "data": map[string]any{"importedPath": "/Volumes/ATLAS/TV/Copied Show"}},
			}})
		case "/api/v3/queue":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 1, "records": []map[string]any{
				{"downloadId": queueDownload, "seriesId": 1},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeLedger(t, home,
		map[string]any{"hash": hashC, "app": "Sonarr", "state": phaseStaging, "savePath": "/dl/Copied", "importedPath": "/Volumes/ATLAS/TV/Copied Show", "importedAt": importedAt, "verifiedAt": verifiedAt, "files": []map[string]any{{"rel": "", "size": 1, "sha256": sha}}},
	)
	c := config.Config{Sonarr: config.App{URL: srv.URL, APIKey: "k"}}
	items, problems := Inventory(c, home, nil)
	if len(problems) != 0 || len(items) != 1 {
		t.Fatalf("items %d, problems %v", len(items), problems)
	}
	if got := items[0].Status; got != "copied" {
		t.Fatalf("a new episode queued for the same show made its verified copy read %q, want copied", got)
	}
	if !items[0].VerifiedAt.Equal(importedAt) {
		t.Fatalf("the one verification time must be the import instant, got %v", items[0].VerifiedAt)
	}
	queueDownload = hashC // the app is now handling the very download that was imported
	items, _ = Inventory(c, home, nil)
	if got := items[0].Status; got != "importing" {
		t.Fatalf("a queued import of that same download reads %q, want importing", got)
	}
}

func TestInventoryShowsActiveWorkAsImportingOrDownloading(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "title": "Fresh Show", "statistics": map[string]any{"episodeFileCount": 0}},
				{"id": 2, "title": "Moving Show", "statistics": map[string]any{"episodeFileCount": 2}},
			})
		case "/api/v3/history":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 0, "records": []map[string]any{}})
		case "/api/v3/queue":
			json.NewEncoder(w).Encode(map[string]any{"page": 1, "totalRecords": 2, "records": []map[string]any{
				{"downloadId": hashC, "seriesId": 1},
				{"downloadId": hashD, "seriesId": 2},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	items, problems := Inventory(config.Config{Sonarr: config.App{URL: srv.URL, APIKey: "k"}}, t.TempDir(), nil)
	if len(problems) != 0 || len(items) != 2 {
		t.Fatalf("items %d, problems %v", len(items), problems)
	}
	by := map[string]Item{}
	for _, it := range items {
		by[it.Title] = it
	}
	if got := by["Fresh Show"].Status; got != "downloading" {
		t.Fatalf("a queued show with no files reads %q, want downloading", got)
	}
	if got := by["Moving Show"].Status; got != "importing" {
		t.Fatalf("a queued show whose files exist reads %q, want importing", got)
	}
}
