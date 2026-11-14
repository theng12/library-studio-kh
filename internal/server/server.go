// Package server serves the dashboard locally, with optional token-protected Tailscale access.
package server

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	librarystudio "librarystudio"
	"librarystudio/internal/config"
	"librarystudio/internal/library"
	"librarystudio/internal/metadata"
	"librarystudio/internal/setup"
	"librarystudio/internal/update"
)

//go:embed web
var web embed.FS

type Server struct {
	Store *config.Store
	Home  string
	Token string
	// LocalURL is set from the bound loopback listener before serving requests.
	LocalURL string
	Now      func() time.Time
	// Meta is the metadata cache; exported so a test entry can point it elsewhere.
	Meta *metadata.Client

	prefetching atomic.Bool // single-flight guard for background metadata fills
	mu          sync.Mutex
	last        *lastRun
	remote      remoteAccess
	access      accessState
	cleanup     *library.StagedCleanup
	setup       *setup.Manager
}

type lastRun struct {
	At      time.Time `json:"at"`
	Removed int       `json:"removed"`
	Errors  []string  `json:"errors,omitempty"`
	Blocked string    `json:"blocked,omitempty"`
}

func New(store *config.Store, home string) *Server {
	s := &Server{Store: store, Home: home, Now: time.Now, Meta: &metadata.Client{Dir: filepath.Join(home, "metadata")}}
	s.cleanup = library.NewStagedCleanup(store.Get, home)
	s.setup = setup.NewManager(home, store)
	s.initAccess()
	return s
}

// RunCleanup is called hourly by main. Staged cleanup only acts when its
// policy is on; the dead-download sweep only when its own switch is on.
func (s *Server) RunCleanup() {
	s.runStagedCleanup()
	c := s.Store.Get()
	if c.DeadArrCleanupOn {
		// The app queue row goes first, while the item is still visible to the arr.
		removed, problems, err := library.SweepDeadArr(c, s.Home, s.Now(), true)
		log.Printf("downloads: arr dead sweep removed=%d problems=%q err=%v", len(removed), strings.Join(problems, " "), err)
	}
	if !c.DeadCleanupOn {
		return
	}
	removed, problems, err := library.SweepDead(c, s.Home, s.Now(), true)
	log.Printf("downloads: dead sweep removed=%d problems=%q err=%v", len(removed), strings.Join(problems, " "), err)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerAuth(mux)
	s.registerControls(mux)
	s.registerTracking(mux)
	s.registerSources(mux)
	s.registerSubtitles(mux)
	s.registerCleanup(mux)
	s.registerFolders(mux)
	s.registerSetup(mux)
	s.RegisterCompanionSettings(mux)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/host-proof", s.hostProof)
	mux.HandleFunc("GET /api/poster", s.poster)
	mux.HandleFunc("GET /api/update/check", s.updateCheck)
	mux.HandleFunc("POST /api/update/open", s.openUpdater)
	static, _ := fs.Sub(web, "web")
	mux.Handle("/", http.FileServerFS(static))
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/library", s.inventory)
	mux.HandleFunc("GET /api/library/detail", s.inventoryDetail)
	mux.HandleFunc("POST /api/settings", s.saveSettings)
	mux.HandleFunc("GET /api/browse", func(w http.ResponseWriter, r *http.Request) {
		items, errs := library.Browse(s.Store.Get())
		writeJSON(w, 200, map[string]any{"items": items, "problems": errs})
	})
	mux.HandleFunc("GET /api/find", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" || len(q) > 200 {
			writeJSON(w, 400, map[string]string{"error": "Type what you're looking for."})
			return
		}
		items, errs := library.Find(s.Store.Get(), q)
		writeJSON(w, 200, map[string]any{"items": items, "problems": errs})
	})
	mux.HandleFunc("POST /api/add", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			App string          `json:"app"`
			Raw json.RawMessage `json:"raw"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || len(req.Raw) == 0 {
			writeJSON(w, 400, map[string]string{"error": "That result could not be read."})
			return
		}
		where, err := library.Add(s.Store.Get(), req.App, req.Raw)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"folder": where})
	})
	mux.HandleFunc("POST /api/torrents", s.addTorrents)
	return s.guard(mux)
}

const maxTorrentUpload = 32 << 20

// addTorrents takes pasted links (one per line) and .torrent files, single or many, and passes them to qBittorrent.
func (s *Server) addTorrents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentUpload)
	if err := r.ParseMultipartForm(maxTorrentUpload); err != nil {
		writeJSON(w, 400, map[string]string{"error": "That was too big or couldn't be read. Add up to 32 MB of torrent files at a time."})
		return
	}
	var links []string
	for _, l := range strings.Split(r.FormValue("links"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		if !strings.HasPrefix(low, "magnet:?") && !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
			writeJSON(w, 400, map[string]string{"error": "This line isn't a magnet or web link: " + truncate(l, 80)})
			return
		}
		links = append(links, l)
	}
	files := map[string][]byte{}
	for _, fh := range r.MultipartForm.File["torrents"] {
		if !strings.HasSuffix(strings.ToLower(fh.Filename), ".torrent") {
			writeJSON(w, 400, map[string]string{"error": fh.Filename + " isn't a .torrent file."})
			return
		}
		f, err := fh.Open()
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": fh.Filename + " couldn't be read."})
			return
		}
		b, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": fh.Filename + " couldn't be read."})
			return
		}
		files[fh.Filename] = b
	}
	if len(links)+len(files) == 0 {
		writeJSON(w, 400, map[string]string{"error": "Paste a link or choose a .torrent file first."})
		return
	}
	if len(links) > 500 {
		writeJSON(w, 400, map[string]string{"error": "Up to 500 links at a time, please."})
		return
	}
	if err := library.AddTorrents(s.Store.Get(), links, files); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]int{"links": len(links), "files": len(files)})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if s.access.err != nil {
				writeJSON(w, 503, map[string]string{"error": s.access.err.Error()})
				return
			}
			if !s.safeWrite(r) {
				writeJSON(w, 403, map[string]string{"error": "This request wasn't accepted. Use the Library page on its own host address."})
				return
			}
			publicAuth := r.URL.Path == "/api/auth/status" || r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/setup" || r.URL.Path == "/api/host-proof"
			if !publicAuth && !s.authorized(r) {
				writeJSON(w, 403, map[string]string{"error": "Please sign in to Library Studio KH."})
				return
			}
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// public hides the keys and password; the screen only learns whether each one is saved.
func public(c config.Config) (config.Config, map[string]bool) {
	saved := map[string]bool{"qbittorrent": c.QBit.Password != "", "sonarr": c.Sonarr.APIKey != "", "radarr": c.Radarr.APIKey != "", "lidarr": c.Lidarr.APIKey != "", "prowlarr": c.Prowlarr.APIKey != "", "bazarr": c.Bazarr.APIKey != "", "whisparr": c.Whisparr.APIKey != ""}
	c.QBit.Password, c.Sonarr.APIKey, c.Radarr.APIKey, c.Lidarr.APIKey, c.Prowlarr.APIKey, c.Bazarr.APIKey, c.Whisparr.APIKey = "", "", "", "", "", "", ""
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
		"snapshot":  library.Gather(c, s.Home, s.Now(), s.Meta),
		"metadata":  s.Meta.Notice(),
		"lastClean": last,
		"remote":    s.RemoteStatus(),
		"localUrl":  s.LocalURL,
	})
}

// inventory lists the library with cache-only metadata, then fills cache gaps in the background.
func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	items, problems := library.Inventory(s.Store.Get(), s.Home, s.Meta)
	if problems == nil {
		problems = []string{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "problems": problems, "metadata": s.Meta.Notice()})
	s.prefetch(items)
}

// inventoryDetail returns one inventory item by app and media ID. Cache-only.
func (s *Server) inventoryDetail(w http.ResponseWriter, r *http.Request) {
	app := r.URL.Query().Get("app")
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if (app != "Sonarr" && app != "Radarr" && app != "Lidarr") || err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "Choose a show, film or artist from the library."})
		return
	}
	items, _ := library.Inventory(s.Store.Get(), s.Home, s.Meta)
	for _, it := range items {
		if it.App == app && it.MediaID == id {
			writeJSON(w, 200, it)
			return
		}
	}
	writeJSON(w, 404, map[string]string{"error": app + " doesn't list that item."})
}

// prefetch runs one background metadata fill at a time; overlapping polls skip.
func (s *Server) prefetch(items []library.Item) {
	if !s.prefetching.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.prefetching.Store(false)
		library.Prefetch(items, s.Meta)
	}()
}

// poster serves one cached poster image by file name. Only files directly in the
// metadata poster cache are reachable; paths, traversal and other names are refused.
func (s *Server) poster(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("path"))
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		http.Error(w, "No such poster.", http.StatusNotFound)
		return
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		http.Error(w, "No such poster.", http.StatusNotFound)
		return
	}
	path := filepath.Join(s.Home, "metadata", "posters", name)
	b, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "No such poster.", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mimeByExt(name))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(b)
}

func mimeByExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	}
	return "image/jpeg"
}

// updateCheck reads the shared KH feed; it never downloads or installs.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, 200, update.Result{State: "failed", Reason: "this Mac's home folder could not be read"})
		return
	}
	writeJSON(w, 200, update.Check(home, update.InstalledVersion(librarystudio.Version()), nil))
}

// openUpdater brings Studio Launcher KH forward; the install stays in that app.
func (s *Server) openUpdater(w http.ResponseWriter, r *http.Request) {
	if err := update.OpenLauncher(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"opened": "Studio Launcher KH"})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	old := s.Store.Get()
	c := old // omitted fields (older forms) keep their saved/default values
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
	keep(&c.Bazarr.APIKey, old.Bazarr.APIKey)
	keep(&c.Whisparr.APIKey, old.Whisparr.APIKey)
	if err := s.Store.Save(c); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var problems []string
	if err := s.ConfigureRemote(); err != nil {
		problems = append(problems, err.Error())
	}
	if c.CleanupOn {
		problems = append(problems, library.Apply(c)...)
	}
	writeJSON(w, 200, map[string]any{"problems": problems})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
