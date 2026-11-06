package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseProtocolAndEligibility(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`1`, "usenet"}, {`2`, "torrent"}, {`"torrent"`, "torrent"}, {`"usenet"`, "usenet"}, {`0`, ""}} {
		r := rawRelease{Protocol: json.RawMessage(tc.raw), DownloadAllowed: true}
		if got := r.release("Radarr", ReleaseContext{ID: 1}); got.Protocol != tc.want || got.Rejected {
			t.Fatalf("protocol %s gave %+v", tc.raw, got)
		}
	}
	for _, r := range []rawRelease{{DownloadAllowed: false}, {DownloadAllowed: true, Rejections: []string{"below profile"}}} {
		if !r.release("Radarr", ReleaseContext{ID: 1}).Rejected {
			t.Fatal("ungrabbable release was shown eligible")
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/movie/1" {
			w.Write([]byte(`{"id":1}`))
			return
		}
		w.Write([]byte(`[{"guid":"fixture","size":"invalid-size"}]`))
	}))
	defer upstream.Close()
	if _, err := NewArr("Radarr", upstream.URL, "fixture-key").Releases(ReleaseContext{ID: 1}); err == nil {
		t.Fatal("malformed upstream data was silently reported as no releases")
	}
}
