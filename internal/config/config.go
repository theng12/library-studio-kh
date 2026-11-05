// Package config keeps the owner's settings in one JSON file in the user's config folder.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

type App struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

type QBit struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Config struct {
	CleanupOn    bool   `json:"cleanupOn"`
	CleanupDays  int    `json:"cleanupDays"`
	DownloadPath string `json:"downloadPath"`
	LibraryPath  string `json:"libraryPath"`
	QBit         QBit   `json:"qbittorrent"`
	Sonarr       App    `json:"sonarr"`
	Radarr       App    `json:"radarr"`
	Lidarr       App    `json:"lidarr"`
	Prowlarr     App    `json:"prowlarr"`
}

func Default() Config { return Config{CleanupOn: true, CleanupDays: 7} }

func (c Config) Validate() error {
	if c.CleanupDays < 0 || c.CleanupDays > 3650 {
		return errors.New("days before removal must be between 0 and 3650")
	}
	for name, u := range map[string]string{"qBittorrent": c.QBit.URL, "Sonarr": c.Sonarr.URL, "Radarr": c.Radarr.URL, "Lidarr": c.Lidarr.URL, "Prowlarr": c.Prowlarr.URL} {
		if u == "" {
			continue
		}
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return fmt.Errorf("%s address must look like http://127.0.0.1:8080", name)
		}
	}
	return nil
}

// Dir is where settings and the removal log live. LIBRARY_STUDIO_HOME overrides it for tests.
func Dir() (string, error) {
	if d := os.Getenv("LIBRARY_STUDIO_HOME"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Library Studio KH"), nil
}

type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// Open loads settings; a missing file means defaults, an unreadable one is an error.
func Open(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "settings.json"), cfg: Default()}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		return nil, fmt.Errorf("settings file %s is damaged: %w", s.path, err)
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Save writes atomically. shortcut: keys sit in a 0600 file like the *arr apps' own config; move to the OS keychain if the owner wants that.
func (s *Store) Save(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = c
	s.mu.Unlock()
	return nil
}
