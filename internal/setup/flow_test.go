package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/platform"
	"librarystudio/internal/stock"
)

// These tests drive the real Manager and stock HTTP helpers against fake
// qBittorrent/Sonarr/Radarr/Lidarr/Prowlarr endpoints. No native app is
// launched, no launch agent is written and no real user profile is read.
//
// Integration defect found 2026-11-07 (left red for the owner of the code):
// configure calls Arr.DisableRemoveCompletedDownloads twice per managed library
// app, once directly and again through library.ApplyCompanionSettings. The
// helper reports "no qBittorrent client with removal switched on" when the
// matching client is already off, so fresh setup blocks at "Import and naming
// settings" and the post-restart recheck can never reach ready. These tests
// encode the intended end state; they pass once that helper treats an
// already-off matching client as success.

var flowBundleNames = map[companioninstall.AppID]string{
	companioninstall.QBittorrent: "qbittorrent.app",
	companioninstall.Sonarr:      "Sonarr.app",
	companioninstall.Radarr:      "Radarr.app",
	companioninstall.Lidarr:      "Lidarr.app",
	companioninstall.Prowlarr:    "Prowlarr.app",
}

var flowArrNames = map[companioninstall.AppID]string{
	companioninstall.Sonarr:   "Sonarr",
	companioninstall.Radarr:   "Radarr",
	companioninstall.Lidarr:   "Lidarr",
	companioninstall.Prowlarr: "Prowlarr",
}

type flowPackages struct {
	mu        sync.Mutex
	installed map[companioninstall.AppID]bool
	resolved  int
	installs  int
}

func (f *flowPackages) Resolve(_ context.Context, id companioninstall.AppID) (companioninstall.Package, error) {
	f.mu.Lock()
	f.resolved++
	f.mu.Unlock()
	return companioninstall.Package{
		AppID: id, ReleaseVersion: "fixture-1.0", OfficialAssetURL: "https://fixture.invalid/" + string(id),
		ExpectedBytes: 1024, SizeCap: 4096,
	}, nil
}

func (f *flowPackages) Detect(_ context.Context, id companioninstall.AppID, dir string) (companioninstall.DetectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := companioninstall.DetectMissing
	if f.installed[id] {
		state = companioninstall.DetectAdopt
	}
	return companioninstall.DetectResult{AppID: id, State: state, Path: filepath.Join(dir, flowBundleNames[id]), Version: "fixture-1.0"}, nil
}

func (f *flowPackages) Install(_ context.Context, pkg companioninstall.Package, dir string) (companioninstall.InstallResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	f.installed[pkg.AppID] = true
	return companioninstall.InstallResult{AppID: pkg.AppID, Path: filepath.Join(dir, flowBundleNames[pkg.AppID]), Version: "fixture-1.0", State: companioninstall.Installed, OwnerApprovalNeeded: true}, nil
}

type flowQBit struct {
	mu         sync.Mutex
	prefs      map[string]any
	categories map[string]string
	failLogin  bool
}

func newFlowQBit() *flowQBit {
	return &flowQBit{prefs: map[string]any{"save_path": ""}, categories: map[string]string{}}
}

func (f *flowQBit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/auth/login":
		if f.failLogin {
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		w.Write([]byte("Ok."))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/app/version":
		w.Write([]byte("v5.0.0"))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/app/preferences":
		flowJSON(w, f.prefs)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/app/setPreferences":
		_ = r.ParseForm()
		var patch map[string]any
		if json.Unmarshal([]byte(r.Form.Get("json")), &patch) != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		for k, v := range patch {
			f.prefs[k] = v
		}
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/torrents/categories":
		out := map[string]any{}
		for name, path := range f.categories {
			out[name] = map[string]any{"name": name, "savePath": path}
		}
		flowJSON(w, out)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v2/torrents/createCategory":
		f.categories[r.FormValue("category")] = r.FormValue("savePath")
		flowJSON(w, map[string]any{})
	default:
		http.NotFound(w, r)
	}
}

type flowArr struct {
	mu          sync.Mutex
	name        string
	failPing    bool
	quality     []map[string]any
	metadata    []map[string]any
	folders     []map[string]any
	clients     []map[string]any
	apps        []map[string]any
	indexers    []map[string]any
	nextID      int
	downloadCfg map[string]any
	namingCfg   map[string]any
	indexerTest int
}

func newFlowArr(name string) *flowArr {
	a := &flowArr{
		name:        name,
		quality:     []map[string]any{{"id": float64(1), "name": "Lossless"}},
		metadata:    []map[string]any{{"id": float64(3), "name": "Standard"}},
		nextID:      1,
		downloadCfg: map[string]any{"id": float64(1), "enableCompletedDownloadHandling": false},
		namingCfg:   map[string]any{"id": float64(1), "renameEpisodes": false, "renameMovies": false},
	}
	if name == "Prowlarr" {
		a.indexers = []map[string]any{{"id": float64(1), "name": "Fixture indexer", "enable": true, "protocol": "torrent", "fields": []any{}}}
	}
	return a
}

func (f *flowArr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	for _, prefix := range []string{"/api/v3", "/api/v1"} {
		if strings.HasPrefix(path, prefix) {
			path = strings.TrimPrefix(path, prefix)
			break
		}
	}
	switch {
	case r.Method == http.MethodGet && path == "/system/status":
		if f.failPing {
			http.Error(w, "native app unavailable", http.StatusServiceUnavailable)
			return
		}
		flowJSON(w, map[string]any{"version": "fixture-1.0"})
	case r.Method == http.MethodGet && path == "/qualityprofile":
		flowJSON(w, f.quality)
	case r.Method == http.MethodGet && path == "/metadataprofile":
		flowJSON(w, f.metadata)
	case r.Method == http.MethodGet && path == "/rootfolder":
		flowJSON(w, f.folders)
	case r.Method == http.MethodPost && path == "/rootfolder":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		folder := map[string]any{"id": float64(len(f.folders) + 1), "path": body["path"], "accessible": true, "freeSpace": float64(1 << 30)}
		for _, key := range []string{"defaultQualityProfileId", "defaultMetadataProfileId", "name"} {
			if v, ok := body[key]; ok {
				folder[key] = v
			}
		}
		f.folders = append(f.folders, folder)
		flowJSON(w, folder)
	case r.Method == http.MethodGet && path == "/downloadclient/schema":
		flowJSON(w, []map[string]any{flowQBitClientSchema()})
	case r.Method == http.MethodGet && path == "/downloadclient":
		flowJSON(w, f.clients)
	case r.Method == http.MethodPost && path == "/downloadclient/test":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && path == "/downloadclient":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["removeCompletedDownloads"]; !ok {
			body["removeCompletedDownloads"] = true
		}
		body["id"] = float64(len(f.clients) + 1)
		f.clients = append(f.clients, body)
		flowJSON(w, body)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/downloadclient/"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		id := strings.TrimPrefix(path, "/downloadclient/")
		for i, c := range f.clients {
			if fmt.Sprint(c["id"]) == id {
				f.clients[i] = body
			}
		}
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodGet && path == "/config/downloadclient":
		flowJSON(w, f.downloadCfg)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/config/downloadclient/"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.downloadCfg = body
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodGet && path == "/config/naming":
		flowJSON(w, f.namingCfg)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/config/naming/"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.namingCfg = body
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodGet && path == "/applications/schema":
		flowJSON(w, flowApplicationSchemas())
	case r.Method == http.MethodGet && path == "/applications":
		flowJSON(w, f.apps)
	case r.Method == http.MethodPost && path == "/applications/test":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && path == "/applications":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["id"] = float64(len(f.apps) + 1)
		f.apps = append(f.apps, body)
		flowJSON(w, body)
	case r.Method == http.MethodGet && path == "/indexer":
		flowJSON(w, f.indexers)
	case r.Method == http.MethodGet && path == "/indexer/schema":
		flowJSON(w, []map[string]any{})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/indexer/"):
		for _, item := range f.indexers {
			if fmt.Sprint(item["id"]) == strings.TrimPrefix(path, "/indexer/") {
				flowJSON(w, item)
				return
			}
		}
		http.NotFound(w, r)
	case r.Method == http.MethodPost && path == "/indexer/test":
		f.indexerTest++
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func flowJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func flowField(name, kind string, value any, required bool) map[string]any {
	return map[string]any{"name": name, "label": name, "type": kind, "value": value, "required": required}
}

func flowQBitClientSchema() map[string]any {
	return map[string]any{
		"name": "qBittorrent", "implementation": "QBittorrent", "configContract": "QBittorrentSettings",
		"enable": true, "removeCompletedDownloads": true,
		"fields": []any{
			flowField("host", "textbox", "localhost", true),
			flowField("port", "number", float64(8080), true),
			flowField("useSsl", "checkbox", false, true),
			flowField("urlBase", "textbox", "/", false),
			flowField("username", "textbox", "admin", false),
			flowField("password", "password", "", true),
			flowField("category", "select", "", false),
		},
	}
}

func flowApplicationSchemas() []map[string]any {
	out := []map[string]any{}
	for _, name := range []string{"Sonarr", "Radarr", "Lidarr"} {
		out = append(out, map[string]any{
			"name": name, "implementation": name, "configContract": name + "Settings",
			"fields": []any{
				flowField("prowlarrUrl", "textbox", "http://localhost:9696", true),
				flowField("baseUrl", "textbox", "http://localhost:8989", true),
				flowField("apiKey", "textbox", "", true),
				flowField("syncCategories", "select", []any{float64(5000)}, true),
			},
		})
	}
	return out
}

type flowHarness struct {
	t        *testing.T
	home     string
	store    *config.Store
	manager  *Manager
	packages *flowPackages
	qbit     *flowQBit
	arrs     map[companioninstall.AppID]*flowArr
	servers  map[companioninstall.AppID]*httptest.Server
	opened   int

	hostNeedsRestart bool
}

func newFlowHarness(t *testing.T) *flowHarness {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("guided companion setup planning is Apple Silicon macOS only")
	}
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	h := &flowHarness{t: t, home: home, store: store, packages: &flowPackages{installed: map[companioninstall.AppID]bool{}}, arrs: map[companioninstall.AppID]*flowArr{}, servers: map[companioninstall.AppID]*httptest.Server{}}
	h.qbit = newFlowQBit()
	h.servers[companioninstall.QBittorrent] = httptest.NewServer(h.qbit)
	for _, id := range []companioninstall.AppID{companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr, companioninstall.Prowlarr} {
		arr := newFlowArr(flowArrNames[id])
		h.arrs[id] = arr
		h.servers[id] = httptest.NewServer(arr)
	}
	t.Cleanup(func() {
		for _, srv := range h.servers {
			srv.Close()
		}
	})
	h.manager = h.newManager()
	h.manager.SetHostPort(18443)
	h.rebuild()
	return h
}

func (h *flowHarness) newManager() *Manager { return NewManager(h.home, h.store) }

func (h *flowHarness) port(id companioninstall.AppID) int {
	u, err := url.Parse(h.servers[id].URL)
	if err != nil {
		h.t.Fatal(err)
	}
	var n int
	if _, err := fmt.Sscan(u.Port(), &n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func (h *flowHarness) request(startAtLogin bool) Request {
	req := Request{
		Apps:         []companioninstall.AppID{companioninstall.QBittorrent, companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr, companioninstall.Prowlarr},
		AppDir:       filepath.Join(h.home, "Applications"),
		DownloadPath: filepath.Join(h.home, "Downloads"),
		Roots:        map[string]string{"sonarr": filepath.Join(h.home, "Library", "TV"), "radarr": filepath.Join(h.home, "Library", "Films"), "lidarr": filepath.Join(h.home, "Library", "Music")},
		Ports:        map[string]int{},
		Categories:   map[string]string{"sonarr": "tv", "radarr": "movies", "lidarr": "music"},
		RenameTV:     true,
		StartAtLogin: startAtLogin,
	}
	for _, id := range req.Apps {
		req.Ports[string(id)] = h.port(id)
	}
	return req
}

// rebuild points a manager (new or reloaded) at the same fakes.
func (h *flowHarness) rebuild() {
	h.manager.installer = h.packages
	h.manager.openApp = func(context.Context, string, []string) error { h.opened++; return nil }
	h.manager.applicationDirs = nil
	h.manager.installCompanion = func(spec platform.CompanionServiceSpec) (platform.CompanionServiceStatus, error) {
		return platform.CompanionServiceStatus{Supported: true, Installed: true, Loaded: true, ID: spec.ID, State: platform.HostServiceLoaded}, nil
	}
	h.manager.installHost = func(platform.HostServiceSpec) (platform.HostServiceStatus, error) {
		if h.hostNeedsRestart {
			return platform.HostServiceStatus{Supported: true, Installed: true, Loaded: true, NeedsRestart: true, State: platform.HostServiceNeedsRestart}, nil
		}
		return platform.HostServiceStatus{Supported: true, Installed: true, Loaded: true, State: platform.HostServiceRunning}, nil
	}
}

func flowWait(t *testing.T, m *Manager, states ...string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last Job
	for time.Now().Before(deadline) {
		m.mu.Lock()
		busy := m.busy
		m.mu.Unlock()
		job, err := m.Status()
		if err != nil {
			t.Fatalf("setup status: %v", err)
		}
		last = job
		if !busy {
			for _, state := range states {
				if job.State == state {
					return job
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("setup stayed in %q: %s\nchecks: %+v", last.State, last.Detail, last.Checks)
	return last
}

func (h *flowHarness) planAndInstall() Job {
	h.t.Helper()
	if _, err := h.manager.Plan(h.request(false)); err != nil {
		h.t.Fatal(err)
	}
	planned := flowWait(h.t, h.manager, "planned", "failed")
	if planned.State != "planned" {
		h.t.Fatalf("plan failed: %s", planned.Detail)
	}
	if len(planned.Apps) != 5 {
		h.t.Fatalf("planned apps = %d, want 5", len(planned.Apps))
	}
	if _, err := h.manager.Run(planned.ID); err != nil {
		h.t.Fatal(err)
	}
	installed := flowWait(h.t, h.manager, "needs_approval", "failed")
	if installed.State != "needs_approval" {
		h.t.Fatalf("install failed: %s", installed.Detail)
	}
	for _, app := range installed.Apps {
		if !app.Managed || app.State != "needs_open" {
			h.t.Fatalf("%s after install = managed %v state %q, want managed needs_open", app.Name, app.Managed, app.State)
		}
	}
	return installed
}

func (h *flowHarness) openAll(id string) Job {
	h.t.Helper()
	job := flowWait(h.t, h.manager, "needs_approval")
	for _, app := range job.Apps {
		if _, err := h.manager.Open(context.Background(), id, app.ID); err != nil {
			h.t.Fatalf("open %s: %v", app.Name, err)
		}
	}
	return flowWait(h.t, h.manager, "needs_approval")
}

func (h *flowHarness) chooseMusicProfiles(job Job) Job {
	h.t.Helper()
	if _, err := h.manager.ChooseMusicProfiles(job.ID, 1, 3); err != nil {
		h.t.Fatal(err)
	}
	return flowWait(h.t, h.manager, "ready", "blocked", "needs_restart")
}

func (h *flowHarness) candidate() config.Config {
	h.manager.mu.Lock()
	defer h.manager.mu.Unlock()
	return h.manager.job.Candidate
}

func hasFlowCheck(job Job, app, name, state string) bool {
	for _, check := range job.Checks {
		if check.App == app && check.Name == name && check.State == state {
			return true
		}
	}
	return false
}

func TestFreshSetupFiveAppsFromPlanThroughMusicChoiceToReady(t *testing.T) {
	h := newFlowHarness(t)
	before := h.store.Get()

	if _, err := h.manager.Plan(h.request(false)); err != nil {
		t.Fatal(err)
	}
	planned := flowWait(t, h.manager, "planned", "failed")
	if planned.State != "planned" {
		t.Fatalf("plan failed: %s", planned.Detail)
	}
	for _, app := range planned.Apps {
		if app.State != "download" || app.Version != "fixture-1.0" || app.ExpectedBytes != 1024 || app.SizeCap != 4096 {
			t.Fatalf("%s plan step = %+v", app.Name, app)
		}
	}
	if h.store.Get() != before || h.opened != 0 {
		t.Fatal("planning saved unverified connections or opened an app")
	}

	if _, err := h.manager.Run(planned.ID); err != nil {
		t.Fatal(err)
	}
	installed := flowWait(t, h.manager, "needs_approval", "failed")
	if installed.State != "needs_approval" || h.opened != 0 {
		t.Fatalf("install reached %q with %d opens; first launch must be an explicit owner action", installed.State, h.opened)
	}
	if h.store.Get() != before {
		t.Fatal("installation saved unverified native accounts")
	}
	for _, app := range installed.Apps {
		if !app.Managed || app.State != "needs_open" {
			t.Fatalf("%s after install = %+v", app.Name, app)
		}
	}

	for _, app := range installed.Apps {
		opened, err := h.manager.Open(context.Background(), installed.ID, app.ID)
		if err != nil {
			t.Fatalf("open %s: %v", app.Name, err)
		}
		if opened.State == "ready" {
			t.Fatal("opening an app claimed the whole setup was ready")
		}
		for _, step := range opened.Apps {
			if step.ID == app.ID && step.State != "opened" {
				t.Fatalf("%s state after open = %q", app.Name, step.State)
			}
		}
	}
	if h.opened != 5 || h.store.Get() != before {
		t.Fatal("open did not stay an explicit, unverified owner action")
	}

	checking, err := h.manager.Continue(installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checking.State == "ready" {
		t.Fatal("checking connections was reported ready before the live music profile choice")
	}
	waiting := flowWait(t, h.manager, "needs_preferences", "blocked")
	if waiting.State != "needs_preferences" || waiting.MusicProfiles == nil {
		t.Fatalf("music profiles were not requested live: %q %+v", waiting.State, waiting.MusicProfiles)
	}
	if len(waiting.MusicProfiles.QualityProfiles) != 1 || !flowHasOption(waiting.MusicProfiles.QualityProfiles, 1, "Lossless") {
		t.Fatalf("offered quality profiles = %+v", waiting.MusicProfiles.QualityProfiles)
	}
	if len(waiting.MusicProfiles.MetadataProfiles) != 1 || !flowHasOption(waiting.MusicProfiles.MetadataProfiles, 3, "Standard") {
		t.Fatalf("offered metadata profiles = %+v", waiting.MusicProfiles.MetadataProfiles)
	}
	if _, err := h.manager.ChooseMusicProfiles(waiting.ID, 99, 3); err == nil {
		t.Fatal("an unoffered quality profile was accepted")
	}

	ready := h.chooseMusicProfiles(waiting)
	if ready.State != "ready" {
		t.Fatalf("KNOWN DEFECT (setup calls remove-after-import off twice; the second call fails once the flag is already false): setup = %q, detail %s, checks %+v", ready.State, ready.Detail, ready.Checks)
	}

	want := map[string][]string{
		"Native connection":          {"qBittorrent", "Sonarr", "Radarr", "Lidarr", "Prowlarr"},
		"Download folder":            {"qBittorrent"},
		"Library folder":             {"Sonarr", "Radarr", "Lidarr"},
		"Download category":          {"Sonarr", "Radarr", "Lidarr"},
		"Download client":            {"Sonarr", "Radarr", "Lidarr"},
		"Import and naming settings": {"Sonarr", "Radarr", "Lidarr"},
		"Prowlarr link":              {"Sonarr", "Radarr", "Lidarr"},
		"Search sources":             {"Prowlarr"},
	}
	for name, apps := range want {
		for _, app := range apps {
			if !hasFlowCheck(ready, app, name, "passed") {
				t.Fatalf("missing passed %s check for %s: %+v", name, app, ready.Checks)
			}
		}
	}
	for _, check := range ready.Checks {
		if check.State != "passed" {
			t.Fatalf("ready claimed an unpassed check: %+v", check)
		}
	}

	saved := h.store.Get()
	candidate := h.candidate()
	if saved.QBit != candidate.QBit || saved.Sonarr != candidate.Sonarr || saved.Radarr != candidate.Radarr || saved.Lidarr != candidate.Lidarr || saved.Prowlarr != candidate.Prowlarr {
		t.Fatalf("saved connections = %+v, candidate = %+v", saved, candidate)
	}
	if saved.DownloadPath != filepath.Join(h.home, "Downloads") || saved.LibraryPath != filepath.Join(h.home, "Library", "Films") {
		t.Fatalf("saved folders = %q %q", saved.DownloadPath, saved.LibraryPath)
	}

	h.qbit.mu.Lock()
	if got := h.qbit.prefs["save_path"]; got != saved.DownloadPath {
		t.Fatalf("qBittorrent save_path = %v, want %s", got, saved.DownloadPath)
	}
	for _, category := range []string{"tv", "movies", "music"} {
		if h.qbit.categories[category] != saved.DownloadPath {
			t.Fatalf("category %s = %q", category, h.qbit.categories[category])
		}
	}
	h.qbit.mu.Unlock()
	for _, id := range []companioninstall.AppID{companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr} {
		arr := h.arrs[id]
		arr.mu.Lock()
		if len(arr.folders) != 1 {
			t.Fatalf("%s root folders = %+v", arr.name, arr.folders)
		}
		if len(arr.clients) != 1 || arr.clients[0]["removeCompletedDownloads"] != false {
			t.Fatalf("%s download clients = %+v", arr.name, arr.clients)
		}
		if arr.downloadCfg["enableCompletedDownloadHandling"] != true {
			t.Fatalf("%s completed-download handling = %+v", arr.name, arr.downloadCfg)
		}
		arr.mu.Unlock()
	}
	prowlarr := h.arrs[companioninstall.Prowlarr]
	prowlarr.mu.Lock()
	if len(prowlarr.apps) != 3 || prowlarr.indexerTest == 0 {
		t.Fatalf("Prowlarr links = %d, indexer tests = %d", len(prowlarr.apps), prowlarr.indexerTest)
	}
	prowlarr.mu.Unlock()

	sonarr := h.arrs[companioninstall.Sonarr]
	sonarr.mu.Lock()
	if sonarr.namingCfg["renameEpisodes"] != true {
		t.Fatalf("Sonarr renameEpisodes = %+v", sonarr.namingCfg)
	}
	sonarr.mu.Unlock()

	raw, _ := json.Marshal(ready)
	text := string(raw)
	for _, secret := range []string{candidate.QBit.Password, candidate.Sonarr.APIKey, candidate.Radarr.APIKey, candidate.Lidarr.APIKey, candidate.Prowlarr.APIKey} {
		if secret != "" && strings.Contains(text, secret) {
			t.Fatal("a native secret escaped into the public setup job")
		}
	}
}

func flowHasOption(options []stock.Option, id int, name string) bool {
	for _, option := range options {
		if option.ID == id && option.Name == name {
			return true
		}
	}
	return false
}

func TestNativeFailureBlocksWithoutReadyOrSavedConnectionsThenRetrySucceeds(t *testing.T) {
	h := newFlowHarness(t)
	h.arrs[companioninstall.Lidarr].failPing = true
	installed := h.planAndInstall()
	opened := h.openAll(installed.ID)
	before := h.store.Get()

	if _, err := h.manager.Continue(opened.ID); err != nil {
		t.Fatal(err)
	}
	blocked := flowWait(t, h.manager, "blocked", "ready")
	if blocked.State != "blocked" {
		t.Fatalf("a failed native connection was reported %q", blocked.State)
	}
	if hasFlowCheck(blocked, "Lidarr", "Native connection", "passed") {
		t.Fatal("failed native connection was marked passed")
	}
	if !hasFlowCheck(blocked, "Lidarr", "Native connection", "failed") {
		t.Fatalf("missing failed native check: %+v", blocked.Checks)
	}
	if h.store.Get() != before {
		t.Fatal("failed native connection changed Library settings")
	}
	h.qbit.mu.Lock()
	if len(h.qbit.categories) != 0 || h.qbit.prefs["save_path"] != "" {
		t.Fatalf("failed setup wrote qBittorrent: %+v", h.qbit.categories)
	}
	h.qbit.mu.Unlock()
	for _, id := range []companioninstall.AppID{companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr} {
		arr := h.arrs[id]
		arr.mu.Lock()
		if len(arr.folders) != 0 || len(arr.clients) != 0 {
			t.Fatalf("failed setup wrote %s native settings", arr.name)
		}
		arr.mu.Unlock()
	}
	raw, _ := json.Marshal(blocked)
	candidate := h.candidate()
	for _, secret := range []string{candidate.QBit.Password, candidate.Sonarr.APIKey, candidate.Lidarr.APIKey} {
		if secret != "" && strings.Contains(string(raw), secret) {
			t.Fatal("failed job leaked a native secret")
		}
	}

	h.arrs[companioninstall.Lidarr].failPing = false
	if _, err := h.manager.Continue(blocked.ID); err != nil {
		t.Fatal(err)
	}
	waiting := flowWait(t, h.manager, "needs_preferences", "blocked")
	if waiting.State != "needs_preferences" {
		t.Fatalf("KNOWN DEFECT (setup calls remove-after-import off twice; the second call fails once the flag is already false): retry = %q: %s", waiting.State, waiting.Detail)
	}
	ready := h.chooseMusicProfiles(waiting)
	if ready.State != "ready" {
		t.Fatalf("KNOWN DEFECT (setup calls remove-after-import off twice; the second call fails once the flag is already false): retry after native failure = %q: %s", ready.State, ready.Detail)
	}
	if h.store.Get().Lidarr.URL == "" || h.store.Get().QBit.URL == "" {
		t.Fatal("successful retry did not save the live connections")
	}
}

func TestStartupHandoffNeedsRestartThenReady(t *testing.T) {
	h := newFlowHarness(t)
	h.hostNeedsRestart = true

	if _, err := h.manager.Plan(h.request(true)); err != nil {
		t.Fatal(err)
	}
	planned := flowWait(t, h.manager, "planned", "failed")
	if planned.State != "planned" {
		t.Fatalf("plan failed: %s", planned.Detail)
	}
	if _, err := h.manager.Run(planned.ID); err != nil {
		t.Fatal(err)
	}
	installed := flowWait(t, h.manager, "needs_approval", "failed")
	for _, app := range installed.Apps {
		if _, err := h.manager.Open(context.Background(), installed.ID, app.ID); err != nil {
			t.Fatalf("open %s: %v", app.Name, err)
		}
	}
	if _, err := h.manager.Continue(installed.ID); err != nil {
		t.Fatal(err)
	}
	waiting := flowWait(t, h.manager, "needs_preferences", "blocked", "ready")
	if waiting.State != "needs_preferences" {
		t.Fatalf("KNOWN DEFECT (setup calls remove-after-import off twice; the second call fails once the flag is already false): before music choice = %q: %s", waiting.State, waiting.Detail)
	}
	if _, err := h.manager.ChooseMusicProfiles(waiting.ID, 1, 3); err != nil {
		t.Fatal(err)
	}
	restart := flowWait(t, h.manager, "needs_restart", "ready", "blocked")
	if restart.State != "needs_restart" {
		t.Fatalf("KNOWN DEFECT (setup calls remove-after-import off twice; the second call fails once the flag is already false): startup handoff = %q: %s", restart.State, restart.Detail)
	}
	if !hasFlowCheck(restart, "Library", "At-login configuration", "not_checked") {
		t.Fatalf("startup handoff did not stay not_checked: %+v", restart.Checks)
	}

	// A fresh manager simulates quitting and reopening Library after the job was saved.
	loaded := h.newManager()
	h.manager = loaded
	h.hostNeedsRestart = false
	h.rebuild()
	loaded.SetHostPort(18443)
	if _, err := loaded.Continue(restart.ID); err != nil {
		t.Fatal(err)
	}
	after := flowWait(t, loaded, "ready", "blocked")
	if after.State != "ready" {
		applied := ""
		for _, check := range after.Checks {
			if check.State == "failed" {
				applied = check.App + ": " + check.Name + " — " + check.Detail
			}
		}
		t.Fatalf("KNOWN DEFECT: setup must reach ready after the at-login handoff restart, but a recheck failed at %q: %s", after.State, applied)
	}
}
