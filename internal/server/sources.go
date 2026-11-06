package server

import (
	"net/http"

	"librarystudio/internal/library"
)

// registerSources adds the read-only source status route and the explicit
// catalog, edit, add, save and test routes. The lead calls it from Handler; the
// existing guard and auth wrap them.
func (s *Server) registerSources(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sources", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, library.Sources(s.Store.Get()))
	})
	s.registerSourceManagement(mux)
}
