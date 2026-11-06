package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"librarystudio/internal/config"
)

func TestPosterBlobURLsAreAllowedWithoutOpeningRemoteImageOrigins(t *testing.T) {
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, home)
	r := httptest.NewRequest("GET", "http://127.0.0.1:7000/api/cleanup/status", nil)
	r.Header.Set("X-Library-Token", s.Token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "img-src 'self' blob:", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q is missing %q", csp, want)
		}
	}
	for _, unwanted := range []string{"img-src *", "http://", "https:"} {
		if strings.Contains(csp, unwanted) {
			t.Fatalf("CSP %q opened remote image origins via %q", csp, unwanted)
		}
	}
}
