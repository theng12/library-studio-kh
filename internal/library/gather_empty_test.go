package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"librarystudio/internal/config"
)

func TestGatherFailedAppRemainsFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Mock service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	s := Gather(config.Config{Sonarr: config.App{URL: srv.URL, APIKey: "fixture"}}, t.TempDir(), time.Now())
	if len(s.Apps) != 1 || s.Apps[0].Name != "Sonarr" || s.Apps[0].OK || s.Apps[0].Error == "" {
		t.Fatalf("failed service must remain visible, got %+v", s.Apps)
	}
	if s.Blocked == "" {
		t.Fatal("failed import history must keep cleanup blocked")
	}
}

func TestGatherUnconfiguredAppsEncodeAsEmptyArray(t *testing.T) {
	// A fresh host has no configured companions. The dashboard maps apps directly.
	s := Gather(config.Config{}, t.TempDir(), time.Now())
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"apps", "items"} {
		if string(wire[field]) != "[]" {
			t.Fatalf("fresh snapshot %s = %s, want []", field, wire[field])
		}
	}
}
