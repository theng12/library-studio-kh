package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func cleanupRequest(t *testing.T, s *Server, method, path, body string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "http://127.0.0.1:7000"+path, nil)
	} else {
		r = httptest.NewRequest(method, "http://127.0.0.1:7000"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Library-Token", s.Token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w, w.Body.Bytes()
}

func TestCleanupStatusAndDisabledRun(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)

	w, body := cleanupRequest(t, s, "GET", "/api/cleanup/status", "")
	if w.Code != 200 {
		t.Fatalf("status: %d %s", w.Code, body)
	}
	var status struct {
		Enabled        bool `json:"enabled"`
		PressureFreeGB int  `json:"pressureFreeGB"`
		Phases         []struct {
			Name string `json:"name"`
			Days int    `json:"days"`
		} `json:"phases"`
		Items []any `json:"items"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}
	if status.Enabled || status.PressureFreeGB != 50 || len(status.Phases) != 3 || status.Items == nil {
		t.Fatalf("status contract wrong: %s", body)
	}

	w, body = cleanupRequest(t, s, "POST", "/api/cleanup/run", "")
	if w.Code != 409 || !strings.Contains(string(body), "Turn staged cleanup on") {
		t.Fatalf("run must refuse while off: %d %s", w.Code, body)
	}
	for _, action := range []string{"/api/cleanup/preview", "/api/cleanup/run"} {
		r := httptest.NewRequest("POST", "http://whatever.example:7000"+action, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s must refuse a foreign host without auth: %d", action, w.Code)
		}
	}
	w, body = cleanupRequest(t, s, "POST", "/api/cleanup/restore", `{"hash":"`+strings.Repeat("a", 40)+`"}`)
	if w.Code != 400 || !strings.Contains(string(body), "isn't tracked") {
		t.Fatalf("unknown restore must be refused: %d %s", w.Code, body)
	}
}

func TestCleanupPreviewDoesNothingWhileOff(t *testing.T) {
	home := t.TempDir()
	store, _ := config.Open(home)
	s := New(store, home)
	w, body := cleanupRequest(t, s, "POST", "/api/cleanup/preview", "")
	if w.Code != 200 {
		t.Fatalf("preview with no apps configured should be a clean no-op: %d %s", w.Code, body)
	}
}
