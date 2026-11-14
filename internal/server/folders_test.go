package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/config"
)

func folderTestServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	store, err := config.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	return New(store, home)
}

func browseDirsRequest(t *testing.T, s *Server, path string) (*httptest.ResponseRecorder, folderListing) {
	t.Helper()
	url := "http://127.0.0.1:7881/api/browse-dirs"
	if path != "" {
		url += "?path=" + queryEscape(path)
	}
	r := httptest.NewRequest("GET", url, nil)
	r.Header.Set("X-Library-Token", s.Token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var got folderListing
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("browse body: %v: %s", err, w.Body.String())
		}
	}
	return w, got
}

func queryEscape(s string) string { return url.QueryEscape(s) }

func TestBrowseDirsListsOnlyFoldersAndSkipsHidden(t *testing.T) {
	s := folderTestServer(t)
	root := t.TempDir()
	for _, name := range []string{"Movies", "Shows"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"_trash", ".cache"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := false
	if err := os.Symlink(filepath.Join(root, "Movies"), filepath.Join(root, "Linked")); err == nil {
		linked = true
	}
	w, got := browseDirsRequest(t, s, root)
	if w.Code != 200 {
		t.Fatalf("browse: %d %s", w.Code, w.Body.String())
	}
	if got.Path != root || got.Parent != filepath.Dir(root) {
		t.Fatalf("path/parent: %q / %q", got.Path, got.Parent)
	}
	names := make([]string, len(got.Dirs))
	for i, dir := range got.Dirs {
		names[i] = dir.Name
		if dir.Path != filepath.Join(root, dir.Name) {
			t.Fatalf("dir path %q does not sit under %q", dir.Path, root)
		}
	}
	want := []string{"Movies", "Shows"}
	if linked {
		want = []string{"Linked", "Movies", "Shows"}
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("dirs = %v, want %v", names, want)
	}
}

func TestBrowseDirsRejectsMissingRelativeAndUnauthenticated(t *testing.T) {
	s := folderTestServer(t)
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), "relative/path", filepath.Join(t.TempDir(), "file.txt")} {
		if strings.HasSuffix(path, "file.txt") {
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		w, _ := browseDirsRequest(t, s, path)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("browse %q: %d %s", path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:7881/api/browse-dirs?path="+queryEscape(t.TempDir()), nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("unauthenticated browse: %d", w.Code)
	}
}

func TestBrowseDirsStartsAtHome(t *testing.T) {
	s := folderTestServer(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder on this machine")
	}
	w, got := browseDirsRequest(t, s, "")
	if w.Code != 200 || got.Path != filepath.Clean(home) {
		t.Fatalf("default browse: %d %q %s", w.Code, got.Path, w.Body.String())
	}
}

func TestPickFolderRemoteSessionNeverBlocksOrShowsNativeUI(t *testing.T) {
	s := folderTestServer(t)
	secret := strings.Repeat("r", 64) // session cookies are exactly 64 characters
	sum := sha256.Sum256([]byte(secret))
	s.access.data.Sessions[hex.EncodeToString(sum[:])] = browserSession{
		Created: time.Now(),
		Expires: time.Now().Add(time.Hour),
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:7881/api/pick-folder", strings.NewReader(`{"prompt":"Choose"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Library-Request", "1")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: secret})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var got map[string]bool
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || !got["remote"] {
		t.Fatalf("remote pick must ask for the in-app browser: %d %s", w.Code, w.Body.String())
	}

	anon := httptest.NewRequest("POST", "http://127.0.0.1:7881/api/pick-folder", strings.NewReader(`{}`))
	anon.Header.Set("Content-Type", "application/json")
	anon.Header.Set("X-Library-Request", "1")
	aw := httptest.NewRecorder()
	s.Handler().ServeHTTP(aw, anon)
	if aw.Code != 403 {
		t.Fatalf("unauthenticated pick: %d", aw.Code)
	}
}
