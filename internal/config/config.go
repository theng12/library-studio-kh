// Package config keeps the owner's settings in one JSON file in the user's config folder.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	TailscaleOn   bool   `json:"tailscaleOn"`
	TailscalePort int    `json:"tailscalePort"`
	CleanupOn     bool   `json:"cleanupOn"`
	CleanupDays   int    `json:"cleanupDays"`
	DownloadPath  string `json:"downloadPath"`
	LibraryPath   string `json:"libraryPath"`
	// Staged local-copy cleanup (docs/STORAGE_POLICY.md). Off unless the owner switches it on.
	StagedCleanupOn         bool    `json:"stagedCleanupOn"`
	ManagedTrashPath        string  `json:"managedTrashPath"`        // empty = DownloadPath/_trash; must stay on a local volume, never the NAS library
	StagedDays              int     `json:"stagedDays"`              // days in the normal download folder after verified import
	ManagedTrashDays        int     `json:"managedTrashDays"`        // extra days in Library's _trash
	NativeTrashDays         int     `json:"nativeTrashDays"`         // days kept in the OS Trash
	PressureBypassRetention bool    `json:"pressureBypassRetention"` // explicit pressure override for verified completed local copies
	PressureFreeGB          int     `json:"pressureFreeGB"`          // purge eligible native-Trash items below this; 0 = no pressure purge
	SeedRatio               float64 `json:"seedRatio"`
	SeedMinutes             int     `json:"seedMinutes"`
	QBit                    QBit    `json:"qbittorrent"`
	Sonarr                  App     `json:"sonarr"`
	Radarr                  App     `json:"radarr"`
	Lidarr                  App     `json:"lidarr"`
	Prowlarr                App     `json:"prowlarr"`
	BazarrOn                bool    `json:"bazarrOn"`
	Bazarr                  App     `json:"bazarr"`
	// Whisparr is an optional native Arr app (v3, Radarr-derived). Off by
	// default; nothing queries or starts it unless the owner enables it.
	WhisparrOn bool `json:"whisparrOn"`
	Whisparr   App  `json:"whisparr"`
}

const StagedCleanupNotice = "The old permanent-delete cleanup is retired. Use the staged cleanup switch instead."

func Default() Config {
	return Config{
		CleanupOn: false, CleanupDays: 7, TailscalePort: 7882,
		StagedCleanupOn: false, StagedDays: 3, ManagedTrashDays: 4, NativeTrashDays: 30,
		PressureFreeGB: 50, SeedRatio: 2, SeedMinutes: 2880,
	}
}

func (c Config) Validate() error {
	if c.CleanupOn {
		return errors.New(StagedCleanupNotice)
	}
	if c.TailscaleOn && (c.TailscalePort < 1024 || c.TailscalePort > 65535) {
		return errors.New("Tailscale port must be a whole number from 1024 to 65535")
	}
	if c.CleanupDays < 0 || c.CleanupDays > 3650 {
		return errors.New("days before removal must be between 0 and 3650")
	}
	if c.StagedDays < 1 || c.StagedDays > 365 {
		return errors.New("days in the download folder must be between 1 and 365")
	}
	if c.ManagedTrashDays < 0 || c.ManagedTrashDays > 365 {
		return errors.New("days in Library's _trash must be between 0 and 365")
	}
	if c.NativeTrashDays < 1 || c.NativeTrashDays > 3650 {
		return errors.New("days in the system Trash must be between 1 and 3650")
	}
	if c.PressureFreeGB < 0 || c.PressureFreeGB > 10_000_000 {
		return errors.New("free-space pressure threshold must be between 0 and 10000000 GB")
	}
	if math.IsNaN(c.SeedRatio) || math.IsInf(c.SeedRatio, 0) || c.SeedRatio < 0 || c.SeedRatio > 1_000_000 {
		return errors.New("seed ratio must be between 0 and 1000000")
	}
	if c.SeedMinutes < 0 || c.SeedMinutes > 5_256_000 {
		return errors.New("seed minutes must be between 0 and 5256000")
	}
	if c.StagedCleanupOn && c.DownloadPath == "" && strings.TrimSpace(c.ManagedTrashPath) == "" {
		return errors.New("set the local download folder or a managed _trash folder before turning staged cleanup on")
	}
	for name, u := range map[string]string{"qBittorrent": c.QBit.URL, "Sonarr": c.Sonarr.URL, "Radarr": c.Radarr.URL, "Lidarr": c.Lidarr.URL, "Prowlarr": c.Prowlarr.URL, "Bazarr": c.Bazarr.URL, "Whisparr": c.Whisparr.URL} {
		if name == "Bazarr" && !c.BazarrOn {
			continue
		}
		if name == "Whisparr" && !c.WhisparrOn {
			continue
		}
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
	// Retire the old permanent-delete policy without enabling a replacement on migration.
	s.cfg.CleanupOn = false
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Save writes atomically. shortcut: keys sit in a 0600 file like the *arr apps' own config; move to the OS keychain if the owner wants that.
func (s *Store) Save(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(c)
}

// SaveIfUnchanged prevents setup from replacing settings edited during a native check.
func (s *Store) SaveIfUnchanged(before, next Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg != before {
		return errors.New("settings changed during setup; check connections again before saving")
	}
	return s.saveLocked(next)
}

func (s *Store) saveLocked(c Config) error {
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
	s.cfg = c
	return nil
}
