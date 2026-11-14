package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func locationPost(t *testing.T, s *Server, path string, body map[string]any, token bool) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "http://127.0.0.1:7881"+path, strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Library-Request", "1")
	if token {
		r.Header.Set("X-Library-Token", s.Token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestSetLibraryLocationValidatesAndSaves(t *testing.T) {
	s := folderTestServer(t)
	folder := t.TempDir()
	if w := locationPost(t, s, "/api/library/location", map[string]any{"app": "Sonarr", "id": 9, "path": folder}, true); w.Code != 200 {
		t.Fatalf("set location: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(readFile(t, filepath.Join(s.Home, "locations.json")), folder) {
		t.Error("the chosen folder was not saved")
	}
	file := filepath.Join(t.TempDir(), "a.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, body := range []map[string]any{
		{"app": "Sonarr", "id": 9, "path": file},                // not a folder
		{"app": "Sonarr", "id": 9, "path": "relative/folder"},   // not absolute
		{"app": "Prowlarr", "id": 9, "path": folder},            // not a library app
		{"app": "Sonarr", "id": 0, "path": folder},              // no title
		{"app": "Sonarr", "id": 9, "path": folder + "/missing"}, // does not exist
	} {
		if w := locationPost(t, s, "/api/library/location", body, true); w.Code != 400 {
			t.Errorf("bad choice %v: %d %s", body, w.Code, w.Body.String())
		}
	}
}

func TestOpenLibraryLocationNeverLaunchesForARemoteSession(t *testing.T) {
	s := folderTestServer(t)
	folder := t.TempDir()
	// A phone session has no launch token. It must be told to look on the Mac,
	// never claim a window opened on the host.
	secret := strings.Repeat("s", 64)
	sum := sha256.Sum256([]byte(secret))
	s.access.data.Sessions[hex.EncodeToString(sum[:])] = browserSession{Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	b, _ := json.Marshal(map[string]any{"path": folder})
	req := httptest.NewRequest("POST", "http://127.0.0.1:7881/api/library/open-location", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Library-Request", "1")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: secret})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"remote":true`) {
		t.Errorf("remote answer = %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"opened"`) {
		t.Errorf("remote answer claimed an open: %s", w.Body.String())
	}
	if w := locationPost(t, s, "/api/library/open-location", map[string]any{"path": "relative"}, true); w.Code != 400 {
		t.Errorf("relative open: %d", w.Code)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	if w := locationPost(t, s, "/api/library/open-location", map[string]any{"path": gone}, true); w.Code != 400 {
		t.Errorf("missing folder open: %d", w.Code)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func TestSavingALocationRebuildsThePageCache(t *testing.T) {
	s := folderTestServer(t)
	first := s.pageData()
	folder := t.TempDir()
	if w := locationPost(t, s, "/api/library/location", map[string]any{"app": "Radarr", "id": 3, "path": folder}, true); w.Code != 200 {
		t.Fatalf("set location: %d %s", w.Code, w.Body.String())
	}
	second := s.pageData()
	if !second.BuiltAt.After(first.BuiltAt) {
		t.Errorf("cache was not rebuilt after the location change: %v then %v", first.BuiltAt, second.BuiltAt)
	}
}
