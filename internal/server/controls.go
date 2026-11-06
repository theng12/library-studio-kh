package server

import (
	"encoding/json"
	"net/http"

	"librarystudio/internal/library"
	"librarystudio/internal/stock"
)

var mediaApps = map[string]bool{"Sonarr": true, "Radarr": true, "Lidarr": true}

// registerControls adds the download queue and media choice routes. The lead calls it from Handler.
func (s *Server) registerControls(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/downloads", func(w http.ResponseWriter, r *http.Request) {
		items, problems, err := library.DownloadQueue(s.Store.Get())
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
		if err := library.TorrentAction(s.Store.Get(), req.Hashes, req.Action); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
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
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || len(req.Raw) == 0 {
			writeJSON(w, 400, map[string]string{"error": "That result could not be read."})
			return
		}
		if !mediaApps[req.App] {
			writeJSON(w, 400, map[string]string{"error": "app must be Sonarr, Radarr or Lidarr."})
			return
		}
		sel := stock.AddSelection{QualityProfileID: req.QualityProfileID, RootFolderPath: req.RootFolderPath, MetadataProfileID: req.MetadataProfileID}
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
