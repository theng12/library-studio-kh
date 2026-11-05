package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"librarystudio/internal/config"
)

func TestSampleAndAdd(t *testing.T) {
	speeds := []int64{4e6, 0, 30e6}
	i := 0
	var gotLinks string
	var gotFiles int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			json.NewEncoder(w).Encode([]map[string]any{
				{"hash": "d1", "name": "Active", "progress": 0.5, "state": "downloading", "dlspeed": speeds[i]},
				{"hash": "d2", "name": "Done", "progress": 1, "state": "uploading", "dlspeed": 0},
			})
		case "/api/v2/torrents/add":
			r.ParseMultipartForm(1 << 20)
			if r.MultipartForm != nil {
				gotFiles += len(r.MultipartForm.File["torrents"])
			}
			if u := r.FormValue("urls"); u != "" {
				gotLinks = u
			}
			w.Write([]byte("Ok."))
		}
	}))
	defer srv.Close()
	c := config.Default()
	c.QBit.URL = srv.URL
	home := t.TempDir()
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for ; i < len(speeds); i++ {
		if err := Sample(c, home, start.Add(time.Duration(i)*SampleEvery)); err != nil {
			t.Fatal(err)
		}
	}
	// A reading after a long gap counts only one interval, not the gap.
	i = 0
	if err := Sample(c, home, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	m := readSpeeds(home)
	if m["d2"] != nil {
		t.Fatal("finished torrents must not be sampled")
	}
	r := m["d1"]
	if r == nil || r.Seconds != 4*SampleEvery.Seconds() || r.Min != 4e6 || r.Max != 30e6 {
		t.Fatalf("unexpected record %+v", r)
	}
	if r.Bands[0] != SampleEvery.Seconds() || r.Bands[4] != SampleEvery.Seconds() {
		t.Fatalf("bands %v", r.Bands)
	}

	if err := AddTorrents(c, []string{"magnet:?xt=urn:btih:abc", "https://example.org/x.torrent"}, map[string][]byte{"a.torrent": []byte("d4:infoe")}); err != nil {
		t.Fatal(err)
	}
	if gotLinks != "magnet:?xt=urn:btih:abc\nhttps://example.org/x.torrent" || gotFiles != 1 {
		t.Fatalf("qBittorrent got links %q files %d", gotLinks, gotFiles)
	}
}
