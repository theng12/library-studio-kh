package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"

func TestControlRoutes(t *testing.T) {
	var paused bool
	var added bool
	var put map[string]any
	stockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Write([]byte(`[{"hash":"` + testHash + `","name":"One","progress":0.5}]`))
		case "/api/v2/torrents/stop":
			r.ParseForm()
			paused = r.Form.Get("hashes") == testHash
		case "/api/v3/qualityprofile":
			w.Write([]byte(`[{"id":6,"name":"HD"}]`))
		case "/api/v3/rootfolder":
			w.Write([]byte(`[{"id":1,"path":"/nas/films"}]`))
		case "/api/v3/movie":
			added = true
			w.WriteHeader(http.StatusCreated)
		case "/api/v3/movie/4":
			if r.Method == http.MethodGet {
				w.Write([]byte(`{"id":4,"title":"Big Fish","monitored":true,"tmdbId":587}`))
				return
			}
			json.NewDecoder(r.Body).Decode(&put)
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer stockSrv.Close()

	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.QBit.URL = stockSrv.URL
	c.Radarr = config.App{URL: stockSrv.URL, APIKey: "k"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerControls(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}

	if w := do("GET", "/api/downloads", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"One"`) {
		t.Fatalf("downloads: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/downloads/action", `{"hashes":["`+testHash+`"],"action":"pause"}`); w.Code != 200 || !paused {
		t.Fatalf("pause: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/downloads/action", `{"hashes":["`+testHash+`"],"action":"delete"}`); w.Code != 400 {
		t.Fatalf("delete must be refused: %d %s", w.Code, w.Body.String())
	}
	if w := do("GET", "/api/media/options?app=Radarr", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"rootFolders":[{"id":1,"path":"/nas/films"}]`) {
		t.Fatalf("options: %d %s", w.Code, w.Body.String())
	}
	if w := do("GET", "/api/media/options?app=Prowlarr", ""); w.Code != 400 {
		t.Fatalf("unknown app: %d", w.Code)
	}
	bad := `{"app":"Radarr","raw":{"title":"Big Fish"},"qualityProfileId":99,"rootFolderPath":"/nas/films"}`
	if w := do("POST", "/api/media/add", bad); w.Code != 400 || added {
		t.Fatalf("bad selection must not add: %d %s", w.Code, w.Body.String())
	}
	good := `{"app":"Radarr","raw":{"title":"Big Fish","tmdbId":587},"qualityProfileId":6,"rootFolderPath":"/nas/films"}`
	if w := do("POST", "/api/media/add", good); w.Code != 200 || !added || !strings.Contains(w.Body.String(), `"folder":"/nas/films"`) {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/media/monitor", `{"app":"Radarr","id":4,"monitored":false}`); w.Code != 200 {
		t.Fatalf("monitor: %d %s", w.Code, w.Body.String())
	}
	if put["monitored"] != false || put["title"] != "Big Fish" || put["tmdbId"] != 587.0 {
		t.Fatalf("monitor lost fields: %v", put)
	}
	if w := do("POST", "/api/media/monitor", `{"app":"Radarr","id":0,"monitored":true}`); w.Code != 400 {
		t.Fatalf("missing item must be refused: %d", w.Code)
	}
}

func TestControlRoutesReportFailure(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Radarr = config.App{URL: down.URL, APIKey: "k"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerControls(mux)
	do := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if w := do("/api/downloads"); w.Code != 502 || !strings.Contains(w.Body.String(), "qBittorrent isn't connected") {
		t.Fatalf("absent qBittorrent must be explicit: %d %s", w.Code, w.Body.String())
	}
	if w := do("/api/media/options?app=Radarr"); w.Code != 502 || !strings.Contains(w.Body.String(), "Radarr") {
		t.Fatalf("upstream failure must be explicit: %d %s", w.Code, w.Body.String())
	}
}
