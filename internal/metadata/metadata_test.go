package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func iTunesStub(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if r.URL.Path == "/artwork.jpg" {
			w.Write([]byte("jpeg"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"artworkUrl100":"` + "http://" + r.Host + `/artwork.jpg","longDescription":"A film.","releaseDate":"2021-03-01","primaryGenreName":"Drama","trackTimeMillis":7200000}]}`))
	}))
}

func TestLookupFetchesThenCachesAndDownloadsArtwork(t *testing.T) {
	var hits int32
	server := iTunesStub(t, &hits)
	defer server.Close()
	client := &Client{Dir: t.TempDir(), iTunesBase: server.URL, tvmazeBase: server.URL, HTTP: server.Client()}
	first := client.Lookup(context.Background(), "movie", "Example Film", 2021)
	if first.Source != "itunes" || first.Year != 2021 || first.RuntimeMinutes != 120 || len(first.Genres) != 1 {
		t.Fatalf("first lookup = %+v", first)
	}
	if first.PosterPath == "" {
		t.Fatal("artwork was not cached")
	}
	if _, err := os.Stat(first.PosterPath); err != nil {
		t.Fatalf("cached poster missing: %v", err)
	}
	second := client.Lookup(context.Background(), "movie", "Example Film", 2021)
	if second.Source != "cache" || second.Overview != first.Overview {
		t.Fatalf("second lookup = %+v", second)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("provider hits = %d, want 2 (search + artwork) and none for the cached lookup", got)
	}
	if !strings.Contains(second.PosterPath, filepath.Join("posters")) {
		t.Fatalf("cache path = %q", second.PosterPath)
	}
}

func TestLookupFallsBackToAPlaceholderWhenOffline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	address := server.URL
	server.Close()
	client := &Client{Dir: t.TempDir(), iTunesBase: address, tvmazeBase: address, HTTP: &http.Client{}}
	info := client.Lookup(context.Background(), "movie", "Offline Film", 0)
	if info.Source != "placeholder" || info.PosterPath != "" {
		t.Fatalf("offline lookup = %+v", info)
	}
	// The placeholder is cached so the next list render never waits on the network.
	again := client.Lookup(context.Background(), "movie", "Offline Film", 0)
	if again.Source != "cache" {
		t.Fatalf("offline result was not cached: %+v", again)
	}
}

func TestSeriesUseTVmaze(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"image":{"original":"` + "http://" + r.Host + `/poster.jpg"},"summary":"<p>A <b>series</b>.</p>","premiered":"2019-05-01","genres":["Drama"],"averageRuntime":48,"rating":{"average":8.4}}`))
	}))
	defer server.Close()
	client := &Client{Dir: t.TempDir(), iTunesBase: server.URL, tvmazeBase: server.URL, HTTP: server.Client()}
	info := client.Lookup(context.Background(), "series", "Example Series", 0)
	if info.Source != "tvmaze" || info.Year != 2019 || info.Rating != 8.4 || info.RuntimeMinutes != 48 {
		t.Fatalf("series lookup = %+v", info)
	}
	if stripHTML("<p>a <b>b</b></p>") != "a b" {
		t.Fatalf("summary stripping = %q", stripHTML("<p>a <b>b</b></p>"))
	}
}

type unreachable struct{}

func (unreachable) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: no route")
}

func TestUnreachableProviderRaisesOneNoticeAndLogLine(t *testing.T) {
	client := &Client{Dir: t.TempDir(), HTTP: &http.Client{Transport: unreachable{}}}
	if client.Notice() != "" {
		t.Fatal("a client that has not tried yet must not claim an outage")
	}
	info := client.Lookup(context.Background(), "movie", "Example Film", 2021)
	if info.Source != "placeholder" {
		t.Fatalf("lookup = %+v", info)
	}
	off, reason := client.Offline()
	if !off || !strings.Contains(reason, "no route") {
		t.Fatalf("offline=%v reason=%q", off, reason)
	}
	if !strings.Contains(client.Notice(), "Metadata provider unreachable") {
		t.Fatalf("notice = %q", client.Notice())
	}
}
