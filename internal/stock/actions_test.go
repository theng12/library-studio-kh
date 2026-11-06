package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	hashA = "0123456789abcdef0123456789abcdef01234567"
	hashB = "89abcdef0123456789abcdef0123456789abcdef"
)

func TestTorrentActionUsesQBit5AndFallsBackOnlyOn404(t *testing.T) {
	var calls []string
	legacy := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		calls = append(calls, r.URL.Path+" "+r.Form.Get("hashes"))
		switch r.URL.Path {
		case "/api/v2/torrents/stop", "/api/v2/torrents/start":
			if legacy {
				http.NotFound(w, r)
			}
		case "/api/v2/torrents/pause", "/api/v2/torrents/resume":
			if !legacy {
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	q := NewQBit(srv.URL, "", "")

	if err := q.TorrentAction([]string{hashA, hashB}, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := q.TorrentAction([]string{hashA}, "resume"); err != nil {
		t.Fatal(err)
	}
	legacy = true
	if err := q.TorrentAction([]string{hashA}, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := q.TorrentAction([]string{hashA}, "resume"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/api/v2/torrents/stop " + hashA + "|" + hashB,
		"/api/v2/torrents/start " + hashA,
		"/api/v2/torrents/stop " + hashA,
		"/api/v2/torrents/pause " + hashA,
		"/api/v2/torrents/start " + hashA,
		"/api/v2/torrents/resume " + hashA,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestTorrentActionRejectsBadInput(t *testing.T) {
	q := NewQBit("http://127.0.0.1:1", "", "")
	for _, c := range []struct {
		hashes []string
		action string
	}{
		{nil, "pause"},
		{[]string{"aaa"}, "pause"},
		{[]string{hashA}, "delete"},
		{[]string{strings.Repeat("z", 40)}, "resume"},
	} {
		if err := q.TorrentAction(c.hashes, c.action); err == nil {
			t.Errorf("%v %q should be refused", c.hashes, c.action)
		}
	}
}

func TestTorrentActionReportsUpstreamFailureWithoutLegacyRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := NewQBit(srv.URL, "", "").TorrentAction([]string{hashA}, "pause")
	if err == nil || !strings.Contains(err.Error(), "500") || calls != 1 {
		t.Fatalf("want one 500 report, got %v after %d calls", err, calls)
	}
}

func TestOptionsAndSelectedAddValidatesLiveChoices(t *testing.T) {
	var posted map[string]any
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/qualityprofile":
			w.Write([]byte(`[{"id":6,"name":"HD"},{"id":7,"name":"4K"}]`))
		case "/api/v3/rootfolder":
			w.Write([]byte(`[{"id":1,"path":"/nas/films"}]`))
		case "/api/v3/movie":
			posts++
			json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := NewArr("Radarr", srv.URL, "k")

	o, err := a.Options()
	if err != nil || len(o.QualityProfiles) != 2 || o.QualityProfiles[1].Name != "4K" || len(o.RootFolders) != 1 || len(o.MetadataProfiles) != 0 {
		t.Fatalf("options: %+v %v", o, err)
	}
	raw := json.RawMessage(`{"title":"Big Fish","tmdbId":587}`)
	if _, err := a.AddSelected(raw, AddSelection{QualityProfileID: 99, RootFolderPath: "/nas/films"}); err == nil {
		t.Error("unknown quality profile must be refused")
	}
	if _, err := a.AddSelected(raw, AddSelection{QualityProfileID: 7, RootFolderPath: "/elsewhere"}); err == nil {
		t.Error("unknown root folder must be refused")
	}
	if posts != 0 {
		t.Fatal("nothing may be added before the choices check out")
	}
	where, err := a.AddSelected(raw, AddSelection{QualityProfileID: 7, RootFolderPath: "/nas/films"})
	if err != nil || where != "/nas/films" || posts != 1 {
		t.Fatalf("add: %q %v %d", where, err, posts)
	}
	if posted["qualityProfileId"] != 7.0 || posted["rootFolderPath"] != "/nas/films" || posted["monitored"] != true {
		t.Fatalf("posted: %v", posted)
	}
}

func TestLidarrAddRequiresListedMetadataProfile(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/qualityprofile":
			w.Write([]byte(`[{"id":1,"name":"Lossless"}]`))
		case "/api/v1/rootfolder":
			w.Write([]byte(`[{"id":1,"path":"/nas/music"}]`))
		case "/api/v1/metadataprofile":
			w.Write([]byte(`[{"id":3,"name":"Standard"}]`))
		case "/api/v1/artist":
			posts++
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := NewArr("Lidarr", srv.URL, "k")
	raw := json.RawMessage(`{"artistName":"Band","foreignArtistId":"abc"}`)
	if _, err := a.AddSelected(raw, AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/music"}); err == nil {
		t.Error("Lidarr must require a metadata profile")
	}
	if _, err := a.AddSelected(raw, AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/music", MetadataProfileID: 9}); err == nil {
		t.Error("unknown metadata profile must be refused")
	}
	if posts != 0 {
		t.Fatal("nothing may be added")
	}
	if _, err := a.AddSelected(raw, AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/music", MetadataProfileID: 3}); err != nil || posts != 1 {
		t.Fatalf("add: %v %d", err, posts)
	}
}

func TestSetMonitoredPreservesUpstreamItem(t *testing.T) {
	var put map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/series/12" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"id":12,"title":"Show","monitored":true,"qualityProfileId":6,"tvdbId":99}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&put)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	if err := NewArr("Sonarr", srv.URL, "k").SetMonitored(12, false); err != nil {
		t.Fatal(err)
	}
	if put["monitored"] != false || put["title"] != "Show" || put["qualityProfileId"] != 6.0 || put["tvdbId"] != 99.0 {
		t.Fatalf("PUT lost upstream fields: %v", put)
	}
}
