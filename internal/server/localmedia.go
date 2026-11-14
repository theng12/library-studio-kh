package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"librarystudio/internal/library"
	"librarystudio/internal/localmedia"
	"librarystudio/internal/metadata"
)

const (
	localMediaRefreshEvery = 2 * time.Minute
	localMediaLookupMax    = 40
)

// localMediaCache keeps the last scan of the owner's local media folder, so a
// page render never walks the disk. Scans and metadata lookups run off the
// request path; requests only read what is already cached.
type localMediaCache struct {
	mu       sync.Mutex
	root     string
	items    []localmedia.LocalItem
	problems []string
	scanned  time.Time
	scanning bool
}

func (s *Server) registerLocalMedia(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/localmedia/match", s.matchLocalMedia)
}

// localMediaItems returns the cached local media rows and the problems worth
// showing. A missing folder is a real problem; an unset folder means the feature
// is not in use and says nothing. A stale cache starts one background refresh.
func (s *Server) localMediaItems() ([]library.Item, []string) {
	root := strings.TrimSpace(s.Store.Get().LibraryPath)
	if root == "" {
		return nil, nil
	}
	c := s.local
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	items, problems := c.items, c.problems
	ready := c.root == root
	stale := !ready || time.Since(c.scanned) > localMediaRefreshEvery
	c.mu.Unlock()
	if stale {
		go s.refreshLocalMedia()
	}
	if !ready {
		return nil, nil
	}
	return localLibraryItems(items), problems
}

// refreshLocalMedia scans the configured folder once, publishes the cache-only
// result at once, then fills provider metadata in the background and republishes.
func (s *Server) refreshLocalMedia() {
	root := strings.TrimSpace(s.Store.Get().LibraryPath)
	if root == "" {
		return
	}
	c := s.local
	c.mu.Lock()
	if c.scanning {
		c.mu.Unlock()
		return
	}
	c.scanning = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.scanning = false
		c.mu.Unlock()
	}()

	store, err := localmedia.Open(s.Home)
	if err != nil {
		s.publishLocalMedia(root, nil, []string{err.Error()})
		return
	}
	scanned, problems := localmedia.Scan(root)
	items := store.Items(s.Meta, scanned)
	s.publishLocalMedia(root, items, problems)
	if fillLocalMetadata(items, s.Meta) {
		s.publishLocalMedia(root, store.Items(s.Meta, scanned), problems)
	}
}

// publishLocalMedia replaces the cache only when the folder it came from is
// still the configured one, so a slow scan never overwrites a newer choice.
func (s *Server) publishLocalMedia(root string, items []localmedia.LocalItem, problems []string) {
	c := s.local
	c.mu.Lock()
	defer c.mu.Unlock()
	if root != strings.TrimSpace(s.Store.Get().LibraryPath) {
		return
	}
	c.root, c.items, c.problems, c.scanned = root, items, problems, time.Now()
}

// fillLocalMetadata looks up films the cache doesn't know yet, one at a time and
// capped, and reports whether anything was fetched. Runs off the request path.
func fillLocalMetadata(items []localmedia.LocalItem, meta *metadata.Client) bool {
	seen := map[string]bool{}
	looked := 0
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		key := strings.ToLower(title) + "|" + strconv.Itoa(item.Year)
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, ok := meta.Cached("movie", title, item.Year); ok {
			continue
		}
		if looked == localMediaLookupMax {
			break
		}
		looked++
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		meta.Lookup(ctx, "movie", title, item.Year)
		cancel()
	}
	return looked > 0
}

// matchLocalMedia saves the owner's correction for one file and refreshes the
// list, so a matched title can pick up provider facts and a poster.
func (s *Server) matchLocalMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		Year  int    `json:"year"`
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") ||
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]string{"error": "Enter a title for this file."})
		return
	}
	req.Path, req.Title = strings.TrimSpace(req.Path), strings.TrimSpace(req.Title)
	if req.Path == "" || req.Title == "" || len(req.Title) > 300 || req.Year < 0 || req.Year > 3000 {
		writeJSON(w, 400, map[string]string{"error": "Enter a title (up to 300 characters) and an optional year."})
		return
	}
	root := strings.TrimSpace(s.Store.Get().LibraryPath)
	inside, err := filepath.Abs(req.Path)
	if err != nil || !filepath.IsAbs(req.Path) {
		writeJSON(w, 400, map[string]string{"error": "Choose a file from your local media folder."})
		return
	}
	if rel, err := filepath.Rel(root, inside); root == "" || err != nil || strings.HasPrefix(rel, "..") {
		writeJSON(w, 400, map[string]string{"error": "That file is outside your local media folder."})
		return
	}
	store, err := localmedia.Open(s.Home)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := store.Match(inside, req.Title, req.Year); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	// The library row must show the matched title on the next read.
	s.invalidateData()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		s.Meta.Lookup(ctx, "movie", req.Title, req.Year)
		cancel()
		s.refreshLocalMedia()
	}()
	writeJSON(w, 200, map[string]bool{"saved": true})
}

// unmanagedLocal removes files that sit inside a title the library apps already
// manage, so the library folder is not listed twice. A file or folder the app
// reported is the app's row; only the rest is local media.
func unmanagedLocal(local, managed []library.Item) []library.Item {
	roots := make([]string, 0, len(managed))
	for _, item := range managed {
		if item.App == "Local" || strings.TrimSpace(item.TargetPath) == "" {
			continue
		}
		roots = append(roots, filepath.Clean(item.TargetPath))
	}
	if len(roots) == 0 {
		return local
	}
	out := make([]library.Item, 0, len(local))
	for _, item := range local {
		path := filepath.Clean(item.TargetPath)
		covered := false
		for _, root := range roots {
			same := path == root
			inside := strings.HasPrefix(path, root+string(filepath.Separator))
			if runtime.GOOS == "windows" {
				same = strings.EqualFold(path, root)
				inside = strings.HasPrefix(strings.ToLower(path), strings.ToLower(root)+string(filepath.Separator))
			}
			if same || inside {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, item)
		}
	}
	return out
}

// localLibraryItems maps scanned files onto the shared library row so local
// media sorts, filters and renders with everything else. The file was found by
// the scan, so its honest status is "On disk".
func localLibraryItems(items []localmedia.LocalItem) []library.Item {
	out := make([]library.Item, 0, len(items))
	for _, item := range items {
		out = append(out, library.Item{
			ID:             item.Path,
			App:            "Local",
			Kind:           "library",
			Status:         "on-disk",
			Title:          item.Title,
			Year:           item.Year,
			Genres:         item.Genres,
			Overview:       item.Overview,
			RuntimeMinutes: item.RuntimeMinutes,
			Rating:         item.Rating,
			PosterPath:     item.PosterPath,
			TargetPath:     item.Path,
		})
	}
	return out
}
