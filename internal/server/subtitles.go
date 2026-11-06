package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"librarystudio/internal/library"
)

func (s *Server) registerSubtitles(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/media/subtitles", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		view, err := library.Subtitles(s.Store.Get(), r.URL.Query().Get("app"), id)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, view)
	})
	for _, action := range []string{"english", "profile"} {
		mux.HandleFunc("POST /api/media/subtitles/"+action, func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				App       string `json:"app"`
				ID        int    `json:"id"`
				ItemID    int    `json:"itemId"`
				ProfileID int    `json:"profileId"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
				writeJSON(w, 400, map[string]string{"error": "That subtitle request could not be read."})
				return
			}
			var err error
			if action == "english" {
				err = library.SearchEnglishSubtitles(s.Store.Get(), req.App, req.ID, req.ItemID)
			} else {
				err = library.AssignEnglishSubtitles(s.Store.Get(), req.App, req.ID, req.ProfileID)
			}
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			if action == "english" {
				writeJSON(w, 200, map[string]any{"ok": true, "status": "requested"})
			} else {
				writeJSON(w, 200, map[string]any{"ok": true})
			}
		})
	}
}
