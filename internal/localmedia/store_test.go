package localmedia

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"librarystudio/internal/metadata"
)

type tripFunc func(*http.Request) (*http.Response, error)

func (f tripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

// cachedMetadata fills the metadata cache for one film through the public
// Lookup path using a fake transport, then makes any further network use panic.
func cachedMetadata(t *testing.T, dir string) *metadata.Client {
	t.Helper()
	result := `{"results":[{"artworkUrl100":"https://example.test/poster.jpg","longDescription":"A thief who steals corporate secrets.","releaseDate":"2010-07-16T00:00:00Z","primaryGenreName":"Thriller","trackTimeMillis":8880000}]}`
	client := &metadata.Client{Dir: dir, HTTP: &http.Client{Transport: tripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			return stubResponse("jpeg"), nil
		}
		return stubResponse(result), nil
	})}}
	if info := client.Lookup(context.Background(), "movie", "Inception", 2010); info.Source != "itunes" {
		t.Fatalf("cache warm-up failed: %+v", info)
	}
	client.HTTP = &http.Client{Transport: tripFunc(func(*http.Request) (*http.Response, error) {
		panic("Items used the network")
	})}
	return client
}

func TestMatchPersistsAndReloads(t *testing.T) {
	home := t.TempDir()
	media := filepath.Join(home, "Wrong Title (2019).mkv")
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Match(media, "The Correct Title", 2019); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "localmedia.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("store mode = %o, want 600", perm)
	}

	reopened, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	items := reopened.Items(nil)
	if len(items) != 1 {
		t.Fatalf("reloaded items = %+v", items)
	}
	item := items[0]
	if item.Path != media || item.Title != "The Correct Title" || item.Year != 2019 {
		t.Errorf("reloaded item = %+v", item)
	}
	if !item.Manual || item.Source != "manual" {
		t.Errorf("reloaded item is not marked manual: %+v", item)
	}
	if err := store.Match("", "Title", 2020); err == nil {
		t.Error("Match accepted an empty path")
	}
}

func TestItemsAppliesManualMatch(t *testing.T) {
	home := t.TempDir()
	media := filepath.Join(home, "Wrong.Title.2019.1080p.mkv")
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Match(media, "Right Title", 2019); err != nil {
		t.Fatal(err)
	}
	scanned := []LocalItem{{Path: media, Title: "Wrong Title", Year: 2019, Source: "filename"}}
	items := store.Items(nil, scanned)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title != "Right Title" || !items[0].Manual || items[0].Source != "manual" {
		t.Errorf("manual match not applied: %+v", items[0])
	}
}

func TestItemsUsesCacheOnlyAndBarePoster(t *testing.T) {
	home := t.TempDir()
	meta := cachedMetadata(t, filepath.Join(home, "metadata"))
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	scanned := []LocalItem{{Path: filepath.Join(home, "Inception.2010.1080p.mkv"), Title: "Inception", Year: 2010, Source: "filename"}}
	items := store.Items(meta, scanned)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	item := items[0]
	if item.Overview != "A thief who steals corporate secrets." {
		t.Errorf("overview = %q", item.Overview)
	}
	if len(item.Genres) != 1 || item.Genres[0] != "Thriller" {
		t.Errorf("genres = %v", item.Genres)
	}
	if item.RuntimeMinutes != 148 {
		t.Errorf("runtime = %d, want 148", item.RuntimeMinutes)
	}
	if item.PosterPath == "" || filepath.Base(item.PosterPath) != item.PosterPath {
		t.Errorf("poster is not a bare file name: %q", item.PosterPath)
	}
	if filepath.Ext(item.PosterPath) != ".jpg" {
		t.Errorf("poster extension = %q", filepath.Ext(item.PosterPath))
	}
}

func TestOpenRejectsBrokenStore(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "localmedia.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(home); err == nil {
		t.Error("Open accepted a broken store")
	}
	if _, err := Open(""); err == nil {
		t.Error("Open accepted an empty home")
	}
}
