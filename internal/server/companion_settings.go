package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"librarystudio/internal/library"
)

// RegisterCompanionSettings adds the native companion settings routes. The
// lead calls it from Handler before guard, so the existing host, token and CSRF
// checks cover both routes.
func (s *Server) RegisterCompanionSettings(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/companion/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, library.CompanionSettings(s.Store.Get()))
	})

	mux.HandleFunc("POST /api/companion/settings", func(w http.ResponseWriter, r *http.Request) {
		var p library.CompanionPatch
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			writeJSON(w, 400, map[string]string{"error": "That settings change could not be read."})
			return
		}
		app, err := library.ApplyCompanionSettings(s.Store.Get(), p)
		if err != nil {
			var bad *library.ValidationError
			if errors.As(err, &bad) {
				writeJSON(w, 400, map[string]string{"error": bad.Error()})
				return
			}
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"app": app})
	})
}
