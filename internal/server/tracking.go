package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"librarystudio/internal/library"
	"librarystudio/internal/stock"
)

// registerTracking adds the media detail, child tracking and release routes.
// The lead calls it from Handler; the existing guard and auth cover all of them.
func (s *Server) registerTracking(mux *http.ServeMux) {
	s.registerMovieExtras(mux)
	mux.HandleFunc("GET /api/media/details", func(w http.ResponseWriter, r *http.Request) {
		app := r.URL.Query().Get("app")
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if !mediaApps[app] || err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "App must be Sonarr, Radarr or Lidarr and id must be a library item."})
			return
		}
		d, err := library.Detail(s.Store.Get(), app, id)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, d)
	})

	mux.HandleFunc("POST /api/media/tracking", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			App          string `json:"app"`
			ID           int    `json:"id"`
			Kind         string `json:"kind"`
			ChildID      int    `json:"childId"`
			SeasonNumber *int   `json:"seasonNumber"`
			Monitored    *bool  `json:"monitored"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "That request could not be read."})
			return
		}
		ok := req.ID > 0 && req.Monitored != nil && trackingKindOK(req.App, req.Kind)
		switch {
		case req.App == "Radarr":
			writeJSON(w, 400, map[string]string{"error": "Radarr has no seasons, episodes or albums to track."})
			return
		case !mediaApps[req.App]:
			ok = false
		case req.Kind == "season":
			ok = ok && req.SeasonNumber != nil
		default:
			ok = ok && req.ChildID > 0
		}
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "Choose a library item, what to track and whether it is monitored."})
			return
		}
		season := 0
		if req.SeasonNumber != nil {
			season = *req.SeasonNumber
		}
		if err := library.SetTracking(s.Store.Get(), req.App, req.ID, req.Kind, req.ChildID, season, *req.Monitored); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/media/releases", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		app, id, episodeID, albumID, ok := releaseIDs(q.Get("app"), q.Get("id"), q.Get("episodeId"), q.Get("albumId"))
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "App must be Sonarr, Radarr or Lidarr, with the episode or album being searched."})
			return
		}
		items, err := library.Releases(s.Store.Get(), app, stock.ReleaseContext{ID: id, EpisodeID: episodeID, AlbumID: albumID})
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		if items == nil {
			items = []stock.Release{}
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})

	mux.HandleFunc("POST /api/media/grab", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			App       string `json:"app"`
			ID        int    `json:"id"`
			EpisodeID int    `json:"episodeId"`
			AlbumID   int    `json:"albumId"`
			Key       string `json:"key"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "That request could not be read."})
			return
		}
		app, id, episodeID, albumID, ok := releaseIDs(req.App, strconv.Itoa(req.ID), strconv.Itoa(req.EpisodeID), strconv.Itoa(req.AlbumID))
		if !ok || req.Key == "" {
			writeJSON(w, 400, map[string]string{"error": "Choose a searched release to grab."})
			return
		}
		if err := library.Grab(s.Store.Get(), app, stock.ReleaseContext{ID: id, EpisodeID: episodeID, AlbumID: albumID}, req.Key); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}

func trackingKindOK(app, kind string) bool {
	switch app {
	case "Sonarr":
		return kind == "season" || kind == "episode"
	case "Lidarr":
		return kind == "album"
	}
	return false
}

// releaseIDs validates a release or detail request: known app, positive item
// id, and the child id that app's search needs.
func releaseIDs(app, idText, episodeText, albumText string) (string, int, int, int, bool) {
	if !mediaApps[app] {
		return "", 0, 0, 0, false
	}
	id, err := strconv.Atoi(idText)
	if err != nil || id <= 0 {
		return "", 0, 0, 0, false
	}
	episodeID, albumID := 0, 0
	if app == "Sonarr" {
		episodeID, err = strconv.Atoi(episodeText)
		if err != nil || episodeID <= 0 {
			return "", 0, 0, 0, false
		}
	}
	if app == "Lidarr" {
		albumID, err = strconv.Atoi(albumText)
		if err != nil || albumID <= 0 {
			return "", 0, 0, 0, false
		}
	}
	return app, id, episodeID, albumID, true
}
