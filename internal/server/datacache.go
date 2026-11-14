package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"librarystudio/internal/library"
)

// Pages read the last snapshot at once, while one background rebuild talks to
// the library apps. The snapshot is also written to disk, so a warm launch can
// paint the library before any app answers. A short age keeps live changes
// visible without hammering the apps on every poll.
const (
	dataCacheFile   = "library-cache.json"
	dataCacheMaxAge = 20 * time.Second
)

type dataSnapshot struct {
	Items    []library.Item    `json:"items"`
	Problems []string          `json:"problems"`
	Snapshot *library.Snapshot `json:"snapshot,omitempty"`
	BuiltAt  time.Time         `json:"builtAt"`
}

type dataCache struct {
	mu       sync.Mutex
	cond     *sync.Cond
	data     dataSnapshot
	ready    bool
	building bool
	loaded   bool
}

func (s *Server) dataCachePath() string { return filepath.Join(s.Home, dataCacheFile) }

// pageData returns the cached library and activity snapshot. A stale cache is
// served as-is while one background rebuild runs; with no cache at all the first
// caller waits for the build rather than seeing an empty page.
func (s *Server) pageData() dataSnapshot {
	c := &s.data
	c.mu.Lock()
	if !c.loaded {
		c.loaded = true
		if data, ok := readDataCache(s.dataCachePath()); ok {
			c.data, c.ready = data, true
		}
	}
	for c.building {
		c.cond.Wait()
	}
	if c.ready && time.Since(c.data.BuiltAt) <= dataCacheMaxAge {
		data := c.data
		c.mu.Unlock()
		return data
	}
	c.building = true
	hadData, data := c.ready, c.data
	c.mu.Unlock()
	if hadData {
		go s.collectPageData()
		return data
	}
	return s.collectPageData()
}

// invalidateData makes the next page request rebuild, for a change the owner
// just made (settings, cleanup, a new download).
func (s *Server) invalidateData() {
	c := &s.data
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()
}

func (s *Server) collectPageData() dataSnapshot {
	started := time.Now()
	cfg := s.Store.Get()
	items, problems := library.Inventory(cfg, s.Home, s.Meta)
	local, localProblems := s.localMediaItems()
	items = append(items, unmanagedLocal(local, items)...)
	problems = append(problems, localProblems...)
	snapshot := library.Gather(cfg, s.Home, s.Now(), s.Meta)
	data := dataSnapshot{Items: items, Problems: problems, Snapshot: &snapshot, BuiltAt: s.Now()}

	c := &s.data
	c.mu.Lock()
	c.data, c.ready, c.building = data, true, false
	c.cond.Broadcast()
	c.mu.Unlock()

	if err := writeDataCache(s.dataCachePath(), data); err != nil {
		log.Printf("cache: snapshot not saved: %v", err)
	}
	log.Printf("cache: rebuilt in %s (items=%d problems=%d)", time.Since(started).Round(time.Millisecond), len(items), len(problems))
	return data
}

func readDataCache(path string) (dataSnapshot, bool) {
	var data dataSnapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return data, false
	}
	if err := json.Unmarshal(b, &data); err != nil || data.BuiltAt.IsZero() || data.Snapshot == nil {
		return dataSnapshot{}, false
	}
	return data, true
}

func writeDataCache(path string, data dataSnapshot) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
