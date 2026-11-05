package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestGuardAndSecrets(t *testing.T) {
	home := t.TempDir()
	store, _ := config.Open(home)
	c := config.Default()
	c.Sonarr = config.App{URL: "http://127.0.0.1:1", APIKey: "secret-key"}
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	h := s.Handler()
	call := func(host, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://"+host+"/api/state", nil)
		if token != "" {
			r.Header.Set("X-Library-Token", token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if call("127.0.0.1:7000", "").Code != 403 || call("127.0.0.1:7000", "wrong").Code != 403 || call("evil.example:7000", s.Token).Code != 403 {
		t.Fatal("API must refuse missing token, wrong token and foreign host names")
	}
	w := call("127.0.0.1:7000", s.Token)
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret-key") || !strings.Contains(w.Body.String(), `"version":"`) {
		t.Fatalf("state must load without leaking keys: %d %s", w.Code, w.Body.String())
	}
	if r := httptest.NewRecorder(); true {
		h.ServeHTTP(r, httptest.NewRequest("GET", "http://localhost:7000/", nil))
		if r.Code != 200 || !strings.Contains(r.Body.String(), "Library Studio KH") {
			t.Fatalf("dashboard page missing: %d", r.Code)
		}
	}
}
