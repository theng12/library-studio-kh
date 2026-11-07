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

func TestAddSelectedMonitoredAndSearchPayloads(t *testing.T) {
	bTrue := true
	bFalse := false

	tests := []struct {
		app            string
		postPath       string
		qualityPath    string
		rootPath       string
		metaPath       string
		raw            json.RawMessage
		sel            AddSelection
		wantMonitored  bool
		wantSearch     bool
		wantMonitorOpt string // empty for Radarr
	}{
		// Sonarr default: monitored nil -> true, search default false
		{
			app:            "Sonarr",
			postPath:       "/api/v3/series",
			qualityPath:    "/api/v3/qualityprofile",
			rootPath:       "/api/v3/rootfolder",
			raw:            json.RawMessage(`{"title":"Show","tvdbId":10}`),
			sel:            AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/tv"},
			wantMonitored:  true,
			wantSearch:     false,
			wantMonitorOpt: "all",
		},
		// Sonarr explicit monitored=true, search=true
		{
			app:            "Sonarr",
			postPath:       "/api/v3/series",
			qualityPath:    "/api/v3/qualityprofile",
			rootPath:       "/api/v3/rootfolder",
			raw:            json.RawMessage(`{"title":"Show","tvdbId":10}`),
			sel:            AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/tv", Monitored: &bTrue, Search: true},
			wantMonitored:  true,
			wantSearch:     true,
			wantMonitorOpt: "all",
		},
		// Sonarr unmonitored: monitored=false, search=false -> monitor "none", search false
		{
			app:            "Sonarr",
			postPath:       "/api/v3/series",
			qualityPath:    "/api/v3/qualityprofile",
			rootPath:       "/api/v3/rootfolder",
			raw:            json.RawMessage(`{"title":"Show","tvdbId":10}`),
			sel:            AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/tv", Monitored: &bFalse, Search: false},
			wantMonitored:  false,
			wantSearch:     false,
			wantMonitorOpt: "none",
		},
		// Radarr default: monitored nil -> true, search default false
		{
			app:           "Radarr",
			postPath:      "/api/v3/movie",
			qualityPath:   "/api/v3/qualityprofile",
			rootPath:      "/api/v3/rootfolder",
			raw:           json.RawMessage(`{"title":"Movie","tmdbId":20}`),
			sel:           AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/movies"},
			wantMonitored: true,
			wantSearch:    false,
		},
		// Radarr explicit monitored=true, search=true
		{
			app:           "Radarr",
			postPath:      "/api/v3/movie",
			qualityPath:   "/api/v3/qualityprofile",
			rootPath:      "/api/v3/rootfolder",
			raw:           json.RawMessage(`{"title":"Movie","tmdbId":20}`),
			sel:           AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/movies", Monitored: &bTrue, Search: true},
			wantMonitored: true,
			wantSearch:    true,
		},
		// Radarr unmonitored: monitored=false, search=false
		{
			app:           "Radarr",
			postPath:      "/api/v3/movie",
			qualityPath:   "/api/v3/qualityprofile",
			rootPath:      "/api/v3/rootfolder",
			raw:           json.RawMessage(`{"title":"Movie","tmdbId":20}`),
			sel:           AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/movies", Monitored: &bFalse, Search: false},
			wantMonitored: false,
			wantSearch:    false,
		},
		// Lidarr default: monitored nil -> true, search default false
		{
			app:            "Lidarr",
			postPath:       "/api/v1/artist",
			qualityPath:    "/api/v1/qualityprofile",
			rootPath:       "/api/v1/rootfolder",
			metaPath:       "/api/v1/metadataprofile",
			raw:            json.RawMessage(`{"artistName":"Artist","foreignArtistId":"30"}`),
			sel:            AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/music", MetadataProfileID: 5},
			wantMonitored:  true,
			wantSearch:     false,
			wantMonitorOpt: "all",
		},
		// Lidarr explicit monitored=true, search=true
		{
			app:            "Lidarr",
			postPath:       "/api/v1/artist",
			qualityPath:    "/api/v1/qualityprofile",
			rootPath:       "/api/v1/rootfolder",
			metaPath:       "/api/v1/metadataprofile",
			raw:            json.RawMessage(`{"artistName":"Artist","foreignArtistId":"30"}`),
			sel:            AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/music", MetadataProfileID: 5, Monitored: &bTrue, Search: true},
			wantMonitored:  true,
			wantSearch:     true,
			wantMonitorOpt: "all",
		},
		// Lidarr unmonitored: monitored=false, search=false -> monitor "none", search false
		{
			app:            "Lidarr",
			postPath:       "/api/v1/artist",
			qualityPath:    "/api/v1/qualityprofile",
			rootPath:       "/api/v1/rootfolder",
			metaPath:       "/api/v1/metadataprofile",
			raw:            json.RawMessage(`{"artistName":"Artist","foreignArtistId":"30"}`),
			sel:            AddSelection{QualityProfileID: 1, RootFolderPath: "/nas/music", MetadataProfileID: 5, Monitored: &bFalse, Search: false},
			wantMonitored:  false,
			wantSearch:     false,
			wantMonitorOpt: "none",
		},
	}

	for _, tc := range tests {
		var posted map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case tc.qualityPath:
				w.Write([]byte(`[{"id":1,"name":"Standard"}]`))
			case tc.rootPath:
				w.Write([]byte(`[{"id":1,"path":"` + tc.sel.RootFolderPath + `"}]`))
			case tc.metaPath:
				w.Write([]byte(`[{"id":5,"name":"Standard"}]`))
			case tc.postPath:
				json.NewDecoder(r.Body).Decode(&posted)
				w.WriteHeader(http.StatusCreated)
			default:
				http.NotFound(w, r)
			}
		}))

		a := NewArr(tc.app, srv.URL, "k")
		where, err := a.AddSelected(tc.raw, tc.sel)
		srv.Close()
		if err != nil {
			t.Fatalf("%s (%+v): unexpected error: %v", tc.app, tc.sel, err)
		}
		if where != tc.sel.RootFolderPath {
			t.Fatalf("%s: expected folder %s, got %s", tc.app, tc.sel.RootFolderPath, where)
		}

		if posted["monitored"] != tc.wantMonitored {
			t.Fatalf("%s (%+v): want monitored %v, got %v", tc.app, tc.sel, tc.wantMonitored, posted["monitored"])
		}

		addOpts, ok := posted["addOptions"].(map[string]any)
		if !ok {
			t.Fatalf("%s (%+v): missing addOptions in posted: %v", tc.app, tc.sel, posted)
		}

		switch tc.app {
		case "Sonarr":
			if addOpts["searchForMissingEpisodes"] != tc.wantSearch {
				t.Fatalf("Sonarr: want searchForMissingEpisodes %v, got %v", tc.wantSearch, addOpts["searchForMissingEpisodes"])
			}
			if addOpts["monitor"] != tc.wantMonitorOpt {
				t.Fatalf("Sonarr: want monitor %v, got %v", tc.wantMonitorOpt, addOpts["monitor"])
			}
		case "Radarr":
			if addOpts["searchForMovie"] != tc.wantSearch {
				t.Fatalf("Radarr: want searchForMovie %v, got %v", tc.wantSearch, addOpts["searchForMovie"])
			}
		case "Lidarr":
			if addOpts["searchForMissingAlbums"] != tc.wantSearch {
				t.Fatalf("Lidarr: want searchForMissingAlbums %v, got %v", tc.wantSearch, addOpts["searchForMissingAlbums"])
			}
			if addOpts["monitor"] != tc.wantMonitorOpt {
				t.Fatalf("Lidarr: want monitor %v, got %v", tc.wantMonitorOpt, addOpts["monitor"])
			}
		}
	}
}

func TestAddSelectedRejectsSearchWhenUnmonitoredWithoutWriting(t *testing.T) {
	bFalse := false
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	for _, app := range []string{"Sonarr", "Radarr", "Lidarr"} {
		a := NewArr(app, srv.URL, "k")
		raw := json.RawMessage(`{"title":"Item","tmdbId":1}`)
		sel := AddSelection{
			QualityProfileID: 1,
			RootFolderPath:   "/nas/media",
			Monitored:        &bFalse,
			Search:           true,
		}
		_, err := a.AddSelected(raw, sel)
		if err == nil {
			t.Fatalf("%s: expected error when search=true and monitored=false", app)
		}
		if requests != 0 {
			t.Fatalf("%s: invalid search+unmonitored must make 0 requests upstream, made %d", app, requests)
		}
	}
}

func TestLegacyAddPreservesMonitoredAndSearch(t *testing.T) {
	for _, tc := range []struct {
		app         string
		postPath    string
		qualityPath string
		rootPath    string
		metaPath    string
		raw         json.RawMessage
		searchKey   string
	}{
		{"Sonarr", "/api/v3/series", "/api/v3/qualityprofile", "/api/v3/rootfolder", "", json.RawMessage(`{"title":"Show","tvdbId":1}`), "searchForMissingEpisodes"},
		{"Radarr", "/api/v3/movie", "/api/v3/qualityprofile", "/api/v3/rootfolder", "", json.RawMessage(`{"title":"Film","tmdbId":1}`), "searchForMovie"},
		{"Lidarr", "/api/v1/artist", "/api/v1/qualityprofile", "/api/v1/rootfolder", "/api/v1/metadataprofile", json.RawMessage(`{"artistName":"Band","foreignArtistId":"1"}`), "searchForMissingAlbums"},
	} {
		var posted map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case tc.qualityPath:
				w.Write([]byte(`[{"id":1,"name":"Standard"}]`))
			case tc.rootPath:
				w.Write([]byte(`[{"id":1,"path":"/nas/media"}]`))
			case tc.metaPath:
				w.Write([]byte(`[{"id":1,"name":"Standard"}]`))
			case tc.postPath:
				json.NewDecoder(r.Body).Decode(&posted)
				w.WriteHeader(http.StatusCreated)
			default:
				http.NotFound(w, r)
			}
		}))

		a := NewArr(tc.app, srv.URL, "k")
		where, err := a.Add(tc.raw)
		srv.Close()
		if err != nil {
			t.Fatalf("%s: legacy Add: %v", tc.app, err)
		}
		if where != "/nas/media" {
			t.Fatalf("%s: legacy Add where: %s", tc.app, where)
		}
		if posted["monitored"] != true {
			t.Fatalf("%s: legacy Add must post monitored: true, got %v", tc.app, posted["monitored"])
		}
		addOpts, ok := posted["addOptions"].(map[string]any)
		if !ok || addOpts[tc.searchKey] != true {
			t.Fatalf("%s: legacy Add must post %s: true, got %v", tc.app, tc.searchKey, addOpts[tc.searchKey])
		}
		if tc.app != "Radarr" && addOpts["monitor"] != "all" {
			t.Fatalf("%s: legacy Add must post monitor: all, got %v", tc.app, addOpts["monitor"])
		}
	}
}
