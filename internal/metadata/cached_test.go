package metadata

import (
	"net/http"
	"sync/atomic"
	"testing"
)

type countingTransport struct{ calls *int32 }

func (c countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(c.calls, 1)
	panic("Cached must never use the network")
}

func TestCachedReadsDiskOnly(t *testing.T) {
	var calls int32
	client := &Client{Dir: t.TempDir(), HTTP: &http.Client{Transport: countingTransport{&calls}}}
	client.writeCache(cacheKey("movie", "Known Film", 2020), Info{Overview: "Saved.", Source: "itunes"})
	var none *Client
	for _, tc := range []struct {
		name   string
		c      *Client
		title  string
		year   int
		hit    bool
		source string
	}{
		{"hit", client, "Known Film", 2020, true, "cache"},
		{"hit ignores title case", client, "known film", 2020, true, "cache"},
		{"miss other year", client, "Known Film", 2021, false, ""},
		{"miss unknown", client, "Unknown Film", 0, false, ""},
		{"blank title", client, " ", 0, false, ""},
		{"nil client", none, "Known Film", 2020, false, ""},
	} {
		info, ok := tc.c.Cached("movie", tc.title, tc.year)
		if ok != tc.hit || info.Source != tc.source {
			t.Errorf("%s: Cached = %+v, %v", tc.name, info, ok)
		}
	}
	if calls != 0 {
		t.Fatalf("Cached made %d HTTP calls, want 0", calls)
	}
}
