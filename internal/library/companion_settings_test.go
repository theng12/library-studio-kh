package library

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func testBool(v bool) *bool        { return &v }
func testFloat(v float64) *float64 { return &v }
func testInt(v int) *int           { return &v }

func companionFixture(t *testing.T) (*httptest.Server, *config.Config, *int) {
	t.Helper()
	prefs := map[string]any{
		"max_ratio_enabled": false, "max_ratio": 2.0, "max_ratio_act": 0,
		"max_seeding_time_enabled": true, "max_seeding_time": 2880.0,
		"dl_limit": 0.0, "up_limit": 0.0,
	}
	var writes int
	handlingOn, removeOn, renameOn := false, true, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/config/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": 4, "enableCompletedDownloadHandling": handlingOn, "keepMe": "x"})
		case r.URL.Path == "/api/v3/config/downloadclient/4" && r.Method == "PUT":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			handlingOn, _ = body["enableCompletedDownloadHandling"].(bool)
			writes++
			json.NewEncoder(w).Encode(body)
		case r.URL.Path == "/api/v3/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 9, "name": "qBit", "implementation": "QBittorrent", "removeCompletedDownloads": removeOn, "host": "keep"},
				{"id": 10, "name": "Deluge", "implementation": "Deluge", "removeCompletedDownloads": true},
			})
		case r.URL.Path == "/api/v3/downloadclient/9" && r.Method == "PUT":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			removeOn, _ = body["removeCompletedDownloads"].(bool)
			writes++
			json.NewEncoder(w).Encode(body)
		case r.URL.Path == "/api/v3/config/naming" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 2, "renameEpisodes": renameOn, "seasonFolderFormat": "keep"}})
		case r.URL.Path == "/api/v3/config/naming/2" && r.Method == "PUT":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			renameOn, _ = body["renameEpisodes"].(bool)
			writes++
			w.Write([]byte(`{}`))
		case r.URL.Path == "/api/v2/app/version" && r.Method == "GET":
			w.Write([]byte("v5.2.3"))
		case r.URL.Path == "/api/v2/app/preferences" && r.Method == "GET":
			json.NewEncoder(w).Encode(prefs)
		case r.URL.Path == "/api/v2/app/setPreferences" && r.Method == "POST":
			var raw map[string]any
			json.Unmarshal([]byte(r.FormValue("json")), &raw)
			prefs = raw
			writes++
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := config.Default()
	c.Sonarr = config.App{URL: srv.URL, APIKey: "sonarr-key"}
	c.QBit = config.QBit{URL: srv.URL}
	return srv, &c, &writes
}

func settingOf(app CompanionApp, key string) (CompanionSetting, bool) {
	for _, s := range app.Settings {
		if s.Key == key {
			return s, true
		}
	}
	return CompanionSetting{}, false
}

func TestCompanionSettingsReportsActualSupportedValues(t *testing.T) {
	_, c, writes := companionFixture(t)
	st := CompanionSettings(*c)
	if *writes != 0 {
		t.Fatalf("reading settings must not write, got %d writes", *writes)
	}
	if len(st.Apps) != 3 || !st.Apps[0].Connected || st.Apps[1].Configured || st.Apps[2].Configured {
		t.Fatalf("apps: %+v", st.Apps)
	}
	handling, ok := settingOf(st.Apps[0], "enableCompletedDownloadHandling")
	if !ok || handling.Value != false || !handling.Editable || handling.ManagedBy != "app" {
		t.Fatalf("handling: %+v", handling)
	}
	rename, ok := settingOf(st.Apps[0], "renameEpisodes")
	if !ok || rename.Value != false || !rename.Editable {
		t.Fatalf("rename: %+v", rename)
	}
	if len(st.Apps[0].Clients) != 2 || !st.Apps[0].Clients[0].CanDisable || st.Apps[0].Clients[0].Reason == "" {
		t.Fatalf("qBittorrent removal must explain why it stays off and be actionable: %+v", st.Apps[0].Clients)
	}
	if st.Apps[0].Clients[1].CanDisable || st.Apps[0].Clients[1].Reason == "" || !strings.Contains(st.Apps[0].Clients[1].Reason, "Open Sonarr") {
		t.Fatalf("unsupported clients must be blocked honestly: %+v", st.Apps[0].Clients[1])
	}
	ratio, ok := settingOf(st.QBit, "max_ratio")
	if !ok || ratio.Value != 2.0 || !ratio.Editable || ratio.ManagedBy != "app" {
		t.Fatalf("qbit ratio: %+v", ratio)
	}
	time, ok := settingOf(st.QBit, "max_seeding_time")
	if !ok || time.Value != 2880.0 || time.Unit != "minutes" {
		t.Fatalf("qbit time: %+v", time)
	}
	act, ok := settingOf(st.QBit, "max_ratio_act")
	if !ok || act.Value != 0 || act.ManagedBy != "library" {
		t.Fatalf("qbit action: %+v", act)
	}
	if st.Policy == "" {
		t.Fatal("policy explanation missing")
	}
}

func TestCompanionSettingsValidationRefusesUnsafeWrites(t *testing.T) {
	_, c, writes := companionFixture(t)
	cases := []struct {
		name  string
		patch CompanionPatch
	}{
		{"unknown app", CompanionPatch{App: "Plex"}},
		{"remove on", CompanionPatch{App: "Sonarr", RemoveCompletedDownloads: testBool(true)}},
		{"rename episodes on radarr", CompanionPatch{App: "Radarr", RenameEpisodes: testBool(true)}},
		{"seeding on sonarr", CompanionPatch{App: "Sonarr", MaxRatio: testFloat(2)}},
		{"empty arr patch", CompanionPatch{App: "Sonarr"}},
		{"empty qbit patch", CompanionPatch{App: "qBittorrent"}},
		{"ratio out of range", CompanionPatch{App: "qBittorrent", MaxRatio: testFloat(2000)}},
		{"time out of range", CompanionPatch{App: "qBittorrent", MaxSeedingTime: testInt(600000)}},
		{"remove action", CompanionPatch{App: "qBittorrent", MaxRatioAct: testInt(3)}},
		{"arr fields on qbit", CompanionPatch{App: "qBittorrent", EnableCompletedDownloadHandling: testBool(true)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ApplyCompanionSettings(*c, tc.patch); err == nil {
				t.Fatal("unsafe or incomplete patch must be refused")
			} else {
				var bad *ValidationError
				if !errors.As(err, &bad) {
					t.Fatalf("must be a validation error, got %T %v", err, err)
				}
			}
		})
	}
	if *writes != 0 {
		t.Fatalf("refused patches must not write, got %d writes", *writes)
	}
}

func TestCompanionApplyWritesThenReadsBack(t *testing.T) {
	_, c, _ := companionFixture(t)
	app, err := ApplyCompanionSettings(*c, CompanionPatch{
		App: "Sonarr", EnableCompletedDownloadHandling: testBool(true), RemoveCompletedDownloads: testBool(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Connected {
		t.Fatalf("app must be connected after apply: %+v", app)
	}
	handling, _ := settingOf(app, "enableCompletedDownloadHandling")
	if handling.Value != true {
		t.Fatalf("handling readback: %+v", handling)
	}

	qapp, err := ApplyCompanionSettings(*c, CompanionPatch{
		App: "qBittorrent", MaxRatio: testFloat(3), MaxRatioAct: testInt(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ratio, _ := settingOf(qapp, "max_ratio")
	if ratio.Value != 3.0 {
		t.Fatalf("qbit ratio readback: %+v", ratio)
	}
}

func TestCompanionApplyRedactsSecretsInUpstreamErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`[{"errorMessage":"bad key supersecret-key"}]`))
	}))
	defer srv.Close()
	c := config.Default()
	c.Sonarr = config.App{URL: srv.URL, APIKey: "supersecret-key"}
	_, err := ApplyCompanionSettings(c, CompanionPatch{App: "Sonarr", EnableCompletedDownloadHandling: testBool(true)})
	if err == nil {
		t.Fatal("upstream error must be returned")
	}
	if strings.Contains(err.Error(), "supersecret-key") || !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("secret must be redacted: %v", err)
	}
}

func TestCompanionApplyReportsPartialStateWithoutRollback(t *testing.T) {
	handlingOn, renameOn := false, false
	var handlingPuts, namingPuts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/config/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": 4, "enableCompletedDownloadHandling": handlingOn})
		case r.URL.Path == "/api/v3/config/downloadclient/4" && r.Method == "PUT":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			handlingOn, _ = body["enableCompletedDownloadHandling"].(bool)
			handlingPuts++
		case r.URL.Path == "/api/v3/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.URL.Path == "/api/v3/config/naming" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 2, "renameEpisodes": renameOn}})
		case r.URL.Path == "/api/v3/config/naming/2" && r.Method == "PUT":
			namingPuts++
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`[{"errorMessage":"naming blocked"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := config.Default()
	c.Sonarr = config.App{URL: srv.URL, APIKey: "sonarr-key"}

	app, err := ApplyCompanionSettings(c, CompanionPatch{
		App: "Sonarr", EnableCompletedDownloadHandling: testBool(true), RenameEpisodes: testBool(true),
	})
	var partial *PartialApplyError
	if !errors.As(err, &partial) {
		t.Fatalf("second write failure must report partial apply, got %T %v", err, err)
	}
	if len(partial.Applied) != 1 || partial.Applied[0] != "enableCompletedDownloadHandling" {
		t.Fatalf("applied fields: %v", partial.Applied)
	}
	if !app.Connected || app.Error != "" {
		t.Fatalf("fresh state must be returned after a partial apply: %+v", app)
	}
	handling, _ := settingOf(app, "enableCompletedDownloadHandling")
	rename, _ := settingOf(app, "renameEpisodes")
	if handling.Value != true || rename.Value != false {
		t.Fatalf("readback must show the real partial state: handling=%+v rename=%+v", handling, rename)
	}
	if handlingPuts != 1 || namingPuts != 1 || !handlingOn {
		t.Fatalf("the first change must stay applied without rollback: handlingPuts=%d namingPuts=%d handlingOn=%v", handlingPuts, namingPuts, handlingOn)
	}
}

func TestCompanionApplyReportsUnverifiedReadback(t *testing.T) {
	prefs := map[string]any{"max_ratio": 2.0, "max_seeding_time": 2880.0, "max_ratio_act": 0}
	wrote := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/version":
			w.Write([]byte("v5.2.3"))
		case "/api/v2/app/preferences":
			if wrote {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(prefs)
		case "/api/v2/app/setPreferences":
			wrote = true
		}
	}))
	defer srv.Close()
	c := config.Default()
	c.QBit = config.QBit{URL: srv.URL}
	app, err := ApplyCompanionSettings(c, CompanionPatch{App: "qBittorrent", MaxRatio: testFloat(3)})
	if err != nil {
		t.Fatal(err)
	}
	if app.Connected || !strings.Contains(app.Error, "Saved, but the new value couldn't be read back") {
		t.Fatalf("unverified readback must be honest: %+v", app)
	}
}
