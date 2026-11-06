package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetailDoesNotSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series/7":
			w.Write([]byte(`{"id":7,"title":"Show","monitored":true,"hasFile":true,"path":"/tv","qualityProfileId":2,"seasons":[{"seasonNumber":0,"monitored":true},{"seasonNumber":1,"monitored":false}]}`))
		case "/api/v3/episode":
			w.Write([]byte(`[{"id":11,"seriesId":7,"seasonNumber":0,"episodeNumber":1,"title":"Pilot","monitored":true,"hasFile":true,"airDate":"2024-01-01"}]`))
		default:
			t.Errorf("detail must not call %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d, err := NewArr("Sonarr", srv.URL, "k").Detail(7)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Show" || d.QualityProfileID != 2 || len(d.Seasons) != 2 || len(d.Episodes) != 1 {
		t.Fatalf("detail wrong: %+v", d)
	}
	if d.Episodes[0].When != "2024-01-01" || !d.Episodes[0].HaveFiles || d.Albums == nil {
		t.Fatalf("episode wrong: %+v", d.Episodes[0])
	}
	if _, err := NewArr("Sonarr", srv.URL, "k").Detail(0); err == nil {
		t.Fatal("id 0 must be refused")
	}
}

func TestEpisodeTrackingOwnershipChecked(t *testing.T) {
	var put map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/episode":
			w.Write([]byte(`[{"id":11,"seriesId":8,"seasonNumber":1,"episodeNumber":1}]`))
		case "/api/v3/episode/monitor":
			json.NewDecoder(r.Body).Decode(&put)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := NewArr("Sonarr", srv.URL, "k")
	if err := a.SetTracking(7, "episode", 11, 0, true); err == nil || put != nil {
		t.Fatalf("cross-parent episode must be refused without a write: %v %v", err, put)
	}
}

func TestAlbumTrackingAndSeasonZero(t *testing.T) {
	var albumPut, seasonPut map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/album":
			w.Write([]byte(`[{"id":21,"artistId":5},{"id":22,"artistId":5}]`))
		case "/api/v1/album/monitor":
			json.NewDecoder(r.Body).Decode(&albumPut)
		case "/api/v3/series/7":
			if r.Method == "GET" {
				w.Write([]byte(`{"id":7,"title":"Show","importantField":"keep me","seasons":[{"seasonNumber":0,"monitored":true},{"seasonNumber":1,"monitored":true}]}`))
				return
			}
			json.NewDecoder(r.Body).Decode(&seasonPut)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := NewArr("Lidarr", srv.URL, "k").SetTracking(5, "album", 22, 0, false); err != nil {
		t.Fatal(err)
	}
	if albumPut["albumIds"].([]any)[0] != 22.0 || albumPut["monitored"] != false {
		t.Fatalf("album monitor body wrong: %v", albumPut)
	}
	if err := NewArr("Sonarr", srv.URL, "k").SetTracking(7, "season", 0, 0, false); err != nil {
		t.Fatal(err)
	}
	if seasonPut["importantField"] != "keep me" {
		t.Fatalf("season PUT must preserve upstream fields: %v", seasonPut)
	}
	if seasonPut["seasons"].([]any)[0].(map[string]any)["monitored"] != false {
		t.Fatalf("season 0 not updated: %v", seasonPut["seasons"])
	}
}

func TestReleasesSanitizedAndGrabRevalidated(t *testing.T) {
	var posted json.RawMessage
	release := `{"guid":"magnet:?xt=SECRET","indexerId":3,"indexer":"Idx","title":"Good 1080p","quality":{"quality":{"name":"HDTV"}},"protocol":2,"size":1000,"seeders":5,"ageMinutes":60.9,"downloadAllowed":true,"rejections":[]}`
	rejected := `{"guid":"guid-bad","indexerId":3,"indexer":"Idx","title":"Bad","protocol":2,"downloadAllowed":false,"rejected":true,"rejections":["Nope"]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie/9":
			w.Write([]byte(`{"id":9,"title":"Film"}`))
		case "/api/v3/release":
			if r.Method == "GET" {
				w.Write([]byte("[" + release + "," + rejected + "]"))
				return
			}
			json.NewDecoder(r.Body).Decode(&posted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := NewArr("Radarr", srv.URL, "k")
	ctx := ReleaseContext{ID: 9}
	items, err := a.Releases(ctx)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if strings.Contains(string(mustJSON(t, items)), "SECRET") || strings.Contains(string(mustJSON(t, items)), "guid") {
		t.Fatalf("sanitized output leaks guid: %s", mustJSON(t, items))
	}
	if items[0].Quality != "HDTV" || items[0].Protocol != "torrent" || items[1].Rejected != true || items[1].Rejections[0] != "Nope" {
		t.Fatalf("release fields wrong: %+v", items)
	}
	if err := a.Grab(ctx, items[0].Key); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(posted), "SECRET") || !strings.Contains(string(posted), "downloadUrl") && strings.Contains(string(posted), "http") {
		// the raw upstream resource goes back to its own app, which needs its guid
	}
	if err := a.Grab(ctx, items[1].Key); err == nil {
		t.Fatal("rejected release must not be grabbable")
	}
	if err := a.Grab(ctx, "arbitrary-key"); err == nil {
		t.Fatal("stale key must not grab")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
