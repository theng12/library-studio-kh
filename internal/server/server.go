// Package server serves the dashboard and its small JSON API on 127.0.0.1 only.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	librarystudio "librarystudio"
	"librarystudio/internal/config"
	"librarystudio/internal/library"
)

//go:embed web
var web embed.FS

type Server struct {
	Store *config.Store
	Home  string
	Token string
	Now   func() time.Time

	mu   sync.Mutex
	last *lastRun
}

type lastRun struct {
	At      time.Time `json:"at"`
	Removed int       `json:"removed"`
	Errors  []string  `json:"errors,omitempty"`
	Blocked string    `json:"blocked,omitempty"`
}

func New(store *config.Store, home string) *Server {
	b := make([]byte, 24)
	rand.Read(b)
	return &Server{Store: store, Home: home, Token: hex.EncodeToString(b), Now: time.Now}
}

// RunCleanup is called hourly by main.
func (s *Server) RunCleanup() {
	c := s.Store.Get()
	now := s.Now()
	snap := library.Gather(c, s.Home, now)
	n, errs := library.Cleanup(c, s.Home, snap, now)
	s.mu.Lock()
	s.last = &lastRun{At: now, Removed: n, Errors: errs, Blocked: snap.Blocked}
	s.mu.Unlock()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(web, "web")
	mux.Handle("/", http.FileServerFS(static))
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/settings", s.saveSettings)
	return s.guard(mux)
}

// guard: only this machine's own names (stops DNS rebinding), and the API needs the launch token
// (a custom header also forces a CORS preflight, which is never answered, so other sites can't call it).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Library-Token")), []byte(s.Token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// public hides the keys and password; the screen only learns whether each one is saved.
func public(c config.Config) (config.Config, map[string]bool) {
	saved := map[string]bool{"qbittorrent": c.QBit.Password != "", "sonarr": c.Sonarr.APIKey != "", "radarr": c.Radarr.APIKey != "", "lidarr": c.Lidarr.APIKey != "", "prowlarr": c.Prowlarr.APIKey != ""}
	c.QBit.Password, c.Sonarr.APIKey, c.Radarr.APIKey, c.Lidarr.APIKey, c.Prowlarr.APIKey = "", "", "", "", ""
	return c, saved
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	c := s.Store.Get()
	pub, saved := public(c)
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"version":   librarystudio.Version(),
		"releases":  librarystudio.Releases(),
		"settings":  pub,
		"saved":     saved,
		"snapshot":  library.Gather(c, s.Home, s.Now()),
		"lastClean": last,
	})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	old := s.Store.Get()
	var c config.Config
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&c); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Settings could not be read."})
		return
	}
	keep := func(v *string, was string) {
		if *v == "" {
			*v = was
		}
	}
	keep(&c.QBit.Password, old.QBit.Password)
	keep(&c.Sonarr.APIKey, old.Sonarr.APIKey)
	keep(&c.Radarr.APIKey, old.Radarr.APIKey)
	keep(&c.Lidarr.APIKey, old.Lidarr.APIKey)
	keep(&c.Prowlarr.APIKey, old.Prowlarr.APIKey)
	if err := s.Store.Save(c); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var problems []string
	if c.CleanupOn {
		problems = library.Apply(c)
	}
	writeJSON(w, 200, map[string]any{"problems": problems})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
