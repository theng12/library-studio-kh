package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"librarystudio/internal/metadata"
)

// quietMeta is a metadata client whose network answers are canned, so a test
// never calls a real provider.
func quietMeta(dir string, results string) *metadata.Client {
	return &metadata.Client{Dir: dir, HTTP: &http.Client{Transport: foldertripFunc(func(r *http.Request) (*http.Response, error) {
		body := results
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			body = "jpeg"
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
}

type foldertripFunc func(*http.Request) (*http.Response, error)

func (f foldertripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const inceptionResults = `{"results":[{"artworkUrl100":"https://example.test/poster.jpg","longDescription":"A thief who steals corporate secrets.","releaseDate":"2010-07-16T00:00:00Z","primaryGenreName":"Thriller","trackTimeMillis":8880000}]}`

func localMediaTestFile(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func useLocalFolder(t *testing.T, s *Server, root string) {
	t.Helper()
	c := s.Store.Get()
	c.LibraryPath = root
	if err := s.Store.Save(c); err != nil {
		t.Fatal(err)
	}
}

func TestLocalMediaRowsComeFromTheConfiguredFolder(t *testing.T) {
	s := folderTestServer(t)
	s.Meta = quietMeta(filepath.Join(s.Home, "metadata"), inceptionResults)
	root := t.TempDir()
	localMediaTestFile(t, root, "Inception.2010.1080p.mkv")
	localMediaTestFile(t, root, ".hidden/ignored.mkv")
	useLocalFolder(t, s, root)

	s.refreshLocalMedia()
	items, problems := s.localMediaItems()
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if len(items) != 1 {
		t.Fatalf("local items = %+v, want 1", items)
	}
	item := items[0]
	if item.App != "Local" || item.Kind != "library" || item.Status != "on-disk" {
		t.Errorf("local row = app %q kind %q status %q", item.App, item.Kind, item.Status)
	}
	if item.Title != "Inception" || item.Year != 2010 {
		t.Errorf("local row title/year = %q/%d", item.Title, item.Year)
	}
	if item.TargetPath != filepath.Join(root, "Inception.2010.1080p.mkv") {
		t.Errorf("local row path = %q", item.TargetPath)
	}
	if item.Overview == "" || item.PosterPath == "" {
		t.Errorf("cached provider facts were not applied: %+v", item)
	}
}

func TestLocalMediaMatchIsSavedAndScopedToTheFolder(t *testing.T) {
	s := folderTestServer(t)
	s.Meta = quietMeta(filepath.Join(s.Home, "metadata"), inceptionResults)
	root := t.TempDir()
	media := localMediaTestFile(t, root, "Wrong.Title.2019.mkv")
	useLocalFolder(t, s, root)
	s.refreshLocalMedia()

	post := func(body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "http://127.0.0.1:7881/api/localmedia/match", strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Library-Request", "1")
		r.Header.Set("X-Library-Token", s.Token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}

	start := time.Now()
	if w := post(map[string]any{"path": media, "title": "Inception", "year": 2010}); w.Code != 200 {
		t.Fatalf("match: %d %s", w.Code, w.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(s.Home, "localmedia.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Inception") {
		t.Errorf("match was not saved: %s", b)
	}
	// The handler refreshes in the background; wait for it before reading.
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.local.mu.Lock()
		done := s.local.scanned.After(start) && !s.local.scanning
		s.local.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background refresh did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	items, _ := s.localMediaItems()
	if len(items) != 1 || items[0].Title != "Inception" {
		t.Fatalf("matched row = %+v", items)
	}

	if w := post(map[string]any{"path": filepath.Join(t.TempDir(), "outside.mkv"), "title": "Nope"}); w.Code != 400 {
		t.Errorf("outside-folder match: %d %s", w.Code, w.Body.String())
	}
	if w := post(map[string]any{"path": media, "title": "  "}); w.Code != 400 {
		t.Errorf("empty-title match: %d %s", w.Code, w.Body.String())
	}
}

func TestNoLocalFolderMeansNoRowsAndNoNoise(t *testing.T) {
	s := folderTestServer(t)
	if items, problems := s.localMediaItems(); items != nil || problems != nil {
		t.Fatalf("unset folder produced %v / %v", items, problems)
	}
}

func TestInventoryIncludesLocalMedia(t *testing.T) {
	s := folderTestServer(t)
	s.Meta = quietMeta(filepath.Join(s.Home, "metadata"), inceptionResults)
	root := t.TempDir()
	localMediaTestFile(t, root, "Inception.2010.mkv")
	useLocalFolder(t, s, root)
	s.refreshLocalMedia()

	r := httptest.NewRequest("GET", "http://127.0.0.1:7881/api/library", nil)
	r.Header.Set("X-Library-Token", s.Token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("library: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Items []struct {
			App    string `json:"app"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range got.Items {
		if item.App == "Local" && item.Title == "Inception" && item.Status == "on-disk" {
			found = true
		}
	}
	if !found {
		t.Fatalf("local media row missing from /api/library: %s", w.Body.String())
	}
}
