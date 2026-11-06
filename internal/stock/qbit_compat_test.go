package stock

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQBitNoContentResponses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fixture", Path: "/"})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		cookie, err := r.Cookie("SID")
		if err != nil || cookie.Value != "fixture" {
			http.Error(w, "login required", 403)
			return
		}
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			io.WriteString(w, `[{"hash":"abc","name":"Fixture","progress":0.5}]`)
		case "/api/v2/torrents/stop", "/api/v2/torrents/start", "/api/v2/app/setPreferences":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v2/torrents/add":
			io.WriteString(w, "Fails.")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	q := NewQBit(upstream.URL, "fixture-user", "fixture-password")
	items, err := q.Torrents()
	if err != nil || len(items) != 1 {
		t.Fatalf("204 login did not authenticate info: count=%d err=%v", len(items), err)
	}
	if err := q.TorrentAction([]string{strings.Repeat("a", 40)}, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := q.TorrentAction([]string{strings.Repeat("a", 40)}, "resume"); err != nil {
		t.Fatal(err)
	}
	if err := q.SetPrefs(map[string]any{"fixture": true}); err != nil {
		t.Fatal(err)
	}
	if err := q.AddLinks([]string{"https://example.org/fixture.torrent"}); err == nil {
		t.Fatal("200 Fails response was reported as accepted")
	}
}
