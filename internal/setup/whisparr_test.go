package setup

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/library"
	"librarystudio/internal/platform"
)

func TestWhisparrRequestRequiresNativeChoices(t *testing.T) {
	makeRequest := func() Request {
		return Request{Apps: []companioninstall.AppID{companioninstall.Whisparr}, AppDir: t.TempDir(), DownloadPath: t.TempDir(), Ports: map[string]int{"whisparr": 6969}, Roots: map[string]string{"whisparr": t.TempDir()}, Categories: map[string]string{"whisparr": "entertainment"}}
	}
	if err := validateRequest(makeRequest()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.Ports["whisparr"] = 0 },
		func(r *Request) { r.Roots["whisparr"] = "relative" },
		func(r *Request) { r.Categories["whisparr"] = "" },
		func(r *Request) { r.Apps = append(r.Apps, companioninstall.Sonarr); r.Ports["sonarr"] = 6969 },
	} {
		r := makeRequest()
		change(&r)
		if validateRequest(r) == nil {
			t.Fatal("invalid native request accepted")
		}
	}
	r := makeRequest()
	r.Apps = []companioninstall.AppID{companioninstall.Recyclarr, companioninstall.Unpackerr}
	r.Ports = nil
	r.Roots = nil
	r.Categories = nil
	if err := validateRequest(r); err != nil {
		t.Fatal("helpers unexpectedly require native choices", err)
	}
}

func TestWhisparrDisabledNeverCallsNativeAPI(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unexpected call", 500) }))
	defer srv.Close()
	c := config.Default()
	c.Whisparr = config.App{URL: srv.URL, APIKey: "whisparr-fixture-key"}
	if c.WhisparrOn {
		t.Fatal("optional app enabled by default")
	}
	if nativePing(c, AppStep{ID: companioninstall.Whisparr, Name: "Whisparr"}) == nil {
		t.Fatal("disabled app looked ready")
	}
	_ = library.CompanionSettings(c)
	if calls != 0 {
		t.Fatal("disabled Whisparr was called")
	}
}

func TestWhisparrConfigureChecksNativeReadinessAndRedactsFailure(t *testing.T) {
	m, _ := managerFixture(t)
	c := m.store.Get()
	c.Whisparr = config.App{APIKey: "whisparr-fixture-key"}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v3/system/status" || r.Header.Get("X-Api-Key") != c.Whisparr.APIKey {
			t.Error("wrong native readiness request")
		}
		http.Error(w, "unavailable "+c.Whisparr.APIKey, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c.Whisparr.URL = srv.URL
	if err := m.store.Save(c); err != nil {
		t.Fatal(err)
	}
	m.job.Baseline = c
	m.job.Candidate = c
	m.job.View.Request.Apps = []companioninstall.AppID{companioninstall.Whisparr}
	m.job.View.Apps = []AppStep{{ID: companioninstall.Whisparr, Name: "Whisparr"}}
	m.configure()
	job, _ := m.Status()
	if calls != 1 || job.State != "blocked" || len(job.Checks) != 1 || job.Checks[0].State != "failed" || m.store.Get() != c {
		t.Fatal("native failure was skipped or saved as ready")
	}
	data, _ := json.Marshal(job)
	if strings.Contains(string(data), c.Whisparr.APIKey) || strings.Contains(safeSetupError(errors.New("failed "+c.Whisparr.APIKey), c), c.Whisparr.APIKey) {
		t.Fatal("setup error exposed API key")
	}
}

func TestWhisparrManagedSetupUsesNativeProfileFoldersAndLinks(t *testing.T) {
	h := newFlowHarness(t)
	arr := newFlowArr("Whisparr")
	key := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v3/") || r.Header.Get("X-Api-Key") != key {
			t.Error("wrong Whisparr API version or credential")
		}
		arr.ServeHTTP(w, r)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	m := h.manager
	req := Request{Apps: []companioninstall.AppID{companioninstall.Whisparr}, AppDir: filepath.Join(h.home, "Applications"), DownloadPath: filepath.Join(h.home, "Downloads"), Ports: map[string]int{"whisparr": port}, Roots: map[string]string{"whisparr": filepath.Join(h.home, "Library", "Optional")}, Categories: map[string]string{"whisparr": "entertainment"}}
	c := h.store.Get()
	c.QBit = config.QBit{URL: h.servers[companioninstall.QBittorrent].URL, Username: "admin", Password: "fixture-password"}
	c.Prowlarr = config.App{URL: h.servers[companioninstall.Prowlarr].URL, APIKey: "prowlarr-fixture"}
	if err := h.store.Save(c); err != nil {
		t.Fatal(err)
	}
	m.job = &savedJob{Version: 1, View: Job{ID: "whisparr-fixture", Request: req, Apps: []AppStep{{ID: companioninstall.Whisparr, Name: "Whisparr", Managed: true, Path: "/fixture/Whisparr.app"}}}, Baseline: c, Candidate: c}
	if err := m.prepareConnection(m.job.View.Apps[0]); err != nil {
		t.Fatal(err)
	}
	key = m.job.Candidate.Whisparr.APIKey
	if key == "" {
		t.Fatal("managed connection was lost")
	}
	// Supply the native target schema reported by the mock Prowlarr instance.
	p := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/applications/schema" {
			schemas := flowApplicationSchemas()
			item := schemas[1]
			item["name"] = "Whisparr"
			item["implementation"] = "Whisparr"
			item["configContract"] = "WhisparrSettings"
			flowJSON(w, []map[string]any{item})
			return
		}
		h.arrs[companioninstall.Prowlarr].ServeHTTP(w, r)
	}))
	defer p.Close()
	c.Prowlarr.URL = p.URL
	if err := h.store.Save(c); err != nil {
		t.Fatal(err)
	}
	m.job.Baseline = c
	m.configure()
	job, _ := m.Status()
	if job.State != "ready" {
		t.Fatalf("mock setup did not finish: %s; checks %+v", job.State, job.Checks)
	}
	after := h.store.Get()
	if !after.WhisparrOn || after.Whisparr.APIKey != key || after.Whisparr.URL != srv.URL || after.QBit != c.QBit {
		t.Fatal("verified connection not saved or existing account changed")
	}
	if len(arr.folders) != 1 || len(arr.clients) != 1 || arr.downloadCfg["enableCompletedDownloadHandling"] != true || arr.clients[0]["removeCompletedDownloads"] != false || len(h.arrs[companioninstall.Prowlarr].apps) != 1 {
		t.Fatal("native configuration was skipped")
	}
	data, _ := json.Marshal(job)
	if strings.Contains(string(data), key) {
		t.Fatal("managed key entered job response")
	}
}

func TestWhisparrStartupIsNativeWhileHelpersRemainManual(t *testing.T) {
	m, _ := managerFixture(t)
	m.SetHostPort(18881)
	m.job.View.Apps = []AppStep{{ID: companioninstall.Whisparr, Name: "Whisparr", Managed: true}, {ID: companioninstall.Recyclarr, Managed: true}, {ID: companioninstall.Unpackerr, Managed: true}}
	calls := 0
	m.installCompanion = func(s platform.CompanionServiceSpec) (platform.CompanionServiceStatus, error) {
		calls++
		if s.ID != platform.CompanionWhisparr {
			t.Fatal("helper startup installed")
		}
		return platform.CompanionServiceStatus{Supported: true, Installed: true, Loaded: true, State: platform.HostServiceLoaded}, nil
	}
	m.installHost = func(platform.HostServiceSpec) (platform.HostServiceStatus, error) {
		return platform.HostServiceStatus{Supported: true, Installed: true, Loaded: true}, nil
	}
	ready, err := m.installStartup()
	if err != nil || !ready || calls != 1 {
		t.Fatal("native optional startup skipped", err)
	}
}
