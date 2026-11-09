package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"librarystudio/internal/config"
)

func TestCleanupScopeKeepsAuthAndRejectsUnknownApp(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	cfg.StagedCleanupOn = true
	cfg.DownloadPath = t.TempDir()
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	server := New(store, home)
	for _, action := range []string{"preview", "run"} {
		path := "/api/cleanup/" + action + "?app=Sonarr"
		unauth := httptest.NewRequest("POST", "http://127.0.0.1:7000"+path, nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, unauth)
		if recorder.Code != 403 {
			t.Fatalf("scoped %s lost authentication: %d", action, recorder.Code)
		}
		w, body := cleanupRequest(t, server, "POST", "/api/cleanup/"+action+"?app=unknown", "")
		if w.Code != 400 {
			t.Fatalf("scoped %s accepted arbitrary app: %d %s", action, w.Code, body)
		}
	}
	w, body := cleanupRequest(t, server, "POST", "/api/cleanup/preview?app=Sonarr", "")
	if w.Code != 200 {
		t.Fatalf("scoped preview: %d %s", w.Code, body)
	}
	var result struct {
		App string `json:"app"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.App != "Sonarr" {
		t.Fatalf("missing explicit scope: %s", body)
	}
}
