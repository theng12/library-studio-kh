package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"librarystudio/internal/config"
)

func TestHealthIsAuthenticatedAndMinimal(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	h := s.guard(mux)
	call := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1:7881/api/health", nil)
		if token != "" {
			r.Header.Set("X-Library-Token", token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if call("").Code != 403 || call("wrong").Code != 403 {
		t.Fatal("health must refuse missing and wrong tokens")
	}
	w := call(s.Token)
	if w.Code != 200 {
		t.Fatalf("health returned %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["ok"] != true || got["version"] == "" {
		t.Fatalf("health must be exactly {ok,version}: %v", got)
	}
}
