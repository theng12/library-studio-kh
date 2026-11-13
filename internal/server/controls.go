package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"librarystudio/internal/library"
	"librarystudio/internal/stock"
)

var mediaApps = map[string]bool{"Sonarr": true, "Radarr": true, "Lidarr": true}

// runReveal shows a path in Finder. Tests replace it so Finder never opens.
var runReveal = func(name string, args ...string) error { return exec.Command(name, args...).Run() }

// revealDownload shows a listed torrent's save path in Finder, only on the
// owner's click. The path must be absolute and exist; nothing else is opened.
func (s *Server) revealDownload(hash string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("Show in Finder is only available on macOS.")
	}
	t, err := library.Torrent(s.Store.Get(), hash)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(t.SavePath) {
		return errors.New("qBittorrent didn't report a full folder path for that download.")
	}
	p := filepath.Clean(t.SavePath)
	if _, err := os.Stat(p); err != nil {
		return errors.New("That download's folder isn't on this Mac anymore.")
	}
	return runReveal("open", "-R", p)
}

// registerControls adds the download queue and media choice routes. The lead calls it from Handler.
func (s *Server) registerControls(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/downloads", func(w http.ResponseWriter, r *http.Request) {
		items, problems, err := library.DownloadQueue(s.Store.Get(), s.Home, s.Meta)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		if items == nil {
			items = []library.Download{}
		}
		writeJSON(w, 200, map[string]any{"items": items, "problems": problems})
	})
	mux.HandleFunc("POST /api/downloads/action", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hashes []string `json:"hashes"`
			Action string   `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "That request could not be read."})
			return
		}
		errs := []string{}
		switch req.Action {
		case "pause", "resume":
			if err := library.TorrentAction(s.Store.Get(), req.Hashes, req.Action); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
		case "remove", "clear": // clear is the same metadata-only removal, for rows whose files are gone
			var err error
			if errs, err = library.RemoveTorrents(s.Store.Get(), req.Hashes); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
		default:
			writeJSON(w, 400, map[string]string{"error": `action must be "pause", "resume", "remove" or "clear"`})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": len(errs) == 0, "errors": errs})
	})
	mux.HandleFunc("POST /api/downloads/reveal", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hash string `json:"hash"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Hash == "" {
			writeJSON(w, 400, map[string]string{"error": "Choose a download to show."})
			return
		}
		if err := s.revealDownload(req.Hash); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "errors": []string{}})
	})
	mux.HandleFunc("GET /api/media/options", func(w http.ResponseWriter, r *http.Request) {
		app := r.URL.Query().Get("app")
		if !mediaApps[app] {
			writeJSON(w, 400, map[string]string{"error": "app must be Sonarr, Radarr or Lidarr."})
			return
		}
		o, err := library.MediaOptions(s.Store.Get(), app)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, o)
	})
	mux.HandleFunc("POST /api/media/add", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			App               string          `json:"app"`
			Raw               json.RawMessage `json:"raw"`
			QualityProfileID  int             `json:"qualityProfileId"`
			RootFolderPath    string          `json:"rootFolderPath"`
			MetadataProfileID int             `json:"metadataProfileId"`
			Monitored         *bool           `json:"monitored"`
			Search            bool            `json:"search"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || len(req.Raw) == 0 {
			writeJSON(w, 400, map[string]string{"error": "That result could not be read."})
			return
		}
		if !mediaApps[req.App] {
			writeJSON(w, 400, map[string]string{"error": "app must be Sonarr, Radarr or Lidarr."})
			return
		}
		sel := stock.AddSelection{
			QualityProfileID:  req.QualityProfileID,
			RootFolderPath:    req.RootFolderPath,
			MetadataProfileID: req.MetadataProfileID,
			Monitored:         req.Monitored,
			Search:            req.Search,
		}
		where, err := library.AddSelected(s.Store.Get(), req.App, req.Raw, sel)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"folder": where})
	})
	mux.HandleFunc("POST /api/media/monitor", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			App       string `json:"app"`
			ID        int    `json:"id"`
			Monitored *bool  `json:"monitored"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || !mediaApps[req.App] || req.ID <= 0 || req.Monitored == nil {
			writeJSON(w, 400, map[string]string{"error": "Choose a library item from Sonarr, Radarr or Lidarr."})
			return
		}
		if err := library.Monitor(s.Store.Get(), req.App, req.ID, *req.Monitored); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}
