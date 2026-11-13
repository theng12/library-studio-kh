package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPosterServesOnlyCachedImages(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "metadata", "posters")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.jpg"), []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Home: home}

	for _, tc := range []struct {
		query string
		want  int
	}{
		{"abc.jpg", http.StatusOK},
		{"../settings.json", http.StatusNotFound},
		{"%2Fetc%2Fpasswd", http.StatusNotFound},
		{"abc.txt", http.StatusNotFound},
		{"", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		s.poster(rec, httptest.NewRequest(http.MethodGet, "/api/poster?path="+tc.query, nil))
		if rec.Code != tc.want {
			t.Fatalf("poster(%q) = %d, want %d", tc.query, rec.Code, tc.want)
		}
		if tc.want == http.StatusOK && rec.Body.String() != "jpeg" {
			t.Fatalf("poster body = %q", rec.Body.String())
		}
	}
}
