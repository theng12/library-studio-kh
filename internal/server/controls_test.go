package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"

func TestControlRoutes(t *testing.T) {
	var paused bool
	var added bool
	var addedPayload map[string]any
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
			json.NewDecoder(r.Body).Decode(&addedPayload)
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
	if addedPayload["monitored"] != true {
		t.Fatalf("add default monitored must be true: %v", addedPayload["monitored"])
	}
	if opts, _ := addedPayload["addOptions"].(map[string]any); opts["searchForMovie"] != false {
		t.Fatalf("add default search must be false: %v", opts["searchForMovie"])
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

func TestMediaAddWiring(t *testing.T) {
	var posted map[string]any
	postCalls := 0
	stockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/qualityprofile":
			w.Write([]byte(`[{"id":6,"name":"HD"}]`))
		case "/api/v3/rootfolder":
			w.Write([]byte(`[{"id":1,"path":"/nas/films"}]`))
		case "/api/v3/movie":
			postCalls++
			json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer stockSrv.Close()

	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Radarr = config.App{URL: stockSrv.URL, APIKey: "k"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerControls(mux)
	do := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/media/add", strings.NewReader(body)))
		return w
	}

	// 1. Unmonitored, search false
	unmon := `{"app":"Radarr","raw":{"title":"Film","tmdbId":1},"qualityProfileId":6,"rootFolderPath":"/nas/films","monitored":false,"search":false}`
	if w := do(unmon); w.Code != 200 {
		t.Fatalf("unmonitored add: %d %s", w.Code, w.Body.String())
	}
	if posted["monitored"] != false {
		t.Fatalf("want monitored false, got: %v", posted["monitored"])
	}
	if opts, _ := posted["addOptions"].(map[string]any); opts["searchForMovie"] != false {
		t.Fatalf("want searchForMovie false, got: %v", opts["searchForMovie"])
	}

	// 2. Monitored true, search true
	monSearch := `{"app":"Radarr","raw":{"title":"Film","tmdbId":1},"qualityProfileId":6,"rootFolderPath":"/nas/films","monitored":true,"search":true}`
	if w := do(monSearch); w.Code != 200 {
		t.Fatalf("monitored+search add: %d %s", w.Code, w.Body.String())
	}
	if posted["monitored"] != true {
		t.Fatalf("want monitored true, got: %v", posted["monitored"])
	}
	if opts, _ := posted["addOptions"].(map[string]any); opts["searchForMovie"] != true {
		t.Fatalf("want searchForMovie true, got: %v", opts["searchForMovie"])
	}

	// 3. Invalid: unmonitored with search true -> must fail with 400 and NO upstream post
	beforeCalls := postCalls
	invalid := `{"app":"Radarr","raw":{"title":"Film","tmdbId":1},"qualityProfileId":6,"rootFolderPath":"/nas/films","monitored":false,"search":true}`
	if w := do(invalid); w.Code != 400 || !strings.Contains(w.Body.String(), "cannot search an unmonitored item") {
		t.Fatalf("invalid unmonitored+search must be rejected with 400: %d %s", w.Code, w.Body.String())
	}
	if postCalls != beforeCalls {
		t.Fatalf("invalid request must not write to upstream, calls went from %d to %d", beforeCalls, postCalls)
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

func TestDownloadRemoveKeepsFilesAndRevealIsGuarded(t *testing.T) {
	const otherHash = "fedcba9876543210fedcba9876543210fedcba98"
	src := t.TempDir()
	var deletes []url.Values
	var resumed bool
	savePath := src
	stockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			json.NewEncoder(w).Encode([]map[string]any{{"hash": testHash, "name": "One", "progress": 1, "save_path": savePath}})
		case "/api/v2/torrents/delete":
			r.ParseForm()
			deletes = append(deletes, r.PostForm)
			if r.PostForm.Get("hashes") == otherHash {
				w.WriteHeader(http.StatusInternalServerError)
			}
		case "/api/v2/torrents/start":
			r.ParseForm()
			resumed = r.Form.Get("hashes") == testHash
		default:
			http.NotFound(w, r)
		}
	}))
	defer stockSrv.Close()
	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.QBit.URL = stockSrv.URL
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(store, home).registerControls(mux)
	do := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w
	}

	for _, action := range []string{"remove", "clear"} {
		deletes = nil
		w := do("/api/downloads/action", `{"hashes":["`+testHash+`","`+otherHash+`"],"action":"`+action+`"}`)
		var out struct {
			OK     bool     `json:"ok"`
			Errors []string `json:"errors"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 200 || out.OK || len(out.Errors) != 1 || !strings.Contains(out.Errors[0], otherHash) {
			t.Fatalf("%s must report the one failed hash: %d %s", action, w.Code, w.Body.String())
		}
		if len(deletes) != 2 {
			t.Fatalf("%s must try every hash, got %v", action, deletes)
		}
		for _, f := range deletes {
			if f.Get("deleteFiles") != "false" {
				t.Fatalf("%s must never delete files: %v", action, f)
			}
		}
	}
	if w := do("/api/downloads/action", `{"hashes":["`+testHash+`"],"action":"resume"}`); w.Code != 200 || !resumed || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("resume: %d %s", w.Code, w.Body.String())
	}
	// A fully successful removal is recorded in Activity, naming the item and path.
	deletes = nil
	if w := do("/api/downloads/action", `{"hashes":["`+testHash+`"],"action":"remove"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("single remove: %d %s", w.Code, w.Body.String())
	}
	events, err := os.ReadFile(filepath.Join(home, "removed.json"))
	if err != nil || !strings.Contains(string(events), `"download-removed"`) || !strings.Contains(string(events), `"One"`) || !strings.Contains(string(events), savePath) {
		t.Fatalf("the removal must be recorded in Activity with its name and path: %v %s", err, events)
	}
	deletes = nil
	if w := do("/api/downloads/action", `{"hashes":["`+testHash+`"],"action":"purge"}`); w.Code != 400 || deletes != nil {
		t.Fatalf("unknown action must be refused untouched: %d %s", w.Code, w.Body.String())
	}

	var ran []string
	old := runReveal
	runReveal = func(name string, args ...string) error { ran = append([]string{name}, args...); return nil }
	defer func() { runReveal = old }()
	if runtime.GOOS != "darwin" {
		if w := do("/api/downloads/reveal", `{"hash":"`+testHash+`"}`); w.Code != 400 || ran != nil {
			t.Fatalf("reveal must refuse off macOS: %d %s", w.Code, w.Body.String())
		}
		return
	}
	for name, tc := range map[string]struct{ hash, path string }{
		"not listed": {otherHash, src},
		"relative":   {testHash, "downloads/tv"},
		"empty":      {testHash, ""},
		"missing":    {testHash, filepath.Join(src, "gone")},
	} {
		savePath = tc.path
		if w := do("/api/downloads/reveal", `{"hash":"`+tc.hash+`"}`); w.Code != 400 || ran != nil {
			t.Fatalf("reveal %s must be refused without running anything: %d %s %v", name, w.Code, w.Body.String(), ran)
		}
	}
	file := filepath.Join(src, "One.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	savePath = file
	if w := do("/api/downloads/reveal", `{"hash":"`+strings.ToUpper(testHash)+`"}`); w.Code != 200 || strings.Join(ran, " ") != "open -R "+file {
		t.Fatalf("reveal: %d %s ran=%v", w.Code, w.Body.String(), ran)
	}
}
