package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"librarystudio/internal/config"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	s.Token = "local-test-bootstrap"
	return s
}

func setupRequest(s *Server, method, path string, body any) *http.Request {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://localhost"+path, bytes.NewReader(data))
	r.RemoteAddr = "127.0.0.1:43210"
	r.Header.Set("X-Library-Token", s.Token)
	r.Header.Set("X-Library-Request", "1")
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestClientProbeNeverForwardsLocalCredentials(t *testing.T) {
	var token, cookie, authorization string
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("X-Library-Token")
		cookie = r.Header.Get("Cookie")
		authorization = r.Header.Get("Authorization")
		if r.URL.Path != "/api/auth/status" {
			t.Error("unexpected probe path")
		}
		json.NewEncoder(w).Encode(map[string]any{"authenticated": false, "setupRequired": false, "minPasswordLength": 6})
	}))
	defer host.Close()
	s := setupTestServer(t)
	r := setupRequest(s, "POST", "/api/setup/client", map[string]string{"url": host.URL})
	r.Header.Set("Cookie", "private-local-cookie")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("client setup returned %d", w.Code)
	}
	if token != "" || cookie != "" || authorization != "" {
		t.Fatal("local credentials reached the selected host")
	}
	if got, err := config.LoadClientHost(s.Home); err != nil || got != host.URL {
		t.Fatal("normal host URL was not persisted")
	}
}

func TestClientProbeRefusesRedirectsAndRemoteModeChanges(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer host.Close()
	s := setupTestServer(t)
	r := setupRequest(s, "POST", "/api/setup/client", map[string]string{"url": host.URL})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 502 || redirected {
		t.Fatal("client probe followed a redirect")
	}
	r = setupRequest(s, "POST", "/api/setup/client", map[string]string{"url": host.URL})
	r.RemoteAddr = "192.0.2.1:43210"
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("remote request changed local client mode")
	}
	if got, err := config.LoadClientHost(s.Home); err != nil || got != "" {
		t.Fatal("failed check persisted client mode")
	}
}

func TestSetupRoutesRequireAuthenticationAndRejectPackageInjection(t *testing.T) {
	s := setupTestServer(t)
	r := httptest.NewRequest("GET", "http://localhost/api/setup/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("setup state was public")
	}
	r = setupRequest(s, "POST", "/api/setup/plan", map[string]any{"apps": []string{"sonarr"}, "officialAssetURL": "https://untrusted.example.test/app.zip"})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("caller could submit a package URL")
	}
	r = setupRequest(s, "GET", "/api/setup/status", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("local owner could not read initial setup")
	}
	var out struct {
		Job struct {
			State string `json:"state"`
		} `json:"job"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Job.State != "not_started" {
		t.Fatal("setup invented prior success")
	}
}
