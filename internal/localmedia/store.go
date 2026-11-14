package localmedia

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"librarystudio/internal/metadata"
)

const storeFile = "localmedia.json"

// Match is an owner-chosen correction for one file, keyed by absolute path.
type Match struct {
	Title  string `json:"title"`
	Year   int    `json:"year"`
	Manual bool   `json:"manual"`
}

type stored struct {
	Matches map[string]Match `json:"matches,omitempty"`
}

// Store keeps manual matches in <home>/localmedia.json. The scanned folder is
// not stored here; the caller owns which folder to scan.
type Store struct {
	path    string
	mu      sync.Mutex
	matches map[string]Match
}

// Open reads the store, or starts an empty one when the file does not exist.
func Open(home string) (*Store, error) {
	if strings.TrimSpace(home) == "" {
		return nil, errors.New("local media store: home is not set")
	}
	store := &Store{path: filepath.Join(home, storeFile), matches: map[string]Match{}}
	b, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("local media store: %w", err)
	}
	var file stored
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("local media store %s: %w", store.path, err)
	}
	for path, match := range file.Matches {
		store.matches[path] = match
	}
	return store, nil
}

// Match records the owner's title and year for one file and saves at once.
func (s *Store) Match(path, title string, year int) error {
	path, title = strings.TrimSpace(path), strings.TrimSpace(title)
	if path == "" || title == "" {
		return errors.New("local media match needs a file and a title")
	}
	key, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matches[key] = Match{Title: title, Year: year, Manual: true}
	return s.saveLocked()
}

// Items lists stored manual matches, enriched from the metadata cache only —
// never from the network. Pass the result of Scan(root) to list scanned files
// with the manual matches applied; without it only stored matches are listed.
func (s *Store) Items(meta *metadata.Client, scanned ...[]LocalItem) []LocalItem {
	s.mu.Lock()
	matches := make(map[string]Match, len(s.matches))
	for path, match := range s.matches {
		matches[path] = match
	}
	s.mu.Unlock()

	var items []LocalItem
	if len(scanned) > 0 {
		items = append(items, scanned[0]...)
		for i := range items {
			if match, ok := matches[absKey(items[i].Path)]; ok {
				items[i].Title, items[i].Year = match.Title, match.Year
				items[i].Manual, items[i].Source = true, "manual"
			}
		}
	} else {
		paths := make([]string, 0, len(matches))
		for path := range matches {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		items = make([]LocalItem, 0, len(paths))
		for _, path := range paths {
			match := matches[path]
			items = append(items, LocalItem{Path: path, Title: match.Title, Year: match.Year, Manual: true, Source: "manual"})
		}
	}
	for i := range items {
		info, ok := meta.Cached("movie", items[i].Title, items[i].Year)
		if !ok {
			continue
		}
		if items[i].Overview == "" {
			items[i].Overview = info.Overview
		}
		if len(items[i].Genres) == 0 {
			items[i].Genres = info.Genres
		}
		if items[i].Rating == 0 {
			items[i].Rating = info.Rating
		}
		if items[i].RuntimeMinutes == 0 {
			items[i].RuntimeMinutes = info.RuntimeMinutes
		}
		if info.PosterPath != "" {
			items[i].PosterPath = filepath.Base(info.PosterPath)
		}
	}
	return items
}

func absKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func (s *Store) saveLocked() error {
	b, _ := json.MarshalIndent(stored{Matches: s.matches}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
