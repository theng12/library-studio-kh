package stock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

func TestArrCompanionSettingsAndPatchesPreserveUnrelatedFields(t *testing.T) {
	var handling, naming, client map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/config/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"id": 4, "enableCompletedDownloadHandling": false, "keepMe": "x"})
		case r.URL.Path == "/api/v3/config/downloadclient/4" && r.Method == "PUT":
			json.NewDecoder(r.Body).Decode(&handling)
		case r.URL.Path == "/api/v3/downloadclient" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 9, "name": "qBit", "implementation": "QBittorrent", "removeCompletedDownloads": true, "host": "keep"},
				{"id": 10, "name": "Other", "implementation": "Other", "removeCompletedDownloads": true},
			})
		case r.URL.Path == "/api/v3/downloadclient/9" && r.Method == "PUT":
			json.NewDecoder(r.Body).Decode(&client)
		case r.URL.Path == "/api/v3/config/naming" && r.Method == "GET":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 2, "renameEpisodes": false, "seasonFolderFormat": "keep"}})
		case r.URL.Path == "/api/v3/config/naming/2" && r.Method == "PUT":
			json.NewDecoder(r.Body).Decode(&naming)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := NewArr("Sonarr", srv.URL, "secret-key")
	got, err := a.CompanionSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Handling || got.HandlingID != 4 || got.RenameEpisodes == nil || *got.RenameEpisodes || got.NamingID != 2 {
		t.Fatalf("readback: %+v", got)
	}
	if len(got.Clients) != 2 || !got.Clients[0].RemoveCompletedDownloads || got.Clients[1].Implementation != "Other" {
		t.Fatalf("clients: %+v", got.Clients)
	}

	if err := a.SetCompletedDownloadHandling(true); err != nil {
		t.Fatal(err)
	}
	if handling["enableCompletedDownloadHandling"] != true || handling["keepMe"] != "x" {
		t.Fatalf("handling PUT must merge, got %v", handling)
	}
	if err := a.DisableRemoveCompletedDownloads(9); err != nil {
		t.Fatal(err)
	}
	if client["removeCompletedDownloads"] != false || client["host"] != "keep" {
		t.Fatalf("client PUT must clear only removal and keep host, got %v", client)
	}
	if err := a.DisableRemoveCompletedDownloads(10); err == nil {
		t.Fatal("a non-qBittorrent client id must not be reported as changed")
	}
	if err := a.SetRename("renameEpisodes", true); err != nil {
		t.Fatal(err)
	}
	if naming["renameEpisodes"] != true || naming["seasonFolderFormat"] != "keep" {
		t.Fatalf("naming PUT must merge, got %v", naming)
	}
}

func TestQBitSeedingReadsAndMergesOnlyAllowlistedFields(t *testing.T) {
	prefs := map[string]any{
		"max_ratio_enabled": false, "max_ratio": 2.0, "max_ratio_act": 0,
		"max_seeding_time_enabled": true, "max_seeding_time": 2880.0,
		"dl_limit": 0.0, "up_limit": 0.0, "save_path": "/keep",
	}
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/version":
			w.Write([]byte("v5.2.3"))
		case "/api/v2/app/preferences":
			json.NewEncoder(w).Encode(prefs)
		case "/api/v2/app/setPreferences":
			var raw map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("json")), &raw); err != nil {
				t.Errorf("setPreferences payload: %v", err)
			}
			posted = raw
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	q := NewQBit(srv.URL, "", "")
	got, err := q.Seeding()
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxRatio == nil || *got.MaxRatio != 2 || got.MaxSeedingTime == nil || *got.MaxSeedingTime != 2880 || got.MaxRatioAct == nil || *got.MaxRatioAct != 0 {
		t.Fatalf("seeding readback: %+v", got)
	}
	if err := q.ApplySeedingPatch(SeedingPatch{MaxRatio: floatPtr(3), MaxSeedingTime: intPtr(1440)}); err != nil {
		t.Fatal(err)
	}
	if posted["max_ratio"] != 3.0 || posted["max_seeding_time"] != 1440.0 {
		t.Fatalf("changed fields missing: %v", posted)
	}
	if posted["dl_limit"] != 0.0 || posted["up_limit"] != 0.0 || posted["save_path"] != "/keep" || posted["max_ratio_act"] != 0.0 {
		t.Fatalf("unrelated preferences were not preserved: %v", posted)
	}
}

func TestQBitSeedingReportsMalformedPreferences(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if _, err := NewQBit(srv.URL, "", "").Seeding(); err == nil {
		t.Fatal("malformed preferences must be an error, not zero values")
	}
}

func TestQBitSeedingRefusesOldWebAPIVersion(t *testing.T) {
	wrote := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/app/version":
			w.Write([]byte("v3.3.0"))
		case "/api/v2/app/preferences":
			w.Write([]byte(`{"max_ratio":2}`))
		case "/api/v2/app/setPreferences":
			wrote = true
		}
	}))
	defer srv.Close()
	if err := NewQBit(srv.URL, "", "").ApplySeedingPatch(SeedingPatch{MaxRatio: floatPtr(3)}); err == nil {
		t.Fatal("old WebUI API must be refused")
	}
	if wrote {
		t.Fatal("old WebUI API must not be written to")
	}
}
