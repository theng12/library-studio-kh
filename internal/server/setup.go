package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"librarystudio/internal/companioninstall"
	"librarystudio/internal/config"
	"librarystudio/internal/setup"
)

func (s *Server) registerSetup(mux *http.ServeMux) {
	if local, err := url.Parse(s.LocalURL); err == nil {
		if port, err := strconv.Atoi(local.Port()); err == nil {
			s.setup.SetHostPort(port)
		}
	}
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/setup/plan", func(w http.ResponseWriter, r *http.Request) {
		var req setup.Request
		if !readSetup(w, r, &req) {
			return
		}
		job, err := s.setup.Plan(req)
		setupReply(w, job, err)
	})
	mux.HandleFunc("POST /api/setup/run", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if !readSetup(w, r, &req) {
			return
		}
		job, err := s.setup.Run(req.ID)
		setupReply(w, job, err)
	})
	mux.HandleFunc("POST /api/setup/open", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID  string                 `json:"id"`
			App companioninstall.AppID `json:"app"`
		}
		if !readSetup(w, r, &req) {
			return
		}
		job, err := s.setup.Open(r.Context(), req.ID, req.App)
		setupReply(w, job, err)
	})
	mux.HandleFunc("POST /api/setup/check", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if !readSetup(w, r, &req) {
			return
		}
		job, err := s.setup.Continue(req.ID)
		setupReply(w, job, err)
	})
	mux.HandleFunc("POST /api/setup/preferences", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID                string `json:"id"`
			QualityProfileID  int    `json:"qualityProfileId"`
			MetadataProfileID int    `json:"metadataProfileId"`
		}
		if !readSetup(w, r, &req) {
			return
		}
		job, err := s.setup.ChooseMusicProfiles(req.ID, req.QualityProfileID, req.MetadataProfileID)
		setupReply(w, job, err)
	})
	mux.HandleFunc("POST /api/setup/client", s.setupClient)
	mux.HandleFunc("POST /api/setup/optional", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID      string                 `json:"id"`
			App     companioninstall.AppID `json:"app"`
			Action  string                 `json:"action"`
			Confirm bool                   `json:"confirm"`
		}
		if !readSetup(w, r, &req) {
			return
		}
		job, err := s.setup.OptionalAction(r.Context(), req.ID, req.App, req.Action, req.Confirm)
		setupReply(w, job, err)
	})
}

func readSetup(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		writeJSON(w, 400, map[string]string{"error": "Setup choices could not be read. Check the fields and try again."})
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "Send one setup request at a time."})
		return false
	}
	return true
}

func setupReply(w http.ResponseWriter, job setup.Job, err error) {
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "The current user's home folder is unavailable."})
		return
	}
	c := s.Store.Get()
	download := c.DownloadPath
	if download == "" {
		download = filepath.Join(home, "Downloads", "Library Studio KH")
	}
	movies := c.LibraryPath
	if movies == "" {
		movies = filepath.Join(home, "Movies", "Films")
	}
	defaults := setup.Request{Apps: []companioninstall.AppID{companioninstall.QBittorrent, companioninstall.Sonarr, companioninstall.Radarr, companioninstall.Lidarr, companioninstall.Prowlarr}, AppDir: filepath.Join(home, "Applications"), DownloadPath: download, Roots: map[string]string{"sonarr": filepath.Join(home, "Movies", "TV Shows"), "radarr": movies, "lidarr": filepath.Join(home, "Music", "Library")}, Ports: map[string]int{"qbittorrent": 8080, "sonarr": 8989, "radarr": 7878, "lidarr": 8686, "prowlarr": 9696}, Categories: map[string]string{"sonarr": "tv", "radarr": "movies", "lidarr": "music"}, RenameTV: true, StartAtLogin: false}
	job, err := s.setup.Status()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"supported":   runtime.GOOS == "darwin" && runtime.GOARCH == "arm64",
		"platform":    runtime.GOOS + "/" + runtime.GOARCH,
		// The full official catalog, so the UI can show which entries are
		// optional helpers; defaults never include them.
		"apps": companioninstall.Registry(),
		"defaults": defaults, "job": job, "clientAllowed": s.bootstrap(r) && loopbackPeer(r)})
}

func (s *Server) setupClient(w http.ResponseWriter, r *http.Request) {
	_, remote := r.Context().Value(remoteOriginHostKey{}).(string)
	if remote || !loopbackPeer(r) || !s.bootstrap(r) {
		writeJSON(w, 403, map[string]string{"error": "Choose client mode in this computer's desktop app."})
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if !readSetup(w, r, &req) {
		return
	}
	host, err := config.ClientHostURL(req.URL)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimRight(s.LocalURL, "/") == host {
		writeJSON(w, 400, map[string]string{"error": "This is the current local host. Enter the other computer's Library webpage address."})
		return
	}
	// Probe without a cookie jar, bootstrap header, URL credentials or redirects.
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	probe, err := http.NewRequestWithContext(r.Context(), http.MethodGet, host+"/api/auth/status", nil)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "The host address could not be used."})
		return
	}
	resp, err := client.Do(probe)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "The host did not answer. Check its webpage address and private network connection."})
		return
	}
	defer resp.Body.Close()
	var status struct {
		Authenticated     *bool `json:"authenticated"`
		SetupRequired     *bool `json:"setupRequired"`
		MinPasswordLength *int  `json:"minPasswordLength"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&status) != nil || status.Authenticated == nil || status.SetupRequired == nil || status.MinPasswordLength == nil || *status.MinPasswordLength < 1 {
		writeJSON(w, 502, map[string]string{"error": "That address did not return a compatible Library sign-in page. No local credentials were sent."})
		return
	}
	if *status.SetupRequired {
		writeJSON(w, 409, map[string]string{"error": "Finish creating the owner account on that host before connecting this client."})
		return
	}
	host, err = config.SaveClientHost(s.Home, host)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "The client choice could not be saved; the current local host was preserved."})
		return
	}
	writeJSON(w, 200, map[string]string{"url": host})
}
